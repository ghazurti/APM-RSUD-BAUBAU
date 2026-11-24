//go:build integration

// Uji integrasi alur penuh terhadap DB SALINAN Khanza + server BPJS tiruan.
// JANGAN arahkan ke DB produksi. Jalankan:
//   APM_TEST_DSN='root:@tcp(127.0.0.1:3306)/apm_uji?parseTime=true&loc=Asia%2FMakassar' \
//   APM_TEST_NOKA=0001234567890 go test -tags integration ./internal/service/ -v
package service

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	lzstring "github.com/daku10/go-lz-string"
	_ "github.com/go-sql-driver/mysql"

	"apm-khanza/internal/bpjs"
	"apm-khanza/internal/config"
	"apm-khanza/internal/khanza"
)

const consID, secret = "9999", "rahasiauji"

var sepCounter atomic.Int64 // nomor SEP unik lintas mock server

type mockBPJS struct {
	mu        sync.Mutex
	calls     []string
	bodies    map[string][]string
	sepTetap  string // bila diisi, insert SEP selalu mengembalikan noSep ini
	status    string // statusPeserta.kode
	noka, nik string
}

func enc(ts, plain string) string {
	comp, _ := lzstring.CompressToEncodedURIComponent(plain)
	key := sha256.Sum256([]byte(consID + secret + ts))
	block, _ := aes.NewCipher(key[:])
	p := []byte(comp)
	pad := aes.BlockSize - len(p)%aes.BlockSize
	p = append(p, bytes.Repeat([]byte{byte(pad)}, pad)...)
	out := make([]byte, len(p))
	cipher.NewCBCEncrypter(block, key[:16]).CryptBlocks(out, p)
	return base64.StdEncoding.EncodeToString(out)
}

func (m *mockBPJS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ts := r.Header.Get("X-timestamp")
	if r.Header.Get("X-signature") != bpjs.Signature(consID, secret, ts) {
		w.Write([]byte(`{"metaData":{"code":"401","message":"signature salah"}}`))
		return
	}
	body, _ := io.ReadAll(r.Body)
	m.mu.Lock()
	m.calls = append(m.calls, r.Method+" "+r.URL.Path)
	m.bodies[r.URL.Path] = append(m.bodies[r.URL.Path], string(body))
	m.mu.Unlock()

	ok := func(meta, resp string) {
		if resp == "" {
			fmt.Fprintf(w, `{"%s":{"code":"200","message":"Ok"},"response":null}`, meta)
			return
		}
		fmt.Fprintf(w, `{"%s":{"code":"200","message":"OK"},"response":%q}`, meta, enc(ts, resp))
	}
	p := r.URL.Path
	switch {
	case strings.HasPrefix(p, "/vclaim/Peserta/"):
		ok("metaData", fmt.Sprintf(`{"peserta":{"noKartu":"%s","nik":"%s","nama":"AHMAD SALEH","sex":"L","tglLahir":"1990-01-15",
			"mr":{"noMR":"999001","noTelepon":"081234567890"},"statusPeserta":{"kode":"%s","keterangan":"AKTIF"},
			"provUmum":{"kdProvider":"0001","nmProvider":"PUSKESMAS UJI"},"jenisPeserta":{"kode":"1","keterangan":"PNS DAERAH"},
			"hakKelas":{"kode":"1","keterangan":"KELAS I"},"informasi":{"prolanisPRB":null}}}`, m.noka, m.nik, m.status))
	case strings.HasPrefix(p, "/vclaim/Rujukan/List/Peserta/"):
		tgl := time.Now().AddDate(0, 0, -5).Format("2006-01-02")
		lama := time.Now().AddDate(0, 0, -120).Format("2006-01-02")
		ok("metaData", fmt.Sprintf(`{"rujukan":[
			{"noKunjungan":"0001R0011125B000001","tglKunjungan":"%s","provPerujuk":{"kode":"0001R001","nama":"PUSKESMAS UJI"},
			 "poliRujukan":{"kode":"ANA","nama":"ANAK"},"diagnosa":{"kode":"J06.9","nama":"Acute upper respiratory infection"}},
			{"noKunjungan":"0001R0010725B000099","tglKunjungan":"%s","provPerujuk":{"kode":"0001R001","nama":"PUSKESMAS UJI"},
			 "poliRujukan":{"kode":"ANA","nama":"ANAK"},"diagnosa":{"kode":"A09","nama":"Diarrhoea"}}]}`, tgl, lama))
	case strings.HasPrefix(p, "/vclaim/SEP/FingerPrint/"):
		ok("metaData", `{"kode":"1","status":"Peserta sudah verifikasi"}`)
	case p == "/vclaim/SEP/2.0/insert":
		m.mu.Lock()
		no := fmt.Sprintf("1906R0011125V%06d", sepCounter.Add(1))
		if m.sepTetap != "" {
			no = m.sepTetap
		}
		m.mu.Unlock()
		ok("metaData", fmt.Sprintf(`{"sep":{"noSep":"%s"}}`, no))
	case p == "/vclaim/SEP/2.0/delete":
		ok("metaData", `"`+"ok"+`"`)
	case strings.HasPrefix(p, "/antrean/antrean/"):
		ok("metadata", "")
	default:
		w.Write([]byte(`{"metaData":{"code":"201","message":"tidak ditemukan"}}`))
	}
}

func (m *mockBPJS) count(call string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, c := range m.calls {
		if c == call {
			n++
		}
	}
	return n
}

func setup(t *testing.T) (*Service, *sql.DB, *mockBPJS) {
	dsn := os.Getenv("APM_TEST_DSN")
	if dsn == "" || strings.Contains(dsn, "/sik?") {
		t.Skip("APM_TEST_DSN belum di-set (harus DB salinan, bukan sik)")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	zona, _ := time.LoadLocation("Asia/Makassar")
	m := &mockBPJS{bodies: map[string][]string{}, status: "0", noka: os.Getenv("APM_TEST_NOKA"), nik: "7472010101900001"}
	srv := httptest.NewServer(m)
	t.Cleanup(srv.Close)
	cfg := &config.Config{
		KioskID: "UJI", UserSEP: "APM", VClaimURL: srv.URL + "/vclaim", AntreanURL: srv.URL + "/antrean",
		BPJSTimeout: 5 * time.Second, KirimAntrean: true, KirimTask3: true, MenitPerPasien: 5,
		KdPJBPJS: "BPJ", UrutNoReg: "dokter + poli", BaseNoReg: "booking", SEPCatatan: "Pendaftaran APM",
		SEPKontrolTujuan: "2", SEPKontrolAsesmen: "5", FingerprintMode: "semua", MasaBerlakuRujukan: 90, Zona: zona,
	}
	client := bpjs.NewClient(bpjs.Options{VClaimURL: cfg.VClaimURL, AntreanURL: cfg.AntreanURL, ConsID: consID,
		Secret: secret, AntreanConsID: consID, AntreanSecret: secret, Timeout: cfg.BPJSTimeout}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	repo := khanza.New(db)
	if err := repo.CekSkema(context.Background(), "apm_uji"); err != nil {
		t.Fatal(err)
	}
	st, err := repo.Setting(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return New(cfg, repo, client, slog.New(slog.NewTextHandler(os.Stderr, nil)), st), db, m
}

func bersih(db *sql.DB) {
	db.Exec("DELETE FROM rujuk_masuk WHERE no_rawat IN (SELECT no_rawat FROM reg_periksa WHERE no_rkm_medis LIKE '9990%')")
	db.Exec("DELETE FROM rujuk_masuk WHERE no_balasan LIKE 'BR/%'")
	db.Exec("DELETE FROM referensi_mobilejkn_bpjs_taskid")
	db.Exec("DELETE FROM reg_periksa WHERE no_rkm_medis LIKE '9990%'")
	db.Exec("DELETE FROM bridging_sep WHERE nomr LIKE '9990%'")
}

func alur(t *testing.T, s *Service, nomor string) (*Sesi, *Hasil, error) {
	ctx := context.Background()
	ss, err := s.CariPeserta(ctx, nomor)
	if err != nil {
		return nil, nil, err
	}
	list, err := s.ListRujukan(ctx, ss, JenisFKTP)
	if err != nil {
		return ss, nil, err
	}
	if len(list) != 2 || !list[0].Berlaku || list[1].Berlaku {
		t.Fatalf("rujukan: berlaku/kedaluwarsa salah: %+v", list)
	}
	if err := s.PilihRujukan(ss, list[1].NoKunjungan); err == nil {
		t.Fatal("rujukan kedaluwarsa harus ditolak")
	}
	if err := s.PilihRujukan(ss, list[0].NoKunjungan); err != nil {
		return ss, nil, err
	}
	jad, err := s.ListJadwal(ctx, ss)
	if err != nil {
		return ss, nil, err
	}
	var pilih *JadwalItem
	for i := range jad {
		if jad[i].Bisa {
			pilih = &jad[i]
			break
		}
	}
	if pilih == nil {
		return ss, nil, &UserError{Msg: "tidak ada jadwal yang bisa dipilih"}
	}
	if err := s.PilihJadwal(ctx, ss, pilih.KdDokter, pilih.KdPoli, pilih.JamMulai); err != nil {
		return ss, nil, err
	}
	if perlu, _, err := s.PerluBiometrik(ctx, ss); err != nil || perlu {
		t.Fatalf("biometrik: %v %v", perlu, err)
	}
	h, err := s.Daftar(ctx, ss)
	return ss, h, err
}

func TestAlurLengkap(t *testing.T) {
	s, db, m := setup(t)
	tgl := time.Now().In(s.cfg.Zona).Format("2006-01-02")
	bersih(db)

	_, h, err := alur(t, s, m.noka)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("hasil: %+v", *h)
	if !strings.HasPrefix(h.NoRawat, strings.ReplaceAll(tgl, "-", "/")+"/") || len(h.NoRawat) != 17 {
		t.Fatalf("format no_rawat salah: %s", h.NoRawat)
	}
	if len(h.NoReg) != 3 || !h.AntreanTerkirim {
		t.Fatalf("no_reg/antrean: %+v", h)
	}

	// reg_periksa persis nilai Khanza
	var stts, sttsDaftar, lanjut, kdpj, bayar, statusPoli, sttsumur, pj, almt, hub string
	var umur int
	var biaya float64
	err = db.QueryRow(`SELECT stts, stts_daftar, status_lanjut, kd_pj, status_bayar, status_poli, umurdaftar, sttsumur,
		p_jawab, almt_pj, hubunganpj, biaya_reg FROM reg_periksa WHERE no_rawat=?`, h.NoRawat).
		Scan(&stts, &sttsDaftar, &lanjut, &kdpj, &bayar, &statusPoli, &umur, &sttsumur, &pj, &almt, &hub, &biaya)
	if err != nil {
		t.Fatal(err)
	}
	if stts != "Belum" || sttsDaftar != "Lama" || lanjut != "Ralan" || kdpj != "BPJ" || bayar != "Belum Bayar" ||
		statusPoli != "Baru" || sttsumur != "Th" || umur < 30 || pj != "SITI UJI" || hub != "ISTRI" || biaya != 80000 ||
		!strings.HasPrefix(almt, "JL. UJI 1, ") {
		t.Fatalf("reg_periksa salah: %s %s %s %s %s %s %d %s %q %q %s %v", stts, sttsDaftar, lanjut, kdpj, bayar, statusPoli, umur, sttsumur, pj, almt, hub, biaya)
	}
	var umurPasien string
	db.QueryRow("SELECT umur FROM pasien WHERE no_rkm_medis='999001'").Scan(&umurPasien)
	if !strings.Contains(umurPasien, " Th ") {
		t.Fatalf("pasien.umur tidak diupdate: %q", umurPasien)
	}

	// bridging_sep
	var asal, eks, cob, tglpulang, tujuan, kdpoli, kls, peserta, user string
	err = db.QueryRow(`SELECT asal_rujukan, eksekutif, cob, CAST(tglpulang AS CHAR), tujuankunjungan, kdpolitujuan, klsrawat, peserta, user
		FROM bridging_sep WHERE no_sep=? AND no_rawat=?`, h.NoSEP, h.NoRawat).
		Scan(&asal, &eks, &cob, &tglpulang, &tujuan, &kdpoli, &kls, &peserta, &user)
	if err != nil {
		t.Fatal(err)
	}
	if asal != "1. Faskes 1" || eks != "0. Tidak" || cob != "0. Tidak" || tglpulang != "0000-00-00 00:00:00" ||
		tujuan != "0" || kdpoli != "ANA" || kls != "1" || peserta != "PNS DAERAH" || user != "APM" {
		t.Fatalf("bridging_sep salah: %s %s %s %s %s %s %s %s %s", asal, eks, cob, tglpulang, tujuan, kdpoli, kls, peserta, user)
	}
	var noBalasan string
	if err := db.QueryRow("SELECT no_balasan FROM rujuk_masuk WHERE no_rawat=?", h.NoRawat).Scan(&noBalasan); err != nil ||
		!strings.HasPrefix(noBalasan, "BR/"+strings.ReplaceAll(tgl, "-", "/")+"/") {
		t.Fatalf("rujuk_masuk: %q %v", noBalasan, err)
	}
	var task string
	if err := db.QueryRow("SELECT taskid FROM referensi_mobilejkn_bpjs_taskid WHERE no_rawat=?", h.NoRawat).Scan(&task); err != nil || task != "3" {
		t.Fatalf("taskid: %q %v", task, err)
	}

	// isi request antrean/add sama dengan format Khanza
	var add map[string]any
	json.Unmarshal([]byte(m.bodies["/antrean/antrean/add"][0]), &add)
	if add["kodebooking"] != h.NoRawat || add["nomorantrean"] != h.NoReg || add["jenispasien"] != "JKN" ||
		add["kodedokter"].(float64) != 4379 || add["jeniskunjungan"].(float64) != 1 || add["nomorreferensi"] != "0001R0011125B000001" {
		t.Fatalf("antrean/add: %v", add)
	}
	var tsep map[string]any
	json.Unmarshal([]byte(m.bodies["/vclaim/SEP/2.0/insert"][0]), &tsep)
	ts := tsep["request"].(map[string]any)["t_sep"].(map[string]any)
	if ts["noMR"] != "999001" || ts["jnsPelayanan"] != "2" || ts["dpjpLayan"] != "4379" || ts["noTelp"] != "081234567890" {
		t.Fatalf("t_sep: %v", ts)
	}

	// Daftar ulang poli yang sama hari ini → ditolak, tanpa SEP baru
	sepSebelum := m.count("POST /vclaim/SEP/2.0/insert")
	if _, _, err := alur(t, s, m.noka); err == nil {
		t.Fatal("pendaftaran ganda harus ditolak")
	}
	if m.count("POST /vclaim/SEP/2.0/insert") != sepSebelum {
		t.Fatal("SEP tidak boleh dibuat untuk pendaftaran ganda")
	}

	// Batal dari layar selesai
	ss2 := &Sesi{Hasil: h}
	if err := s.Batal(context.Background(), ss2); err != nil {
		t.Fatal(err)
	}
	db.QueryRow("SELECT stts FROM reg_periksa WHERE no_rawat=?", h.NoRawat).Scan(&stts)
	var nSep int
	db.QueryRow("SELECT COUNT(*) FROM bridging_sep WHERE no_sep=?", h.NoSEP).Scan(&nSep)
	db.QueryRow("SELECT taskid FROM referensi_mobilejkn_bpjs_taskid WHERE no_rawat=? AND taskid='99'", h.NoRawat).Scan(&task)
	if stts != "Batal" || nSep != 0 || task != "99" || m.count("DELETE /vclaim/SEP/2.0/delete") != 1 || m.count("POST /antrean/antrean/batal") != 1 {
		t.Fatalf("batal: stts=%s sep=%d task=%s calls=%v", stts, nSep, task, m.calls)
	}
}

func TestRollbackHapusSEP(t *testing.T) {
	s, db, m := setup(t)
	db.Exec("DELETE FROM reg_periksa WHERE no_rkm_medis LIKE '9990%'")
	bersih(db)
	db.Exec("DELETE FROM bridging_sep WHERE no_sep='SEPDOBEL'")
	// SEP dengan nomor yang sudah ada di bridging_sep → insert gagal di tengah transaksi
	if err := khanza.InsertBridgingSEP(context.Background(), db, khanza.BridgingSEP{NoSep: "SEPDOBEL", NoRawat: "x",
		TglSep: "2020-01-01", TglRujukan: "2020-01-01", JnsPelayanan: "2", KlsRawat: "3", LakaLantas: "0", NoMR: "000000",
		TanggalLahir: "2000-01-01", JKel: "L", TglPulang: "0000-00-00 00:00:00", AsalRujukan: "1. Faskes 1",
		Eksekutif: "0. Tidak", Cob: "0. Tidak", Katarak: "0. Tidak", TglKKL: "0000-00-00", Suplesi: "0. Tidak",
		TujuanKunjungan: "0"}); err != nil {
		t.Fatal(err)
	}
	defer db.Exec("DELETE FROM bridging_sep WHERE no_sep='SEPDOBEL'")
	m.sepTetap = "SEPDOBEL"

	_, _, err := alur(t, s, m.noka)
	var ue *UserError
	if !errors.As(err, &ue) {
		t.Fatalf("harus gagal: %v", err)
	}
	var n int
	db.QueryRow("SELECT COUNT(*) FROM reg_periksa WHERE no_rkm_medis='999001' AND tgl_registrasi=CURDATE()").Scan(&n)
	if n != 0 {
		t.Fatal("reg_periksa harus di-rollback")
	}
	if m.count("DELETE /vclaim/SEP/2.0/delete") != 1 {
		t.Fatalf("SEP harus dihapus via VClaim: %v", m.calls)
	}
}

func TestPesertaDitolak(t *testing.T) {
	s, _, m := setup(t)
	m.status = "9"
	if _, err := s.CariPeserta(context.Background(), m.noka); err == nil {
		t.Fatal("peserta tidak aktif harus ditolak")
	}
	m.status, m.noka, m.nik = "0", "0009999999999", "1111111111111111"
	if _, err := s.CariPeserta(context.Background(), m.noka); err == nil || !strings.Contains(err.Error(), "pasien baru") {
		t.Fatalf("pasien baru harus ditolak: %v", err)
	}
	if _, err := s.CariPeserta(context.Background(), "123"); err == nil {
		t.Fatal("panjang nomor salah harus ditolak")
	}
}

// Banyak kiosk mendaftarkan pasien berbeda bersamaan → no_rawat & no_reg tidak bentrok.
func TestKonkuren(t *testing.T) {
	_, db, _ := setup(t)
	bersih(db)
	const n = 6
	for i := 2; i < 2+n; i++ {
		db.Exec(`INSERT IGNORE INTO pasien SELECT ?,nm_pasien,?,jk,tmp_lahir,tgl_lahir,nm_ibu,alamat,gol_darah,pekerjaan,stts_nikah,agama,
			tgl_daftar,no_tlp,umur,pnd,keluarga,namakeluarga,kd_pj,?,kd_kel,kd_kec,kd_kab,pekerjaanpj,alamatpj,kelurahanpj,kecamatanpj,
			kabupatenpj,perusahaan_pasien,suku_bangsa,bahasa_pasien,cacat_fisik,email,nip,kd_prop,propinsipj FROM pasien WHERE no_rkm_medis='999001'`,
			fmt.Sprintf("99900%d", i), fmt.Sprintf("747201010190000%d", i), fmt.Sprintf("000123456789%d", i))
	}
	var wg sync.WaitGroup
	hasil := make(chan *Hasil, n)
	for i := 2; i < 2+n; i++ {
		wg.Add(1)
		go func(noka string) {
			defer wg.Done()
			s2, _, m2 := setup(t)
			m2.noka, m2.nik = noka, "x"
			_, h, err := alur(t, s2, noka)
			if err != nil {
				t.Error(noka, err)
				return
			}
			hasil <- h
		}(fmt.Sprintf("000123456789%d", i))
	}
	wg.Wait()
	close(hasil)
	rawat, reg := map[string]bool{}, map[string]bool{}
	for h := range hasil {
		if rawat[h.NoRawat] || reg[h.NoReg] {
			t.Fatalf("bentrok: %s / %s", h.NoRawat, h.NoReg)
		}
		rawat[h.NoRawat], reg[h.NoReg] = true, true
	}
	if len(rawat) != n {
		t.Fatalf("hanya %d dari %d berhasil", len(rawat), n)
	}
}

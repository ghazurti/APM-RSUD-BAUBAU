// Package service mengorkestrasi alur APM: peserta → rujukan/kontrol → jadwal → SEP → Khanza → antrean.
package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"

	"apm-khanza/internal/bpjs"
	"apm-khanza/internal/config"
	"apm-khanza/internal/khanza"
)

const (
	JenisFKTP    = 1
	JenisKontrol = 3
	JenisRS      = 4
)

// UserError = pesan ramah untuk pasien. Biometrik=true → tampilkan layar verifikasi sidik jari/wajah.
type UserError struct {
	Msg       string
	Biometrik bool
}

func (e *UserError) Error() string { return e.Msg }

func keLoket(format string, a ...any) error {
	return &UserError{Msg: fmt.Sprintf(format, a...)}
}

type Service struct {
	cfg     *config.Config
	repo    *khanza.Repo
	bpjs    *bpjs.Client
	log     *slog.Logger
	Setting *khanza.Setting
}

func New(cfg *config.Config, repo *khanza.Repo, b *bpjs.Client, log *slog.Logger, st *khanza.Setting) *Service {
	return &Service{cfg: cfg, repo: repo, bpjs: b, log: log, Setting: st}
}

func (s *Service) now() time.Time { return time.Now().In(s.cfg.Zona) }
func (s *Service) today() string  { return s.now().Format("2006-01-02") }

func (s *Service) kodePPK() string {
	if s.cfg.KdPPK != "" {
		return s.cfg.KdPPK
	}
	return s.Setting.KodePPK
}

// ===== State per pasien (disimpan di server, bukan di browser) =====

type RujukanItem struct {
	bpjs.Rujukan
	Berlaku       bool
	BerlakuSampai string
}

type Kontrol struct {
	NoSurat      string
	NoSepAsal    string
	TglRencana   string
	KdPoliBPJS   string
	NmPoliBPJS   string
	KdDokterBPJS string
	NmDokterBPJS string
	Asal         *khanza.SEPAsal // data rujukan dari SEP asal (diisi saat dipilih)
}

type JadwalItem struct {
	khanza.JadwalDokter
	Bisa   bool
	Alasan string
}

type Hasil struct {
	NoRawat, NoReg, NoSEP, NoRM, NamaPasien, NoKartu, TglLahir string
	Poli, PoliBPJS, Dokter, Tgl, Jam, Estimasi                 string
	JenisPeserta, Kelas, Diagnosa, FaskesPerujuk, NoRujukan    string
	Catatan, JenisKunjungan, PRB                               string
	NoBooking                                                  string
	IsMJKN                                                     bool
	AntreanTerkirim                                            bool
	Batal                                                      bool
}

type Sesi struct {
	Peserta       *bpjs.Peserta
	Pasien        *khanza.Pasien
	MJKN          *khanza.BookingMJKN
	Jenis         int
	DaftarRujukan []RujukanItem
	Rujukan       *bpjs.Rujukan
	DaftarKontrol []Kontrol
	Kontrol       *Kontrol
	Jadwal        *khanza.JadwalDokter
	FPGagal       int
	Hasil         *Hasil
}

// ===== 6.1 Cari peserta & pasien =====

var hanyaAngka = regexp.MustCompile(`^\d+$`)

func (s *Service) CariPeserta(ctx context.Context, nomor string) (*Sesi, error) {
	nomor = strings.TrimSpace(nomor)
	jenis := ""
	switch {
	case !hanyaAngka.MatchString(nomor):
		return nil, &UserError{Msg: "Nomor hanya boleh berisi angka."}
	case len(nomor) == 13:
		jenis = "nokartu"
	case len(nomor) == 16:
		jenis = "nik"
	default:
		return nil, &UserError{Msg: "Masukkan 13 digit No. Kartu BPJS atau 16 digit NIK."}
	}

	p, err := s.bpjs.GetPeserta(ctx, jenis, nomor, s.today())
	if err != nil {
		return nil, s.bpjsErr(err, "Data peserta BPJS tidak ditemukan")
	}
	if p.StatusPeserta.Kode != "0" {
		return nil, keLoket("Status kepesertaan BPJS Anda: %s. Silakan ke loket pendaftaran.", p.StatusPeserta.Keterangan)
	}
	pas, err := s.repo.CariPasien(ctx, p.NoKartu, p.NIK)
	if err != nil {
		s.log.Error("db_cari_pasien", "err", err.Error())
		return nil, keLoket("Sistem sedang bermasalah. Silakan ke loket pendaftaran.")
	}
	if pas == nil {
		return nil, keLoket("Data Anda belum terdaftar sebagai pasien di rumah sakit ini (pasien baru). Silakan ke loket pendaftaran.")
	}
	if mr := p.NoMR(); mr != "" && mr != pas.NoRM {
		// Jangan ubah apa pun: tetap pakai data Khanza, cukup dicatat.
		s.log.Warn("nomr_beda", "no_kartu", p.NoKartu, "nomr_bpjs", mr, "nomr_khanza", pas.NoRM)
	}
	bk, berr := s.repo.CariBookingMJKN(ctx, p.NoKartu, pas.NoRM, s.today())
	if berr != nil {
		s.log.Warn("db_cari_mjkn", "no_kartu", p.NoKartu, "err", berr.Error())
	}
	return &Sesi{Peserta: p, Pasien: pas, MJKN: bk}, nil
}

// SiapkanMJKN mengisi data rujukan/kontrol dan jadwal sesi langsung dari booking Mobile JKN hari ini.
func (s *Service) SiapkanMJKN(ctx context.Context, sesi *Sesi) error {
	m := sesi.MJKN
	if m == nil {
		return &UserError{Msg: "Data booking Mobile JKN tidak ditemukan."}
	}
	if m.NoSEP != "" {
		return keLoket("Anda sudah Check-in dan SEP %s untuk poli %s sudah terbit hari ini. Silakan langsung menuju poliklinik atau ke loket untuk cetak ulang.", m.NoSEP, m.NmPoliRS)
	}
	jenis := m.JenisKode()
	switch jenis {
	case JenisFKTP, JenisRS:
		asal := 1
		if jenis == JenisRS {
			asal = 2
		}
		var ruj *bpjs.Rujukan
		list, err := s.bpjs.ListRujukan(ctx, asal, sesi.Peserta.NoKartu)
		if err != nil {
			return s.bpjsErr(err, "Gagal mengambil data rujukan Mobile JKN")
		}
		for i := range list {
			if list[i].NoKunjungan == m.NomorReferensi {
				rr := list[i]
				ruj = &rr
				break
			}
		}
		if ruj == nil && m.NomorReferensi != "" {
			if r2, rerr := s.bpjs.GetRujukan(ctx, asal, m.NomorReferensi); rerr == nil && r2 != nil {
				ruj = r2
			}
		}
		if ruj == nil {
			return keLoket("Rujukan %s dari Mobile JKN tidak ditemukan di BPJS. Silakan ke loket pendaftaran.", m.NomorReferensi)
		}
		hariIni, _ := time.ParseInLocation("2006-01-02", s.today(), s.cfg.Zona)
		if t, err := time.ParseInLocation("2006-01-02", ruj.TglKunjungan, s.cfg.Zona); err == nil {
			batas := t.AddDate(0, 0, s.cfg.MasaBerlakuRujukan)
			if hariIni.After(batas) {
				return keLoket("Rujukan %s sudah kedaluwarsa (lebih dari %d hari). Silakan ke loket pendaftaran.", ruj.NoKunjungan, s.cfg.MasaBerlakuRujukan)
			}
		}
		sesi.Jenis, sesi.Rujukan, sesi.Kontrol = jenis, ruj, nil

	case JenisKontrol:
		if m.NomorReferensi == "" {
			return keLoket("Nomor surat kontrol dari Mobile JKN kosong. Silakan ke loket pendaftaran.")
		}
		var k *Kontrol
		if lokal, err := s.repo.SuratKontrolHariIni(ctx, sesi.Peserta.NoKartu, s.today()); err == nil {
			for _, it := range lokal {
				if it.NoSurat == m.NomorReferensi {
					k = &Kontrol{
						NoSurat: it.NoSurat, NoSepAsal: it.NoSep, TglRencana: it.TglRencana,
						KdPoliBPJS: it.KdPoliBPJS, NmPoliBPJS: it.NmPoliBPJS,
						KdDokterBPJS: it.KdDokterBPJS, NmDokterBPJS: it.NmDokterBPJS,
					}
					break
				}
			}
		}
		var asal *khanza.SEPAsal
		if k != nil && k.NoSepAsal != "" {
			asal, _ = s.repo.SEPAsal(ctx, k.NoSepAsal)
		}
		if k == nil || asal == nil {
			sk, err := s.bpjs.GetSuratKontrol(ctx, m.NomorReferensi)
			if err != nil {
				return s.bpjsErr(err, "Gagal mengambil detail surat kontrol Mobile JKN")
			}
			if k == nil {
				k = &Kontrol{
					NoSurat: sk.NoSuratKontrol, NoSepAsal: sk.Sep.NoSep, TglRencana: sk.TglRencanaKontrol,
					KdPoliBPJS: sk.PoliTujuan, NmPoliBPJS: sk.NamaPoliTujuan,
					KdDokterBPJS: sk.KodeDokter, NmDokterBPJS: sk.NamaDokter,
				}
			}
			if asal == nil {
				pp := sk.Sep.ProvPerujuk
				kode, nama := pecahDiagnosa(sk.Sep.Diagnosa)
				asal = &khanza.SEPAsal{
					NoSep: sk.Sep.NoSep, TglRujukan: pp.TglRujukan, NoRujukan: pp.NoRujukan,
					KdPPKRujukan: pp.KdProviderPerujuk, NmPPKRujukan: pp.NmProviderPerujuk,
					AsalRujukan: pp.AsalRujukan, DiagAwal: kode, NmDiagnosaAwal: nama,
				}
			}
		}
		if asal.NoRujukan == "" || asal.DiagAwal == "" {
			return keLoket("Data rujukan untuk surat kontrol %s tidak lengkap. Silakan ke loket pendaftaran.", m.NomorReferensi)
		}
		k.Asal = asal
		sesi.Jenis, sesi.Kontrol, sesi.Rujukan = JenisKontrol, k, nil

	default:
		return keLoket("Jenis kunjungan Mobile JKN (%s) dilayani di loket pendaftaran.", m.JenisKunjungan)
	}

	jamMulai, jamSelesai := m.JamMulaiSelesai()
	sesi.Jadwal = &khanza.JadwalDokter{
		KdDokter:     m.KdDokterRS,
		NmDokter:     m.NmDokterRS,
		KdPoli:       m.KdPoliRS,
		NmPoli:       m.NmPoliRS,
		JamMulai:     jamMulai,
		JamSelesai:   jamSelesai,
		KdPoliBPJS:   m.KdPoliBPJS,
		NmPoliBPJS:   m.NmPoliBPJS,
		KdDokterBPJS: m.KdDokterBPJS,
		NmDokterBPJS: m.NmDokterBPJS,
	}
	return nil
}

// ===== 6.2 Rujukan / kontrol =====

func (s *Service) ListRujukan(ctx context.Context, sesi *Sesi, jenis int) ([]RujukanItem, error) {
	asal := 1
	if jenis == JenisRS {
		asal = 2
	}
	list, err := s.bpjs.ListRujukan(ctx, asal, sesi.Peserta.NoKartu)
	if err != nil {
		return nil, s.bpjsErr(err, "Gagal mengambil data rujukan")
	}
	hariIni, _ := time.ParseInLocation("2006-01-02", s.today(), s.cfg.Zona)
	var out []RujukanItem
	for _, r := range list {
		it := RujukanItem{Rujukan: r}
		if t, err := time.ParseInLocation("2006-01-02", r.TglKunjungan, s.cfg.Zona); err == nil {
			batas := t.AddDate(0, 0, s.cfg.MasaBerlakuRujukan)
			it.Berlaku = !hariIni.After(batas)
			it.BerlakuSampai = batas.Format("02-01-2006")
		}
		out = append(out, it)
	}
	sesi.Jenis, sesi.DaftarRujukan, sesi.Kontrol = jenis, out, nil
	return out, nil
}

func (s *Service) PilihRujukan(sesi *Sesi, noKunjungan string) error {
	for _, r := range sesi.DaftarRujukan {
		if r.NoKunjungan == noKunjungan {
			if !r.Berlaku {
				return keLoket("Rujukan %s sudah kedaluwarsa (lebih dari %d hari). Silakan ke loket pendaftaran.", noKunjungan, s.cfg.MasaBerlakuRujukan)
			}
			rr := r.Rujukan
			sesi.Rujukan = &rr
			return nil
		}
	}
	return &UserError{Msg: "Rujukan tidak ditemukan, silakan pilih ulang."}
}

func (s *Service) ListKontrol(ctx context.Context, sesi *Sesi) ([]Kontrol, error) {
	tgl := s.today()
	var out []Kontrol
	lokal, err := s.repo.SuratKontrolHariIni(ctx, sesi.Peserta.NoKartu, tgl)
	if err != nil {
		s.log.Error("db_surat_kontrol", "err", err.Error())
	}
	for _, k := range lokal {
		out = append(out, Kontrol{NoSurat: k.NoSurat, NoSepAsal: k.NoSep, TglRencana: k.TglRencana,
			KdPoliBPJS: k.KdPoliBPJS, NmPoliBPJS: k.NmPoliBPJS, KdDokterBPJS: k.KdDokterBPJS, NmDokterBPJS: k.NmDokterBPJS})
	}
	if len(out) == 0 {
		n := s.now()
		list, err := s.bpjs.ListRencanaKontrol(ctx, sesi.Peserta.NoKartu, n.Format("01"), n.Format("2006"))
		if err != nil {
			return nil, s.bpjsErr(err, "Gagal mengambil data surat kontrol")
		}
		for _, k := range list {
			if k.JnsKontrol != "2" || k.TglRencanaKontrol != tgl || strings.EqualFold(k.TerbitSEP, "Sudah") {
				continue
			}
			out = append(out, Kontrol{NoSurat: k.NoSuratKontrol, NoSepAsal: k.NoSepAsalKontrol, TglRencana: k.TglRencanaKontrol,
				KdPoliBPJS: k.PoliTujuan, NmPoliBPJS: k.NamaPoliTujuan, KdDokterBPJS: k.KodeDokter, NmDokterBPJS: k.NamaDokter})
		}
	}
	sesi.Jenis, sesi.DaftarKontrol, sesi.Rujukan = JenisKontrol, out, nil
	return out, nil
}

func (s *Service) PilihKontrol(ctx context.Context, sesi *Sesi, noSurat string) error {
	var k *Kontrol
	for i := range sesi.DaftarKontrol {
		if sesi.DaftarKontrol[i].NoSurat == noSurat {
			kk := sesi.DaftarKontrol[i]
			k = &kk
		}
	}
	if k == nil {
		return &UserError{Msg: "Surat kontrol tidak ditemukan, silakan pilih ulang."}
	}
	// Data rujukan diambil dari SEP asal di Khanza; bila tidak ada, dari VClaim.
	asal, err := s.repo.SEPAsal(ctx, k.NoSepAsal)
	if err != nil {
		s.log.Error("db_sep_asal", "err", err.Error())
	}
	if asal == nil {
		sk, err := s.bpjs.GetSuratKontrol(ctx, k.NoSurat)
		if err != nil {
			return s.bpjsErr(err, "Gagal mengambil detail surat kontrol")
		}
		pp := sk.Sep.ProvPerujuk
		kode, nama := pecahDiagnosa(sk.Sep.Diagnosa)
		asal = &khanza.SEPAsal{NoSep: sk.Sep.NoSep, TglRujukan: pp.TglRujukan, NoRujukan: pp.NoRujukan,
			KdPPKRujukan: pp.KdProviderPerujuk, NmPPKRujukan: pp.NmProviderPerujuk, AsalRujukan: pp.AsalRujukan,
			DiagAwal: kode, NmDiagnosaAwal: nama}
	}
	if asal.NoRujukan == "" || asal.DiagAwal == "" {
		return keLoket("Data rujukan untuk surat kontrol %s tidak lengkap. Silakan ke loket pendaftaran.", noSurat)
	}
	k.Asal = asal
	sesi.Kontrol = k
	return nil
}

// "K30 - Dyspepsia" → ("K30", "Dyspepsia")
func pecahDiagnosa(s string) (string, string) {
	if i := strings.Index(s, " - "); i > 0 {
		return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+3:])
	}
	return strings.TrimSpace(s), strings.TrimSpace(s)
}

// ===== 6.3 Poli, dokter, jadwal, kuota =====

func (s *Service) poliTujuanBPJS(sesi *Sesi) string {
	if sesi.Kontrol != nil {
		return sesi.Kontrol.KdPoliBPJS
	}
	if sesi.Rujukan != nil {
		return sesi.Rujukan.PoliRujukan.Kode
	}
	return ""
}

func (s *Service) ListJadwal(ctx context.Context, sesi *Sesi) ([]JadwalItem, error) {
	poli := s.poliTujuanBPJS(sesi)
	if poli == "" {
		return nil, &UserError{Msg: "Silakan pilih rujukan / surat kontrol terlebih dahulu."}
	}
	n := s.now()
	list, err := s.repo.ListJadwal(ctx, khanza.HariKerja(n), s.today(), poli)
	if err != nil {
		s.log.Error("db_jadwal", "err", err.Error())
		return nil, keLoket("Sistem sedang bermasalah. Silakan ke loket pendaftaran.")
	}
	jam := n.Format("15:04:05")
	var out []JadwalItem
	for _, j := range list {
		it := JadwalItem{JadwalDokter: j, Bisa: true}
		switch {
		case j.Kuota <= 0:
			it.Bisa, it.Alasan = false, "Kuota belum diatur"
		case j.Sisa() == 0:
			it.Bisa, it.Alasan = false, "Kuota habis"
		case jam > j.JamSelesai:
			it.Bisa, it.Alasan = false, "Praktik sudah selesai"
		}
		if it.Bisa {
			if ada, err := s.repo.SudahTerdaftar(ctx, sesi.Pasien.NoRM, j.KdPoli, s.today()); err == nil && ada {
				it.Bisa, it.Alasan = false, "Anda sudah terdaftar di poli ini hari ini"
			}
		}
		out = append(out, it)
	}
	return out, nil
}

func (s *Service) PilihJadwal(ctx context.Context, sesi *Sesi, kdDokter, kdPoli, jamMulai string) error {
	j, err := s.repo.GetJadwal(ctx, khanza.HariKerja(s.now()), s.today(), kdDokter, kdPoli, jamMulai)
	if err != nil {
		s.log.Error("db_jadwal", "err", err.Error())
		return keLoket("Sistem sedang bermasalah. Silakan ke loket pendaftaran.")
	}
	if j == nil || j.KdPoliBPJS != s.poliTujuanBPJS(sesi) {
		return &UserError{Msg: "Jadwal tidak ditemukan, silakan pilih ulang."}
	}
	if j.Kuota <= 0 || j.Sisa() == 0 {
		return &UserError{Msg: "Kuota dokter ini sudah habis. Silakan pilih dokter lain atau ke loket."}
	}
	sesi.Jadwal = j
	return nil
}

// ===== 6.5 Verifikasi biometrik =====

// PerluBiometrik: true bila verifikasi sidik jari/wajah diwajibkan dan peserta belum melakukannya.
func (s *Service) PerluBiometrik(ctx context.Context, sesi *Sesi) (bool, string, error) {
	switch s.cfg.FingerprintMode {
	case "off":
		return false, "", nil
	case "poli":
		wajib := false
		for _, p := range s.cfg.FingerprintPoli {
			if sesi.Jadwal != nil && p == strings.ToUpper(sesi.Jadwal.KdPoliBPJS) {
				wajib = true
			}
		}
		if !wajib {
			return false, "", nil
		}
	}
	fp, err := s.bpjs.CekFingerPrint(ctx, sesi.Peserta.NoKartu, s.today())
	if err != nil {
		return false, "", s.bpjsErr(err, "Gagal memeriksa status verifikasi")
	}
	return fp.Kode != "1", fp.Status, nil
}

func isPesanBiometrik(m string) bool {
	m = strings.ToLower(m)
	for _, k := range []string{"sidik jari", "finger", "biometrik", "frista", "wajah", "face"} {
		if strings.Contains(m, k) {
			return true
		}
	}
	return false
}

// ===== 6.6 – 6.8 Proses pendaftaran =====

type dataRujukan struct {
	asalKode, asalEnum, tgl, no, kdPPK, nmPPK, diag, nmDiag string
}

func (s *Service) rujukanUntukSEP(sesi *Sesi) (dataRujukan, error) {
	switch {
	case sesi.Jenis == JenisKontrol && sesi.Kontrol != nil && sesi.Kontrol.Asal != nil:
		a := sesi.Kontrol.Asal
		kode := "1"
		if strings.HasPrefix(a.AsalRujukan, "2") {
			kode = "2"
		}
		return dataRujukan{kode, enumAsal(kode), a.TglRujukan, a.NoRujukan, a.KdPPKRujukan, a.NmPPKRujukan, a.DiagAwal, a.NmDiagnosaAwal}, nil
	case (sesi.Jenis == JenisFKTP || sesi.Jenis == JenisRS) && sesi.Rujukan != nil:
		r := sesi.Rujukan
		kode := "1"
		if sesi.Jenis == JenisRS {
			kode = "2"
		}
		return dataRujukan{kode, enumAsal(kode), r.TglKunjungan, r.NoKunjungan, r.ProvPerujuk.Kode, r.ProvPerujuk.Nama, r.Diagnosa.Kode, r.Diagnosa.Nama}, nil
	}
	return dataRujukan{}, &UserError{Msg: "Data rujukan belum dipilih."}
}

// Nilai enum bridging_sep.asal_rujukan persis seperti combobox BPJSDataSEP.
func enumAsal(kode string) string {
	if kode == "2" {
		return "2. Faskes 2(RS)"
	}
	return "1. Faskes 1"
}

func noTelp(sesi *Sesi) string {
	for _, t := range []string{sesi.Pasien.NoTlp, sesi.Peserta.NoTelepon()} {
		d := strings.Map(func(r rune) rune {
			if r >= '0' && r <= '9' {
				return r
			}
			return -1
		}, t)
		if len(d) >= 8 {
			return d
		}
	}
	return "0"
}

// catatanSEP menggabungkan SEP_CATATAN dengan tanggal batas berlaku rujukan (tglRujukan + MasaBerlakuRujukan)
// dan menjaga panjang <= 100 karakter sesuai kolom bridging_sep.catatan (varchar(100)).
func (s *Service) catatanSEP(tglRujukan string) string {
	base := strings.TrimSpace(s.cfg.SEPCatatan)
	out := base
	if t, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(tglRujukan), s.cfg.Zona); err == nil {
		batas := t.AddDate(0, 0, s.cfg.MasaBerlakuRujukan).Format("02-01-2006")
		if base != "" {
			out = base + " | Batas Rujukan: " + batas
		} else {
			out = "Batas Rujukan: " + batas
		}
	}
	if len(out) > 100 {
		out = out[:100]
	}
	return out
}

func (s *Service) Daftar(ctx context.Context, sesi *Sesi) (*Hasil, error) {
	if sesi.Hasil != nil {
		return sesi.Hasil, nil // cegah double submit
	}
	if sesi.Jadwal == nil {
		return nil, &UserError{Msg: "Silakan pilih poli dan dokter terlebih dahulu."}
	}
	now := s.now()
	tgl, jam := now.Format("2006-01-02"), now.Format("15:04:05")
	pas, pes := sesi.Pasien, sesi.Peserta

	// Validasi ulang (data bisa berubah sejak layar sebelumnya).
	isMJKN := sesi.MJKN != nil
	var j *khanza.JadwalDokter
	var err error
	if isMJKN {
		j = sesi.Jadwal
	} else {
		j, err = s.repo.GetJadwal(ctx, khanza.HariKerja(now), tgl, sesi.Jadwal.KdDokter, sesi.Jadwal.KdPoli, sesi.Jadwal.JamMulai)
		if err != nil || j == nil {
			return nil, keLoket("Jadwal dokter tidak ditemukan. Silakan ke loket pendaftaran.")
		}
		if j.Kuota <= 0 || j.Sisa() == 0 {
			return nil, keLoket("Kuota dokter %s hari ini sudah habis. Silakan ke loket pendaftaran.", j.NmDokter)
		}
		ada, serr := s.repo.SudahTerdaftar(ctx, pas.NoRM, j.KdPoli, tgl)
		if serr != nil {
			s.log.Error("db_validasi", "err", serr.Error())
			return nil, keLoket("Sistem sedang bermasalah. Silakan ke loket pendaftaran.")
		}
		if ada {
			return nil, keLoket("Anda sudah terdaftar di %s hari ini. Silakan ke loket bila ada perubahan.", j.NmPoli)
		}
	}
	if ada, serr := s.repo.SudahAdaSEPPoli(ctx, pes.NoKartu, tgl, j.NmPoliBPJS); serr != nil {
		s.log.Error("db_validasi", "err", serr.Error())
		return nil, keLoket("Sistem sedang bermasalah. Silakan ke loket pendaftaran.")
	} else if ada {
		return nil, keLoket("SEP untuk poli %s hari ini sudah pernah dibuat. Silakan ke loket pendaftaran.", j.NmPoliBPJS)
	}
	if blm, err := s.repo.TagihanBelumClosing(ctx, pas.NoRM); err == nil && blm {
		return nil, keLoket("Kunjungan sebelumnya masih memiliki tagihan yang belum diselesaikan. Silakan ke kasir/loket.")
	}

	rj, err := s.rujukanUntukSEP(sesi)
	if err != nil {
		return nil, err
	}

	// ---- Bangun t_sep (default sama dengan form SEP Khanza) ----
	var t bpjs.TSep
	t.NoKartu, t.TglSep, t.PpkPelayanan, t.JnsPelayanan = pes.NoKartu, tgl, s.kodePPK(), "2"
	t.KlsRawat.KlsRawatHak = pes.HakKelas.Kode
	t.NoMR = pas.NoRM
	t.Rujukan.AsalRujukan, t.Rujukan.TglRujukan, t.Rujukan.NoRujukan, t.Rujukan.PpkRujukan = rj.asalKode, rj.tgl, rj.no, rj.kdPPK
	catatan := s.catatanSEP(rj.tgl)
	t.Catatan, t.DiagAwal = catatan, rj.diag
	t.Poli.Tujuan, t.Poli.Eksekutif = j.KdPoliBPJS, "0"
	t.Cob.Cob, t.Katarak.Katarak = "0", "0"
	t.Jaminan.LakaLantas = "0"
	t.Jaminan.Penjamin.Suplesi.Suplesi = "0"
	t.TujuanKunj = "0"
	kdDPJP, nmDPJP := j.KdDokterBPJS, j.NmDokterBPJS
	noSKDP := ""
	if sesi.Jenis == JenisKontrol {
		t.TujuanKunj, t.AssesmentPel = s.cfg.SEPKontrolTujuan, s.cfg.SEPKontrolAsesmen
		noSKDP = sesi.Kontrol.NoSurat
		kdDPJP, nmDPJP = sesi.Kontrol.KdDokterBPJS, sesi.Kontrol.NmDokterBPJS
	}
	t.Skdp.NoSurat, t.Skdp.KodeDPJP = noSKDP, kdDPJP
	t.DpjpLayan = j.KdDokterBPJS
	t.NoTelp = noTelp(sesi)
	t.User = s.cfg.UserSEP

	noSep, err := s.bpjs.InsertSEP(ctx, t)
	if err != nil {
		var be *bpjs.Error
		var je *bpjs.ErrJaringan
		switch {
		case errors.As(err, &be) && isPesanBiometrik(be.Message):
			return nil, &UserError{Msg: be.Message, Biometrik: true}
		case errors.As(err, &be):
			return nil, keLoket("SEP tidak dapat dibuat: %s. Silakan ke loket pendaftaran.", be.Message)
		case errors.As(err, &je):
			// Status tidak pasti: JANGAN retry otomatis. Cek apakah SEP sudah terbit.
			s.cekSEPTidakPasti(pes.NoKartu, tgl, j.NmPoliBPJS)
			return nil, keLoket("Koneksi ke BPJS terputus saat membuat SEP. Silakan ke loket pendaftaran agar dicek petugas.")
		default:
			s.log.Error("sep_insert", "err", err.Error())
			return nil, keLoket("SEP tidak dapat dibuat. Silakan ke loket pendaftaran.")
		}
	}
	s.log.Info("sep_terbit", "no_sep", noSep, "no_kartu", pes.NoKartu, "kiosk", s.cfg.KioskID)

	// ---- 6.7 Simpan ke Khanza dalam SATU transaksi ----
	var h *Hasil
	if isMJKN {
		h, err = s.simpanKhanzaMJKN(ctx, sesi, j, rj, noSep, noSKDP, kdDPJP, nmDPJP, catatan, tgl, jam)
	} else {
		h, err = s.simpanKhanza(ctx, sesi, j, rj, noSep, noSKDP, kdDPJP, nmDPJP, catatan, tgl, jam)
	}
	if err != nil {
		s.log.Error("simpan_khanza_gagal", "err", err.Error(), "no_sep", noSep)
		dctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if derr := s.bpjs.DeleteSEP(dctx, noSep, s.cfg.UserSEP); derr != nil {
			s.log.Error("sep_rollback_gagal", "no_sep", noSep, "err", derr.Error())
			return nil, keLoket("Pendaftaran gagal disimpan. Sampaikan ke petugas loket: SEP %s perlu dihapus.", noSep)
		}
		s.log.Info("sep_rollback", "no_sep", noSep)
		return nil, keLoket("Pendaftaran gagal disimpan. Silakan ke loket pendaftaran.")
	}
	h.Catatan = t.Catatan
	s.log.Info("registrasi_ok", "no_rawat", h.NoRawat, "no_reg", h.NoReg, "no_sep", noSep, "no_rm", pas.NoRM,
		"kd_poli", j.KdPoli, "kd_dokter", j.KdDokter, "mjkn", isMJKN, "no_booking", h.NoBooking, "kiosk", s.cfg.KioskID)

	// ---- 6.8 Antrean BPJS + Task 3 (gagal tidak membatalkan registrasi) ----
	if s.cfg.KirimAntrean {
		if isMJKN {
			if s.cfg.KirimTask3 {
				tctx, tcancel := context.WithTimeout(context.Background(), 2*s.cfg.BPJSTimeout)
				defer tcancel()
				h.AntreanTerkirim = s.kirimTaskKode(tctx, sesi.MJKN.NoBooking, h.NoRawat, 3, time.Now().In(s.cfg.Zona))
			}
		} else {
			h.AntreanTerkirim = s.kirimAntrean(sesi, j, rj, h, noSKDP, now)
		}
	}
	sesi.Hasil = h
	return h, nil
}

func (s *Service) simpanKhanza(ctx context.Context, sesi *Sesi, j *khanza.JadwalDokter, rj dataRujukan,
	noSep, noSKDP, kdDPJP, nmDPJP, catatan, tgl, jam string) (*Hasil, error) {

	pas, pes := sesi.Pasien, sesi.Peserta
	conn, err := s.repo.DB.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	// Serialisasi antar-kiosk (lock sesi MySQL, bukan objek DB baru). Bentrok dengan loket ditangani retry.
	var got sql.NullInt64
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK('apm_khanza_registrasi', 15)").Scan(&got); err != nil {
		return nil, err
	}
	defer conn.ExecContext(context.Background(), "SELECT RELEASE_LOCK('apm_khanza_registrasi')")

	// READ COMMITTED supaya MAX() saat retry melihat nomor yang baru di-commit loket.
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	sttsDaftar := khanza.SttsDaftar(pas.TglDaftar, tgl)
	biaya, err := khanza.BiayaReg(ctx, tx, j.KdPoli, sttsDaftar)
	if err != nil {
		return nil, err
	}
	statusPoli, err := khanza.StatusPoli(ctx, tx, pas.NoRM, j.KdPoli)
	if err != nil {
		return nil, err
	}
	umur, sttsUmur, err := s.repo.Umur(ctx, tx, pas.NoRM, tgl)
	if err != nil {
		return nil, err
	}

	var noRawat, noReg string
	for coba := 1; ; coba++ {
		if noRawat, err = khanza.GenNoRawat(ctx, tx, tgl); err != nil {
			return nil, err
		}
		if noReg, err = khanza.GenNoReg(ctx, tx, s.cfg.UrutNoReg, s.cfg.BaseNoReg, j.KdDokter, j.KdPoli, tgl); err != nil {
			return nil, err
		}
		err = khanza.InsertRegPeriksa(ctx, tx, khanza.RegPeriksa{
			NoReg: noReg, NoRawat: noRawat, TglRegistrasi: tgl, JamReg: jam, KdDokter: j.KdDokter, NoRM: pas.NoRM,
			KdPoli: j.KdPoli, PJawab: pas.NamaKeluarga, AlmtPJ: pas.Asal, HubunganPJ: pas.Keluarga, BiayaReg: biaya,
			SttsDaftar: sttsDaftar, KdPJ: s.cfg.KdPJBPJS, UmurDaftar: umur, SttsUmur: sttsUmur, StatusPoli: statusPoli,
		})
		if err == nil {
			break
		}
		if !khanza.IsDuplicate(err) || coba >= 5 {
			return nil, fmt.Errorf("insert reg_periksa (coba %d): %w", coba, err)
		}
		s.log.Warn("no_rawat_bentrok", "no_rawat", noRawat, "coba", coba)
		time.Sleep(time.Duration(100*coba) * time.Millisecond)
	}

	if err := khanza.UpdateUmur(ctx, tx, pas.NoRM); err != nil {
		return nil, err
	}

	b := khanza.BridgingSEP{
		NoSep: noSep, NoRawat: noRawat, TglSep: tgl, TglRujukan: rj.tgl, NoRujukan: rj.no, KdPPKRujukan: rj.kdPPK,
		NmPPKRujukan: rj.nmPPK, KdPPKPelayanan: s.kodePPK(), NmPPKPelayanan: s.Setting.NamaInstansi, JnsPelayanan: "2",
		Catatan: catatan, DiagAwal: rj.diag, NmDiagnosaAwal: rj.nmDiag, KdPoliTujuan: j.KdPoliBPJS,
		NmPoliTujuan: j.NmPoliBPJS, KlsRawat: pes.HakKelas.Kode, KlsNaik: "", Pembiayaan: "", PJNaikKelas: "",
		LakaLantas: "0", User: s.cfg.UserSEP, NoMR: pas.NoRM, NamaPasien: pas.Nama, TanggalLahir: pas.TglLahir,
		Peserta: pes.JenisPeserta.Keterangan, JKel: pas.JK, NoKartu: pes.NoKartu, TglPulang: "0000-00-00 00:00:00",
		AsalRujukan: rj.asalEnum, Eksekutif: "0. Tidak", Cob: "0. Tidak", NoTelep: noTelp(sesi), Katarak: "0. Tidak",
		TglKKL: "0000-00-00", Suplesi: "0. Tidak", NoSKDP: noSKDP, KdDPJP: kdDPJP, NmDPJP: nmDPJP,
		TujuanKunjungan: "0", KdDPJPLayanan: j.KdDokterBPJS, NmDPJPLayanan: j.NmDokterBPJS,
	}
	if sesi.Jenis == JenisKontrol {
		b.TujuanKunjungan, b.AsesmenPelayanan = s.cfg.SEPKontrolTujuan, s.cfg.SEPKontrolAsesmen
	}
	if err := khanza.InsertBridgingSEP(ctx, tx, b); err != nil {
		return nil, fmt.Errorf("insert bridging_sep: %w", err)
	}
	if err := khanza.InsertRujukMasuk(ctx, tx, noRawat, tgl, rj.nmPPK, rj.no, rj.diag); err != nil {
		return nil, fmt.Errorf("insert rujuk_masuk: %w", err)
	}
	prb := strings.TrimSpace(pes.PRB())
	if prb != "" && prb != "null" {
		if err := khanza.InsertPRB(ctx, tx, noSep, prb); err != nil {
			return nil, fmt.Errorf("insert bpjs_prb: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	jk := map[int]string{JenisFKTP: "Rujukan FKTP", JenisKontrol: "Kontrol", JenisRS: "Rujukan Antar RS"}[sesi.Jenis]
	return &Hasil{
		NoRawat: noRawat, NoReg: noReg, NoSEP: noSep, NoRM: pas.NoRM, NamaPasien: pas.Nama, NoKartu: pes.NoKartu,
		TglLahir: pas.TglLahir, Poli: j.NmPoli, PoliBPJS: j.NmPoliBPJS, Dokter: j.NmDokter, Tgl: tgl, Jam: jam,
		Estimasi:     estimasi(tgl, j.JamMulai, noReg, s.cfg.MenitPerPasien, s.cfg.Zona).Format("15:04"),
		JenisPeserta: pes.JenisPeserta.Keterangan, Kelas: pes.HakKelas.Keterangan,
		Diagnosa: strings.TrimSpace(rj.diag + " - " + rj.nmDiag), FaskesPerujuk: rj.nmPPK, NoRujukan: rj.no,
		JenisKunjungan: jk, PRB: prb,
	}, nil
}

// simpanKhanzaMJKN: TIDAK membuat reg_periksa baru; memakai no_rawat & no_reg milik Mobile JKN,
// lalu menyimpan bridging_sep (+ rujuk_masuk / bpjs_prb) dan mengubah status MJKN menjadi 'Checkin'.
func (s *Service) simpanKhanzaMJKN(ctx context.Context, sesi *Sesi, j *khanza.JadwalDokter, rj dataRujukan,
	noSep, noSKDP, kdDPJP, nmDPJP, catatan, tgl, jam string) (*Hasil, error) {

	pas, pes, m := sesi.Pasien, sesi.Peserta, sesi.MJKN
	tx, err := s.repo.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	noRawat, noReg := m.NoRawat, m.NoReg

	b := khanza.BridgingSEP{
		NoSep: noSep, NoRawat: noRawat, TglSep: tgl, TglRujukan: rj.tgl, NoRujukan: rj.no, KdPPKRujukan: rj.kdPPK,
		NmPPKRujukan: rj.nmPPK, KdPPKPelayanan: s.kodePPK(), NmPPKPelayanan: s.Setting.NamaInstansi, JnsPelayanan: "2",
		Catatan: catatan, DiagAwal: rj.diag, NmDiagnosaAwal: rj.nmDiag, KdPoliTujuan: j.KdPoliBPJS,
		NmPoliTujuan: j.NmPoliBPJS, KlsRawat: pes.HakKelas.Kode, KlsNaik: "", Pembiayaan: "", PJNaikKelas: "",
		LakaLantas: "0", User: s.cfg.UserSEP, NoMR: pas.NoRM, NamaPasien: pas.Nama, TanggalLahir: pas.TglLahir,
		Peserta: pes.JenisPeserta.Keterangan, JKel: pas.JK, NoKartu: pes.NoKartu, TglPulang: "0000-00-00 00:00:00",
		AsalRujukan: rj.asalEnum, Eksekutif: "0. Tidak", Cob: "0. Tidak", NoTelep: noTelp(sesi), Katarak: "0. Tidak",
		TglKKL: "0000-00-00", Suplesi: "0. Tidak", NoSKDP: noSKDP, KdDPJP: kdDPJP, NmDPJP: nmDPJP,
		TujuanKunjungan: "0", KdDPJPLayanan: j.KdDokterBPJS, NmDPJPLayanan: j.NmDokterBPJS,
	}
	if sesi.Jenis == JenisKontrol {
		b.TujuanKunjungan, b.AsesmenPelayanan = s.cfg.SEPKontrolTujuan, s.cfg.SEPKontrolAsesmen
	}
	if err := khanza.InsertBridgingSEP(ctx, tx, b); err != nil {
		return nil, fmt.Errorf("insert bridging_sep: %w", err)
	}
	if err := khanza.InsertRujukMasuk(ctx, tx, noRawat, tgl, rj.nmPPK, rj.no, rj.diag); err != nil {
		return nil, fmt.Errorf("insert rujuk_masuk: %w", err)
	}
	prb := strings.TrimSpace(pes.PRB())
	if prb != "" && prb != "null" {
		if err := khanza.InsertPRB(ctx, tx, noSep, prb); err != nil {
			return nil, fmt.Errorf("insert bpjs_prb: %w", err)
		}
	}
	if err := khanza.UpdateStatusMJKN(ctx, tx, m.NoBooking, "Checkin", tgl+" "+jam); err != nil {
		return nil, fmt.Errorf("update status mjkn: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	est := estimasi(tgl, j.JamMulai, noReg, s.cfg.MenitPerPasien, s.cfg.Zona).Format("15:04")
	if ms, perr := strconv.ParseInt(strings.TrimSpace(m.EstimasiDilayani), 10, 64); perr == nil && ms > 0 {
		est = time.UnixMilli(ms).In(s.cfg.Zona).Format("15:04")
	}

	jk := map[int]string{JenisFKTP: "Rujukan FKTP", JenisKontrol: "Kontrol", JenisRS: "Rujukan Antar RS"}[sesi.Jenis]
	return &Hasil{
		NoRawat: noRawat, NoReg: noReg, NoSEP: noSep, NoRM: pas.NoRM, NamaPasien: pas.Nama, NoKartu: pes.NoKartu,
		TglLahir: pas.TglLahir, Poli: j.NmPoli, PoliBPJS: j.NmPoliBPJS, Dokter: j.NmDokter, Tgl: tgl, Jam: jam,
		Estimasi: est, JenisPeserta: pes.JenisPeserta.Keterangan, Kelas: pes.HakKelas.Keterangan,
		Diagnosa: strings.TrimSpace(rj.diag + " - " + rj.nmDiag), FaskesPerujuk: rj.nmPPK, NoRujukan: rj.no,
		JenisKunjungan: jk, PRB: prb, NoBooking: m.NoBooking, IsMJKN: true,
	}, nil
}

// estimasi = tgl jam_mulai + no_reg × menit (Khanza: INTERVAL no_reg*5 MINUTE).
func estimasi(tgl, jamMulai, noReg string, menit int, zona *time.Location) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04:05", tgl+" "+jamMulai, zona)
	if err != nil {
		return time.Now().In(zona)
	}
	n, _ := strconv.Atoi(noReg)
	return t.Add(time.Duration(n*menit) * time.Minute)
}

func (s *Service) kirimAntrean(sesi *Sesi, j *khanza.JadwalDokter, rj dataRujukan, h *Hasil, noSKDP string, now time.Time) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*s.cfg.BPJSTimeout)
	defer cancel()
	kodeDokter, _ := strconv.ParseInt(j.KdDokterBPJS, 10, 64)
	if sesi.Jenis == JenisKontrol && sesi.Kontrol != nil {
		if k, err := strconv.ParseInt(sesi.Kontrol.KdDokterBPJS, 10, 64); err == nil {
			kodeDokter = k // Khanza mengirim KdDPJP (dokter surat kontrol)
		}
	}
	angka, _ := strconv.Atoi(h.NoReg)
	nik := sesi.Pasien.NoKTP
	if nik == "" {
		nik = sesi.Peserta.NIK
	}
	a := bpjs.AntreanAdd{
		KodeBooking: h.NoRawat, JenisPasien: "JKN", NomorKartu: sesi.Peserta.NoKartu, NIK: nik, NoHP: noTelp(sesi),
		KodePoli: j.KdPoliBPJS, NamaPoli: j.NmPoliBPJS, PasienBaru: 0, NoRM: sesi.Pasien.NoRM, TanggalPeriksa: h.Tgl,
		KodeDokter: kodeDokter, NamaDokter: j.NmDokterBPJS, JamPraktek: j.JamPraktek(), JenisKunjungan: sesi.Jenis,
		NomorReferensi: rj.no, NomorAntrean: h.NoReg, AngkaAntrean: angka,
		EstimasiDilayani: estimasi(h.Tgl, j.JamMulai, h.NoReg, s.cfg.MenitPerPasien, s.cfg.Zona).UnixMilli(),
		SisaKuotaJKN: j.Kuota - angka, KuotaJKN: j.Kuota, SisaKuotaNonJKN: j.Kuota - angka, KuotaNonJKN: j.Kuota,
		Keterangan: s.cfg.KeteranganAntrol,
	}
	if sesi.Jenis == JenisKontrol {
		a.NamaDokter = sesi.Kontrol.NmDokterBPJS
	}
	err := s.bpjs.AntreanAdd(ctx, a)
	// Sama seperti Khanza: bila ditolak (201) dengan No. Rujukan dan ada SKDP, kirim ulang pakai No. SKDP.
	var be *bpjs.Error
	if errors.As(err, &be) && be.Code == "201" && noSKDP != "" && noSKDP != a.NomorReferensi {
		a.NomorReferensi = noSKDP
		err = s.bpjs.AntreanAdd(ctx, a)
	}
	if err != nil {
		s.log.Error("antrean_add_gagal", "no_rawat", h.NoRawat, "err", err.Error())
		return false
	}
	if s.cfg.KirimTask3 {
		s.kirimTask(ctx, h.NoRawat, 3, time.Now().In(s.cfg.Zona))
	}
	return true
}

func (s *Service) kirimTask(ctx context.Context, noRawat string, task int, waktu time.Time) {
	s.kirimTaskKode(ctx, noRawat, noRawat, task, waktu)
}

// kirimTaskKode mengirim updatewaktu ke BPJS memakai kodeBooking (nobooking untuk MJKN, no_rawat untuk onsite)
// dan mencatat hasilnya di referensi_mobilejkn_bpjs_taskid dengan kunci no_rawat.
func (s *Service) kirimTaskKode(ctx context.Context, kodeBooking, noRawat string, task int, waktu time.Time) bool {
	if err := s.bpjs.UpdateWaktu(ctx, kodeBooking, task, waktu.UnixMilli()); err != nil {
		s.log.Error("taskid_gagal", "kode_booking", kodeBooking, "no_rawat", noRawat, "taskid", task, "err", err.Error())
		return false // tidak dicatat → modul Khanza bisa mengirim ulang
	}
	if err := s.repo.InsertTaskID(ctx, noRawat, strconv.Itoa(task), waktu.Format("2006-01-02 15:04:05")); err != nil {
		s.log.Error("db_taskid", "no_rawat", noRawat, "taskid", task, "err", err.Error())
	}
	return true
}

func (s *Service) cekSEPTidakPasti(noKartu, tgl, nmPoli string) {
	ctx, cancel := context.WithTimeout(context.Background(), s.cfg.BPJSTimeout)
	defer cancel()
	hist, err := s.bpjs.HistoriPelayanan(ctx, noKartu, tgl)
	if err != nil {
		s.log.Error("sep_tidak_pasti", "no_kartu", noKartu, "cek_histori_err", err.Error())
		return
	}
	for _, h := range hist {
		if h.TglSep == tgl && h.JnsPelayanan == "2" {
			s.log.Error("sep_tidak_pasti_terbit", "no_kartu", noKartu, "no_sep", h.NoSep, "poli", h.Poli, "poli_dipilih", nmPoli)
			return
		}
	}
	s.log.Warn("sep_tidak_pasti_tidak_ada", "no_kartu", noKartu)
}

// Batal: pasien membatalkan dari layar selesai → antrean/batal + task 99, hapus SEP, reg_periksa stts='Batal'.
func (s *Service) Batal(ctx context.Context, sesi *Sesi) error {
	h := sesi.Hasil
	if h == nil || h.Batal {
		return nil
	}
	kodeBooking := h.NoRawat
	if h.IsMJKN && h.NoBooking != "" {
		kodeBooking = h.NoBooking
	}
	if h.AntreanTerkirim || h.IsMJKN {
		if err := s.bpjs.AntreanBatal(ctx, kodeBooking, "Dibatalkan pasien di APM"); err != nil {
			s.log.Error("antrean_batal_gagal", "kode_booking", kodeBooking, "no_rawat", h.NoRawat, "err", err.Error())
		} else {
			s.kirimTaskKode(ctx, kodeBooking, h.NoRawat, 99, s.now())
		}
	}
	if err := s.bpjs.DeleteSEP(ctx, h.NoSEP, s.cfg.UserSEP); err != nil {
		s.log.Error("batal_hapus_sep_gagal", "no_sep", h.NoSEP, "err", err.Error())
		return keLoket("Pembatalan belum selesai. Silakan ke loket dan sebutkan No. SEP %s.", h.NoSEP)
	}
	tx, err := s.repo.DB.BeginTx(ctx, nil)
	if err == nil {
		defer tx.Rollback()
		if err = khanza.HapusSEPLokal(ctx, tx, h.NoSEP, h.NoRawat); err == nil {
			if err = khanza.BatalkanRegistrasi(ctx, tx, h.NoRawat); err == nil {
				if h.IsMJKN && h.NoBooking != "" {
					err = khanza.UpdateStatusMJKN(ctx, tx, h.NoBooking, "Batal", s.now().Format("2006-01-02 15:04:05"))
				}
				if err == nil {
					err = tx.Commit()
				}
			}
		}
	}
	if err != nil {
		s.log.Error("batal_db_gagal", "no_rawat", h.NoRawat, "err", err.Error())
		return keLoket("Pembatalan belum selesai. Silakan ke loket dan sebutkan No. Rawat %s.", h.NoRawat)
	}
	h.Batal = true
	s.log.Info("registrasi_batal", "no_rawat", h.NoRawat, "no_sep", h.NoSEP)
	return nil
}

func (s *Service) bpjsErr(err error, judul string) error {
	var be *bpjs.Error
	if errors.As(err, &be) {
		return keLoket("%s: %s. Silakan ke loket pendaftaran.", judul, be.Message)
	}
	var je *bpjs.ErrJaringan
	if errors.As(err, &je) {
		return keLoket("Koneksi ke server BPJS sedang terganggu. Silakan coba lagi atau ke loket pendaftaran.")
	}
	s.log.Error("bpjs_lain", "err", err.Error())
	return keLoket("%s. Silakan ke loket pendaftaran.", judul)
}

// DataCetakSEP mengambil SEP dari bridging_sep (sumber yang sama dengan cetak SEP Khanza).
func (s *Service) DataCetakSEP(ctx context.Context, noSep string) (*khanza.CetakSEP, error) {
	return s.repo.DataCetakSEP(ctx, noSep)
}

func (s *Service) LogoBPJS(ctx context.Context) []byte { return s.repo.LogoBPJS(ctx) }

// Samarkan: "AHMAD SALEH" → "AHM** SAL**"
func Samarkan(nama string) string {
	parts := strings.Fields(nama)
	for i, p := range parts {
		r := []rune(p)
		if len(r) > 3 {
			parts[i] = string(r[:3]) + strings.Repeat("*", len(r)-3)
		}
	}
	return strings.Join(parts, " ")
}

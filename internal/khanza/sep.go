package khanza

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// BridgingSEP = 52 kolom bridging_sep, diisi sama seperti BPJSDataSEP.insertSEP Khanza.
type BridgingSEP struct {
	NoSep, NoRawat, TglSep, TglRujukan, NoRujukan, KdPPKRujukan, NmPPKRujukan string
	KdPPKPelayanan, NmPPKPelayanan, JnsPelayanan, Catatan, DiagAwal, NmDiagnosaAwal string
	KdPoliTujuan, NmPoliTujuan, KlsRawat, KlsNaik, Pembiayaan, PJNaikKelas, LakaLantas string
	User, NoMR, NamaPasien, TanggalLahir, Peserta, JKel, NoKartu, TglPulang string
	AsalRujukan, Eksekutif, Cob, NoTelep, Katarak, TglKKL, KeteranganKKL string
	Suplesi, NoSepSuplesi, KdProp, NmProp, KdKab, NmKab, KdKec, NmKec string
	NoSKDP, KdDPJP, NmDPJP, TujuanKunjungan, FlagProsedur, Penunjang, AsesmenPelayanan string
	KdDPJPLayanan, NmDPJPLayanan string
}

func InsertBridgingSEP(ctx context.Context, q queryer, s BridgingSEP) error {
	_, err := q.ExecContext(ctx, `INSERT INTO bridging_sep
		(no_sep, no_rawat, tglsep, tglrujukan, no_rujukan, kdppkrujukan, nmppkrujukan, kdppkpelayanan, nmppkpelayanan,
		 jnspelayanan, catatan, diagawal, nmdiagnosaawal, kdpolitujuan, nmpolitujuan, klsrawat, klsnaik, pembiayaan,
		 pjnaikkelas, lakalantas, user, nomr, nama_pasien, tanggal_lahir, peserta, jkel, no_kartu, tglpulang,
		 asal_rujukan, eksekutif, cob, notelep, katarak, tglkkl, keterangankkl, suplesi, no_sep_suplesi,
		 kdprop, nmprop, kdkab, nmkab, kdkec, nmkec, noskdp, kddpjp, nmdpdjp, tujuankunjungan, flagprosedur,
		 penunjang, asesmenpelayanan, kddpjplayanan, nmdpjplayanan)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		s.NoSep, s.NoRawat, s.TglSep, s.TglRujukan, s.NoRujukan, s.KdPPKRujukan, s.NmPPKRujukan, s.KdPPKPelayanan,
		s.NmPPKPelayanan, s.JnsPelayanan, s.Catatan, s.DiagAwal, s.NmDiagnosaAwal, s.KdPoliTujuan, s.NmPoliTujuan,
		s.KlsRawat, s.KlsNaik, s.Pembiayaan, s.PJNaikKelas, s.LakaLantas, s.User, s.NoMR, s.NamaPasien, s.TanggalLahir,
		s.Peserta, s.JKel, s.NoKartu, s.TglPulang, s.AsalRujukan, s.Eksekutif, s.Cob, s.NoTelep, s.Katarak, s.TglKKL,
		s.KeteranganKKL, s.Suplesi, s.NoSepSuplesi, s.KdProp, s.NmProp, s.KdKab, s.NmKab, s.KdKec, s.NmKec, s.NoSKDP,
		s.KdDPJP, s.NmDPJP, s.TujuanKunjungan, s.FlagProsedur, s.Penunjang, s.AsesmenPelayanan, s.KdDPJPLayanan,
		s.NmDPJPLayanan)
	return err
}

// InsertRujukMasuk = yang dilakukan BPJSDataSEP setelah SEP tersimpan.
// no_balasan: BR/yyyy/MM/dd/nnnn dari MAX(RIGHT(rujuk_masuk.no_rawat,4)) per tanggal registrasi.
func InsertRujukMasuk(ctx context.Context, q queryer, noRawat, tgl, nmPerujuk, noRujukan, kdPenyakit string) error {
	var n int
	if err := q.QueryRowContext(ctx, `SELECT IFNULL(MAX(CONVERT(RIGHT(rujuk_masuk.no_rawat,4),SIGNED)),0)
		FROM reg_periksa INNER JOIN rujuk_masuk ON reg_periksa.no_rawat=rujuk_masuk.no_rawat
		WHERE reg_periksa.tgl_registrasi=?`, tgl).Scan(&n); err != nil {
		return err
	}
	noBalasan := fmt.Sprintf("BR/%s/%04d", strings.ReplaceAll(tgl, "-", "/"), n+1)
	_, err := q.ExecContext(ctx, `INSERT INTO rujuk_masuk
		(no_rawat, perujuk, alamat, no_rujuk, jm_perujuk, dokter_perujuk, kd_penyakit, kategori_rujuk, keterangan, no_balasan)
		VALUES (?,?,'-',?,0,?,?,'-','-',?)`, noRawat, nmPerujuk, noRujukan, nmPerujuk, kdPenyakit, noBalasan)
	return err
}

func InsertPRB(ctx context.Context, q queryer, noSep, prb string) error {
	_, err := q.ExecContext(ctx, "INSERT INTO bpjs_prb (no_sep, prb) VALUES (?,?)", noSep, prb)
	return err
}

// SudahAdaSEPPoli = cek BPJSDataSEP: sudah ada SEP ralan hari ini di poli yang sama.
func (r *Repo) SudahAdaSEPPoli(ctx context.Context, noKartu, tgl, nmPoliBPJS string) (bool, error) {
	var n int
	err := r.DB.QueryRowContext(ctx,
		"SELECT COUNT(no_kartu) FROM bridging_sep WHERE no_kartu=? AND jnspelayanan='2' AND tglsep=? AND nmpolitujuan=?",
		noKartu, tgl, nmPoliBPJS).Scan(&n)
	return n > 0, err
}

// SEPAsal = data rujukan dari SEP sebelumnya (dipakai untuk SEP kontrol).
type SEPAsal struct {
	NoSep, TglRujukan, NoRujukan, KdPPKRujukan, NmPPKRujukan, AsalRujukan string
	DiagAwal, NmDiagnosaAwal                                              string
}

func (r *Repo) SEPAsal(ctx context.Context, noSep string) (*SEPAsal, error) {
	var s SEPAsal
	err := r.DB.QueryRowContext(ctx, `SELECT no_sep, IFNULL(DATE_FORMAT(tglrujukan,'%Y-%m-%d'),''), no_rujukan, kdppkrujukan,
		nmppkrujukan, asal_rujukan, diagawal, nmdiagnosaawal FROM bridging_sep WHERE no_sep=?`, noSep).
		Scan(&s.NoSep, &s.TglRujukan, &s.NoRujukan, &s.KdPPKRujukan, &s.NmPPKRujukan, &s.AsalRujukan, &s.DiagAwal, &s.NmDiagnosaAwal)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &s, err
}

// SuratKontrol yang tercatat di Khanza (bridging_surat_kontrol_bpjs) untuk tanggal rencana tertentu.
type SuratKontrolKhanza struct {
	NoSurat, NoSep, TglRencana, KdDokterBPJS, NmDokterBPJS, KdPoliBPJS, NmPoliBPJS string
}

func (r *Repo) SuratKontrolHariIni(ctx context.Context, noKartu, tgl string) ([]SuratKontrolKhanza, error) {
	rows, err := r.DB.QueryContext(ctx, `SELECT sk.no_surat, sk.no_sep, DATE_FORMAT(sk.tgl_rencana,'%Y-%m-%d'),
		sk.kd_dokter_bpjs, sk.nm_dokter_bpjs, sk.kd_poli_bpjs, sk.nm_poli_bpjs
		FROM bridging_surat_kontrol_bpjs sk INNER JOIN bridging_sep s ON s.no_sep=sk.no_sep
		WHERE s.no_kartu=? AND sk.tgl_rencana=?
		  AND sk.no_surat NOT IN (SELECT noskdp FROM bridging_sep WHERE no_kartu=? AND noskdp<>'')`, noKartu, tgl, noKartu)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SuratKontrolKhanza
	for rows.Next() {
		var s SuratKontrolKhanza
		if err := rows.Scan(&s.NoSurat, &s.NoSep, &s.TglRencana, &s.KdDokterBPJS, &s.NmDokterBPJS, &s.KdPoliBPJS, &s.NmPoliBPJS); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ===== Pembatalan (pasien membatalkan dari layar selesai) =====

func HapusSEPLokal(ctx context.Context, q queryer, noSep, noRawat string) error {
	if _, err := q.ExecContext(ctx, "DELETE FROM bpjs_prb WHERE no_sep=?", noSep); err != nil {
		return err
	}
	if _, err := q.ExecContext(ctx, "DELETE FROM bridging_sep WHERE no_sep=?", noSep); err != nil {
		return err
	}
	_, err := q.ExecContext(ctx, "DELETE FROM rujuk_masuk WHERE no_rawat=?", noRawat)
	return err
}

// BatalkanRegistrasi: stts='Batal' (nilai enum Khanza), data tetap bisa dilihat petugas di DlgReg.
func BatalkanRegistrasi(ctx context.Context, q queryer, noRawat string) error {
	_, err := q.ExecContext(ctx, "UPDATE reg_periksa SET stts='Batal' WHERE no_rawat=? AND stts='Belum'", noRawat)
	return err
}

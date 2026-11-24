package khanza

import (
	"context"
	"database/sql"
	"errors"
)

type Pasien struct {
	NoRM         string
	Nama         string
	NoKTP        string
	JK           string
	TglLahir     string // yyyy-MM-dd
	TglDaftar    string // yyyy-MM-dd
	NoTlp        string
	NamaKeluarga string
	Keluarga     string
	NoPeserta    string
	// Asal = alamat lengkap persis seperti DlgReg.isCekPasien (dipakai untuk reg_periksa.almt_pj)
	Asal string
}

// CariPasien: by no_peserta atau no_ktp. Prioritaskan yang cocok no_peserta.
func (r *Repo) CariPasien(ctx context.Context, noKartu, nik string) (*Pasien, error) {
	if noKartu == "" {
		noKartu = "-"
	}
	if nik == "" {
		nik = "-"
	}
	var p Pasien
	err := r.DB.QueryRowContext(ctx, `
		SELECT p.no_rkm_medis, p.nm_pasien, IFNULL(p.no_ktp,''), p.jk,
		       IFNULL(DATE_FORMAT(p.tgl_lahir,'%Y-%m-%d'),''), IFNULL(DATE_FORMAT(p.tgl_daftar,'%Y-%m-%d'),''),
		       IFNULL(p.no_tlp,''), IFNULL(p.namakeluarga,''), IFNULL(p.keluarga,''), IFNULL(p.no_peserta,''),
		       CONCAT(p.alamat,', ',IFNULL(kel.nm_kel,''),', ',IFNULL(kec.nm_kec,''),', ',IFNULL(kab.nm_kab,''))
		FROM pasien p
		LEFT JOIN kelurahan kel ON p.kd_kel=kel.kd_kel
		LEFT JOIN kecamatan kec ON p.kd_kec=kec.kd_kec
		LEFT JOIN kabupaten kab ON p.kd_kab=kab.kd_kab
		WHERE p.no_peserta=? OR p.no_ktp=?
		ORDER BY (p.no_peserta=?) DESC, p.tgl_daftar DESC
		LIMIT 1`, noKartu, nik, noKartu).
		Scan(&p.NoRM, &p.Nama, &p.NoKTP, &p.JK, &p.TglLahir, &p.TglDaftar, &p.NoTlp,
			&p.NamaKeluarga, &p.Keluarga, &p.NoPeserta, &p.Asal)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &p, err
}

// Umur dihitung dengan SQL yang sama persis dengan DlgReg.isCekPasien (relatif ke tgl registrasi).
func (r *Repo) Umur(ctx context.Context, q queryer, noRM, tgl string) (umur int, stts string, err error) {
	var th, bl, hr int
	err = q.QueryRowContext(ctx, `
		SELECT TIMESTAMPDIFF(YEAR, tgl_lahir, ?),
		       (TIMESTAMPDIFF(MONTH, tgl_lahir, ?) - ((TIMESTAMPDIFF(MONTH, tgl_lahir, ?) div 12) * 12)),
		       TIMESTAMPDIFF(DAY, DATE_ADD(DATE_ADD(tgl_lahir,INTERVAL TIMESTAMPDIFF(YEAR, tgl_lahir, ?) YEAR),
		           INTERVAL TIMESTAMPDIFF(MONTH, tgl_lahir, ?) - ((TIMESTAMPDIFF(MONTH, tgl_lahir, ?) div 12) * 12) MONTH), ?)
		FROM pasien WHERE no_rkm_medis=?`, tgl, tgl, tgl, tgl, tgl, tgl, tgl, noRM).Scan(&th, &bl, &hr)
	if err != nil {
		return 0, "", err
	}
	switch {
	case th > 0:
		return th, "Th", nil
	case bl > 0:
		return bl, "Bl", nil
	default:
		return hr, "Hr", nil
	}
}

// UpdateUmur = DlgReg.UpdateUmur (dijalankan Khanza setelah registrasi berhasil).
func UpdateUmur(ctx context.Context, q queryer, noRM string) error {
	_, err := q.ExecContext(ctx, `UPDATE pasien SET umur=CONCAT(CONCAT(CONCAT(TIMESTAMPDIFF(YEAR, tgl_lahir, CURDATE()), ' Th '),
		CONCAT(TIMESTAMPDIFF(MONTH, tgl_lahir, CURDATE()) - ((TIMESTAMPDIFF(MONTH, tgl_lahir, CURDATE()) div 12) * 12), ' Bl ')),
		CONCAT(TIMESTAMPDIFF(DAY, DATE_ADD(DATE_ADD(tgl_lahir,INTERVAL TIMESTAMPDIFF(YEAR, tgl_lahir, CURDATE()) YEAR),
		INTERVAL TIMESTAMPDIFF(MONTH, tgl_lahir, CURDATE()) - ((TIMESTAMPDIFF(MONTH, tgl_lahir, CURDATE()) div 12) * 12) MONTH), CURDATE()), ' Hr'))
		WHERE no_rkm_medis=?`, noRM)
	return err
}

// SudahTerdaftar: pasien sudah registrasi hari ini di poli yang sama (stts<>'Batal').
func (r *Repo) SudahTerdaftar(ctx context.Context, noRM, kdPoli, tgl string) (bool, error) {
	var n int
	err := r.DB.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM reg_periksa WHERE no_rkm_medis=? AND kd_poli=? AND tgl_registrasi=? AND stts<>'Batal'",
		noRM, kdPoli, tgl).Scan(&n)
	return n > 0, err
}

// TagihanBelumClosing = cek DlgReg.isPas bila set_validasi_registrasi.wajib_closing_kasir='Yes'.
func (r *Repo) TagihanBelumClosing(ctx context.Context, noRM string) (bool, error) {
	var wajib string
	err := r.DB.QueryRowContext(ctx, "SELECT wajib_closing_kasir FROM set_validasi_registrasi LIMIT 1").Scan(&wajib)
	if errors.Is(err, sql.ErrNoRows) || isNoTable(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if wajib != "Yes" {
		return false, nil
	}
	var n int
	err = r.DB.QueryRowContext(ctx,
		"SELECT COUNT(no_rkm_medis) FROM reg_periksa WHERE no_rkm_medis=? AND status_bayar='Belum Bayar' AND stts<>'Batal'",
		noRM).Scan(&n)
	return n > 0, err
}

package khanza

import (
	"context"
	"fmt"
	"strings"
)

// GenNoRawat = DlgReg.isNumber: yyyy/MM/dd/ + 6 digit dari MAX(RIGHT(no_rawat,6)) per tgl_registrasi.
func GenNoRawat(ctx context.Context, q queryer, tgl string) (string, error) {
	var n int
	err := q.QueryRowContext(ctx,
		"SELECT IFNULL(MAX(CONVERT(RIGHT(reg_periksa.no_rawat,6),SIGNED)),0) FROM reg_periksa WHERE reg_periksa.tgl_registrasi=?",
		tgl).Scan(&n)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s/%06d", strings.ReplaceAll(tgl, "-", "/"), n+1), nil
}

// GenNoReg = DlgReg.isNumber (URUTNOREG + BASENOREG), 3 digit zero-pad.
// Bila BASENOREG=booking, dipakai MAX dari booking_registrasi atau reg_periksa (mana yang lebih besar),
// sama seperti Khanza, supaya nomor tidak bentrok dengan pasien booking.
func GenNoReg(ctx context.Context, q queryer, urut, base, kdDokter, kdPoli, tgl string) (string, error) {
	var whereReg, whereBook string
	var args []any
	switch urut {
	case "poli":
		whereReg, whereBook, args = "kd_poli=?", "kd_poli=?", []any{kdPoli}
	case "dokter + poli":
		whereReg, whereBook, args = "kd_dokter=? AND kd_poli=?", "kd_dokter=? AND kd_poli=?", []any{kdDokter, kdPoli}
	default: // dokter
		whereReg, whereBook, args = "kd_dokter=?", "kd_dokter=?", []any{kdDokter}
	}
	var maxReg int
	if err := q.QueryRowContext(ctx,
		"SELECT IFNULL(MAX(CONVERT(no_reg,SIGNED)),0) FROM reg_periksa WHERE "+whereReg+" AND tgl_registrasi=?",
		append(args, tgl)...).Scan(&maxReg); err != nil {
		return "", err
	}
	if base == "booking" {
		var maxBook int
		if err := q.QueryRowContext(ctx,
			"SELECT IFNULL(MAX(CONVERT(no_reg,SIGNED)),0) FROM booking_registrasi WHERE "+whereBook+" AND tanggal_periksa=?",
			append(args, tgl)...).Scan(&maxBook); err != nil {
			return "", err
		}
		if maxBook >= maxReg {
			maxReg = maxBook
		}
	}
	return fmt.Sprintf("%03d", maxReg+1), nil
}

type RegPeriksa struct {
	NoReg, NoRawat, TglRegistrasi, JamReg     string
	KdDokter, NoRM, KdPoli                    string
	PJawab, AlmtPJ, HubunganPJ                string
	BiayaReg                                  float64
	SttsDaftar, KdPJ                          string
	UmurDaftar                                int
	SttsUmur, StatusPoli                      string
}

// InsertRegPeriksa: nilai tetap sama dengan DlgReg.isRegistrasi (stts='Belum', 'Ralan', 'Belum Bayar').
func InsertRegPeriksa(ctx context.Context, q queryer, r RegPeriksa) error {
	_, err := q.ExecContext(ctx, `INSERT INTO reg_periksa
		(no_reg, no_rawat, tgl_registrasi, jam_reg, kd_dokter, no_rkm_medis, kd_poli, p_jawab, almt_pj, hubunganpj,
		 biaya_reg, stts, stts_daftar, status_lanjut, kd_pj, umurdaftar, sttsumur, status_bayar, status_poli)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,'Belum',?,'Ralan',?,?,?,'Belum Bayar',?)`,
		r.NoReg, r.NoRawat, r.TglRegistrasi, r.JamReg, r.KdDokter, r.NoRM, r.KdPoli, r.PJawab, r.AlmtPJ, r.HubunganPJ,
		r.BiayaReg, r.SttsDaftar, r.KdPJ, r.UmurDaftar, r.SttsUmur, r.StatusPoli)
	return err
}

// StatusPoli: 'Lama' bila pasien pernah registrasi di poli ini (DlgReg.isRegistrasi).
func StatusPoli(ctx context.Context, q queryer, noRM, kdPoli string) (string, error) {
	var n int
	if err := q.QueryRowContext(ctx,
		"SELECT COUNT(no_rkm_medis) FROM reg_periksa WHERE no_rkm_medis=? AND kd_poli=?", noRM, kdPoli).Scan(&n); err != nil {
		return "", err
	}
	if n > 0 {
		return "Lama", nil
	}
	return "Baru", nil
}

// BiayaReg: registrasi (pasien Baru) / registrasilama (Lama), seperti DlgReg.isCekPasien.
func BiayaReg(ctx context.Context, q queryer, kdPoli, sttsDaftar string) (float64, error) {
	kol := "registrasi"
	if sttsDaftar == "Lama" {
		kol = "registrasilama"
	}
	var b float64
	err := q.QueryRowContext(ctx, "SELECT IFNULL("+kol+",0) FROM poliklinik WHERE kd_poli=?", kdPoli).Scan(&b)
	return b, err
}

// SttsDaftar: DlgReg memakai if(pasien.tgl_daftar=tgl_registrasi,'Baru','Lama').
func SttsDaftar(tglDaftar, tglReg string) string {
	if tglDaftar == tglReg {
		return "Baru"
	}
	return "Lama"
}

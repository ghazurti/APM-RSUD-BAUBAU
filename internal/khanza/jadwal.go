package khanza

import (
	"context"
	"database/sql"
	"errors"
)

// JadwalDokter = satu baris jadwal praktik hari ini yang sudah termaping BPJS.
type JadwalDokter struct {
	KdDokter     string
	NmDokter     string
	KdPoli       string
	NmPoli       string
	JamMulai     string // HH:MM:SS
	JamSelesai   string
	Kuota        int
	Terisi       int
	KdPoliBPJS   string
	NmPoliBPJS   string
	KdDokterBPJS string
	NmDokterBPJS string
}

func (j JadwalDokter) Sisa() int {
	if s := j.Kuota - j.Terisi; s > 0 {
		return s
	}
	return 0
}

func (j JadwalDokter) JamPraktek() string { return j.JamMulai[:5] + "-" + j.JamSelesai[:5] }

const sqlJadwal = `
	SELECT j.kd_dokter, d.nm_dokter, j.kd_poli, p.nm_poli,
	       TIME_FORMAT(j.jam_mulai,'%H:%i:%s'), TIME_FORMAT(j.jam_selesai,'%H:%i:%s'), IFNULL(j.kuota,0),
	       mp.kd_poli_bpjs, mp.nm_poli_bpjs, md.kd_dokter_bpjs, md.nm_dokter_bpjs,
	       (SELECT COUNT(*) FROM reg_periksa r WHERE r.kd_dokter=j.kd_dokter AND r.kd_poli=j.kd_poli
	           AND r.tgl_registrasi=? AND r.stts<>'Batal') AS terisi
	FROM jadwal j
	JOIN dokter d ON d.kd_dokter=j.kd_dokter AND d.status='1'
	JOIN poliklinik p ON p.kd_poli=j.kd_poli AND p.status='1'
	JOIN maping_poli_bpjs mp ON mp.kd_poli_rs=j.kd_poli
	JOIN maping_dokter_dpjpvclaim md ON md.kd_dokter=j.kd_dokter
	WHERE j.hari_kerja=?`

// ListJadwal: jadwal hari ini. kdPoliBPJS kosong = semua poli.
func (r *Repo) ListJadwal(ctx context.Context, hari, tgl, kdPoliBPJS string) ([]JadwalDokter, error) {
	q, args := sqlJadwal, []any{tgl, hari}
	if kdPoliBPJS != "" {
		q += " AND mp.kd_poli_bpjs=?"
		args = append(args, kdPoliBPJS)
	}
	q += " ORDER BY p.nm_poli, d.nm_dokter, j.jam_mulai"
	rows, err := r.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []JadwalDokter
	for rows.Next() {
		var j JadwalDokter
		if err := rows.Scan(&j.KdDokter, &j.NmDokter, &j.KdPoli, &j.NmPoli, &j.JamMulai, &j.JamSelesai, &j.Kuota,
			&j.KdPoliBPJS, &j.NmPoliBPJS, &j.KdDokterBPJS, &j.NmDokterBPJS, &j.Terisi); err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// GetJadwal: satu jadwal (validasi ulang saat proses).
func (r *Repo) GetJadwal(ctx context.Context, hari, tgl, kdDokter, kdPoli, jamMulai string) (*JadwalDokter, error) {
	q := sqlJadwal + " AND j.kd_dokter=? AND j.kd_poli=? AND j.jam_mulai=? LIMIT 1"
	var j JadwalDokter
	err := r.DB.QueryRowContext(ctx, q, tgl, hari, kdDokter, kdPoli, jamMulai).Scan(
		&j.KdDokter, &j.NmDokter, &j.KdPoli, &j.NmPoli, &j.JamMulai, &j.JamSelesai, &j.Kuota,
		&j.KdPoliBPJS, &j.NmPoliBPJS, &j.KdDokterBPJS, &j.NmDokterBPJS, &j.Terisi)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &j, err
}

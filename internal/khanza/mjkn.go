package khanza

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// BookingMJKN = data antrean Mobile JKN hari ini yang sudah otomatis masuk reg_periksa.
type BookingMJKN struct {
	NoBooking        string
	NoRawat          string
	NoReg            string
	JamReg           string
	NoKartu          string
	NoRM             string
	TglPeriksa       string
	KdPoliBPJS       string
	NmPoliBPJS       string
	KdPoliRS         string
	NmPoliRS         string
	KdDokterBPJS     string
	NmDokterBPJS     string
	KdDokterRS       string
	NmDokterRS       string
	JamPraktek       string
	JenisKunjungan   string
	NomorReferensi   string
	NomorAntrean     string
	EstimasiDilayani string
	Status           string
	NoSEP            string
}

// JenisKode mengubah enum referensi_mobilejkn_bpjs.jeniskunjungan ('1 (Rujukan FKTP)', dll.) menjadi 1/2/3/4.
func (b BookingMJKN) JenisKode() int {
	s := strings.TrimSpace(b.JenisKunjungan)
	switch {
	case strings.HasPrefix(s, "1"):
		return 1
	case strings.HasPrefix(s, "2"):
		return 2
	case strings.HasPrefix(s, "3"):
		return 3
	case strings.HasPrefix(s, "4"):
		return 4
	default:
		return 0
	}
}

// JamMulaiSelesai memecah jampraktek ("08:00-14:00") menjadi ("08:00:00", "14:00:00").
func (b BookingMJKN) JamMulaiSelesai() (string, string) {
	parts := strings.Split(strings.TrimSpace(b.JamPraktek), "-")
	if len(parts) == 2 {
		mulai := strings.TrimSpace(parts[0])
		selesai := strings.TrimSpace(parts[1])
		if len(mulai) == 5 {
			mulai += ":00"
		}
		if len(selesai) == 5 {
			selesai += ":00"
		}
		if len(mulai) >= 5 && len(selesai) >= 5 {
			return mulai, selesai
		}
	}
	return "08:00:00", "14:00:00"
}

// CariBookingMJKN mencari booking Mobile JKN aktif hari ini yang sudah punya baris reg_periksa (stts<>'Batal').
func (r *Repo) CariBookingMJKN(ctx context.Context, noKartu, noRM, tgl string) (*BookingMJKN, error) {
	var b BookingMJKN
	err := r.DB.QueryRowContext(ctx, `
		SELECT m.nobooking, m.no_rawat, r.no_reg, IFNULL(TIME_FORMAT(r.jam_reg,'%H:%i:%s'),''),
		       IFNULL(m.nomorkartu,''), IFNULL(m.norm,''), DATE_FORMAT(m.tanggalperiksa,'%Y-%m-%d'),
		       IFNULL(m.kodepoli,''), IFNULL(mp.nm_poli_bpjs, p.nm_poli), r.kd_poli, p.nm_poli,
		       IFNULL(m.kodedokter,''), IFNULL(md.nm_dokter_bpjs, d.nm_dokter), r.kd_dokter, d.nm_dokter,
		       IFNULL(m.jampraktek,''), IFNULL(m.jeniskunjungan,''), IFNULL(m.nomorreferensi,''),
		       IFNULL(m.nomorantrean,''), IFNULL(m.estimasidilayani,''), m.status,
		       IFNULL((SELECT bs.no_sep FROM bridging_sep bs WHERE bs.no_rawat=m.no_rawat AND bs.jnspelayanan='2' LIMIT 1),'')
		FROM referensi_mobilejkn_bpjs m
		INNER JOIN reg_periksa r ON r.no_rawat=m.no_rawat AND r.stts<>'Batal'
		INNER JOIN poliklinik p ON p.kd_poli=r.kd_poli
		INNER JOIN dokter d ON d.kd_dokter=r.kd_dokter
		LEFT JOIN maping_poli_bpjs mp ON mp.kd_poli_rs=r.kd_poli
		LEFT JOIN maping_dokter_dpjpvclaim md ON md.kd_dokter=r.kd_dokter
		WHERE m.tanggalperiksa=?
		  AND (m.nomorkartu=? OR m.norm=?)
		  AND m.status IN ('Belum','Checkin')
		  AND m.no_rawat<>''
		ORDER BY (m.status='Belum') DESC, m.validasi DESC
		LIMIT 1`, tgl, noKartu, noRM).Scan(
		&b.NoBooking, &b.NoRawat, &b.NoReg, &b.JamReg,
		&b.NoKartu, &b.NoRM, &b.TglPeriksa,
		&b.KdPoliBPJS, &b.NmPoliBPJS, &b.KdPoliRS, &b.NmPoliRS,
		&b.KdDokterBPJS, &b.NmDokterBPJS, &b.KdDokterRS, &b.NmDokterRS,
		&b.JamPraktek, &b.JenisKunjungan, &b.NomorReferensi,
		&b.NomorAntrean, &b.EstimasiDilayani, &b.Status, &b.NoSEP,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &b, err
}

// UpdateStatusMJKN memperbarui status ('Checkin' / 'Batal') dan waktu validasi di referensi_mobilejkn_bpjs.
func UpdateStatusMJKN(ctx context.Context, q queryer, noBooking, status, waktu string) error {
	_, err := q.ExecContext(ctx,
		"UPDATE referensi_mobilejkn_bpjs SET status=?, validasi=? WHERE nobooking=?",
		status, waktu, noBooking)
	return err
}

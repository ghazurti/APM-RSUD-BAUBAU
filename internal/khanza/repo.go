// Package khanza membaca/menulis DB SIMRS Khanza (sik) apa adanya:
// tidak ada tabel/kolom/trigger/view baru, dan nilai enum sama persis dengan Khanza.
package khanza

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
)

type Repo struct {
	DB *sql.DB
}

func New(db *sql.DB) *Repo { return &Repo{DB: db} }

// queryer dipenuhi *sql.DB, *sql.Tx dan *sql.Conn.
type queryer interface {
	ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row
}

func IsDuplicate(err error) bool {
	var me *mysql.MySQLError
	return errors.As(err, &me) && me.Number == 1062
}

func isNoTable(err error) bool {
	var me *mysql.MySQLError
	return errors.As(err, &me) && me.Number == 1146
}

// HariKerja = nilai enum jadwal.hari_kerja Khanza.
func HariKerja(t time.Time) string {
	return [...]string{"AKHAD", "SENIN", "SELASA", "RABU", "KAMIS", "JUMAT", "SABTU"}[t.Weekday()]
}

// ===== Cek skema (wajib sebelum query) =====

var kolomWajib = map[string][]string{
	"pasien": {"no_rkm_medis", "nm_pasien", "no_ktp", "jk", "tgl_lahir", "alamat", "tgl_daftar", "no_tlp", "umur",
		"keluarga", "namakeluarga", "no_peserta", "kd_kel", "kd_kec", "kd_kab"},
	"reg_periksa": {"no_reg", "no_rawat", "tgl_registrasi", "jam_reg", "kd_dokter", "no_rkm_medis", "kd_poli", "p_jawab",
		"almt_pj", "hubunganpj", "biaya_reg", "stts", "stts_daftar", "status_lanjut", "kd_pj", "umurdaftar", "sttsumur",
		"status_bayar", "status_poli"},
	"jadwal":                   {"kd_dokter", "hari_kerja", "jam_mulai", "jam_selesai", "kd_poli", "kuota"},
	"poliklinik":               {"kd_poli", "nm_poli", "registrasi", "registrasilama", "status"},
	"dokter":                   {"kd_dokter", "nm_dokter", "status"},
	"maping_poli_bpjs":         {"kd_poli_rs", "kd_poli_bpjs", "nm_poli_bpjs"},
	"maping_dokter_dpjpvclaim": {"kd_dokter", "kd_dokter_bpjs", "nm_dokter_bpjs"},
	"bridging_sep": {"no_sep", "no_rawat", "tglsep", "tglrujukan", "no_rujukan", "kdppkrujukan", "nmppkrujukan",
		"kdppkpelayanan", "nmppkpelayanan", "jnspelayanan", "catatan", "diagawal", "nmdiagnosaawal", "kdpolitujuan",
		"nmpolitujuan", "klsrawat", "klsnaik", "pembiayaan", "pjnaikkelas", "lakalantas", "user", "nomr", "nama_pasien",
		"tanggal_lahir", "peserta", "jkel", "no_kartu", "tglpulang", "asal_rujukan", "eksekutif", "cob", "notelep",
		"katarak", "tglkkl", "keterangankkl", "suplesi", "no_sep_suplesi", "kdprop", "nmprop", "kdkab", "nmkab", "kdkec",
		"nmkec", "noskdp", "kddpjp", "nmdpdjp", "tujuankunjungan", "flagprosedur", "penunjang", "asesmenpelayanan",
		"kddpjplayanan", "nmdpjplayanan"},
	"rujuk_masuk": {"no_rawat", "perujuk", "alamat", "no_rujuk", "jm_perujuk", "dokter_perujuk", "kd_penyakit",
		"kategori_rujuk", "keterangan", "no_balasan"},
	"bpjs_prb":                        {"no_sep", "prb"},
	"bridging_surat_kontrol_bpjs":     {"no_sep", "no_surat", "tgl_rencana", "kd_dokter_bpjs", "nm_dokter_bpjs", "kd_poli_bpjs", "nm_poli_bpjs"},
	"referensi_mobilejkn_bpjs":        {"nobooking", "no_rawat", "nomorkartu", "kodepoli", "norm", "tanggalperiksa", "kodedokter", "jampraktek", "jeniskunjungan", "nomorreferensi", "nomorantrean", "estimasidilayani", "status", "validasi"},
	"referensi_mobilejkn_bpjs_taskid": {"no_rawat", "taskid", "waktu"},
	"setting":                         {"nama_instansi", "alamat_instansi", "kabupaten", "kode_ppk"},
}

// CekSkema = langkah "DESCRIBE dulu": gagal start bila ada kolom yang dipakai APM tidak ada di DB.
func (r *Repo) CekSkema(ctx context.Context, dbName string) error {
	rows, err := r.DB.QueryContext(ctx,
		"SELECT table_name, column_name FROM information_schema.columns WHERE table_schema=?", dbName)
	if err != nil {
		return err
	}
	defer rows.Close()
	ada := map[string]bool{}
	for rows.Next() {
		var t, c string
		if err := rows.Scan(&t, &c); err != nil {
			return err
		}
		ada[strings.ToLower(t)+"."+strings.ToLower(c)] = true
	}
	var kurang []string
	for t, cols := range kolomWajib {
		for _, c := range cols {
			if !ada[t+"."+c] {
				kurang = append(kurang, t+"."+c)
			}
		}
	}
	if len(kurang) > 0 {
		return fmt.Errorf("kolom tidak ditemukan di DB Khanza: %s", strings.Join(kurang, ", "))
	}
	return rows.Err()
}

// ===== Setting RS =====

type Setting struct {
	NamaInstansi string
	Alamat       string
	Kabupaten    string
	KodePPK      string
}

func (r *Repo) Setting(ctx context.Context) (*Setting, error) {
	var s Setting
	err := r.DB.QueryRowContext(ctx,
		"SELECT nama_instansi, IFNULL(alamat_instansi,''), IFNULL(kabupaten,''), IFNULL(kode_ppk,'') FROM setting LIMIT 1").
		Scan(&s.NamaInstansi, &s.Alamat, &s.Kabupaten, &s.KodePPK)
	return &s, err
}

package khanza

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// CetakSEP = data cetak SEP rawat jalan, query & transformasi sama dengan report/rptBridgingSEP2.jrxml Khanza.
type CetakSEP struct {
	NoSep, TglSep, NoKartu, NoMR, NamaPasien, TglLahir, NoTelep, Peserta string
	JnsRawat, JnsKunjungan, Flag, NmPoliTujuan, NmDPJP, KlsHak, KlsRawat string
	NmPPKRujukan, NmDiagnosaAwal, Penjamin, Catatan, PRB                 string
}

func (r *Repo) DataCetakSEP(ctx context.Context, noSep string) (*CetakSEP, error) {
	var c CetakSEP
	var tglLahir, tujuan, flag, klsnaik, laka string
	err := r.DB.QueryRowContext(ctx, `SELECT bridging_sep.no_sep, DATE_FORMAT(bridging_sep.tglsep,'%Y-%m-%d'), bridging_sep.no_kartu,
		bridging_sep.nomr, bridging_sep.nama_pasien, IFNULL(DATE_FORMAT(bridging_sep.tanggal_lahir,'%Y-%m-%d'),''),
		IFNULL(bridging_sep.notelep,''), IFNULL(bridging_sep.peserta,''),
		IF(bridging_sep.jnspelayanan='1','Rawat Inap','Rawat Jalan'),
		IFNULL(bridging_sep.tujuankunjungan,''), IFNULL(bridging_sep.flagprosedur,''), IFNULL(bridging_sep.nmpolitujuan,''),
		IFNULL(bridging_sep.nmdpdjp,''),
		IF(bridging_sep.klsrawat='1','Kelas 1',IF(bridging_sep.klsrawat='2','Kelas 2','Kelas 3')),
		IFNULL(bridging_sep.klsnaik,''), IFNULL(bridging_sep.nmppkrujukan,''), IFNULL(bridging_sep.nmdiagnosaawal,''),
		IFNULL(bridging_sep.lakalantas,''), IFNULL(bridging_sep.catatan,''),
		IFNULL((SELECT bpjs_prb.prb FROM bpjs_prb WHERE bpjs_prb.no_sep=bridging_sep.no_sep LIMIT 1),'')
		FROM bridging_sep WHERE no_sep=?`, noSep).Scan(
		&c.NoSep, &c.TglSep, &c.NoKartu, &c.NoMR, &c.NamaPasien, &tglLahir, &c.NoTelep, &c.Peserta, &c.JnsRawat,
		&tujuan, &flag, &c.NmPoliTujuan, &c.NmDPJP, &c.KlsHak, &klsnaik, &c.NmPPKRujukan, &c.NmDiagnosaAwal, &laka,
		&c.Catatan, &c.PRB)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	// Pola tanggal lahir di report: dd/MM/yyyy
	if t, e := time.Parse("2006-01-02", tglLahir); e == nil {
		c.TglLahir = t.Format("02/01/2006")
	}
	// Ekspresi field report Khanza
	if tujuan == "0" {
		c.JnsKunjungan = "- Konsultasi dokter(pertama)"
	} else {
		c.JnsKunjungan = "- Kunjungan Kontrol(ulangan)"
	}
	c.Flag = strings.NewReplacer("0", "- Prosedur Tidak Berkelanjutan", "1", "- Prosedur dan Terapi Berkelanjutan").Replace(flag)
	c.KlsRawat = strings.NewReplacer("1", "VVIP", "2", "VIP", "3", "Kelas I", "4", "Kelas II", "5", "Kelas III", "6", "ICCU").Replace(klsnaik)
	c.Penjamin = strings.NewReplacer("0", "BPJS Kesehatan", "1", "Jasa Raharja",
		"2", "Jasa Raharja & BPJS Ketenagakerjaan/Taspen", "3", "BPJS Ketenagakerjaan, Taspen, dll").Replace(laka)
	return &c, nil
}

// LogoBPJS = gambar.bpjs (logo yang dipakai report SEP Khanza). Tabel tidak ada → nil.
func (r *Repo) LogoBPJS(ctx context.Context) []byte {
	var b []byte
	if err := r.DB.QueryRowContext(ctx, "SELECT bpjs FROM gambar LIMIT 1").Scan(&b); err != nil {
		return nil
	}
	return b
}

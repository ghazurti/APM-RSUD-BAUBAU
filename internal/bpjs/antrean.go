package bpjs

import (
	"context"
	"net/http"
	"strconv"
)

// AntreanAdd mengikuti JSON yang dikirim Khanza (BPJSDataSEP.SimpanAntrianOnSite).
type AntreanAdd struct {
	KodeBooking      string `json:"kodebooking"`
	JenisPasien      string `json:"jenispasien"`
	NomorKartu       string `json:"nomorkartu"`
	NIK              string `json:"nik"`
	NoHP             string `json:"nohp"`
	KodePoli         string `json:"kodepoli"`
	NamaPoli         string `json:"namapoli"`
	PasienBaru       int    `json:"pasienbaru"`
	NoRM             string `json:"norm"`
	TanggalPeriksa   string `json:"tanggalperiksa"`
	KodeDokter       int64  `json:"kodedokter"`
	NamaDokter       string `json:"namadokter"`
	JamPraktek       string `json:"jampraktek"`
	JenisKunjungan   int    `json:"jeniskunjungan"`
	NomorReferensi   string `json:"nomorreferensi"`
	NomorAntrean     string `json:"nomorantrean"`
	AngkaAntrean     int    `json:"angkaantrean"`
	EstimasiDilayani int64  `json:"estimasidilayani"`
	SisaKuotaJKN     int    `json:"sisakuotajkn"`
	KuotaJKN         int    `json:"kuotajkn"`
	SisaKuotaNonJKN  int    `json:"sisakuotanonjkn"`
	KuotaNonJKN      int    `json:"kuotanonjkn"`
	Keterangan       string `json:"keterangan"`
}

func (c *Client) AntreanAdd(ctx context.Context, a AntreanAdd) error {
	_, err := c.call(ctx, c.antrean, http.MethodPost, "antrean/add", a)
	return err
}

// UpdateWaktu mengirim task id. waktu = epoch milidetik. Format sama dengan Khanza (string).
func (c *Client) UpdateWaktu(ctx context.Context, kodeBooking string, taskID int, waktuMs int64) error {
	body := map[string]string{
		"kodebooking": kodeBooking,
		"taskid":      strconv.Itoa(taskID),
		"waktu":       strconv.FormatInt(waktuMs, 10),
	}
	_, err := c.call(ctx, c.antrean, http.MethodPost, "antrean/updatewaktu", body)
	return err
}

func (c *Client) AntreanBatal(ctx context.Context, kodeBooking, keterangan string) error {
	body := map[string]string{"kodebooking": kodeBooking, "keterangan": keterangan}
	_, err := c.call(ctx, c.antrean, http.MethodPost, "antrean/batal", body)
	return err
}

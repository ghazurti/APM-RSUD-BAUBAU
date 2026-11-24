package bpjs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

type KodeNama struct {
	Kode string `json:"kode"`
	Nama string `json:"nama"`
}

type KodeKet struct {
	Kode       string `json:"kode"`
	Keterangan string `json:"keterangan"`
}

type Peserta struct {
	NoKartu  string `json:"noKartu"`
	NIK      string `json:"nik"`
	Nama     string `json:"nama"`
	Sex      string `json:"sex"`
	TglLahir string `json:"tglLahir"`
	MR       struct {
		NoMR      *string `json:"noMR"`
		NoTelepon *string `json:"noTelepon"`
	} `json:"mr"`
	StatusPeserta KodeKet `json:"statusPeserta"`
	ProvUmum      struct {
		KdProvider string `json:"kdProvider"`
		NmProvider string `json:"nmProvider"`
	} `json:"provUmum"`
	JenisPeserta KodeKet `json:"jenisPeserta"`
	HakKelas     KodeKet `json:"hakKelas"`
	Informasi    struct {
		ProlanisPRB *string `json:"prolanisPRB"`
	} `json:"informasi"`
}

func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func (p *Peserta) NoMR() string      { return str(p.MR.NoMR) }
func (p *Peserta) NoTelepon() string { return str(p.MR.NoTelepon) }
func (p *Peserta) PRB() string       { return str(p.Informasi.ProlanisPRB) }

// GetPeserta: jenis "nokartu" (13 digit) atau "nik" (16 digit).
func (c *Client) GetPeserta(ctx context.Context, jenis, nomor, tgl string) (*Peserta, error) {
	raw, err := c.call(ctx, c.vclaim, http.MethodGet,
		fmt.Sprintf("Peserta/%s/%s/tglSEP/%s", jenis, url.PathEscape(nomor), tgl), nil)
	if err != nil {
		return nil, err
	}
	var r struct {
		Peserta Peserta `json:"peserta"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("parse peserta: %w", err)
	}
	return &r.Peserta, nil
}

type Rujukan struct {
	NoKunjungan  string   `json:"noKunjungan"`
	TglKunjungan string   `json:"tglKunjungan"`
	Keluhan      string   `json:"keluhan"`
	ProvPerujuk  KodeNama `json:"provPerujuk"`
	PoliRujukan  KodeNama `json:"poliRujukan"`
	Diagnosa     KodeNama `json:"diagnosa"`
	Pelayanan    KodeNama `json:"pelayanan"`
}

// ListRujukan: asal 1 = FKTP, 2 = RS. Data tidak ditemukan → slice kosong.
func (c *Client) ListRujukan(ctx context.Context, asal int, noka string) ([]Rujukan, error) {
	path := "Rujukan/List/Peserta/" + url.PathEscape(noka)
	if asal == 2 {
		path = "Rujukan/RS/List/Peserta/" + url.PathEscape(noka)
	}
	raw, err := c.call(ctx, c.vclaim, http.MethodGet, path, nil)
	if err != nil {
		if IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	var r struct {
		Rujukan []Rujukan `json:"rujukan"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("parse rujukan: %w", err)
	}
	return r.Rujukan, nil
}

// GetRujukan mengambil detail satu rujukan berdasarkan nomor (asal 1 = FKTP, 2 = RS).
func (c *Client) GetRujukan(ctx context.Context, asal int, noRujukan string) (*Rujukan, error) {
	path := "Rujukan/" + url.PathEscape(noRujukan)
	if asal == 2 {
		path = "Rujukan/RS/" + url.PathEscape(noRujukan)
	}
	raw, err := c.call(ctx, c.vclaim, http.MethodGet, path, nil)
	if err != nil {
		if IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	var r struct {
		Rujukan Rujukan `json:"rujukan"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("parse rujukan: %w", err)
	}
	return &r.Rujukan, nil
}

type RencanaKontrolItem struct {
	NoSuratKontrol    string `json:"noSuratKontrol"`
	JnsKontrol        string `json:"jnsKontrol"` // 1 = SPRI, 2 = Surat Kontrol
	TglRencanaKontrol string `json:"tglRencanaKontrol"`
	NoSepAsalKontrol  string `json:"noSepAsalKontrol"`
	PoliTujuan        string `json:"poliTujuan"`
	NamaPoliTujuan    string `json:"namaPoliTujuan"`
	KodeDokter        string `json:"kodeDokter"`
	NamaDokter        string `json:"namaDokter"`
	NoKartu           string `json:"noKartu"`
	TerbitSEP         string `json:"terbitSEP"`
}

// ListRencanaKontrol: filter 2 = berdasarkan tanggal rencana kontrol.
func (c *Client) ListRencanaKontrol(ctx context.Context, noka, bulan, tahun string) ([]RencanaKontrolItem, error) {
	raw, err := c.call(ctx, c.vclaim, http.MethodGet,
		fmt.Sprintf("RencanaKontrol/ListRencanaKontrol/Bulan/%s/Tahun/%s/Nokartu/%s/filter/2", bulan, tahun, url.PathEscape(noka)), nil)
	if err != nil {
		if IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	var r struct {
		List []RencanaKontrolItem `json:"list"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("parse rencana kontrol: %w", err)
	}
	return r.List, nil
}

type SuratKontrol struct {
	NoSuratKontrol    string `json:"noSuratKontrol"`
	TglRencanaKontrol string `json:"tglRencanaKontrol"`
	JnsKontrol        string `json:"jnsKontrol"`
	PoliTujuan        string `json:"poliTujuan"`
	NamaPoliTujuan    string `json:"namaPoliTujuan"`
	KodeDokter        string `json:"kodeDokter"`
	NamaDokter        string `json:"namaDokter"`
	Sep               struct {
		NoSep       string `json:"noSep"`
		TglSep      string `json:"tglSep"`
		Diagnosa    string `json:"diagnosa"`
		ProvPerujuk struct {
			KdProviderPerujuk string `json:"kdProviderPerujuk"`
			NmProviderPerujuk string `json:"nmProviderPerujuk"`
			AsalRujukan       string `json:"asalRujukan"`
			NoRujukan         string `json:"noRujukan"`
			TglRujukan        string `json:"tglRujukan"`
		} `json:"provPerujuk"`
	} `json:"sep"`
}

func (c *Client) GetSuratKontrol(ctx context.Context, noSurat string) (*SuratKontrol, error) {
	raw, err := c.call(ctx, c.vclaim, http.MethodGet, "RencanaKontrol/noSuratKontrol/"+url.PathEscape(noSurat), nil)
	if err != nil {
		return nil, err
	}
	var r SuratKontrol
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("parse surat kontrol: %w", err)
	}
	return &r, nil
}

type FingerPrint struct {
	Kode   FlexString `json:"kode"`
	Status string     `json:"status"`
}

func (c *Client) CekFingerPrint(ctx context.Context, noka, tgl string) (*FingerPrint, error) {
	raw, err := c.call(ctx, c.vclaim, http.MethodGet,
		fmt.Sprintf("SEP/FingerPrint/Peserta/%s/TglPelayanan/%s", url.PathEscape(noka), tgl), nil)
	if err != nil {
		return nil, err
	}
	var r FingerPrint
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("parse fingerprint: %w", err)
	}
	return &r, nil
}

// TSep = isi request SEP/2.0/insert (urutan & nilai mengikuti BPJSDataSEP.insertSEP Khanza).
type TSep struct {
	NoKartu      string `json:"noKartu"`
	TglSep       string `json:"tglSep"`
	PpkPelayanan string `json:"ppkPelayanan"`
	JnsPelayanan string `json:"jnsPelayanan"`
	KlsRawat     struct {
		KlsRawatHak     string `json:"klsRawatHak"`
		KlsRawatNaik    string `json:"klsRawatNaik"`
		Pembiayaan      string `json:"pembiayaan"`
		PenanggungJawab string `json:"penanggungJawab"`
	} `json:"klsRawat"`
	NoMR    string `json:"noMR"`
	Rujukan struct {
		AsalRujukan string `json:"asalRujukan"`
		TglRujukan  string `json:"tglRujukan"`
		NoRujukan   string `json:"noRujukan"`
		PpkRujukan  string `json:"ppkRujukan"`
	} `json:"rujukan"`
	Catatan  string `json:"catatan"`
	DiagAwal string `json:"diagAwal"`
	Poli     struct {
		Tujuan    string `json:"tujuan"`
		Eksekutif string `json:"eksekutif"`
	} `json:"poli"`
	Cob struct {
		Cob string `json:"cob"`
	} `json:"cob"`
	Katarak struct {
		Katarak string `json:"katarak"`
	} `json:"katarak"`
	Jaminan struct {
		LakaLantas string `json:"lakaLantas"`
		NoLP       string `json:"noLP"`
		Penjamin   struct {
			TglKejadian string `json:"tglKejadian"`
			Keterangan  string `json:"keterangan"`
			Suplesi     struct {
				Suplesi      string `json:"suplesi"`
				NoSepSuplesi string `json:"noSepSuplesi"`
				LokasiLaka   struct {
					KdPropinsi  string `json:"kdPropinsi"`
					KdKabupaten string `json:"kdKabupaten"`
					KdKecamatan string `json:"kdKecamatan"`
				} `json:"lokasiLaka"`
			} `json:"suplesi"`
		} `json:"penjamin"`
	} `json:"jaminan"`
	TujuanKunj    string `json:"tujuanKunj"`
	FlagProcedure string `json:"flagProcedure"`
	KdPenunjang   string `json:"kdPenunjang"`
	AssesmentPel  string `json:"assesmentPel"`
	Skdp          struct {
		NoSurat  string `json:"noSurat"`
		KodeDPJP string `json:"kodeDPJP"`
	} `json:"skdp"`
	DpjpLayan string `json:"dpjpLayan"`
	NoTelp    string `json:"noTelp"`
	User      string `json:"user"`
}

// InsertSEP — TIDAK di-retry otomatis (risiko SEP ganda).
func (c *Client) InsertSEP(ctx context.Context, t TSep) (string, error) {
	body := map[string]any{"request": map[string]any{"t_sep": t}}
	raw, err := c.call(ctx, c.vclaim, http.MethodPost, "SEP/2.0/insert", body)
	if err != nil {
		return "", err
	}
	var r struct {
		Sep struct {
			NoSep string `json:"noSep"`
		} `json:"sep"`
	}
	if err := json.Unmarshal(raw, &r); err != nil || r.Sep.NoSep == "" {
		return "", fmt.Errorf("response insert SEP tanpa noSep")
	}
	return r.Sep.NoSep, nil
}

func (c *Client) DeleteSEP(ctx context.Context, noSep, user string) error {
	body := map[string]any{"request": map[string]any{"t_sep": map[string]string{"noSep": noSep, "user": user}}}
	_, err := c.call(ctx, c.vclaim, http.MethodDelete, "SEP/2.0/delete", body)
	return err
}

type HistoriPelayanan struct {
	NoSep        string `json:"noSep"`
	TglSep       string `json:"tglSep"`
	JnsPelayanan string `json:"jnsPelayanan"`
	Poli         string `json:"poli"`
	PpkPelayanan string `json:"ppkPelayanan"`
	NoRujukan    string `json:"noRujukan"`
}

// HistoriPelayanan dipakai untuk memastikan apakah SEP sudah terbit setelah insert timeout.
func (c *Client) HistoriPelayanan(ctx context.Context, noka, tgl string) ([]HistoriPelayanan, error) {
	raw, err := c.call(ctx, c.vclaim, http.MethodGet,
		fmt.Sprintf("monitoring/HistoriPelayanan/NoKartu/%s/tglMulai/%s/tglAkhir/%s", url.PathEscape(noka), tgl, tgl), nil)
	if err != nil {
		if IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	var r struct {
		Histori []HistoriPelayanan `json:"histori"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, err
	}
	return r.Histori, nil
}

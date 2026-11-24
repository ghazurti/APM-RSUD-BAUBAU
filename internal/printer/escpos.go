// Package printer mencetak karcis antrean & SEP ke printer thermal 80mm (ESC/POS raw).
package printer

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"apm-khanza/internal/service"
)

const lebar = 48 // karakter per baris font A pada kertas 80mm

type Printer struct {
	Mode string // tcp | windows-share | file | off
	Addr string // tcp: ip:9100 ; windows-share: \\PC-KIOSK\EPSON ; file: path
}

func (p *Printer) Kirim(data []byte) error {
	switch p.Mode {
	case "off", "":
		return nil
	case "tcp":
		c, err := net.DialTimeout("tcp", p.Addr, 5*time.Second)
		if err != nil {
			return err
		}
		defer c.Close()
		_ = c.SetWriteDeadline(time.Now().Add(10 * time.Second))
		_, err = c.Write(data)
		return err
	case "windows-share":
		// Printer di-share dari Windows (\\host\nama_share) menerima raw bytes langsung.
		f, err := os.OpenFile(p.Addr, os.O_WRONLY, 0)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = f.Write(data)
		return err
	case "file":
		f, err := os.OpenFile(p.Addr, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = f.Write(data)
		return err
	}
	return fmt.Errorf("PRINTER_MODE tidak dikenal: %s", p.Mode)
}

// ---- builder ESC/POS ----

type buf struct{ bytes.Buffer }

func (b *buf) init()          { b.Write([]byte{0x1B, 0x40}) }
func (b *buf) center()        { b.Write([]byte{0x1B, 0x61, 1}) }
func (b *buf) left()          { b.Write([]byte{0x1B, 0x61, 0}) }
func (b *buf) bold(on bool)   { b.Write([]byte{0x1B, 0x45, bit(on)}) }
func (b *buf) size(w, h byte) { b.Write([]byte{0x1D, 0x21, (w-1)<<4 | (h - 1)}) }
func (b *buf) line(s string)  { b.WriteString(ascii(s) + "\n") }
func (b *buf) garis()         { b.line(strings.Repeat("-", lebar)) }
func (b *buf) feedCut()       { b.Write([]byte{0x1B, 0x64, 4, 0x1D, 0x56, 0x42, 0}) }

func (b *buf) kv(k, v string) {
	const kw = 14
	k = fmt.Sprintf("%-*s: ", kw, k)
	for _, l := range wrap(v, lebar-len(k)) {
		b.line(k + l)
		k = strings.Repeat(" ", len(k))
	}
}

func bit(on bool) byte {
	if on {
		return 1
	}
	return 0
}

// ascii: printer thermal umumnya hanya CP437; ganti karakter non-ASCII.
func ascii(s string) string {
	return strings.Map(func(r rune) rune {
		if r > 126 {
			return '?'
		}
		return r
	}, s)
}

func wrap(s string, n int) []string {
	if n < 8 {
		n = 8
	}
	var out []string
	cur := ""
	for _, w := range strings.Fields(s) {
		for len(w) > n {
			if cur != "" {
				out, cur = append(out, cur), ""
			}
			out, w = append(out, w[:n]), w[n:]
		}
		if cur == "" {
			cur = w
		} else if len(cur)+1+len(w) <= n {
			cur += " " + w
		} else {
			out, cur = append(out, cur), w
		}
	}
	if cur != "" || len(out) == 0 {
		out = append(out, cur)
	}
	return out
}

func tglIndo(ymd string) string {
	if t, err := time.Parse("2006-01-02", ymd); err == nil {
		return t.Format("02-01-2006")
	}
	return ymd
}

type RS struct{ Nama, Alamat, Kabupaten string }

// Karcis antrean: No. Antrean double size, nama disamarkan.
func Karcis(rs RS, h *service.Hasil) []byte {
	var b buf
	b.init()
	b.center()
	b.bold(true)
	b.line(rs.Nama)
	b.bold(false)
	for _, l := range wrap(rs.Alamat+" "+rs.Kabupaten, lebar) {
		b.line(l)
	}
	b.garis()
	b.line("ANTREAN POLI")
	b.bold(true)
	b.line(h.Poli)
	b.bold(false)
	b.size(3, 3)
	b.line(h.NoReg)
	b.size(1, 1)
	b.line(h.Dokter)
	b.garis()
	b.left()
	b.kv("No. RM", h.NoRM)
	b.kv("Nama", service.Samarkan(h.NamaPasien))
	b.kv("Tgl/Jam", tglIndo(h.Tgl)+" "+h.Jam)
	b.kv("Estimasi", "pukul "+h.Estimasi+" WITA")
	b.kv("No. SEP", h.NoSEP)
	b.kv("Kunjungan", h.JenisKunjungan)
	b.garis()
	b.center()
	b.line("Simpan karcis ini & tunggu panggilan")
	b.line("di ruang tunggu poli.")
	b.feedCut()
	return b.Bytes()
}

// SEP mengikuti isi cetak SEP Khanza.
func SEP(rs RS, h *service.Hasil) []byte {
	var b buf
	b.init()
	b.center()
	b.bold(true)
	b.line("SURAT ELIGIBILITAS PESERTA")
	b.line(rs.Nama)
	b.bold(false)
	b.garis()
	b.left()
	b.kv("No. SEP", h.NoSEP)
	b.kv("Tgl. SEP", tglIndo(h.Tgl))
	b.kv("No. Kartu", h.NoKartu+" (MR. "+h.NoRM+")")
	b.kv("Nama Peserta", h.NamaPasien)
	b.kv("Tgl. Lahir", tglIndo(h.TglLahir))
	b.kv("Sub/Spesialis", h.PoliBPJS)
	b.kv("Dokter", h.Dokter)
	b.kv("Faskes Perujuk", h.FaskesPerujuk)
	b.kv("No. Rujukan", h.NoRujukan)
	b.kv("Diagnosa Awal", h.Diagnosa)
	b.kv("Peserta", h.JenisPeserta)
	b.kv("Jns. Rawat", "Rawat Jalan")
	b.kv("Jns. Kunjungan", h.JenisKunjungan)
	b.kv("Kelas Hak", h.Kelas)
	b.kv("Catatan", h.Catatan)
	if h.PRB != "" {
		b.kv("PRB", h.PRB)
	}
	b.garis()
	for _, l := range wrap("*Saya menyetujui BPJS Kesehatan menggunakan informasi medis pasien jika diperlukan. "+
		"*SEP bukan sebagai bukti penjaminan peserta.", lebar) {
		b.line(l)
	}
	b.line("")
	b.line("Pasien/Keluarga Pasien")
	b.line("")
	b.line("")
	b.line("(" + strings.Repeat("_", 24) + ")")
	b.line("Cetak: " + time.Now().Format("02-01-2006 15:04:05"))
	b.feedCut()
	return b.Bytes()
}

// Package kiosk dipakai oleh apmagent.exe di PC anjungan: membuka aplikasi BPJS
// (Sidik Jari / Frista) lalu mengetik isian seperti DlgReg.BukaFingerPrint di Khanza.
package kiosk

import (
	"fmt"
	"strconv"
	"strings"
)

// Langkah adalah satu aksi keyboard. Jenis: tunggu | ketik | tab | enter | fokus.
type Langkah struct {
	Jenis string
	Teks  string // untuk ketik; boleh berisi {NOKA} {NIK} {USER} {PASS}
	Ms    int    // untuk tunggu
}

// ParseLangkah membaca urutan seperti "tunggu:1500;ketik:{USER};tab;ketik:{PASS};enter".
func ParseLangkah(s string) ([]Langkah, error) {
	var out []Langkah
	for _, bag := range strings.Split(s, ";") {
		bag = strings.TrimSpace(bag)
		if bag == "" {
			continue
		}
		jenis, arg, _ := strings.Cut(bag, ":")
		jenis = strings.ToLower(strings.TrimSpace(jenis))
		switch jenis {
		case "tunggu":
			ms, err := strconv.Atoi(strings.TrimSpace(arg))
			if err != nil || ms < 0 || ms > 30000 {
				return nil, fmt.Errorf("langkah %q: tunggu harus 0-30000 ms", bag)
			}
			out = append(out, Langkah{Jenis: jenis, Ms: ms})
		case "ketik":
			if arg == "" {
				return nil, fmt.Errorf("langkah %q: teks kosong", bag)
			}
			out = append(out, Langkah{Jenis: jenis, Teks: arg})
		case "tab", "enter", "fokus":
			out = append(out, Langkah{Jenis: jenis})
		default:
			return nil, fmt.Errorf("langkah %q tidak dikenal (pakai tunggu/ketik/tab/enter/fokus)", bag)
		}
	}
	return out, nil
}

// Isi mengganti placeholder dengan nilai sebenarnya.
func Isi(teks string, nilai map[string]string) string {
	for k, v := range nilai {
		teks = strings.ReplaceAll(teks, "{"+k+"}", v)
	}
	return teks
}

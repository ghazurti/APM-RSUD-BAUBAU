package kiosk

import (
	"reflect"
	"testing"
)

func TestParseLangkah(t *testing.T) {
	got, err := ParseLangkah(" tunggu:1500; ketik:{USER} ;TAB;ketik:{PASS};enter;fokus;ketik:{NOKA}; ")
	if err != nil {
		t.Fatal(err)
	}
	want := []Langkah{
		{Jenis: "tunggu", Ms: 1500},
		{Jenis: "ketik", Teks: "{USER}"},
		{Jenis: "tab"},
		{Jenis: "ketik", Teks: "{PASS}"},
		{Jenis: "enter"},
		{Jenis: "fokus"},
		{Jenis: "ketik", Teks: "{NOKA}"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestParseLangkahSalah(t *testing.T) {
	for _, s := range []string{"tunggu:abc", "tunggu:99999", "ketik:", "klik"} {
		if _, err := ParseLangkah(s); err == nil {
			t.Errorf("%q seharusnya error", s)
		}
	}
}

func TestIsi(t *testing.T) {
	got := Isi("{NOKA}-{NIK}-{NOKA}", map[string]string{"NOKA": "0001", "NIK": "73"})
	if got != "0001-73-0001" {
		t.Fatalf("got %q", got)
	}
}

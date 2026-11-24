package service

import (
	"strings"
	"testing"
	"time"

	"apm-khanza/internal/config"
)

func TestCatatanSEP(t *testing.T) {
	loc := time.FixedZone("WITA", 8*3600)
	svc := &Service{cfg: &config.Config{
		SEPCatatan:         "Pendaftaran APM",
		MasaBerlakuRujukan: 90,
		Zona:               loc,
	}}

	got := svc.catatanSEP("2026-11-23")
	want := "Pendaftaran APM | Batas Rujukan: 21-02-2027"
	if got != want {
		t.Fatalf("catatanSEP = %q, want %q", got, want)
	}

	svc.cfg.SEPCatatan = ""
	if got2 := svc.catatanSEP("2026-11-23"); got2 != "Batas Rujukan: 21-02-2027" {
		t.Fatalf("catatanSEP kosong = %q", got2)
	}

	svc.cfg.SEPCatatan = strings.Repeat("A", 95)
	if got3 := svc.catatanSEP("2026-11-23"); len(got3) != 100 {
		t.Fatalf("catatanSEP harus <= 100, dapat %d", len(got3))
	}
}

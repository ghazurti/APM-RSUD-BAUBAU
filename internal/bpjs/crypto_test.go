package bpjs

import (
	"testing"
	"time"
)

// Vektor dibuat di luar Go (OpenSSL + lz-string JS 1.5.0, implementasi yang sama dengan
// yang dipakai BPJS/Khanza), jadi test ini memverifikasi kecocokan lintas implementasi.
const (
	tConsID = "1234"
	tSecret = "rahasia"
	tTS     = "1700000000"
)

func TestSignature(t *testing.T) {
	// echo -n "1234&1700000000" | openssl dgst -sha256 -hmac "rahasia" -binary | base64
	want := "KPRS9TtuHrVFuDkmpci6eMlvW4INEXDsL4Hh+T+auLo="
	if got := Signature(tConsID, tSecret, tTS); got != want {
		t.Fatalf("signature = %s, want %s", got, want)
	}
}

func TestTimestamp(t *testing.T) {
	if got := Timestamp(time.Unix(1700000000, 0)); got != tTS {
		t.Fatalf("timestamp = %s", got)
	}
}

func TestDecryptVektorEksternal(t *testing.T) {
	enc := "Rt3CTIMyMbLjfOUa+DuFxBUJCiJezqo4EvlSqWPyK6MOuET7rNdAsoVCWIDDJ3ZuHBqU4H6QlTNyC74qqpaaYQJ39L/C6HnHxIIJkdnd8TjKHmeh8aH29v9NWwRJ2fCAp8GinMUKMoeevjL4fGIS/Q=="
	want := `{"peserta":{"noKartu":"0001234567890","nama":"AHMAD SALEH"}}`
	got, err := Decrypt(tConsID, tSecret, tTS, enc)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("decrypt = %s", got)
	}
}

func TestDecryptTimestampSalah(t *testing.T) {
	enc := "Rt3CTIMyMbLjfOUa+DuFxBUJCiJezqo4EvlSqWPyK6MOuET7rNdAsoVCWIDDJ3ZuHBqU4H6QlTNyC74qqpaaYQJ39L/C6HnHxIIJkdnd8TjKHmeh8aH29v9NWwRJ2fCAp8GinMUKMoeevjL4fGIS/Q=="
	if _, err := Decrypt(tConsID, tSecret, "1700000001", enc); err == nil {
		t.Fatal("harusnya gagal dengan timestamp berbeda")
	}
}

func TestRoundTripUnicode(t *testing.T) {
	plain := `{"nama":"SITI ÄISYAH","diagnosa":"K30 - Dyspepsia","list":[1,2,3]}`
	enc, err := encryptForTest(tConsID, tSecret, tTS, plain)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decrypt(tConsID, tSecret, tTS, enc)
	if err != nil || got != plain {
		t.Fatalf("roundtrip = %q, %v", got, err)
	}
}

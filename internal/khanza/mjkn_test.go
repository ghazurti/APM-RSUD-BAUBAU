package khanza

import "testing"

func TestBookingMJKNHelpers(t *testing.T) {
	cases := []struct {
		jenis string
		want  int
	}{
		{"1 (Rujukan FKTP)", 1},
		{"2 (Rujukan Internal)", 2},
		{"3 (Kontrol)", 3},
		{"4 (Rujukan Antar RS)", 4},
		{"", 0},
	}
	for _, tc := range cases {
		b := BookingMJKN{JenisKunjungan: tc.jenis}
		if got := b.JenisKode(); got != tc.want {
			t.Errorf("JenisKode(%q) = %d, want %d", tc.jenis, got, tc.want)
		}
	}

	b := BookingMJKN{JamPraktek: "08:00-14:00"}
	mulai, selesai := b.JamMulaiSelesai()
	if mulai != "08:00:00" || selesai != "14:00:00" {
		t.Errorf("JamMulaiSelesai = (%q, %q), want (08:00:00, 14:00:00)", mulai, selesai)
	}

	b2 := BookingMJKN{JamPraktek: ""}
	m2, s2 := b2.JamMulaiSelesai()
	if m2 != "08:00:00" || s2 != "14:00:00" {
		t.Errorf("JamMulaiSelesai fallback = (%q, %q), want (08:00:00, 14:00:00)", m2, s2)
	}
}

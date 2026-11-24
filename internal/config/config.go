// Package config memuat konfigurasi APM dari file .env server.
// Kredensial BPJS hanya ada di server, tidak pernah dikirim ke browser kiosk.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	AppPort    string
	KioskID    string
	UserSEP    string
	IdleDetik  int
	SelesaiDtk int

	DBHost string
	DBPort string
	DBName string
	DBUser string
	DBPass string

	VClaimURL        string
	AntreanURL       string
	ConsID           string
	Secret           string
	UserKeyVClaim    string
	UserKeyAntrean   string
	AntreanConsID    string // kosong = pakai ConsID (Khanza: CONSIDAPIMOBILEJKN)
	AntreanSecret    string // kosong = pakai Secret
	BPJSTimeout      time.Duration
	KirimAntrean     bool // sama dengan ADDANTRIANAPIMOBILEJKN di Khanza
	KirimTask3       bool
	MenitPerPasien   int
	KeteranganAntrol string

	KdPJBPJS  string
	UrutNoReg string // "dokter" | "poli" | "dokter + poli" (sama dgn URUTNOREG Khanza)
	BaseNoReg string // "registrasi" | "booking" (sama dgn BASENOREG Khanza)
	KdPPK     string

	SEPCatatan         string
	SEPKontrolTujuan   string
	SEPKontrolAsesmen  string
	FingerprintMode    string   // off | poli | semua
	FingerprintPoli    []string // kd_poli_bpjs yang wajib biometrik bila mode=poli
	MasaBerlakuRujukan int      // hari

	// apmagent.exe di PC kiosk (buka aplikasi Sidik Jari / Frista). Token kosong = fitur mati.
	AgentToken       string
	AgentURL         string // kosong = http://<IP PC kiosk peminta>:<AgentPort>
	AgentPort        string
	BiometrikTahan   int // detik layar APM tidak kembali ke beranda selama pasien di aplikasi BPJS
	AgentFingerprint bool
	AgentFrista      bool

	PrinterMode string // tcp | windows-share | file | off
	PrinterAddr string
	CetakSEP    string // browser (format Khanza, printer default PC kiosk) | escpos (thermal) | off

	Zona *time.Location
}

func Load() (*Config, error) {
	_ = godotenv.Load() // .env opsional bila env sudah di-set oleh service manager

	zona, err := time.LoadLocation("Asia/Makassar")
	if err != nil {
		// Windows tanpa tzdata: pakai offset tetap WITA (+08:00)
		zona = time.FixedZone("WITA", 8*3600)
	}

	c := &Config{
		AppPort:    env("APP_PORT", "8088"),
		KioskID:    env("APP_KIOSK_ID", "APM-01"),
		UserSEP:    env("APP_USER_SEP", "APM"),
		IdleDetik:  envInt("APP_IDLE_DETIK", 60),
		SelesaiDtk: envInt("APP_SELESAI_DETIK", 15),

		DBHost: env("DB_HOST", "127.0.0.1"),
		DBPort: env("DB_PORT", "3306"),
		DBName: env("DB_NAME", "sik"),
		DBUser: env("DB_USER", ""),
		DBPass: env("DB_PASS", ""),

		VClaimURL:        strings.TrimRight(env("BPJS_VCLAIM_URL", ""), "/"),
		AntreanURL:       strings.TrimRight(env("BPJS_ANTREAN_URL", ""), "/"),
		ConsID:           env("BPJS_CONS_ID", ""),
		Secret:           env("BPJS_SECRET", ""),
		UserKeyVClaim:    env("BPJS_USER_KEY_VCLAIM", ""),
		UserKeyAntrean:   env("BPJS_USER_KEY_ANTREAN", ""),
		AntreanConsID:    env("BPJS_ANTREAN_CONS_ID", ""),
		AntreanSecret:    env("BPJS_ANTREAN_SECRET", ""),
		BPJSTimeout:      time.Duration(envInt("BPJS_TIMEOUT_SEC", 20)) * time.Second,
		KirimAntrean:     envBool("BPJS_KIRIM_ANTREAN", true),
		KirimTask3:       envBool("BPJS_KIRIM_TASK3", true),
		MenitPerPasien:   envInt("ANTREAN_MENIT_PER_PASIEN", 5),
		KeteranganAntrol: env("ANTREAN_KETERANGAN", "Peserta harap 30 menit lebih awal guna pencatatan administrasi."),

		KdPJBPJS:  env("KHANZA_KD_PJ_BPJS", "BPJ"),
		UrutNoReg: normalisasiUrut(env("KHANZA_URUT_NOREG", "dokter")),
		BaseNoReg: strings.ToLower(env("KHANZA_BASE_NOREG", "registrasi")),
		KdPPK:     env("KHANZA_KD_PPK", ""),

		SEPCatatan:         env("SEP_CATATAN", "Pendaftaran APM"),
		SEPKontrolTujuan:   env("SEP_KONTROL_TUJUANKUNJ", "2"),
		SEPKontrolAsesmen:  env("SEP_KONTROL_ASESMEN", "5"),
		FingerprintMode:    strings.ToLower(env("FINGERPRINT_MODE", "poli")),
		FingerprintPoli:    envList("FINGERPRINT_POLI_WAJIB"),
		MasaBerlakuRujukan: envInt("RUJUKAN_BERLAKU_HARI", 90),

		AgentToken:       env("KIOSK_AGENT_TOKEN", ""),
		AgentURL:         strings.TrimRight(env("KIOSK_AGENT_URL", ""), "/"),
		AgentPort:        env("KIOSK_AGENT_PORT", "8099"),
		BiometrikTahan:   envInt("BIOMETRIK_TAHAN_DETIK", 180),
		AgentFingerprint: envBool("KIOSK_TOMBOL_FINGERPRINT", true),
		AgentFrista:      envBool("KIOSK_TOMBOL_FRISTA", true),

		PrinterMode: strings.ToLower(env("PRINTER_MODE", "tcp")),
		PrinterAddr: env("PRINTER_ADDR", ""),
		CetakSEP:    strings.ToLower(env("PRINTER_SEP", "browser")),

		Zona: zona,
	}

	var kurang []string
	for k, v := range map[string]string{
		"DB_USER": c.DBUser, "BPJS_VCLAIM_URL": c.VClaimURL, "BPJS_CONS_ID": c.ConsID,
		"BPJS_SECRET": c.Secret, "BPJS_USER_KEY_VCLAIM": c.UserKeyVClaim,
	} {
		if v == "" {
			kurang = append(kurang, k)
		}
	}
	if c.KirimAntrean && (c.AntreanURL == "" || c.UserKeyAntrean == "") {
		kurang = append(kurang, "BPJS_ANTREAN_URL/BPJS_USER_KEY_ANTREAN")
	}
	if len(kurang) > 0 {
		return nil, fmt.Errorf("konfigurasi .env belum lengkap: %s", strings.Join(kurang, ", "))
	}
	if c.AntreanConsID == "" {
		c.AntreanConsID = c.ConsID
	}
	if c.AntreanSecret == "" {
		c.AntreanSecret = c.Secret
	}
	if !envBool("PRINTER_CETAK_SEP", true) { // nama lama
		c.CetakSEP = "off"
	}
	if c.CetakSEP != "escpos" && c.CetakSEP != "off" {
		c.CetakSEP = "browser"
	}
	if c.AgentToken != "" && len(c.AgentToken) < 16 {
		return nil, fmt.Errorf("KIOSK_AGENT_TOKEN minimal 16 karakter")
	}
	if c.BaseNoReg != "booking" {
		c.BaseNoReg = "registrasi"
	}
	return c, nil
}

// DSN koneksi MySQL. Zona waktu WITA dipakai di semua tempat.
func (c *Config) DSN() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true&loc=Asia%%2FMakassar&timeout=10s&readTimeout=30s&writeTimeout=30s",
		c.DBUser, c.DBPass, c.DBHost, c.DBPort, c.DBName)
}

func normalisasiUrut(s string) string {
	s = strings.ToLower(strings.ReplaceAll(s, " ", ""))
	switch s {
	case "poli":
		return "poli"
	case "dokter+poli":
		return "dokter + poli"
	default:
		return "dokter"
	}
}

func env(k, def string) string {
	if v, ok := os.LookupEnv(k); ok {
		return strings.TrimSpace(v)
	}
	return def
}

func envInt(k string, def int) int {
	if n, err := strconv.Atoi(env(k, "")); err == nil {
		return n
	}
	return def
}

func envBool(k string, def bool) bool {
	switch strings.ToLower(env(k, "")) {
	case "1", "yes", "ya", "true", "on":
		return true
	case "0", "no", "tidak", "false", "off":
		return false
	}
	return def
}

func envList(k string) []string {
	var out []string
	for _, s := range strings.Split(env(k, ""), ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, strings.ToUpper(s))
		}
	}
	return out
}

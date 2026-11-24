// apmagent berjalan di PC anjungan (sesi Windows yang login, BUKAN service NSSM, karena
// service tidak bisa menampilkan jendela). Server APM memanggilnya untuk membuka aplikasi
// Sidik Jari / Frista BPJS dan mengetik No. Kartu, seperti tombol finger di DlgReg Khanza.
//
// Konfigurasi: agent.env di folder yang sama dengan apmagent.exe.
package main

import (
	"crypto/subtle"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/joho/godotenv"

	"apm-khanza/internal/kiosk"
)

var reAngka = regexp.MustCompile(`^[0-9]{0,20}$`)

func main() {
	if exe, err := os.Executable(); err == nil {
		_ = os.Chdir(filepath.Dir(exe))
	}
	_ = godotenv.Load("agent.env")
	// Dibuild dengan -H windowsgui (tanpa console), jadi log ditulis ke file.
	var out io.Writer = os.Stdout
	if err := os.MkdirAll("logs", 0o755); err == nil {
		if f, err := os.OpenFile(filepath.Join("logs", "apmagent.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
			out = f
		}
	}
	log := slog.New(slog.NewJSONHandler(out, nil))

	token := env("AGENT_TOKEN", "")
	if len(token) < 16 {
		log.Error("AGENT_TOKEN wajib diisi minimal 16 karakter (samakan dengan KIOSK_AGENT_TOKEN di .env server APM)")
		os.Exit(1)
	}
	izinIP := map[string]bool{}
	for _, ip := range strings.Split(env("AGENT_IZIN_IP", ""), ",") {
		if ip = strings.TrimSpace(ip); ip != "" {
			izinIP[ip] = true
		}
	}

	ag := &kiosk.Agent{Apps: map[string]*kiosk.App{}, Log: log}
	for nama, prefix := range map[string]string{"fingerprint": "FINGERPRINT_", "frista": "FRISTA_"} {
		exe := env(prefix+"EXE", "")
		if exe == "" {
			continue
		}
		langkah, err := kiosk.ParseLangkah(env(prefix+"LANGKAH", ""))
		if err != nil {
			log.Error("config", "app", nama, "err", err.Error())
			os.Exit(1)
		}
		ag.Apps[nama] = &kiosk.App{
			Nama: nama, Exe: exe, Judul: env(prefix+"JUDUL", ""),
			User: env(prefix+"USER", ""), Pass: env(prefix+"PASS", ""),
			Langkah: langkah, TungguJdl: 15 * time.Second,
		}
		log.Info("aplikasi siap", "app", nama, "langkah", len(langkah))
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/buka", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		ip, _, _ := net.SplitHostPort(r.RemoteAddr)
		if len(izinIP) > 0 && !izinIP[ip] {
			log.Warn("ditolak", "ip", ip)
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Agent-Token")), []byte(token)) != 1 {
			log.Warn("token salah", "ip", ip)
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		var req struct{ App, NoKartu, NIK string }
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil ||
			!reAngka.MatchString(req.NoKartu) || !reAngka.MatchString(req.NIK) {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := ag.Buka(req.App, req.NoKartu, req.NIK); err != nil {
			log.Error("buka", "app", req.App, "err", err.Error())
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	addr := env("AGENT_LISTEN", ":8099")
	log.Info("apmagent mulai", "addr", addr)
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	if err := srv.ListenAndServe(); err != nil {
		log.Error("listen", "err", err.Error())
		os.Exit(1)
	}
}

func env(k, def string) string {
	if v, ok := os.LookupEnv(k); ok {
		return strings.TrimSpace(v)
	}
	return def
}

// Package applog menulis log JSON-lines ke logs/apm-YYYY-MM-DD.log (tidak ke DB).
package applog

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// dailyWriter membuka file baru setiap ganti tanggal (WITA).
type dailyWriter struct {
	mu   sync.Mutex
	dir  string
	zona *time.Location
	tgl  string
	f    *os.File
}

func (w *dailyWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	tgl := time.Now().In(w.zona).Format("2006-01-02")
	if w.f == nil || tgl != w.tgl {
		if w.f != nil {
			_ = w.f.Close()
		}
		f, err := os.OpenFile(filepath.Join(w.dir, "apm-"+tgl+".log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return os.Stderr.Write(p)
		}
		w.f, w.tgl = f, tgl
	}
	return w.f.Write(p)
}

// New membuat logger JSON. Juga ditulis ke stdout supaya terlihat di NSSM/console.
func New(dir string, zona *time.Location) *slog.Logger {
	_ = os.MkdirAll(dir, 0o755)
	w := io.MultiWriter(&dailyWriter{dir: dir, zona: zona}, os.Stdout)
	h := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: slog.LevelInfo,
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				a.Value = slog.StringValue(a.Value.Time().In(zona).Format("2006-01-02T15:04:05.000-07:00"))
			}
			return a
		},
	})
	return slog.New(h)
}

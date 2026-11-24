package kiosk

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// App = satu aplikasi BPJS yang bisa dibuka dari anjungan.
type App struct {
	Nama       string
	Exe        string
	Judul      string // potongan judul jendela; dipakai bila exe membuka proses lain (launcher)
	User, Pass string
	Langkah    []Langkah
	TungguJdl  time.Duration
	proc       *os.Process
}

type Agent struct {
	mu   sync.Mutex // satu aplikasi dibuka pada satu waktu
	Apps map[string]*App
	Log  *slog.Logger
}

var ErrTidakDidukung = errors.New("hanya didukung di Windows (amd64)")

// Buka menjalankan aplikasi lalu mengetik langkah-langkahnya ke jendela aplikasi itu.
// Tidak pernah mengetik bila jendela aplikasi tidak ditemukan, supaya input tidak masuk ke browser.
func (a *Agent) Buka(nama, noka, nik string) error {
	if !didukung {
		return ErrTidakDidukung
	}
	app := a.Apps[nama]
	if app == nil || app.Exe == "" {
		return fmt.Errorf("aplikasi %q belum dikonfigurasi", nama)
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	// Tutup jendela milik pasien sebelumnya agar data lama tidak terpakai.
	if app.proc != nil {
		_ = app.proc.Kill() // Wait sudah dijalankan goroutine di bawah
		app.proc = nil
	}
	cmd := exec.Command(app.Exe)
	cmd.Dir = filepath.Dir(app.Exe)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("gagal menjalankan %s: %w", app.Exe, err)
	}
	app.proc = cmd.Process
	go func(p *os.Process) { _, _ = p.Wait() }(cmd.Process)

	jendela, err := cariJendela(cmd.Process.Pid, app.Judul, app.TungguJdl)
	if err != nil {
		return err
	}
	nilai := map[string]string{"NOKA": noka, "NIK": nik, "USER": app.User, "PASS": app.Pass}
	for _, l := range app.Langkah {
		switch l.Jenis {
		case "tunggu":
			time.Sleep(time.Duration(l.Ms) * time.Millisecond)
			continue
		}
		// Fokus ulang sebelum tiap input: bila pasien menyentuh layar, input tetap ke aplikasi BPJS.
		if err := fokusJendela(jendela); err != nil {
			return err
		}
		switch l.Jenis {
		case "ketik":
			err = ketikTeks(Isi(l.Teks, nilai))
		case "tab":
			err = tekanTab()
		case "enter":
			err = tekanEnter()
		}
		if err != nil {
			return err
		}
		time.Sleep(80 * time.Millisecond)
	}
	a.Log.Info("buka", "app", nama, "noka", samarkan(noka))
	return nil
}

func samarkan(s string) string {
	if len(s) <= 4 {
		return "****"
	}
	return "****" + s[len(s)-4:]
}

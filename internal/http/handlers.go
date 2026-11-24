// Package http berisi handler Fiber. Semua endpoint mengembalikan partial HTML untuk HTMX.
package http

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"

	"apm-khanza/internal/config"
	"apm-khanza/internal/printer"
	"apm-khanza/internal/service"
)

const cookieSesi = "apm_sid"

type entry struct {
	mu   sync.Mutex // satu request per pasien pada satu waktu (cegah double submit)
	sesi *service.Sesi
	seen time.Time
}

type store struct {
	mu sync.Mutex
	m  map[string]*entry
}

func (s *store) get(id string) *entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.m[id]
	if e != nil {
		e.seen = time.Now()
	}
	return e
}

func (s *store) put(id string, ss *service.Sesi) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[id] = &entry{sesi: ss, seen: time.Now()}
}

func (s *store) del(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, id)
}

func (s *store) janitor(ttl time.Duration) {
	for range time.Tick(time.Minute) {
		s.mu.Lock()
		for k, e := range s.m {
			if time.Since(e.seen) > ttl {
				delete(s.m, k)
			}
		}
		s.mu.Unlock()
	}
}

type Handler struct {
	cfg   *config.Config
	svc   *service.Service
	log   *slog.Logger
	print *printer.Printer
	rs    printer.RS
	st    *store
	logo  []byte
}

func New(cfg *config.Config, svc *service.Service, log *slog.Logger, p *printer.Printer) *Handler {
	h := &Handler{cfg: cfg, svc: svc, log: log, print: p, st: &store{m: map[string]*entry{}},
		rs: printer.RS{Nama: svc.Setting.NamaInstansi, Alamat: svc.Setting.Alamat, Kabupaten: svc.Setting.Kabupaten}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	h.logo = svc.LogoBPJS(ctx)
	go h.st.janitor(15 * time.Minute)
	return h
}

func (h *Handler) Routes(app *fiber.App) {
	app.Get("/", h.index)
	app.Get("/beranda", h.beranda)
	app.Get("/batal", h.batal)
	app.Get("/umum", func(c *fiber.Ctx) error {
		return h.pesan(c, "Pendaftaran pasien umum/non-BPJS dilayani di loket pendaftaran.", false)
	})
	app.Get("/cetak/sep", h.cetakSEP)
	app.Get("/cetak/logo-bpjs", func(c *fiber.Ctx) error {
		if len(h.logo) == 0 {
			return c.SendStatus(fiber.StatusNotFound)
		}
		c.Set("Content-Type", http.DetectContentType(h.logo))
		return c.Send(h.logo)
	})
	app.Get("/identitas", h.identitas)
	app.Get("/mjkn-checkin", h.mjknCheckin)
	app.Post("/peserta", h.peserta)

	g := app.Group("", h.butuhSesi)
	g.Get("/konfirmasi", h.konfirmasi)
	g.Post("/mjkn-lanjut", h.mjknLanjut)
	g.Get("/jenis", h.jenis)
	g.Get("/rujukan", h.rujukan)
	g.Post("/pilih-rujukan", h.pilihRujukan)
	g.Get("/kontrol", h.kontrol)
	g.Post("/pilih-kontrol", h.pilihKontrol)
	g.Get("/jadwal", h.jadwal)
	g.Post("/pilih-jadwal", h.pilihJadwal)
	g.Post("/cek-biometrik", h.cekBiometrik)
	g.Post("/buka-biometrik", h.bukaBiometrik)
	g.Post("/proses", h.proses)
	g.Post("/cetak-ulang", h.cetakUlang)
	g.Post("/batal-daftar", h.batalDaftar)
}

// ---- util ----

func (h *Handler) base(c *fiber.Ctx, m fiber.Map) fiber.Map {
	if m == nil {
		m = fiber.Map{}
	}
	m["RS"] = h.rs.Nama
	m["Kiosk"] = h.cfg.KioskID
	m["Idle"] = h.cfg.IdleDetik
	m["SelesaiDetik"] = h.cfg.SelesaiDtk
	m["TombolFinger"] = h.cfg.AgentToken != "" && h.cfg.AgentFingerprint
	m["TombolFrista"] = h.cfg.AgentToken != "" && h.cfg.AgentFrista
	return m
}

func (h *Handler) render(c *fiber.Ctx, name string, m fiber.Map) error {
	c.Set("Cache-Control", "no-store")
	return c.Render("partials/"+name, h.base(c, m))
}

func (h *Handler) pesan(c *fiber.Ctx, msg string, biometrik bool) error {
	return h.render(c, "pesan", fiber.Map{"Msg": msg, "Biometrik": biometrik})
}

func (h *Handler) gagal(c *fiber.Ctx, err error) error {
	var ue *service.UserError
	if errors.As(err, &ue) {
		if ue.Biometrik {
			return h.render(c, "biometrik", fiber.Map{"Status": ue.Msg})
		}
		return h.pesan(c, ue.Msg, false)
	}
	h.log.Error("handler", "path", c.Path(), "err", err.Error())
	return h.pesan(c, "Terjadi gangguan sistem. Silakan ke loket pendaftaran.", false)
}

func (h *Handler) sesi(c *fiber.Ctx) *entry { return c.Locals("entry").(*entry) }

func (h *Handler) butuhSesi(c *fiber.Ctx) error {
	e := h.st.get(c.Cookies(cookieSesi))
	if e == nil {
		return h.render(c, "beranda", fiber.Map{"Info": "Sesi berakhir, silakan mulai lagi."})
	}
	if !e.mu.TryLock() {
		return c.SendStatus(fiber.StatusConflict) // request ganda saat proses berjalan
	}
	defer e.mu.Unlock()
	c.Locals("entry", e)
	return c.Next()
}

func (h *Handler) ctx(c *fiber.Ctx, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(c.UserContext(), d)
}

func idBaru() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ---- layar ----

func (h *Handler) index(c *fiber.Ctx) error {
	h.st.del(c.Cookies(cookieSesi))
	return c.Render("partials/beranda", h.base(c, nil), "layouts/main")
}

func (h *Handler) beranda(c *fiber.Ctx) error { return h.render(c, "beranda", nil) }

func (h *Handler) batal(c *fiber.Ctx) error {
	h.st.del(c.Cookies(cookieSesi))
	c.ClearCookie(cookieSesi)
	return h.render(c, "beranda", nil)
}

func (h *Handler) identitas(c *fiber.Ctx) error {
	return h.render(c, "identitas", fiber.Map{"Mode": c.Query("mode")})
}

func (h *Handler) mjknCheckin(c *fiber.Ctx) error { return h.render(c, "mjkn_checkin", nil) }

func (h *Handler) peserta(c *fiber.Ctx) error {
	ctx, cancel := h.ctx(c, 3*h.cfg.BPJSTimeout)
	defer cancel()
	mode := c.FormValue("mode")
	ss, err := h.svc.CariPeserta(ctx, c.FormValue("nomor"))
	if err != nil {
		var ue *service.UserError
		if errors.As(err, &ue) && (len(c.FormValue("nomor")) != 13 && len(c.FormValue("nomor")) != 16) {
			return h.render(c, "identitas", fiber.Map{"Err": ue.Msg, "Mode": mode})
		}
		return h.gagal(c, err)
	}
	if mode == "mjkn" && ss.MJKN == nil {
		return h.pesan(c, "Booking Mobile JKN Anda untuk hari ini tidak ditemukan. Pastikan sudah mendaftar di aplikasi Mobile JKN, atau pilih jalur Pasien BPJS Onsite.", false)
	}
	id := idBaru()
	h.st.put(id, ss)
	c.Cookie(&fiber.Cookie{Name: cookieSesi, Value: id, HTTPOnly: true, SameSite: "Strict", Path: "/"})
	return h.renderKonfirmasi(c, ss)
}

func (h *Handler) renderKonfirmasi(c *fiber.Ctx, ss *service.Sesi) error {
	return h.render(c, "konfirmasi", fiber.Map{
		"Nama": service.Samarkan(ss.Pasien.Nama), "NoRM": ss.Pasien.NoRM, "NoKartu": ss.Peserta.NoKartu,
		"Kelas": ss.Peserta.HakKelas.Keterangan, "Status": ss.Peserta.StatusPeserta.Keterangan,
		"Jenis": ss.Peserta.JenisPeserta.Keterangan, "Faskes": ss.Peserta.ProvUmum.NmProvider,
		"MJKN": ss.MJKN,
	})
}

func (h *Handler) konfirmasi(c *fiber.Ctx) error { return h.renderKonfirmasi(c, h.sesi(c).sesi) }

func (h *Handler) mjknLanjut(c *fiber.Ctx) error {
	ss := h.sesi(c).sesi
	ctx, cancel := h.ctx(c, 3*h.cfg.BPJSTimeout)
	defer cancel()
	if err := h.svc.SiapkanMJKN(ctx, ss); err != nil {
		return h.gagal(c, err)
	}
	return h.setelahJadwal(c, ss)
}

func (h *Handler) jenis(c *fiber.Ctx) error { return h.render(c, "jenis", nil) }

func (h *Handler) rujukan(c *fiber.Ctx) error {
	jenis := service.JenisFKTP
	if c.Query("jenis") == "4" {
		jenis = service.JenisRS
	}
	ctx, cancel := h.ctx(c, 3*h.cfg.BPJSTimeout)
	defer cancel()
	list, err := h.svc.ListRujukan(ctx, h.sesi(c).sesi, jenis)
	if err != nil {
		return h.gagal(c, err)
	}
	judul := "Rujukan dari Puskesmas / Klinik (FKTP)"
	if jenis == service.JenisRS {
		judul = "Rujukan dari Rumah Sakit Lain"
	}
	return h.render(c, "rujukan", fiber.Map{"List": list, "Judul": judul})
}

func (h *Handler) pilihRujukan(c *fiber.Ctx) error {
	if err := h.svc.PilihRujukan(h.sesi(c).sesi, c.FormValue("no")); err != nil {
		return h.gagal(c, err)
	}
	return h.jadwal(c)
}

func (h *Handler) kontrol(c *fiber.Ctx) error {
	ctx, cancel := h.ctx(c, 3*h.cfg.BPJSTimeout)
	defer cancel()
	list, err := h.svc.ListKontrol(ctx, h.sesi(c).sesi)
	if err != nil {
		return h.gagal(c, err)
	}
	return h.render(c, "kontrol", fiber.Map{"List": list})
}

func (h *Handler) pilihKontrol(c *fiber.Ctx) error {
	ctx, cancel := h.ctx(c, 2*h.cfg.BPJSTimeout)
	defer cancel()
	if err := h.svc.PilihKontrol(ctx, h.sesi(c).sesi, c.FormValue("no")); err != nil {
		return h.gagal(c, err)
	}
	return h.jadwal(c)
}

func (h *Handler) jadwal(c *fiber.Ctx) error {
	ss := h.sesi(c).sesi
	list, err := h.svc.ListJadwal(c.UserContext(), ss)
	if err != nil {
		return h.gagal(c, err)
	}
	kembali := "/jenis"
	if ss.Jenis == service.JenisKontrol {
		kembali = "/kontrol"
	} else if ss.Jenis == service.JenisRS {
		kembali = "/rujukan?jenis=4"
	} else if ss.Jenis == service.JenisFKTP {
		kembali = "/rujukan?jenis=1"
	}
	return h.render(c, "jadwal", fiber.Map{"List": list, "Kembali": kembali})
}

func (h *Handler) pilihJadwal(c *fiber.Ctx) error {
	ss := h.sesi(c).sesi
	if err := h.svc.PilihJadwal(c.UserContext(), ss, c.FormValue("dokter"), c.FormValue("poli"), c.FormValue("jam")); err != nil {
		return h.gagal(c, err)
	}
	return h.setelahJadwal(c, ss)
}

func (h *Handler) setelahJadwal(c *fiber.Ctx, ss *service.Sesi) error {
	ctx, cancel := h.ctx(c, 2*h.cfg.BPJSTimeout)
	defer cancel()
	perlu, status, err := h.svc.PerluBiometrik(ctx, ss)
	if err != nil {
		return h.gagal(c, err)
	}
	if perlu {
		kembali := "/jadwal"
		if ss.MJKN != nil {
			kembali = "/konfirmasi"
		}
		return h.render(c, "biometrik", fiber.Map{"Status": status, "Kembali": kembali})
	}
	return h.render(c, "ringkasan", fiber.Map{"S": ss, "Nama": service.Samarkan(ss.Pasien.Nama)})
}

func (h *Handler) cekBiometrik(c *fiber.Ctx) error {
	ss := h.sesi(c).sesi
	ctx, cancel := h.ctx(c, 2*h.cfg.BPJSTimeout)
	defer cancel()
	perlu, status, err := h.svc.PerluBiometrik(ctx, ss)
	if err != nil {
		return h.gagal(c, err)
	}
	if perlu && h.cfg.FingerprintMode != "off" {
		ss.FPGagal++
		if ss.FPGagal >= 3 {
			return h.pesan(c, "Verifikasi sidik jari / wajah belum berhasil. Silakan ke loket pendaftaran.", false)
		}
		kembali := "/jadwal"
		if ss.MJKN != nil {
			kembali = "/konfirmasi"
		}
		return h.render(c, "biometrik", fiber.Map{"Status": status, "Gagal": ss.FPGagal, "Kembali": kembali})
	}
	return h.render(c, "ringkasan", fiber.Map{"S": ss, "Nama": service.Samarkan(ss.Pasien.Nama)})
}

func (h *Handler) proses(c *fiber.Ctx) error {
	ss := h.sesi(c).sesi
	// Jangan pakai context request: proses SEP + simpan tidak boleh terputus di tengah jalan bila browser reset.
	ctx, cancel := context.WithTimeout(context.Background(), 4*h.cfg.BPJSTimeout)
	defer cancel()
	hasil, err := h.svc.Daftar(ctx, ss)
	if err != nil {
		var ue *service.UserError
		if errors.As(err, &ue) && ue.Biometrik {
			ss.FPGagal++
			if ss.FPGagal >= 3 {
				return h.pesan(c, "Verifikasi sidik jari / wajah belum berhasil. Silakan ke loket pendaftaran.", false)
			}
		}
		return h.gagal(c, err)
	}
	return h.renderSelesai(c, hasil, h.cetak(hasil))
}

// cetakSEP: halaman SEP format Khanza (rptBridgingSEP2), dicetak browser ke printer default PC kiosk.
// Hanya SEP milik sesi ini yang bisa dibuka (tidak bisa ambil SEP lain lewat URL).
func (h *Handler) cetakSEP(c *fiber.Ctx) error {
	e := h.st.get(c.Cookies(cookieSesi))
	if e == nil || e.sesi.Hasil == nil || e.sesi.Hasil.Batal {
		return c.Status(fiber.StatusNotFound).SendString("SEP tidak tersedia")
	}
	data, err := h.svc.DataCetakSEP(c.UserContext(), e.sesi.Hasil.NoSEP)
	if err != nil || data == nil {
		if err != nil {
			h.log.Error("cetak_sep", "no_sep", e.sesi.Hasil.NoSEP, "err", err.Error())
		}
		return c.Status(fiber.StatusNotFound).SendString("SEP tidak tersedia")
	}
	c.Set("Cache-Control", "no-store")
	return c.Render("cetak/sep", fiber.Map{
		"RS": h.rs.Nama, "S": data, "Logo": len(h.logo) > 0,
		// pola report Khanza: dd/MM/yyyy h:mm:ss a
		"Cetak": time.Now().In(h.cfg.Zona).Format("02/01/2006 3:04:05 PM"),
	})
}

func (h *Handler) cetak(hasil *service.Hasil) string {
	data := printer.Karcis(h.rs, hasil)
	if h.cfg.CetakSEP == "escpos" {
		data = append(data, printer.SEP(h.rs, hasil)...)
	}
	if err := h.print.Kirim(data); err != nil {
		h.log.Error("printer", "no_rawat", hasil.NoRawat, "err", err.Error())
		return "Printer bermasalah. Tunjukkan layar ini / sebutkan No. Antrean di loket untuk cetak ulang."
	}
	return ""
}

func (h *Handler) renderSelesai(c *fiber.Ctx, hasil *service.Hasil, errCetak string) error {
	return h.render(c, "selesai", fiber.Map{"H": hasil, "Nama": service.Samarkan(hasil.NamaPasien), "ErrCetak": errCetak,
		"SEPBrowser": h.cfg.CetakSEP == "browser", "T": time.Now().UnixNano()})
}

func (h *Handler) cetakUlang(c *fiber.Ctx) error {
	ss := h.sesi(c).sesi
	if ss.Hasil == nil || ss.Hasil.Batal {
		return h.render(c, "beranda", nil)
	}
	return h.renderSelesai(c, ss.Hasil, h.cetak(ss.Hasil))
}

func (h *Handler) batalDaftar(c *fiber.Ctx) error {
	ss := h.sesi(c).sesi
	ctx, cancel := context.WithTimeout(context.Background(), 3*h.cfg.BPJSTimeout)
	defer cancel()
	if err := h.svc.Batal(ctx, ss); err != nil {
		return h.gagal(c, err)
	}
	h.st.del(c.Cookies(cookieSesi))
	return h.pesan(c, "Pendaftaran Anda sudah dibatalkan.", false)
}

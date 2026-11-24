package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/template/html/v2"

	"apm-khanza/internal/applog"
	"apm-khanza/internal/bpjs"
	"apm-khanza/internal/config"
	apmhttp "apm-khanza/internal/http"
	"apm-khanza/internal/khanza"
	"apm-khanza/internal/printer"
	"apm-khanza/internal/service"
	"apm-khanza/web"
)

func main() {
	cfg, err := config.Load()
	zona := time.FixedZone("WITA", 8*3600)
	if cfg != nil {
		zona = cfg.Zona
	}
	log := applog.New("logs", zona)
	if err != nil {
		log.Error("config", "err", err.Error())
		os.Exit(1)
	}

	db, err := sql.Open("mysql", cfg.DSN())
	if err != nil {
		log.Error("db_open", "err", err.Error())
		os.Exit(1)
	}
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		log.Error("db_ping", "err", err.Error())
		os.Exit(1)
	}
	repo := khanza.New(db)
	if err := repo.CekSkema(ctx, cfg.DBName); err != nil {
		log.Error("skema", "err", err.Error())
		os.Exit(1)
	}
	st, err := repo.Setting(ctx)
	if err != nil {
		log.Error("setting", "err", err.Error())
		os.Exit(1)
	}
	if cfg.KdPPK == "" && st.KodePPK == "" {
		log.Error("setting", "err", "kode PPK kosong: isi KHANZA_KD_PPK atau setting.kode_ppk")
		os.Exit(1)
	}

	client := bpjs.NewClient(bpjs.Options{
		VClaimURL: cfg.VClaimURL, AntreanURL: cfg.AntreanURL, ConsID: cfg.ConsID, Secret: cfg.Secret,
		AntreanConsID: cfg.AntreanConsID, AntreanSecret: cfg.AntreanSecret,
		UserKeyVClaim: cfg.UserKeyVClaim, UserKeyAntrean: cfg.UserKeyAntrean, Timeout: cfg.BPJSTimeout,
	}, log)
	svc := service.New(cfg, repo, client, log, st)

	engine := html.NewFileSystem(http.FS(web.Templates()), ".html")
	app := fiber.New(fiber.Config{
		Views:                 engine,
		DisableStartupMessage: true,
		ReadTimeout:           2 * time.Minute,
		WriteTimeout:          2 * time.Minute,
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			var fe *fiber.Error
			if errors.As(err, &fe) && fe.Code == fiber.StatusNotFound {
				return c.Status(404).SendString("Halaman tidak ditemukan")
			}
			log.Error("http", "path", c.Path(), "err", err.Error())
			return c.Status(500).SendString(`<div class="kartu text-center text-3xl">Terjadi gangguan sistem. Silakan ke loket pendaftaran.</div>`)
		},
	})
	app.Use(recover.New())
	app.Use("/static", filesystem.New(filesystem.Config{Root: http.FS(web.Static()), MaxAge: 3600}))
	app.Use(func(c *fiber.Ctx) error {
		if !strings.HasPrefix(c.Path(), "/static") {
			log.Info("req", "method", c.Method(), "path", c.Path(), "ip", c.IP())
		}
		return c.Next()
	})
	apmhttp.New(cfg, svc, log, &printer.Printer{Mode: cfg.PrinterMode, Addr: cfg.PrinterAddr}).Routes(app)

	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		<-sig
		log.Info("shutdown")
		_ = app.ShutdownWithTimeout(90 * time.Second) // biarkan proses SEP yang berjalan selesai
	}()

	log.Info("start", "port", cfg.AppPort, "kiosk", cfg.KioskID, "rs", st.NamaInstansi,
		"urut_noreg", cfg.UrutNoReg, "base_noreg", cfg.BaseNoReg, "kirim_antrean", cfg.KirimAntrean)
	if err := app.Listen(":" + cfg.AppPort); err != nil {
		log.Error("listen", "err", err.Error())
		os.Exit(1)
	}
}

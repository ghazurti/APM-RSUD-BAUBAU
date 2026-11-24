package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/gofiber/fiber/v2"
)

var agentClient = &http.Client{Timeout: 45 * time.Second}

// bukaBiometrik meminta apmagent.exe di PC kiosk membuka aplikasi Sidik Jari / Frista BPJS
// dan mengetik No. Kartu pasien sesi ini. No. Kartu tidak pernah dikirim ke browser.
func (h *Handler) bukaBiometrik(c *fiber.Ctx) error {
	app := c.Query("app")
	if h.cfg.AgentToken == "" || (app != "fingerprint" && app != "frista") ||
		(app == "fingerprint" && !h.cfg.AgentFingerprint) || (app == "frista" && !h.cfg.AgentFrista) {
		return c.SendStatus(fiber.StatusNotFound)
	}
	ss := h.sesi(c).sesi
	if ss.Peserta == nil {
		return h.render(c, "agentstatus", fiber.Map{"Gagal": true, "Msg": "Data peserta tidak ditemukan. Silakan mulai lagi."})
	}

	url := h.cfg.AgentURL
	if url == "" {
		url = "http://" + net.JoinHostPort(c.IP(), h.cfg.AgentPort)
	}
	body, _ := json.Marshal(map[string]string{"App": app, "NoKartu": ss.Peserta.NoKartu, "NIK": ss.Peserta.NIK})
	ctx, cancel := context.WithTimeout(c.Context(), 45*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url+"/buka", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Agent-Token", h.cfg.AgentToken)

	nama := map[string]string{"fingerprint": "Sidik Jari", "frista": "Frista (wajah)"}[app]
	resp, err := agentClient.Do(req)
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			var e struct{ Error string }
			_ = json.NewDecoder(resp.Body).Decode(&e)
			err = fmt.Errorf("agent %d: %s", resp.StatusCode, e.Error)
		}
	}
	if err != nil {
		h.log.Error("agent", "app", app, "url", url, "err", err.Error())
		return h.render(c, "agentstatus", fiber.Map{"Gagal": true,
			"Msg": "Aplikasi " + nama + " tidak bisa dibuka otomatis. Silakan buka manual atau minta bantuan petugas."})
	}
	// Tahan hitung mundur idle selama pasien memakai aplikasi BPJS (sentuhan tidak sampai ke browser).
	c.Set("HX-Trigger", fmt.Sprintf(`{"tahanIdle":%d}`, h.cfg.BiometrikTahan))
	return h.render(c, "agentstatus", fiber.Map{"Msg": "Aplikasi " + nama +
		" sudah dibuka. Ikuti petunjuk di aplikasi, tutup aplikasinya, lalu tekan \"Saya Sudah Verifikasi\"."})
}

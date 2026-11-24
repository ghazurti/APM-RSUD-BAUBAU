package bpjs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// FlexString menerima nilai JSON string maupun angka (BPJS kadang mengirim code sebagai angka).
type FlexString string

func (f *FlexString) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	switch {
	case s == "null":
		*f = ""
	case strings.HasPrefix(s, `"`):
		var v string
		if err := json.Unmarshal(b, &v); err != nil {
			return err
		}
		*f = FlexString(v)
	default:
		*f = FlexString(s)
	}
	return nil
}

func (f FlexString) String() string { return string(f) }

type Meta struct {
	Code    FlexString `json:"code"`
	Message string     `json:"message"`
}

// Error dari BPJS (metaData.code bukan 200). Message aman ditampilkan ke pasien.
type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string { return fmt.Sprintf("BPJS %s: %s", e.Code, e.Message) }

// IsNotFound: BPJS memakai code 201 untuk "data tidak ditemukan" pada banyak endpoint.
func IsNotFound(err error) bool {
	var be *Error
	if errors.As(err, &be) {
		m := strings.ToLower(be.Message)
		return be.Code == "201" && (strings.Contains(m, "tidak ditemukan") || strings.Contains(m, "tidak ada") || strings.Contains(m, "kosong"))
	}
	return false
}

// ErrJaringan: request tidak mendapat jawaban pasti (timeout / koneksi putus).
// Untuk POST insert SEP status SEP menjadi tidak pasti.
type ErrJaringan struct{ Err error }

func (e *ErrJaringan) Error() string { return "koneksi BPJS: " + e.Err.Error() }
func (e *ErrJaringan) Unwrap() error { return e.Err }

type service struct {
	name    string
	baseURL string
	consID  string
	secret  string
	userKey string
	json    bool // Antrean: application/json; VClaim: Application/x-www-form-urlencoded
}

type Client struct {
	http    *http.Client
	log     *slog.Logger
	vclaim  service
	antrean service
}

type Options struct {
	VClaimURL, AntreanURL                 string
	ConsID, Secret                        string
	AntreanConsID, AntreanSecret          string
	UserKeyVClaim, UserKeyAntrean         string
	Timeout                               time.Duration
}

func NewClient(o Options, log *slog.Logger) *Client {
	return &Client{
		http: &http.Client{Timeout: o.Timeout},
		log:  log,
		vclaim: service{name: "vclaim", baseURL: o.VClaimURL, consID: o.ConsID, secret: o.Secret,
			userKey: o.UserKeyVClaim},
		antrean: service{name: "antrean", baseURL: o.AntreanURL, consID: o.AntreanConsID, secret: o.AntreanSecret,
			userKey: o.UserKeyAntrean, json: true},
	}
}

type envelope struct {
	MetaData *Meta          `json:"metaData"`
	Metadata *Meta          `json:"metadata"`
	Response json.RawMessage `json:"response"`
}

// call mengirim request dan mengembalikan JSON response yang sudah didekripsi (bisa nil).
// GET di-retry maks 2x; POST/PUT/DELETE TIDAK pernah di-retry otomatis.
func (c *Client) call(ctx context.Context, s service, method, path string, body any) (json.RawMessage, error) {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return nil, err
		}
	}
	maxCoba := 1
	if method == http.MethodGet {
		maxCoba = 3
	}
	var lastErr error
	for coba := 1; coba <= maxCoba; coba++ {
		res, retryable, err := c.once(ctx, s, method, path, payload)
		if err == nil || !retryable || coba == maxCoba || ctx.Err() != nil {
			return res, err
		}
		lastErr = err
		time.Sleep(time.Duration(coba) * 700 * time.Millisecond)
	}
	return nil, lastErr
}

func (c *Client) once(ctx context.Context, s service, method, path string, payload []byte) (json.RawMessage, bool, error) {
	mulai := time.Now()
	url := s.baseURL + "/" + strings.TrimLeft(path, "/")
	ts := Timestamp(time.Now())

	var rdr io.Reader
	if payload != nil {
		rdr = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rdr)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("X-cons-id", s.consID)
	req.Header.Set("X-timestamp", ts)
	req.Header.Set("X-signature", Signature(s.consID, s.secret, ts))
	req.Header.Set("user_key", s.userKey)
	if s.json {
		req.Header.Set("Content-Type", "application/json")
	} else {
		req.Header.Set("Content-Type", "Application/x-www-form-urlencoded")
	}

	logAttrs := []any{"svc", s.name, "method", method, "path", path}
	if payload != nil {
		logAttrs = append(logAttrs, "request", json.RawMessage(payload))
	}

	resp, err := c.http.Do(req)
	if err != nil {
		c.log.Error("bpjs_error", append(logAttrs, "err", err.Error(), "ms", time.Since(mulai).Milliseconds())...)
		return nil, true, &ErrJaringan{Err: err}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, true, &ErrJaringan{Err: err}
	}
	if resp.StatusCode >= 500 {
		c.log.Error("bpjs_http", append(logAttrs, "status", resp.StatusCode, "body", potong(string(raw), 500))...)
		return nil, true, &ErrJaringan{Err: fmt.Errorf("HTTP %d", resp.StatusCode)}
	}

	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		c.log.Error("bpjs_parse", append(logAttrs, "status", resp.StatusCode, "body", potong(string(raw), 500))...)
		return nil, false, fmt.Errorf("response BPJS tidak valid (HTTP %d)", resp.StatusCode)
	}
	meta := env.MetaData
	if meta == nil {
		meta = env.Metadata
	}
	if meta == nil {
		return nil, false, fmt.Errorf("response BPJS tanpa metadata (HTTP %d)", resp.StatusCode)
	}

	// Cek metadata dulu sebelum decrypt.
	var hasil json.RawMessage
	if meta.Code == "200" && len(env.Response) > 0 && string(env.Response) != "null" {
		var enc string
		if json.Unmarshal(env.Response, &enc) == nil {
			if enc != "" {
				plain, err := Decrypt(s.consID, s.secret, ts, enc)
				if err != nil {
					c.log.Error("bpjs_decrypt", append(logAttrs, "err", err.Error())...)
					return nil, false, fmt.Errorf("gagal membuka response BPJS: %w", err)
				}
				hasil = json.RawMessage(plain)
			}
		} else {
			hasil = env.Response // response tidak terenkripsi (objek langsung)
		}
	}

	logAttrs = append(logAttrs, "code", meta.Code.String(), "message", meta.Message, "ms", time.Since(mulai).Milliseconds())
	if hasil != nil {
		logAttrs = append(logAttrs, "response", potong(string(hasil), 4000))
	}
	c.log.Info("bpjs", logAttrs...)

	if meta.Code != "200" {
		return nil, false, &Error{Code: meta.Code.String(), Message: meta.Message}
	}
	return hasil, false, nil
}

func potong(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

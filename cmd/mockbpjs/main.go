// mockbpjs: server BPJS tiruan untuk uji UI / latihan tanpa internet. JANGAN dipakai di produksi.
// Response dienkripsi persis seperti BPJS (AES-256-CBC + LZString) memakai MOCK_CONS_ID/MOCK_SECRET.
//
//	MOCK_CONS_ID=9999 MOCK_SECRET=rahasiauji go run ./cmd/mockbpjs   (listen :9099)
//	lalu di .env APM: BPJS_VCLAIM_URL=http://127.0.0.1:9099/vclaim  BPJS_ANTREAN_URL=http://127.0.0.1:9099/antrean
package main

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	lzstring "github.com/daku10/go-lz-string"

	"apm-khanza/internal/bpjs"
)

var consID, secret = env("MOCK_CONS_ID", "9999"), env("MOCK_SECRET", "rahasiauji")
var nomorSEP atomic.Int64

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func enc(ts, plain string) string {
	comp, _ := lzstring.CompressToEncodedURIComponent(plain)
	key := sha256.Sum256([]byte(consID + secret + ts))
	block, _ := aes.NewCipher(key[:])
	p := []byte(comp)
	pad := aes.BlockSize - len(p)%aes.BlockSize
	p = append(p, bytes.Repeat([]byte{byte(pad)}, pad)...)
	out := make([]byte, len(p))
	cipher.NewCBCEncrypter(block, key[:16]).CryptBlocks(out, p)
	return base64.StdEncoding.EncodeToString(out)
}

func main() {
	nomorSEP.Store(time.Now().Unix() % 100000)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		ts := r.Header.Get("X-timestamp")
		body, _ := io.ReadAll(r.Body)
		log.Printf("%s %s %s", r.Method, r.URL.Path, body)
		if r.Header.Get("X-signature") != bpjs.Signature(consID, secret, ts) {
			w.Write([]byte(`{"metaData":{"code":"401","message":"Signature tidak valid"}}`))
			return
		}
		ok := func(meta, resp string) {
			if resp == "" {
				fmt.Fprintf(w, `{"%s":{"code":"200","message":"Ok"},"response":null}`, meta)
				return
			}
			fmt.Fprintf(w, `{"%s":{"code":"200","message":"OK"},"response":%q}`, meta, enc(ts, resp))
		}
		p := r.URL.Path
		seg := strings.Split(p, "/")
		switch {
		case strings.HasPrefix(p, "/vclaim/Peserta/"):
			nomor := seg[4]
			noka, nik := nomor, env("MOCK_NIK", "7472010101900001")
			if len(nomor) == 16 {
				noka, nik = env("MOCK_NOKA", "0001234567890"), nomor
			}
			ok("metaData", fmt.Sprintf(`{"peserta":{"noKartu":"%s","nik":"%s","nama":"PESERTA UJI","sex":"L","tglLahir":"1990-01-15",
				"mr":{"noMR":null,"noTelepon":"081234567890"},"statusPeserta":{"kode":"0","keterangan":"AKTIF"},
				"provUmum":{"kdProvider":"0001","nmProvider":"PUSKESMAS WAJO"},"jenisPeserta":{"kode":"1","keterangan":"PNS DAERAH"},
				"hakKelas":{"kode":"1","keterangan":"KELAS I"},"informasi":{"prolanisPRB":null}}}`, noka, nik))
		case strings.HasPrefix(p, "/vclaim/Rujukan/"):
			ok("metaData", fmt.Sprintf(`{"rujukan":[
				{"noKunjungan":"0001R0011125B000001","tglKunjungan":"%s","provPerujuk":{"kode":"0001R001","nama":"PUSKESMAS WAJO"},
				 "poliRujukan":{"kode":"%s","nama":"%s"},"diagnosa":{"kode":"J06.9","nama":"Acute upper respiratory infection"}},
				{"noKunjungan":"0001R0010725B000099","tglKunjungan":"%s","provPerujuk":{"kode":"0001R001","nama":"PUSKESMAS WAJO"},
				 "poliRujukan":{"kode":"%s","nama":"%s"},"diagnosa":{"kode":"A09","nama":"Diarrhoea"}}]}`,
				time.Now().AddDate(0, 0, -3).Format("2006-01-02"), env("MOCK_POLI", "ANA"), env("MOCK_NMPOLI", "ANAK"),
				time.Now().AddDate(0, 0, -120).Format("2006-01-02"), env("MOCK_POLI", "ANA"), env("MOCK_NMPOLI", "ANAK")))
		case strings.HasPrefix(p, "/vclaim/RencanaKontrol/"):
			w.Write([]byte(`{"metaData":{"code":"201","message":"Data tidak ditemukan"},"response":null}`))
		case strings.HasPrefix(p, "/vclaim/SEP/FingerPrint/"):
			ok("metaData", `{"kode":"1","status":"Peserta sudah melakukan verifikasi"}`)
		case p == "/vclaim/SEP/2.0/insert":
			ok("metaData", fmt.Sprintf(`{"sep":{"noSep":"1906R0011125V%06d"}}`, nomorSEP.Add(1)))
		case p == "/vclaim/SEP/2.0/delete":
			ok("metaData", `"1906R0011125V000001"`)
		case strings.HasPrefix(p, "/antrean/"):
			ok("metadata", "")
		default:
			w.Write([]byte(`{"metaData":{"code":"201","message":"Data tidak ditemukan"},"response":null}`))
		}
	})
	log.Println("mock BPJS di :9099")
	log.Fatal(http.ListenAndServe(env("MOCK_ADDR", ":9099"), nil))
}

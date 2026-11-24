// Package bpjs berisi bridging VClaim 2.0 dan Antrean RS (Mobile JKN).
package bpjs

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"time"

	lzstring "github.com/daku10/go-lz-string"
)

// Timestamp = detik UTC sejak epoch, dalam bentuk string.
func Timestamp(t time.Time) string {
	return strconv.FormatInt(t.UTC().Unix(), 10)
}

// Signature = base64(HMAC-SHA256(consId + "&" + timestamp, secretKey)).
func Signature(consID, secret, ts string) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(consID + "&" + ts))
	return base64.StdEncoding.EncodeToString(m.Sum(nil))
}

// Decrypt membuka response terenkripsi BPJS.
// key = SHA256(consId + secretKey + timestamp), iv = key[0:16], AES-256-CBC + PKCS7,
// lalu LZString.decompressFromEncodedURIComponent.
// timestamp WAJIB sama dengan header X-timestamp request yang bersangkutan.
func Decrypt(consID, secret, ts, data string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return "", fmt.Errorf("base64: %w", err)
	}
	key := sha256.Sum256([]byte(consID + secret + ts))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", err
	}
	if len(raw) == 0 || len(raw)%aes.BlockSize != 0 {
		return "", errors.New("panjang ciphertext tidak valid")
	}
	plain := make([]byte, len(raw))
	cipher.NewCBCDecrypter(block, key[:aes.BlockSize]).CryptBlocks(plain, raw)
	plain, err = unpadPKCS7(plain)
	if err != nil {
		return "", err
	}
	out, err := lzstring.DecompressFromEncodedURIComponent(string(plain))
	if err != nil {
		return "", fmt.Errorf("lz-string: %w", err)
	}
	if out == "" {
		return "", errors.New("hasil dekompresi kosong (kunci/timestamp salah?)")
	}
	return out, nil
}

func unpadPKCS7(b []byte) ([]byte, error) {
	n := int(b[len(b)-1])
	if n == 0 || n > aes.BlockSize || n > len(b) {
		return nil, errors.New("padding PKCS7 tidak valid")
	}
	if !bytes.Equal(b[len(b)-n:], bytes.Repeat([]byte{byte(n)}, n)) {
		return nil, errors.New("padding PKCS7 tidak valid")
	}
	return b[:len(b)-n], nil
}

// encryptForTest kebalikan Decrypt; dipakai unit test untuk membuat sampel.
func encryptForTest(consID, secret, ts, plain string) (string, error) {
	comp, err := lzstring.CompressToEncodedURIComponent(plain)
	if err != nil {
		return "", err
	}
	key := sha256.Sum256([]byte(consID + secret + ts))
	block, _ := aes.NewCipher(key[:])
	p := []byte(comp)
	pad := aes.BlockSize - len(p)%aes.BlockSize
	p = append(p, bytes.Repeat([]byte{byte(pad)}, pad)...)
	out := make([]byte, len(p))
	cipher.NewCBCEncrypter(block, key[:aes.BlockSize]).CryptBlocks(out, p)
	return base64.StdEncoding.EncodeToString(out), nil
}

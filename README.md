# APM BPJS — SIMRS Khanza (RSUD Kota Baubau)

Anjungan Pendaftaran Mandiri untuk **pasien BPJS lama**. Hasilnya sama seperti petugas mendaftarkan lewat
`DlgReg` + Bridging SEP (`BPJSDataSEP`): registrasi muncul di DlgReg, SEP di Data SEP, task ID di modul antrean.

Stack: Go + Fiber + HTMX 2 + Alpine 3 + Tailwind. Semua aset (JS/CSS/template) tertanam di dalam `apm.exe`, jadi **tidak ada CDN**.

## Alur

Beranda → No. Kartu/NIK (keypad) → konfirmasi peserta → jenis kunjungan (FKTP / Kontrol / antar RS) →
rujukan atau surat kontrol → poli & dokter (jadwal hari ini, sudah termaping, sisa kuota) → biometrik (bila wajib) →
ringkasan → **buat SEP → simpan ke Khanza (1 transaksi) → kirim antrean + Task 3** → karcis + SEP tercetak.

Ditolak dan diarahkan ke loket: pasien baru, peserta tidak aktif, rujukan > 90 hari, kuota habis atau belum diatur,
sudah terdaftar di poli yang sama hari ini, sudah ada SEP poli tersebut hari ini, dan tagihan belum di-closing
(bila `set_validasi_registrasi.wajib_closing_kasir='Yes'`).

## Aturan Khanza yang diikuti (dari source asli)

| Hal | Sumber | Perilaku |
|---|---|---|
| no_rawat | `DlgReg.isNumber` | `yyyy/MM/dd/` + 6 digit dari `MAX(RIGHT(no_rawat,6))` per tanggal |
| no_reg | `DlgReg.isNumber` | `URUTNOREG` (dokter / poli / dokter + poli) dan `BASENOREG=booking` (ambil MAX dari `booking_registrasi` juga) |
| stts_daftar | `DlgReg.isCekPasien` | `Baru` bila `pasien.tgl_daftar` = tanggal registrasi, selain itu `Lama` |
| biaya_reg | `DlgReg.isCekPasien` | `registrasi` (Baru) / `registrasilama` (Lama) |
| almt_pj | `DlgReg.isCekPasien` | `alamat, kelurahan, kecamatan, kabupaten` |
| status_poli | `DlgReg.isRegistrasi` | `Lama` bila pernah ada reg_periksa di poli ini |
| umur | `DlgReg.isCekPasien` + `UpdateUmur` | SQL yang sama persis; `pasien.umur` diupdate setelah registrasi |
| bridging_sep | `BPJSDataSEP.insertSEP` | 52 kolom dengan nilai enum Khanza (`1. Faskes 1`, `0. Tidak`, `tglpulang=0000-00-00 00:00:00`) |
| rujuk_masuk, bpjs_prb | `BPJSDataSEP.insertSEP` | no_balasan `BR/yyyy/MM/dd/nnnn`; PRB disimpan bila ada |
| antrean/add | `BPJSDataSEP.SimpanAntrianOnSite` | kodebooking = no_rawat, nomorantrean = no_reg, estimasi = jam_mulai + no_reg × 5 menit; kalau ditolak dengan No. Rujukan, dikirim ulang pakai No. SKDP |
| task id | `KhanzaHMSServiceMobileJKN` | baris `referensi_mobilejkn_bpjs_taskid` hanya disimpan bila `updatewaktu` berhasil |

Pasien onsite **tidak** dimasukkan ke `referensi_mobilejkn_bpjs`, karena Khanza juga tidak melakukannya untuk SEP onsite.

Tidak ada tabel, kolom, trigger, atau view baru. Saat start, aplikasi memeriksa `information_schema` dan **menolak jalan**
bila ada kolom yang dipakai tidak ditemukan.

## Keamanan transaksi

- Insert SEP **tidak pernah di-retry otomatis**. Bila koneksi putus saat insert, histori pelayanan dicek dan hasilnya dicatat di log, lalu pasien diarahkan ke loket.
- Gagal simpan ke Khanza setelah SEP terbit → transaksi di-rollback dan SEP dihapus lewat `SEP/2.0/delete`. Bila penghapusan juga gagal, No. SEP ditampilkan untuk petugas.
- Bentrok nomor dengan kiosk lain diserialisasi dengan `GET_LOCK` (lock sesi MySQL, tidak membuat objek DB). Bentrok dengan loket Khanza ditangani seperti DlgReg: generate → insert → bila duplikat, generate ulang (maks 5x) dengan isolasi READ COMMITTED.
- Gagal kirim antrean tidak membatalkan registrasi; kejadian dicatat di log.
- Pasien bisa **Batalkan** dari layar selesai: antrean/batal + Task 99, SEP dihapus, `reg_periksa.stts='Batal'`.

## Konfigurasi

Salin `.env.example` ke `.env` di folder yang sama dengan `apm.exe`. **Samakan `KHANZA_URUT_NOREG` dan `KHANZA_BASE_NOREG` dengan
`setting/database.xml` Khanza** (RS ini: `dokter + poli` dan `booking`). Kredensial BPJS hanya ada di `.env` server.

Log: `logs/apm-YYYY-MM-DD.log` (JSON lines: request/response BPJS tanpa secret, error, no_rawat/no_sep yang dibuat).

## Build

```sh
./build.sh            # Tailwind → web/static/app.css, unit test, lalu dist/apm.exe (Windows amd64)
```

Manual: `go build -ldflags "-s -w" -o apm.exe ./cmd/apm`

## Pasang sebagai service Windows (NSSM)

```bat
nssm install APM-Khanza C:\apm\apm.exe
nssm set APM-Khanza AppDirectory C:\apm
nssm set APM-Khanza AppStopMethodConsole 90000
nssm start APM-Khanza
```

`AppDirectory` wajib diisi supaya `.env` dan folder `logs\` terbaca.

## PC kiosk

Shortcut Chrome:

```
chrome.exe --kiosk --kiosk-printing --noerrdialogs --disable-pinch --overscroll-history-navigation=0 http://<server>:8088
```

**Cetak SEP** (`PRINTER_SEP=browser`, default): SEP dicetak dengan **format yang sama dengan cetak SEP Khanza**
(`report/rptBridgingSEP2.jrxml`: kertas 595×390 pt, Tahoma, logo dari `gambar.bpjs`, data dari `bridging_sep`).
Pencetakan dilakukan oleh Chrome ke **printer default Windows PC kiosk**; dengan `--kiosk-printing` langsung tercetak tanpa dialog.
Atur ukuran kertas printer SEP di driver Windows sesuai kertas yang dipakai Khanza.
Halaman `/cetak/sep` hanya menampilkan SEP milik sesi pasien yang sedang aktif.

Karcis antrean dicetak ke printer thermal: `PRINTER_MODE=tcp` (printer LAN port 9100) atau `windows-share` (`PRINTER_ADDR=\\PC-KIOSK01\EPSON-TM`;
akun service harus punya akses ke share tersebut). Verifikasi sidik jari/Frista dilakukan pasien di aplikasi BPJS
yang terpasang di PC kiosk, lalu menekan "Saya Sudah Verifikasi". Browser tidak bisa membuka aplikasi lokal.

## User database produksi

Jangan pakai `root`. APM hanya perlu baca semua tabel `sik` dan menulis ke tabel berikut
(daftar diambil dari query di `internal/khanza` dan `internal/service`):

```sql
CREATE USER 'apm'@'<IP server APM>' IDENTIFIED BY '<password kuat>';
GRANT SELECT ON sik.* TO 'apm'@'<IP server APM>';
GRANT INSERT, DELETE ON sik.reg_periksa TO 'apm'@'<IP server APM>';
GRANT INSERT, DELETE ON sik.bridging_sep TO 'apm'@'<IP server APM>';
GRANT INSERT, DELETE ON sik.rujuk_masuk TO 'apm'@'<IP server APM>';
GRANT INSERT, DELETE ON sik.bpjs_prb TO 'apm'@'<IP server APM>';
GRANT INSERT, DELETE ON sik.referensi_mobilejkn_bpjs_taskid TO 'apm'@'<IP server APM>';
GRANT UPDATE ON sik.referensi_mobilejkn_bpjs TO 'apm'@'<IP server APM>';
GRANT UPDATE ON sik.reg_periksa TO 'apm'@'<IP server APM>';
GRANT UPDATE ON sik.pasien TO 'apm'@'<IP server APM>';
```

Bila trigger di `sik` menulis ke tabel lain, tambahkan hak untuk tabel itu (pesan error MySQL menyebut nama tabelnya).

## Tombol "Buka Sidik Jari / Frista" (apmagent.exe)

Browser tidak bisa membuka aplikasi lokal, jadi PC kiosk menjalankan `apmagent.exe`. Saat pasien menekan tombol di layar
biometrik, server APM memanggil `http://<IP PC kiosk>:8099/buka` (header `X-Agent-Token`). Agent menjalankan exe aplikasi BPJS,
membawa jendelanya ke depan, lalu mengetik langkah dari `agent.env` (pola sama dengan `DlgReg.BukaFingerPrint` Khanza).
No. Kartu dikirim server → agent, tidak lewat browser. Bila jendela aplikasi tidak bisa difokuskan, agent **tidak mengetik**
(supaya input tidak masuk ke Chrome). Hitung mundur idle APM ditahan `BIOMETRIK_TAHAN_DETIK` sampai pasien menekan tombol berikutnya.

Pasang di PC kiosk:

1. Salin `dist\apmagent.exe` dan `agent.env.example` → `C:\apm-agent\`, ganti nama menjadi `agent.env`, isi path exe, akun, dan langkah.
2. Jalankan **di sesi user yang login** (bukan NSSM/service — service Windows tidak bisa menampilkan jendela):
   Task Scheduler → *At log on* → `C:\apm-agent\apmagent.exe`, *Start in* `C:\apm-agent`.
3. Firewall PC kiosk: izinkan TCP 8099 masuk **hanya dari IP server APM**; isi juga `AGENT_IZIN_IP`.
4. Di `.env` server: `KIOSK_AGENT_TOKEN` = `AGENT_TOKEN` agent. Bila APM dan Chrome di PC yang sama, agent dipanggil lewat `127.0.0.1`.

Sesuaikan `*_LANGKAH` dengan layar aplikasi versi RS (urutan Frista hanya contoh). Log: `C:\apm-agent\logs\apmagent.log` (No. Kartu disamarkan).

## Pengujian

```sh
go test ./internal/...                     # unit test kripto (vektor dari OpenSSL + lz-string JS)

# integrasi: DB SALINAN Khanza (bukan sik!) + server BPJS tiruan
APM_TEST_DSN='root:@tcp(127.0.0.1:3306)/apm_uji?parseTime=true&loc=Asia%2FMakassar' \
APM_TEST_NOKA=0001234567890 go test -tags integration ./internal/service/ -v

# uji UI tanpa internet: server BPJS tiruan di :9099
go run ./cmd/mockbpjs
```

Urutan go-live: DB staging + BPJS **development** (`apijkn-dev…`) → cek data di DlgReg/Data SEP/Task ID Khanza → baru produksi.

## Acceptance checklist

- [ ] Pasien BPJS lama bisa daftar sampai cetak karcis tanpa petugas
- [ ] Registrasi muncul di DlgReg dengan no_rawat/no_reg berurutan (uji bersamaan dengan loket)
- [ ] SEP muncul di Data SEP, bisa dicetak & dihapus dari Khanza
- [ ] Task 3 tercatat; Khanza bisa lanjut Task 4–7
- [ ] Tidak ada tabel/kolom baru di `sik`
- [ ] Kasus penolakan diarahkan ke loket
- [ ] Gagal simpan DB setelah SEP terbit → SEP terhapus
- [ ] Tidak ada secret BPJS di log maupun di browser

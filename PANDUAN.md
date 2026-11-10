# Panduan Lengkap: dari Instal sampai Berjalan

[🇬🇧 English](GUIDE.md) · 🇮🇩 Bahasa Indonesia · [README](README.md)

Panduan ini membawa Anda dari nol sampai AI Gateway berjalan dan bisa dipakai.

## 1. Yang dibutuhkan

- **Go 1.22 atau lebih baru.** Cek dengan `go version`.
- Minimal **satu API key** dari penyedia AI (ada yang gratis, lihat langkah 3).
- Terminal: Terminal (macOS/Linux), PowerShell (Windows), atau terminal Firebase Studio.

## 2. Instal Go

| Sistem | Cara |
|---|---|
| Windows / macOS | Unduh dari https://go.dev/dl lalu jalankan installer. Buka terminal baru. |
| Linux (Debian/Ubuntu) | Unduh tarball dari https://go.dev/dl, lalu `sudo rm -rf /usr/local/go && sudo tar -C /usr/local -xzf go*.tar.gz` dan tambahkan `export PATH=$PATH:/usr/local/go/bin` ke `~/.bashrc`. |
| Nix / NixOS | `nix-shell -p go` (pilih paket `go`, bukan `gccgo` atau `go_1_21`). |
| Firebase Studio | Di `.idx/dev.nix` tambahkan `pkgs.go` pada `packages`, lalu *Rebuild environment*. |

Cek hasilnya:

```bash
go version      # harus go1.22 atau lebih tinggi
```

Jika muncul `C compiler "gcc" not found`, jalankan sekali: `go env -w CGO_ENABLED=0` (proyek ini murni Go, tidak butuh gcc).

## 3. Dapatkan API key (pilih satu atau lebih)

| Penyedia | Biaya | Dapatkan di |
|---|---|---|
| Gemini | Gratis (ada batas) | https://aistudio.google.com/apikey |
| Groq | Gratis (ada batas) | https://console.groq.com/keys |
| OpenRouter | Gratis untuk model `:free` | https://openrouter.ai/keys |
| Mistral | Gratis (ada batas) | https://console.mistral.ai |
| Claude / GPT | Berbayar | Opsional, dipakai sebagai cadangan terakhir |

Tanpa key sekalipun, Custom AI (gambar ASCII, prediksi dari deret harga yang Anda kirim, jawaban dari data latih) tetap jalan. Untuk menjawab pertanyaan umum Anda butuh minimal satu key.

## 4. Ambil kode

```bash
# dari zip
unzip ai-gateway.zip && cd ai-gateway

# atau dari GitHub
git clone https://github.com/USERNAME/REPO.git && cd REPO
```

Pastikan Anda berada di folder yang berisi `go.mod`.

## 5. Isi konfigurasi (`.env`)

```bash
cp .env.example .env
```

Buka `.env` dengan editor, lalu isi. Contoh:

```
GATEWAY_KEY=kata-sandi-panjang-buatan-anda
GEMINI_API_KEYS=key_gemini_anda
GROQ_API_KEYS=key_groq_anda
```

Aturan penulisan (sering jadi sumber error):
- **Tanpa spasi** di sekitar `=`. Benar: `GROQ_API_KEYS=gsk_xxx`. Salah: `GROQ_API_KEYS= gsk_xxx`.
- **Tanpa komentar** di baris yang sama dengan key.
- **Hanya key-nya.** Jangan tulis kata `Bearer` di depan key.
- Beberapa key untuk satu penyedia dipisah koma: `GEMINI_API_KEYS=key1,key2`.
- `GATEWAY_KEY` adalah kata sandi untuk API ini (karang sendiri). Siapa pun yang tahu nilainya bisa memakai API Anda.
- **Jangan pernah** menaruh key di `.env.example` atau meng-commit `.env` ke GitHub.

## 6. Jalankan

```bash
go run ./cmd/server
```

Tanda berhasil: muncul `AI gateway aktif di :8080` (atau `127.0.0.1:8080`). Baris `peringatan: provider "..." dilewati` hanya berarti penyedia itu belum punya key, dan itu normal.
Biarkan terminal ini terbuka. Untuk menghentikan, tekan `Ctrl+C`.

Port lain: `PORT=9000 go run ./cmd/server` (Windows PowerShell: `$env:PORT=9000; go run ./cmd/server`).

## 7. Coba (di terminal kedua)

Ambil `GATEWAY_KEY` ke variabel supaya tidak perlu diketik ulang:

```bash
# macOS / Linux
export GATEWAY_KEY=kata-sandi-panjang-buatan-anda
```

```powershell
# Windows PowerShell
$env:GATEWAY_KEY="kata-sandi-panjang-buatan-anda"
```

**Cek hidup**

```bash
curl localhost:8080/healthz
# {"status":"ok"}
```

**Tanya AI**

```bash
curl -X POST localhost:8080/v1/chat -H "Authorization: Bearer $GATEWAY_KEY" -d '{"prompt":"Halo, jawab satu kalimat saja"}'
```

Respons berisi `text` (jawaban), `provider` (siapa yang menjawab), dan `failed_attempts` (siapa yang gagal sebelum itu).

**Latih lalu tanya**

```bash
curl -X POST localhost:8080/v1/train -H "Authorization: Bearer $GATEWAY_KEY" \
  -d '{"question":"Jam buka toko?","answer":"Buka 08.00-17.00 setiap hari."}'

curl -X POST localhost:8080/v1/chat -H "Authorization: Bearer $GATEWAY_KEY" \
  -d '{"prompt":"jam buka toko?"}'
```

**Fitur Custom AI**

```bash
# gambar -> seni ASCII (tambahkan "dalam biner" untuk pola 0/1)
curl -X POST localhost:8080/v1/chat -H "Authorization: Bearer $GATEWAY_KEY" -d '{"prompt":"buatkan gambar kucing"}'

# prediksi pasar -> "text" + "secondary" (rumus)
curl -X POST localhost:8080/v1/chat -H "Authorization: Bearer $GATEWAY_KEY" -d '{"prompt":"prediksi harga bitcoin 30 hari"}'
```

**Lihat status key dan provider**

```bash
curl localhost:8080/v1/status -H "Authorization: Bearer $GATEWAY_KEY"
```

Pengguna Windows: di PowerShell, `curl` adalah alias lain. Pakai `curl.exe`, atau:

```powershell
Invoke-RestMethod -Method Post -Uri http://localhost:8080/v1/chat `
  -Headers @{Authorization="Bearer $env:GATEWAY_KEY"} `
  -Body '{"prompt":"Halo"}'
```

## 8. Memakai dari aplikasi lain

API ini kompatibel dengan format OpenAI. Contoh Python (`pip install openai`):

```python
from openai import OpenAI
c = OpenAI(base_url="http://localhost:8080/v1", api_key="GATEWAY_KEY_ANDA")
r = c.chat.completions.create(model="auto", messages=[{"role": "user", "content": "Halo"}])
print(r.choices[0].message.content)
```

`model` boleh `auto`, nama provider (`gemini`), atau `provider/model` (`groq/openai/gpt-oss-20b`).

## 9. Mengubah model atau urutan provider

Nama model sering berubah. Untuk mengubah tanpa menyentuh kode, pakai env di `.env`:

```
GEMINI_MODEL=gemini-3.8-flash
GROQ_MODEL=openai/gpt-oss-20b
OPENROUTER_MODEL=openrouter/free
```

Untuk mengubah urutan, salin `config.example.json` menjadi `config.json` lalu ubah angka `priority` (kecil = dicoba lebih dulu). Lihat model yang boleh dipakai key Groq Anda:

```bash
curl https://api.groq.com/openai/v1/models -H "Authorization: Bearer KEY_GROQ_ANDA"
```

Restart server setelah mengubah `.env` atau `config.json`.

## 10. Jalankan tes

```bash
go vet ./...
go test ./...
```

## 11. Pemecahan masalah

| Gejala | Penyebab dan solusi |
|---|---|
| `401 API key gateway salah/tidak ada` | Header `Authorization: Bearer ...` tidak sama dengan `GATEWAY_KEY` di `.env`. Pastikan variabel shell Anda tidak kosong (`echo ${#GATEWAY_KEY}` harus > 0). |
| `503 semua provider gagal` | Baca daftar `attempts`. Tiap baris menyebut alasan per provider (di bawah). |
| `HTTP 404 model ... not found` / `no longer available` | Nama model sudah tidak ada atau akun Anda tidak punya akses. Ganti lewat `<NAMA>_MODEL` (langkah 9). |
| `HTTP 401` dari satu provider | Key salah/dicabut, atau ada kata `Bearer` / spasi / komentar ikut tertulis di `.env`. |
| `HTTP 429` | Kuota gratis habis atau kena batas. Gateway otomatis pindah; tunggu beberapa menit. |
| `HTTP 503 ... high demand` | Server penyedia sedang ramai (sementara). Gateway otomatis pindah. |
| `tidak ada provider aktif` | Tidak ada key yang terbaca. Cek `.env` ada di folder yang sama tempat Anda menjalankan `go run`. |
| `connection refused` | Server belum jalan. Jalankan `go run ./cmd/server` dan biarkan terbuka. |
| `command not found` saat `source .env` | Ada spasi setelah `=` di `.env`. Perbaiki format (langkah 5). Server Go sendiri tidak butuh `source`. |
| `address already in use` | Port terpakai. Pakai `PORT=8081 go run ./cmd/server`. |
| `gcc not found` | `go env -w CGO_ENABLED=0`. |
| Di Firebase Studio, alamat `cloudworkstations.dev` mengalihkan (302) ke login | Itu normal; alamat itu dijaga login Google. Tes lewat `localhost:8080` di terminal, atau lewat panel Web Preview. |
| Custom AI menjawab "lewati" terus | Normal untuk pertanyaan baru; gateway lanjut ke AI lain lalu mempelajarinya. |

## 12. Keamanan (wajib dibaca)

- Isi `GATEWAY_KEY` sebelum membuka ke internet.
- Jangan tempel isi `.env` atau keluaran terminal yang memuat key ke chat, issue, atau forum. Jika terlanjur, buat ulang key itu di situs penyedianya dan hapus yang lama.
- Jangan meng-commit `.env`. File `.gitignore` sudah mengecualikannya; cek dengan `git status`.
- Jika GitHub menolak push karena "secret", hapus key dari commit (bukan sekadar menambah commit baru) dan ganti key itu.
- Memori Custom AI dipakai bersama semua pengguna gateway; jangan kirim data pribadi.

## 13. Langkah berikutnya

- Deploy ke internet: Vercel atau Netlify (lihat `README.md`, bagian Deploy). Data latih di sana wajib memakai Upstash.
- Detail Custom AI (opsi, batasan, privasi): bagian "Custom AI" di `README.md`.

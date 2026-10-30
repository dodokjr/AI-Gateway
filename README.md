# AI Gateway (Golang) — Claude + GPT + Gemini + lainnya, teks saja

Satu API untuk banyak AI. Kalau token/kuota satu key atau provider habis (429/402),
gateway otomatis pindah ke key/provider berikutnya. Bisa "dilatih" lewat basis
pengetahuan. Hanya teks: gambar/video/audio ditolak (HTTP 400).

Inti gateway tanpa dependensi eksternal (hanya standard library Go); hanya fungsi Netlify yang memakai `aws-lambda-go`.

## Jalankan

```bash
cp .env.example .env        # isi API key (minimal satu)
go run ./cmd/server
# opsional: cp config.example.json config.json untuk mengubah urutan/model provider
```

Provider tanpa key otomatis dilewati. Urutan dicoba sesuai `priority` (kecil dulu).
Bawaan: Gemini > Groq > OpenRouter > Mistral (gratis) > Claude > GPT (berbayar, cadangan).
Untuk 100% gratis tanpa batas, jalankan Ollama lokal lalu set `"disabled": false` pada provider `ollama`.

## Cara failover

- Banyak key per provider: `GEMINI_API_KEYS=key1,key2,key3` (diputar bergantian).
- 429 / 402 (kuota/saldo habis) -> key masuk cooldown (1 menit, atau 1 jam bila kuota harian), pindah key/provider lain.
- 401/403 -> key diistirahatkan 6 jam. 5xx / timeout -> 30 detik.
- Cek kondisi: `GET /v1/status`.

## Endpoint

Semua (kecuali `/healthz`) butuh header `Authorization: Bearer $GATEWAY_KEY`.

| Method | Path | Fungsi |
|---|---|---|
| POST | `/v1/chat` | Chat sederhana (`prompt` atau `messages`) |
| POST | `/v1/chat/completions` | Format kompatibel OpenAI (bisa dipakai SDK OpenAI) |
| POST | `/v1/train` | Tambah pengetahuan (`question`,`answer`) atau `items:[...]` |
| GET | `/v1/train` | Daftar pengetahuan (`?q=` untuk cari) |
| DELETE | `/v1/train/{id}` | Hapus pengetahuan |
| POST | `/v1/feedback` | Rating `1`/`-1` pada pengetahuan (`id`), atau simpan jawaban bagus |
| GET/PUT | `/v1/persona` | Atur kepribadian/instruksi tetap |
| GET | `/v1/status` | Status provider & key |

### Contoh

```bash
# Latih
curl -X POST localhost:8080/v1/train -H "Authorization: Bearer $GATEWAY_KEY" \
  -d '{"question":"Jam buka toko?","answer":"Buka 08.00-17.00 setiap hari."}'

# Tanya (pengetahuan relevan otomatis disisipkan)
curl -X POST localhost:8080/v1/chat -H "Authorization: Bearer $GATEWAY_KEY" \
  -d '{"prompt":"Toko buka jam berapa?"}'

# Pilih provider tertentu (tetap failover bila gagal)
curl -X POST localhost:8080/v1/chat -H "Authorization: Bearer $GATEWAY_KEY" \
  -d '{"prompt":"Halo","provider":"claude"}'
```

Pada `/v1/chat/completions`, isi `model` dengan `auto`, nama provider (`gemini`),
atau `provider/model` (`groq/llama-3.3-70b-versatile`).

## Soal "melatih"

Ini **bukan** fine-tuning bobot model (itu tidak bisa gratis lintas provider).
Gateway menyimpan contoh tanya-jawab di `data/knowledge.json`, mencari 3 yang paling
relevan per pertanyaan, dan menyisipkannya ke prompt. Hasilnya berlaku di semua
provider, dan makin pintar seiring data dan feedback bertambah.


## Deploy ke Vercel

1. Upload folder ini ke GitHub, lalu *Add New Project* di Vercel dan pilih repo-nya (atau `vercel` via CLI).
2. Di *Settings > Environment Variables* isi: `GATEWAY_KEY`, API key provider (mis. `GEMINI_API_KEYS`, `GROQ_API_KEYS`), dan Upstash (`UPSTASH_REDIS_REST_URL`, `UPSTASH_REDIS_REST_TOKEN`).
3. Deploy. Tes: `curl https://NAMA.vercel.app/healthz`.

`vercel.json` sudah mengarahkan `/v1/*` ke fungsi `api/index.go` (maxDuration 60 dtk).

## Deploy ke Netlify

1. Sekali saja di komputer Anda, buat `go.sum` fungsi: `cd netlify/functions/api && go mod tidy`, lalu commit.
2. Upload ke GitHub, *Add new site > Import from Git* di Netlify.
3. Isi *Site configuration > Environment variables* seperti di Vercel.
4. Deploy. Tes: `curl https://NAMA.netlify.app/healthz`.

Batas waktu fungsi Netlify bawaan 10 dtk (bisa dinaikkan sampai 26 dtk di pengaturan situs). Gateway otomatis memakai batas 8 dtk per percobaan dan 24 dtk total di Netlify; bila Anda tidak menaikkan timeout, set `GATEWAY_TIMEOUT_SEC=4` dan `GATEWAY_TOTAL_TIMEOUT_SEC=9`.

## Penting di serverless

- **Data training wajib pakai Upstash.** Disk Vercel/Netlify sementara; tanpa Upstash, data training hilang saat instance didaur ulang (`GET /v1/status` menampilkan `"storage": "ephemeral"` atau `"upstash"`).
- Status cooldown key disimpan per instance (memori), jadi tiap instance bisa sesekali mencoba key yang sudah habis dulu. Aman, hanya menambah satu percobaan gagal.
- Upstash gratis memuat sekitar 900 KB data training.
- Variabel lain: `CONFIG_JSON` (isi config.json penuh, menimpa bawaan).

## Catatan

- Kuota gratis & nama model berubah sewaktu-waktu; sesuaikan di `config.json`.
- Isi `GATEWAY_KEY` sebelum membuka ke internet. Jangan commit `.env`.
- `stream` belum didukung.
- Tes: `go test ./...`
# AI-Gateway

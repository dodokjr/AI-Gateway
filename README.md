# AI Gateway (Golang) — Claude + GPT + Gemini + lainnya, teks saja

[🇬🇧 English](README.en.md) · 🇮🇩 Bahasa Indonesia

Panduan lengkap dari instal sampai jalan: [PANDUAN.md](PANDUAN.md)

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
Bawaan: Custom AI > Gemini > Groq > OpenRouter > Mistral (gratis) > Claude > GPT (berbayar, cadangan).
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
atau `provider/model` (`groq/openai/gpt-oss-20b`).

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

## Custom AI (mesin lokal, prioritas 1)

Provider bertipe `custom` (`gateway/custom/`) dicoba lebih dulu. Ia **bukan model bahasa generatif**: jawabannya
berasal dari memori terlatih, knowledge base (`/v1/train`), hasil pencarian web, dan rumus. Bila keyakinannya di bawah
`threshold` (bawaan 0,8) ia "melewatkan" permintaan, gateway pindah ke AI lain (tanpa cooldown untuk Custom AI),
membawa konteks pencarian web ke AI itu, lalu **jawaban AI itu dipelajari** Custom AI untuk pertanyaan berikutnya.

| File | Isi |
|---|---|
| `custom_provider.go` | Engine, ambang keyakinan, dual-jawaban, `Declined` (pemicu failover) |
| `math_engine.go` | TF-IDF, Cosine, Levenshtein, Softmax, EMA, RSI, GBM, Monte Carlo, Black-Scholes |
| `market_data.go` | Data harga: Stooq (saham/indeks/forex/komoditas) dan CoinGecko (crypto) |
| `web_search.go` | DuckDuckGo (tanpa kunci), Google Programmable Search, Brave Search |
| `learning.go` | Memori belajar mandiri, TTL, filter data pribadi/waktu-nyata |
| `weather.go` | Prakiraan cuaca seluruh dunia, hari ini + hingga 15 hari (Open-Meteo, gratis, tanpa kunci) |
| `weather_i18n.go` | Terjemahan laporan cuaca: id, en, es, fr, de, pt, ja, zh, ko, ru (tabel bawaan, offline) |
| `code_logic.go`, `code_snippets.go` | Logika pemrograman: perapi + pemeriksa sintaks Go/JSON, tabel kebenaran/tautologi/ekuivalensi, 8 algoritma terverifikasi (Go, Python, JavaScript) |
| `guard.go` | Keamanan: penyaring rahasia, karakter tersembunyi, injeksi instruksi |
| `selfheal.go` | Perbaikan-diri: uji diri, pemulihan memori dari cadangan, pembersihan, circuit breaker |
| `ascii_art.go` | Permintaan gambar -> seni ASCII monokrom atau pola biner 0/1 |

Contoh:

```bash
# prediksi pasar -> "text" (jawaban utama) + "secondary" (rincian rumus)
curl -sX POST localhost:8080/v1/chat -H "Authorization: Bearer $GATEWAY_KEY" -d '{"prompt":"prediksi harga bitcoin 30 hari"}'
# cuaca hari ini + 7 hari ke depan (data Open-Meteo, tanpa kunci)
curl -sX POST localhost:8080/v1/chat -H "Authorization: Bearer $GATEWAY_KEY" -d '{"prompt":"cuaca di Bandung hari ini dan 7 hari ke depan"}'
# kota mana pun di dunia + bahasa lain (kota senama: "Paris, Texas")
curl -sX POST localhost:8080/v1/chat -H "Authorization: Bearer $GATEWAY_KEY" -d '{"prompt":"cuaca di Tokyo 7 hari dalam bahasa Jepang"}'
# gambar -> ASCII / biner
curl -sX POST localhost:8080/v1/chat -H "Authorization: Bearer $GATEWAY_KEY" -d '{"prompt":"buatkan gambar kucing dalam biner"}'
# beri umpan balik pada jawaban dari memori (id ada di meta.memory_id); -1 dua kali = tidak dipakai lagi
curl -sX POST localhost:8080/v1/feedback -H "Authorization: Bearer $GATEWAY_KEY" -d '{"id":"ID","rating":-1}'
```

Opsi di `config.json` -> provider `custom` -> `options`: `threshold`, `web_search`, `search_engines`
(`duckduckgo`, `google`, `brave`), `max_results`, `simulations`, `learn_from_peers`, `peer_ttl_days`, `market`, `weather`, `default_city`.

Batasan penting:
- **Kecocokan memori berbasis kata, sengaja ketat.** Ia tidak bisa membedakan "jam buka" dari "jam tutup" atau
  "Warung Biru" dari "Warung Merah", jadi hanya pertanyaan yang (hampir) sama yang dijawab dari memori. Menurunkan
  `threshold` menambah jawaban keliru.
- **Memori dipakai bersama semua pengguna gateway.** Pertanyaan berisi email, nomor panjang, atau rahasia, serta data
  waktu-nyata (hari ini, terbaru, harga), tidak dipelajari. Tetap hindari mengirim data pribadi.
- **Privasi:** pertanyaan yang tidak ada di memori dikirim ke mesin pencari (DuckDuckGo secara bawaan). Setel `"web_search": false` untuk mematikan.
- Pastikan syarat layanan penyedia AI yang Anda pakai mengizinkan menyimpan jawabannya; matikan dengan `"learn_from_peers": false`.
- Scraping halaman Google/Bing tidak dilakukan (melanggar ketentuan, cepat diblokir). Bing Search API sudah dihentikan; gunakan Brave.
- **Terjemahan cuaca** memakai tabel bawaan (gratis, tanpa layanan luar) untuk 10 bahasa: sebut "dalam bahasa Jepang" / "in French", atau tulis pertanyaan dengan kata cuaca bahasa itu (météo, clima, Wetter, 天気, 天气, 날씨, погода). Tanpa itu, bahasa mengikuti pertanyaan (Indonesia/Inggris). Bahasa lain belum didukung; pesan galat tetap berbahasa Indonesia.
- **Cuaca** berasal dari Open-Meteo (Custom AI tidak menghitung cuaca sendiri); nama kota dikirim ke layanan itu. Akurasi menurun untuk hari yang jauh. Tanpa kota di pertanyaan dipakai `default_city` (bawaan Jakarta); kota yang disebut eksplisit dan tak ditemukan tidak diganti diam-diam. Matikan dengan `"weather": false`.
- Prediksi pasar adalah proyeksi statistik dari data historis, bukan nasihat keuangan.
- Pada serverless, memori belajar butuh Upstash agar bertahan (sama seperti knowledge base).

## Logika pemrograman (Custom AI)

```bash
# algoritma terverifikasi (Go bawaan; sebut python / javascript)
-d '{"prompt":"buatkan fungsi fibonacci dalam python"}'    # fibonacci, faktorial, prima, gcd, binary search, merge sort, palindrom, fizzbuzz
# rapikan + periksa kode Go (gofmt, impor tak terpakai) atau JSON
-d '{"prompt":"rapikan kode go ini:\n```go\nfunc main(){}\n```"}'
# logika boolean: tabel kebenaran, tautologi, ekuivalensi (De Morgan)
-d '{"prompt":"apakah !(A && B) ekuivalen dengan !A || !B"}'
```

Kode di pustaka dijalankan di Go, Python, dan JavaScript saat pengujian (`CODE_RUN=1 go test ./gateway/custom`) dan keluarannya identik.
Kode pengguna **tidak pernah dijalankan** di server; hanya diformat dan diperiksa sintaksnya. Permintaan di luar kemampuan ini
(mis. "buatkan fungsi login") diteruskan ke AI lain. Operator boolean: `! ~ ¬ not`, `&& & ∧ and`, `|| | ∨ or`, `^ ⊕ xor`, `-> → implies`, `<-> ↔ iff` (maks 6 variabel).

## Keamanan

- Autentikasi `GATEWAY_KEY` dengan perbandingan waktu-konstan; **10 kegagalan dalam 5 menit mengunci IP itu 5 menit**.
- Pembatas laju per IP: `RATE_LIMIT_PER_MIN` (bawaan 60, `0` = mati). Di serverless batas ini per instance.
- **Gagal-tertutup:** di Vercel/Netlify, bila `GATEWAY_KEY` kosong semua permintaan (kecuali `/healthz`) ditolak 503. Setel `ALLOW_NO_AUTH=1` hanya bila sengaja.
- Header pengaman (nosniff, DENY frame, CSP, no-referrer, no-store), batas badan 1 MB, `TRUST_PROXY=1` bila di belakang proxy tepercaya (otomatis di serverless).
- Teks dari web/AI lain dibersihkan: karakter tersembunyi/bidi dibuang, baris injeksi instruksi dibuang, kunci/token disamarkan. Pesan >20.000 karakter dilewatkan ke AI lain.
- Bahasa pemrograman: seluruh sistem tetap Go (aman memori, pustaka kripto bawaan, satu biner, bisa jalan di Vercel/Netlify). Mencampur bahasa lain berarti proses terpisah dan tidak bisa di-deploy ke fungsi serverless Go.

## Perbaikan-diri

- `GET /v1/selfcheck`: uji jawaban-diketahui (matematika, logika, pustaka kode, terjemahan, penyaring keamanan, memori). `POST /v1/selfheal`: bersihkan memori + uji ulang.
- Memori yang rusak dikarantina (`data/custom_memory.json.corrupt`) dan dipulihkan dari cadangan (`.bak`, atau kunci `:bak` di Upstash). Pembersihan: kedaluwarsa, dinilai buruk, duplikat, entri tak valid, dan entri AI lain yang kini tak lolos penyaring.
- Circuit breaker: 3 galat internal/panic beruntun -> Custom AI dilewati 60 detik (AI lain mengambil alih), lalu dicoba lagi.
- Server lokal menjalankan pemeliharaan saat start dan tiap 6 jam.
- **Tidak ada pembaruan kode otomatis.** Custom AI tidak mengubah kodenya sendiri dan tidak mengunduh/menjalankan kode dari internet (celah rantai pasok dan eksekusi kode jarak jauh). Memperbarui kode = Anda deploy ulang. Yang "memperbarui diri" adalah pengetahuannya (belajar dari pengguna dan AI lain).

## Catatan

- Kuota gratis & nama model berubah sewaktu-waktu; sesuaikan di `config.json`.
- Isi `GATEWAY_KEY` sebelum membuka ke internet. Jangan commit `.env`.
- `stream` belum didukung.
- Tes: `go test ./...`
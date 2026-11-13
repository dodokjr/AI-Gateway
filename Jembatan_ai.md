# Jembatan ke banyak AI + Mode Dewan AI

Terakhir diperbarui: 2026-11-12.

## 1. Apa yang dimaksud "semua AI di dunia"

Tidak ada cara menyambung ke **semua** AI di dunia secara harfiah: tiap penyedia punya API sendiri, kunci sendiri, dan model
yang berganti-ganti. Yang bisa dilakukan, dan sudah dilakukan di sini:

| Jalur | Cakupan |
|---|---|
| Adapter bawaan | OpenAI (GPT), Anthropic (Claude), Google (Gemini) |
| `openai_compat` | **Semua** layanan yang meniru API OpenAI: Groq, OpenRouter, Mistral, Together, DeepSeek, xAI, Perplexity, Fireworks, Cerebras, SambaNova, NVIDIA NIM, Hugging Face, Cohere (mode kompatibel), Moonshot, Qwen, GitHub Models, dan lainnya |
| Model lokal | Ollama, LM Studio, llama.cpp, vLLM (semua menyediakan API OpenAI-compatible di `localhost`) |
| **OpenRouter** | Satu key membuka ratusan model dari banyak penyedia sekaligus; cara tercepat untuk "banyak AI" |

Yang **tidak** dilakukan:
- Tidak menyambung ke antarmuka web chat (situs ChatGPT, Claude.ai, dan sejenisnya) lewat otomasi/scraping: melanggar ketentuan layanan dan cepat diblokir.
- Penyedia yang tidak punya API kompatibel perlu adapter baru (bagian kode `provider.go`). Katakan penyedianya, nanti saya buatkan.

## 2. Menambah AI baru (3 langkah)

1. Buka `config.providers.example.json`, **salin hanya entri yang Anda perlukan** ke `providers` di `config.json`.
2. Isi key di `.env`, mis. `TOGETHER_API_KEYS=key1,key2` (boleh banyak key dipisah koma; diputar otomatis).
3. Isi nama model asli dari dashboard penyedia, lewat `config.json` atau env `<NAMA>_MODEL` (mis. `TOGETHER_MODEL=...`).

Provider tanpa key otomatis dilewati. Model lokal bawaannya `disabled: true`; ubah ke `false` setelah server lokalnya jalan.

Catatan jujur: entri di `config.providers.example.json` **belum diuji ke layanan aslinya**. URL diambil dari dokumentasi
publik masing-masing penyedia. Bila 404, cek ulang URL dan nama model. Nama model sengaja `ISI_NAMA_MODEL` karena berubah cepat.

## 3. Mode Dewan AI (`POST /v1/council`)

Alur:
1. **Custom AI menjawab lebih dulu** (dari memori, pustaka kode, cuaca, pasar, dan seterusnya). Ia tidak melempar ke AI lain; bila ia
   tidak tahu, bagian `custom_declined` terisi.
2. **AI lain ikut menjawab** secara mandiri, paralel (bawaan 3, maks 5 peninjau). Yang gagal digantikan cadangan.
3. Tiap AI juga **menilai jawaban Custom AI**: `BENAR` / `SEBAGIAN` / `SALAH` + alasan.
4. Hasil **dirangkum**: `didukung`, `ditolak`, `campuran`, `custom-tidak-menjawab`, dan seterusnya, plus tingkat kesepakatan antar-AI.

```bash
curl -sX POST localhost:8080/v1/council -H "Authorization: Bearer $GATEWAY_KEY" \
  -d '{"prompt":"Apa ibu kota Prancis?","reviewers":3,"providers":["gemini","groq","deepseek"]}'
```
`reviewers` dan `providers` opsional. Bentuk respons (diringkas):
```json
{
  "question": "Apa ibu kota Prancis?",
  "custom": {"text": "...", "meta": {"source": "memory:peer:groq", "memory_id": "ab12..", "trust": "konsensus"}},
  "opinions": [
    {"provider": "groq", "answer": "...", "verdict": "BENAR", "reason": "...", "similarity_to_custom": 0.71},
    {"provider": "gemini", "error": "HTTP 503 ..."}
  ],
  "summary": {"verdict": "didukung", "counts": {"BENAR": 2}, "responded": 2,
              "agreement_between_reviewers": 0.68, "note": "...", "suggestion": "..."}
}
```

### Hubungannya dengan pembelajaran
- Jawaban AI lain yang masuk lewat dewan memakai **jalur belajar biasa**: dua AI berbeda yang sepakat naik menjadi `konsensus`. Lihat `docs/METODE_PEMBELAJARAN.md`.
- Hasil penilaian **tidak mengubah memori otomatis**. Bila dewan menolak atau mendukung jawaban dari memori, kolom `suggestion` memberi perintah umpan balik (`+1` atau `-1`) yang **Anda** putuskan untuk dijalankan. Alasannya: peninjau AI bisa salah, dan memori dipakai bersama semua pengguna.

### Batas dan risiko
- **Biaya dan kuota:** satu permintaan dewan = 1 panggilan per peninjau (hingga 5). Pakai untuk pertanyaan penting saja. Pembatas laju per IP tetap berlaku.
- **Privasi:** pertanyaan Anda **dan jawaban Custom AI** dikirim ke setiap peninjau (penyedia pihak ketiga). Jangan dipakai untuk data pribadi atau rahasia.
- **Peninjau bisa keliru atau saling sepakat dalam kesalahan.** `didukung` berarti mayoritas peninjau setuju, bukan bukti kebenaran.
- Peninjau yang tidak mengikuti format `JAWABAN / PENILAIAN / ALASAN` ditandai `TIDAK_TERBACA`; jawabannya tetap ditampilkan.
- Jawaban Custom AI ditempatkan di dalam pembatas `<jawaban_custom>` sebagai **data tak tepercaya** (anti-injeksi), dan jawaban peninjau disanitasi (karakter tersembunyi, baris injeksi, kunci) sebelum ditampilkan.
- Percakapan multi-giliran: Custom AI biasanya melewatkan; peninjau tetap menjawab dengan konteks penuh.
- Batas waktu mengikuti `GATEWAY_TIMEOUT_SEC` (per panggilan) dan `GATEWAY_TOTAL_TIMEOUT_SEC` (total). Di Vercel/Netlify, naikkan batas fungsi bila memakai banyak peninjau.

## 4. Catatan keputusan

| Tanggal | Keputusan | Alasan |
|---|---|---|
| 2026-11-12 | Penilaian dan jawaban peninjau diminta dalam **satu panggilan** | Setengah biaya dibanding dua tahap; risikonya peninjau terpengaruh jawaban Custom AI, dikurangi dengan instruksi "jawab sendiri dulu" |
| 2026-11-12 | Umpan balik memori hanya **disarankan**, tidak otomatis | Peninjau AI bisa salah; memori dipakai bersama |
| 2026-11-12 | Preset provider dipisah di `config.providers.example.json`, bukan di config bawaan | Belum diuji ke layanan asli; mencegah 13 peringatan "tidak ada key" tiap start |
| 2026-11-12 | Tidak menyambung ke web chat konsumen | Melanggar ketentuan layanan |

# Metode Pembelajaran Custom AI

Catatan bersama: apa yang dipakai Custom AI untuk "belajar", bagaimana memakainya, apa yang sengaja tidak dipakai, dan
riwayat keputusannya. Terakhir diperbarui: 2026-11-12.

## 1. Apa arti "belajar" di sini

Custom AI **bukan model bahasa**. Ia tidak mengubah bobot apa pun. "Belajar" berarti **menambah dan memperbaiki memori
pertanyaan-jawaban**, lalu mencocokkannya lagi (TF-IDF, koreksi salah ketik, ambang 0,8). Karena itu kualitasnya sama dengan
kualitas isi memori. Metode di bawah ini fokus pada satu hal: **menjaga isi memori tetap benar**.

## 2. Metode yang dipakai

| # | Metode (nama umum) | Status | Cara kerja di Custom AI | Kode |
|---|---|---|---|---|
| 1 | Pencarian memori (retrieval / RAG ringan) | ada | TF-IDF + kemiripan kosinus + koreksi typo; ambang 0,8; di bawahnya diserahkan ke AI lain | `math_engine.go`, `learning.go` |
| 2 | Pelatihan oleh manusia (supervised, few-shot) | ada | `POST /v1/train`; jawaban pengguna **otoritatif**, permanen, tidak bisa ditimpa AI lain | `learning.go` |
| 3 | Umpan balik manusia (RLHF versi ringan) | ditingkatkan | `POST /v1/feedback`: `+1` pada jawaban AI lain = **terverifikasi** dan permanen; `-1` dua kali = tidak dipakai lagi | `learning.go` |
| 4 | Distilasi dari AI lain (guru-murid) | ada | Bila Custom AI melewatkan dan AI lain menjawab, jawabannya dipelajari (satu giliran saja, lolos penyaring) | `custom_adapter.go` |
| 5 | **Konsensus antar-AI (ensemble / self-consistency)** | baru | Bila **dua AI berbeda** memberi jawaban yang saling setuju (kemiripan kata ≥ 0,5), pengetahuan naik jadi `konsensus` | `learning.go` |
| 6 | **Active learning (uncertainty sampling)** | baru | `GET /v1/learn/review`: antrean jawaban AI lain yang menunggu persetujuan, **yang paling tidak pasti dulu** | `learning.go` |
| 7 | **Retensi adaptif (mirip spaced repetition / kurva lupa)** | baru | Belum diverifikasi = 30 hari; konsensus = 90 hari; terverifikasi dan pengguna = permanen | `learning.go` |
| 8 | Pembersihan berkala | ada | `POST /v1/selfheal`: buang kedaluwarsa, dinilai buruk, duplikat, tidak aman | `selfheal.go` |
| 10 | **Log pelatihan (semua tanya-jawab)** | baru | Setiap pasangan pertanyaan-jawaban (Custom AI, AI lain, dan mode dewan) dicatat dengan data pribadi/rahasia disamarkan; bisa dilihat, diekspor JSONL, dan diputar ulang ke memori | `trainlog.go` |
| 9 | Konteks web ke AI lain | ada | Hasil pencarian web diteruskan sebagai petunjuk tak tepercaya (bukan perintah) | `custom_provider.go`, `guard.go` |

## 2b. Log pelatihan: semua pertanyaan dan jawaban disimpan

Mulai sekarang **setiap** tanya-jawab yang lewat gateway dicatat (nyala secara bawaan): jawaban Custom AI, jawaban AI lain,
dan semua jawaban di mode dewan (dengan penilaian peninjau). Log ini **bahan mentah**, bukan memori aktif:

| | Log pelatihan | Memori aktif Custom AI |
|---|---|---|
| Isi | semua tanya-jawab (data pribadi/rahasia disamarkan) | hanya yang lolos penyaring dan jenjang kepercayaan |
| Dipakai untuk menjawab? | **tidak** | ya |
| Kegunaan | audit, ekspor untuk melatih model lain, memutar ulang ke memori | menjawab pertanyaan |

Alasan dipisah: bila semua jawaban langsung jadi memori aktif, jawaban keliru atau data pribadi satu orang akan disajikan ke
semua pengguna. Jalur dari log ke memori adalah **replay**, yang tetap melewati penyaring, jenjang kepercayaan, dan konsensus.

```bash
# lihat 50 catatan terbaru (terbaru dulu) + jumlah total + galat penulisan terakhir
curl -s "localhost:8080/v1/training/log?limit=50" -H "Authorization: Bearer $GATEWAY_KEY"

# ekspor JSONL (format percakapan {"messages":[user,assistant]}) untuk melatih/fine-tune model lain
curl -s "localhost:8080/v1/training/export?limit=1000&exclude_custom=1&provider=groq,gemini" -H "Authorization: Bearer $GATEWAY_KEY" > latihan.jsonl

# putar ulang log ke memori Custom AI (penyaring + konsensus tetap berlaku)
curl -sX POST localhost:8080/v1/training/replay -H "Authorization: Bearer $GATEWAY_KEY" -d '{"limit":1000}'

# hapus seluruh log
curl -sX DELETE localhost:8080/v1/training/log -H "Authorization: Bearer $GATEWAY_KEY"
```

Yang dicatat per baris: waktu, pertanyaan, jawaban, provider, model, `via` (`chat` atau `council`), `source` dan `trust`
(untuk jawaban Custom AI), `single_turn`, dan `verdict` (penilaian peninjau di mode dewan).

Batas dan hal yang perlu diketahui:
- **Disamarkan, bukan disimpan apa adanya:** email, nomor panjang (11 digit atau lebih: telepon, KTP, kartu), dan kunci/token diganti penanda. Angka biasa (jam, tanggal, harga) dibiarkan. Penyamaran berbasis pola, jadi tidak menjamin semua data pribadi tertangkap; jangan memasukkan data sensitif.
- **Ukuran:** pertanyaan dipotong 2000 karakter, jawaban 8000. Berkas lokal diputar di 8 MB (menyisakan dua generasi). Upstash menyimpan 1000 catatan terakhir. Di Vercel/Netlify tanpa Upstash, log hilang saat instance didaur ulang.
- **Dipakai bersama semua pengguna gateway.** Matikan dengan `LOG_QA=0` di `.env`; hapus dengan perintah di atas.
- **Hanya pasangan satu-giliran** yang diekspor dan diputar ulang; percakapan multi-giliran tetap tercatat tetapi dilewati.
- **Ekspor untuk melatih model lain:** kualitasnya setara jawaban AI sumbernya (bisa salah). Banyak penyedia AI **melarang memakai keluaran mereka untuk melatih model lain**; periksa syarat layanan tiap penyedia sebelum fine-tuning. Saring dulu dengan `exclude_custom=1` dan `provider=...`.
- Berkas lokal ada di `data/training_log.jsonl` (sudah tercakup `.gitignore` lewat `data/`).

## 3. Jenjang kepercayaan

```
AI lain menjawab           AI berbeda setuju          Anda setujui (+1)
belum-diverifikasi  ───►   konsensus (90 hari)  ───►  terverifikasi (permanen)
   (30 hari)                                              ▲
                                                          │ Anda melatih langsung
                                                       pengguna (permanen, otoritatif)
```

Aturan yang dijaga kode:
- Jawaban **pengguna** tidak pernah ditimpa AI lain. Pengguna yang melatih ulang menimpa apa pun.
- Jawaban **terverifikasi** tidak ditimpa. Bila AI lain menjawab berbeda, hanya `conflicts` bertambah.
- Bila AI berbeda menjawab **tidak setuju** pada pengetahuan yang belum diverifikasi, jawaban lama dipertahankan dan `conflicts` bertambah (sinyal untuk ditinjau).
- Bila AI **yang sama** mengubah jawabannya, jawaban diganti, konsensus direset, dan `conflicts` bertambah (tanda ia tidak stabil).
- `-1` pada jawaban terverifikasi mencabut status verifikasinya.
- Setiap jawaban dari memori membawa `meta.trust` (`pengguna`, `terverifikasi`, `konsensus`, `belum-diverifikasi`) dan `meta.memory_id`.

## 4. Alur kerja yang disarankan (5 menit seminggu)

```bash
# 1. Lihat antrean tinjauan (yang paling meragukan di atas) + statistik kepercayaan
curl -s "localhost:8080/v1/learn/review?limit=20" -H "Authorization: Bearer $GATEWAY_KEY"

# 2. Jawaban benar -> setujui (jadi permanen)
curl -sX POST localhost:8080/v1/feedback -H "Authorization: Bearer $GATEWAY_KEY" -d '{"id":"ID","rating":1}'

# 3. Jawaban salah -> tolak dua kali (tidak dipakai lagi dan keluar antrean)
curl -sX POST localhost:8080/v1/feedback -H "Authorization: Bearer $GATEWAY_KEY" -d '{"id":"ID","rating":-1}'

# 4. Jawaban hampir benar -> latih versi yang benar (otoritatif, menimpa)
curl -sX POST localhost:8080/v1/train -H "Authorization: Bearer $GATEWAY_KEY" \
  -d '{"question":"PERTANYAAN PERSIS","answer":"JAWABAN YANG BENAR"}'
```

Untuk kode pemrograman: **jalankan dulu kodenya**, baru setujui atau latih. Custom AI mengembalikan isi memori apa adanya.

## 5. Yang sengaja TIDAK dipakai (dan alasannya)

| Metode | Alasan |
|---|---|
| Fine-tuning bobot model | Butuh GPU, data besar, dan biaya; tidak bisa dilakukan gratis lintas provider. Custom AI memang bukan model |
| Embedding saraf (pencarian makna) | Butuh model atau API tambahan. Kandidat pengembangan (lihat bagian 6) bila Anda mau menambah satu dependensi |
| Mengubah kode sendiri / mengunduh pembaruan | Celah rantai pasok dan eksekusi kode jarak jauh. Pembaruan kode = Anda deploy ulang |
| Belajar bebas dari web | Risiko peracunan data. Web hanya jadi petunjuk untuk AI lain, tidak pernah langsung jadi pengetahuan permanen |
| Belajar dari percakapan multi-giliran | Jawabannya bergantung konteks; hasilnya sering salah bila dicabut dari konteks |

## 6. Kandidat berikutnya (belum dikerjakan)

1. **BM25** menggantikan TF-IDF murni: biasanya lebih baik untuk jawaban panjang. Perlu uji ulang ambang 0,8.
2. **Ambang adaptif**: naikkan ambang bila banyak umpan balik `-1` pada jawaban memori.
3. **Penghitung pemakaian** (berapa kali setiap pengetahuan dijawab) untuk retensi berbasis pemakaian. Belum dibuat karena setiap penghitungan berarti satu penulisan ke penyimpanan (mahal di Upstash).
4. **Kamus sinonim** buatan pengguna ("buka" = "mulai", "toko" = "warung") untuk menaikkan skor parafrase dengan aman.
5. **Embedding opsional** untuk parafrase. Perlu satu dependensi baru dan keputusan privasi (teks dikirim ke layanan embedding).

## 7. Cara mengukur apakah belajarnya berhasil

Saya **belum mengukur** efektivitasnya pada data Anda. Urutan metode di atas berdasarkan praktik umum (manusia di dalam
lingkaran, konsensus antar-model, retensi menurut kepercayaan), bukan hasil uji pada gateway ini. Ukur sendiri:
- **Porsi jawaban Custom AI**: persentase respons dengan `"provider":"custom"` dibanding total. Naik = memori makin berguna.
- **Panjang antrean tinjauan** dan rasio `terverifikasi` per `belum-diverifikasi` pada `GET /v1/learn/review` (`stats`).
- **Konflik**: item dengan `conflicts > 0` adalah topik yang AI-nya tidak sepakat; tinjau lebih dulu.
- **Umpan balik `-1`** pada jawaban dengan `meta.source` berawalan `memory:`: bila sering, naikkan `threshold` atau latih ulang.

## 8. Batas dan keamanan

- Pertanyaan berisi email, nomor panjang, atau rahasia, serta data waktu-nyata (hari ini, terbaru, harga), tidak dipelajari otomatis. Permintaan cuaca, pasar, gambar, dan kode pustaka juga tidak.
- Pertanyaan maks 500 karakter, jawaban maks 4000 karakter. Memori maks 2000 pengetahuan; yang terbawah (skor rendah, terlama) dibuang.
- Memori dipakai **bersama semua pengguna gateway**. Jangan melatih dengan data pribadi.
- Di Vercel/Netlify, memori butuh Upstash agar bertahan. Cadangan otomatis ada di kunci `:bak`.
- Pastikan syarat layanan penyedia AI mengizinkan menyimpan jawabannya. Matikan dengan `"learn_from_peers": false`.

## 9. Catatan keputusan

| Tanggal | Keputusan | Alasan |
|---|---|---|
| 2026-11-12 | Jawaban AI lain **tidak lagi menimpa** pengetahuan yang sama bila isinya berbeda | Sebelumnya jawaban terbaru selalu menang, sehingga satu jawaban keliru bisa menggantikan yang benar |
| 2026-11-12 | Tiga tingkat kepercayaan + antrean tinjauan | Memori dipakai bersama; manusia perlu tahu mana yang belum diperiksa |
| 2026-11-12 | Konsensus = 2 AI berbeda dengan kemiripan kata ≥ 0,5 | Sederhana, deterministik, tanpa model tambahan. Dua AI yang sepakat tetap bisa sama-sama salah, jadi konsensus **belum** berarti benar |
| 2026-11-12 | Konsensus hanya memperpanjang retensi (90 hari), tidak membuat permanen | Hanya persetujuan manusia yang membuat permanen |
| 2026-11-12 | Tanpa pembaruan kode otomatis | Lihat bagian 5 |
| 2026-11-13 | Semua tanya-jawab dicatat ke **log terpisah**, bukan langsung ke memori aktif | Permintaan: simpan semuanya untuk melatih. Log menyimpan semuanya (disamarkan); memori aktif tetap disaring supaya satu jawaban keliru atau data pribadi tidak disajikan ke semua pengguna |
| 2026-11-13 | Pencatatan nyala secara bawaan, dengan penyamaran data pribadi dan `LOG_QA=0` untuk mematikan | Sesuai permintaan, tetapi pengguna gateway perlu tahu pertanyaannya tersimpan |
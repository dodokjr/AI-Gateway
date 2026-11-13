package custom

import (
	"context"
	"strings"
	"testing"
	"time"
)

const (
	qFr    = "Apa ibu kota Prancis?"
	ansFrA = "Ibu kota Prancis adalah Paris, kota besar di Eropa."
	ansFrB = "Paris adalah ibu kota Prancis, kota besar di Eropa."
)

func TestTrustLadder(t *testing.T) {
	l := NewLearner(&memStore{}, 100, 24*time.Hour)
	it, ok, _ := l.Learn(qFr, ansFrA, "peer:groq")
	if !ok || it.Trust() != "belum-diverifikasi" || len(it.Peers) != 1 {
		t.Fatalf("awal: %+v", it)
	}
	first := it.Expires
	// AI yang sama mengulang -> bukan konsensus
	it, _, _ = l.Learn(qFr, ansFrA, "peer:groq")
	if it.Trust() != "belum-diverifikasi" {
		t.Fatalf("AI yang sama bukan konsensus: %s", it.Trust())
	}
	// AI berbeda setuju -> konsensus + retensi lebih panjang (3x TTL)
	it, ok, _ = l.Learn(qFr, ansFrB, "peer:gemini")
	if !ok || it.Trust() != "konsensus" || len(it.Peers) != 2 {
		t.Fatalf("konsensus: %+v", it)
	}
	if !it.Expires.After(first.Add(40 * time.Hour)) {
		t.Fatalf("konsensus harus disimpan ~3x lebih lama: %v vs %v", it.Expires, first)
	}
	// jawaban memori memuat trust
	if m := l.Search(qFr, 1); len(m) != 1 || m[0].Item.Trust() != "konsensus" {
		t.Fatalf("search: %+v", m)
	}
	// umpan balik +1 -> terverifikasi, permanen, tidak ditimpa
	l.Feedback(it.ID, 1)
	it2, ok, _ := l.Learn(qFr, "Jawaban sangat berbeda soal kota Lyon dan Marseille di selatan.", "peer:mistral")
	if ok || it2.Trust() != "terverifikasi" || !it2.Expires.IsZero() || it2.Conflicts != 1 || !strings.Contains(it2.Answer, "Paris") {
		t.Fatalf("terverifikasi tak boleh ditimpa: %+v", it2)
	}
	// pengguna otoritatif: menimpa dan permanen
	it3, ok, _ := l.Learn(qFr, "Prancis beribu kota di Paris sejak berabad-abad lalu.", "user")
	if !ok || it3.Trust() != "pengguna" || !strings.Contains(it3.Answer, "berabad") {
		t.Fatalf("user: %+v", it3)
	}
	// AI lain tidak bisa menimpa jawaban pengguna
	if it4, ok, _ := l.Learn(qFr, ansFrA, "peer:groq"); ok || it4.Source != "user" {
		t.Fatalf("peer menimpa user: %+v", it4)
	}
}

func TestConflictAndReviewQueue(t *testing.T) {
	l := NewLearner(&memStore{}, 100, 24*time.Hour)
	l.Learn("Siapa penemu telepon?", "Telepon dipatenkan oleh Alexander Graham Bell pada 1876.", "peer:a") // 1 AI
	time.Sleep(2 * time.Millisecond)
	l.Learn(qFr, ansFrA, "peer:a")
	l.Learn(qFr, ansFrB, "peer:b") // konsensus
	time.Sleep(2 * time.Millisecond)
	l.Learn("Apa warna langit?", "Langit tampak biru karena hamburan Rayleigh di atmosfer.", "peer:a")
	it, ok, _ := l.Learn("Apa warna langit?", "Langit berwarna hijau tua sepanjang tahun menurut para petani.", "peer:b")
	if ok || it.Conflicts != 1 || !strings.Contains(it.Answer, "Rayleigh") {
		t.Fatalf("konflik: jawaban lama dipertahankan & dicatat: %+v ok=%v", it, ok)
	}
	// AI yang sama berubah pikiran -> jawaban diganti, peers direset
	it, ok, _ = l.Learn("Siapa penemu telepon?", "Penemu telepon yang diakui paten adalah Antonio Meucci menurut sebagian sejarawan.", "peer:a")
	if !ok || !strings.Contains(it.Answer, "Meucci") || len(it.Peers) != 1 {
		t.Fatalf("AI sama berubah: %+v", it)
	}
	r := l.Review(10)
	if len(r) != 3 {
		t.Fatalf("antrean harus 3: %+v", r)
	}
	// urutan ketidakpastian: yang berkonflik dulu (terlama lebih dulu), konsensus terakhir.
	// "telepon": AI yang sama berubah pikiran juga dihitung konflik (tanda tidak stabil).
	if r[0].Question != "Siapa penemu telepon?" || r[0].Conflicts != 1 || r[1].Question != "Apa warna langit?" || r[1].Conflicts != 1 || r[2].Trust != "konsensus" {
		t.Fatalf("urutan salah: %+v", r)
	}
	if got := l.Review(1); len(got) != 1 {
		t.Fatal("limit harus berlaku")
	}
	// persetujuan manusia mengeluarkan dari antrean
	l.Feedback(r[0].ID, 1)
	if got := l.Review(10); len(got) != 2 {
		t.Fatalf("yang disetujui keluar dari antrean: %d", len(got))
	}
	st := l.LearnStats()
	if st["terverifikasi"] != 1 || st["konsensus"] != 1 || st["belum-diverifikasi"] != 1 {
		t.Fatalf("statistik: %v", st)
	}
	// penolakan: -1 dua kali = tidak dipakai & keluar antrean
	l.Feedback(r[1].ID, -1)
	l.Feedback(r[1].ID, -1)
	for _, x := range l.Review(10) {
		if x.ID == r[1].ID {
			t.Fatal("yang ditolak harus keluar dari antrean")
		}
	}
	// data lama (tanpa field baru) tetap terbaca: dianggap belum diverifikasi / pengguna
	old := &memStore{b: []byte(`{"items":[{"id":"a","question":"Q lama?","answer":"Jawaban lama yang cukup panjang.","source":"peer:x","score":0,"created":"2026-01-01T00:00:00Z"},{"id":"b","question":"Q user?","answer":"Jawaban user yang cukup panjang.","source":"user","score":0,"created":"2026-01-01T00:00:00Z"}]}`)}
	ol := NewLearner(old, 10, 0)
	if st := ol.LearnStats(); st["belum-diverifikasi"] != 1 || st["pengguna"] != 1 {
		t.Fatalf("kompatibel mundur: %v", st)
	}
}

func TestMemoryReplyCarriesTrust(t *testing.T) {
	e := newTestEngine(t, nil)
	e.Learn(qFr, ansFrA, "peer:groq")
	r, err := e.Respond(context.Background(), u("ibu kota Prancis apa?"))
	if err != nil || r.Meta["trust"] != "belum-diverifikasi" {
		t.Fatalf("meta.trust: %+v %v", r, err)
	}
	e.Learn(qFr, ansFrB, "peer:gemini")
	if r, _ := e.Respond(context.Background(), u("ibu kota Prancis apa?")); r.Meta["trust"] != "konsensus" {
		t.Fatalf("trust harus naik: %+v", r.Meta)
	}
	if ok, _ := e.Feedback(r.Meta["memory_id"].(string), 1); !ok {
		t.Fatal("feedback")
	}
	if r, _ := e.Respond(context.Background(), u("ibu kota Prancis apa?")); r.Meta["trust"] != "terverifikasi" {
		t.Fatalf("trust harus terverifikasi: %+v", r.Meta)
	}
}

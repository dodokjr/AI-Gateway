package custom

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type memStore struct {
	mu sync.Mutex
	b  []byte
}

func (m *memStore) Load() ([]byte, error) { m.mu.Lock(); defer m.mu.Unlock(); return m.b, nil }
func (m *memStore) Save(b []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.b = append([]byte(nil), b...)
	return nil
}
func (m *memStore) Remote() bool { return false }

func near(t *testing.T, name string, got, want, tol float64) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Fatalf("%s = %v, harapan %v (±%v)", name, got, want, tol)
	}
}

// ---------- matematika ----------

func TestTextMath(t *testing.T) {
	if Levenshtein("kitten", "sitting") != 3 {
		t.Fatal("levenshtein salah")
	}
	near(t, "lev sim sama", LevSimilarity("abc", "abc"), 1, 1e-9)
	c := NewCorpus([][]string{Tokenize("jam buka toko"), Tokenize("cara reset password")})
	a := c.Vector(Tokenize("jam buka toko"))
	near(t, "cosine sama", Cosine(a, a), 1, 1e-9)
	near(t, "cosine beda", Cosine(a, c.Vector(Tokenize("cara reset password"))), 0, 1e-9)
	sm := Softmax([]float64{1, 2, 3}, 1)
	near(t, "jumlah softmax", sm[0]+sm[1]+sm[2], 1, 1e-9)
	if !(sm[2] > sm[1] && sm[1] > sm[0]) {
		t.Fatal("urutan softmax salah")
	}
	if big := Softmax([]float64{1000, 1001}, 1); math.IsNaN(big[0]) || math.IsNaN(big[1]) {
		t.Fatal("softmax tidak stabil untuk angka besar")
	}
}

func TestIndicators(t *testing.T) {
	// Data klasik Wilder; RSI14 pertama ≈ 70.46.
	data := []float64{44.34, 44.09, 44.15, 43.61, 44.33, 44.83, 45.10, 45.42, 45.84, 46.08, 45.89, 46.03, 45.61, 46.28, 46.28}
	r, err := RSI(data, 14)
	if err != nil {
		t.Fatal(err)
	}
	near(t, "RSI", r, 70.46, 0.3)
	up := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	if r, _ := RSI(up, 14); r != 100 {
		t.Fatalf("RSI naik terus harus 100, dapat %v", r)
	}
	e := EMA([]float64{5, 5, 5, 5}, 3)
	near(t, "EMA konstan", e[3], 5, 1e-12)
	e2 := EMA([]float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, 3)
	if !(e2[9] < 10 && e2[9] > 8) {
		t.Fatalf("EMA tren naik harus tertinggal di bawah harga: %v", e2[9])
	}
}

func TestBlackScholes(t *testing.T) {
	call := BlackScholes(100, 100, 1, 0.05, 0.2, true)
	put := BlackScholes(100, 100, 1, 0.05, 0.2, false)
	near(t, "call", call, 10.4506, 0.01)
	near(t, "put", put, 5.5735, 0.01)
	near(t, "put-call parity", call-put, 100-100*math.Exp(-0.05), 1e-9)
	near(t, "intrinsic", BlackScholes(120, 100, 0, 0.05, 0.2, true), 20, 1e-9)
}

func TestGBMAndMonteCarlo(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	path := SimulateGBM(100, 0.0005, 0.01, 5000, rng)
	mu, sigma := EstimateGBM(path)
	near(t, "mu", mu, 0.0005, 0.0005)
	near(t, "sigma", sigma, 0.01, 0.0005)

	mc := MonteCarlo(100, 0.001, 0.02, 30, 20000, 1)
	near(t, "mean MC", mc.Mean, 100*math.Exp(0.03), 0.5)
	if !(mc.P5 < mc.P25 && mc.P25 < mc.P50 && mc.P50 < mc.P75 && mc.P75 < mc.P95) {
		t.Fatal("persentil tidak berurutan")
	}
	if mc.ProbUp < 0.5 || mc.ProbUp > 0.7 {
		t.Fatalf("ProbUp tidak masuk akal: %v", mc.ProbUp)
	}
	if mc.CVaR95 < mc.VaR95 {
		t.Fatal("CVaR harus >= VaR")
	}
	again := MonteCarlo(100, 0.001, 0.02, 30, 20000, 1)
	if again != mc {
		t.Fatal("seed sama harus menghasilkan hasil sama")
	}
}

func TestAnalyze(t *testing.T) {
	if _, err := Analyze([]float64{1, 2, 3}, 10, 252, 1000); err == nil {
		t.Fatal("data terlalu sedikit harus ditolak")
	}
	bad := make([]float64, 40)
	if _, err := Analyze(bad, 10, 252, 1000); err == nil {
		t.Fatal("harga nol harus ditolak")
	}
	// tren naik stabil -> condong naik
	p := make([]float64, 120)
	rng := rand.New(rand.NewSource(3))
	p[0] = 100
	for i := 1; i < len(p); i++ {
		p[i] = p[i-1] * math.Exp(0.004+0.005*rng.NormFloat64())
	}
	f, err := Analyze(p, 30, 252, 5000)
	if err != nil {
		t.Fatal(err)
	}
	if f.Trend != "naik" {
		t.Fatalf("tren seharusnya naik, dapat %s (sinyal %.2f)", f.Trend, f.Signal)
	}
	near(t, "jumlah skenario", f.Scenarios[0]+f.Scenarios[1]+f.Scenarios[2], 1, 1e-9)
	if f.Scenarios[0] <= f.Scenarios[2] {
		t.Fatal("skenario naik harus > turun")
	}
	if !strings.Contains(f.Summary("X", "periode"), "bukan nasihat keuangan") {
		t.Fatal("ringkasan harus memuat disclaimer")
	}
	for _, w := range []string{"GBM", "EMA12", "Monte Carlo", "Softmax", "Black-Scholes", "VaR95"} {
		if !strings.Contains(f.Reference("X"), w) {
			t.Fatalf("referensi matematis kurang %q", w)
		}
	}
}

// ---------- parser pasar ----------

func TestMarketParsers(t *testing.T) {
	cases := map[string]int{"prediksi btc 7 hari": 7, "forecast 2 minggu": 14, "proyeksi 3 bulan": 90, "1 tahun": 365, "tanpa horizon": 30, "5000 tahun": 730}
	for in, want := range cases {
		if got := ParseHorizonDays(in); got != want {
			t.Fatalf("ParseHorizonDays(%q)=%d, harapan %d", in, got, want)
		}
	}
	if ParseInlineSeries("data: 1 2 3") != nil {
		t.Fatal("kurang dari 15 angka tidak boleh dianggap deret")
	}
	if s := ParseInlineSeries("prediksi: 1,2,3,4,5,6,7,8,9,10,11,12,13,14,15.5"); len(s) != 15 || s[14] != 15.5 {
		t.Fatalf("deret salah: %v", s)
	}
	a, ok := ResolveAsset("prediksi bitcoin 30 hari")
	if !ok || a.Kind != "coingecko" || a.ID != "bitcoin" || a.PeriodsPerYear != 365 {
		t.Fatalf("bitcoin: %+v %v", a, ok)
	}
	if a, _ := ResolveAsset("ramalan harga emas"); a.ID != "xauusd" {
		t.Fatalf("emas: %+v", a)
	}
	if a, _ := ResolveAsset("prediksi saham AAPL"); a.ID != "aapl.us" {
		t.Fatalf("AAPL: %+v", a)
	}
	if _, ok := ResolveAsset("apakah ada hujan besok"); ok {
		t.Fatal("kata 'ada' tidak boleh dianggap aset")
	}
	if !IsMarketIntent("prediksi harga bitcoin") || !IsMarketIntent("forecast saham AAPL 7 hari") {
		t.Fatal("seharusnya niat pasar")
	}
	if IsMarketIntent("siapa presiden pertama indonesia") || IsMarketIntent("harga bitcoin sekarang") {
		t.Fatal("bukan permintaan prediksi")
	}
}

func fakeCSV(n int) string {
	var b strings.Builder
	b.WriteString("Date,Open,High,Low,Close,Volume\n")
	p := 100.0
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < n; i++ {
		p *= math.Exp(0.001 + 0.01*rng.NormFloat64())
		fmt.Fprintf(&b, "2026-01-%02d,%f,%f,%f,%f,1000\n", i%28+1, p, p, p, p)
	}
	return b.String()
}

func TestMarketFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/stooq"):
			if r.URL.Query().Get("s") != "aapl.us" {
				http.Error(w, "x", 404)
				return
			}
			_, _ = w.Write([]byte(fakeCSV(60)))
		case strings.Contains(r.URL.Path, "/coins/bitcoin/market_chart"):
			_, _ = w.Write([]byte(`{"prices":[[1,100.5],[2,101.5],[3,0],[4,102.5]]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	m := NewMarketClient()
	m.StooqURL, m.CoinGeckoURL = srv.URL+"/stooq", srv.URL
	p, err := m.Fetch(context.Background(), Asset{Kind: "stooq", ID: "aapl.us"})
	if err != nil || len(p) != 60 {
		t.Fatalf("stooq: %d %v", len(p), err)
	}
	p, err = m.Fetch(context.Background(), Asset{Kind: "coingecko", ID: "bitcoin"})
	if err != nil || len(p) != 3 { // harga 0 dibuang
		t.Fatalf("coingecko: %v %v", p, err)
	}
	if _, err := m.Fetch(context.Background(), Asset{Kind: "stooq", ID: "nope"}); err == nil {
		t.Fatal("simbol tak dikenal harus error")
	}
}

// ---------- pencarian web ----------

const ddgFixture = `
<div class="result"><h2 class="result__title"><a rel="nofollow" class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fa&amp;rut=abc">Contoh <b>Satu</b></a></h2>
<a class="result__snippet" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fa">Ini &amp; snippet <b>pertama</b></a></div>
<div class="result"><a rel="nofollow" class="result__a" href="https://duckduckgo.com/y.js?ad_provider=x">Iklan</a><a class="result__snippet" href="x">iklan</a></div>
<div class="result"><a rel="nofollow" class="result__a" href="https://example.org/b">Dua</a>
<a class="result__snippet" href="https://example.org/b">Snippet dua</a></div>`

func TestParseDDG(t *testing.T) {
	rs := parseDDG(ddgFixture)
	if len(rs) != 2 {
		t.Fatalf("harus 2 hasil (iklan dibuang): %+v", rs)
	}
	if rs[0].URL != "https://example.com/a" || rs[0].Title != "Contoh Satu" || rs[0].Snippet != "Ini & snippet pertama" {
		t.Fatalf("hasil 1 salah: %+v", rs[0])
	}
	if rs[1].URL != "https://example.org/b" || rs[1].Snippet != "Snippet dua" {
		t.Fatalf("hasil 2 salah: %+v", rs[1])
	}
}

func TestSearcherEnginesAndFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ddg":
			http.Error(w, "blocked", 202+300) // 502 -> mesin pertama gagal
		case "/google":
			if r.URL.Query().Get("key") != "K" || r.URL.Query().Get("cx") != "CX" {
				http.Error(w, "x", 403)
				return
			}
			_, _ = w.Write([]byte(`{"items":[{"title":"G <b>1</b>","link":"https://g.example/1","snippet":"snip g"}]}`))
		case "/brave":
			if r.Header.Get("X-Subscription-Token") != "B" {
				http.Error(w, "x", 401)
				return
			}
			_, _ = w.Write([]byte(`{"web":{"results":[{"title":"B1","url":"https://b.example/1","description":"desc <strong>b</strong>"},{"title":"dup","url":"https://g.example/1","description":"dup"}]}}`))
		}
	}))
	defer srv.Close()
	s := NewSearcher([]string{"duckduckgo", "google", "brave"})
	s.DDGURL, s.GoogleURL, s.BraveURL = srv.URL+"/ddg", srv.URL+"/google", srv.URL+"/brave"
	s.GoogleKey, s.GoogleCX, s.BraveKey = "K", "CX", "B"
	rs, err := s.Search(context.Background(), "tes", 5)
	if err != nil || len(rs) != 2 {
		t.Fatalf("fallback + dedupe gagal: %+v %v", rs, err)
	}
	if rs[0].Title != "G 1" || rs[1].Snippet != "desc b" {
		t.Fatalf("pembersihan HTML salah: %+v", rs)
	}
	// mesin tanpa kunci dilewati dengan error yang jelas
	s2 := NewSearcher([]string{"google"})
	s2.GoogleKey, s2.GoogleCX = "", ""
	if _, err := s2.Search(context.Background(), "x", 3); err == nil || !strings.Contains(err.Error(), "GOOGLE_CSE_API_KEY") {
		t.Fatalf("harus menjelaskan konfigurasi hilang: %v", err)
	}
}

func TestSearchErrorDoesNotLeakKey(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close() // koneksi akan ditolak
	s := NewSearcher([]string{"google"})
	s.GoogleURL, s.GoogleKey, s.GoogleCX = url, "RAHASIA-KEY-123", "CX"
	_, err := s.Search(context.Background(), "x", 3)
	if err == nil {
		t.Fatal("harus error")
	}
	if strings.Contains(err.Error(), "RAHASIA-KEY-123") {
		t.Fatalf("API key bocor di pesan error: %v", err)
	}
}

// ---------- belajar mandiri ----------

func TestLearnerSearchAndRules(t *testing.T) {
	st := &memStore{}
	l := NewLearner(st, 100, time.Hour)
	if _, ok, _ := l.Learn("Apa ibu kota Prancis?", "Ibu kota Prancis adalah Paris, kota besar di Eropa.", "peer:groq"); !ok {
		t.Fatal("seharusnya tersimpan")
	}
	l.Learn("Siapa penemu telepon?", "Telepon dipatenkan oleh Alexander Graham Bell pada 1876.", "peer:groq")
	ms := l.Search("ibu kota Prancis apa?", 3)
	if len(ms) == 0 || !strings.Contains(ms[0].Item.Answer, "Paris") || ms[0].Score < 0.99 {
		t.Fatalf("pertanyaan sama (beda huruf/stopword) harus cocok penuh: %+v", ms)
	}
	if ms := l.Search("ibu kota Prancsi apa?", 3); len(ms) == 0 || ms[0].Score < 0.8 {
		t.Fatalf("salah ketik 'Prancsi' harus tetap cocok: %+v", ms)
	}
	if ms := l.Search("resep nasi goreng", 3); len(ms) > 0 && ms[0].Score > 0.3 {
		t.Fatalf("pertanyaan tak terkait tidak boleh cocok: %+v", ms[0])
	}
	// persisten
	l2 := NewLearner(st, 100, time.Hour)
	if l2.Count() != 2 {
		t.Fatalf("persist gagal: %d", l2.Count())
	}
	// jawaban user tidak ditimpa peer
	l.Learn("Apa warna langit?", "Langit tampak biru karena hamburan Rayleigh.", "user")
	l.Learn("Apa warna langit?", "Jawaban peer yang berbeda, cukup panjang.", "peer:gemini")
	if m := l.Search("Apa warna langit?", 1); !strings.Contains(m[0].Item.Answer, "Rayleigh") {
		t.Fatal("jawaban user tertimpa peer")
	}
	// umpan balik negatif menurunkan, dua kali menyembunyikan
	id := l.Search("Siapa penemu telepon?", 1)[0].Item.ID
	l.Feedback(id, -1)
	l.Feedback(id, -1)
	for _, m := range l.Search("Siapa penemu telepon?", 3) {
		if m.Item.ID == id {
			t.Fatal("item dengan skor <= -2 harus disembunyikan")
		}
	}
}

func TestShouldLearn(t *testing.T) {
	long := "Jawaban yang cukup panjang untuk disimpan sebagai pengetahuan."
	bad := []struct{ q, a string }{
		{"Berapa suhu hari ini?", long},                      // cepat basi
		{"Siapa presiden terbaru Prancis?", long},            // cepat basi
		{"prediksi harga bitcoin 7 hari", long},              // pasar
		{"buatkan gambar kucing", long},                      // gambar
		{"Email saya budi@contoh.com, apa kabar?", long},     // data pribadi
		{"Nomor KTP saya 3201234567890123 valid?", long},     // data pribadi
		{"Simpan password saya hunter2", long},               // rahasia
		{"Apa ibu kota Prancis?", "Pendek"},                  // jawaban terlalu pendek
		{"Apa itu API?", "Gunakan key gsk_abcdefghijklmnop"}, // rahasia di jawaban
	}
	for _, c := range bad {
		if ShouldLearn(c.q, c.a) {
			t.Fatalf("seharusnya tidak dipelajari: %q", c.q)
		}
	}
	if !ShouldLearn("Apa ibu kota Prancis?", long) {
		t.Fatal("pertanyaan normal harus boleh dipelajari")
	}
}

func TestPeerTTL(t *testing.T) {
	l := NewLearner(&memStore{}, 100, time.Millisecond)
	l.Learn("Apa ibu kota Prancis?", "Ibu kota Prancis adalah Paris, kota besar di Eropa.", "peer:x")
	time.Sleep(5 * time.Millisecond)
	if len(l.Search("Apa ibu kota Prancis?", 1)) != 0 {
		t.Fatal("pengetahuan kedaluwarsa tidak boleh dipakai")
	}
}

// ---------- ASCII / biner ----------

func TestAsciiArt(t *testing.T) {
	yes := []string{"buatkan gambar kucing", "gambarkan rumah", "draw a cat", "bikin ascii art hati", "create an image of a tree"}
	no := []string{"jelaskan foto kemerdekaan", "apa itu gambar digital", "siapa penemu telepon"}
	for _, q := range yes {
		if !IsImageRequest(q) {
			t.Fatalf("harus terdeteksi: %q", q)
		}
	}
	for _, q := range no {
		if IsImageRequest(q) {
			t.Fatalf("tidak boleh terdeteksi: %q", q)
		}
	}
	r, subj := RenderImageRequest("buatkan gambar kucing")
	if subj != "kucing" || !strings.Contains(r, "##") {
		t.Fatalf("ascii kucing salah: %s", r)
	}
	r, _ = RenderImageRequest("gambarkan hati dalam biner")
	art := r[strings.Index(r, "```\n")+4 : strings.LastIndex(r, "\n```")]
	for _, c := range art {
		if c != '0' && c != '1' && c != ' ' && c != '\n' {
			t.Fatalf("mode biner hanya boleh 0/1: %q", c)
		}
	}
	if !strings.Contains(art, "1") || !strings.Contains(art, "0") {
		t.Fatal("biner harus memuat 0 dan 1")
	}
	r, subj = RenderImageRequest("buatkan gambar HALO")
	if subj != "HALO" || !strings.Contains(r, "belum tersedia") {
		t.Fatalf("subjek tak dikenal harus jadi banner: %s", r)
	}
	r, _ = RenderImageRequest("buat gambar lingkaran ukuran 9")
	if lines := strings.Count(r[strings.Index(r, "```\n")+4:], "\n"); lines < 9 {
		t.Fatalf("ukuran 9 harus 9 baris, dapat %d", lines)
	}
}

// ---------- Engine ----------

func newTestEngine(t *testing.T, mut func(*Config)) *Engine {
	cfg := DefaultConfig()
	cfg.WebSearch = false
	if mut != nil {
		mut(&cfg)
	}
	return NewEngine(cfg, &memStore{})
}

func u(s string) []Message { return []Message{{Role: "user", Content: s}} }

func TestEngineMemoryAndDecline(t *testing.T) {
	e := newTestEngine(t, nil)
	_, err := e.Respond(context.Background(), u("Apa ibu kota Prancis?"))
	var d *Declined
	if !errors.As(err, &d) || d.Declined() == false {
		t.Fatalf("memori kosong harus Declined: %v", err)
	}
	e.Learn("Apa ibu kota Prancis?", "Ibu kota Prancis adalah Paris, kota besar di Eropa.", "peer:groq")
	r, err := e.Respond(context.Background(), u("ibu kota Prancis apa?"))
	if err != nil || !strings.Contains(r.Text, "Paris") || r.Meta["source"] != "memory:peer:groq" {
		t.Fatalf("harus menjawab dari memori: %+v %v", r, err)
	}
	// percakapan multi-giliran tidak memakai memori
	multi := []Message{{Role: "user", Content: "halo"}, {Role: "assistant", Content: "hai"}, {Role: "user", Content: "Apa ibu kota Prancis?"}}
	if _, err := e.Respond(context.Background(), multi); !errors.As(err, &d) {
		t.Fatal("multi-giliran harus Declined")
	}
	// knowledge base gateway
	e.SetKnowledge(func(q string, n int) []Fact {
		return []Fact{{ID: "7", Question: "Siapa pemilik Warung Biru?", Answer: "Bu Sari sejak 1999."}}
	})
	r, err = e.Respond(context.Background(), u("Siapa pemlik Warung Biru?")) // salah ketik
	if err != nil || r.Meta["source"] != "knowledge_base" || !strings.Contains(r.Text, "Bu Sari") {
		t.Fatalf("harus memakai KB: %+v %v", r, err)
	}
	// KEAMANAN: pertanyaan yang BERBEDA makna tidak boleh dijawab dengan jawaban yang salah.
	for _, q := range []string{"Siapa pemilik Warung Merah?", "Siapa yang punya Warung Biru?"} {
		if r, err := e.Respond(context.Background(), u(q)); err == nil {
			t.Fatalf("%q tidak boleh dijawab dari memori: %q", q, r.Text)
		}
	}
	e.Learn("Jam buka Warung Biru?", "Warung Biru buka pukul 08.00 sampai 17.00 setiap hari.", "user")
	if r, err := e.Respond(context.Background(), u("Jam tutup Warung Biru?")); err == nil {
		t.Fatalf("'tutup' tidak boleh dijawab dengan jawaban 'buka': %q", r.Text)
	}
	// ambang lebih tinggi -> tidak menjawab
	e.Cfg.Threshold = 0.999
	if _, err := e.Respond(context.Background(), u("Siapa pemilik Warung Biru ya?")); !errors.As(err, &d) {
		t.Fatal("di bawah ambang harus Declined")
	}
	if d.Confidence <= 0 || d.Confidence >= 0.999 {
		t.Fatalf("keyakinan harus dilaporkan: %v", d.Confidence)
	}
}

func TestEngineImageAndMarket(t *testing.T) {
	e := newTestEngine(t, nil)
	r, err := e.Respond(context.Background(), u("buatkan gambar kucing"))
	if err != nil || r.Meta["source"] != "ascii_art" {
		t.Fatalf("gambar: %+v %v", r, err)
	}
	// deret harga dari pengguna
	var nums []string
	p := 100.0
	rng := rand.New(rand.NewSource(5))
	for i := 0; i < 60; i++ {
		p *= math.Exp(0.002 + 0.01*rng.NormFloat64())
		nums = append(nums, fmt.Sprintf("%.2f", p))
	}
	r, err = e.Respond(context.Background(), u("prediksi 14 hari ke depan, data: "+strings.Join(nums, ", ")))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.Text, "Analisis") || r.Secondary == "" || !strings.Contains(r.Secondary, "Monte Carlo") {
		t.Fatalf("dual-jawaban salah: %q / %q", r.Text, r.Secondary)
	}
	if r.Meta["source"] != "math_engine" {
		t.Fatalf("meta salah: %v", r.Meta)
	}
	// aset lewat API pasar tiruan
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(fakeCSV(80))) }))
	defer srv.Close()
	e.Market.StooqURL = srv.URL
	r, err = e.Respond(context.Background(), u("prediksi harga emas 30 hari"))
	if err != nil || r.Secondary == "" {
		t.Fatalf("emas: %+v %v", r, err)
	}
	// sumber data gagal -> Declined (agar AI lain mengambil alih)
	e.Market.StooqURL = "http://127.0.0.1:1"
	var d *Declined
	if _, err := e.Respond(context.Background(), u("prediksi harga emas 30 hari")); !errors.As(err, &d) {
		t.Fatalf("data pasar gagal harus Declined: %v", err)
	}
}

func TestEngineWebHintAndExtractive(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<a class="result__a" href="https://example.com/x">Menara Eiffel</a>
<a class="result__snippet" href="x">Menara Eiffel di Paris memiliki tinggi sekitar 330 meter.</a>`))
	}))
	defer srv.Close()
	e := newTestEngine(t, func(c *Config) { c.WebSearch = true })
	e.Search.DDGURL = srv.URL
	_, err := e.Respond(context.Background(), u("Berapa tinggi menara eiffel di paris?"))
	var d *Declined
	if !errors.As(err, &d) {
		t.Fatalf("snippet web saja belum cukup yakin: %v", err)
	}
	if !strings.Contains(d.Hint(), "330 meter") || !strings.Contains(d.Hint(), "https://example.com/x") {
		t.Fatalf("hint web harus dibawa untuk AI berikutnya: %q", d.Hint())
	}
	if d.Confidence > 0.6 {
		t.Fatalf("keyakinan ekstraktif dibatasi 0.6: %v", d.Confidence)
	}
	// ambang diturunkan -> jawab ekstraktif beserta sumber
	e.Cfg.Threshold = 0.3
	r, err := e.Respond(context.Background(), u("Berapa tinggi menara eiffel di paris?"))
	if err != nil || r.Meta["source"] != "web" || !strings.Contains(r.Text, "Sumber: https://example.com/x") {
		t.Fatalf("jawaban web salah: %+v %v", r, err)
	}
}

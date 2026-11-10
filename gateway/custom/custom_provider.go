// Package custom adalah mesin "Custom AI" lokal: memori belajar mandiri, pencarian web,
// prediksi pasar kuantitatif, dan seni ASCII/biner. Ia BUKAN model bahasa generatif:
// jawabannya berasal dari memori terlatih, knowledge base, hasil pencarian, dan rumus.
// Bila keyakinannya rendah, ia mengembalikan *Declined sehingga gateway pindah ke AI lain,
// lalu jawaban AI lain itu dipelajari kembali (knowledge transfer).
package custom

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"
)

// Config mengatur perilaku Custom AI (dibaca dari "options" di config provider).
type Config struct {
	Threshold      float64  `json:"threshold"`        // ambang keyakinan 0..1; di bawahnya -> failover
	WebSearch      bool     `json:"web_search"`       // cari di web saat memori tidak cukup
	Engines        []string `json:"search_engines"`   // duckduckgo, google, brave
	MaxResults     int      `json:"max_results"`      // jumlah hasil pencarian
	Simulations    int      `json:"simulations"`      // jalur Monte Carlo
	LearnFromPeers bool     `json:"learn_from_peers"` // pelajari jawaban AI lain
	TTLDays        int      `json:"peer_ttl_days"`    // masa berlaku pengetahuan dari AI lain (0 = selamanya)
	Market         bool     `json:"market"`           // aktifkan prediksi pasar
	Weather        bool     `json:"weather"`          // aktifkan prakiraan cuaca (Open-Meteo)
	DefaultCity    string   `json:"default_city"`     // kota bila pertanyaan cuaca tidak menyebut lokasi ("" = tidak ada)
}

func DefaultConfig() Config {
	return Config{
		Threshold: 0.8, WebSearch: true, Engines: []string{"duckduckgo"}, MaxResults: 5,
		Simulations: 5000, LearnFromPeers: true, TTLDays: 30, Market: true, Weather: true,
	}
}

type Message struct{ Role, Content string }

// Reply: Text = jawaban utama; Secondary = referensi alternatif (rumus matematis finansial).
type Reply struct {
	Text      string
	Secondary string
	Meta      map[string]any
}

// Fact adalah pengetahuan dari basis pengetahuan gateway (hasil /v1/train).
type Fact struct{ ID, Question, Answer string }

// Declined berarti Custom AI memilih tidak menjawab (keyakinan rendah / di luar kemampuan).
type Declined struct {
	Confidence, Threshold float64
	Reason                string
	HintText              string // konteks web untuk AI berikutnya
}

func (d *Declined) Error() string {
	return fmt.Sprintf("custom AI dilewati: %s (keyakinan %.2f < ambang %.2f)", d.Reason, d.Confidence, d.Threshold)
}
func (d *Declined) Declined() bool { return true }
func (d *Declined) Hint() string   { return d.HintText }

type Engine struct {
	Cfg     Config
	Learner *Learner
	Search  *Searcher
	Market  *MarketClient
	Weather *WeatherClient

	mu      sync.RWMutex
	kb      func(q string, n int) []Fact
	breaker breaker
}

func NewEngine(cfg Config, store Store) *Engine {
	return &Engine{
		Cfg:     cfg,
		Learner: NewLearner(store, 2000, time.Duration(cfg.TTLDays)*24*time.Hour),
		Search:  NewSearcher(cfg.Engines),
		Market:  NewMarketClient(),
		Weather: NewWeatherClient(),
	}
}

// SetKnowledge menghubungkan basis pengetahuan gateway (opsional).
func (e *Engine) SetKnowledge(fn func(q string, n int) []Fact) {
	e.mu.Lock()
	e.kb = fn
	e.mu.Unlock()
}

func lastUser(msgs []Message) (q string, single bool) {
	users, others := 0, 0
	for _, m := range msgs {
		switch m.Role {
		case "user":
			users++
			q = strings.TrimSpace(m.Content)
		case "assistant", "model":
			others++
		}
	}
	return q, users == 1 && others == 0
}

type memHit struct {
	Answer, ID, Source string
	Score              float64
}

func (e *Engine) bestMemory(q string) (best memHit) {
	for _, m := range e.Learner.Search(q, 3) {
		if m.Score > best.Score {
			best = memHit{Answer: m.Item.Answer, ID: m.Item.ID, Source: "memory:" + m.Item.Source, Score: m.Score}
		}
	}
	e.mu.RLock()
	kb := e.kb
	e.mu.RUnlock()
	if kb != nil {
		facts := kb(q, 3)
		if len(facts) > 0 {
			qs := make([]string, len(facts))
			for i, f := range facts {
				qs[i] = f.Question
			}
			for i, s := range QuestionScores(q, qs) {
				if s > best.Score {
					best = memHit{Answer: facts[i].Answer, ID: "kb:" + facts[i].ID, Source: "knowledge_base", Score: s}
				}
			}
		}
	}
	return best
}

// Respond menjawab, atau mengembalikan *Declined bila tidak yakin.
func (e *Engine) respond(ctx context.Context, msgs []Message) (*Reply, error) {
	q, single := lastUser(msgs)
	if cq, ok := SanitizeInput(q); !ok {
		return nil, &Declined{Reason: "pesan terlalu panjang untuk Custom AI", Threshold: e.Cfg.Threshold}
	} else {
		q = cq
	}
	if q == "" {
		return nil, &Declined{Reason: "tidak ada pertanyaan", Threshold: e.Cfg.Threshold}
	}

	// 1. Permintaan gambar -> seni ASCII/biner (tanpa menolak).
	if IsImageRequest(q) {
		text, subj := RenderImageRequest(q)
		return &Reply{Text: text, Meta: map[string]any{"source": "ascii_art", "confidence": 1.0, "subject": subj}}, nil
	}

	// 2a. Cuaca -> prakiraan hari ini + N hari ke depan dari Open-Meteo.
	var reason string
	if e.Cfg.Weather && IsWeatherIntent(q) {
		r, err := e.weather(ctx, q)
		if err == nil {
			return r, nil
		}
		reason = "prakiraan cuaca gagal: " + err.Error()
	}

	// 2a'. Logika pemrograman -> perapi kode, tabel kebenaran, pustaka algoritma.
	if reason == "" && IsCodeIntent(q) {
		r, err := e.code(q)
		if err == nil {
			return r, nil
		}
		reason = "logika pemrograman: " + err.Error()
	}

	// 2b. Prediksi pasar -> jawaban utama + referensi matematis.
	if reason == "" && e.Cfg.Market && IsMarketIntent(q) {
		r, err := e.forecast(ctx, q)
		if err == nil {
			return r, nil
		}
		reason = "prediksi pasar gagal: " + err.Error()
	}

	// 3. Memori terlatih + knowledge base (hanya untuk pertanyaan tunggal tanpa konteks percakapan).
	conf := 0.0
	if single && reason == "" {
		if h := e.bestMemory(q); h.Score > 0 {
			conf = h.Score
			if h.Score >= e.Cfg.Threshold {
				return &Reply{Text: h.Answer, Meta: map[string]any{"source": h.Source, "confidence": round2(h.Score), "memory_id": h.ID}}, nil
			}
		}
	} else if reason == "" {
		reason = "percakapan multi-giliran butuh konteks penuh"
	}

	// 4. Pencarian web: jawaban ekstraktif bila cukup yakin, selain itu jadi konteks untuk AI berikutnya.
	hint := ""
	if e.Cfg.WebSearch && e.Search != nil {
		sctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		rs, err := e.Search.Search(sctx, q, e.Cfg.MaxResults)
		cancel()
		if err == nil && len(rs) > 0 {
			hint = SanitizeUntrusted(FormatHint(rs, 1500))
			if ans, c := extractive(q, rs); c > conf {
				conf = c
				if c >= e.Cfg.Threshold {
					return &Reply{Text: ans, Meta: map[string]any{"source": "web", "confidence": round2(c)}}, nil
				}
			}
		} else if err != nil && reason == "" {
			reason = "pencarian web gagal"
		}
	}
	if reason == "" {
		reason = "tidak ada pengetahuan yang cukup mirip"
	}
	return nil, &Declined{Confidence: conf, Threshold: e.Cfg.Threshold, Reason: reason, HintText: hint}
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

// extractive memilih snippet yang paling menutup kata kunci pertanyaan.
// Keyakinan dibatasi maksimal 0.6: ringkasan mentah mesin pencari tidak setara jawaban terverifikasi.
func extractive(q string, rs []SearchResult) (string, float64) {
	qt := Tokenize(q)
	if len(qt) == 0 {
		return "", 0
	}
	type cand struct {
		r   SearchResult
		cov float64
	}
	var best []cand
	for _, r := range rs {
		have := map[string]bool{}
		for _, t := range Tokenize(r.Title + " " + r.Snippet) {
			have[t] = true
		}
		hit := 0
		for _, t := range qt {
			if have[t] {
				hit++
			}
		}
		if r.Snippet != "" {
			best = append(best, cand{r, float64(hit) / float64(len(qt))})
		}
	}
	if len(best) == 0 {
		return "", 0
	}
	top := best[0]
	for _, c := range best[1:] {
		if c.cov > top.cov {
			top = c
		}
	}
	return SanitizeUntrusted(fmt.Sprintf("%s\n\nSumber: %s", top.r.Snippet, top.r.URL)), math.Min(0.6, top.cov*0.6)
}

func (e *Engine) weather(ctx context.Context, q string) (*Reply, error) {
	lang, q := DetectWeatherLang(q)
	city, explicit := ExtractLocation(q)
	if city == "" {
		city, explicit = e.Cfg.DefaultCity, false
	}
	if city == "" {
		return nil, errors.New("sebutkan kota, mis. \"cuaca di Bandung\"")
	}
	wctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	place, err := e.Weather.GeocodeLang(wctx, city, lang)
	if err != nil && !explicit && e.Cfg.DefaultCity != "" && city != e.Cfg.DefaultCity {
		// tebakan lokasi gagal: pakai kota bawaan (kota yang disebut eksplisit tidak pernah diganti diam-diam)
		place, err = e.Weather.GeocodeLang(wctx, e.Cfg.DefaultCity, lang)
	}
	if err != nil {
		return nil, err
	}
	days := WeatherHorizon(q)
	f, err := e.Weather.Forecast(wctx, place, days)
	if err != nil {
		return nil, fmt.Errorf("gagal mengambil prakiraan %s: %w", place.Name, err)
	}
	text, secondary := f.RenderLang(lang)
	return &Reply{Text: text, Secondary: secondary,
		Meta: map[string]any{"source": "open-meteo", "location": place.String(), "days": len(f.Days), "lang": lang}}, nil
}

func (e *Engine) forecast(ctx context.Context, q string) (*Reply, error) {
	days := ParseHorizonDays(q)
	var prices []float64
	var name string
	ppy := 252.0
	if s := ParseInlineSeries(q); s != nil {
		prices, name = s, "data yang Anda berikan"
	} else {
		a, ok := ResolveAsset(q)
		if !ok {
			return nil, fmt.Errorf("aset tidak dikenali (sebut mis. bitcoin, emas, ihsg, AAPL, atau kirim deret harga)")
		}
		mctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		p, err := e.Market.Fetch(mctx, a)
		if err != nil {
			return nil, fmt.Errorf("gagal mengambil data %s: %w", a.Name, err)
		}
		prices, name, ppy = p, a.Name, a.PeriodsPerYear
	}
	steps := int(math.Round(float64(days) * ppy / 365))
	if steps < 1 {
		steps = 1
	}
	f, err := Analyze(prices, steps, ppy, e.Cfg.Simulations)
	if err != nil {
		return nil, err
	}
	text := f.Summary(name, fmt.Sprintf("periode (≈%d hari kalender)", days))
	if e.Cfg.WebSearch && e.Search != nil {
		sctx, cancel := context.WithTimeout(ctx, 4*time.Second)
		rs, serr := e.Search.Search(sctx, name+" market news", 3)
		cancel()
		if serr == nil && len(rs) > 0 {
			text += "\n\nKonteks web terkait:\n" + FormatHint(rs, 600)
		}
	}
	conf := math.Max(f.Scenarios[0], math.Max(f.Scenarios[1], f.Scenarios[2]))
	return &Reply{
		Text: text, Secondary: f.Reference(name),
		Meta: map[string]any{"source": "math_engine", "confidence": round2(conf), "trend": f.Trend, "prob_up": round2(f.MC.ProbUp)},
	}, nil
}

// Learn mempelajari pasangan Q&A (mis. dari jawaban AI lain). Mengembalikan true bila tersimpan.
func (e *Engine) Learn(q, a, source string) bool {
	if strings.HasPrefix(source, "peer") && !e.Cfg.LearnFromPeers {
		return false
	}
	_, ok, _ := e.Learner.Learn(q, a, source)
	return ok
}

func (e *Engine) Feedback(id string, delta int) (bool, error) { return e.Learner.Feedback(id, delta) }

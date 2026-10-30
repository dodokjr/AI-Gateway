package gateway

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

type ProviderCfg struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"` // openai | openai_compat | anthropic | gemini
	BaseURL  string   `json:"base_url,omitempty"`
	Model    string   `json:"model"`
	Keys     []string `json:"keys,omitempty"`     // kunci langsung (tidak disarankan)
	KeysEnv  string   `json:"keys_env,omitempty"` // nama env var; boleh banyak kunci dipisah koma
	NoKey    bool     `json:"no_key,omitempty"`   // mis. Ollama lokal
	Priority int      `json:"priority"`           // kecil = dicoba lebih dulu
	Disabled bool     `json:"disabled,omitempty"`
}

type Config struct {
	Listen     string        `json:"listen"`
	KBPath     string        `json:"kb_path"`
	TimeoutSec int           `json:"timeout_sec"`
	Providers  []ProviderCfg `json:"providers"`
}

type keyState struct {
	key       string
	coolUntil time.Time
	ok, fail  int
	lastErr   string
}

type provState struct {
	cfg  ProviderCfg
	impl Provider
	keys []*keyState
	rr   int
}

type Gateway struct {
	mu      sync.Mutex
	provs   []*provState
	timeout time.Duration
}

type ChatOptions struct {
	Provider    string
	Model       string
	Temperature *float64
	MaxTokens   int
}

type ChatResult struct {
	Text     string
	Provider string
	Model    string
	Failed   []Attempt
}

type AllFailedError struct{ Attempts []Attempt }

func (e *AllFailedError) Error() string {
	return "semua provider gagal atau sedang cooldown"
}

func NewGateway(cfg Config) (*Gateway, error) {
	g := &Gateway{timeout: time.Duration(cfg.TimeoutSec) * time.Second}
	if g.timeout <= 0 {
		g.timeout = 60 * time.Second
	}
	for _, pc := range cfg.Providers {
		if pc.Disabled {
			continue
		}
		impl, err := newProvider(pc)
		if err != nil {
			return nil, err
		}
		keys := append([]string(nil), pc.Keys...)
		if pc.KeysEnv != "" {
			for _, k := range strings.Split(os.Getenv(pc.KeysEnv), ",") {
				if k = strings.TrimSpace(k); k != "" {
					keys = append(keys, k)
				}
			}
		}
		ps := &provState{cfg: pc, impl: impl}
		for _, k := range keys {
			ps.keys = append(ps.keys, &keyState{key: k})
		}
		if pc.NoKey {
			ps.keys = append(ps.keys, &keyState{})
		}
		if len(ps.keys) == 0 {
			fmt.Fprintf(os.Stderr, "peringatan: provider %q dilewati (tidak ada API key; isi env %s)\n", pc.Name, pc.KeysEnv)
			continue
		}
		g.provs = append(g.provs, ps)
	}
	if len(g.provs) == 0 {
		return nil, errors.New("tidak ada provider aktif: isi minimal satu API key di .env atau aktifkan Ollama")
	}
	sort.SliceStable(g.provs, func(i, j int) bool { return g.provs[i].cfg.Priority < g.provs[j].cfg.Priority })
	return g, nil
}

func (g *Gateway) Names() []string {
	var n []string
	for _, p := range g.provs {
		n = append(n, p.cfg.Name)
	}
	return n
}

func (g *Gateway) order(prefer string) []*provState {
	out := make([]*provState, 0, len(g.provs))
	for _, p := range g.provs {
		if prefer != "" && strings.EqualFold(p.cfg.Name, prefer) {
			out = append(out, p)
		}
	}
	for _, p := range g.provs {
		if prefer == "" || !strings.EqualFold(p.cfg.Name, prefer) {
			out = append(out, p)
		}
	}
	return out
}

func (g *Gateway) pickKey(p *provState, tried map[*keyState]bool) *keyState {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now()
	n := len(p.keys)
	for i := 0; i < n; i++ {
		k := p.keys[(p.rr+i)%n]
		if tried[k] || now.Before(k.coolUntil) {
			continue
		}
		p.rr = (p.rr + i + 1) % n // rotasi round-robin antar key
		return k
	}
	return nil
}

func containsAny(s string, subs ...string) bool {
	for _, x := range subs {
		if strings.Contains(s, x) {
			return true
		}
	}
	return false
}

// classify menentukan berapa lama key diistirahatkan. providerWide=true berarti
// kesalahan ada pada permintaan/model (bukan key), jadi lompat ke provider lain.
func classify(err error) (cool time.Duration, providerWide bool) {
	var ae *APIError
	if !errors.As(err, &ae) {
		return 30 * time.Second, false // jaringan/timeout
	}
	body := strings.ToLower(ae.Body)
	switch {
	case ae.Status == 401 || ae.Status == 403:
		return 6 * time.Hour, false // key salah/dicabut
	case ae.Status == 402:
		return time.Hour, false // saldo/kredit habis
	case ae.Status == 429:
		d := time.Minute
		if ae.RetryAfter > 0 {
			d = ae.RetryAfter
		} else if containsAny(body, "perday", "per day", "daily", "billing", "credit", "insufficient") {
			d = time.Hour // kuota harian habis
		}
		if d > 6*time.Hour {
			d = 6 * time.Hour
		}
		return d, false
	case ae.Status >= 500:
		return 30 * time.Second, false
	case ae.Status == 400 || ae.Status == 404 || ae.Status == 413 || ae.Status == 422:
		return 0, true
	}
	return 30 * time.Second, false
}

func (g *Gateway) markOK(k *keyState) {
	g.mu.Lock()
	defer g.mu.Unlock()
	k.ok++
	k.lastErr = ""
}

func (g *Gateway) markFail(k *keyState, err error) (providerWide bool) {
	cool, wide := classify(err)
	g.mu.Lock()
	defer g.mu.Unlock()
	k.fail++
	k.lastErr = truncate(err.Error(), 200)
	if cool > 0 {
		k.coolUntil = time.Now().Add(cool)
	}
	return wide
}

func mask(k string) string {
	switch {
	case k == "":
		return "-"
	case len(k) <= 8:
		return "****"
	}
	return k[:4] + "..." + k[len(k)-2:]
}

// Chat mencoba provider berurutan; tiap provider memutar key-nya. Jika token
// habis (429/402) key masuk cooldown dan gateway otomatis pindah ke berikutnya.
func (g *Gateway) Chat(ctx context.Context, o ChatOptions, system string, msgs []Message) (*ChatResult, error) {
	msgs = normalize(msgs)
	var failed []Attempt
	for _, p := range g.order(o.Provider) {
		model := p.cfg.Model
		if o.Model != "" && strings.EqualFold(p.cfg.Name, o.Provider) {
			model = o.Model
		}
		tried := map[*keyState]bool{}
		for {
			k := g.pickKey(p, tried)
			if k == nil {
				if len(tried) == 0 {
					failed = append(failed, Attempt{Provider: p.cfg.Name, Key: "-", Error: "semua key sedang cooldown"})
				}
				break
			}
			tried[k] = true
			cctx, cancel := context.WithTimeout(ctx, g.timeout)
			text, err := p.impl.Chat(cctx, k.key, ProviderRequest{
				Model: model, System: system, Messages: msgs,
				Temperature: o.Temperature, MaxTokens: o.MaxTokens,
			})
			cancel()
			if err == nil && strings.TrimSpace(text) == "" {
				err = errors.New("respons kosong")
			}
			if err == nil {
				g.markOK(k)
				return &ChatResult{Text: text, Provider: p.cfg.Name, Model: model, Failed: failed}, nil
			}
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			failed = append(failed, Attempt{Provider: p.cfg.Name, Key: mask(k.key), Error: truncate(err.Error(), 200)})
			if g.markFail(k, err) {
				break // salah model/permintaan: coba provider lain
			}
		}
	}
	return nil, &AllFailedError{Attempts: failed}
}

type KeyStatus struct {
	Key         string `json:"key"`
	Available   bool   `json:"available"`
	CooldownSec int    `json:"cooldown_sec"`
	OK          int    `json:"ok"`
	Fail        int    `json:"fail"`
	LastError   string `json:"last_error,omitempty"`
}

type ProviderStatus struct {
	Name     string      `json:"name"`
	Type     string      `json:"type"`
	Model    string      `json:"model"`
	Priority int         `json:"priority"`
	Keys     []KeyStatus `json:"keys"`
}

func (g *Gateway) Status() []ProviderStatus {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now()
	var out []ProviderStatus
	for _, p := range g.provs {
		ps := ProviderStatus{Name: p.cfg.Name, Type: p.cfg.Type, Model: p.cfg.Model, Priority: p.cfg.Priority}
		for _, k := range p.keys {
			ks := KeyStatus{Key: mask(k.key), Available: !now.Before(k.coolUntil), OK: k.ok, Fail: k.fail, LastError: k.lastErr}
			if !ks.Available {
				ks.CooldownSec = int(k.coolUntil.Sub(now).Seconds()) + 1
			}
			ps.Keys = append(ps.Keys, ks)
		}
		out = append(out, ps)
	}
	return out
}

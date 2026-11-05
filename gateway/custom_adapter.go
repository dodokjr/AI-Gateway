package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"ai-gateway/gateway/custom"
)

// customStoreFactory dapat diganti di tes agar tidak menyentuh disk/Upstash.
var customStoreFactory = CustomStoreFromEnv

// customProv membungkus custom.Engine agar memenuhi Provider/RichProvider.
type customProv struct{ e *custom.Engine }

func newCustomProvider(c ProviderCfg) (Provider, error) {
	cfg := custom.DefaultConfig()
	if len(c.Options) > 0 {
		if err := json.Unmarshal(c.Options, &cfg); err != nil {
			return nil, fmt.Errorf("provider %q: options tidak valid: %w", c.Name, err)
		}
	}
	if cfg.Threshold <= 0 || cfg.Threshold > 1 {
		return nil, fmt.Errorf("provider %q: threshold harus di antara 0 dan 1", c.Name)
	}
	if cfg.Simulations < 100 {
		cfg.Simulations = 100
	}
	if cfg.Simulations > 50000 {
		cfg.Simulations = 50000
	}
	return &customProv{e: custom.NewEngine(cfg, customStoreFactory())}, nil
}

func (c *customProv) ChatRich(ctx context.Context, _ string, r ProviderRequest) (Reply, error) {
	msgs := make([]custom.Message, len(r.Messages))
	for i, m := range r.Messages {
		msgs[i] = custom.Message{Role: m.Role, Content: m.Content}
	}
	rep, err := c.e.Respond(ctx, msgs)
	if err != nil {
		return Reply{}, err
	}
	return Reply{Text: rep.Text, Secondary: rep.Secondary, Meta: rep.Meta}, nil
}

// Chat (tanpa metadata): jawaban utama + referensi alternatif digabung dalam satu teks.
func (c *customProv) Chat(ctx context.Context, key string, r ProviderRequest) (string, error) {
	rep, err := c.ChatRich(ctx, key, r)
	if err != nil {
		return "", err
	}
	if rep.Secondary != "" {
		return rep.Text + "\n\n--- Jawaban alternatif (rumus matematis) ---\n" + rep.Secondary, nil
	}
	return rep.Text, nil
}

// AttachKnowledge menghubungkan knowledge base (/v1/train) ke semua Custom AI.
func (g *Gateway) AttachKnowledge(kb *KB) {
	for _, p := range g.provs {
		if cp, ok := p.impl.(*customProv); ok {
			cp.e.SetKnowledge(func(q string, n int) []custom.Fact {
				ex := kb.Search(q, n)
				out := make([]custom.Fact, len(ex))
				for i, e := range ex {
					out[i] = custom.Fact{ID: e.ID, Question: e.Question, Answer: e.Answer}
				}
				return out
			})
		}
	}
}

// learnFromPeer: setelah AI lain berhasil menjawab pertanyaan tunggal, Custom AI mempelajarinya.
func (g *Gateway) learnFromPeer(msgs []Message, answer, from string) {
	q, single := singleQuestion(msgs)
	if !single {
		return
	}
	for _, p := range g.provs {
		if cp, ok := p.impl.(*customProv); ok {
			cp.e.Learn(q, answer, "peer:"+from)
		}
	}
}

// CustomFeedback menaikkan/menurunkan skor pengetahuan di memori Custom AI.
func (g *Gateway) CustomFeedback(id string, delta int) (bool, error) {
	id = strings.TrimPrefix(id, "mem:")
	for _, p := range g.provs {
		if cp, ok := p.impl.(*customProv); ok {
			if ok, err := cp.e.Feedback(id, delta); ok || err != nil {
				return ok, err
			}
		}
	}
	return false, nil
}
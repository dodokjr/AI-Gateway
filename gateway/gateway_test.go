package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

type mock struct {
	srv  *httptest.Server
	hits int32
	mu   sync.Mutex
	last string
}

func newMock(t *testing.T, handle func(auth string) (int, string)) *mock {
	m := &mock{}
	m.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&m.hits, 1)
		buf := new(bytes.Buffer)
		_, _ = buf.ReadFrom(r.Body)
		m.mu.Lock()
		m.last = buf.String()
		m.mu.Unlock()
		code, reply := handle(r.Header.Get("Authorization"))
		if code != 200 {
			w.WriteHeader(code)
			_, _ = w.Write([]byte(`{"error":"x"}`))
			return
		}
		b, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": reply}}}})
		_, _ = w.Write(b)
	}))
	t.Cleanup(m.srv.Close)
	return m
}

func (m *mock) lastBody() string { m.mu.Lock(); defer m.mu.Unlock(); return m.last }

func userMsg(s string) []Message { return []Message{{Role: "user", Content: s}} }

func TestFailoverAndCooldown(t *testing.T) {
	a := newMock(t, func(string) (int, string) { return 429, "" })
	b := newMock(t, func(string) (int, string) { return 200, "dari-b" })
	gw, err := NewGateway(Config{Providers: []ProviderCfg{
		{Name: "a", Type: "openai_compat", BaseURL: a.srv.URL, Model: "m", Keys: []string{"ka"}, Priority: 1},
		{Name: "b", Type: "openai_compat", BaseURL: b.srv.URL, Model: "m", Keys: []string{"kb"}, Priority: 2},
	}})
	if err != nil {
		t.Fatal(err)
	}
	res, err := gw.Chat(context.Background(), ChatOptions{}, "", userMsg("halo"))
	if err != nil || res.Text != "dari-b" || res.Provider != "b" || len(res.Failed) != 1 {
		t.Fatalf("failover gagal: %+v %v", res, err)
	}
	if _, err = gw.Chat(context.Background(), ChatOptions{}, "", userMsg("lagi")); err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&a.hits) != 1 {
		t.Fatalf("provider a harus cooldown setelah 429, hits=%d", a.hits)
	}
}

func TestKeyRotation(t *testing.T) {
	m := newMock(t, func(auth string) (int, string) {
		if auth == "Bearer k1" {
			return 429, ""
		}
		return 200, "ok-k2"
	})
	gw, _ := NewGateway(Config{Providers: []ProviderCfg{
		{Name: "a", Type: "openai_compat", BaseURL: m.srv.URL, Model: "m", Keys: []string{"k1", "k2"}, Priority: 1},
	}})
	res, err := gw.Chat(context.Background(), ChatOptions{}, "", userMsg("halo"))
	if err != nil || res.Text != "ok-k2" {
		t.Fatalf("rotasi key gagal: %+v %v", res, err)
	}
}

func TestAllFailed(t *testing.T) {
	m := newMock(t, func(string) (int, string) { return 402, "" })
	gw, _ := NewGateway(Config{Providers: []ProviderCfg{
		{Name: "a", Type: "openai_compat", BaseURL: m.srv.URL, Model: "m", Keys: []string{"k"}, Priority: 1},
	}})
	_, err := gw.Chat(context.Background(), ChatOptions{}, "", userMsg("halo"))
	if _, ok := err.(*AllFailedError); !ok {
		t.Fatalf("harus AllFailedError, dapat %v", err)
	}
}

func TestKBSearchAndPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kb.json")
	kb := NewKB(FileStore{Path: path})
	if _, err := kb.Add("Apa jam buka toko Maju Jaya?", "Toko buka 08.00-17.00 setiap hari.", nil); err != nil {
		t.Fatal(err)
	}
	_, _ = kb.Add("Cara reset password akun", "Klik Lupa Password di halaman login.", nil)
	got := kb.Search("jam buka toko maju jaya kapan?", 3)
	if len(got) != 1 || !strings.Contains(got[0].Answer, "08.00") {
		t.Fatalf("pencarian salah: %+v", got)
	}
	kb2 := NewKB(FileStore{Path: path})
	if len(kb2.List()) != 2 {
		t.Fatal("persist gagal")
	}
	if len(kb.Search("cuaca hari ini di bandung", 3)) != 0 {
		t.Fatal("pertanyaan tak relevan tidak boleh cocok")
	}
}

func TestServerTrainThenChatAndTextOnly(t *testing.T) {
	m := newMock(t, func(string) (int, string) { return 200, "jawaban" })
	gw, _ := NewGateway(Config{Providers: []ProviderCfg{
		{Name: "a", Type: "openai_compat", BaseURL: m.srv.URL, Model: "m", Keys: []string{"k"}, Priority: 1},
	}})
	kb := NewKB(FileStore{Path: filepath.Join(t.TempDir(), "kb.json")})
	api := httptest.NewServer((&Server{gw: gw, kb: kb, apiKey: "rahasia"}).routes())
	t.Cleanup(api.Close)

	do := func(method, path, body string) (int, string) {
		req, _ := http.NewRequest(method, api.URL+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer rahasia")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		buf := new(bytes.Buffer)
		_, _ = buf.ReadFrom(resp.Body)
		return resp.StatusCode, buf.String()
	}

	// tanpa auth ditolak
	resp, _ := http.Post(api.URL+"/v1/chat", "application/json", strings.NewReader(`{"prompt":"hi"}`))
	if resp.StatusCode != 401 {
		t.Fatalf("harus 401, dapat %d", resp.StatusCode)
	}
	if code, b := do("POST", "/v1/train", `{"question":"Siapa pendiri Warung Biru?","answer":"Warung Biru didirikan Bu Sari tahun 1999."}`); code != 200 {
		t.Fatalf("train: %d %s", code, b)
	}
	if code, b := do("POST", "/v1/chat", `{"prompt":"siapa pendiri warung biru?"}`); code != 200 || !strings.Contains(b, "jawaban") {
		t.Fatalf("chat: %d %s", code, b)
	}
	if !strings.Contains(m.lastBody(), "Bu Sari tahun 1999") {
		t.Fatalf("pengetahuan terlatih tidak masuk prompt: %s", m.lastBody())
	}
	img := `{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"http://x/a.png"}}]}]}`
	if code, _ := do("POST", "/v1/chat/completions", img); code != 400 {
		t.Fatalf("gambar harus ditolak 400, dapat %d", code)
	}
	if code, b := do("POST", "/v1/chat/completions", `{"model":"auto","messages":[{"role":"user","content":[{"type":"text","text":"halo"}]}]}`); code != 200 || !strings.Contains(b, "chat.completion") {
		t.Fatalf("format OpenAI: %d %s", code, b)
	}
}

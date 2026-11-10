package gateway

import (
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

type memStoreT struct {
	mu sync.Mutex
	b  []byte
}

func (m *memStoreT) Load() ([]byte, error) { m.mu.Lock(); defer m.mu.Unlock(); return m.b, nil }
func (m *memStoreT) Save(b []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.b = append([]byte(nil), b...)
	return nil
}
func (m *memStoreT) Remote() bool { return false }
func (m *memStoreT) Kind() string { return "memory" }

func newCustomGateway(t *testing.T, peerURL, options string) (*Gateway, *customProv) {
	t.Helper()
	old := customStoreFactory
	customStoreFactory = func() Store { return &memStoreT{} }
	t.Cleanup(func() { customStoreFactory = old })
	gw, err := NewGateway(Config{Providers: []ProviderCfg{
		{Name: "custom", Type: "custom", Model: "custom-1", NoKey: true, Priority: 1, Options: json.RawMessage(options)},
		{Name: "peer", Type: "openai_compat", BaseURL: peerURL, Model: "m", Keys: []string{"k"}, Priority: 2},
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range gw.provs {
		if cp, ok := p.impl.(*customProv); ok {
			return gw, cp
		}
	}
	t.Fatal("provider custom tidak terdaftar")
	return nil, nil
}

const noWeb = `{"web_search":false}`

func TestCustomFailoverLearnAndReuse(t *testing.T) {
	peer := newMock(t, func(string) (int, string) {
		return 200, "Ibu kota Prancis adalah Paris, sebuah kota besar di Eropa."
	})
	gw, cp := newCustomGateway(t, peer.srv.URL, noWeb)
	ctx := context.Background()

	// 1) Custom AI tidak tahu -> failover ke peer; jawaban peer dipelajari.
	res, err := gw.Chat(ctx, ChatOptions{}, "", userMsg("Apa ibu kota Prancis?"))
	if err != nil || res.Provider != "peer" {
		t.Fatalf("harus dijawab peer: %+v %v", res, err)
	}
	if len(res.Failed) != 1 || res.Failed[0].Provider != "custom" || !strings.Contains(res.Failed[0].Error, "dilewati") {
		t.Fatalf("percobaan custom harus tercatat: %+v", res.Failed)
	}
	if cp.e.Learner.Count() != 1 {
		t.Fatalf("jawaban peer harus dipelajari, count=%d", cp.e.Learner.Count())
	}

	// 2) Pertanyaan sama -> dijawab Custom AI sendiri, tanpa memanggil peer.
	res, err = gw.Chat(ctx, ChatOptions{}, "", userMsg("ibu kota prancis apa?"))
	if err != nil || res.Provider != "custom" || !strings.Contains(res.Text, "Paris") {
		t.Fatalf("harus dijawab custom: %+v %v", res, err)
	}
	if atomic.LoadInt32(&peer.hits) != 1 {
		t.Fatalf("peer tidak boleh dipanggil lagi, hits=%d", peer.hits)
	}
	if res.Meta["source"] != "memory:peer:peer" {
		t.Fatalf("meta sumber salah: %v", res.Meta)
	}

	// 3) Declined bukan kegagalan key: tidak ada cooldown dan tidak ada hitungan fail.
	st := gw.Status()[0]
	if st.Name != "custom" || !st.Keys[0].Available || st.Keys[0].Fail != 0 {
		t.Fatalf("custom tidak boleh kena cooldown/fail: %+v", st)
	}

	// 4) Percakapan multi-giliran tidak dipelajari (bergantung konteks).
	before := cp.e.Learner.Count()
	multi := []Message{{Role: "user", Content: "halo"}, {Role: "assistant", Content: "hai"}, {Role: "user", Content: "Apa warna langit?"}}
	if _, err := gw.Chat(ctx, ChatOptions{}, "", multi); err != nil {
		t.Fatal(err)
	}
	if cp.e.Learner.Count() != before {
		t.Fatal("percakapan multi-giliran tidak boleh dipelajari")
	}
}

func TestCustomWebHintForwardedToPeer(t *testing.T) {
	ddg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<a class="result__a" href="https://example.com/x">Menara Eiffel</a>
<a class="result__snippet" href="x">Menara Eiffel memiliki tinggi sekitar 330 meter.</a>`))
	}))
	defer ddg.Close()
	peer := newMock(t, func(string) (int, string) { return 200, "Tingginya sekitar 330 meter menurut konteks." })
	gw, cp := newCustomGateway(t, peer.srv.URL, `{"web_search":true}`)
	cp.e.Search.DDGURL = ddg.URL

	res, err := gw.Chat(context.Background(), ChatOptions{}, "persona", userMsg("Berapa tinggi menara eiffel?"))
	if err != nil || res.Provider != "peer" {
		t.Fatalf("%+v %v", res, err)
	}
	body := peer.lastBody()
	for _, want := range []string{"330 meter", "https://example.com/x", "tak tepercaya", "persona"} {
		if !strings.Contains(body, want) {
			t.Fatalf("prompt peer harus memuat %q: %s", want, body)
		}
	}
}

func TestCustomOverHTTP(t *testing.T) {
	peer := newMock(t, func(string) (int, string) { return 200, "jawaban peer yang cukup panjang untuk disimpan." })
	gw, _ := newCustomGateway(t, peer.srv.URL, noWeb)
	kb := NewKB(FileStore{Path: filepath.Join(t.TempDir(), "kb.json")})
	gw.AttachKnowledge(kb)
	api := httptest.NewServer(NewServer(gw, kb, "", 0).Handler())
	defer api.Close()

	post := func(path, body string) map[string]any {
		t.Helper()
		resp, err := http.Post(api.URL+path, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return out
	}

	// gambar -> ASCII dari custom, peer tidak dipanggil
	out := post("/v1/chat", `{"prompt":"buatkan gambar hati"}`)
	if out["provider"] != "custom" || !strings.Contains(out["text"].(string), "```") {
		t.Fatalf("gambar: %v", out)
	}

	// pasar -> dual-jawaban (text + secondary)
	var nums []string
	for i := 0; i < 50; i++ {
		nums = append(nums, strings.TrimSpace(strings.Replace(strings.Repeat("x", 0)+jsonNum(100+float64(i%7)+float64(i)*0.3), "\n", "", -1)))
	}
	out = post("/v1/chat", `{"prompt":"prediksi 7 hari, data: `+strings.Join(nums, ", ")+`"}`)
	if out["provider"] != "custom" || out["secondary"] == nil || !strings.Contains(out["secondary"].(string), "Monte Carlo") {
		t.Fatalf("dual-jawaban: %v", out)
	}
	if atomic.LoadInt32(&peer.hits) != 0 {
		t.Fatal("peer tidak boleh dipanggil untuk gambar/pasar")
	}

	// pelajari lewat peer lalu beri umpan balik negatif lewat /v1/feedback
	post("/v1/chat", `{"prompt":"Apa warna langit saat siang?"}`)
	out = post("/v1/chat", `{"prompt":"apa warna langit saat siang?"}`)
	meta, _ := out["meta"].(map[string]any)
	id, _ := meta["memory_id"].(string)
	if out["provider"] != "custom" || id == "" {
		t.Fatalf("harus dijawab dari memori: %v", out)
	}
	for i := 0; i < 2; i++ {
		if r := post("/v1/feedback", `{"id":"`+id+`","rating":-1}`); r["status"] == nil {
			t.Fatalf("feedback gagal: %v", r)
		}
	}
	out = post("/v1/chat", `{"prompt":"apa warna langit saat siang?"}`)
	if out["provider"] != "peer" {
		t.Fatalf("pengetahuan berskor buruk harus tidak dipakai lagi: %v", out)
	}
}

func jsonNum(f float64) string { b, _ := json.Marshal(f); return string(b) }

func TestSelfCheckEndpoints(t *testing.T) {
	t.Setenv("RATE_LIMIT_PER_MIN", "0")
	peer := newMock(t, func(string) (int, string) { return 200, "jawaban peer yang cukup panjang untuk disimpan." })
	gw, _ := newCustomGateway(t, peer.srv.URL, noWeb)
	kb := NewKB(FileStore{Path: filepath.Join(t.TempDir(), "kb.json")})
	api := httptest.NewServer(NewServer(gw, kb, "kunci", 0).Handler())
	defer api.Close()
	for _, c := range []struct{ method, path string }{{"GET", "/v1/selfcheck"}, {"POST", "/v1/selfheal"}} {
		req, _ := http.NewRequest(c.method, api.URL+c.path, nil)
		if resp, err := http.DefaultClient.Do(req); err != nil || resp.StatusCode != 401 {
			t.Fatalf("%s tanpa kunci harus 401", c.path)
		}
		req.Header.Set("Authorization", "Bearer kunci")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var out struct {
			Custom []struct {
				Name   string `json:"name"`
				Report struct {
					Healthy bool `json:"healthy"`
				} `json:"report"`
			} `json:"custom"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&out)
		resp.Body.Close()
		if resp.StatusCode != 200 || len(out.Custom) != 1 || out.Custom[0].Name != "custom" || !out.Custom[0].Report.Healthy {
			t.Fatalf("%s: %d %+v", c.path, resp.StatusCode, out)
		}
	}
}

package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ai-gateway/gateway/custom"
)

type memLog struct {
	mu   sync.Mutex
	recs [][]byte
}

func (m *memLog) Append(b []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.recs = append(m.recs, append([]byte(nil), b...))
	return nil
}
func (m *memLog) Recent(n int) ([][]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.recs
	if n > 0 && len(r) > n {
		r = r[len(r)-n:]
	}
	return append([][]byte(nil), r...), nil
}
func (m *memLog) Count() (int, error) { m.mu.Lock(); defer m.mu.Unlock(); return len(m.recs), nil }
func (m *memLog) Clear() error        { m.mu.Lock(); defer m.mu.Unlock(); m.recs = nil; return nil }
func (m *memLog) Kind() string        { return "memory" }

func TestRedactPII(t *testing.T) {
	in := "Email budi@example.com, telp +62 812 3456 7890, KTP 3201234567890123, key sk-abcdefghijklmnopqrstuv, " +
		"jam 08.00-17.00, tanggal 2026-11-12, harga Rp 1.500.000, tahun 2026"
	out := custom.RedactPII(in)
	for _, leak := range []string{"budi@", "812 3456", "3201234567890123", "sk-abc"} {
		if strings.Contains(out, leak) {
			t.Fatalf("bocor %q: %s", leak, out)
		}
	}
	for _, keep := range []string{"08.00-17.00", "2026-11-12", "1.500.000", "tahun 2026"} {
		if !strings.Contains(out, keep) {
			t.Fatalf("angka biasa harus dipertahankan %q: %s", keep, out)
		}
	}
}

func TestLogEveryQAWithRedaction(t *testing.T) {
	var calls int32
	peer := newMock(t, func(string) (int, string) {
		if atomic.AddInt32(&calls, 1) == 2 { // hanya jawaban ke-2 memuat data pribadi/rahasia
			return 200, "Penemunya Bell. Hubungi budi@example.com atau pakai kunci sk-abcdefghijklmnopqrstuv."
		}
		return 200, "Ibu kota Prancis adalah Paris, sebuah kota besar di Eropa."
	})
	gw, _ := newCustomGateway(t, peer.srv.URL, noWeb)
	lg := &memLog{}
	gw.SetTrainingLog(lg)
	ctx := context.Background()

	// 1) Custom diam -> peer menjawab; 2) pertanyaan sama -> Custom menjawab dari memori
	if _, err := gw.Chat(ctx, ChatOptions{}, "", userMsg("Apa ibu kota Prancis?")); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.Chat(ctx, ChatOptions{}, "", userMsg("Siapa penemu telepon? (kontak 081234567890)")); err != nil {
		t.Fatal(err)
	}
	// 3) multi-giliran
	multi := []Message{{Role: "user", Content: "halo"}, {Role: "assistant", Content: "hai"}, {Role: "user", Content: "Apa warna langit?"}}
	if _, err := gw.Chat(ctx, ChatOptions{}, "", multi); err != nil {
		t.Fatal(err)
	}
	recs, total, kind, lastErr, err := gw.LogRecent(10)
	if err != nil || total != 3 || kind != "memory" || lastErr != "" || len(recs) != 3 {
		t.Fatalf("log: total=%d kind=%s err=%v %v", total, kind, lastErr, err)
	}
	// terbaru dulu
	if recs[0].Question != "Apa warna langit?" || recs[0].Single || recs[2].Single == false {
		t.Fatalf("urutan/flag single salah: %+v", recs)
	}
	first := recs[2]
	if first.Provider != "peer" || first.Via != "chat" || !strings.Contains(first.Answer, "Paris") {
		t.Fatalf("rekaman pertama: %+v", first)
	}
	if !strings.Contains(recs[1].Question, "[nomor]") {
		t.Fatalf("nomor telepon pada pertanyaan harus disamarkan: %+v", recs[1])
	}
	for _, r := range recs {
		all := r.Question + r.Answer
		if strings.Contains(all, "budi@") || strings.Contains(all, "sk-abc") || strings.Contains(all, "081234567890") {
			t.Fatalf("data pribadi/rahasia bocor di log: %+v", r)
		}
	}
	// jawaban Custom AI dari memori ikut tercatat dengan sumber & trust
	if _, err := gw.Chat(ctx, ChatOptions{}, "", userMsg("Apa ibu kota Prancis?")); err != nil {
		t.Fatal(err)
	}
	recs, _, _, _, _ = gw.LogRecent(1)
	if recs[0].Provider != "custom" || !strings.HasPrefix(recs[0].Source, "memory:") || recs[0].Trust != "belum-diverifikasi" {
		t.Fatalf("jawaban Custom harus tercatat dengan source/trust: %+v", recs[0])
	}
	// dinonaktifkan: tidak mencatat
	gw.SetTrainingLog(nil)
	if _, err := gw.Chat(ctx, ChatOptions{}, "", userMsg("Pertanyaan lain lagi?")); err != nil {
		t.Fatal(err)
	}
	if n, _ := lg.Count(); n != 4 {
		t.Fatalf("setelah nonaktif tidak boleh bertambah: %d", n)
	}
	if _, err := gw.ReplayLog(10); err == nil {
		t.Fatal("replay saat nonaktif harus galat")
	}
}

func TestTrainingEndpointsExportReplayClear(t *testing.T) {
	t.Setenv("RATE_LIMIT_PER_MIN", "0")
	peer := newMock(t, func(string) (int, string) { return 200, "x" })
	gw, cp := newCustomGateway(t, peer.srv.URL, noWeb)
	lg := &memLog{}
	gw.SetTrainingLog(lg)
	add := func(r QARecord) {
		r.Time = time.Now()
		b, _ := json.Marshal(r)
		_ = lg.Append(b)
	}
	q := "Apa ibu kota Prancis?"
	add(QARecord{Question: q, Answer: "Ibu kota Prancis adalah Paris, kota besar di Eropa.", Provider: "groq", Via: "chat", Single: true})
	add(QARecord{Question: q, Answer: "Paris adalah ibu kota Prancis, kota besar di Eropa.", Provider: "gemini", Via: "council", Single: true})
	add(QARecord{Question: q, Answer: "jawaban custom sendiri yang cukup panjang.", Provider: "custom", Via: "chat", Single: true})
	add(QARecord{Question: "lanjutan?", Answer: "jawaban bergantung konteks yang panjang.", Provider: "groq", Via: "chat", Single: false})
	add(QARecord{Question: "Berapa suhu sekarang hari ini?", Answer: "Suhunya sekitar 30 derajat saat ini di Jakarta.", Provider: "groq", Via: "chat", Single: true}) // waktu-nyata: disaring

	kb := NewKB(FileStore{Path: filepath.Join(t.TempDir(), "kb.json")})
	api := httptest.NewServer(NewServer(gw, kb, "kunci", 0).Handler())
	defer api.Close()
	do := func(method, path, token, body string) (*http.Response, []byte) {
		req, _ := http.NewRequest(method, api.URL+path, strings.NewReader(body))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp, b
	}
	if resp, _ := do("GET", "/v1/training/log", "", ""); resp.StatusCode != 401 {
		t.Fatalf("tanpa kunci harus 401: %d", resp.StatusCode)
	}
	// daftar
	resp, b := do("GET", "/v1/training/log?limit=2", "kunci", "")
	var lr struct {
		Count   int        `json:"count"`
		Storage string     `json:"storage"`
		Records []QARecord `json:"records"`
	}
	_ = json.Unmarshal(b, &lr)
	if resp.StatusCode != 200 || lr.Count != 5 || len(lr.Records) != 2 || lr.Records[0].Question != "Berapa suhu sekarang hari ini?" {
		t.Fatalf("log: %d %s", resp.StatusCode, b)
	}
	// ekspor: hanya satu-giliran, format percakapan
	resp, b = do("GET", "/v1/training/export", "kunci", "")
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if resp.StatusCode != 200 || len(lines) != 4 || resp.Header.Get("X-Records") != "4" || !strings.Contains(resp.Header.Get("Content-Type"), "ndjson") {
		t.Fatalf("export: %d %q hdr=%v", resp.StatusCode, b, resp.Header)
	}
	var line struct {
		Messages []map[string]string `json:"messages"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &line); err != nil || len(line.Messages) != 2 || line.Messages[0]["role"] != "user" || line.Messages[1]["role"] != "assistant" {
		t.Fatalf("format baris salah: %q", lines[0])
	}
	_, b = do("GET", "/v1/training/export?exclude_custom=1&provider=groq,gemini", "kunci", "")
	if n := len(strings.Split(strings.TrimSpace(string(b)), "\n")); n != 3 {
		t.Fatalf("filter harus menyisakan 3: %d %q", n, b)
	}
	// replay: 2 AI berbeda setuju -> konsensus; custom, multi-giliran, dan waktu-nyata dilewati
	resp, b = do("POST", "/v1/training/replay", "kunci", `{"limit":100}`)
	var rr ReplayResult
	_ = json.Unmarshal(b, &rr)
	if resp.StatusCode != 200 || rr.Read != 5 || rr.Offered != 3 || rr.Learned != 2 || rr.Skipped != 3 {
		t.Fatalf("replay: %s", b)
	}
	if m := cp.e.Learner.Search(q, 1); len(m) == 0 || m[0].Item.Trust() != "konsensus" {
		t.Fatalf("replay harus membentuk konsensus: %+v", m)
	}
	// hapus
	if resp, _ = do("DELETE", "/v1/training/log", "kunci", ""); resp.StatusCode != 200 {
		t.Fatalf("hapus: %d", resp.StatusCode)
	}
	if n, _ := lg.Count(); n != 0 {
		t.Fatal("log harus kosong")
	}
}

func TestFileLogRotationAndOrder(t *testing.T) {
	f := &fileLog{Path: filepath.Join(t.TempDir(), "d", "log.jsonl"), MaxBytes: 60}
	for _, s := range []string{`{"n":1}`, `{"n":2}`, `{"n":3}`, `{"n":4}`, `{"n":5}`, `{"n":6}`, `{"n":7}`, `{"n":8}`, `{"n":9}`, `{"n":10}`, `{"n":11}`} {
		if err := f.Append([]byte(s)); err != nil {
			t.Fatal(err)
		}
	}
	all, err := f.Recent(0)
	if err != nil || len(all) < 3 {
		t.Fatalf("recent: %v %d", err, len(all))
	}
	if string(all[len(all)-1]) != `{"n":11}` {
		t.Fatalf("urutan lama->baru, terakhir harus 11: %q", all[len(all)-1])
	}
	two, _ := f.Recent(2)
	if len(two) != 2 || string(two[1]) != `{"n":11}` || string(two[0]) != `{"n":10}` {
		t.Fatalf("Recent(2): %q", two)
	}
	if n, _ := f.Count(); n != len(all) {
		t.Fatalf("count: %d vs %d", n, len(all))
	}
	if err := f.Clear(); err != nil {
		t.Fatal(err)
	}
	if n, _ := f.Count(); n != 0 {
		t.Fatal("harus kosong")
	}
	if err := f.Clear(); err != nil { // idempoten
		t.Fatal(err)
	}
}

func TestUpstashLog(t *testing.T) {
	var mu sync.Mutex
	list := []string{}
	var sawPipeline bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(401)
			return
		}
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		defer mu.Unlock()
		exec := func(c []string) any {
			switch c[0] {
			case "RPUSH":
				list = append(list, c[2])
				return len(list)
			case "LTRIM":
				n, _ := parseNeg(c[2])
				if len(list) > n {
					list = list[len(list)-n:]
				}
				return "OK"
			case "LRANGE":
				n, _ := parseNeg(c[2])
				if len(list) > n {
					return list[len(list)-n:]
				}
				return list
			case "LLEN":
				return len(list)
			case "DEL":
				list = nil
				return 1
			}
			return nil
		}
		if strings.HasSuffix(r.URL.Path, "/pipeline") {
			sawPipeline = true
			var cmds [][]string
			_ = json.Unmarshal(b, &cmds)
			var out []map[string]any
			for _, c := range cmds {
				out = append(out, map[string]any{"result": exec(c)})
			}
			_ = json.NewEncoder(w).Encode(out)
			return
		}
		var c []string
		_ = json.Unmarshal(b, &c)
		_ = json.NewEncoder(w).Encode(map[string]any{"result": exec(c)})
	}))
	defer srv.Close()
	u := upstashLog{URL: srv.URL, Token: "tok", Key: "k", Max: 3}
	for _, s := range []string{`{"n":1}`, `{"n":2}`, `{"n":3}`, `{"n":4}`, `{"n":5}`} {
		if err := u.Append([]byte(s)); err != nil {
			t.Fatal(err)
		}
	}
	if !sawPipeline {
		t.Fatal("harus memakai /pipeline (satu permintaan)")
	}
	if n, err := u.Count(); err != nil || n != 3 {
		t.Fatalf("dipangkas ke Max=3: %d %v", n, err)
	}
	got, err := u.Recent(2)
	if err != nil || len(got) != 2 || string(got[1]) != `{"n":5}` {
		t.Fatalf("recent: %q %v", got, err)
	}
	if err := u.Clear(); err != nil {
		t.Fatal(err)
	}
	if n, _ := u.Count(); n != 0 {
		t.Fatal("kosong")
	}
	bad := upstashLog{URL: srv.URL, Token: "salah", Key: "k"}
	if err := bad.Append([]byte("{}")); err == nil {
		t.Fatal("token salah harus galat")
	}
}

func parseNeg(s string) (int, error) {
	n := 0
	for _, c := range strings.TrimPrefix(s, "-") {
		n = n*10 + int(c-'0')
	}
	return n, nil
}

func TestCouncilIsLogged(t *testing.T) {
	t0 = t
	peers := map[string]*mock{"a": reply("JAWABAN: Paris adalah ibu kota Prancis.\nPENILAIAN: BENAR\nALASAN: ok")}
	gw, cp := councilGateway(t, peers, []string{"a"})
	cp.e.Learner.Learn(frQ, frCustom, "peer:x")
	lg := &memLog{}
	gw.SetTrainingLog(lg)
	if _, err := gw.Council(context.Background(), CouncilOptions{}, "", userMsg(frQ)); err != nil {
		t.Fatal(err)
	}
	recs, total, _, _, _ := gw.LogRecent(10)
	if total != 2 {
		t.Fatalf("custom + 1 peninjau harus tercatat: %d %+v", total, recs)
	}
	var sawVerdict, sawCustom bool
	for _, r := range recs {
		if r.Via != "council" {
			t.Fatalf("via: %+v", r)
		}
		sawVerdict = sawVerdict || (r.Provider == "a" && r.Verdict == "BENAR")
		sawCustom = sawCustom || (r.Provider == "custom" && strings.HasPrefix(r.Source, "memory:"))
	}
	if !sawVerdict || !sawCustom {
		t.Fatalf("verdict/custom hilang: %+v", recs)
	}
}
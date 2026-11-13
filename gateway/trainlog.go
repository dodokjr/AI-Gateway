package gateway

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"ai-gateway/gateway/custom"
)

// Log pelatihan: SEMUA pasangan pertanyaan-jawaban yang lewat gateway (dari Custom AI maupun AI lain, juga dari
// mode dewan) disimpan, dengan data pribadi/rahasia disamarkan. Log ini bahan mentah: dari sini Anda bisa
// (1) melihat, (2) mengekspor JSONL untuk melatih model lain, (3) memutar ulang ke memori Custom AI (replay).
// Memori aktif Custom AI tetap lewat penyaring dan jenjang kepercayaan; log TIDAK dipakai untuk menjawab langsung.

const (
	logMaxQuestion = 2000
	logMaxAnswer   = 8000
)

type QARecord struct {
	Time     time.Time `json:"time"`
	Question string    `json:"question"`
	Answer   string    `json:"answer"`
	Provider string    `json:"provider"`
	Model    string    `json:"model,omitempty"`
	Via      string    `json:"via"`               // chat | council
	Source   string    `json:"source,omitempty"`  // asal jawaban Custom AI (memory:..., code-library, open-meteo, ...)
	Trust    string    `json:"trust,omitempty"`   // tingkat kepercayaan memori Custom AI
	Single   bool      `json:"single_turn"`       // false = bergantung konteks percakapan
	Verdict  string    `json:"verdict,omitempty"` // mode dewan: penilaian peninjau terhadap jawaban Custom AI
}

// LogStore: penyimpanan log yang bersifat tambah-saja (append-only).
type LogStore interface {
	Append(rec []byte) error
	Recent(n int) ([][]byte, error) // n terakhir, urut lama -> baru
	Count() (int, error)
	Clear() error
	Kind() string
}

// ---------- file (JSONL) ----------

type fileLog struct {
	Path      string
	MaxBytes  int64 // lewat batas ini berkas diputar ke .1 (log lama dibuang satu generasi)
	Ephemeral bool
	mu        sync.Mutex
}

func (f *fileLog) Kind() string {
	if f.Ephemeral {
		return "ephemeral"
	}
	return "file"
}

func (f *fileLog) Append(rec []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(f.Path), 0o755); err != nil {
		return err
	}
	if st, err := os.Stat(f.Path); err == nil && f.MaxBytes > 0 && st.Size() >= f.MaxBytes {
		_ = os.Rename(f.Path, f.Path+".1")
	}
	fh, err := os.OpenFile(f.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer fh.Close()
	_, err = fh.Write(append(bytes.TrimRight(rec, "\n"), '\n'))
	return err
}

func readLines(path string) ([][]byte, error) {
	fh, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	var out [][]byte
	sc := bufio.NewScanner(fh)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		if len(bytes.TrimSpace(sc.Bytes())) > 0 {
			out = append(out, append([]byte(nil), sc.Bytes()...))
		}
	}
	return out, sc.Err()
}

func (f *fileLog) all() ([][]byte, error) {
	old, err := readLines(f.Path + ".1")
	if err != nil {
		return nil, err
	}
	cur, err := readLines(f.Path)
	if err != nil {
		return nil, err
	}
	return append(old, cur...), nil
}

func (f *fileLog) Recent(n int) ([][]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	all, err := f.all()
	if err != nil {
		return nil, err
	}
	if n > 0 && len(all) > n {
		all = all[len(all)-n:]
	}
	return all, nil
}

func (f *fileLog) Count() (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	all, err := f.all()
	return len(all), err
}

func (f *fileLog) Clear() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range []string{f.Path, f.Path + ".1"} {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

// ---------- Upstash (daftar Redis, dipangkas ke Max entri) ----------

type upstashLog struct {
	URL, Token, Key string
	Max             int
}

func (u upstashLog) Kind() string { return "upstash" }

func (u upstashLog) run(path string, body any, out any) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return doJSON(ctx, strings.TrimRight(u.URL, "/")+path, map[string]string{"Authorization": "Bearer " + u.Token}, body, out)
}

func (u upstashLog) one(args ...string) (json.RawMessage, error) {
	var out struct {
		Result json.RawMessage `json:"result"`
		Error  string          `json:"error"`
	}
	if err := u.run("", args, &out); err != nil {
		return nil, err
	}
	if out.Error != "" {
		return nil, errors.New("upstash: " + out.Error)
	}
	return out.Result, nil
}

func (u upstashLog) Append(rec []byte) error {
	max := u.Max
	if max <= 0 {
		max = 1000
	}
	var out []struct {
		Result json.RawMessage `json:"result"`
		Error  string          `json:"error"`
	}
	cmds := [][]string{{"RPUSH", u.Key, string(rec)}, {"LTRIM", u.Key, strconv.Itoa(-max), "-1"}}
	if err := u.run("/pipeline", cmds, &out); err != nil {
		return err
	}
	for _, o := range out {
		if o.Error != "" {
			return errors.New("upstash: " + o.Error)
		}
	}
	return nil
}

func (u upstashLog) Recent(n int) ([][]byte, error) {
	if n <= 0 {
		n = 1000
	}
	r, err := u.one("LRANGE", u.Key, strconv.Itoa(-n), "-1")
	if err != nil {
		return nil, err
	}
	var ss []string
	if err := json.Unmarshal(r, &ss); err != nil {
		return nil, err
	}
	out := make([][]byte, len(ss))
	for i, s := range ss {
		out[i] = []byte(s)
	}
	return out, nil
}

func (u upstashLog) Count() (int, error) {
	r, err := u.one("LLEN", u.Key)
	if err != nil {
		return 0, err
	}
	var n int
	return n, json.Unmarshal(r, &n)
}

func (u upstashLog) Clear() error { _, err := u.one("DEL", u.Key); return err }

// LogStoreFromEnv memilih penyimpanan log. LOG_QA=0 mematikan pencatatan.
func LogStoreFromEnv() LogStore {
	if v := strings.ToLower(strings.TrimSpace(os.Getenv("LOG_QA"))); v == "0" || v == "false" || v == "off" {
		return nil
	}
	url := firstEnv("UPSTASH_REDIS_REST_URL", "KV_REST_API_URL")
	tok := firstEnv("UPSTASH_REDIS_REST_TOKEN", "KV_REST_API_TOKEN")
	if url != "" && tok != "" {
		return upstashLog{URL: url, Token: tok, Key: "ai-gateway:qa-log", Max: 1000}
	}
	if isServerless() {
		return &fileLog{Path: filepath.Join(os.TempDir(), "qa_log.jsonl"), MaxBytes: 4 << 20, Ephemeral: true}
	}
	return &fileLog{Path: "data/training_log.jsonl", MaxBytes: 8 << 20}
}

// ---------- pencatatan ----------

func (g *Gateway) SetTrainingLog(l LogStore) { g.tlog = l }

func (g *Gateway) logQA(msgs []Message, answer, provider, model, via string, meta map[string]any, verdict string) {
	if g.tlog == nil {
		return
	}
	q, single := singleQuestion(msgs)
	q, answer = strings.TrimSpace(q), strings.TrimSpace(answer)
	if q == "" || answer == "" {
		return
	}
	rec := QARecord{
		Time: time.Now().UTC(), Question: cut(custom.RedactPII(q), logMaxQuestion), Answer: cut(custom.RedactPII(answer), logMaxAnswer),
		Provider: provider, Model: model, Via: via, Single: single, Verdict: verdict,
	}
	rec.Source, _ = meta["source"].(string)
	rec.Trust, _ = meta["trust"].(string)
	b, err := json.Marshal(rec)
	if err == nil {
		err = g.tlog.Append(b)
	}
	g.mu.Lock()
	if err != nil {
		g.logErr = custom.RedactSecrets(truncate(err.Error(), 200))
	} else {
		g.logErr = ""
	}
	g.mu.Unlock()
}

func cut(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

// ---------- baca / ekspor / putar ulang ----------

func decodeRecords(raw [][]byte) []QARecord {
	out := make([]QARecord, 0, len(raw))
	for _, b := range raw {
		var r QARecord
		if json.Unmarshal(b, &r) == nil && r.Question != "" && r.Answer != "" {
			out = append(out, r)
		}
	}
	return out
}

// LogRecent mengembalikan n catatan terbaru (terbaru dulu), jumlah total, dan galat penulisan terakhir.
func (g *Gateway) LogRecent(n int) (recs []QARecord, total int, kind, lastErr string, err error) {
	if g.tlog == nil {
		return nil, 0, "nonaktif", "", nil
	}
	raw, err := g.tlog.Recent(n)
	if err != nil {
		return nil, 0, g.tlog.Kind(), "", err
	}
	recs = decodeRecords(raw)
	for i, j := 0, len(recs)-1; i < j; i, j = i+1, j-1 {
		recs[i], recs[j] = recs[j], recs[i]
	}
	total, _ = g.tlog.Count()
	g.mu.Lock()
	lastErr = g.logErr
	g.mu.Unlock()
	return recs, total, g.tlog.Kind(), lastErr, nil
}

// ExportJSONL: format percakapan (satu objek per baris) untuk melatih/fine-tune model lain.
// Hanya pasangan satu-giliran. excludeCustom=true membuang jawaban Custom AI (agar tidak berputar-putar).
func (g *Gateway) ExportJSONL(limit int, providers []string, excludeCustom bool) ([]byte, int, error) {
	if g.tlog == nil {
		return nil, 0, errors.New("pencatatan log nonaktif (LOG_QA=0)")
	}
	raw, err := g.tlog.Recent(limit)
	if err != nil {
		return nil, 0, err
	}
	want := map[string]bool{}
	for _, p := range providers {
		if p = strings.ToLower(strings.TrimSpace(p)); p != "" {
			want[p] = true
		}
	}
	var buf bytes.Buffer
	n := 0
	for _, r := range decodeRecords(raw) {
		if !r.Single || (excludeCustom && r.Provider == "custom") || (len(want) > 0 && !want[strings.ToLower(r.Provider)]) {
			continue
		}
		line, _ := json.Marshal(map[string]any{"messages": []map[string]string{
			{"role": "user", "content": r.Question}, {"role": "assistant", "content": r.Answer},
		}})
		buf.Write(line)
		buf.WriteByte('\n')
		n++
	}
	return buf.Bytes(), n, nil
}

type ReplayResult struct {
	Read    int `json:"read"`
	Offered int `json:"offered"`
	Learned int `json:"learned"`
	Skipped int `json:"skipped"`
}

// ReplayLog memutar ulang log ke memori Custom AI lewat jalur belajar biasa (penyaring + jenjang kepercayaan +
// konsensus). Jawaban Custom AI sendiri dan percakapan multi-giliran dilewati.
func (g *Gateway) ReplayLog(limit int) (ReplayResult, error) {
	var res ReplayResult
	if g.tlog == nil {
		return res, errors.New("pencatatan log nonaktif (LOG_QA=0)")
	}
	raw, err := g.tlog.Recent(limit)
	if err != nil {
		return res, err
	}
	var engines []*customProv
	for _, p := range g.provs {
		if cp, ok := p.impl.(*customProv); ok {
			engines = append(engines, cp)
		}
	}
	if len(engines) == 0 {
		return res, errors.New("Custom AI tidak aktif")
	}
	recs := decodeRecords(raw)
	res.Read = len(recs)
	for _, r := range recs {
		if !r.Single || r.Provider == "custom" {
			res.Skipped++
			continue
		}
		res.Offered++
		learned := false
		for _, cp := range engines {
			if cp.e.Learn(r.Question, r.Answer, "peer:"+r.Provider) {
				learned = true
			}
		}
		if learned {
			res.Learned++
		} else {
			res.Skipped++
		}
	}
	return res, nil
}

func (g *Gateway) ClearLog() error {
	if g.tlog == nil {
		return errors.New("pencatatan log nonaktif (LOG_QA=0)")
	}
	return g.tlog.Clear()
}
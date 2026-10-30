package gateway

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

// "Pelatihan" di sini = basis pengetahuan + umpan balik. Contoh tanya-jawab
// yang relevan disisipkan ke prompt (retrieval), jadi bekerja di semua provider
// tanpa fine-tuning bobot model.

type Example struct {
	ID        string    `json:"id"`
	Question  string    `json:"question"`
	Answer    string    `json:"answer"`
	Tags      []string  `json:"tags,omitempty"`
	Score     int       `json:"score"`
	CreatedAt time.Time `json:"created_at"`
}

type kbFile struct {
	Persona string    `json:"persona"`
	Items   []Example `json:"items"`
}

type KB struct {
	mu     sync.Mutex
	store  Store
	data   kbFile
	loaded bool
}

const (
	maxItems    = 5000
	maxQuestion = 2000
	maxAnswer   = 8000
)

func NewKB(store Store) *KB { return &KB{store: store} }

// syncLocked memuat data dari store: sekali untuk file lokal, tiap permintaan
// untuk store remote (karena instance serverless bisa berbeda-beda).
func (k *KB) syncLocked() error {
	if k.loaded && !k.store.Remote() {
		return nil
	}
	b, err := k.store.Load()
	if err != nil {
		return err
	}
	var d kbFile
	if len(b) > 0 {
		if err := json.Unmarshal(b, &d); err != nil {
			return fmt.Errorf("data knowledge rusak: %w", err)
		}
	}
	k.data, k.loaded = d, true
	return nil
}

func (k *KB) saveLocked() error {
	b, err := json.MarshalIndent(k.data, "", "  ")
	if err != nil {
		return err
	}
	return k.store.Save(b)
}

func (k *KB) StorageKind() string { return k.store.Kind() }

func newID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func normQ(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }

// Add menambah contoh; pertanyaan yang sama (case-insensitive) diperbarui.
func (k *KB) Add(q, a string, tags []string) (Example, error) {
	q, a = strings.TrimSpace(q), strings.TrimSpace(a)
	if q == "" || a == "" {
		return Example{}, errors.New("question dan answer wajib diisi")
	}
	if len(q) > maxQuestion || len(a) > maxAnswer {
		return Example{}, fmt.Errorf("terlalu panjang (maks %d/%d karakter)", maxQuestion, maxAnswer)
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if err := k.syncLocked(); err != nil {
		return Example{}, err
	}
	for i := range k.data.Items {
		if normQ(k.data.Items[i].Question) == normQ(q) {
			k.data.Items[i].Answer = a
			if len(tags) > 0 {
				k.data.Items[i].Tags = tags
			}
			return k.data.Items[i], k.saveLocked()
		}
	}
	if len(k.data.Items) >= maxItems {
		return Example{}, fmt.Errorf("basis pengetahuan penuh (maks %d)", maxItems)
	}
	ex := Example{ID: newID(), Question: q, Answer: a, Tags: tags, CreatedAt: time.Now().UTC()}
	k.data.Items = append(k.data.Items, ex)
	return ex, k.saveLocked()
}

func (k *KB) Delete(id string) (bool, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if err := k.syncLocked(); err != nil {
		return false, err
	}
	for i, it := range k.data.Items {
		if it.ID == id {
			k.data.Items = append(k.data.Items[:i], k.data.Items[i+1:]...)
			return true, k.saveLocked()
		}
	}
	return false, nil
}

// Feedback menaikkan/menurunkan skor contoh. Skor <= -3 tidak dipakai lagi.
func (k *KB) Feedback(id string, delta int) (bool, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if err := k.syncLocked(); err != nil {
		return false, err
	}
	for i := range k.data.Items {
		if k.data.Items[i].ID == id {
			k.data.Items[i].Score += delta
			return true, k.saveLocked()
		}
	}
	return false, nil
}

func (k *KB) List() []Example {
	k.mu.Lock()
	defer k.mu.Unlock()
	_ = k.syncLocked()
	return append([]Example(nil), k.data.Items...)
}

func (k *KB) Persona() string {
	k.mu.Lock()
	defer k.mu.Unlock()
	_ = k.syncLocked()
	return k.data.Persona
}

func (k *KB) SetPersona(p string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if err := k.syncLocked(); err != nil {
		return err
	}
	k.data.Persona = strings.TrimSpace(p)
	return k.saveLocked()
}

var stopwords = map[string]bool{
	"yang": true, "dan": true, "untuk": true, "dengan": true, "dari": true, "itu": true, "ini": true,
	"apa": true, "bagaimana": true, "adalah": true, "saya": true, "kamu": true, "ada": true,
	"the": true, "and": true, "for": true, "that": true, "this": true, "with": true, "what": true,
	"how": true, "are": true, "you": true, "can": true,
}

func tokenize(s string) []string {
	f := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	out := f[:0]
	for _, t := range f {
		if len([]rune(t)) >= 3 && !stopwords[t] {
			out = append(out, t)
		}
	}
	return out
}

func toSet(ts []string) map[string]bool {
	m := make(map[string]bool, len(ts))
	for _, t := range ts {
		m[t] = true
	}
	return m
}

// Search mengembalikan maks n contoh paling relevan (skor mirip TF-IDF).
func (k *KB) Search(query string, n int) []Example {
	qt := toSet(tokenize(query))
	if len(qt) == 0 || n <= 0 {
		return nil
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	_ = k.syncLocked() // jika gagal, pakai data terakhir di memori
	items := k.data.Items
	N := len(items)
	docs := make([]map[string]bool, N)
	df := map[string]int{}
	for i, it := range items {
		docs[i] = toSet(tokenize(it.Question + " " + strings.Join(it.Tags, " ") + " " + it.Answer))
		for t := range docs[i] {
			df[t]++
		}
	}
	type scored struct {
		ex Example
		s  float64
	}
	var res []scored
	for i, it := range items {
		if it.Score <= -3 {
			continue
		}
		var s float64
		matched := 0
		for t := range qt {
			if docs[i][t] {
				matched++
				s += math.Log(1 + float64(N)/float64(df[t]))
			}
		}
		if matched == 0 || float64(matched)/float64(len(qt)) < 0.34 {
			continue
		}
		bonus := it.Score
		if bonus > 5 {
			bonus = 5
		}
		if bonus < -2 {
			bonus = -2
		}
		res = append(res, scored{it, s * (1 + 0.1*float64(bonus))})
	}
	sort.Slice(res, func(i, j int) bool { return res[i].s > res[j].s })
	if len(res) > n {
		res = res[:n]
	}
	out := make([]Example, len(res))
	for i, r := range res {
		out[i] = r.ex
	}
	return out
}

func buildSystem(persona, userSystem string, ex []Example) string {
	var parts []string
	if persona != "" {
		parts = append(parts, persona)
	}
	if userSystem != "" {
		parts = append(parts, userSystem)
	}
	if len(ex) > 0 {
		var sb strings.Builder
		sb.WriteString("Pengetahuan terlatih yang relevan (utamakan jika cocok dengan pertanyaan):\n")
		for i, e := range ex {
			fmt.Fprintf(&sb, "\n[%d] Pertanyaan: %s\n    Jawaban: %s\n", i+1, e.Question, e.Answer)
		}
		parts = append(parts, sb.String())
	}
	return strings.Join(parts, "\n\n")
}

package custom

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Store adalah penyimpanan persisten (dipenuhi oleh gateway.Store).
type Store interface {
	Load() ([]byte, error)
	Save([]byte) error
	Remote() bool // true = muat ulang tiap operasi (instance serverless berbeda-beda)
}

// Item adalah satu pengetahuan yang dipelajari Custom AI.
type Item struct {
	ID       string    `json:"id"`
	Question string    `json:"question"`
	Answer   string    `json:"answer"`
	Source   string    `json:"source"` // "user" | "peer:<provider>"
	Score    int       `json:"score"`  // umpan balik pengguna
	Created  time.Time `json:"created"`
	Expires  time.Time `json:"expires,omitempty"` // nol = tidak kedaluwarsa
}

type Match struct {
	Item  Item
	Score float64
}

type memFile struct {
	Items []Item `json:"items"`
}

// Learner = memori belajar mandiri: menyimpan Q&A dan mencarinya kembali dengan TF-IDF + Levenshtein.
type Learner struct {
	mu       sync.Mutex
	store    Store
	items    []Item
	loaded   bool
	maxItems int
	ttl      time.Duration
	lastGood []byte // blob terakhir yang valid (untuk cadangan)
}

func NewLearner(store Store, maxItems int, ttl time.Duration) *Learner {
	if maxItems <= 0 {
		maxItems = 2000
	}
	return &Learner{store: store, maxItems: maxItems, ttl: ttl}
}

func (l *Learner) syncLocked() error {
	if l.loaded && !l.store.Remote() {
		return nil
	}
	b, err := l.store.Load()
	if err != nil {
		return err
	}
	var f memFile
	if len(b) > 0 {
		if err := json.Unmarshal(b, &f); err != nil {
			if restored, herr := l.syncHealLocked(); herr == nil && restored {
				return nil
			}
			return fmt.Errorf("memori custom rusak: %w", err)
		}
	}
	l.items, l.loaded = f.Items, true
	if len(b) > 0 {
		l.lastGood = b
	}
	return nil
}

func (l *Learner) saveLocked() error {
	b, err := json.Marshal(memFile{Items: l.items})
	if err != nil {
		return err
	}
	if bs, ok := l.store.(BackupStore); ok && len(l.lastGood) > 0 {
		_ = bs.SaveBackup(l.lastGood) // cadangan = versi valid sebelumnya
	}
	if err := l.store.Save(b); err != nil {
		return err
	}
	l.lastGood = b
	return nil
}

func newID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func normQ(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }

var (
	timeSensitive = regexp.MustCompile(`(?i)\b(hari ini|sekarang|saat ini|terbaru|terkini|barusan|tadi|kemarin|besok|today|now|latest|current|currently|tonight|yesterday|tomorrow)\b`)
	emailRe       = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)
	longDigitsRe  = regexp.MustCompile(`\d[\d\s.-]{8,}\d`)
	secretRe      = regexp.MustCompile(`(?i)(gsk_|sk-[A-Za-z0-9_-]{12,}|AIza[0-9A-Za-z_-]{10,}|AKIA[0-9A-Z]{8,}|bearer\s+[A-Za-z0-9._-]{12,}|password|kata sandi|api[_ -]?key)`)
)

// ShouldLearn menyaring hal yang tidak boleh/baik dipelajari: data waktu-nyata (cepat basi),
// permintaan pasar/gambar, serta data pribadi/rahasia (memori ini dipakai bersama semua pengguna).
func ShouldLearn(q, a string) bool {
	q, a = strings.TrimSpace(q), strings.TrimSpace(a)
	if len([]rune(q)) < 3 || len([]rune(q)) > 500 || len([]rune(a)) < 20 || len([]rune(a)) > 4000 {
		return false
	}
	if timeSensitive.MatchString(q) || IsMarketIntent(q) || IsImageRequest(q) || IsWeatherIntent(q) || IsCodeIntent(q) {
		return false
	}
	for _, s := range []string{q, a} {
		if emailRe.MatchString(s) || longDigitsRe.MatchString(s) || secretRe.MatchString(s) {
			return false
		}
	}
	return true
}

// Learn menyimpan pasangan Q&A. source: "user" atau "peer:<nama>". Mengembalikan (item, disimpan?).
// Pertanyaan yang sama (dinormalisasi) diperbarui; jawaban dari "user" tidak ditimpa oleh "peer".
func (l *Learner) Learn(q, a, source string) (Item, bool, error) {
	if !ShouldLearn(q, a) {
		return Item{}, false, nil
	}
	q, a = strings.TrimSpace(q), strings.TrimSpace(a)
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.syncLocked(); err != nil {
		return Item{}, false, err
	}
	now := time.Now().UTC()
	var exp time.Time
	if l.ttl > 0 && strings.HasPrefix(source, "peer") {
		exp = now.Add(l.ttl)
	}
	// buang yang kedaluwarsa
	kept := l.items[:0]
	for _, it := range l.items {
		if it.Expires.IsZero() || now.Before(it.Expires) {
			kept = append(kept, it)
		}
	}
	l.items = kept
	for i := range l.items {
		if normQ(l.items[i].Question) == normQ(q) {
			if l.items[i].Source == "user" && source != "user" {
				return l.items[i], false, nil
			}
			l.items[i].Answer, l.items[i].Source, l.items[i].Expires = a, source, exp
			return l.items[i], true, l.saveLocked()
		}
	}
	if len(l.items) >= l.maxItems { // buang yang paling lama dan skornya terendah
		sort.SliceStable(l.items, func(i, j int) bool {
			if l.items[i].Score != l.items[j].Score {
				return l.items[i].Score > l.items[j].Score
			}
			return l.items[i].Created.After(l.items[j].Created)
		})
		l.items = l.items[:l.maxItems-1]
	}
	it := Item{ID: newID(), Question: q, Answer: a, Source: source, Created: now, Expires: exp}
	l.items = append(l.items, it)
	return it, true, l.saveLocked()
}

// Search mengembalikan maksimal k pertanyaan paling mirip beserta skor 0..1.
func (l *Learner) Search(query string, k int) []Match {
	l.mu.Lock()
	defer l.mu.Unlock()
	_ = l.syncLocked() // bila gagal, pakai data terakhir di memori
	now := time.Now()
	var cands []Item
	var qs []string
	for _, it := range l.items {
		if it.Score <= -2 || (!it.Expires.IsZero() && now.After(it.Expires)) {
			continue
		}
		cands = append(cands, it)
		qs = append(qs, it.Question)
	}
	if len(cands) == 0 {
		return nil
	}
	scores := QuestionScores(query, qs)
	res := make([]Match, len(cands))
	for i := range cands {
		s := scores[i]
		bonus := cands[i].Score
		if bonus > 4 {
			bonus = 4
		}
		if bonus < 0 {
			s *= 0.85
		} else {
			s = min(1, s*(1+0.03*float64(bonus)))
		}
		res[i] = Match{Item: cands[i], Score: s}
	}
	sort.SliceStable(res, func(i, j int) bool { return res[i].Score > res[j].Score })
	if k > 0 && len(res) > k {
		res = res[:k]
	}
	return res
}

// Feedback menaikkan/menurunkan skor sebuah pengetahuan (delta +1 / -1).
func (l *Learner) Feedback(id string, delta int) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.syncLocked(); err != nil {
		return false, err
	}
	for i := range l.items {
		if l.items[i].ID == id {
			l.items[i].Score += delta
			return true, l.saveLocked()
		}
	}
	return false, nil
}

func (l *Learner) Count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	_ = l.syncLocked()
	return len(l.items)
}

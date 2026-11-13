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

	// Pembelajaran berjenjang kepercayaan (lihat docs/METODE_PEMBELAJARAN.md).
	Verified  bool     `json:"verified,omitempty"`  // disetujui manusia (umpan balik +1) atau dilatih pengguna
	Peers     []string `json:"peers,omitempty"`     // AI berbeda yang jawabannya SALING SETUJU (konsensus)
	Conflicts int      `json:"conflicts,omitempty"` // berapa kali AI lain menjawab berbeda
}

// Trust: tingkat kepercayaan sebuah pengetahuan.
func (it Item) Trust() string {
	switch {
	case it.Source == "user":
		return "pengguna"
	case it.Verified:
		return "terverifikasi"
	case len(it.Peers) >= 2:
		return "konsensus"
	}
	return "belum-diverifikasi"
}

// answerSimilarity: kemiripan dua jawaban (Jaccard kata) 0..1.
func answerSimilarity(a, b string) float64 {
	sa, sb := map[string]bool{}, map[string]bool{}
	for _, t := range Tokenize(a) {
		sa[t] = true
	}
	for _, t := range Tokenize(b) {
		sb[t] = true
	}
	if len(sa) == 0 || len(sb) == 0 {
		return 0
	}
	inter := 0
	for t := range sa {
		if sb[t] {
			inter++
		}
	}
	return float64(inter) / float64(len(sa)+len(sb)-inter)
}

// AnswerSimilarity: kemiripan kata dua jawaban 0..1 (dipakai gateway untuk membandingkan jawaban antar-AI).
func AnswerSimilarity(a, b string) float64 { return answerSimilarity(a, b) }

const consensusSim = 0.5 // jawaban dianggap "setuju" bila kemiripan kata >= 0,5

func addPeer(peers []string, p string) []string {
	for _, x := range peers {
		if x == p {
			return peers
		}
	}
	return append(peers, p)
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
		if normQ(l.items[i].Question) != normQ(q) {
			continue
		}
		ex := &l.items[i]
		isPeer := strings.HasPrefix(source, "peer")
		switch {
		case !isPeer: // pengguna melatih: otoritatif, menimpa, permanen
			ex.Answer, ex.Source, ex.Expires = a, source, time.Time{}
			ex.Verified, ex.Peers, ex.Conflicts = true, nil, 0
		case ex.Source == "user":
			return *ex, false, nil // jawaban pengguna tidak ditimpa AI lain
		case ex.Verified: // sudah disetujui manusia: jangan ditimpa, hanya catat bila AI lain berbeda
			if answerSimilarity(ex.Answer, a) < consensusSim {
				ex.Conflicts++
				return *ex, false, l.saveLocked()
			}
			return *ex, false, nil
		case answerSimilarity(ex.Answer, a) >= consensusSim: // AI lain SETUJU -> konsensus (ensemble)
			ex.Peers = addPeer(addPeer(ex.Peers, ex.Source), source)
			if ex.Source == source {
				ex.Answer = a
			}
			if l.ttl > 0 {
				ex.Expires = now.Add(l.ttl)
				if len(ex.Peers) >= 2 {
					ex.Expires = now.Add(3 * l.ttl) // konsensus disimpan lebih lama (retensi adaptif)
				}
			}
		default: // berbeda
			ex.Conflicts++
			if ex.Source != source {
				return *ex, false, l.saveLocked() // jawaban lama dipertahankan, dicatat sebagai konflik
			}
			ex.Answer, ex.Peers, ex.Expires = a, []string{source}, exp // AI yang sama mengubah jawaban
		}
		return *ex, true, l.saveLocked()
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
	it := Item{ID: newID(), Question: q, Answer: a, Source: source, Created: now, Expires: exp, Verified: !strings.HasPrefix(source, "peer")}
	if strings.HasPrefix(source, "peer") {
		it.Peers = []string{source}
	}
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
			it := &l.items[i]
			it.Score += delta
			if strings.HasPrefix(it.Source, "peer") {
				switch {
				case delta > 0: // disetujui manusia -> terverifikasi & permanen
					it.Verified, it.Expires = true, time.Time{}
				case delta < 0:
					it.Verified = false
				}
			}
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

// ReviewItem: pengetahuan dari AI lain yang menunggu persetujuan manusia (active learning).
type ReviewItem struct {
	ID        string    `json:"id"`
	Question  string    `json:"question"`
	Answer    string    `json:"answer"` // dipotong 300 karakter
	Source    string    `json:"source"`
	Trust     string    `json:"trust"`
	Peers     []string  `json:"peers,omitempty"`
	Conflicts int       `json:"conflicts"`
	Created   time.Time `json:"created"`
	Expires   time.Time `json:"expires,omitempty"`
}

// Review mengembalikan antrean tinjauan, yang PALING TIDAK PASTI lebih dulu (uncertainty sampling):
// 1) ada konflik antar-AI, 2) hanya satu AI yang menjawab, 3) sudah konsensus. Lalu yang terlama.
func (l *Learner) Review(limit int) []ReviewItem {
	l.mu.Lock()
	defer l.mu.Unlock()
	_ = l.syncLocked()
	now := time.Now()
	var out []ReviewItem
	rank := map[string]int{}
	for _, it := range l.items {
		if !strings.HasPrefix(it.Source, "peer") || it.Verified || it.Score <= -2 || (!it.Expires.IsZero() && now.After(it.Expires)) {
			continue
		}
		a := []rune(it.Answer)
		if len(a) > 300 {
			a = append(a[:300], '…')
		}
		r := ReviewItem{ID: it.ID, Question: it.Question, Answer: string(a), Source: it.Source, Trust: it.Trust(), Peers: it.Peers, Conflicts: it.Conflicts, Created: it.Created, Expires: it.Expires}
		switch {
		case it.Conflicts > 0:
			rank[it.ID] = 0
		case len(it.Peers) < 2:
			rank[it.ID] = 1
		default:
			rank[it.ID] = 2
		}
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if rank[out[i].ID] != rank[out[j].ID] {
			return rank[out[i].ID] < rank[out[j].ID]
		}
		return out[i].Created.Before(out[j].Created)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// LearnStats: ringkasan tingkat kepercayaan memori.
func (l *Learner) LearnStats() map[string]int {
	l.mu.Lock()
	defer l.mu.Unlock()
	_ = l.syncLocked()
	m := map[string]int{"pengguna": 0, "terverifikasi": 0, "konsensus": 0, "belum-diverifikasi": 0}
	for _, it := range l.items {
		m[it.Trust()]++
	}
	return m
}
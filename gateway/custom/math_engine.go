package custom

import (
	"errors"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"strings"
	"unicode"
)

// ===================== Teks: TF-IDF, Cosine, Levenshtein, Softmax =====================

var stopwords = map[string]bool{
	"yang": true, "dan": true, "untuk": true, "dengan": true, "dari": true, "itu": true, "ini": true,
	"apa": true, "adalah": true, "saya": true, "kamu": true, "ada": true, "di": true, "ke": true,
	"the": true, "and": true, "for": true, "that": true, "this": true, "with": true, "what": true,
	"how": true, "are": true, "you": true, "can": true, "is": true, "of": true, "to": true, "in": true,
}

// Tokenize memecah teks menjadi kata huruf kecil tanpa stopword.
func Tokenize(s string) []string {
	f := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	out := make([]string, 0, len(f))
	for _, t := range f {
		if len([]rune(t)) >= 2 && !stopwords[t] {
			out = append(out, t)
		}
	}
	return out
}

// Corpus menyimpan document frequency untuk TF-IDF.
type Corpus struct {
	n  int
	df map[string]int
}

func NewCorpus(docs [][]string) *Corpus {
	c := &Corpus{n: len(docs), df: map[string]int{}}
	for _, d := range docs {
		seen := map[string]bool{}
		for _, t := range d {
			if !seen[t] {
				seen[t] = true
				c.df[t]++
			}
		}
	}
	return c
}

// IDF ter-smooth: log((N+1)/(df+1)) + 1 (selalu > 0, stabil untuk korpus kecil).
func (c *Corpus) IDF(t string) float64 {
	return math.Log(float64(c.n+1)/float64(c.df[t]+1)) + 1
}

// Vector menghitung bobot TF-IDF: (1+ln tf) * idf.
func (c *Corpus) Vector(tokens []string) map[string]float64 {
	tf := map[string]int{}
	for _, t := range tokens {
		tf[t]++
	}
	v := make(map[string]float64, len(tf))
	for t, n := range tf {
		v[t] = (1 + math.Log(float64(n))) * c.IDF(t)
	}
	return v
}

// Cosine similarity dua vektor jarang.
func Cosine(a, b map[string]float64) float64 {
	var dot, na, nb float64
	for t, x := range a {
		na += x * x
		if y, ok := b[t]; ok {
			dot += x * y
		}
	}
	for _, y := range b {
		nb += y * y
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// Levenshtein menghitung jarak edit antar string (per rune).
func Levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	if len(ra) == 0 {
		return len(rb)
	}
	if len(rb) == 0 {
		return len(ra)
	}
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(min(prev[j]+1, cur[j-1]+1), prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}

// typoDistance = Levenshtein + transposisi huruf bersebelahan bernilai 1 (optimal string alignment).
func typoDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	d := make([][]int, len(ra)+1)
	for i := range d {
		d[i] = make([]int, len(rb)+1)
		d[i][0] = i
	}
	for j := 0; j <= len(rb); j++ {
		d[0][j] = j
	}
	for i := 1; i <= len(ra); i++ {
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			d[i][j] = min(min(d[i-1][j]+1, d[i][j-1]+1), d[i-1][j-1]+cost)
			if i > 1 && j > 1 && ra[i-1] == rb[j-2] && ra[i-2] == rb[j-1] {
				d[i][j] = min(d[i][j], d[i-2][j-2]+1)
			}
		}
	}
	return d[len(ra)][len(rb)]
}

// LevSimilarity: 1 - jarak/panjang_maks, rentang 0..1.
func LevSimilarity(a, b string) float64 {
	a, b = strings.ToLower(strings.TrimSpace(a)), strings.ToLower(strings.TrimSpace(b))
	m := max(len([]rune(a)), len([]rune(b)))
	if m == 0 {
		return 1
	}
	return 1 - float64(Levenshtein(a, b))/float64(m)
}

// Softmax menormalkan skor menjadi probabilitas 0..1 (jumlah = 1); temp>0.
func Softmax(xs []float64, temp float64) []float64 {
	if len(xs) == 0 {
		return nil
	}
	if temp <= 0 {
		temp = 1
	}
	mx := xs[0]
	for _, x := range xs {
		mx = math.Max(mx, x)
	}
	out := make([]float64, len(xs))
	var sum float64
	for i, x := range xs {
		out[i] = math.Exp((x - mx) / temp)
		sum += out[i]
	}
	for i := range out {
		out[i] /= sum
	}
	return out
}

// correctTokens memperbaiki salah ketik per kata: kata yang tidak ada di kosakata diganti kata
// terdekat (jarak Levenshtein <=1 untuk kata >=5 huruf, <=2 untuk >=8 huruf). Kata pendek tidak
// disentuh agar "buka" tidak berubah menjadi kata lain.
func correctTokens(q []string, vocab map[string]bool) []string {
	out := make([]string, len(q))
	for i, t := range q {
		out[i] = t
		if vocab[t] {
			continue
		}
		limit := 0
		switch n := len([]rune(t)); {
		case n >= 8:
			limit = 2
		case n >= 5:
			limit = 1
		}
		if limit == 0 {
			continue
		}
		best, bd := "", limit+1
		for v := range vocab {
			if d := typoDistance(t, v); d < bd || (d == bd && v < best) {
				best, bd = v, d
			}
		}
		if best != "" && bd <= limit {
			out[i] = best
		}
	}
	return out
}

// QuestionScores memberi skor kemiripan query terhadap kandidat pertanyaan (0..1):
// cosine TF-IDF setelah perbaikan salah ketik. Sengaja KETAT: kemiripan berbasis kata tidak bisa
// membedakan "jam buka" dari "jam tutup" atau "Warung Biru" dari "Warung Merah", sehingga
// hanya pertanyaan yang (hampir) sama yang mendapat skor tinggi.
func QuestionScores(query string, cands []string) []float64 {
	docs := make([][]string, 0, len(cands))
	vocab := map[string]bool{}
	for _, c := range cands {
		toks := Tokenize(c)
		docs = append(docs, toks)
		for _, t := range toks {
			vocab[t] = true
		}
	}
	corpus := NewCorpus(docs)
	qv := corpus.Vector(correctTokens(Tokenize(query), vocab))
	out := make([]float64, len(cands))
	for i := range cands {
		out[i] = Cosine(qv, corpus.Vector(docs[i]))
	}
	return out
}

// ===================== Keuangan kuantitatif =====================

// EMA: exponential moving average, alpha = 2/(period+1). Panjang output = input.
func EMA(xs []float64, period int) []float64 {
	if len(xs) == 0 || period < 1 {
		return nil
	}
	a := 2.0 / float64(period+1)
	out := make([]float64, len(xs))
	out[0] = xs[0]
	for i := 1; i < len(xs); i++ {
		out[i] = a*xs[i] + (1-a)*out[i-1]
	}
	return out
}

// RSI (Wilder smoothing) nilai terakhir, 0..100.
func RSI(xs []float64, period int) (float64, error) {
	if period < 1 || len(xs) <= period {
		return 0, errors.New("data tidak cukup untuk RSI")
	}
	p := float64(period)
	var gain, loss float64
	for i := 1; i <= period; i++ {
		d := xs[i] - xs[i-1]
		if d > 0 {
			gain += d
		} else {
			loss -= d
		}
	}
	gain /= p
	loss /= p
	for i := period + 1; i < len(xs); i++ {
		d := xs[i] - xs[i-1]
		g, l := 0.0, 0.0
		if d > 0 {
			g = d
		} else {
			l = -d
		}
		gain = (gain*(p-1) + g) / p
		loss = (loss*(p-1) + l) / p
	}
	if loss == 0 {
		if gain == 0 {
			return 50, nil
		}
		return 100, nil
	}
	return 100 - 100/(1+gain/loss), nil
}

func LogReturns(prices []float64) []float64 {
	if len(prices) < 2 {
		return nil
	}
	out := make([]float64, 0, len(prices)-1)
	for i := 1; i < len(prices); i++ {
		out = append(out, math.Log(prices[i]/prices[i-1]))
	}
	return out
}

// MeanStd: rata-rata dan simpangan baku sampel.
func MeanStd(xs []float64) (mean, std float64) {
	if len(xs) == 0 {
		return 0, 0
	}
	for _, x := range xs {
		mean += x
	}
	mean /= float64(len(xs))
	if len(xs) < 2 {
		return mean, 0
	}
	var ss float64
	for _, x := range xs {
		ss += (x - mean) * (x - mean)
	}
	return mean, math.Sqrt(ss / float64(len(xs)-1))
}

// EstimateGBM mengestimasi drift mu dan volatilitas sigma per periode dari harga historis.
// Log-return r ~ N(mu - sigma^2/2, sigma^2)  =>  mu = mean(r) + sigma^2/2.
func EstimateGBM(prices []float64) (mu, sigma float64) {
	m, s := MeanStd(LogReturns(prices))
	return m + s*s/2, s
}

// SimulateGBM menghasilkan satu lintasan harga: S(t+1) = S(t) * exp((mu - sigma^2/2) + sigma*Z).
func SimulateGBM(s0, mu, sigma float64, steps int, rng *rand.Rand) []float64 {
	path := make([]float64, steps+1)
	path[0] = s0
	for i := 1; i <= steps; i++ {
		path[i] = path[i-1] * math.Exp(mu-sigma*sigma/2+sigma*rng.NormFloat64())
	}
	return path
}

// MCResult adalah ringkasan distribusi harga akhir hasil Monte Carlo.
type MCResult struct {
	Paths, Steps                 int
	Mean, P5, P25, P50, P75, P95 float64
	ProbUp                       float64 // P(harga akhir > harga awal)
	ExpReturn                    float64 // rata-rata return sederhana
	VaR95                        float64 // kerugian (fraksi) yang tidak terlampaui pada 95% skenario
	CVaR95                       float64 // rata-rata kerugian di 5% skenario terburuk
}

// MonteCarlo mensimulasikan harga akhir GBM (solusi eksak) dengan variat antitetik.
// Seed tetap -> hasil dapat direproduksi.
func MonteCarlo(s0, mu, sigma float64, steps, paths int, seed int64) MCResult {
	if paths < 2 {
		paths = 2
	}
	rng := rand.New(rand.NewSource(seed))
	t := float64(steps)
	drift := (mu - sigma*sigma/2) * t
	vol := sigma * math.Sqrt(t)
	ends := make([]float64, paths)
	var z float64
	var sum float64
	up := 0
	for i := range ends {
		if i%2 == 0 {
			z = rng.NormFloat64()
		} else {
			z = -z
		}
		ends[i] = s0 * math.Exp(drift+vol*z)
		sum += ends[i]
		if ends[i] > s0 {
			up++
		}
	}
	sort.Float64s(ends)
	pct := func(p float64) float64 { return ends[int(p*float64(paths-1))] }
	r := MCResult{
		Paths: paths, Steps: steps, Mean: sum / float64(paths),
		P5: pct(0.05), P25: pct(0.25), P50: pct(0.5), P75: pct(0.75), P95: pct(0.95),
		ProbUp: float64(up) / float64(paths),
	}
	r.ExpReturn = r.Mean/s0 - 1
	r.VaR95 = math.Max(0, 1-r.P5/s0)
	tail := paths / 20
	if tail < 1 {
		tail = 1
	}
	var ts float64
	for _, v := range ends[:tail] {
		ts += v
	}
	r.CVaR95 = math.Max(0, 1-(ts/float64(tail))/s0)
	return r
}

func NormCDF(x float64) float64 { return 0.5 * (1 + math.Erf(x/math.Sqrt2)) }

// BlackScholes: harga opsi Eropa. t dalam tahun, r suku bunga bebas risiko, sigma tahunan.
func BlackScholes(s, k, t, r, sigma float64, call bool) float64 {
	if t <= 0 || sigma <= 0 {
		if call {
			return math.Max(0, s-k)
		}
		return math.Max(0, k-s)
	}
	d1 := (math.Log(s/k) + (r+sigma*sigma/2)*t) / (sigma * math.Sqrt(t))
	d2 := d1 - sigma*math.Sqrt(t)
	if call {
		return s*NormCDF(d1) - k*math.Exp(-r*t)*NormCDF(d2)
	}
	return k*math.Exp(-r*t)*NormCDF(-d2) - s*NormCDF(-d1)
}

// Forecast adalah hasil analisis kuantitatif satu aset.
type Forecast struct {
	N, Steps                int
	Last                    float64
	PeriodsPerYear          float64
	Mu, Sigma               float64 // per periode
	MuAnn, SigmaAnn         float64
	EMAFast, EMASlow, RSI14 float64
	Signal                  float64    // -1..1
	Trend                   string     // naik | turun | sideways
	Scenarios               [3]float64 // bullish, sideways, bearish (softmax)
	MC                      MCResult
	BSCall, BSPut           float64 // opsi ATM, r=0
}

// Analyze menggabungkan EMA, RSI, GBM, Monte Carlo, dan Softmax.
func Analyze(prices []float64, steps int, periodsPerYear float64, paths int) (*Forecast, error) {
	if len(prices) < 30 {
		return nil, fmt.Errorf("butuh minimal 30 titik data harga (ada %d)", len(prices))
	}
	for _, p := range prices {
		if p <= 0 || math.IsNaN(p) || math.IsInf(p, 0) {
			return nil, errors.New("data harga harus positif")
		}
	}
	if steps < 1 {
		steps = 30
	}
	if periodsPerYear <= 0 {
		periodsPerYear = 252
	}
	last := prices[len(prices)-1]
	f := &Forecast{N: len(prices), Steps: steps, Last: last, PeriodsPerYear: periodsPerYear}
	f.Mu, f.Sigma = EstimateGBM(prices)
	f.MuAnn, f.SigmaAnn = f.Mu*periodsPerYear, f.Sigma*math.Sqrt(periodsPerYear)
	f.EMAFast = EMA(prices, 12)[len(prices)-1]
	f.EMASlow = EMA(prices, 26)[len(prices)-1]
	f.RSI14, _ = RSI(prices, 14)
	seed := int64(math.Float64bits(last)) ^ int64(len(prices))
	f.MC = MonteCarlo(last, f.Mu, f.Sigma, steps, paths, seed)

	// Sinyal heuristik gabungan (-1..1): momentum EMA, RSI, dan probabilitas naik MC.
	emaSig := math.Tanh((f.EMAFast - f.EMASlow) / f.EMASlow * 20)
	rsiSig := math.Max(-1, math.Min(1, (f.RSI14-50)/50))
	mcSig := (f.MC.ProbUp - 0.5) * 2
	f.Signal = 0.4*emaSig + 0.3*rsiSig + 0.3*mcSig
	sm := Softmax([]float64{3 * f.Signal, 0, -3 * f.Signal}, 1)
	copy(f.Scenarios[:], sm)
	switch {
	case f.Signal > 0.15:
		f.Trend = "naik"
	case f.Signal < -0.15:
		f.Trend = "turun"
	default:
		f.Trend = "sideways"
	}
	ty := float64(steps) / periodsPerYear
	f.BSCall = BlackScholes(last, last, ty, 0, f.SigmaAnn, true)
	f.BSPut = BlackScholes(last, last, ty, 0, f.SigmaAnn, false)
	return f, nil
}

func fmtPrice(v float64) string {
	switch {
	case math.Abs(v) >= 100:
		return fmt.Sprintf("%.2f", v)
	case math.Abs(v) >= 1:
		return fmt.Sprintf("%.4f", v)
	default:
		return fmt.Sprintf("%.6f", v)
	}
}

// Summary: jawaban utama (bahasa alami ringkas).
func (f *Forecast) Summary(name, unit string) string {
	return fmt.Sprintf(
		"Analisis %s (%d titik data historis, harga terakhir %s). Proyeksi %d %s ke depan: tren condong %s. "+
			"Peluang harga akhir lebih tinggi dari sekarang ≈ %.0f%% (Monte Carlo, %d simulasi); "+
			"skenario: naik %.0f%%, sideways %.0f%%, turun %.0f%%. Rentang 90%% harga akhir: %s – %s. "+
			"Ini estimasi statistik dari data historis, bukan jaminan dan bukan nasihat keuangan.",
		name, f.N, fmtPrice(f.Last), f.Steps, unit, f.Trend,
		f.MC.ProbUp*100, f.MC.Paths,
		f.Scenarios[0]*100, f.Scenarios[1]*100, f.Scenarios[2]*100,
		fmtPrice(f.MC.P5), fmtPrice(f.MC.P95))
}

// Reference: jawaban alternatif — rincian rumus dan parameter.
func (f *Forecast) Reference(name string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Referensi matematis %s\n", name)
	fmt.Fprintf(&b, "• GBM: S(t+1)=S(t)·exp((μ−σ²/2)+σZ); μ=%.5f, σ=%.5f per periode (tahunan: μ=%.2f%%, σ=%.2f%%)\n",
		f.Mu, f.Sigma, f.MuAnn*100, f.SigmaAnn*100)
	fmt.Fprintf(&b, "• EMA12=%s, EMA26=%s → %s; RSI14=%.1f\n", fmtPrice(f.EMAFast), fmtPrice(f.EMASlow),
		map[bool]string{true: "momentum naik (EMA cepat > lambat)", false: "momentum turun/netral (EMA cepat ≤ lambat)"}[f.EMAFast > f.EMASlow], f.RSI14)
	fmt.Fprintf(&b, "• Monte Carlo (%d jalur, %d periode): rata-rata %s; P5=%s, P25=%s, P50=%s, P75=%s, P95=%s\n",
		f.MC.Paths, f.MC.Steps, fmtPrice(f.MC.Mean), fmtPrice(f.MC.P5), fmtPrice(f.MC.P25), fmtPrice(f.MC.P50), fmtPrice(f.MC.P75), fmtPrice(f.MC.P95))
	fmt.Fprintf(&b, "• Return harapan %.2f%%; VaR95 %.2f%%; CVaR95 %.2f%%\n", f.MC.ExpReturn*100, f.MC.VaR95*100, f.MC.CVaR95*100)
	fmt.Fprintf(&b, "• Softmax(3·sinyal, 0, −3·sinyal) dengan sinyal gabungan %.2f → naik %.1f%% / sideways %.1f%% / turun %.1f%%\n",
		f.Signal, f.Scenarios[0]*100, f.Scenarios[1]*100, f.Scenarios[2]*100)
	fmt.Fprintf(&b, "• Black-Scholes opsi ATM (K=harga terakhir, r=0, σ tahunan): call=%s, put=%s", fmtPrice(f.BSCall), fmtPrice(f.BSPut))
	return b.String()
}
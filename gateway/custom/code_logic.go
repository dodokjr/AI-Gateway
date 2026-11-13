package custom

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"regexp"
	"strings"
	"unicode"
)

// Logika pemrograman yang deterministik (tanpa model bahasa, tanpa mengeksekusi kode pengguna):
//  1. merapikan + memeriksa sintaks kode Go (gofmt) dan JSON,
//  2. tabel kebenaran / tautologi / ekuivalensi ekspresi boolean,
//  3. pustaka algoritma terverifikasi (Go, Python, JavaScript).
// Permintaan di luar itu dilewatkan ke AI lain (failover).

var (
	fenceRe     = regexp.MustCompile("(?s)```([A-Za-z0-9+#_-]*)[ \\t]*\\r?\\n(.*?)```")
	formatCueRe = rx(`\b(rapikan|rapihkan|format(?:kan)?|beautify|prettify|gofmt|periksa sintaks|cek sintaks|periksa syntax|cek syntax|syntax check|check syntax|validasi json|validate json)\b`)
	truthCueRe  = rx(`tabel kebenaran|truth table|tautologi|tautology|kontradiksi|contradiction|ekuivalen|equivalent|≡`)
	makeCueRe   = rx(`\b(buat(?:kan)?|tulis(?:kan)?|contoh|kode|code|fungsi|function|program|algoritma|algorithm|implementasi|implement|write|create|show|tunjukkan|berikan|give)\b`)

	langGoRe     = rx(`golang|\b(?:in|dalam|bahasa|pakai|dengan|using|with|di)\s+go\b|\bgo\s+(?:code|func|function|program|lang)\b`)
	langPyRe     = rx(`python|\bpy\b`)
	langJSRe     = rx(`javascript|\bjs\b|node\.?js|\bnode\b`)
	langOtherRe  = rx(`\b(java|kotlin|swift|rust|php|ruby|typescript|c\+\+|c#|csharp|dart|scala|perl|lua|haskell|elixir|cobol|fortran|bash|powershell|sql)\b`)
	promptWordRe = rx(`\b(apakah|apa|is|are|cek|periksa|check|buktikan|prove|bahwa|that|tolong|please|buatkan|buat|dari|untuk|of|for|dengan|pernyataan|ekspresi|expression|statement|berikut|logika|boolean|the|sama|dan|and)\b`)
)

// IsCodeIntent: apakah pesan meminta salah satu kemampuan di atas.
func IsCodeIntent(q string) bool {
	if truthCueRe.MatchString(q) {
		return true
	}
	if formatCueRe.MatchString(q) && (strings.Contains(q, "```") || strings.Contains(q, "{") || strings.Contains(q, "func ") || strings.Contains(q, "package ")) {
		return true
	}
	if makeCueRe.MatchString(q) {
		for _, s := range snippets {
			if s.Re.MatchString(q) {
				return true
			}
		}
	}
	return false
}

func (e *Engine) code(q string) (*Reply, error) {
	switch {
	case truthCueRe.MatchString(q):
		return truthReply(q)
	case formatCueRe.MatchString(q):
		return formatReply(q)
	}
	return snippetReply(q)
}

// ---------- pustaka algoritma ----------

func snippetReply(q string) (*Reply, error) {
	lang := "go"
	switch {
	case langPyRe.MatchString(q):
		lang = "python"
	case langJSRe.MatchString(q):
		lang = "javascript"
	case !langGoRe.MatchString(q):
		if m := langOtherRe.FindString(q); m != "" {
			return nil, fmt.Errorf("bahasa %q belum ada di pustaka (tersedia: Go, Python, JavaScript)", m)
		}
	}
	for _, s := range snippets {
		if !s.Re.MatchString(q) {
			continue
		}
		code := s.Code[lang]
		fence := map[string]string{"go": "go", "python": "python", "javascript": "javascript"}[lang]
		text := fmt.Sprintf("%s (%s)\n\n```%s\n%s```\n\nKeluaran bila dijalankan:\n```\n%s\n```\n%s",
			s.Title, map[string]string{"go": "Go", "python": "Python", "javascript": "JavaScript"}[lang], fence, code, s.Output, s.Complexity)
		return &Reply{Text: text, Secondary: s.Notes + " Kode ini dari pustaka bawaan yang sudah diuji; ia tidak dijalankan di server.",
			Meta: map[string]any{"source": "code-library", "topic": s.Key, "language": lang, "confidence": 1.0}}, nil
	}
	return nil, errors.New("algoritma tidak ada di pustaka")
}

// ---------- merapikan & memeriksa kode ----------

func extractCode(q string) (lang, code string) {
	if m := fenceRe.FindStringSubmatch(q); m != nil {
		return strings.ToLower(m[1]), m[2]
	}
	if i := strings.IndexAny(q, "\n"); i >= 0 {
		code = q[i+1:]
	} else if i := strings.Index(q, ":"); i >= 0 {
		code = q[i+1:]
	}
	code = strings.TrimSpace(code)
	switch {
	case strings.HasPrefix(code, "{") || strings.HasPrefix(code, "["):
		return "json", code
	case strings.Contains(code, "func ") || strings.Contains(code, "package "):
		return "go", code
	}
	return "", code
}

func formatReply(q string) (*Reply, error) {
	lang, code := extractCode(q)
	if strings.TrimSpace(code) == "" {
		return nil, errors.New("tidak ada kode untuk dirapikan (kirim dalam blok ```)")
	}
	if len(code) > 100_000 {
		return nil, errors.New("kode terlalu panjang (maks 100 KB)")
	}
	switch lang {
	case "go", "golang":
		return formatGo(code)
	case "json":
		return formatJSON(code)
	}
	return nil, fmt.Errorf("perapi hanya mendukung Go dan JSON (bahasa terdeteksi: %q)", lang)
}

func formatGo(code string) (*Reply, error) {
	out, err := format.Source([]byte(code))
	if err != nil {
		return &Reply{Text: "Sintaks Go TIDAK valid, kode belum bisa dirapikan:\n" + cleanGoErr(err),
			Meta: map[string]any{"source": "gofmt", "valid": false, "confidence": 1.0}}, nil
	}
	changed := countChangedLines(code, string(out))
	status := "Sintaks valid. Kode sudah rapi (tidak ada perubahan)."
	if changed > 0 {
		status = fmt.Sprintf("Sintaks valid. Dirapikan dengan gofmt (%d baris berubah).", changed)
	}
	warn := goWarnings(code)
	sec := "Tidak ada peringatan statis."
	if len(warn) > 0 {
		sec = "Peringatan:\n• " + strings.Join(warn, "\n• ")
	}
	return &Reply{Text: status + "\n\n```go\n" + strings.TrimRight(string(out), "\n") + "\n```", Secondary: sec,
		Meta: map[string]any{"source": "gofmt", "valid": true, "changed_lines": changed, "warnings": len(warn), "confidence": 1.0}}, nil
}

func cleanGoErr(err error) string {
	s := strings.ReplaceAll(err.Error(), "<standard input>:", "baris ")
	if len(s) > 800 {
		s = s[:800] + "…"
	}
	return s
}

func countChangedLines(a, b string) int {
	la, lb := strings.Split(strings.TrimRight(a, "\n"), "\n"), strings.Split(strings.TrimRight(b, "\n"), "\n")
	n := 0
	for i := 0; i < max(len(la), len(lb)); i++ {
		var x, y string
		if i < len(la) {
			x = la[i]
		}
		if i < len(lb) {
			y = lb[i]
		}
		if x != y {
			n++
		}
	}
	return n
}

// goWarnings: pemeriksaan statis sederhana yang pasti benar (impor yang tidak dipakai).
func goWarnings(code string) []string {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", code, parser.ParseComments)
	if err != nil {
		f, err = parser.ParseFile(fset, "x.go", "package p\n"+code, parser.ParseComments)
		if err != nil {
			return nil
		}
	}
	used := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		if se, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := se.X.(*ast.Ident); ok {
				used[id.Name] = true
			}
		}
		return true
	})
	var w []string
	for _, im := range f.Imports {
		path := strings.Trim(im.Path.Value, `"`)
		name := path[strings.LastIndex(path, "/")+1:]
		if im.Name != nil {
			if im.Name.Name == "_" || im.Name.Name == "." {
				continue
			}
			name = im.Name.Name
		}
		if i := strings.LastIndex(path, "/"); i >= 0 && regexp.MustCompile(`^v\d+$`).MatchString(name) {
			name = path[:i][strings.LastIndex(path[:i], "/")+1:]
		}
		if !used[name] {
			w = append(w, fmt.Sprintf("impor %q tidak dipakai (Go menolak kompilasi)", path))
		}
	}
	return w
}

func formatJSON(code string) (*Reply, error) {
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(code), "", "  "); err != nil {
		return &Reply{Text: "JSON TIDAK valid: " + err.Error(), Meta: map[string]any{"source": "json", "valid": false, "confidence": 1.0}}, nil
	}
	return &Reply{Text: "JSON valid. Hasil format (indentasi 2 spasi):\n\n```json\n" + buf.String() + "\n```",
		Meta: map[string]any{"source": "json", "valid": true, "confidence": 1.0}}, nil
}

// ---------- logika boolean ----------

type bexpr interface {
	eval(env map[string]bool) bool
	String() string
}

type (
	bvar   string
	bconst bool
	bnot   struct{ x bexpr }
	bbin   struct {
		op   string // ∧ ∨ ⊕ → ↔
		l, r bexpr
	}
)

func (v bvar) eval(env map[string]bool) bool { return env[string(v)] }
func (v bvar) String() string                { return string(v) }
func (c bconst) eval(map[string]bool) bool   { return bool(c) }
func (c bconst) String() string {
	if c {
		return "1"
	}
	return "0"
}
func (n bnot) eval(env map[string]bool) bool { return !n.x.eval(env) }
func (n bnot) String() string                { return "¬" + n.x.String() }
func (b bbin) eval(env map[string]bool) bool {
	l, r := b.l.eval(env), b.r.eval(env)
	switch b.op {
	case "∧":
		return l && r
	case "∨":
		return l || r
	case "⊕":
		return l != r
	case "→":
		return !l || r
	default: // ↔
		return l == r
	}
}
func (b bbin) String() string { return "(" + b.l.String() + " " + b.op + " " + b.r.String() + ")" }

type btok struct {
	kind string // var const op not lp rp
	val  string
	pos  int
}

func lexBool(s string) ([]btok, error) {
	rs := []rune(s)
	var out []btok
	for i := 0; i < len(rs); {
		c := rs[i]
		rest := string(rs[i:])
		switch {
		case unicode.IsSpace(c):
			i++
		case c == '(':
			out = append(out, btok{"lp", "(", i})
			i++
		case c == ')':
			out = append(out, btok{"rp", ")", i})
			i++
		case strings.HasPrefix(rest, "<->") || strings.HasPrefix(rest, "<=>"):
			out = append(out, btok{"op", "↔", i})
			i += 3
		case c == '↔':
			out = append(out, btok{"op", "↔", i})
			i++
		case strings.HasPrefix(rest, "->") || strings.HasPrefix(rest, "=>"):
			out = append(out, btok{"op", "→", i})
			i += 2
		case c == '→':
			out = append(out, btok{"op", "→", i})
			i++
		case strings.HasPrefix(rest, "&&"), strings.HasPrefix(rest, "||"):
			op := map[string]string{"&&": "∧", "||": "∨"}[rest[:2]]
			out = append(out, btok{"op", op, i})
			i += 2
		case c == '&' || c == '∧':
			out = append(out, btok{"op", "∧", i})
			i++
		case c == '|' || c == '∨' || c == '+':
			out = append(out, btok{"op", "∨", i})
			i++
		case c == '^' || c == '⊕':
			out = append(out, btok{"op", "⊕", i})
			i++
		case c == '!' || c == '~' || c == '¬':
			out = append(out, btok{"not", "¬", i})
			i++
		case unicode.IsLetter(c) || c == '_' || unicode.IsDigit(c):
			j := i
			for j < len(rs) && (unicode.IsLetter(rs[j]) || unicode.IsDigit(rs[j]) || rs[j] == '_') {
				j++
			}
			w := string(rs[i:j])
			switch strings.ToLower(w) {
			case "and", "dan":
				out = append(out, btok{"op", "∧", i})
			case "or", "atau":
				out = append(out, btok{"op", "∨", i})
			case "xor":
				out = append(out, btok{"op", "⊕", i})
			case "implies", "maka":
				out = append(out, btok{"op", "→", i})
			case "iff":
				out = append(out, btok{"op", "↔", i})
			case "not", "tidak", "bukan":
				out = append(out, btok{"not", "¬", i})
			case "true", "benar", "1":
				out = append(out, btok{"const", "1", i})
			case "false", "salah", "0":
				out = append(out, btok{"const", "0", i})
			default:
				if len([]rune(w)) > 12 {
					return nil, fmt.Errorf("nama variabel %q terlalu panjang (maks 12 karakter)", w)
				}
				out = append(out, btok{"var", w, i})
			}
			i = j
		default:
			return nil, fmt.Errorf("karakter %q pada posisi %d tidak dikenal", string(c), i+1)
		}
	}
	return out, nil
}

type bparser struct {
	t    []btok
	p    int
	vars []string
	seen map[string]bool
	dep  int
}

func (p *bparser) peek() *btok {
	if p.p < len(p.t) {
		return &p.t[p.p]
	}
	return nil
}

// Prioritas (tinggi -> rendah): ¬, ∧, ⊕, ∨, →(kanan), ↔.
var binLevels = [][]string{{"↔"}, {"→"}, {"∨"}, {"⊕"}, {"∧"}}

func (p *bparser) parseLevel(l int) (bexpr, error) {
	if l == len(binLevels) {
		return p.parseUnary()
	}
	left, err := p.parseLevel(l + 1)
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		if t == nil || t.kind != "op" || t.val != binLevels[l][0] {
			return left, nil
		}
		p.p++
		var right bexpr
		if t.val == "→" { // asosiatif kanan
			right, err = p.parseLevel(l)
		} else {
			right, err = p.parseLevel(l + 1)
		}
		if err != nil {
			return nil, err
		}
		left = bbin{t.val, left, right}
	}
}

func (p *bparser) parseUnary() (bexpr, error) {
	t := p.peek()
	if t == nil {
		return nil, errors.New("ekspresi berakhir terlalu cepat")
	}
	p.dep++
	defer func() { p.dep-- }()
	if p.dep > 60 {
		return nil, errors.New("ekspresi terlalu bersarang")
	}
	switch t.kind {
	case "not":
		p.p++
		x, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return bnot{x}, nil
	case "lp":
		p.p++
		x, err := p.parseLevel(0)
		if err != nil {
			return nil, err
		}
		if n := p.peek(); n == nil || n.kind != "rp" {
			return nil, errors.New("tanda kurung tutup ')' hilang")
		}
		p.p++
		return x, nil
	case "var":
		p.p++
		if !p.seen[t.val] {
			p.seen[t.val] = true
			p.vars = append(p.vars, t.val)
		}
		return bvar(t.val), nil
	case "const":
		p.p++
		return bconst(t.val == "1"), nil
	}
	return nil, fmt.Errorf("token %q tak terduga pada posisi %d", t.val, t.pos+1)
}

func parseBool(s string, seen map[string]bool, vars *[]string) (bexpr, error) {
	toks, err := lexBool(s)
	if err != nil {
		return nil, err
	}
	if len(toks) == 0 {
		return nil, errors.New("ekspresi kosong")
	}
	if len(toks) > 200 {
		return nil, errors.New("ekspresi terlalu panjang")
	}
	p := &bparser{t: toks, seen: seen, vars: *vars}
	e, err := p.parseLevel(0)
	if err != nil {
		return nil, err
	}
	if p.p != len(toks) {
		return nil, fmt.Errorf("token %q tak terduga pada posisi %d (dua variabel berurutan tanpa operator?)", toks[p.p].val, toks[p.p].pos+1)
	}
	*vars = p.vars
	return e, nil
}

var equivSplitRe = rx(`\s*(?:ekuivalen(?:\s+dengan)?|equivalent(?:\s+to)?|≡)\s*`)

func cleanExpr(s string) string {
	s = strings.TrimSpace(s)
	s = regexp.MustCompile(`(?i)^(tabel kebenaran|truth table|tautologi|tautology|kontradiksi|contradiction)\b`).ReplaceAllString(s, "")
	s = regexp.MustCompile(`(?i)\b(tabel kebenaran|truth table|tautologi|tautology|kontradiksi|contradiction)\b`).ReplaceAllString(s, " ")
	s = promptWordRe.ReplaceAllStringFunc(s, func(w string) string {
		// "dan"/"and" adalah operator: jangan dibuang bila berada di antara dua operand
		if l := strings.ToLower(w); l == "dan" || l == "and" {
			return w
		}
		return " "
	})
	s = strings.TrimRight(strings.TrimSpace(s), "?.;: \t\n") // "!" di akhir bukan operator valid, tapi di awal adalah NOT
	s = strings.TrimRight(s, "!")
	return strings.TrimSpace(strings.TrimLeft(s, ":; \t\n"))
}

func truthReply(q string) (*Reply, error) {
	if len(q) > 2000 {
		return nil, errors.New("ekspresi terlalu panjang")
	}
	seen := map[string]bool{}
	var vars []string
	var exprs []bexpr
	if parts := equivSplitRe.Split(q, 2); len(parts) == 2 && strings.TrimSpace(parts[0]) != "" && strings.TrimSpace(parts[1]) != "" {
		for _, side := range parts {
			e, err := parseBool(cleanExpr(side), seen, &vars)
			if err != nil {
				return nil, fmt.Errorf("ekspresi tidak bisa dibaca: %w", err)
			}
			exprs = append(exprs, e)
		}
	} else {
		e, err := parseBool(cleanExpr(q), seen, &vars)
		if err != nil {
			return nil, fmt.Errorf("ekspresi tidak bisa dibaca: %w", err)
		}
		exprs = append(exprs, e)
	}
	if len(vars) > 6 {
		return nil, fmt.Errorf("terlalu banyak variabel (%d, maks 6)", len(vars))
	}
	n := len(vars)
	rows := 1 << n
	env := map[string]bool{}
	vals := make([][]bool, len(exprs))
	for i := range vals {
		vals[i] = make([]bool, rows)
	}
	for r := 0; r < rows; r++ {
		for k, v := range vars {
			env[v] = (r>>(n-1-k))&1 == 0 // baris pertama: semua benar
		}
		for i, e := range exprs {
			vals[i][r] = e.eval(env)
		}
	}
	bit := func(b bool) string {
		if b {
			return "1"
		}
		return "0"
	}
	var tb strings.Builder
	header := append([]string{}, vars...)
	for _, e := range exprs {
		header = append(header, e.String())
	}
	tb.WriteString(strings.Join(header[:n], " ") + " | " + strings.Join(header[n:], " | ") + "\n")
	for r := 0; r < rows; r++ {
		var cells []string
		for k := range vars {
			cells = append(cells, bit((r>>(n-1-k))&1 == 0))
		}
		var res []string
		for i := range exprs {
			w := len([]rune(header[n+i]))
			res = append(res, fmt.Sprintf("%-*s", w, bit(vals[i][r])))
		}
		tb.WriteString(strings.Join(cells, " ") + " | " + strings.Join(res, " | ") + "\n")
	}
	meta := map[string]any{"source": "logic", "variables": n, "rows": rows, "confidence": 1.0}
	if len(exprs) == 2 {
		diff := -1
		for r := 0; r < rows; r++ {
			if vals[0][r] != vals[1][r] {
				diff = r
				break
			}
		}
		var verdict string
		if diff < 0 {
			verdict = "Kedua ekspresi EKUIVALEN (hasilnya sama pada semua " + fmt.Sprint(rows) + " kombinasi)."
		} else {
			var asg []string
			for k, v := range vars {
				asg = append(asg, v+"="+bit((diff>>(n-1-k))&1 == 0))
			}
			verdict = "Kedua ekspresi TIDAK ekuivalen. Contoh penyangkal: " + strings.Join(asg, ", ") + "."
		}
		meta["equivalent"] = diff < 0
		return &Reply{Text: verdict + "\n\n```\n" + tb.String() + "```", Meta: meta}, nil
	}
	trues := 0
	var terms []string
	for r := 0; r < rows; r++ {
		if vals[0][r] {
			trues++
			var lits []string
			for k, v := range vars {
				if (r>>(n-1-k))&1 == 0 {
					lits = append(lits, v)
				} else {
					lits = append(lits, "¬"+v)
				}
			}
			terms = append(terms, "("+strings.Join(lits, " ∧ ")+")")
		}
	}
	kind := "kontingen (kadang benar, kadang salah)"
	dnf := strings.Join(terms, " ∨ ")
	switch {
	case trues == rows:
		kind, dnf = "TAUTOLOGI (selalu benar)", "1"
	case trues == 0:
		kind, dnf = "KONTRADIKSI (selalu salah)", "0"
	}
	meta["tautology"], meta["true_rows"] = trues == rows, trues
	return &Reply{Text: fmt.Sprintf("Ekspresi %s: %s (%d dari %d baris bernilai 1).\n\n```\n%s```", exprs[0], kind, trues, rows, tb.String()),
		Secondary: "Bentuk normal disjungtif kanonik (DNF): " + dnf + "\nUrutan operator (tinggi ke rendah): ¬, ∧, ⊕, ∨, →, ↔. Baris pertama = semua variabel bernilai 1.", Meta: meta}, nil
}
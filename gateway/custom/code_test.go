package custom

import (
	"context"
	"errors"
	"go/format"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSnippetsFormatted(t *testing.T) {
	for _, s := range snippets {
		for _, l := range []string{"go", "python", "javascript"} {
			c, ok := s.Code[l]
			if !ok || strings.TrimSpace(c) == "" {
				t.Fatalf("%s/%s: kode hilang", s.Key, l)
			}
		}
		src := s.Code["go"]
		out, err := format.Source([]byte(src))
		if err != nil {
			t.Fatalf("%s: Go tidak valid: %v", s.Key, err)
		}
		if string(out) != src {
			t.Fatalf("%s: kode Go belum gofmt", s.Key)
		}
		if w := goWarnings(src); len(w) > 0 {
			t.Fatalf("%s: %v", s.Key, w)
		}
	}
}

// Opt-in: CODE_RUN=1 go test -run TestSnippetsRun  (butuh go, python3, node)
func TestSnippetsRun(t *testing.T) {
	if os.Getenv("CODE_RUN") == "" {
		t.Skip("set CODE_RUN=1 untuk menjalankan semua potongan kode")
	}
	dir := t.TempDir()
	for _, s := range snippets {
		for l, ext := range map[string]string{"go": "go", "python": "py", "javascript": "js"} {
			f := filepath.Join(dir, s.Key+"_"+l+"."+ext)
			if err := os.WriteFile(f, []byte(s.Code[l]), 0o600); err != nil {
				t.Fatal(err)
			}
			var cmd *exec.Cmd
			switch l {
			case "go":
				cmd = exec.Command("go", "run", f)
			case "python":
				cmd = exec.Command("python3", "-I", f)
			default:
				cmd = exec.Command("node", f)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			out, err := exec.CommandContext(ctx, cmd.Path, cmd.Args[1:]...).CombinedOutput()
			cancel()
			if err != nil {
				t.Fatalf("%s/%s: %v\n%s", s.Key, l, err, out)
			}
			if got := strings.TrimSpace(string(out)); got != s.Output {
				t.Fatalf("%s/%s: keluaran %q, harapan %q", s.Key, l, got, s.Output)
			}
		}
	}
}

func TestCodeIntentAndSnippets(t *testing.T) {
	for _, q := range []string{"buatkan fungsi fibonacci dalam golang", "tulis kode merge sort python", "contoh binary search javascript", "buat program fizzbuzz", "Rapikan kode go ini:\n```go\nfunc a(){}\n```", "tabel kebenaran A and B"} {
		if !IsCodeIntent(q) {
			t.Fatalf("harus terdeteksi: %q", q)
		}
	}
	for _, q := range []string{"siapa presiden pertama", "cuaca di Bandung", "prediksi harga bitcoin", "buatkan fungsi login pengguna", "fibonacci itu apa"} {
		if IsCodeIntent(q) {
			t.Fatalf("bukan niat kode: %q", q)
		}
	}
	e := newTestEngine(t, nil)
	ctx := context.Background()
	for q, want := range map[string][2]string{
		"buatkan fungsi fibonacci":                {"fibonacci", "go"},
		"tulis kode faktorial dengan python":      {"factorial", "python"},
		"contoh gcd javascript":                   {"gcd", "javascript"},
		"buat fungsi bilangan prima dalam golang": {"prime", "go"},
		"tulis kode palindrom js":                 {"palindrome", "javascript"},
		"buatkan fizzbuzz python":                 {"fizzbuzz", "python"},
		"tulis kode merge sort":                   {"mergesort", "go"},
		"buat contoh binary search":               {"binarysearch", "go"},
	} {
		r, err := e.Respond(ctx, u(q))
		if err != nil || r.Meta["topic"] != want[0] || r.Meta["language"] != want[1] || !strings.Contains(r.Text, "```") {
			t.Fatalf("%q -> %+v %v", q, r, err)
		}
	}
	var d *Declined
	if _, err := e.Respond(ctx, u("buatkan fungsi fibonacci dalam rust")); !errors.As(err, &d) || !strings.Contains(d.Reason, "belum ada di pustaka") {
		t.Fatalf("bahasa tak ada harus Declined: %v", err)
	}
	if ShouldLearn("buatkan fungsi fibonacci dalam golang", "func Fibonacci(n int) int { return n } dan seterusnya sampai panjang") {
		t.Fatal("kode tidak boleh dipelajari")
	}
}

func TestFormatGoAndJSON(t *testing.T) {
	e := newTestEngine(t, nil)
	ctx := context.Background()
	r, err := e.Respond(ctx, u("Rapikan kode go ini:\n```go\npackage main\nimport (\"fmt\"\n\"os\")\nfunc main(){fmt.Println(  \"hi\")}\n```"))
	if err != nil || r.Meta["valid"] != true || r.Meta["changed_lines"].(int) == 0 || !strings.Contains(r.Text, "fmt.Println(\"hi\")") {
		t.Fatalf("format gagal: %+v %v", r, err)
	}
	if !strings.Contains(r.Secondary, `impor "os" tidak dipakai`) {
		t.Fatalf("impor tak terpakai harus dilaporkan: %q", r.Secondary)
	}
	r, _ = e.Respond(ctx, u("periksa sintaks kode go:\n```go\nfunc a( {\n```"))
	if r.Meta["valid"] != false || !strings.Contains(r.Text, "TIDAK valid") {
		t.Fatalf("sintaks salah harus dilaporkan: %+v", r)
	}
	r, _ = e.Respond(ctx, u("rapikan json ini\n```json\n{\"a\":1,\"b\":[1,2]}\n```"))
	if r.Meta["valid"] != true || !strings.Contains(r.Text, "  \"a\": 1") {
		t.Fatalf("json: %+v", r)
	}
	r, _ = e.Respond(ctx, u("validasi json ini\n```json\n{\"a\":}\n```"))
	if r.Meta["valid"] != false {
		t.Fatalf("json salah: %+v", r)
	}
	var d *Declined
	if _, err := e.Respond(ctx, u("rapikan kode ini\n```python\nx=1\n```")); !errors.As(err, &d) {
		t.Fatalf("bahasa tak didukung harus Declined: %v", err)
	}
}

func TestTruthTables(t *testing.T) {
	e := newTestEngine(t, nil)
	ctx := context.Background()
	r, err := e.Respond(ctx, u("buatkan tabel kebenaran untuk A and B"))
	if err != nil || r.Meta["rows"] != 4 || r.Meta["true_rows"] != 1 || !strings.Contains(r.Text, "(A ∧ B)") || !strings.Contains(r.Text, "kontingen") {
		t.Fatalf("A and B: %+v %v", r, err)
	}
	// baris pertama = semua benar
	if !strings.Contains(r.Text, "\n1 1 | 1") {
		t.Fatalf("baris pertama salah:\n%s", r.Text)
	}
	for q, taut := range map[string]bool{
		"apakah A || !A tautologi?":             true,
		"tabel kebenaran (A -> B) <-> (!A | B)": true,
		"tabel kebenaran A && !A":               false,
	} {
		r, err := e.Respond(ctx, u(q))
		if err != nil || r.Meta["tautology"] != taut {
			t.Fatalf("%q: %+v %v", q, r, err)
		}
	}
	r, err = e.Respond(ctx, u("apakah !(A && B) ekuivalen dengan !A || !B ?"))
	if err != nil || r.Meta["equivalent"] != true || !strings.Contains(r.Text, "EKUIVALEN") {
		t.Fatalf("De Morgan: %+v %v", r, err)
	}
	r, err = e.Respond(ctx, u("apakah A -> B ekuivalen dengan B -> A"))
	if err != nil || r.Meta["equivalent"] != false || !strings.Contains(r.Text, "penyangkal") {
		t.Fatalf("konversi: %+v %v", r, err)
	}
	// prioritas: A or B and C = A or (B and C); implikasi asosiatif kanan
	r, _ = e.Respond(ctx, u("tabel kebenaran A or B and C"))
	if !strings.Contains(r.Text, "(A ∨ (B ∧ C))") {
		t.Fatalf("prioritas salah: %s", r.Text)
	}
	r, _ = e.Respond(ctx, u("tabel kebenaran A -> B -> C"))
	if !strings.Contains(r.Text, "(A → (B → C))") {
		t.Fatalf("asosiatif kanan salah: %s", r.Text)
	}
	var d *Declined
	for _, q := range []string{"tabel kebenaran A and", "tabel kebenaran (A and B", "tabel kebenaran A B", "tabel kebenaran A and B and C and D and E and F and G"} {
		if _, err := e.Respond(ctx, u(q)); !errors.As(err, &d) {
			t.Fatalf("%q harus Declined: %v", q, err)
		}
	}
}
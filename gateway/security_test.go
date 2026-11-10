package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLimiter(t *testing.T) {
	l := newLimiter(60) // 1/dtk, burst 15
	t0 := time.Now()
	allowed := 0
	for i := 0; i < 40; i++ {
		if ok, _ := l.allow("1.1.1.1", t0); ok {
			allowed++
		}
	}
	if allowed != 15 {
		t.Fatalf("burst harus 15, dapat %d", allowed)
	}
	if ok, wait := l.allow("1.1.1.1", t0); ok || wait < time.Second {
		t.Fatalf("harus ditolak dengan waktu tunggu: %v %v", ok, wait)
	}
	if ok, _ := l.allow("2.2.2.2", t0); !ok {
		t.Fatal("klien lain tidak boleh terdampak")
	}
	if ok, _ := l.allow("1.1.1.1", t0.Add(3*time.Second)); !ok {
		t.Fatal("token harus terisi ulang")
	}
	if newLimiter(0) != nil {
		t.Fatal("0 = nonaktif")
	}
	small := newLimiter(60)
	small.maxKeys = 5
	for i := 0; i < 50; i++ {
		small.allow(string(rune('a'+i%26))+string(rune('A'+i)), t0)
	}
	if len(small.b) > 6 {
		t.Fatalf("memori limiter harus dibatasi: %d", len(small.b))
	}
}

func TestAuthLockoutAndHeaders(t *testing.T) {
	t.Setenv("RATE_LIMIT_PER_MIN", "0") // uji penguncian terpisah dari pembatas laju
	srv := &Server{apiKey: "rahasia-yang-panjang"}
	h := srv.routes()
	do := func(token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/v1/status", nil)
		r.RemoteAddr = "9.9.9.9:1234"
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	for i := 0; i < 10; i++ {
		if w := do("salah"); w.Code != 401 {
			t.Fatalf("percobaan %d: %d", i, w.Code)
		}
	}
	w := do("rahasia-yang-panjang") // kunci benar pun ditolak selama terkunci
	if w.Code != 429 || w.Header().Get("Retry-After") == "" {
		t.Fatalf("harus terkunci: %d %v", w.Code, w.Header())
	}
	// IP lain tetap bisa masuk dengan kunci benar
	r := httptest.NewRequest("GET", "/healthz", nil)
	r.RemoteAddr = "8.8.8.8:1"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	for k, want := range map[string]string{"X-Content-Type-Options": "nosniff", "X-Frame-Options": "DENY", "Referrer-Policy": "no-referrer"} {
		if rec.Header().Get(k) != want {
			t.Fatalf("header %s = %q", k, rec.Header().Get(k))
		}
	}
	if rec.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("CSP hilang")
	}
	if w := do(""); w.Code != 429 {
		t.Fatalf("tanpa kunci dari IP terkunci: %d", w.Code)
	}
}

func TestRateLimitMiddlewareAndFailClosed(t *testing.T) {
	t.Setenv("RATE_LIMIT_PER_MIN", "60")
	srv := NewServer(nil, nil, "", 0)
	h := srv.routes()
	got429 := false
	for i := 0; i < 40; i++ {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = "7.7.7.7:1"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code == 429 {
			got429 = true
		}
	}
	if !got429 {
		t.Fatal("pembatas laju harus menolak setelah burst")
	}
	// /healthz tidak dibatasi
	for i := 0; i < 40; i++ {
		r := httptest.NewRequest("GET", "/healthz", nil)
		r.RemoteAddr = "7.7.7.7:1"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("/healthz harus selalu 200, dapat %d", w.Code)
		}
	}
	fc := failClosed(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	for path, want := range map[string]int{"/healthz": 200, "/v1/chat": 503, "/": 503} {
		w := httptest.NewRecorder()
		fc.ServeHTTP(w, httptest.NewRequest("POST", path, nil))
		if w.Code != want {
			t.Fatalf("failClosed %s = %d, harapan %d", path, w.Code, want)
		}
	}
}

func TestClientIP(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.1:5555"
	r.Header.Set("X-Forwarded-For", "203.0.113.9, 10.0.0.1")
	t.Setenv("TRUST_PROXY", "")
	t.Setenv("VERCEL", "")
	t.Setenv("NETLIFY", "")
	t.Setenv("AWS_LAMBDA_FUNCTION_NAME", "")
	if ip := clientIP(r); ip != "10.0.0.1" {
		t.Fatalf("tanpa TRUST_PROXY header palsu harus diabaikan: %s", ip)
	}
	t.Setenv("TRUST_PROXY", "1")
	if ip := clientIP(r); ip != "203.0.113.9" {
		t.Fatalf("dengan TRUST_PROXY: %s", ip)
	}
	r.Header.Set("X-Forwarded-For", "bukan-ip")
	if ip := clientIP(r); ip != "10.0.0.1" {
		t.Fatalf("XFF tidak valid harus diabaikan: %s", ip)
	}
}

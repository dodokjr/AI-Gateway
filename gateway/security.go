package gateway

import (
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Lapisan keamanan HTTP: header pengaman, pembatas laju per klien, penguncian sementara setelah banyak
// kegagalan autentikasi (anti brute-force), dan gagal-tertutup bila GATEWAY_KEY kosong di serverless.

// ---------- IP klien ----------

func trustProxy() bool { return os.Getenv("TRUST_PROXY") == "1" || isServerless() }

func clientIP(r *http.Request) string {
	if trustProxy() {
		if v := r.Header.Get("X-Forwarded-For"); v != "" {
			if ip := strings.TrimSpace(strings.Split(v, ",")[0]); net.ParseIP(ip) != nil {
				return ip
			}
		}
		if v := strings.TrimSpace(r.Header.Get("X-Nf-Client-Connection-Ip")); net.ParseIP(v) != nil {
			return v
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ---------- pembatas laju (token bucket) ----------

type bucket struct {
	tokens float64
	last   time.Time
}

type limiter struct {
	mu      sync.Mutex
	b       map[string]*bucket
	perSec  float64
	burst   float64
	maxKeys int
}

func newLimiter(perMin int) *limiter {
	if perMin <= 0 {
		return nil
	}
	burst := float64(perMin) / 4
	if burst < 10 {
		burst = 10
	}
	return &limiter{b: map[string]*bucket{}, perSec: float64(perMin) / 60, burst: burst, maxKeys: 10000}
}

func (l *limiter) allow(key string, now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.b) >= l.maxKeys {
		for k, v := range l.b {
			if now.Sub(v.last) > 5*time.Minute {
				delete(l.b, k)
			}
		}
		if len(l.b) >= l.maxKeys { // serangan banyak-IP: mulai dari nol daripada kehabisan memori
			l.b = map[string]*bucket{}
		}
	}
	bk := l.b[key]
	if bk == nil {
		bk = &bucket{tokens: l.burst, last: now}
		l.b[key] = bk
	}
	bk.tokens = min(l.burst, bk.tokens+now.Sub(bk.last).Seconds()*l.perSec)
	bk.last = now
	if bk.tokens < 1 {
		return false, time.Duration((1-bk.tokens)/l.perSec*float64(time.Second)) + time.Second
	}
	bk.tokens--
	return true, 0
}

// ---------- penguncian setelah gagal autentikasi ----------

type failState struct {
	n           int
	first       time.Time
	lockedUntil time.Time
}

type failTracker struct {
	mu       sync.Mutex
	m        map[string]*failState
	max      int
	window   time.Duration
	lockTime time.Duration
}

func newFailTracker() *failTracker {
	return &failTracker{m: map[string]*failState{}, max: 10, window: 5 * time.Minute, lockTime: 5 * time.Minute}
}

func (f *failTracker) locked(ip string, now time.Time) (bool, time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s := f.m[ip]; s != nil && now.Before(s.lockedUntil) {
		return true, s.lockedUntil.Sub(now)
	}
	return false, 0
}

func (f *failTracker) fail(ip string, now time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.m) > 10000 {
		f.m = map[string]*failState{}
	}
	s := f.m[ip]
	if s == nil || now.Sub(s.first) > f.window {
		s = &failState{first: now}
		f.m[ip] = s
	}
	s.n++
	if s.n >= f.max {
		s.lockedUntil = now.Add(f.lockTime)
		s.n, s.first = 0, now
	}
}

func (f *failTracker) ok(ip string) {
	f.mu.Lock()
	delete(f.m, ip)
	f.mu.Unlock()
}

// ---------- middleware ----------

func rateLimitPerMin() int {
	if v := strings.TrimSpace(os.Getenv("RATE_LIMIT_PER_MIN")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return n
		}
	}
	return 60
}

func secHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'")
		if strings.HasPrefix(r.URL.Path, "/v1/") {
			h.Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) limit(next http.Handler) http.Handler {
	if s.rl == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			if ok, wait := s.rl.allow(clientIP(r), time.Now()); !ok {
				w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
				writeErr(w, http.StatusTooManyRequests, "terlalu banyak permintaan, coba lagi sebentar")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// failClosed: bila GATEWAY_KEY kosong di lingkungan publik (serverless), tolak semua kecuali /healthz.
func failClosed(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		writeErr(w, http.StatusServiceUnavailable, "GATEWAY_KEY belum diatur: server menolak semua permintaan demi keamanan (set GATEWAY_KEY, atau ALLOW_NO_AUTH=1 bila memang disengaja)")
	})
}
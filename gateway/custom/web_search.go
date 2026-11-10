package custom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// SearchResult adalah satu hasil pencarian web.
type SearchResult struct {
	Title, URL, Snippet, Engine string
}

// Searcher mencari di beberapa mesin secara berurutan (fallback) dan menggabungkan hasil.
//
// Mesin yang didukung:
//   - "duckduckgo": endpoint HTML publik, tanpa kunci (parser bisa rusak jika DDG mengubah HTML).
//   - "google": Google Programmable Search JSON API (GOOGLE_CSE_API_KEY + GOOGLE_CSE_CX).
//   - "brave": Brave Search API (BRAVE_API_KEY). Pengganti Bing, karena Bing Search API sudah dihentikan Microsoft.
//
// Scraping halaman hasil Google/Bing sengaja tidak dilakukan: melanggar ketentuan layanan dan cepat diblokir.
type Searcher struct {
	HTTP      *http.Client
	Engines   []string
	GoogleKey string
	GoogleCX  string
	BraveKey  string
	DDGURL    string
	GoogleURL string
	BraveURL  string
	UserAgent string
}

func NewSearcher(engines []string) *Searcher {
	if len(engines) == 0 {
		engines = []string{"duckduckgo"}
	}
	return &Searcher{
		HTTP:      &http.Client{Timeout: 8 * time.Second},
		Engines:   engines,
		GoogleKey: os.Getenv("GOOGLE_CSE_API_KEY"),
		GoogleCX:  os.Getenv("GOOGLE_CSE_CX"),
		BraveKey:  os.Getenv("BRAVE_API_KEY"),
		DDGURL:    "https://html.duckduckgo.com/html/",
		GoogleURL: "https://www.googleapis.com/customsearch/v1",
		BraveURL:  "https://api.search.brave.com/res/v1/web/search",
		UserAgent: "Mozilla/5.0 (compatible; ai-gateway/1.0)",
	}
}

// redact membuang URL (yang bisa memuat API key) dari error jaringan.
func redact(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return fmt.Errorf("%s: %w", ue.Op, ue.Err)
	}
	return err
}

func (s *Searcher) get(ctx context.Context, u string, headers map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, redact(err)
	}
	req.Header.Set("User-Agent", s.UserAgent)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return nil, redact(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return b, nil
}

// Search mencoba tiap mesin sampai terkumpul n hasil unik.
func (s *Searcher) Search(ctx context.Context, q string, n int) ([]SearchResult, error) {
	if n < 1 {
		n = 5
	}
	var all []SearchResult
	var errs []string
	seen := map[string]bool{}
	for _, e := range s.Engines {
		var rs []SearchResult
		var err error
		switch strings.ToLower(strings.TrimSpace(e)) {
		case "duckduckgo", "ddg":
			rs, err = s.ddg(ctx, q)
		case "google":
			rs, err = s.google(ctx, q, n)
		case "brave":
			rs, err = s.brave(ctx, q, n)
		default:
			err = errors.New("mesin tidak dikenal")
		}
		if err != nil {
			errs = append(errs, e+": "+err.Error())
			continue
		}
		for _, r := range rs {
			key := strings.TrimRight(strings.ToLower(r.URL), "/")
			if r.URL == "" || seen[key] {
				continue
			}
			seen[key] = true
			all = append(all, r)
		}
		if len(all) >= n {
			break
		}
	}
	if len(all) > n {
		all = all[:n]
	}
	if len(all) == 0 && len(errs) > 0 {
		return nil, errors.New(strings.Join(errs, "; "))
	}
	return all, nil
}

var (
	tagRe     = regexp.MustCompile(`<[^>]*>`)
	spaceRe   = regexp.MustCompile(`\s+`)
	ddgLinkRe = regexp.MustCompile(`(?s)<a[^>]+class="[^"]*result__a[^"]*"[^>]*href="([^"]+)"[^>]*>(.*?)</a>`)
	ddgSnipRe = regexp.MustCompile(`(?s)<a[^>]+class="[^"]*result__snippet[^"]*"[^>]*>(.*?)</a>`)
)

func cleanText(s string) string {
	return strings.TrimSpace(spaceRe.ReplaceAllString(html.UnescapeString(tagRe.ReplaceAllString(s, "")), " "))
}

func (s *Searcher) ddg(ctx context.Context, q string) ([]SearchResult, error) {
	b, err := s.get(ctx, s.DDGURL+"?q="+url.QueryEscape(q), nil)
	if err != nil {
		return nil, err
	}
	return parseDDG(string(b)), nil
}

func parseDDG(page string) []SearchResult {
	locs := ddgLinkRe.FindAllStringSubmatchIndex(page, -1)
	var out []SearchResult
	for i, m := range locs {
		href := html.UnescapeString(page[m[2]:m[3]])
		title := cleanText(page[m[4]:m[5]])
		end := len(page)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		snippet := ""
		if sm := ddgSnipRe.FindStringSubmatch(page[m[1]:end]); sm != nil {
			snippet = cleanText(sm[1])
		}
		u := resolveDDG(href)
		if u == "" || strings.Contains(u, "duckduckgo.com/y.js") {
			continue // iklan atau tautan tidak valid
		}
		out = append(out, SearchResult{Title: title, URL: u, Snippet: snippet, Engine: "duckduckgo"})
	}
	return out
}

func resolveDDG(href string) string {
	if strings.HasPrefix(href, "//") {
		href = "https:" + href
	}
	u, err := url.Parse(href)
	if err != nil {
		return ""
	}
	if t := u.Query().Get("uddg"); t != "" {
		return t
	}
	if u.Scheme == "http" || u.Scheme == "https" {
		return href
	}
	return ""
}

func (s *Searcher) google(ctx context.Context, q string, n int) ([]SearchResult, error) {
	if s.GoogleKey == "" || s.GoogleCX == "" {
		return nil, errors.New("belum dikonfigurasi (GOOGLE_CSE_API_KEY, GOOGLE_CSE_CX)")
	}
	if n > 10 {
		n = 10
	}
	u := s.GoogleURL + "?key=" + url.QueryEscape(s.GoogleKey) + "&cx=" + url.QueryEscape(s.GoogleCX) +
		"&num=" + strconv.Itoa(n) + "&q=" + url.QueryEscape(q)
	b, err := s.get(ctx, u, nil)
	if err != nil {
		return nil, err
	}
	var r struct {
		Items []struct{ Title, Link, Snippet string } `json:"items"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	out := make([]SearchResult, 0, len(r.Items))
	for _, it := range r.Items {
		out = append(out, SearchResult{Title: cleanText(it.Title), URL: it.Link, Snippet: cleanText(it.Snippet), Engine: "google"})
	}
	return out, nil
}

func (s *Searcher) brave(ctx context.Context, q string, n int) ([]SearchResult, error) {
	if s.BraveKey == "" {
		return nil, errors.New("belum dikonfigurasi (BRAVE_API_KEY)")
	}
	b, err := s.get(ctx, s.BraveURL+"?count="+strconv.Itoa(n)+"&q="+url.QueryEscape(q),
		map[string]string{"X-Subscription-Token": s.BraveKey, "Accept": "application/json"})
	if err != nil {
		return nil, err
	}
	var r struct {
		Web struct {
			Results []struct{ Title, URL, Description string } `json:"results"`
		} `json:"web"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	out := make([]SearchResult, 0, len(r.Web.Results))
	for _, it := range r.Web.Results {
		out = append(out, SearchResult{Title: cleanText(it.Title), URL: it.URL, Snippet: cleanText(it.Description), Engine: "brave"})
	}
	return out, nil
}

// FormatHint menyusun konteks teks dari hasil pencarian (dibatasi panjangnya).
func FormatHint(rs []SearchResult, maxChars int) string {
	var b strings.Builder
	for i, r := range rs {
		line := fmt.Sprintf("[%d] %s — %s (%s)\n", i+1, r.Title, r.Snippet, r.URL)
		if b.Len()+len(line) > maxChars {
			break
		}
		b.WriteString(line)
	}
	return strings.TrimSpace(b.String())
}

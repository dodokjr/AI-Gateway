package custom

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Asset menjelaskan aset yang bisa diambil datanya.
type Asset struct {
	Name           string
	Kind           string // "stooq" | "coingecko"
	ID             string
	PeriodsPerYear float64 // 365 untuk crypto, 252 untuk pasar lain
}

// MarketClient mengambil harga historis harian.
//   - Stooq (CSV, tanpa kunci): saham, indeks, forex, komoditas.
//   - CoinGecko (JSON): crypto. Kunci demo opsional lewat COINGECKO_API_KEY.
type MarketClient struct {
	HTTP         *http.Client
	StooqURL     string
	CoinGeckoURL string
	CGKey        string
}

func NewMarketClient() *MarketClient {
	return &MarketClient{
		HTTP:         &http.Client{Timeout: 10 * time.Second},
		StooqURL:     "https://stooq.com/q/d/l/",
		CoinGeckoURL: "https://api.coingecko.com/api/v3",
		CGKey:        os.Getenv("COINGECKO_API_KEY"),
	}
}

var cryptoAliases = map[string]string{
	"bitcoin": "bitcoin", "btc": "bitcoin", "ethereum": "ethereum", "eth": "ethereum",
	"solana": "solana", "xrp": "ripple", "ripple": "ripple",
	"dogecoin": "dogecoin", "doge": "dogecoin", "bnb": "binancecoin", "cardano": "cardano",
}

var stooqAliases = map[string]string{
	"emas": "xauusd", "gold": "xauusd", "xau": "xauusd", "xauusd": "xauusd",
	"perak": "xagusd", "silver": "xagusd", "xag": "xagusd",
	"minyak": "cl.f", "oil": "cl.f", "crude": "cl.f", "wti": "cl.f",
	"ihsg": "^jkse", "nasdaq": "^ndq", "sp500": "^spx", "dow": "^dji", "dji": "^dji", "nikkei": "^nkx", "dax": "^dax",
	"eurusd": "eurusd", "gbpusd": "gbpusd", "usdjpy": "usdjpy", "usdidr": "usdidr", "rupiah": "usdidr", "idr": "usdidr",
}

var tickerRe = regexp.MustCompile(`\b[A-Z]{2,5}\b`)

var notTickers = map[string]bool{
	"AI": true, "RSI": true, "EMA": true, "USD": true, "IDR": true, "ETF": true, "GBM": true, "API": true,
	"CEO": true, "OK": true, "KB": true, "MC": true, "VAR": true, "BTC": true, "ETH": true,
}

// ResolveAsset mencari aset yang dimaksud pada teks (alias dikenal, lalu ticker AS huruf besar).
func ResolveAsset(text string) (Asset, bool) {
	for _, tok := range strings.FieldsFunc(strings.ToLower(strings.ReplaceAll(text, "&", "")), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
	}) {
		if id, ok := cryptoAliases[tok]; ok {
			return Asset{Name: strings.ToUpper(tok), Kind: "coingecko", ID: id, PeriodsPerYear: 365}, true
		}
		if id, ok := stooqAliases[tok]; ok {
			return Asset{Name: strings.ToUpper(tok), Kind: "stooq", ID: id, PeriodsPerYear: 252}, true
		}
	}
	if strings.Contains(strings.ToLower(text), "s&p") {
		return Asset{Name: "S&P 500", Kind: "stooq", ID: "^spx", PeriodsPerYear: 252}, true
	}
	for _, t := range tickerRe.FindAllString(text, -1) {
		if !notTickers[t] {
			return Asset{Name: t, Kind: "stooq", ID: strings.ToLower(t) + ".us", PeriodsPerYear: 252}, true
		}
	}
	return Asset{}, false
}

var numRe = regexp.MustCompile(`-?\d+(?:\.\d+)?`)

// ParseInlineSeries mengambil deret angka dari teks (minimal 15 angka), mis. "data: 100, 101, 99.5 ...".
func ParseInlineSeries(text string) []float64 {
	m := numRe.FindAllString(text, -1)
	if len(m) < 15 {
		return nil
	}
	out := make([]float64, 0, len(m))
	for _, s := range m {
		v, err := strconv.ParseFloat(s, 64)
		if err == nil {
			out = append(out, v)
		}
	}
	return out
}

var horizonRe = regexp.MustCompile(`(?i)(\d+)\s*(hari|minggu|pekan|bulan|tahun|day|days|week|weeks|month|months|year|years)`)

// ParseHorizonDays mengembalikan horizon proyeksi dalam hari kalender (default 30).
func ParseHorizonDays(text string) int {
	m := horizonRe.FindStringSubmatch(text)
	if m == nil {
		return 30
	}
	n, _ := strconv.Atoi(m[1])
	mult := map[string]int{"hari": 1, "day": 1, "days": 1, "minggu": 7, "pekan": 7, "week": 7, "weeks": 7,
		"bulan": 30, "month": 30, "months": 30, "tahun": 365, "year": 365, "years": 365}[strings.ToLower(m[2])]
	d := n * mult
	if d < 1 {
		return 30
	}
	if d > 730 {
		d = 730
	}
	return d
}

var forecastWords = []string{"prediksi", "prakiraan", "ramalan", "forecast", "predict", "proyeksi", "outlook", "analisis teknikal", "monte carlo"}
var marketWords = []string{"saham", "crypto", "kripto", "forex", "komoditas", "harga", "kurs", "indeks", "market", "pasar", "investasi", "stock"}

// IsMarketIntent mendeteksi permintaan prediksi/analisis pasar.
func IsMarketIntent(text string) bool {
	l := strings.ToLower(text)
	if ParseInlineSeries(text) != nil && containsAny(l, forecastWords...) {
		return true
	}
	if !containsAny(l, forecastWords...) {
		return false
	}
	if containsAny(l, marketWords...) {
		return true
	}
	_, ok := ResolveAsset(text)
	return ok
}

func containsAny(s string, subs ...string) bool {
	for _, x := range subs {
		if strings.Contains(s, x) {
			return true
		}
	}
	return false
}

func (m *MarketClient) get(ctx context.Context, u string, headers map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, redact(err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; ai-gateway/1.0)")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := m.HTTP.Do(req)
	if err != nil {
		return nil, redact(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return b, nil
}

// Fetch mengambil maksimal ~250 harga penutupan harian terakhir (urut lama -> baru).
func (m *MarketClient) Fetch(ctx context.Context, a Asset) ([]float64, error) {
	var prices []float64
	switch a.Kind {
	case "stooq":
		b, err := m.get(ctx, m.StooqURL+"?i=d&s="+url.QueryEscape(a.ID), nil)
		if err != nil {
			return nil, err
		}
		rows, err := csv.NewReader(strings.NewReader(string(b))).ReadAll()
		if err != nil || len(rows) < 2 {
			return nil, errors.New("data Stooq kosong atau simbol tidak dikenal")
		}
		col := -1
		for i, h := range rows[0] {
			if strings.EqualFold(h, "Close") {
				col = i
			}
		}
		if col < 0 {
			return nil, errors.New("data Stooq tidak valid (kolom Close tidak ada)")
		}
		for _, r := range rows[1:] {
			if col < len(r) {
				if v, err := strconv.ParseFloat(r[col], 64); err == nil && v > 0 {
					prices = append(prices, v)
				}
			}
		}
	case "coingecko":
		h := map[string]string{}
		if m.CGKey != "" {
			h["x-cg-demo-api-key"] = m.CGKey
		}
		b, err := m.get(ctx, m.CoinGeckoURL+"/coins/"+url.PathEscape(a.ID)+"/market_chart?vs_currency=usd&days=365&interval=daily", h)
		if err != nil {
			return nil, err
		}
		var r struct {
			Prices [][]float64 `json:"prices"`
		}
		if err := json.Unmarshal(b, &r); err != nil {
			return nil, err
		}
		for _, p := range r.Prices {
			if len(p) == 2 && p[1] > 0 {
				prices = append(prices, p[1])
			}
		}
	default:
		return nil, errors.New("jenis sumber data tidak dikenal")
	}
	if len(prices) > 250 {
		prices = prices[len(prices)-250:]
	}
	return prices, nil
}

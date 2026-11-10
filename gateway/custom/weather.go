package custom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Prakiraan cuaca. Custom AI TIDAK menghitung cuaca sendiri: ia mengambil prakiraan model cuaca
// numerik dari Open-Meteo (gratis, tanpa kunci) lalu menyusunnya dalam bahasa Indonesia.
// Nama kota pengguna dikirim ke Open-Meteo untuk geocoding.

type WeatherClient struct {
	HTTP        *http.Client
	GeocodeURL  string
	ForecastURL string
}

func NewWeatherClient() *WeatherClient {
	return &WeatherClient{
		HTTP:        &http.Client{Timeout: 10 * time.Second},
		GeocodeURL:  "https://geocoding-api.open-meteo.com/v1/search",
		ForecastURL: "https://api.open-meteo.com/v1/forecast",
	}
}

type Place struct {
	Name, Admin, Country, Timezone string
	Lat, Lon                       float64
}

func (p Place) String() string {
	parts := []string{p.Name}
	if p.Admin != "" && p.Admin != p.Name {
		parts = append(parts, p.Admin)
	}
	if p.Country != "" {
		parts = append(parts, p.Country)
	}
	return strings.Join(parts, ", ")
}

type Day struct {
	Date       time.Time
	Code       int
	TMax, TMin float64
	RainMM     float64
	RainProb   *float64 // bisa tidak tersedia untuk hari yang jauh
	WindMax    float64
}

type Current struct {
	TempC, Humidity, WindKmh float64
	Code                     int
	Time                     string
}

type WeatherForecast struct {
	Place    Place
	Timezone string
	Current  *Current
	Days     []Day
}

func (w *WeatherClient) get(ctx context.Context, u string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return redact(err)
	}
	req.Header.Set("User-Agent", "ai-gateway/1.0")
	resp, err := w.HTTP.Do(req)
	if err != nil {
		return redact(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return json.Unmarshal(b, out)
}

// Geocode mencari kota di seluruh dunia (nama tempat dalam bahasa Indonesia).
func (w *WeatherClient) Geocode(ctx context.Context, name string) (Place, error) {
	return w.GeocodeLang(ctx, name, "id")
}

// GeocodeLang: seperti Geocode, dengan nama tempat dalam bahasa lang. "Kota, Negara/Provinsi"
// (mis. "Paris, Texas") dipakai untuk memilih di antara kota senama.
func (w *WeatherClient) GeocodeLang(ctx context.Context, name, lang string) (Place, error) {
	var r struct {
		Results []struct {
			Name        string  `json:"name"`
			Admin1      string  `json:"admin1"`
			Country     string  `json:"country"`
			CountryCode string  `json:"country_code"`
			Timezone    string  `json:"timezone"`
			Latitude    float64 `json:"latitude"`
			Longitude   float64 `json:"longitude"`
		} `json:"results"`
	}
	city, qual := strings.TrimSpace(name), ""
	if i := strings.Index(city, ","); i > 0 {
		city, qual = strings.TrimSpace(city[:i]), strings.ToLower(strings.TrimSpace(city[i+1:]))
	}
	count := 1
	if qual != "" {
		count = 10
	}
	u := fmt.Sprintf("%s?count=%d&language=%s&format=json&name=%s", w.GeocodeURL, count, url.QueryEscape(lang), url.QueryEscape(city))
	if err := w.get(ctx, u, &r); err != nil {
		return Place{}, err
	}
	if len(r.Results) == 0 {
		return Place{}, fmt.Errorf("lokasi %q tidak ditemukan", name)
	}
	pick := 0
	if qual != "" {
		pick = -1
		for i, x := range r.Results {
			if strings.EqualFold(x.CountryCode, qual) || strings.Contains(strings.ToLower(x.Country), qual) || strings.Contains(strings.ToLower(x.Admin1), qual) {
				pick = i
				break
			}
		}
		if pick < 0 {
			return Place{}, fmt.Errorf("lokasi %q tidak ditemukan", name)
		}
	}
	x := r.Results[pick]
	return Place{Name: x.Name, Admin: x.Admin1, Country: x.Country, Timezone: x.Timezone, Lat: x.Latitude, Lon: x.Longitude}, nil
}

// Forecast mengambil kondisi saat ini dan prakiraan harian untuk hari ini + extraDays hari ke depan.
func (w *WeatherClient) Forecast(ctx context.Context, p Place, extraDays int) (*WeatherForecast, error) {
	if extraDays < 0 {
		extraDays = 0
	}
	if extraDays > 15 {
		extraDays = 15
	}
	q := url.Values{}
	q.Set("latitude", strconv.FormatFloat(p.Lat, 'f', 4, 64))
	q.Set("longitude", strconv.FormatFloat(p.Lon, 'f', 4, 64))
	q.Set("timezone", "auto")
	q.Set("forecast_days", strconv.Itoa(extraDays+1))
	q.Set("current", "temperature_2m,relative_humidity_2m,weather_code,wind_speed_10m")
	q.Set("daily", "weather_code,temperature_2m_max,temperature_2m_min,precipitation_sum,precipitation_probability_max,wind_speed_10m_max")
	var r struct {
		Timezone string `json:"timezone"`
		Current  *struct {
			Time     string   `json:"time"`
			Temp     *float64 `json:"temperature_2m"`
			Humidity *float64 `json:"relative_humidity_2m"`
			Code     *int     `json:"weather_code"`
			Wind     *float64 `json:"wind_speed_10m"`
		} `json:"current"`
		Daily struct {
			Time     []string   `json:"time"`
			Code     []*int     `json:"weather_code"`
			TMax     []*float64 `json:"temperature_2m_max"`
			TMin     []*float64 `json:"temperature_2m_min"`
			Rain     []*float64 `json:"precipitation_sum"`
			RainProb []*float64 `json:"precipitation_probability_max"`
			Wind     []*float64 `json:"wind_speed_10m_max"`
		} `json:"daily"`
	}
	if err := w.get(ctx, w.ForecastURL+"?"+q.Encode(), &r); err != nil {
		return nil, err
	}
	if len(r.Daily.Time) == 0 {
		return nil, errors.New("respons prakiraan kosong")
	}
	at := func(s []*float64, i int) *float64 {
		if i < len(s) {
			return s[i]
		}
		return nil
	}
	val := func(p *float64) float64 {
		if p == nil {
			return math.NaN()
		}
		return *p
	}
	f := &WeatherForecast{Place: p, Timezone: r.Timezone}
	if r.Current != nil && r.Current.Temp != nil {
		c := &Current{Time: r.Current.Time, TempC: *r.Current.Temp, Humidity: val(r.Current.Humidity), WindKmh: val(r.Current.Wind), Code: -1}
		if r.Current.Code != nil {
			c.Code = *r.Current.Code
		}
		f.Current = c
	}
	for i, ds := range r.Daily.Time {
		t, err := time.Parse("2006-01-02", ds)
		if err != nil {
			continue
		}
		d := Day{Date: t, Code: -1, TMax: val(at(r.Daily.TMax, i)), TMin: val(at(r.Daily.TMin, i)),
			RainMM: val(at(r.Daily.Rain, i)), RainProb: at(r.Daily.RainProb, i), WindMax: val(at(r.Daily.Wind, i))}
		if i < len(r.Daily.Code) && r.Daily.Code[i] != nil {
			d.Code = *r.Daily.Code[i]
		}
		f.Days = append(f.Days, d)
	}
	if len(f.Days) == 0 {
		return nil, errors.New("respons prakiraan tidak valid")
	}
	return f, nil
}

// wmoText menerjemahkan kode cuaca WMO ke bahasa Indonesia.
func wmoText(code int) string { return wxPacks["id"].Cond[wmoKey(code)] }

func numOrDash(v float64, format string) string {
	if math.IsNaN(v) {
		return "-"
	}
	return fmt.Sprintf(format, v)
}

// Render menyusun jawaban utama dan referensi statistik dalam bahasa Indonesia.
func (f *WeatherForecast) Render() (text, secondary string) { return f.RenderLang("id") }

// RenderLang menyusun laporan dalam salah satu bahasa di WeatherLanguages() (tak dikenal -> Inggris).
func (f *WeatherForecast) RenderLang(lang string) (text, secondary string) {
	p := wxPacks[lang]
	if p == nil {
		p = wxPacks["en"]
	}
	var b strings.Builder
	days := len(f.Days) - 1
	if days <= 0 {
		fmt.Fprintf(&b, p.TitleToday, f.Place, f.Timezone)
	} else {
		fmt.Fprintf(&b, p.TitleDays, f.Place, days, f.Timezone)
	}
	if c := f.Current; c != nil {
		fmt.Fprintf(&b, p.Now, p.Cond[wmoKey(c.Code)], numOrDash(c.TempC, "%.0f"), numOrDash(c.Humidity, "%.0f"), numOrDash(c.WindKmh, "%.0f"))
	}
	b.WriteString("\n")
	var sumMax, sumMin, rain float64
	var nMax, nMin, rainy int
	hot, wet := -1, -1
	for i, d := range f.Days {
		label := p.dateLabel(d.Date)
		if i == 0 {
			label += " (" + p.Today + ")"
		}
		prob := "-"
		if d.RainProb != nil {
			prob = fmt.Sprintf("%.0f%%", *d.RainProb)
		}
		fmt.Fprintf(&b, p.Day, label, p.Cond[wmoKey(d.Code)], numOrDash(d.TMin, "%.0f"), numOrDash(d.TMax, "%.0f"), prob,
			numOrDash(d.RainMM, "%.1f"), numOrDash(d.WindMax, "%.0f"))
		if !math.IsNaN(d.TMax) {
			sumMax += d.TMax
			nMax++
			if hot < 0 || d.TMax > f.Days[hot].TMax {
				hot = i
			}
		}
		if !math.IsNaN(d.TMin) {
			sumMin += d.TMin
			nMin++
		}
		if !math.IsNaN(d.RainMM) {
			rain += d.RainMM
			if d.RainMM >= 1 {
				rainy++
			}
			if wet < 0 || d.RainMM > f.Days[wet].RainMM {
				wet = i
			}
		}
	}
	if hot >= 0 && wet >= 0 && f.Days[wet].RainMM >= 1 {
		fmt.Fprintf(&b, p.Hot, p.Weekdays[f.Days[hot].Date.Weekday()], numOrDash(f.Days[hot].TMax, "%.0f"),
			p.Weekdays[f.Days[wet].Date.Weekday()], numOrDash(f.Days[wet].RainMM, "%.1f"))
	} else if hot >= 0 {
		fmt.Fprintf(&b, p.HotDry, p.Weekdays[f.Days[hot].Date.Weekday()], numOrDash(f.Days[hot].TMax, "%.0f"))
	}
	b.WriteString(p.Note)

	var s strings.Builder
	s.WriteString(p.SecHead)
	if nMax > 0 && nMin > 0 {
		fmt.Fprintf(&s, p.SecTemp, sumMax/float64(nMax), sumMin/float64(nMin), len(f.Days))
	}
	fmt.Fprintf(&s, p.SecRain, rain, rainy, len(f.Days))
	return b.String(), s.String()
}

// ---------- deteksi niat & ekstraksi lokasi ----------

var (
	weatherCoreRe = regexp.MustCompile(`(?i)(\b(cuaca|weather|clima|météo|wetter|wettervorhersage|previsão)\b|天気|天气|天氣|날씨|погода|погоду)`)
	weatherSoftRe = regexp.MustCompile(`(?i)\b(hujan|suhu|rain|temperature)\b`)
	weatherTimeRe = regexp.MustCompile(`(?i)\b(besok|hari ini|tomorrow|today|minggu|prediksi|ramalan|prakiraan|forecast)\b`)
	prepLocRe     = regexp.MustCompile(`(?i)\b(?:di|untuk|kota|in|for|at|en|em|à|bei|para|pour|für)\s+([\p{L}][\p{L} .,'-]{1,40}?)(?:\s+(?:hari|besok|selama|dalam|pada|minggu|sampai|hingga|today|tomorrow|for|next|this)\b|\s+\d|[?.!;]|$)`)
	daysRe        = regexp.MustCompile(`(?i)(\d+)\s*(hari|days|day|minggu|weeks|week|días|dias|jours|tage|日間|日|天|일|дн)`)
	weekAheadRe   = regexp.MustCompile(`(?i)\b(minggu|week|depan|ahead|next)\b`)
	todayOnlyRe   = regexp.MustCompile(`(?i)\b(hari ini|today|sekarang|now)\b`)
)

// IsWeatherIntent: apakah pengguna menanyakan cuaca.
func IsWeatherIntent(text string) bool {
	if weatherCoreRe.MatchString(text) {
		return true
	}
	return weatherSoftRe.MatchString(text) && weatherTimeRe.MatchString(text)
}

var weatherFillers = func() map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(`cuaca weather clima météo wetter tiempo pronóstico previsão previsión prévisions vorhersage wettervorhersage hoy hoje aujourd'hui heute días dias jours tage los las les die der das el la le für para pour de del en em à bei in prediksi ramalan prakiraan forecast hari ini depan ke yang akan datang dan sampai hingga
		minggu week days day next ahead today tomorrow besok sekarang now tolong dong buatkan buat bikin tampilkan show the in for at di untuk
		kota selama dalam pada apa bagaimana gimana berapa suhu hujan rain temperature custom custome kustom ai dari`) {
		m[w] = true
	}
	return m
}()

// ExtractLocation mengambil nama kota dari pertanyaan ("" bila tidak ada). Nilai kedua true bila
// kota disebut eksplisit dengan kata depan ("di Bandung"); false bila hanya tebakan dari sisa kata.
func ExtractLocation(text string) (string, bool) {
	if m := prepLocRe.FindStringSubmatch(text); m != nil {
		loc := strings.TrimSpace(m[1])
		var keep []string
		for _, w := range strings.Fields(loc) {
			if !weatherFillers[strings.ToLower(strings.Trim(w, ".,"))] {
				keep = append(keep, w)
			}
		}
		if s := strings.TrimRight(strings.Join(keep, " "), " ,"); s != "" {
			return s, true
		}
	}
	var keep []string
	for _, w := range strings.FieldsFunc(text, func(r rune) bool { return !(r >= '0' && r <= '9') && !isLetter(r) }) {
		if _, err := strconv.Atoi(w); err == nil {
			continue
		}
		if !weatherFillers[strings.ToLower(w)] {
			keep = append(keep, w)
		}
	}
	if len(keep) > 3 { // terlalu panjang untuk nama kota: bukan lokasi
		return "", false
	}
	return strings.Join(keep, " "), false
}

func isLetter(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r > 127
}

// WeatherHorizon: jumlah hari KE DEPAN di luar hari ini. Bawaan 7; "hari ini" saja = 0; maksimal 15.
func WeatherHorizon(text string) int {
	if m := daysRe.FindStringSubmatch(text); m != nil {
		n, _ := strconv.Atoi(m[1])
		if u := strings.ToLower(m[2]); strings.HasPrefix(u, "minggu") || strings.HasPrefix(u, "week") {
			n *= 7
		}
		return min(max(n, 0), 15)
	}
	if weekAheadRe.MatchString(text) {
		return 7
	}
	if todayOnlyRe.MatchString(text) {
		return 0
	}
	return 7
}

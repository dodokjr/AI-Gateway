package custom

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const geoJSON = `{"results":[{"name":"Bandung","admin1":"Jawa Barat","country":"Indonesia","timezone":"Asia/Jakarta","latitude":-6.9215,"longitude":107.6107}]}`

// 8 hari: 2026-11-05 (Kamis) s.d. 2026-11-12; probabilitas hujan kosong (null) untuk 3 hari terakhir.
const fcJSON = `{"timezone":"Asia/Jakarta",
"current":{"time":"2026-11-05T15:45","temperature_2m":27.4,"relative_humidity_2m":78,"weather_code":3,"wind_speed_10m":9.2},
"daily":{"time":["2026-11-05","2026-11-06","2026-11-07","2026-11-08","2026-11-09","2026-11-10","2026-11-11","2026-11-12"],
"weather_code":[61,63,95,2,1,0,3,80],
"temperature_2m_max":[29.1,28.0,27.5,30.2,31.0,31.5,29.9,28.8],
"temperature_2m_min":[20.1,20.5,20.0,20.8,21.0,21.2,20.9,20.6],
"precipitation_sum":[4.2,9.8,22.5,0.0,0.0,0.0,0.4,6.1],
"precipitation_probability_max":[70,85,95,20,10,5,null,null],
"wind_speed_10m_max":[12,14,25,10,9,8,11,13]}}`

func weatherServer(t *testing.T) (*httptest.Server, *[]string) {
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.Path+"?"+r.URL.RawQuery)
		switch r.URL.Path {
		case "/geo":
			if strings.Contains(r.URL.Query().Get("name"), "Atlantis") {
				_, _ = w.Write([]byte(`{"generationtime_ms":0.5}`))
				return
			}
			_, _ = w.Write([]byte(geoJSON))
		case "/fc":
			_, _ = w.Write([]byte(fcJSON))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &queries
}

func TestWeatherIntentAndParsing(t *testing.T) {
	for _, q := range []string{"prediksi cuaca di Bandung", "cuaca Jakarta hari ini dan 7 hari ke depan", "besok hujan nggak di Surabaya?", "weather in Tokyo", "buatkan prediksi cuaca dari custome ai"} {
		if !IsWeatherIntent(q) {
			t.Fatalf("harus terdeteksi cuaca: %q", q)
		}
	}
	for _, q := range []string{"prediksi harga bitcoin 30 hari", "siapa penemu telepon", "hujan batu adalah mitos?"} {
		if IsWeatherIntent(q) {
			t.Fatalf("bukan cuaca: %q", q)
		}
	}
	locs := map[string]string{
		"prediksi cuaca di Bandung hari ini dan 7 hari ke depan": "Bandung",
		"cuaca di Kota Bandung":                                  "Bandung",
		"weather in New York tomorrow":                           "New York",
		"cuaca Jakarta 7 hari":                                   "Jakarta",
		"buatkan prediksi cuaca dari custome ai dari hari ini dan 7 hari yang akan datang": "",
	}
	for q, want := range locs {
		if got, _ := ExtractLocation(q); got != want {
			t.Fatalf("ExtractLocation(%q)=%q, harapan %q", q, got, want)
		}
	}
	if _, explicit := ExtractLocation("cuaca di Bandung"); !explicit {
		t.Fatal("kota dengan kata depan harus eksplisit")
	}
	if _, explicit := ExtractLocation("cuaca Bandung"); explicit {
		t.Fatal("tebakan tanpa kata depan tidak eksplisit")
	}
	hz := map[string]int{"cuaca hari ini dan 7 hari ke depan": 7, "cuaca hari ini": 0, "cuaca 3 hari": 3, "cuaca 2 minggu": 14, "cuaca 30 hari": 15, "cuaca bandung": 7, "cuaca minggu depan": 7}
	for q, want := range hz {
		if got := WeatherHorizon(q); got != want {
			t.Fatalf("WeatherHorizon(%q)=%d, harapan %d", q, got, want)
		}
	}
}

func TestWeatherClientAndRender(t *testing.T) {
	srv, queries := weatherServer(t)
	w := NewWeatherClient()
	w.GeocodeURL, w.ForecastURL = srv.URL+"/geo", srv.URL+"/fc"
	ctx := context.Background()
	p, err := w.Geocode(ctx, "Bandung")
	if err != nil || p.Name != "Bandung" || p.Country != "Indonesia" {
		t.Fatalf("geocode: %+v %v", p, err)
	}
	if _, err := w.Geocode(ctx, "Atlantis"); err == nil || !strings.Contains(err.Error(), "tidak ditemukan") {
		t.Fatalf("lokasi tak ada harus error jelas: %v", err)
	}
	f, err := w.Forecast(ctx, p, 7)
	if err != nil || len(f.Days) != 8 {
		t.Fatalf("forecast: %v %+v", err, f)
	}
	q := (*queries)[len(*queries)-1]
	for _, want := range []string{"forecast_days=8", "timezone=auto", "latitude=-6.9215", "precipitation_probability_max", "current="} {
		if !strings.Contains(q, want) {
			t.Fatalf("query forecast harus memuat %q: %s", want, q)
		}
	}
	text, sec := f.Render()
	if !strings.Contains(text, "hari ini dan 7 hari ke depan") || !strings.Contains(text, "Bandung, Jawa Barat, Indonesia") {
		t.Fatalf("judul salah:\n%s", text)
	}
	if n := strings.Count(text, "• "); n != 8 {
		t.Fatalf("harus 8 baris (hari ini + 7), dapat %d:\n%s", n, text)
	}
	for _, want := range []string{"Kam 05 Nov (hari ini): Hujan ringan, 20–29°C, peluang hujan 70% (4.2 mm)", "Sab 07 Nov: Badai petir", "Saat ini: Mendung, 27°C", "Open-Meteo"} {
		if !strings.Contains(text, want) {
			t.Fatalf("teks harus memuat %q:\n%s", want, text)
		}
	}
	if !strings.Contains(text, "Rab 11 Nov: Mendung, 21–30°C, peluang hujan - (0.4 mm)") {
		t.Fatalf("probabilitas null harus tampil '-':\n%s", text)
	}
	if !strings.Contains(text, "Paling basah: Sab (22.5 mm)") {
		t.Fatalf("hari paling basah salah:\n%s", text)
	}
	if !strings.Contains(sec, "Total curah hujan 43.0 mm") || !strings.Contains(sec, "hari hujan (≥1 mm): 4 dari 8") {
		t.Fatalf("referensi statistik salah:\n%s", sec)
	}
}

func TestEngineWeather(t *testing.T) {
	srv, _ := weatherServer(t)
	e := newTestEngine(t, func(c *Config) { c.DefaultCity = "Bandung" })
	e.Weather.GeocodeURL, e.Weather.ForecastURL = srv.URL+"/geo", srv.URL+"/fc"

	// frasa asli pengguna, tanpa kota -> kota bawaan
	r, err := e.Respond(context.Background(), u("buatkan prediksi cuaca dari custome ai dari hari ini dan 7 hari yang akan datang"))
	if err != nil || r.Meta["source"] != "open-meteo" || strings.Count(r.Text, "• ") != 8 || r.Secondary == "" {
		t.Fatalf("cuaca gagal: %+v %v", r, err)
	}
	// kota eksplisit tidak ditemukan -> TIDAK diganti diam-diam dengan kota bawaan
	var d *Declined
	if _, err := e.Respond(context.Background(), u("cuaca di Atlantis")); !errors.As(err, &d) || !strings.Contains(d.Reason, "tidak ditemukan") {
		t.Fatalf("kota eksplisit tak ada harus Declined: %v", err)
	}
	// tebakan lokasi gagal -> pakai kota bawaan
	if r, err := e.Respond(context.Background(), u("cuaca Atlantis")); err != nil || r.Meta["source"] != "open-meteo" {
		t.Fatalf("tebakan gagal harus jatuh ke kota bawaan: %v", err)
	}
	// tanpa kota dan tanpa kota bawaan -> Declined dengan petunjuk
	e2 := newTestEngine(t, nil)
	e2.Weather.GeocodeURL, e2.Weather.ForecastURL = srv.URL+"/geo", srv.URL+"/fc"
	if _, err := e2.Respond(context.Background(), u("prediksi cuaca 7 hari")); !errors.As(err, &d) || !strings.Contains(d.Reason, "sebutkan kota") {
		t.Fatalf("tanpa kota harus meminta kota: %v", err)
	}
	// "prediksi cuaca" tidak boleh dianggap prediksi pasar
	if IsMarketIntent("prediksi cuaca di Bandung") {
		t.Fatal("cuaca bukan niat pasar")
	}
	// jawaban cuaca tidak boleh dipelajari (cepat basi)
	if ShouldLearn("Bagaimana cuaca di Bandung minggu ini?", "Cuaca Bandung minggu ini cenderung hujan di sore hari.") {
		t.Fatal("cuaca tidak boleh dipelajari")
	}
	// dimatikan lewat config
	e.Cfg.Weather = false
	if r, err := e.Respond(context.Background(), u("cuaca di Bandung")); err == nil && r.Meta["source"] == "open-meteo" {
		t.Fatal("weather=false harus mematikan fitur")
	}
}

func TestWeatherLanguagesAndWorld(t *testing.T) {
	srv, _ := weatherServer(t)
	w := NewWeatherClient()
	w.GeocodeURL, w.ForecastURL = srv.URL+"/geo", srv.URL+"/fc"
	f, err := w.Forecast(context.Background(), Place{Name: "Bandung", Country: "Indonesia", Timezone: "Asia/Jakarta"}, 7)
	if err != nil {
		t.Fatal(err)
	}
	// setiap bahasa: tanpa format rusak, 8 baris, kondisi sudah diterjemahkan
	for _, l := range WeatherLanguages() {
		txt, sec := f.RenderLang(l)
		if strings.Contains(txt+sec, "%!") || strings.Contains(txt+sec, "BADINDEX") {
			t.Fatalf("[%s] format rusak:\n%s\n%s", l, txt, sec)
		}
		if n := strings.Count(txt, "• "); n != 8 {
			t.Fatalf("[%s] harus 8 baris, dapat %d", l, n)
		}
		if !strings.Contains(txt, wxPacks[l].Cond[cThunder]) || !strings.Contains(txt, wxPacks[l].Today) {
			t.Fatalf("[%s] terjemahan hilang:\n%s", l, txt)
		}
		for i, c := range wxPacks[l].Cond {
			if c == "" {
				t.Fatalf("[%s] kondisi %d kosong", l, i)
			}
		}
	}
	en, _ := f.RenderLang("en")
	if !strings.Contains(en, "Thu Nov 05 (today): Light rain, 20–29°C, chance of rain 70% (4.2 mm)") || !strings.Contains(en, "Thunderstorm") {
		t.Fatalf("Inggris salah:\n%s", en)
	}
	ja, _ := f.RenderLang("ja")
	if !strings.Contains(ja, "11月5日(木) (今日)") {
		t.Fatalf("Jepang salah:\n%s", ja)
	}
	// deteksi bahasa + pembersihan agar "in English" tidak dikira kota
	cases := []struct{ q, lang, city string }{
		{"weather in Paris for 7 days", "en", "Paris"},
		{"weather in Tokyo in Japanese", "ja", "Tokyo"},
		{"cuaca di Madrid dalam bahasa Spanyol", "es", "Madrid"},
		{"cuaca di Berlin bahasa Jerman", "de", "Berlin"},
		{"terjemahkan ke bahasa Inggris cuaca di Sydney", "en", "Sydney"},
		{"cuaca di Bandung 7 hari", "id", "Bandung"},
		{"cuaca di Jepang", "id", "Jepang"}, // negara, bukan bahasa
		{"météo à Lyon", "fr", "Lyon"},
		{"clima en Lima", "es", "Lima"},
		{"Wetter in München", "de", "München"},
		{"weather in Paris, Texas in English", "en", "Paris, Texas"},
	}
	for _, c := range cases {
		lang, cleaned := DetectWeatherLang(c.q)
		city, _ := ExtractLocation(cleaned)
		if lang != c.lang || city != c.city {
			t.Fatalf("%q -> (%s, %q), harapan (%s, %q); cleaned=%q", c.q, lang, city, c.lang, c.city, cleaned)
		}
	}
	for _, q := range []string{"天気 東京", "天气 北京", "날씨 서울", "погода Москва", "clima Lima", "météo Lyon", "Wetter Berlin"} {
		if !IsWeatherIntent(q) {
			t.Fatalf("niat cuaca asing tak terdeteksi: %q", q)
		}
	}
	if WeatherHorizon("pronóstico 5 días") != 5 || WeatherHorizon("météo 3 jours") != 3 {
		t.Fatal("horizon bahasa asing salah")
	}
}

func TestGeocodeQualifier(t *testing.T) {
	var gotCount, gotLang string
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		gotCount, gotLang = r.URL.Query().Get("count"), r.URL.Query().Get("language")
		_, _ = rw.Write([]byte(`{"results":[
		{"name":"Paris","admin1":"Île-de-France","country":"France","country_code":"FR","timezone":"Europe/Paris","latitude":48.85,"longitude":2.35},
		{"name":"Paris","admin1":"Texas","country":"United States","country_code":"US","timezone":"America/Chicago","latitude":33.66,"longitude":-95.55}]}`))
	}))
	defer srv.Close()
	w := NewWeatherClient()
	w.GeocodeURL = srv.URL
	ctx := context.Background()
	p, err := w.GeocodeLang(ctx, "Paris, Texas", "en")
	if err != nil || p.Admin != "Texas" || gotCount != "10" || gotLang != "en" {
		t.Fatalf("kualifier salah: %+v %v count=%s lang=%s", p, err, gotCount, gotLang)
	}
	if p, _ := w.GeocodeLang(ctx, "Paris, FR", "en"); p.Country != "France" {
		t.Fatalf("kode negara salah: %+v", p)
	}
	if p, _ := w.GeocodeLang(ctx, "Paris", "en"); p.Country != "France" || gotCount != "1" {
		t.Fatalf("tanpa kualifier harus hasil teratas: %+v", p)
	}
	if _, err := w.GeocodeLang(ctx, "Paris, Narnia", "en"); err == nil {
		t.Fatal("kualifier tak cocok harus error, bukan diganti diam-diam")
	}
}

func TestEngineWeatherLang(t *testing.T) {
	srv, _ := weatherServer(t)
	e := newTestEngine(t, nil)
	e.Weather.GeocodeURL, e.Weather.ForecastURL = srv.URL+"/geo", srv.URL+"/fc"
	r, err := e.Respond(context.Background(), u("weather in Bandung for 7 days, in Japanese"))
	if err != nil || r.Meta["lang"] != "ja" || !strings.Contains(r.Text, "天気予報") || strings.Count(r.Text, "• ") != 8 {
		t.Fatalf("jawaban Jepang gagal: %+v %v", r, err)
	}
	r, err = e.Respond(context.Background(), u("what's the weather in Bandung today and the next 7 days?"))
	if err != nil || r.Meta["lang"] != "en" || !strings.Contains(r.Text, "Weather forecast for") {
		t.Fatalf("jawaban Inggris gagal: %+v %v", r, err)
	}
}

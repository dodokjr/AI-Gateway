package custom

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Terjemahan teks cuaca. Semuanya tabel bawaan di dalam program (tanpa layanan terjemahan,
// tanpa kunci, tanpa biaya, tanpa mengirim data keluar). Yang diterjemahkan: kondisi cuaca,
// nama hari/bulan, dan kalimat laporan. Nama tempat diterjemahkan oleh geocoding Open-Meteo.

// Indeks kondisi dalam wxPack.Cond.
const (
	cClear = iota
	cMostlyClear
	cPartly
	cOvercast
	cFog
	cDrizzle
	cFrzDrizzle
	cRainLight
	cRainMod
	cRainHeavy
	cFrzRain
	cSnow
	cShowerLight
	cShowerMod
	cShowerHeavy
	cSnowShower
	cThunder
	cThunderHail
	cUnknown
	nCond
)

func wmoKey(code int) int {
	switch code {
	case 0:
		return cClear
	case 1:
		return cMostlyClear
	case 2:
		return cPartly
	case 3:
		return cOvercast
	case 45, 48:
		return cFog
	case 51, 53, 55:
		return cDrizzle
	case 56, 57:
		return cFrzDrizzle
	case 61:
		return cRainLight
	case 63:
		return cRainMod
	case 65:
		return cRainHeavy
	case 66, 67:
		return cFrzRain
	case 71, 73, 75, 77:
		return cSnow
	case 80:
		return cShowerLight
	case 81:
		return cShowerMod
	case 82:
		return cShowerHeavy
	case 85, 86:
		return cSnowShower
	case 95:
		return cThunder
	case 96, 99:
		return cThunderHail
	}
	return cUnknown
}

type wxPack struct {
	Name       string
	Cond       [nCond]string
	Weekdays   [7]string // mulai Minggu
	Months     [13]string
	DateFmt    string // %[1]s hari, %[2]d tanggal, %[3]s bulan
	TitleToday string // tempat, zona waktu
	TitleDays  string // tempat, jumlah hari, zona waktu
	Now        string // kondisi, suhu, kelembapan, angin
	Day        string // label, kondisi, min, maks, peluang, mm, angin
	Today      string
	Hot        string // hari terpanas, suhu, hari terbasah, mm
	HotDry     string // hari terpanas, suhu
	Note       string
	SecHead    string
	SecTemp    string // rata maks, rata min, jumlah hari
	SecRain    string // total mm, hari hujan, jumlah hari
}

var wxPacks = map[string]*wxPack{
	"id": {
		Name:       "Bahasa Indonesia",
		Cond:       [nCond]string{"Cerah", "Cerah berawan", "Berawan sebagian", "Mendung", "Berkabut", "Gerimis", "Gerimis beku", "Hujan ringan", "Hujan sedang", "Hujan lebat", "Hujan beku", "Salju", "Hujan lokal ringan", "Hujan lokal sedang", "Hujan lokal deras", "Salju lokal", "Badai petir", "Badai petir disertai hujan es", "Tidak diketahui"},
		Weekdays:   [7]string{"Min", "Sen", "Sel", "Rab", "Kam", "Jum", "Sab"},
		Months:     [13]string{"", "Jan", "Feb", "Mar", "Apr", "Mei", "Jun", "Jul", "Agu", "Sep", "Okt", "Nov", "Des"},
		DateFmt:    "%[1]s %02[2]d %[3]s",
		TitleToday: "Prakiraan cuaca %s untuk hari ini (zona waktu %s).\n",
		TitleDays:  "Prakiraan cuaca %s: hari ini dan %d hari ke depan (zona waktu %s).\n",
		Now:        "Saat ini: %s, %s°C, kelembapan %s%%, angin %s km/j.\n",
		Day:        "• %s: %s, %s–%s°C, peluang hujan %s (%s mm), angin maks %s km/j\n",
		Today:      "hari ini",
		Hot:        "\nTerpanas: %s (%s°C). Paling basah: %s (%s mm).\n",
		HotDry:     "\nTerpanas: %s (%s°C). Tidak ada hari dengan hujan berarti (≥1 mm).\n",
		Note:       "Akurasi prakiraan menurun untuk hari yang lebih jauh. Sumber: Open-Meteo (model cuaca numerik).",
		SecHead:    "Referensi statistik prakiraan\n",
		SecTemp:    "• Rata-rata suhu maksimum %.1f°C, minimum %.1f°C (%d hari)\n",
		SecRain:    "• Total curah hujan %.1f mm; hari hujan (≥1 mm): %d dari %d",
	},
	"en": {
		Name:       "English",
		Cond:       [nCond]string{"Clear", "Mostly clear", "Partly cloudy", "Overcast", "Fog", "Drizzle", "Freezing drizzle", "Light rain", "Moderate rain", "Heavy rain", "Freezing rain", "Snow", "Light showers", "Moderate showers", "Heavy showers", "Snow showers", "Thunderstorm", "Thunderstorm with hail", "Unknown"},
		Weekdays:   [7]string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"},
		Months:     [13]string{"", "Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"},
		DateFmt:    "%[1]s %[3]s %02[2]d",
		TitleToday: "Weather forecast for %s, today (time zone %s).\n",
		TitleDays:  "Weather forecast for %s: today and the next %d days (time zone %s).\n",
		Now:        "Now: %s, %s°C, humidity %s%%, wind %s km/h.\n",
		Day:        "• %s: %s, %s–%s°C, chance of rain %s (%s mm), max wind %s km/h\n",
		Today:      "today",
		Hot:        "\nHottest: %s (%s°C). Wettest: %s (%s mm).\n",
		HotDry:     "\nHottest: %s (%s°C). No days with significant rain (≥1 mm).\n",
		Note:       "Accuracy decreases for days further out. Source: Open-Meteo (numerical weather models).",
		SecHead:    "Statistical reference\n",
		SecTemp:    "• Average high %.1f°C, average low %.1f°C (%d days)\n",
		SecRain:    "• Total rainfall %.1f mm; rainy days (≥1 mm): %d of %d",
	},
	"es": {
		Name:       "Español",
		Cond:       [nCond]string{"Despejado", "Mayormente despejado", "Parcialmente nublado", "Nublado", "Niebla", "Llovizna", "Llovizna helada", "Lluvia ligera", "Lluvia moderada", "Lluvia fuerte", "Lluvia helada", "Nieve", "Chubascos ligeros", "Chubascos moderados", "Chubascos fuertes", "Chubascos de nieve", "Tormenta", "Tormenta con granizo", "Desconocido"},
		Weekdays:   [7]string{"dom", "lun", "mar", "mié", "jue", "vie", "sáb"},
		Months:     [13]string{"", "ene", "feb", "mar", "abr", "may", "jun", "jul", "ago", "sep", "oct", "nov", "dic"},
		DateFmt:    "%[1]s %02[2]d %[3]s",
		TitleToday: "Pronóstico del tiempo para %s, hoy (zona horaria %s).\n",
		TitleDays:  "Pronóstico del tiempo para %s: hoy y los próximos %d días (zona horaria %s).\n",
		Now:        "Ahora: %s, %s°C, humedad %s%%, viento %s km/h.\n",
		Day:        "• %s: %s, %s–%s°C, probabilidad de lluvia %s (%s mm), viento máx. %s km/h\n",
		Today:      "hoy",
		Hot:        "\nMás caluroso: %s (%s°C). Más lluvioso: %s (%s mm).\n",
		HotDry:     "\nMás caluroso: %s (%s°C). Ningún día con lluvia significativa (≥1 mm).\n",
		Note:       "La precisión disminuye en los días más lejanos. Fuente: Open-Meteo (modelos meteorológicos numéricos).",
		SecHead:    "Referencia estadística\n",
		SecTemp:    "• Máxima media %.1f°C, mínima media %.1f°C (%d días)\n",
		SecRain:    "• Precipitación total %.1f mm; días de lluvia (≥1 mm): %d de %d",
	},
	"fr": {
		Name:       "Français",
		Cond:       [nCond]string{"Dégagé", "Plutôt dégagé", "Partiellement nuageux", "Couvert", "Brouillard", "Bruine", "Bruine verglaçante", "Pluie faible", "Pluie modérée", "Pluie forte", "Pluie verglaçante", "Neige", "Averses faibles", "Averses modérées", "Averses fortes", "Averses de neige", "Orage", "Orage avec grêle", "Inconnu"},
		Weekdays:   [7]string{"dim", "lun", "mar", "mer", "jeu", "ven", "sam"},
		Months:     [13]string{"", "janv.", "févr.", "mars", "avr.", "mai", "juin", "juil.", "août", "sept.", "oct.", "nov.", "déc."},
		DateFmt:    "%[1]s %02[2]d %[3]s",
		TitleToday: "Prévisions météo pour %s, aujourd'hui (fuseau horaire %s).\n",
		TitleDays:  "Prévisions météo pour %s : aujourd'hui et les %d jours suivants (fuseau horaire %s).\n",
		Now:        "Actuellement : %s, %s°C, humidité %s%%, vent %s km/h.\n",
		Day:        "• %s : %s, %s–%s°C, risque de pluie %s (%s mm), vent max. %s km/h\n",
		Today:      "aujourd'hui",
		Hot:        "\nLe plus chaud : %s (%s°C). Le plus pluvieux : %s (%s mm).\n",
		HotDry:     "\nLe plus chaud : %s (%s°C). Aucun jour de pluie significative (≥1 mm).\n",
		Note:       "La précision diminue pour les jours les plus éloignés. Source : Open-Meteo (modèles météorologiques numériques).",
		SecHead:    "Référence statistique\n",
		SecTemp:    "• Maximale moyenne %.1f°C, minimale moyenne %.1f°C (%d jours)\n",
		SecRain:    "• Précipitations totales %.1f mm ; jours de pluie (≥1 mm) : %d sur %d",
	},
	"de": {
		Name:       "Deutsch",
		Cond:       [nCond]string{"Klar", "Überwiegend klar", "Teilweise bewölkt", "Bedeckt", "Nebel", "Nieselregen", "Gefrierender Nieselregen", "Leichter Regen", "Mäßiger Regen", "Starker Regen", "Gefrierender Regen", "Schnee", "Leichte Schauer", "Mäßige Schauer", "Starke Schauer", "Schneeschauer", "Gewitter", "Gewitter mit Hagel", "Unbekannt"},
		Weekdays:   [7]string{"So", "Mo", "Di", "Mi", "Do", "Fr", "Sa"},
		Months:     [13]string{"", "Jan", "Feb", "Mär", "Apr", "Mai", "Jun", "Jul", "Aug", "Sep", "Okt", "Nov", "Dez"},
		DateFmt:    "%[1]s, %02[2]d. %[3]s",
		TitleToday: "Wettervorhersage für %s, heute (Zeitzone %s).\n",
		TitleDays:  "Wettervorhersage für %s: heute und die nächsten %d Tage (Zeitzone %s).\n",
		Now:        "Aktuell: %s, %s°C, Luftfeuchtigkeit %s%%, Wind %s km/h.\n",
		Day:        "• %s: %s, %s–%s°C, Regenwahrscheinlichkeit %s (%s mm), max. Wind %s km/h\n",
		Today:      "heute",
		Hot:        "\nHeißester Tag: %s (%s°C). Nassester Tag: %s (%s mm).\n",
		HotDry:     "\nHeißester Tag: %s (%s°C). Keine Tage mit nennenswertem Regen (≥1 mm).\n",
		Note:       "Die Genauigkeit nimmt für weiter entfernte Tage ab. Quelle: Open-Meteo (numerische Wettermodelle).",
		SecHead:    "Statistische Referenz\n",
		SecTemp:    "• Mittleres Maximum %.1f°C, mittleres Minimum %.1f°C (%d Tage)\n",
		SecRain:    "• Gesamtniederschlag %.1f mm; Regentage (≥1 mm): %d von %d",
	},
	"pt": {
		Name:       "Português",
		Cond:       [nCond]string{"Céu limpo", "Predominantemente limpo", "Parcialmente nublado", "Nublado", "Nevoeiro", "Garoa", "Garoa congelante", "Chuva fraca", "Chuva moderada", "Chuva forte", "Chuva congelante", "Neve", "Pancadas fracas", "Pancadas moderadas", "Pancadas fortes", "Pancadas de neve", "Trovoada", "Trovoada com granizo", "Desconhecido"},
		Weekdays:   [7]string{"dom", "seg", "ter", "qua", "qui", "sex", "sáb"},
		Months:     [13]string{"", "jan", "fev", "mar", "abr", "mai", "jun", "jul", "ago", "set", "out", "nov", "dez"},
		DateFmt:    "%[1]s %02[2]d %[3]s",
		TitleToday: "Previsão do tempo para %s, hoje (fuso horário %s).\n",
		TitleDays:  "Previsão do tempo para %s: hoje e os próximos %d dias (fuso horário %s).\n",
		Now:        "Agora: %s, %s°C, umidade %s%%, vento %s km/h.\n",
		Day:        "• %s: %s, %s–%s°C, chance de chuva %s (%s mm), vento máx. %s km/h\n",
		Today:      "hoje",
		Hot:        "\nMais quente: %s (%s°C). Mais chuvoso: %s (%s mm).\n",
		HotDry:     "\nMais quente: %s (%s°C). Nenhum dia com chuva significativa (≥1 mm).\n",
		Note:       "A precisão diminui para os dias mais distantes. Fonte: Open-Meteo (modelos meteorológicos numéricos).",
		SecHead:    "Referência estatística\n",
		SecTemp:    "• Máxima média %.1f°C, mínima média %.1f°C (%d dias)\n",
		SecRain:    "• Precipitação total %.1f mm; dias de chuva (≥1 mm): %d de %d",
	},
	"ja": {
		Name:       "日本語",
		Cond:       [nCond]string{"快晴", "晴れ", "一部曇り", "曇り", "霧", "霧雨", "着氷性の霧雨", "小雨", "雨", "大雨", "着氷性の雨", "雪", "弱いにわか雨", "にわか雨", "激しいにわか雨", "にわか雪", "雷雨", "雹を伴う雷雨", "不明"},
		Weekdays:   [7]string{"日", "月", "火", "水", "木", "金", "土"},
		Months:     [13]string{"", "1月", "2月", "3月", "4月", "5月", "6月", "7月", "8月", "9月", "10月", "11月", "12月"},
		DateFmt:    "%[3]s%[2]d日(%[1]s)",
		TitleToday: "%sの天気予報:今日(タイムゾーン %s)。\n",
		TitleDays:  "%sの天気予報:今日と今後%d日間(タイムゾーン %s)。\n",
		Now:        "現在: %s、%s°C、湿度 %s%%、風速 %s km/h。\n",
		Day:        "• %s: %s、%s–%s°C、降水確率 %s(%s mm)、最大風速 %s km/h\n",
		Today:      "今日",
		Hot:        "\n最も暑い日: %s(%s°C)。最も雨が多い日: %s(%s mm)。\n",
		HotDry:     "\n最も暑い日: %s(%s°C)。まとまった雨(1 mm以上)の日はありません。\n",
		Note:       "先の日ほど精度は下がります。出典: Open-Meteo(数値気象モデル)。",
		SecHead:    "統計的な参考値\n",
		SecTemp:    "• 平均最高気温 %.1f°C、平均最低気温 %.1f°C(%d日間)\n",
		SecRain:    "• 総降水量 %.1f mm、雨の日(1 mm以上): %d日 / %d日",
	},
	"zh": {
		Name:       "中文",
		Cond:       [nCond]string{"晴", "大部晴朗", "局部多云", "阴", "雾", "毛毛雨", "冻毛毛雨", "小雨", "中雨", "大雨", "冻雨", "雪", "小阵雨", "阵雨", "强阵雨", "阵雪", "雷暴", "雷暴伴冰雹", "未知"},
		Weekdays:   [7]string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"},
		Months:     [13]string{"", "1月", "2月", "3月", "4月", "5月", "6月", "7月", "8月", "9月", "10月", "11月", "12月"},
		DateFmt:    "%[3]s%[2]d日(%[1]s)",
		TitleToday: "%s天气预报:今天(时区 %s)。\n",
		TitleDays:  "%s天气预报:今天及未来%d天(时区 %s)。\n",
		Now:        "当前: %s,%s°C,湿度 %s%%,风速 %s km/h。\n",
		Day:        "• %s: %s,%s–%s°C,降雨概率 %s(%s mm),最大风速 %s km/h\n",
		Today:      "今天",
		Hot:        "\n最热: %s(%s°C)。雨量最大: %s(%s mm)。\n",
		HotDry:     "\n最热: %s(%s°C)。没有明显降雨(≥1 mm)的日子。\n",
		Note:       "越往后的日期,预报准确度越低。来源: Open-Meteo(数值天气模型)。",
		SecHead:    "统计参考\n",
		SecTemp:    "• 平均最高温 %.1f°C,平均最低温 %.1f°C(%d天)\n",
		SecRain:    "• 总降水量 %.1f mm;雨天(≥1 mm): %d / %d 天",
	},
	"ko": {
		Name:       "한국어",
		Cond:       [nCond]string{"맑음", "대체로 맑음", "구름 조금", "흐림", "안개", "이슬비", "어는 이슬비", "약한 비", "보통 비", "강한 비", "어는 비", "눈", "약한 소나기", "소나기", "강한 소나기", "눈 소나기", "뇌우", "우박을 동반한 뇌우", "알 수 없음"},
		Weekdays:   [7]string{"일", "월", "화", "수", "목", "금", "토"},
		Months:     [13]string{"", "1월", "2월", "3월", "4월", "5월", "6월", "7월", "8월", "9월", "10월", "11월", "12월"},
		DateFmt:    "%[3]s %[2]d일(%[1]s)",
		TitleToday: "%s 날씨 예보: 오늘 (시간대 %s).\n",
		TitleDays:  "%s 날씨 예보: 오늘부터 앞으로 %d일 (시간대 %s).\n",
		Now:        "현재: %s, %s°C, 습도 %s%%, 바람 %s km/h.\n",
		Day:        "• %s: %s, %s–%s°C, 강수 확률 %s (%s mm), 최대 풍속 %s km/h\n",
		Today:      "오늘",
		Hot:        "\n가장 더운 날: %s (%s°C). 비가 가장 많은 날: %s (%s mm).\n",
		HotDry:     "\n가장 더운 날: %s (%s°C). 의미 있는 비(≥1 mm)가 오는 날은 없습니다.\n",
		Note:       "먼 날짜일수록 정확도가 떨어집니다. 출처: Open-Meteo (수치 기상 모델).",
		SecHead:    "통계 참고\n",
		SecTemp:    "• 평균 최고기온 %.1f°C, 평균 최저기온 %.1f°C (%d일)\n",
		SecRain:    "• 총 강수량 %.1f mm; 비 온 날(≥1 mm): %d일 / %d일",
	},
	"ru": {
		Name:       "Русский",
		Cond:       [nCond]string{"Ясно", "Преимущественно ясно", "Переменная облачность", "Пасмурно", "Туман", "Морось", "Ледяная морось", "Небольшой дождь", "Умеренный дождь", "Сильный дождь", "Ледяной дождь", "Снег", "Небольшие ливни", "Умеренные ливни", "Сильные ливни", "Снежные заряды", "Гроза", "Гроза с градом", "Неизвестно"},
		Weekdays:   [7]string{"вс", "пн", "вт", "ср", "чт", "пт", "сб"},
		Months:     [13]string{"", "янв", "фев", "мар", "апр", "мая", "июн", "июл", "авг", "сен", "окт", "ноя", "дек"},
		DateFmt:    "%[1]s, %02[2]d %[3]s",
		TitleToday: "Прогноз погоды для %s: сегодня (часовой пояс %s).\n",
		TitleDays:  "Прогноз погоды для %s: сегодня и следующие %d дн. (часовой пояс %s).\n",
		Now:        "Сейчас: %s, %s°C, влажность %s%%, ветер %s км/ч.\n",
		Day:        "• %s: %s, %s–%s°C, вероятность дождя %s (%s мм), макс. ветер %s км/ч\n",
		Today:      "сегодня",
		Hot:        "\nСамый тёплый день: %s (%s°C). Самый дождливый: %s (%s мм).\n",
		HotDry:     "\nСамый тёплый день: %s (%s°C). Дней со значительными осадками (≥1 мм) нет.\n",
		Note:       "Точность прогноза снижается для более отдалённых дней. Источник: Open-Meteo (численные модели погоды).",
		SecHead:    "Статистическая справка\n",
		SecTemp:    "• Средний максимум %.1f°C, средний минимум %.1f°C (%d дн.)\n",
		SecRain:    "• Всего осадков %.1f мм; дождливых дней (≥1 мм): %d из %d",
	},
}

// WeatherLanguages mengembalikan kode bahasa yang didukung.
func WeatherLanguages() []string {
	return []string{"id", "en", "es", "fr", "de", "pt", "ja", "zh", "ko", "ru"}
}

func (p *wxPack) dateLabel(t time.Time) string {
	return fmt.Sprintf(p.DateFmt, p.Weekdays[t.Weekday()], t.Day(), p.Months[t.Month()])
}

// ---------- deteksi bahasa tujuan ----------

// Nama bahasa (huruf kecil) -> kode. Nama bahasa Indonesia yang bentrok dengan nama negara
// (inggris, jerman, ...) hanya dikenali bila didahului kata "bahasa".
var langNames = map[string]string{
	"indonesian": "id", "english": "en", "spanish": "es", "español": "es", "espanol": "es", "french": "fr", "français": "fr", "francais": "fr",
	"german": "de", "deutsch": "de", "portuguese": "pt", "português": "pt", "portugues": "pt", "japanese": "ja", "日本語": "ja",
	"chinese": "zh", "mandarin": "zh", "中文": "zh", "korean": "ko", "한국어": "ko", "russian": "ru", "русский": "ru",
}

var langNamesID = map[string]string{
	"indonesia": "id", "inggris": "en", "spanyol": "es", "prancis": "fr", "perancis": "fr", "jerman": "de", "portugis": "pt",
	"jepang": "ja", "mandarin": "zh", "tionghoa": "zh", "cina": "zh", "china": "zh", "korea": "ko", "rusia": "ru",
}

var (
	langWordsRe  = regexp.MustCompile(`(?i)(?:\b(?:in|into|to|auf|en|em|na)\s+|\bbahasa\s+|\b(?:dalam|ke)\s+bahasa\s+|\bterjemah(?:kan)?\s+(?:ke\s+)?|\btranslate(?:d)?\s+(?:to|into)?\s*)(indonesian|english|spanish|french|german|portuguese|japanese|chinese|mandarin|korean|russian|español|français|deutsch|português|日本語|中文|한국어|русский)\b?`)
	langBahasaRe = regexp.MustCompile(`(?i)\bbahasa\s+(indonesia|inggris|spanyol|prancis|perancis|jerman|portugis|jepang|mandarin|tionghoa|cina|china|korea|rusia)\b`)
	translateRe  = regexp.MustCompile(`(?i)\b(terjemah(?:kan)?|translate[d]?|translation|terjemahan)\b`)
	// kata "cuaca" dalam bahasa lain -> bahasa jawaban bawaan
	nativeWeather = []struct {
		re   *regexp.Regexp
		lang string
	}{
		{regexp.MustCompile(`(?i)\b(clima|pronóstico|pronostico|previsión|prevision)\b`), "es"},
		{regexp.MustCompile(`(?i)\b(météo|meteo|prévisions|previsions)\b`), "fr"},
		{regexp.MustCompile(`(?i)\b(wetter|wettervorhersage)\b`), "de"},
		{regexp.MustCompile(`(?i)\b(previsão|previsao)\b`), "pt"},
		{regexp.MustCompile(`天気`), "ja"},
		{regexp.MustCompile(`天气|天氣`), "zh"},
		{regexp.MustCompile(`날씨`), "ko"},
		{regexp.MustCompile(`(?i)\b(погода|погоду|прогноз)\b`), "ru"},
	}
	englishCueRe = regexp.MustCompile(`(?i)\b(weather|forecast|today|tomorrow|next|please|show|what|how|will|the)\b`)
	indoCueRe    = regexp.MustCompile(`(?i)\b(cuaca|prediksi|ramalan|prakiraan|hari|besok|buatkan|tolong|ke depan|di)\b`)
)

// DetectWeatherLang menentukan bahasa jawaban dan mengembalikan pertanyaan yang sudah dibersihkan
// dari frasa bahasa ("in English", "dalam bahasa Jepang") supaya tidak dikira nama kota.
func DetectWeatherLang(q string) (lang string, cleaned string) {
	cleaned = q
	if m := langBahasaRe.FindStringSubmatch(cleaned); m != nil {
		lang = langNamesID[strings.ToLower(m[1])]
		cleaned = langBahasaRe.ReplaceAllString(cleaned, " ")
	} else if m := langWordsRe.FindStringSubmatch(cleaned); m != nil {
		lang = langNames[strings.ToLower(m[1])]
		cleaned = langWordsRe.ReplaceAllString(cleaned, " ")
	}
	cleaned = strings.Join(strings.Fields(translateRe.ReplaceAllString(cleaned, " ")), " ")
	if lang != "" {
		return lang, cleaned
	}
	for _, n := range nativeWeather {
		if n.re.MatchString(q) {
			return n.lang, cleaned
		}
	}
	if englishCueRe.MatchString(q) && !indoCueRe.MatchString(q) {
		return "en", cleaned
	}
	return "id", cleaned
}
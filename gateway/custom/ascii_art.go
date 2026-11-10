package custom

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// Custom AI tidak membuat gambar sungguhan. Untuk permintaan gambar, ia merender pola
// monokrom: seni ASCII ('#') atau representasi biner ('1' = terisi, '0' = kosong).

type bitmap [][]bool

func parseBitmap(rows []string) bitmap {
	b := make(bitmap, len(rows))
	for i, r := range rows {
		b[i] = make([]bool, len(r))
		for j, c := range r {
			b[i][j] = c == '#'
		}
	}
	return b
}

// render: ASCII memakai "##" per piksel (rasio karakter ~2:1); biner memakai "1"/"0" dipisah spasi.
func (b bitmap) render(binary bool) string {
	var sb strings.Builder
	for _, row := range b {
		var line string
		if binary {
			parts := make([]string, len(row))
			for i, on := range row {
				parts[i] = "0"
				if on {
					parts[i] = "1"
				}
			}
			line = strings.Join(parts, " ")
		} else {
			var lb strings.Builder
			for _, on := range row {
				if on {
					lb.WriteString("##")
				} else {
					lb.WriteString("  ")
				}
			}
			line = strings.TrimRight(lb.String(), " ")
		}
		sb.WriteString(line)
		sb.WriteByte('\n')
	}
	return strings.TrimRight(sb.String(), "\n")
}

var subjectArt = map[string][]string{
	"kucing": {
		"##............##",
		"####........####",
		"################",
		"################",
		"###..######..###",
		"###..######..###",
		"################",
		"#######..#######",
		"################",
		".##############.",
		"..############..",
		"...##########...",
	},
	"hati": {
		".###.....###.",
		"#####...#####",
		"#############",
		"#############",
		"#############",
		".###########.",
		"..#########..",
		"...#######...",
		"....#####....",
		".....###.....",
		"......#......",
	},
	"rumah": {
		".......#.......",
		"......###......",
		".....#####.....",
		"....#######....",
		"...#########...",
		"..###########..",
		".#############.",
		".#############.",
		".##..#####..##.",
		".##..#####..##.",
		".#############.",
		".######..######",
		".######..######",
	},
	"pohon": {
		"......#......",
		".....###.....",
		"....#####....",
		".....###.....",
		"....#####....",
		"...#######...",
		"....#####....",
		"...#######...",
		"..#########..",
		".###########.",
		".....###.....",
		".....###.....",
	},
	"bintang": {
		"......#......",
		"......#......",
		".....###.....",
		"#############",
		".###########.",
		"..#########..",
		"...#######...",
		"..#########..",
		"..####.####..",
		".####...####.",
		".###.....###.",
		".#.........#.",
	},
	"senyum": {
		"....#######....",
		"..###########..",
		".#############.",
		".###..###..###.",
		".###..###..###.",
		".#############.",
		".#############.",
		".##.#######.##.",
		".###.#####.###.",
		".####.....####.",
		"..###########..",
		"....#######....",
	},
}

var subjectAlias = map[string]string{
	"kucing": "kucing", "cat": "kucing", "kitty": "kucing", "meong": "kucing",
	"hati": "hati", "love": "hati", "cinta": "hati", "heart": "hati",
	"rumah": "rumah", "house": "rumah", "home": "rumah",
	"pohon": "pohon", "tree": "pohon",
	"bintang": "bintang", "star": "bintang",
	"senyum": "senyum", "smile": "senyum", "smiley": "senyum", "emoji": "senyum",
	"lingkaran": "lingkaran", "circle": "lingkaran", "bulat": "lingkaran",
	"kotak": "persegi", "persegi": "persegi", "square": "persegi",
	"segitiga": "segitiga", "triangle": "segitiga",
	"diamond": "belah ketupat", "ketupat": "belah ketupat", "belah": "belah ketupat",
}

var font5x5 = map[rune][5]string{
	'A': {".###.", "#...#", "#####", "#...#", "#...#"}, 'B': {"####.", "#...#", "####.", "#...#", "####."},
	'C': {".####", "#....", "#....", "#....", ".####"}, 'D': {"####.", "#...#", "#...#", "#...#", "####."},
	'E': {"#####", "#....", "####.", "#....", "#####"}, 'F': {"#####", "#....", "####.", "#....", "#...."},
	'G': {".####", "#....", "#..##", "#...#", ".####"}, 'H': {"#...#", "#...#", "#####", "#...#", "#...#"},
	'I': {"#####", "..#..", "..#..", "..#..", "#####"}, 'J': {"..###", "...#.", "...#.", "#..#.", ".##.."},
	'K': {"#...#", "#..#.", "###..", "#..#.", "#...#"}, 'L': {"#....", "#....", "#....", "#....", "#####"},
	'M': {"#...#", "##.##", "#.#.#", "#...#", "#...#"}, 'N': {"#...#", "##..#", "#.#.#", "#..##", "#...#"},
	'O': {".###.", "#...#", "#...#", "#...#", ".###."}, 'P': {"####.", "#...#", "####.", "#....", "#...."},
	'Q': {".###.", "#...#", "#.#.#", "#..#.", ".##.#"}, 'R': {"####.", "#...#", "####.", "#..#.", "#...#"},
	'S': {".####", "#....", ".###.", "....#", "####."}, 'T': {"#####", "..#..", "..#..", "..#..", "..#.."},
	'U': {"#...#", "#...#", "#...#", "#...#", ".###."}, 'V': {"#...#", "#...#", "#...#", ".#.#.", "..#.."},
	'W': {"#...#", "#...#", "#.#.#", "##.##", "#...#"}, 'X': {"#...#", ".#.#.", "..#..", ".#.#.", "#...#"},
	'Y': {"#...#", ".#.#.", "..#..", "..#..", "..#.."}, 'Z': {"#####", "...#.", "..#..", ".#...", "#####"},
	'0': {".###.", "#..##", "#.#.#", "##..#", ".###."}, '1': {"..#..", ".##..", "..#..", "..#..", ".###."},
	'2': {".###.", "#...#", "..##.", ".#...", "#####"}, '3': {"####.", "....#", ".###.", "....#", "####."},
	'4': {"#..#.", "#..#.", "#####", "...#.", "...#."}, '5': {"#####", "#....", "####.", "....#", "####."},
	'6': {".###.", "#....", "####.", "#...#", ".###."}, '7': {"#####", "...#.", "..#..", ".#...", ".#..."},
	'8': {".###.", "#...#", ".###.", "#...#", ".###."}, '9': {".###.", "#...#", ".####", "....#", ".###."},
}

func banner(text string) bitmap {
	var rows [5]string
	n := 0
	for _, r := range strings.ToUpper(text) {
		g, ok := font5x5[r]
		if !ok {
			if r == ' ' && n > 0 {
				for i := range rows {
					rows[i] += "..."
				}
			}
			continue
		}
		for i := 0; i < 5; i++ {
			rows[i] += g[i] + "."
		}
		n++
		if n >= 10 {
			break
		}
	}
	return parseBitmap(rows[:])
}

func shapeBitmap(kind string, n int) bitmap {
	if n < 5 {
		n = 5
	}
	if n > 30 {
		n = 30
	}
	var b bitmap
	switch kind {
	case "persegi":
		for y := 0; y < n; y++ {
			row := make([]bool, n)
			for x := range row {
				row[x] = true
			}
			b = append(b, row)
		}
	case "segitiga":
		w := 2*n - 1
		for y := 0; y < n; y++ {
			row := make([]bool, w)
			for x := n - 1 - y; x <= n-1+y; x++ {
				row[x] = true
			}
			b = append(b, row)
		}
	case "belah ketupat":
		if n%2 == 0 {
			n++
		}
		c := n / 2
		for y := 0; y < n; y++ {
			row := make([]bool, n)
			for x := range row {
				row[x] = abs(x-c)+abs(y-c) <= c
			}
			b = append(b, row)
		}
	default: // lingkaran
		c := float64(n-1) / 2
		r := float64(n) / 2
		for y := 0; y < n; y++ {
			row := make([]bool, n)
			for x := range row {
				dx, dy := (float64(x)-c)/r, (float64(y)-c)/r
				row[x] = dx*dx+dy*dy <= 1
			}
			b = append(b, row)
		}
	}
	return b
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

var (
	strongVerbRe = regexp.MustCompile(`(?i)\b(gambarkan|lukiskan|draw|sketch)\b`)
	weakVerbRe   = regexp.MustCompile(`(?i)\b(buat(?:kan)?|bikin(?:kan)?|generate|create|make|render|tampilkan|show|hasilkan|produce)\b`)
	nounRe       = regexp.MustCompile(`(?i)\b(gambar|foto|lukisan|ilustrasi|sketsa|logo|image|picture|drawing)\b`)
	artRe        = regexp.MustCompile(`(?i)\b(ascii art|seni ascii|pixel art)\b`)
	binaryRe     = regexp.MustCompile(`(?i)\b(biner|binary)\b|0\s*dan\s*1|nol\s*dan\s*satu`)
	sizeRe       = regexp.MustCompile(`(?i)(?:ukuran|size)\s*(\d{1,2})|(\d{1,2})\s*[x×]\s*\d{1,2}`)
)

// IsImageRequest: apakah pengguna meminta dibuatkan gambar.
func IsImageRequest(text string) bool {
	return strongVerbRe.MatchString(text) || artRe.MatchString(text) ||
		(weakVerbRe.MatchString(text) && nounRe.MatchString(text))
}

var fillers = func() map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(`buatkan buat bikin bikinkan gambar gambarkan lukis lukiskan lukisan foto ilustrasi sketsa logo
		image picture drawing draw pixel art ascii seni tolong dong ya sebuah seekor satu yang dengan ukuran biner binary
		hitam putih monokrom dari of a an the me make create generate please show tampilkan saya aku untuk kamu bentuk dalam
		format angka dan nol teks text berupa menggunakan size sketch render hasilkan produce`) {
		m[w] = true
	}
	return m
}()

// RenderImageRequest mengubah permintaan gambar menjadi teks monokrom. Tidak pernah menolak:
// subjek tak dikenal dirender sebagai tulisan blok.
func RenderImageRequest(text string) (reply string, subject string) {
	binary := binaryRe.MatchString(text)
	n := 13
	if m := sizeRe.FindStringSubmatch(text); m != nil {
		for _, g := range m[1:] {
			if g != "" {
				n, _ = strconv.Atoi(g)
			}
		}
	}
	low := strings.ReplaceAll(strings.ToLower(text), "belah ketupat", "diamond")
	toks := strings.FieldsFunc(low, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	var bm bitmap
	note := ""
	for _, t := range toks {
		key, ok := subjectAlias[t]
		if !ok {
			continue
		}
		subject = key
		if art, isArt := subjectArt[key]; isArt {
			bm = parseBitmap(art)
		} else {
			bm = shapeBitmap(key, n)
		}
		break
	}
	if bm == nil {
		var words []string
		for _, t := range toks {
			if !fillers[t] {
				words = append(words, t)
			}
		}
		label := strings.ToUpper(strings.Join(words, " "))
		if strings.TrimSpace(label) == "" {
			subject = "hati"
			bm = parseBitmap(subjectArt["hati"])
		} else {
			subject = label
			bm = banner(label)
			note = " Pola gambar untuk subjek ini belum tersedia, jadi saya tampilkan tulisannya sebagai blok."
		}
	}
	mode := "seni ASCII monokrom"
	if binary {
		mode = "pola biner (1 = terisi, 0 = kosong)"
	}
	return fmt.Sprintf("Berikut %s untuk \"%s\" (representasi teks, bukan gambar sungguhan).%s\n\n```\n%s\n```",
		mode, strings.ToLower(subject), note, bm.render(binary)), subject
}

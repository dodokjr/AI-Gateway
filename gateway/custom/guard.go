package custom

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Keamanan Custom AI. Prinsip: (1) teks dari luar (web, AI lain) tidak dipercaya, (2) rahasia tidak
// pernah ikut keluar, (3) Custom AI tidak pernah menjalankan kode atau perintah dari pengguna maupun web.

const maxInputRunes = 20000

var (
	secretPatterns = []*regexp.Regexp{
		regexp.MustCompile(`gsk_[A-Za-z0-9]{16,}`),
		regexp.MustCompile(`sk-[A-Za-z0-9_-]{16,}`),
		regexp.MustCompile(`AIza[0-9A-Za-z_-]{20,}`),
		regexp.MustCompile(`AKIA[0-9A-Z]{12,}`),
		regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{20,}`),
		regexp.MustCompile(`xox[baprs]-[A-Za-z0-9-]{10,}`),
		regexp.MustCompile(`eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`),
		regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._~+/=-]{16,}`),
		regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?(?:-----END [A-Z ]*PRIVATE KEY-----|$)`),
		regexp.MustCompile(`(?i)(api[_-]?key|token|secret|password|passwd|kata sandi)\s*[=:]\s*["']?[A-Za-z0-9._~+/=-]{12,}`),
	}
	// Pola pembajakan instruksi (prompt injection) pada teks tak tepercaya.
	injectionRe = regexp.MustCompile(`(?i)(ignore (all |any |the )?(previous|prior|above) (instructions|prompts?|rules)|disregard (all |the )?(previous|prior|above)|abaikan (semua )?(instruksi|perintah|aturan)( sebelumnya| di atas)?|lupakan (semua )?(instruksi|perintah|aturan)|you are now\b|kamu sekarang adalah|system prompt|prompt sistem|reveal (your|the) (instructions|prompt|keys?)|tampilkan (api[ _-]?key|kunci|prompt sistem)|<\s*/?\s*(system|assistant|tool)\s*>|\[\s*(system|inst)\s*\])`)
)

// RedactSecrets mengganti pola kunci/token/kata sandi dengan penanda.
func RedactSecrets(s string) string {
	for _, re := range secretPatterns {
		s = re.ReplaceAllString(s, "[disembunyikan]")
	}
	return s
}

// stripInvisible membuang karakter kontrol, bidi-override, dan zero-width (dipakai untuk menyembunyikan instruksi).
func stripInvisible(s string) string {
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "")
	}
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t':
			return r
		case r == '\r':
			return -1
		case unicode.IsControl(r):
			return -1
		case (r >= 0x200B && r <= 0x200F) || (r >= 0x202A && r <= 0x202E) || (r >= 0x2060 && r <= 0x2064) || (r >= 0x2066 && r <= 0x2069) || r == 0xFEFF:
			return -1
		}
		return r
	}, s)
}

// SanitizeInput membersihkan pesan pengguna. ok=false bila terlalu panjang untuk Custom AI.
func SanitizeInput(q string) (clean string, ok bool) {
	q = stripInvisible(q)
	if utf8.RuneCountInString(q) > maxInputRunes {
		return "", false
	}
	return strings.TrimSpace(q), true
}

// SanitizeUntrusted membersihkan teks dari web/AI lain sebelum diteruskan ke AI berikutnya atau ke pengguna:
// membuang karakter tersembunyi, baris yang terindikasi injeksi, dan rahasia.
func SanitizeUntrusted(s string) string {
	s = stripInvisible(s)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if injectionRe.MatchString(l) {
			lines[i] = "[baris dibuang: terindikasi injeksi instruksi]"
		}
	}
	return RedactSecrets(strings.Join(lines, "\n"))
}

var phoneLikeRe = regexp.MustCompile(`\+?\d[\d\s-]{9,}\d`)

// RedactPII menyamarkan data pribadi yang jelas (email, nomor panjang >= 11 digit seperti telepon/KTP/kartu)
// serta rahasia. Dipakai untuk log pelatihan; angka biasa (jam, tanggal, harga) dibiarkan.
func RedactPII(s string) string {
	s = RedactSecrets(s)
	s = emailRe.ReplaceAllString(s, "[email]")
	return phoneLikeRe.ReplaceAllStringFunc(s, func(m string) string {
		digits := 0
		for _, r := range m {
			if r >= '0' && r <= '9' {
				digits++
			}
		}
		if digits >= 11 {
			return "[nomor]"
		}
		return m
	})
}
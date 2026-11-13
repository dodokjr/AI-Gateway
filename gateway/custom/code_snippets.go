package custom

import "regexp"

// Pustaka algoritma terverifikasi. Setiap potongan kode lengkap (bisa langsung dijalankan) dan
// menghasilkan keluaran yang SAMA di Go, Python, dan JavaScript (dicek oleh TestSnippetsRun).

type snippet struct {
	Key, Title, Complexity, Notes, Output string
	Re                                    *regexp.Regexp
	Code                                  map[string]string // "go" | "python" | "javascript"
}

func rx(s string) *regexp.Regexp { return regexp.MustCompile(`(?i)` + s) }

var snippets = []snippet{
	{
		Key: "fibonacci", Title: "Bilangan Fibonacci", Re: rx(`fibonacci|fibbonaci`),
		Complexity: "Waktu O(n), memori O(1).",
		Notes:      "Kasus tepi: n negatif ditolak. Go memakai uint64 sehingga n maksimal 93 (lebih dari itu meluap, jadi ditolak). Python dan JavaScript (BigInt) tidak meluap.",
		Output:     "0 1 1 2 3 5 8 13 21 34 55",
		Code: map[string]string{
			"go": `package main

import (
	"errors"
	"fmt"
	"strings"
)

// Fibonacci mengembalikan bilangan ke-n (F0=0, F1=1) secara iteratif.
func Fibonacci(n int) (uint64, error) {
	if n < 0 {
		return 0, errors.New("n tidak boleh negatif")
	}
	if n > 93 {
		return 0, errors.New("n > 93 melebihi kapasitas uint64")
	}
	var a, b uint64 = 0, 1
	for i := 0; i < n; i++ {
		a, b = b, a+b
	}
	return a, nil
}

func main() {
	var out []string
	for i := 0; i <= 10; i++ {
		v, err := Fibonacci(i)
		if err != nil {
			panic(err)
		}
		out = append(out, fmt.Sprint(v))
	}
	fmt.Println(strings.Join(out, " "))
}
`,
			"python": `def fibonacci(n: int) -> int:
    """Bilangan Fibonacci ke-n (F0=0, F1=1), iteratif."""
    if isinstance(n, bool) or not isinstance(n, int) or n < 0:
        raise ValueError("n harus bilangan bulat >= 0")
    a, b = 0, 1
    for _ in range(n):
        a, b = b, a + b
    return a


if __name__ == "__main__":
    print(" ".join(str(fibonacci(i)) for i in range(11)))
`,
			"javascript": `// Bilangan Fibonacci ke-n (F0=0, F1=1), iteratif, memakai BigInt agar tidak meluap.
function fibonacci(n) {
  if (!Number.isInteger(n) || n < 0) {
    throw new RangeError("n harus bilangan bulat >= 0");
  }
  let a = 0n;
  let b = 1n;
  for (let i = 0; i < n; i++) {
    [a, b] = [b, a + b];
  }
  return a;
}

console.log(Array.from({ length: 11 }, (_, i) => fibonacci(i).toString()).join(" "));
`,
		},
	},
	{
		Key: "factorial", Title: "Faktorial", Re: rx(`faktorial|factorial`),
		Complexity: "Waktu O(n), memori O(1).",
		Notes:      "Kasus tepi: 0! = 1; n negatif ditolak. Go memakai uint64 sehingga n maksimal 20 (21! meluap, jadi ditolak). Python dan JavaScript (BigInt) tidak meluap.",
		Output:     "1 1 2 6 24 120 720 5040 40320 362880 3628800",
		Code: map[string]string{
			"go": `package main

import (
	"errors"
	"fmt"
	"strings"
)

// Factorial menghitung n! secara iteratif.
func Factorial(n int) (uint64, error) {
	if n < 0 {
		return 0, errors.New("n tidak boleh negatif")
	}
	if n > 20 {
		return 0, errors.New("n > 20 melebihi kapasitas uint64")
	}
	result := uint64(1)
	for i := 2; i <= n; i++ {
		result *= uint64(i)
	}
	return result, nil
}

func main() {
	var out []string
	for i := 0; i <= 10; i++ {
		v, err := Factorial(i)
		if err != nil {
			panic(err)
		}
		out = append(out, fmt.Sprint(v))
	}
	fmt.Println(strings.Join(out, " "))
}
`,
			"python": `def factorial(n: int) -> int:
    """n! secara iteratif."""
    if isinstance(n, bool) or not isinstance(n, int) or n < 0:
        raise ValueError("n harus bilangan bulat >= 0")
    result = 1
    for i in range(2, n + 1):
        result *= i
    return result


if __name__ == "__main__":
    print(" ".join(str(factorial(i)) for i in range(11)))
`,
			"javascript": `// n! secara iteratif, memakai BigInt agar tidak meluap.
function factorial(n) {
  if (!Number.isInteger(n) || n < 0) {
    throw new RangeError("n harus bilangan bulat >= 0");
  }
  let result = 1n;
  for (let i = 2n; i <= BigInt(n); i++) {
    result *= i;
  }
  return result;
}

console.log(Array.from({ length: 11 }, (_, i) => factorial(i).toString()).join(" "));
`,
		},
	},
	{
		Key: "prime", Title: "Pemeriksa bilangan prima", Re: rx(`prima|prime`),
		Complexity: "Waktu O(√n), memori O(1) (pembagian percobaan dengan langkah 6k±1).",
		Notes:      "Kasus tepi: n < 2 bukan prima; 2 dan 3 prima. Cukup cepat untuk n hingga sekitar 10^12; untuk angka sangat besar pakai uji Miller-Rabin. Di JavaScript, n harus di bawah 2^53.",
		Output:     "2 3 5 7 11 13 17 19 23 29",
		Code: map[string]string{
			"go": `package main

import (
	"fmt"
	"strings"
)

// IsPrime memeriksa n dengan pembagian percobaan 6k±1.
func IsPrime(n int) bool {
	if n < 2 {
		return false
	}
	if n < 4 {
		return true
	}
	if n%2 == 0 || n%3 == 0 {
		return false
	}
	for i := 5; i <= n/i; i += 6 {
		if n%i == 0 || n%(i+2) == 0 {
			return false
		}
	}
	return true
}

func main() {
	var out []string
	for n := 0; n <= 30; n++ {
		if IsPrime(n) {
			out = append(out, fmt.Sprint(n))
		}
	}
	fmt.Println(strings.Join(out, " "))
}
`,
			"python": `def is_prime(n: int) -> bool:
    """Pembagian percobaan 6k±1."""
    if n < 2:
        return False
    if n < 4:
        return True
    if n % 2 == 0 or n % 3 == 0:
        return False
    i = 5
    while i * i <= n:
        if n % i == 0 or n % (i + 2) == 0:
            return False
        i += 6
    return True


if __name__ == "__main__":
    print(" ".join(str(n) for n in range(31) if is_prime(n)))
`,
			"javascript": `// Pembagian percobaan 6k±1 (n harus bilangan bulat aman, < 2^53).
function isPrime(n) {
  if (!Number.isSafeInteger(n) || n < 2) return false;
  if (n < 4) return true;
  if (n % 2 === 0 || n % 3 === 0) return false;
  for (let i = 5; i * i <= n; i += 6) {
    if (n % i === 0 || n % (i + 2) === 0) return false;
  }
  return true;
}

const primes = [];
for (let n = 0; n <= 30; n++) {
  if (isPrime(n)) primes.push(n);
}
console.log(primes.join(" "));
`,
		},
	},
	{
		Key: "gcd", Title: "FPB / GCD (algoritma Euclid)", Re: rx(`\bfpb\b|\bgcd\b|greatest common|faktor persekutuan`),
		Complexity: "Waktu O(log min(a, b)), memori O(1).",
		Notes:      "Kasus tepi: GCD(0, n) = n dan GCD(0, 0) = 0; tanda negatif diabaikan. Di Go, math.MinInt tidak punya pasangan positif pada int, hindari sebagai masukan.",
		Output:     "6 1 9",
		Code: map[string]string{
			"go": `package main

import (
	"fmt"
	"strings"
)

// GCD menghitung faktor persekutuan terbesar dengan algoritma Euclid.
func GCD(a, b int) int {
	if a < 0 {
		a = -a
	}
	if b < 0 {
		b = -b
	}
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

func main() {
	pairs := [][2]int{{48, 18}, {17, 5}, {0, 9}}
	var out []string
	for _, p := range pairs {
		out = append(out, fmt.Sprint(GCD(p[0], p[1])))
	}
	fmt.Println(strings.Join(out, " "))
}
`,
			"python": `def gcd(a: int, b: int) -> int:
    """Algoritma Euclid."""
    a, b = abs(a), abs(b)
    while b:
        a, b = b, a % b
    return a


if __name__ == "__main__":
    print(" ".join(str(gcd(a, b)) for a, b in [(48, 18), (17, 5), (0, 9)]))
`,
			"javascript": `// Algoritma Euclid.
function gcd(a, b) {
  a = Math.abs(a);
  b = Math.abs(b);
  while (b !== 0) {
    [a, b] = [b, a % b];
  }
  return a;
}

console.log([[48, 18], [17, 5], [0, 9]].map(([a, b]) => gcd(a, b)).join(" "));
`,
		},
	},
	{
		Key: "binarysearch", Title: "Pencarian biner", Re: rx(`binary ?search|pencarian biner|pencarian binary|binarysearch`),
		Complexity: "Waktu O(log n), memori O(1).",
		Notes:      "Syarat: data sudah terurut menaik. Mengembalikan indeks, atau -1 bila tidak ada. Titik tengah dihitung lo+(hi-lo)/2 agar tidak meluap. Bila ada nilai ganda, indeks yang dikembalikan salah satu dari yang cocok.",
		Output:     "3 0 5 -1",
		Code: map[string]string{
			"go": `package main

import (
	"cmp"
	"fmt"
	"strings"
)

// BinarySearch mengembalikan indeks target di slice terurut menaik, atau -1.
func BinarySearch[T cmp.Ordered](s []T, target T) int {
	lo, hi := 0, len(s)-1
	for lo <= hi {
		mid := lo + (hi-lo)/2
		switch {
		case s[mid] == target:
			return mid
		case s[mid] < target:
			lo = mid + 1
		default:
			hi = mid - 1
		}
	}
	return -1
}

func main() {
	data := []int{1, 3, 5, 7, 9, 11}
	var out []string
	for _, t := range []int{7, 1, 11, 4} {
		out = append(out, fmt.Sprint(BinarySearch(data, t)))
	}
	fmt.Println(strings.Join(out, " "))
}
`,
			"python": `def binary_search(items: list, target) -> int:
    """Indeks target pada list terurut menaik, atau -1."""
    lo, hi = 0, len(items) - 1
    while lo <= hi:
        mid = lo + (hi - lo) // 2
        if items[mid] == target:
            return mid
        if items[mid] < target:
            lo = mid + 1
        else:
            hi = mid - 1
    return -1


if __name__ == "__main__":
    data = [1, 3, 5, 7, 9, 11]
    print(" ".join(str(binary_search(data, t)) for t in (7, 1, 11, 4)))
`,
			"javascript": `// Indeks target pada array terurut menaik, atau -1.
function binarySearch(items, target) {
  let lo = 0;
  let hi = items.length - 1;
  while (lo <= hi) {
    const mid = lo + Math.floor((hi - lo) / 2);
    if (items[mid] === target) return mid;
    if (items[mid] < target) lo = mid + 1;
    else hi = mid - 1;
  }
  return -1;
}

const data = [1, 3, 5, 7, 9, 11];
console.log([7, 1, 11, 4].map((t) => binarySearch(data, t)).join(" "));
`,
		},
	},
	{
		Key: "mergesort", Title: "Merge sort", Re: rx(`merge ?sort`),
		Complexity: "Waktu O(n log n) di semua kasus, memori O(n). Stabil.",
		Notes:      "Tidak mengubah masukan (mengembalikan salinan terurut). Stabil: elemen sama tetap berurutan seperti aslinya. Slice/list kosong atau satu elemen dikembalikan apa adanya (disalin).",
		Output:     "1 2 5 5 6 9",
		Code: map[string]string{
			"go": `package main

import (
	"cmp"
	"fmt"
	"strings"
)

// MergeSort mengembalikan salinan terurut menaik (stabil); masukan tidak diubah.
func MergeSort[T cmp.Ordered](s []T) []T {
	if len(s) <= 1 {
		return append([]T(nil), s...)
	}
	mid := len(s) / 2
	left, right := MergeSort(s[:mid]), MergeSort(s[mid:])
	out := make([]T, 0, len(s))
	i, j := 0, 0
	for i < len(left) && j < len(right) {
		if right[j] < left[i] {
			out = append(out, right[j])
			j++
		} else {
			out = append(out, left[i])
			i++
		}
	}
	out = append(out, left[i:]...)
	return append(out, right[j:]...)
}

func main() {
	var out []string
	for _, v := range MergeSort([]int{5, 2, 9, 1, 5, 6}) {
		out = append(out, fmt.Sprint(v))
	}
	fmt.Println(strings.Join(out, " "))
}
`,
			"python": `def merge_sort(items: list) -> list:
    """Salinan terurut menaik (stabil); masukan tidak diubah."""
    if len(items) <= 1:
        return list(items)
    mid = len(items) // 2
    left, right = merge_sort(items[:mid]), merge_sort(items[mid:])
    out, i, j = [], 0, 0
    while i < len(left) and j < len(right):
        if right[j] < left[i]:
            out.append(right[j])
            j += 1
        else:
            out.append(left[i])
            i += 1
    out.extend(left[i:])
    out.extend(right[j:])
    return out


if __name__ == "__main__":
    print(" ".join(str(v) for v in merge_sort([5, 2, 9, 1, 5, 6])))
`,
			"javascript": `// Salinan terurut menaik (stabil); masukan tidak diubah. Untuk angka/teks yang dibandingkan dengan <.
function mergeSort(items) {
  if (items.length <= 1) return [...items];
  const mid = Math.floor(items.length / 2);
  const left = mergeSort(items.slice(0, mid));
  const right = mergeSort(items.slice(mid));
  const out = [];
  let i = 0;
  let j = 0;
  while (i < left.length && j < right.length) {
    if (right[j] < left[i]) out.push(right[j++]);
    else out.push(left[i++]);
  }
  return out.concat(left.slice(i), right.slice(j));
}

console.log(mergeSort([5, 2, 9, 1, 5, 6]).join(" "));
`,
		},
	},
	{
		Key: "palindrome", Title: "Pemeriksa palindrom", Re: rx(`palindrom`),
		Complexity: "Waktu O(n), memori O(n).",
		Notes:      "Hanya huruf dan angka yang dibandingkan, huruf besar/kecil diabaikan, dan Unicode ditangani per karakter. String tanpa huruf/angka dianggap palindrom.",
		Output:     "true false true",
		Code: map[string]string{
			"go": `package main

import (
	"fmt"
	"strings"
	"unicode"
)

// IsPalindrome: hanya huruf/angka yang dibandingkan, tanpa membedakan besar-kecil.
func IsPalindrome(s string) bool {
	var r []rune
	for _, c := range s {
		if unicode.IsLetter(c) || unicode.IsDigit(c) {
			r = append(r, unicode.ToLower(c))
		}
	}
	for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
		if r[i] != r[j] {
			return false
		}
	}
	return true
}

func main() {
	var out []string
	for _, s := range []string{"A man, a plan, a canal: Panama", "hello", "Madam"} {
		out = append(out, fmt.Sprint(IsPalindrome(s)))
	}
	fmt.Println(strings.Join(out, " "))
}
`,
			"python": `def is_palindrome(text: str) -> bool:
    """Hanya huruf/angka yang dibandingkan, tanpa membedakan besar-kecil."""
    chars = [c.lower() for c in text if c.isalnum()]
    return chars == chars[::-1]


if __name__ == "__main__":
    samples = ["A man, a plan, a canal: Panama", "hello", "Madam"]
    print(" ".join(str(is_palindrome(s)).lower() for s in samples))
`,
			"javascript": `// Hanya huruf/angka yang dibandingkan, tanpa membedakan besar-kecil.
function isPalindrome(text) {
  const chars = Array.from(text)
    .filter((c) => /[\p{L}\p{N}]/u.test(c))
    .map((c) => c.toLowerCase());
  for (let i = 0, j = chars.length - 1; i < j; i++, j--) {
    if (chars[i] !== chars[j]) return false;
  }
  return true;
}

const samples = ["A man, a plan, a canal: Panama", "hello", "Madam"];
console.log(samples.map(isPalindrome).join(" "));
`,
		},
	},
	{
		Key: "fizzbuzz", Title: "FizzBuzz", Re: rx(`fizz ?buzz`),
		Complexity: "Waktu O(n), memori O(n).",
		Notes:      "Urutan pemeriksaan penting: kelipatan 15 harus dicek lebih dulu (atau digabung dari kelipatan 3 dan 5), kalau tidak \"FizzBuzz\" tidak pernah muncul.",
		Output:     "1 2 Fizz 4 Buzz Fizz 7 8 Fizz Buzz 11 Fizz 13 14 FizzBuzz",
		Code: map[string]string{
			"go": `package main

import (
	"fmt"
	"strconv"
	"strings"
)

// FizzBuzz mengembalikan hasil untuk 1..n.
func FizzBuzz(n int) []string {
	out := make([]string, 0, max(n, 0))
	for i := 1; i <= n; i++ {
		switch {
		case i%15 == 0:
			out = append(out, "FizzBuzz")
		case i%3 == 0:
			out = append(out, "Fizz")
		case i%5 == 0:
			out = append(out, "Buzz")
		default:
			out = append(out, strconv.Itoa(i))
		}
	}
	return out
}

func main() {
	fmt.Println(strings.Join(FizzBuzz(15), " "))
}
`,
			"python": `def fizzbuzz(n: int) -> list[str]:
    """Hasil untuk 1..n."""
    out = []
    for i in range(1, n + 1):
        if i % 15 == 0:
            out.append("FizzBuzz")
        elif i % 3 == 0:
            out.append("Fizz")
        elif i % 5 == 0:
            out.append("Buzz")
        else:
            out.append(str(i))
    return out


if __name__ == "__main__":
    print(" ".join(fizzbuzz(15)))
`,
			"javascript": `// Hasil untuk 1..n.
function fizzbuzz(n) {
  const out = [];
  for (let i = 1; i <= n; i++) {
    if (i % 15 === 0) out.push("FizzBuzz");
    else if (i % 3 === 0) out.push("Fizz");
    else if (i % 5 === 0) out.push("Buzz");
    else out.push(String(i));
  }
  return out;
}

console.log(fizzbuzz(15).join(" "));
`,
		},
	},
}
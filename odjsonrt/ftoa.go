package odjsonrt

import (
	"math"
	"math/bits"
)

// Short decimals.
//
// strconv's shortest formatting runs Ryu on every float64, and most of the
// floats in real documents are prices, coordinates and ratios that were a
// short decimal before they were parsed: 40.8, 12.99, 139.69171. For those
// the shortest representation that round-trips is that decimal, and it can
// be found without Ryu: guess the number of fraction digits from a few
// floating point multiplications, then prove the guess with exact integer
// arithmetic. Only a proven guess is printed; anything else, including every
// value the guess misses, goes to strconv, so the output is strconv's in
// every case.
//
// The proof is Ryu's own definition. A float v = m·2^e with e < 0 parses
// back from a decimal N/10^f exactly when N/10^f lies within half an ulp of
// v, the bounds included when m is even. With P = m·10^f, the candidate is
// N = round(P / 2^s) for s = -e, and the decimal is shortest when the
// interval [(2m-1)·10^f, (2m+1)·10^f] / 2^(s+1) holds an integer at f but
// not at f-1. P is under 2^110 for the f this path accepts, so every
// quantity fits in two words.

// pow10u holds the powers of ten a uint64 holds.
var pow10u = [...]uint64{
	1e0, 1e1, 1e2, 1e3, 1e4, 1e5, 1e6, 1e7, 1e8, 1e9,
	1e10, 1e11, 1e12, 1e13, 1e14, 1e15, 1e16, 1e17, 1e18, 1e19,
}

// maxShortFrac is the most fraction digits the guess considers. Real
// decimals rarely carry more, and the guess is cheap only while it is a
// handful of multiplications.
const maxShortFrac = 7

// appendShortFloat appends v when it is a short decimal, and reports
// whether it did. v must be finite with 1e-6 <= |v| < 1e21, the range
// strconv prints without an exponent; the caller has settled the rest.
func appendShortFloat(dst []byte, v float64) ([]byte, bool) {
	b := math.Float64bits(v)
	exp := int(b>>52) & 0x7FF
	if exp == 0 || exp == 0x7FF {
		return dst, false
	}
	m := b&(1<<52-1) | 1<<52
	e := exp - 1075
	if e >= 0 {
		// An integer; strconv prints it directly.
		return dst, false
	}
	s := uint(-e)
	if s > 100 {
		return dst, false
	}
	abs := math.Abs(v)

	// The guess: the fewest fraction digits at which v·10^f lands on an
	// integer under floating point, and small enough that the candidate
	// keeps under 10^18, where the exact arithmetic below is sized.
	f := -1
	for k := 0; k <= maxShortFrac; k++ {
		p := abs * pow10[k]
		if p >= 1e18 {
			break
		}
		if p == math.Trunc(p) {
			f = k
			break
		}
	}
	if f < 0 {
		return dst, false
	}

	// The proof: an integer within the rounding interval at f, none at
	// f-1, and the candidate nearest to v within that interval. An integer
	// N' at f-1 is the integer 10N' at f, so the second condition is that
	// the range at f holds no multiple of ten.
	even := m&1 == 0
	lo, hi, ok := shortRange(m, s, f, even)
	if !ok || (f > 0 && hi/10*10 >= lo) {
		return dst, false
	}
	ph, pl := bits.Mul64(m, pow10u[f])
	n, c := shiftHalf(ph, pl, s)
	if c > 0 || (c == 0 && n&1 == 1) {
		n++
	}
	if n < lo || n > hi {
		return dst, false
	}

	// The digits, written from the last one: f of them behind the point,
	// which pads a value below one with zeros on its own, then the rest.
	var buf [24]byte
	i := len(buf)
	for range f {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if f > 0 {
		i--
		buf[i] = '.'
	}
	for {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
		if n == 0 {
			break
		}
	}
	if v < 0 {
		i--
		buf[i] = '-'
	}
	return append(dst, buf[i:]...), true
}

// shortRange returns the integers N for which N/10^f parses back to
// m·2^-s, as the inclusive range [lo, hi], and whether there are any. The
// interval is half an ulp either side, with the bounds themselves included
// when the mantissa is even; a mantissa that is a power of two has a
// tighter lower bound, since the ulp below it is half the size.
func shortRange(m uint64, s uint, f int, even bool) (lo, hi uint64, ok bool) {
	p := pow10u[f]
	shift := s + 1
	lm, lc := 2*m-1, uint64(1)
	if m == 1<<52 {
		lm, lc, shift = 4*m-1, 2, s+2
	}
	// hi = floor(H / 2^shift), one less when the bound is exact but
	// excluded.
	hh, hl := bits.Mul64((2*m+1)*lc, p)
	hi, exact := shiftFloor(hh, hl, shift)
	if exact && !even {
		if hi == 0 {
			return 0, 0, false
		}
		hi--
	}
	// lo = ceil(L / 2^shift), one more when the bound is exact but
	// excluded.
	lh, ll := bits.Mul64(lm, p)
	lo, exact = shiftFloor(lh, ll, shift)
	if !exact || !even {
		lo++
	}
	return lo, hi, lo <= hi
}

// shiftFloor returns floor(P / 2^sh) for the two word P and whether the
// division was exact. sh is at most 102 and the quotient is known to fit a
// word.
func shiftFloor(hi, lo uint64, sh uint) (uint64, bool) {
	if sh >= 64 {
		sh -= 64
		return hi >> sh, lo == 0 && hi&(1<<sh-1) == 0
	}
	if sh == 0 {
		return lo, true
	}
	return hi<<(64-sh) | lo>>sh, lo&(1<<sh-1) == 0
}

// shiftHalf returns floor(P / 2^sh) and the comparison of the remainder
// with half the divisor: negative below, zero at exactly half, positive
// above.
func shiftHalf(hi, lo uint64, sh uint) (uint64, int) {
	if sh >= 64 {
		sh -= 64
		q := hi >> sh
		rem := hi & (1<<sh - 1)
		if sh == 0 {
			// The remainder is lo alone, and half is 2^63.
			return q, cmpU(lo, 1<<63)
		}
		half := uint64(1) << (sh - 1)
		if c := cmpU(rem, half); c != 0 {
			return q, c
		}
		return q, cmpU(lo, 0)
	}
	q := hi<<(64-sh) | lo>>sh
	rem := lo & (1<<sh - 1)
	return q, cmpU(rem, uint64(1)<<(sh-1))
}

func cmpU(a, b uint64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

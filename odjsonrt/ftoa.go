package odjsonrt

import (
	"encoding/binary"
	"math"
	"math/bits"
	"slices"
	"strconv"
)

// Float formatting.
//
// strconv's shortest formatting is two thirds bookkeeping: the digits are
// found in a handful of multiplications (Go 1.27 runs Russ Cox's unrounded
// scaling, research.swtch.com/fp), then written into a scratch buffer two
// at a time, trimmed, and copied into the output one byte per append by a
// formatter that serves every verb. For the fixed notation encoding/json
// uses on everything from 1e-6 to 1e21 this file does the same search and
// none of the bookkeeping. A short decimal, which most floats in a document
// are, is settled before the search by one multiplication and one division
// (see "Short decimals" below) and written as one or two words. Anything
// else gets the search, and its digits become ASCII with three
// multiplications per eight of them, written as whole words at computed
// offsets so that a value's shape changes offsets rather than code paths.
//
// The search, from the paper: for a float64 m·2^e with m normalised to 64
// bits, every decimal that parses back to it lies in [m − ½ulp, m + ½ulp]
// (the low end is a quarter ulp when m is a power of two, whose lower
// neighbour is half as far away; the ends are excluded when m is odd,
// because a tie rounds to even). Pick the decimal exponent p that makes
// that interval between one and ten units wide, scale both ends by 10^p
// with a 128-bit multiplication by ⌈10^p·2^k⌉, and take the integers
// inside. If some multiple of ten is inside, that is the answer with one
// digit fewer; otherwise the interval holds at most a few integers and the
// one nearest m·2^e·10^p is strconv's. The multiplication keeps two
// fraction bits and a sticky bit, enough to floor, ceil and round-half-even
// exactly: the paper proves the truncated 128-bit power never puts an
// integer on the wrong side.
//
// Every finite float64 and float32 comes here, in fixed notation (|v| in
// [1e-6, 1e21)) or exponent notation; a float32 runs the same search with
// its own mantissa width and the ends of its own, wider interval, which is
// why the short decimal path below, a float64 argument, does not serve it.
// TestAppendFloatMatchesStrconv holds the output byte for byte to strconv's.

// pow10Entry is hi·2^64 − lo: the form in which the generator stores a
// rounded-up 128-bit power of ten, chosen so that the product's high word
// can be corrected with a single borrow.
type pow10Entry struct {
	hi, lo uint64
}

// The range of ftoaPow10, the table pow10gen.go writes: the search asks for
// p between -296 and 340 over the float64 range (the smallest subnormal is
// 4.9e-324 and the largest value 1.8e308), and the margin is for the log
// estimates. The fixed notation range alone needs -5 to 22.
const (
	ftoaPow10Min = -350
	ftoaPow10Max = 350
)

//go:generate go run pow10gen.go

// unrounded is a real number kept as ⌊4x⌋ with the low bit set when the
// value is not exactly that: two fraction bits and a sticky bit, enough to
// round it any way.
type unrounded uint64

func (u unrounded) floor() uint64 { return uint64(u >> 2) }
func (u unrounded) ceil() uint64  { return uint64((u + 3) >> 2) }

// round rounds half to even.
func (u unrounded) round() uint64 { return uint64((u + 1 + (u>>2)&1) >> 2) }

// log10Pow2 returns ⌊x·log₁₀2⌋ for |x| below 2^14.
func log10Pow2(x int) int { return (x * 78913) >> 18 }

// log2Pow10 returns ⌊x·log₂10⌋ for |x| below 2^14.
func log2Pow10(x int) int { return (x * 108853) >> 15 }

// log10Skewed returns ⌊log₁₀(¾·2^x)⌋, the decimal exponent of the interval
// around a power of two, whose width is three quarters of an ulp.
func log10Skewed(x int) int { return (x*631305 - 261663) >> 21 }

// scale returns x·2^e·10^p as an unrounded, for x with its high bit set,
// pow the table entry for p and s = −(e + ⌊p·log₂10⌋ + 3), which the
// caller keeps in [0, 64).
func scale(x uint64, pow pow10Entry, s uint) unrounded {
	hi, mid := bits.Mul64(x, pow.hi)
	s &= 63
	if hi>>s<<s != hi {
		// Bits below the shift are set: the value is inexact whatever the
		// low product subtracts, and the subtraction cannot reach the kept
		// bits.
		return unrounded(hi>>s | 1)
	}
	mid2, _ := bits.Mul64(x, pow.lo)
	if mid < mid2 {
		hi--
	}
	var sticky uint64
	if mid-mid2 > 1 {
		sticky = 1
	}
	return unrounded(hi>>s | sticky)
}

// shortest returns the shortest decimal d·10^p that parses back to the
// float m·2^e, for m normalised so that its high bit is set, mantBits the
// width of the format's mantissa and minExp the e below which the float
// is subnormal and its ulp no longer shrinks with it.
func shortest(m uint64, e, mantBits, minExp int) (d uint64, p int) {
	z := 63 - mantBits // the ulp, in units of m
	var lo, hi uint64
	switch {
	case m == 1<<63 && e > minExp:
		// A power of two: the neighbour below is half as far.
		p = -log10Skewed(e + z)
		lo = m - 1<<(z-2)
		hi = m + 1<<(z-1)
	case e >= minExp:
		p = -log10Pow2(e + z)
		lo = m - 1<<(z-1)
		hi = m + 1<<(z-1)
	default:
		// Subnormal: the ulp is the smallest one, however far m was
		// shifted to normalise it.
		z += minExp - e
		p = -log10Pow2(e + z)
		lo = m - 1<<(z-1)
		hi = m + 1<<(z-1)
	}
	odd := unrounded(m>>z) & 1

	pow := ftoaPow10[p-ftoaPow10Min]
	s := uint(-(e + log2Pow10(p) + 3))
	// An odd mantissa excludes the ends: nudging each by a quarter before
	// rounding moves an end that landed exactly on an integer inside.
	dmin := (scale(lo, pow, s) + odd).ceil()
	dmax := (scale(hi, pow, s) - odd).floor()

	if d = dmax / 10; d*10 >= dmin {
		return d, 1 - p
	}
	if d = dmin; d < dmax {
		d = scale(m, pow, s).round()
	}
	return d, -p
}

// pow10u holds the powers of ten a uint64 holds.
var pow10u = [...]uint64{
	1e0, 1e1, 1e2, 1e3, 1e4, 1e5, 1e6, 1e7, 1e8, 1e9,
	1e10, 1e11, 1e12, 1e13, 1e14, 1e15, 1e16, 1e17, 1e18, 1e19,
}

// numDigits returns the number of decimal digits in d ≥ 1.
func numDigits(d uint64) int {
	nd := log10Pow2(bits.Len64(d))
	if d >= pow10u[nd] {
		nd++
	}
	return nd
}

// digits8 returns the eight ASCII digits of x < 1e8 as a little-endian word,
// the most significant digit in the low byte, so that storing the word
// writes them in reading order. Three multiplications split the number
// in halves, quarters and digits; TestDigits8 runs every input.
func digits8(x uint64) uint64 {
	// Two four-digit halves, the high one in the low lane.
	hi := (x * 0xd1b71759) >> 45 // x / 10000
	v := hi | (x-hi*10000)<<32
	// Four two-digit quarters, each lane's quotient kept above the shift
	// by the mask so that the shift does not mix the lanes.
	q := ((v * 5243) & 0xfff80000fff80000) >> 19 // v / 100
	v = q | (v-q*100)<<16
	// Eight digits.
	q = ((v * 103) & 0xfc00fc00fc00fc00) >> 10 // v / 10
	v = q | (v-q*10)<<8
	return v + 0x3030303030303030
}

// Short decimals.
//
// Most floats in real documents were short decimals before they were
// parsed: prices, ratios, coordinates to a few places. For those the
// search above is more than is needed, and the general writer's fixed
// layout is wider than the number. For each number of places f in turn the
// candidate is N = round(v·10^f), and it is the answer exactly when N/10^f
// parses back to v. That test is one IEEE division: for N below 2^53 and
// 10^f exact, float64(N)/10^f is the correctly rounded quotient, which is
// the float strconv.ParseFloat returns for the decimal N/10^f. The first f
// whose candidate passes is the shortest, and N, the nearest integer to the
// product, is the closest candidate of that length, which is the one the
// search would find. A real decimal passes in one or two rounds; what
// keeps a full precision value from paying for all seven is a gate in
// front: v·10^7 is an integer for every decimal of at most seven places,
// so a product away from one turns the value away at the cost of a
// multiplication. A neighbour of a short decimal, one or two ulps away,
// passes the gate and fails the divisions, and costs the rounds.
//
// The digits are then at most fifteen, and a number of at most eight
// bytes, which is nearly all of them, is shifted into one word a digit at
// a time and stored whole; a wider one is assembled in two words from the
// digits8 words of its integer part and its fraction.

// maxShortFrac is the most fraction digits the short path handles.
const maxShortFrac = 7

// roundMagic is 1.5·2^52: adding it to a non-negative float64 below 2^51
// and subtracting it again leaves the nearest integer, ties to even, in two
// floating point operations; math.Round is a call and an integer round trip
// carries a false register dependency that measured five times the cost.
const roundMagic = 1.5 * (1 << 52)

// appendShortFloat appends abs, with a sign when neg, if it is a decimal
// with 1 to maxShortFrac fraction digits and fewer than nine integer
// digits, and reports whether it did. abs is finite, not zero and not an
// integer.
func appendShortFloat(dst []byte, neg bool, abs float64) ([]byte, bool) {
	// The places, fewest first: the first candidate that parses back is
	// the shortest. One place is tried before anything else, since it is
	// what most values have and the round is all they should pay.
	for f := 1; f <= maxShortFrac; f++ {
		if f == 2 {
			// The gate, before the rest of the search: a full precision
			// value leaves here, after one round instead of seven.
			p := abs * 1e7
			if p >= 1e15 {
				return dst, false
			}
			r := (p + roundMagic) - roundMagic
			if math.Abs(p-r) > 1e-15*p {
				return dst, false
			}
		}
		p := abs * pow10[f]
		if p >= 1e15 {
			// Beyond where an integer is exact in a float64 and the
			// division is a proof.
			return dst, false
		}
		// A decimal's product lands within a couple of ulps of its
		// integer; 1e-15·p is about four. One branch on the absolute
		// value: two, on the sign of a distance that is random for the
		// values turned away, mispredict half the time. Rounding is two
		// floating point operations (see roundMagic).
		r := (p + roundMagic) - roundMagic
		if math.Abs(p-r) > 1e-15*p || r/pow10[f] != abs {
			continue
		}
		n := uint64(int64(r))
		if n >= 1e7 || f == maxShortFrac {
			// More than eight bytes: seven digits and the point behind
			// an integer part, or seven places behind "0.".
			return appendShortWide(dst, neg, abs, n, f)
		}

		// At most eight bytes: the digits are shifted into one word from
		// the last, the point among them, so that the first ends in the
		// low byte, and the word is stored whole.
		var w uint64
		for range f {
			q := n / 10
			w = w<<8 | '0' + n - q*10
			n = q
		}
		w = w<<8 | '.'
		l := f + 1
		for {
			q := n / 10
			w = w<<8 | '0' + n - q*10
			n = q
			l++
			if n == 0 {
				break
			}
		}

		if cap(dst)-len(dst) < 9 {
			dst = slices.Grow(dst, 9)
		}
		k := len(dst)
		out := dst[k : k+9]
		if neg {
			out[0] = '-'
			out = out[1:]
			k++
		}
		binary.LittleEndian.PutUint64(out[0:8], w)
		return dst[:k+l], true
	}
	return dst, false
}

// appendShortWide appends the decimal n·10^-f = abs, n of eight digits or
// more, as appendShortFloat's wider case: the integer digits, the point
// and the fraction digits assembled in two words, each part's digits at
// the top of its digits8 word.
func appendShortWide(dst []byte, neg bool, abs float64, n uint64, f int) ([]byte, bool) {
	// The integer part is the float's own, one conversion: the decimal
	// crosses no integer the float does not, since any integer between
	// them would be a float64 nearer the decimal.
	i := uint64(int64(abs))
	if i >= 1e8 {
		return dst, false
	}
	ip := numDigits(i | 1) // a value below one prints its zero
	a := digits8(i) >> (64 - 8*uint(ip))
	c := digits8(n-i*pow10u[f]) >> (64 - 8*uint(f))
	o := 8 * uint(ip+1) // byte offset of the fraction
	w0 := a | '.'<<(8*uint(ip)) | c<<o
	var w1 uint64
	if o >= 64 {
		// Seven or eight integer digits put the fraction, and for eight
		// the point too, in the second word.
		w1 = c<<(o-64) | '.'>>(64-8*uint(ip))
	} else {
		w1 = c >> (64 - o)
	}

	if cap(dst)-len(dst) < 17 {
		dst = slices.Grow(dst, 17)
	}
	k := len(dst)
	out := dst[k : k+17]
	if neg {
		out[0] = '-'
		out = out[1:]
		k++
	}
	binary.LittleEndian.PutUint64(out[0:8], w0)
	binary.LittleEndian.PutUint64(out[8:16], w1)
	return dst[:k+ip+1+f], true
}

// nonzeroBytes returns w with bit 7 of each byte set exactly when the byte
// is not zero.
func nonzeroBytes(w uint64) uint64 {
	return (w&0x7f7f7f7f7f7f7f7f + 0x7f7f7f7f7f7f7f7f | w) & 0x8080808080808080
}

// digitZeros is the ASCII zero in every byte.
const digitZeros = 0x3030303030303030

// appendFixed appends abs = d·10^p in fixed notation, with a sign when neg:
// d > 0 with at most 17 digits, abs the float64 the digits were found for,
// and 1e-6 <= abs < 1e21.
//
// Nothing here shifts a digit into place. The integer part is
// ⌊abs⌋, one conversion instruction, since the shortest decimal of a
// float64 below 2^53 crosses no integer the float does not; the fraction
// is what remains of d. Each part is written as a fixed-width, zero-padded
// string ending where it must end in a scratch buffer, the fraction first
// and the integer part and the point over its padding, so that every
// store is a whole word at a computed offset and a value's shape changes
// offsets rather than code paths. The trailing zeros a short decimal
// carries (40.8 is found as 4080000000000000) end the fraction's last
// word, are counted as its leading zero bytes, and are left outside the
// length. The scratch is then copied out in four whole words.
func appendFixed(dst []byte, neg bool, abs float64, d uint64, p int) []byte {
	nd := numDigits(d)
	dp := nd + p // digits before the point
	if dp >= nd {
		return appendFixedInteger(dst, neg, d, dp-nd)
	}

	// The integer part, and the fraction with its f digits.
	f := -p
	ip := max(dp, 1) // digits of integer part: a lone zero below one
	i := uint64(int64(abs))
	fr := d - i*pow10u[min(f, 19)] // zero above 1e17, where i is zero

	// A 24 byte scratch: the number is at most 24 bytes, and starts 16 in
	// so that the padding of its first word has somewhere to land.
	var buf [48]byte
	const start = 16
	end := start + ip + 1 + f

	// The fraction, 17 digits wide, its last word first: the padding of
	// each store is overwritten by the next.
	top := fr / 1e16
	rest := fr - top*1e16
	q := rest / 1e8
	lo := digits8(rest - q*1e8)
	hi := digits8(q)
	binary.LittleEndian.PutUint64(buf[end-8:], lo)
	binary.LittleEndian.PutUint64(buf[end-16:], hi)
	if f > 16 {
		binary.LittleEndian.PutUint64(buf[end-24:], digitZeros|('0'+top)<<56)
	}
	// The integer part ends at the point.
	if i >= 1e8 {
		binary.LittleEndian.PutUint64(buf[start+ip-16:], digits8(i/1e8))
		i %= 1e8
	}
	binary.LittleEndian.PutUint64(buf[start+ip-8:], digits8(i))
	buf[start+ip] = '.'

	// Trailing zeros: leading zero bytes of the last word, then of the one
	// before it, then the top digit.
	nz := nonzeroBytes(lo ^ digitZeros)
	tz := bits.LeadingZeros64(nz) >> 3
	if nz == 0 {
		nz = nonzeroBytes(hi ^ digitZeros)
		tz = 8 + bits.LeadingZeros64(nz)>>3
		if nz == 0 {
			tz = 16
		}
	}
	if tz >= f {
		// A fraction of nothing but zeros, which no float64 sends here
		// but a caller might: an integer, without the point.
		tz = f + 1
	}

	// Out in four words, over 32 bytes of capacity.
	from := start
	if neg {
		buf[start-1] = '-'
		from--
	}
	if cap(dst)-len(dst) < 32 {
		dst = slices.Grow(dst, 32)
	}
	n := len(dst)
	out := dst[n : n+32]
	src := buf[from : from+32]
	binary.LittleEndian.PutUint64(out[0:], binary.LittleEndian.Uint64(src[0:]))
	binary.LittleEndian.PutUint64(out[8:], binary.LittleEndian.Uint64(src[8:]))
	binary.LittleEndian.PutUint64(out[16:], binary.LittleEndian.Uint64(src[16:]))
	binary.LittleEndian.PutUint64(out[24:], binary.LittleEndian.Uint64(src[24:]))
	return dst[:n+end-tz-from]
}

// appendFixedInteger appends d followed by zeros zeros: a value of 1e15
// or more, which arrives here only when it is not an integer below 1e15,
// so seldom enough for a byte loop.
func appendFixedInteger(dst []byte, neg bool, d uint64, zeros int) []byte {
	if neg {
		dst = append(dst, '-')
	}
	dst = strconv.AppendUint(dst, d, 10)
	for range zeros {
		dst = append(dst, '0')
	}
	return dst
}

// appendFloatSearch appends finite v with the fewest digits that parse
// back to it at width (32 or 64) bits of precision: in fixed notation when
// format is 'f', which the caller uses for 1e-6 <= |v| < 1e21 and zero,
// and otherwise in exponent notation.
func appendFloatSearch(dst []byte, v float64, width int, format byte) []byte {
	var mant uint64
	var exp, mantBits, bias, minExp int
	var neg bool
	if width == 32 {
		b := math.Float32bits(float32(v))
		neg = b>>31 != 0
		exp = int(b>>23) & 0xff
		mant = uint64(b & (1<<23 - 1))
		mantBits, bias, minExp = 23, 127, -189
	} else {
		b := math.Float64bits(v)
		neg = b>>63 != 0
		exp = int(b>>52) & 0x7ff
		mant = b & (1<<52 - 1)
		mantBits, bias, minExp = 52, 1023, -1085
	}
	if exp == 0 {
		if mant == 0 {
			if neg {
				return append(dst, '-', '0')
			}
			return append(dst, '0')
		}
		exp = 1
	} else {
		mant |= 1 << mantBits
	}
	s := bits.LeadingZeros64(mant)
	d, p := shortest(mant<<s, exp-bias-mantBits-s, mantBits, minExp)
	if format == 'e' {
		return appendExponent(dst, neg, d, p)
	}
	return appendFixed(dst, neg, math.Abs(v), d, p)
}

// appendExponent appends d·10^p as encoding/json's exponent notation: the
// first digit, a point and the rest when there are more, then 'e', the
// sign and the exponent without padding (strconv pads a two digit
// exponent and encoding/json takes the zero off a negative one; a
// positive exponent here is at least 21). Like appendFixed it writes the
// digits8 words at the end of a scratch, moves the first digit down a
// byte to make room for the point, counts the trailing zeros on the last
// word, puts the exponent where the digits stop, and copies four words
// out; a funnel shift that aligned the words in registers instead
// measured 24 ns against 15 for this.
func appendExponent(dst []byte, neg bool, d uint64, p int) []byte {
	nd := numDigits(d)
	exp := p + nd - 1

	// The digits, 17 wide, ending at end.
	var buf [64]byte
	const end = 24
	top := d / 1e16
	rest := d - top*1e16
	q := rest / 1e8
	lo := digits8(rest - q*1e8)
	hi := digits8(q)
	binary.LittleEndian.PutUint64(buf[end-8:], lo)
	binary.LittleEndian.PutUint64(buf[end-16:], hi)
	buf[end-17] = byte('0' + top)

	// Trailing zeros: leading zero bytes of the last word, then of the one
	// before it, then the top digit.
	nz := nonzeroBytes(lo ^ digitZeros)
	tz := bits.LeadingZeros64(nz) >> 3
	if nz == 0 {
		nz = nonzeroBytes(hi ^ digitZeros)
		tz = 8 + bits.LeadingZeros64(nz)>>3
		if nz == 0 {
			tz = 16
		}
	}

	// The first digit down one byte, the point in its place.
	from := end - nd - 1
	buf[from] = buf[from+1]
	buf[from+1] = '.'
	n := end - tz
	if nd-tz == 1 {
		n = from + 1 // over the point
	}

	// The exponent, without branches on its sign or width, which a
	// document of assorted magnitudes makes unpredictable: three digits
	// in a word, shifted down to the ones it has.
	sign := byte('+')
	if exp < 0 {
		sign = '-'
		exp = -exp
	}
	l := 1
	if exp >= 10 {
		l = 2
	}
	if exp >= 100 {
		l = 3
	}
	tail := uint64('e') | uint64(sign)<<8 |
		(uint64('0'+exp/100)|uint64('0'+exp/10%10)<<8|uint64('0'+exp%10)<<16)>>(8*uint(3-l))<<16
	binary.LittleEndian.PutUint64(buf[n:], tail)
	n += 2 + l

	if neg {
		from--
		buf[from] = '-'
	}
	if cap(dst)-len(dst) < 32 {
		dst = slices.Grow(dst, 32)
	}
	k := len(dst)
	out := dst[k : k+32]
	src := buf[from : from+32]
	binary.LittleEndian.PutUint64(out[0:], binary.LittleEndian.Uint64(src[0:]))
	binary.LittleEndian.PutUint64(out[8:], binary.LittleEndian.Uint64(src[8:]))
	binary.LittleEndian.PutUint64(out[16:], binary.LittleEndian.Uint64(src[16:]))
	binary.LittleEndian.PutUint64(out[24:], binary.LittleEndian.Uint64(src[24:]))
	return dst[:k+n-from]
}

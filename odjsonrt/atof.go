package odjsonrt

import (
	"encoding/binary"
	"math"
	"math/bits"
)

// Float parsing.
//
// strconv.ParseFloat scans the literal once to find its digits, then again
// to fit them: a 53-bit mantissa from at most 19 digits and a power of ten,
// exact when both are small (Clinger's fast path: one correctly rounded
// division or multiplication of two exact floats) and otherwise through
// Eisel and Lemire's 128-bit product with a truncated power of ten, which
// settles every literal but a halfway case or a subnormal and says which
// it could not. The literal a generated decoder hands over has already
// been located, so this file does the scan, the fit and JSON's grammar
// check in one pass over the bytes, eight fraction digits at a time, and
// declines anything it cannot settle bit for bit; the general path in
// [ParseFloat] then runs strconv over it, which is the only thing that can
// produce an error. TestParseSimpleFloatMatchesStrconv holds the result to
// strconv's bits on every literal it accepts.

// pow10Floor is the form in which pow10gen.go stores a rounded-down 128-bit
// power of ten: hi·2^64 + lo.
type pow10Floor struct {
	hi, lo uint64
}

// The range of atofPow10, the table pow10gen.go writes: the decimal
// exponents a float64 can carry, with the digits' own contribution. A
// literal outside it is far below the smallest subnormal or far above the
// largest float64, and strconv, which knows how to say so, gets it.
const (
	atofPow10Min = -342
	atofPow10Max = 308
)

// pow10 holds the powers of ten a float64 represents exactly.
var pow10 = [...]float64{
	1e0, 1e1, 1e2, 1e3, 1e4, 1e5, 1e6, 1e7, 1e8, 1e9, 1e10, 1e11,
	1e12, 1e13, 1e14, 1e15, 1e16, 1e17, 1e18, 1e19, 1e20, 1e21, 1e22,
}

// pow10f32 holds the powers of ten a float32 represents exactly.
var pow10f32 = [...]float32{1e0, 1e1, 1e2, 1e3, 1e4, 1e5, 1e6, 1e7, 1e8, 1e9, 1e10}

// ParseSimpleFloat reads the JSON number at p in one pass and returns its
// value as a float of the given bit size (32 or 64), or declines. It
// accepts a literal of at most nineteen digits whose value it can settle
// bit for bit with what strconv.ParseFloat returns: for float64 by one
// exact floating point operation when the digits fit the mantissa, and
// otherwise by the Eisel-Lemire product, which declines a literal that
// lands exactly halfway between two floats or below the normal range; for
// float32 by the exact operation alone. Everything else, including every
// malformed literal, is declined and left to the general path.
func ParseSimpleFloat(data []byte, p int, bitSize int) (float64, int, bool) {
	i := p
	neg := uint(i) < uint(len(data)) && data[i] == '-'
	if neg {
		i++
	}
	start := i
	var m uint64
	for uint(i) < uint(len(data)) {
		c := data[i] - '0'
		if c > 9 {
			break
		}
		m = m*10 + uint64(c)
		i++
	}
	digits := i - start
	if digits == 0 || (data[start] == '0' && digits > 1) {
		return 0, p, false
	}
	frac := 0
	if uint(i) < uint(len(data)) && data[i] == '.' {
		i++
		fs := i
		// Eight digits at a time: a word of the fraction is tested for
		// digits at once and, when it is all digits, folded into m with
		// three multiplications; the word that ends the fraction has its
		// digits moved to the top and the rest filled with zeros, which
		// fold to the value of the digits alone. A fraction of one digit,
		// which most short decimals have, is read by the byte loop below
		// instead: the word's three multiplications cost more than its
		// one step.
		for uint(i+8) <= uint(len(data)) && data[i+1]-'0' <= 9 {
			t := binary.LittleEndian.Uint64(data[i:i+8]) ^ digitZeros
			nz := (t + 0x7676767676767676 | t) & 0x8080808080808080
			if nz == 0 {
				m = m*1e8 + fold8(t)
				i += 8
				continue
			}
			n := bits.TrailingZeros64(nz) >> 3
			m = m*pow10u[n] + fold8(t<<(8*uint(8-n)))
			i += n
			break
		}
		for uint(i) < uint(len(data)) {
			c := data[i] - '0'
			if c > 9 {
				break
			}
			m = m*10 + uint64(c)
			i++
		}
		frac = i - fs
		if frac == 0 {
			return 0, p, false
		}
		digits += frac
	}
	if digits > 19 {
		return 0, p, false
	}
	exp := 0
	if uint(i) < uint(len(data)) && data[i]|0x20 == 'e' {
		if bitSize == 32 {
			return 0, p, false
		}
		i++
		eneg := false
		if uint(i) < uint(len(data)) && (data[i] == '+' || data[i] == '-') {
			eneg = data[i] == '-'
			i++
		}
		es := i
		for uint(i) < uint(len(data)) {
			c := data[i] - '0'
			if c > 9 {
				break
			}
			if exp < 10000 {
				exp = exp*10 + int(c)
			}
			i++
		}
		if i == es {
			return 0, p, false
		}
		if eneg {
			exp = -exp
		}
	}

	var f float64
	if bitSize == 32 {
		if m >= 1<<24 || frac >= len(pow10f32) {
			return 0, p, false
		}
		f = float64(float32(m) / pow10f32[frac])
	} else if exp == 0 && m < 1<<53 && frac < len(pow10) {
		// The common shape, both parts exact: one correctly rounded
		// division (Clinger's fast path).
		f = float64(m) / pow10[frac]
	} else {
		q := exp - frac
		switch {
		case m == 0:
			// Zero whatever the exponent; the sign survives.
		case m < 1<<53 && q >= -len(pow10)+1 && q <= len(pow10)-1:
			// Both exact, with an exponent: one correctly rounded
			// operation either way.
			f = float64(m)
			if q < 0 {
				f /= pow10[-q]
			} else {
				f *= pow10[q]
			}
		default:
			// The Eisel-Lemire product, from "Number Parsing at a
			// Gigabyte per Second" (Lemire, 2021): m normalised to 64
			// bits is multiplied by the truncated 128-bit power of ten,
			// and the high 54 bits of the product are the mantissa and
			// its rounding bit unless the truncation could have moved
			// them, a product whose low bits are all ones, in which case
			// the low word of the power is folded in. A product that
			// still cannot be settled, one that lands exactly on a
			// rounding boundary, where the truncation error decides, and
			// a result outside the normal range are declined. It is
			// written out here rather than called: a call, even one
			// never taken, gives this function a frame and a stack
			// check, which every float would pay.
			if q < atofPow10Min || q > atofPow10Max {
				return 0, p, false
			}
			clz := bits.LeadingZeros64(m)
			w := m << uint(clz)
			// ⌊q·log₂10⌋ + the biases of the two normalisations.
			e2 := uint64((217706*q)>>16+64+1023) - uint64(clz)

			pow := atofPow10[q-atofPow10Min]
			hi, lo := bits.Mul64(w, pow.hi)
			if hi&0x1ff == 0x1ff && lo+w < w {
				hi2, lo2 := bits.Mul64(w, pow.lo)
				mergedHi, mergedLo := hi, lo+hi2
				if mergedLo < lo {
					mergedHi++
				}
				if mergedHi&0x1ff == 0x1ff && mergedLo+1 == 0 && lo2+w < w {
					return 0, p, false
				}
				hi, lo = mergedHi, mergedLo
			}

			msb := hi >> 63
			mant := hi >> (msb + 9)
			e2 -= 1 ^ msb
			if lo == 0 && hi&0x1ff == 0 && mant&3 == 1 {
				// Exactly halfway, as far as the product can tell.
				return 0, p, false
			}
			mant += mant & 1
			mant >>= 1
			if mant>>53 > 0 {
				mant >>= 1
				e2++
			}
			if e2-1 >= 0x7ff-1 {
				// Subnormal, zero, infinite or beyond.
				return 0, p, false
			}
			f = math.Float64frombits(e2<<52 | mant&(1<<52-1))
		}
	}
	if neg {
		f = -f
	}
	return f, i, true
}

// fold8 returns the value of the eight decimal digits held one per byte in
// t, the first digit in the low byte: pairs, then quads, then the whole,
// each step a multiplication and an add.
func fold8(t uint64) uint64 {
	t = (t*10 + t>>8) & 0x00ff00ff00ff00ff
	t = (t*100 + t>>16) & 0x0000ffff0000ffff
	return (t*10000 + t>>32) & 0xffffffff
}

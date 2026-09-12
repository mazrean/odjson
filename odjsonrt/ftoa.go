package odjsonrt

import "math"

// Short decimals.
//
// strconv's shortest formatting runs Ryu on every float64, and most of the
// floats in real documents are prices, coordinates and ratios that were a
// short decimal before they were parsed: 40.8, 12.99, 139.69171. For those
// the shortest representation that round-trips is that decimal, and it can
// be found without Ryu: for each number of fraction digits f in turn, the
// candidate is N = round(v·10^f), and it is the answer exactly when N/10^f
// parses back to v. That test is one IEEE division: for N below 2^53 and
// 10^f exact, float64(N)/10^f is the correctly rounded quotient, which is
// the double strconv.ParseFloat returns for the decimal N/10^f. The first f
// whose candidate passes is the shortest, and N, the nearest integer to the
// product, is the closest candidate of that length, which is the one Ryu
// prints. Anything the loop does not settle, including every value with
// more than maxShortFrac fraction digits, goes to strconv, so the output is
// strconv's in every case; TestAppendFloatMatchesStrconv holds it there.
//
// A full precision value (a coordinate, a measurement) is usually a
// neighbour of some short decimal, one or two ulps away, and its product
// lands within an ulp of an integer just like the decimal's would. The
// division tells the two apart where nothing cheaper can, so a neighbour
// costs the loop plus one division before it is handed to strconv.

// pow10u holds the powers of ten a uint64 holds.
var pow10u = [...]uint64{
	1e0, 1e1, 1e2, 1e3, 1e4, 1e5, 1e6, 1e7, 1e8, 1e9,
	1e10, 1e11, 1e12, 1e13, 1e14, 1e15, 1e16, 1e17, 1e18, 1e19,
}

// maxShortFrac is the most fraction digits the loop considers. Real
// decimals rarely carry more, and the path is cheap only while it is a
// handful of multiplications.
const maxShortFrac = 7

// roundMagic is 1.5·2^52: adding it to a non-negative float64 below 2^51
// and subtracting it again leaves the nearest integer, ties to even.
const roundMagic = 1.5 * (1 << 52)

// appendShortFloat appends v when it is a short decimal, and reports
// whether it did. v must be finite with 1e-6 <= |v| < 1e21, the range
// strconv prints without an exponent, and not an integer below 1e15; the
// caller has settled the rest.
func appendShortFloat(dst []byte, v float64) ([]byte, bool) {
	abs := math.Abs(v)
	for f := 1; f <= maxShortFrac; f++ {
		p := abs * pow10[f]
		if p >= 1e15 {
			// The candidate would leave the range where an integer is
			// exact in a float64 and the division is a proof.
			return dst, false
		}
		// Round p to the nearest integer by adding and subtracting 1.5·2^52,
		// which rounds away everything below the units in the addition:
		// two FP operations, where math.Round is a call and an int64
		// round trip carries a conversion whose false register dependency
		// measured five times the cost of the loop in situ.
		r := (p + roundMagic) - roundMagic
		// A decimal's product lands within a couple of ulps of its
		// integer; anything further away cannot be one and skips the
		// division. 1e-15·p is about four ulps. One branch on the absolute
		// value: two, on the sign of a distance that is random for the
		// values turned away here, mispredicted half the time and cost
		// more than the rest of the loop.
		if math.Abs(p-r) > 1e-15*p {
			continue
		}
		if r == 0 || r/pow10[f] != abs {
			continue
		}
		n := int64(r)

		// The digits, written from the last one: f of them behind the
		// point, which pads a value below one with zeros on its own, then
		// the rest.
		var buf [24]byte
		i := len(buf)
		for range f {
			i--
			buf[i] = byte('0' + n%10)
			n /= 10
		}
		i--
		buf[i] = '.'
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
	return dst, false
}

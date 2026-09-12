package odjsonrt

import (
	"math"
	"math/big"
	"math/rand/v2"
	"strconv"
	"testing"
)

// appendFloatRef is what encoding/json does with strconv, the answer
// AppendFloat must reproduce byte for byte at either width.
func appendFloatRef(dst []byte, v float64, bits int) []byte {
	abs := math.Abs(v)
	format := byte('f')
	if abs != 0 {
		if bits == 64 && (abs < 1e-6 || abs >= 1e21) ||
			bits == 32 && (float32(abs) < 1e-6 || float32(abs) >= 1e21) {
			format = 'e'
		}
	}
	dst = strconv.AppendFloat(dst, v, format, -1, bits)
	if format == 'e' {
		n := len(dst)
		if n >= 4 && dst[n-4] == 'e' && dst[n-3] == '-' && dst[n-2] == '0' {
			dst[n-2] = dst[n-1]
			dst = dst[:n-1]
		}
	}
	return dst
}

// TestAppendFloatMatchesStrconv checks AppendFloat against strconv on
// every short decimal and the floats next to each, on random bit patterns
// across the fixed notation range and then across the whole of both
// formats, subnormals included, and on the edges. A single byte of
// difference is a failure: the path must print exactly what strconv
// prints.
func TestAppendFloatMatchesStrconv(t *testing.T) {
	var buf, ref []byte
	fails := 0
	checkAt := func(v float64, bits int) {
		buf, ref = buf[:0], ref[:0]
		var err error
		if buf, err = AppendFloat(buf, v, bits); err != nil {
			t.Fatalf("%v: %v", v, err)
		}
		ref = appendFloatRef(ref, v, bits)
		if string(buf) != string(ref) {
			if fails++; fails < 20 {
				t.Errorf("AppendFloat(%v %#x, %d) = %q, want %q", v, math.Float64bits(v), bits, buf, ref)
			}
		}
	}
	check := func(v float64) { checkAt(v, 64) }
	both := func(v float64) {
		check(v)
		check(-v)
		check(math.Nextafter(v, 0))
		check(math.Nextafter(v, math.Inf(1)))
	}

	// Every decimal with up to six significant digits and up to seven
	// fraction digits, and its floating point neighbours.
	for d := uint64(1); d <= 1_000_000; d++ {
		for k := 0; k <= 7; k++ {
			both(float64(d) / pow10[k])
		}
	}
	// Longer decimals, and decimals scaled up into the integer range.
	r := rand.New(rand.NewPCG(7, 8))
	for range 2_000_000 {
		d := r.Uint64N(1 << 53)
		k := r.IntN(20)
		both(float64(d) / pow10[k])
		both(float64(d) * pow10[r.IntN(8)])
	}
	// Boundaries of the fixed notation range, powers of two, and the
	// mantissa edges around every binade in range.
	for _, v := range []float64{1e-6, 1e-5, 0.1, 0.5, 1, 1e15, 1e16, 1e17, 1e18, 1e20, 1e21} {
		both(v)
	}
	for e := -20; e <= 70; e++ {
		p := math.Ldexp(1, e)
		both(p)
		both(math.Nextafter(p, 0))
		both(math.Ldexp(1<<53-1, e-52))
	}
	// Random bit patterns in the fixed notation range, ties included.
	for range 5_000_000 {
		e := 1023 - 20 + r.IntN(70)
		v := math.Float64frombits(uint64(e)<<52 | r.Uint64N(1<<52))
		if v < 1e-6 || v >= 1e21 {
			continue
		}
		check(v)
		check(-v)
	}
	// Exact halves and quarters at every scale, where ties to even matter.
	for e := 0; e <= 60; e++ {
		for _, num := range []float64{1, 3, 5, 7, 9, 11, 13, 15} {
			both(math.Ldexp(num, e-3))
		}
	}
	// Large values: integers that print with trailing zeros, and the
	// non-integers between 1e15 and 1e21 that print as integers.
	for range 200_000 {
		both(float64(r.Uint64N(1<<53)) * pow10[r.IntN(6)+1])
		both(math.Float64frombits(uint64(1023+50+r.IntN(20))<<52 | r.Uint64N(1<<52)))
	}
	// Signed zeros.
	check(0)
	check(math.Copysign(0, -1))

	// Exponent notation: random bit patterns over every binade, both
	// ways, subnormals and the extremes included.
	for range 2_000_000 {
		v := math.Float64frombits(r.Uint64() &^ (1 << 63))
		if math.IsInf(v, 0) || math.IsNaN(v) {
			continue
		}
		check(v)
		check(-v)
	}
	for range 200_000 {
		both(math.Float64frombits(r.Uint64N(1 << 52))) // subnormal
	}
	for _, v := range []float64{math.MaxFloat64, math.SmallestNonzeroFloat64, 2.2250738585072014e-308, 1e-7, 1e-300, 1e21, 1e22, 1e300, 123456789e-20, 5e-324, 1e-6 / 2} {
		check(v)
		check(-v)
		check(math.Nextafter(v, 0))
	}
	// float32, at its own width: every bit pattern in the fixed range has
	// a shortest form of its own, so a sample of them, and of the rest.
	for range 3_000_000 {
		f := math.Float32frombits(r.Uint32())
		if math.IsInf(float64(f), 0) || f != f {
			continue
		}
		checkAt(float64(f), 32)
	}
	for _, f := range []float32{0, float32(math.Copysign(0, -1)), 1, 0.1, 0.3, 1e-6, 1e-7, 1e21, 1e20, 16777216, 16777217, 1048576.25, 3.4028235e38, 1.4e-45, 1.17549435e-38, 33554432, 0.5, 2.5} {
		checkAt(float64(f), 32)
		checkAt(-float64(f), 32)
	}
	if fails > 0 {
		t.Errorf("%d mismatches", fails)
	}
}

// TestDigits8 runs digits8 on every input.
func TestDigits8(t *testing.T) {
	var got [8]byte
	for x := range uint64(1e8) {
		w := digits8(x)
		for i := range got {
			got[i] = byte(w >> (8 * i))
		}
		want := strconv.FormatUint(x+1e8, 10)[1:]
		if string(got[:]) != want {
			t.Fatalf("digits8(%d) = %q, want %q", x, got, want)
		}
	}
}

// TestFtoaPow10 recomputes the table with math/big: each entry must be
// ⌈10^p · 2^k⌉ for the k that puts it in [2^127, 2^128), stored as
// hi·2^64 − lo.
func TestFtoaPow10(t *testing.T) {
	if n := ftoaPow10Max - ftoaPow10Min + 1; len(ftoaPow10) != n {
		t.Fatalf("table has %d entries, want %d", len(ftoaPow10), n)
	}
	one := big.NewInt(1)
	two := big.NewRat(2, 1)
	lo128 := new(big.Rat).SetInt(new(big.Int).Lsh(one, 127))
	hi128 := new(big.Rat).SetInt(new(big.Int).Lsh(one, 128))
	for p := ftoaPow10Min; p <= ftoaPow10Max; p++ {
		r := new(big.Rat)
		if p >= 0 {
			r.SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(p)), nil))
		} else {
			r.SetFrac(one, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(-p)), nil))
		}
		for r.Cmp(lo128) < 0 {
			r.Mul(r, two)
		}
		for r.Cmp(hi128) >= 0 {
			r.Quo(r, two)
		}
		want := new(big.Int).Quo(r.Num(), r.Denom())
		if !r.IsInt() {
			want.Add(want, one)
		}
		e := ftoaPow10[p-ftoaPow10Min]
		got := new(big.Int).Lsh(new(big.Int).SetUint64(e.hi), 64)
		got.Sub(got, new(big.Int).SetUint64(e.lo))
		if got.Cmp(want) != 0 {
			t.Errorf("1e%d: table holds %v, want %v", p, got, want)
		}
	}
}

// appendFixedRef formats d·10^p in fixed notation a byte at a time.
func appendFixedRef(dst []byte, neg bool, d uint64, p int) []byte {
	for d%10 == 0 {
		d /= 10
		p++
	}
	s := strconv.FormatUint(d, 10)
	dp := len(s) + p
	if neg {
		dst = append(dst, '-')
	}
	switch {
	case dp <= 0:
		dst = append(dst, '0', '.')
		for range -dp {
			dst = append(dst, '0')
		}
		dst = append(dst, s...)
	case dp < len(s):
		dst = append(dst, s[:dp]...)
		dst = append(dst, '.')
		dst = append(dst, s[dp:]...)
	default:
		dst = append(dst, s...)
		for range dp - len(s) {
			dst = append(dst, '0')
		}
	}
	return dst
}

// TestAppendFixedShapes checks the word-splicing writer against a byte
// loop on every digit count, every point position the fixed range allows,
// and digit strings with trailing zeros, appending to buffers with and
// without room.
func TestAppendFixedShapes(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	var got, want []byte
	for nd := 1; nd <= 17; nd++ {
		for dp := -5; dp <= 21; dp++ {
			for trial := range 200 {
				d := pow10u[nd-1] + r.Uint64N(pow10u[nd]-pow10u[nd-1])
				if trial%4 == 0 {
					// Trailing zeros, which the writer must trim.
					z := 1 + r.IntN(nd)
					d = d / pow10u[z] * pow10u[z]
					if d == 0 {
						continue
					}
				}
				p := dp - nd
				if float64(d)*math.Pow(10, float64(p)) < 1e-6 || float64(d)*math.Pow(10, float64(p)) >= 1e21 {
					continue
				}
				neg := trial%2 == 1
				// The float the digits stand for, as the writer sees it.
				abs, err := strconv.ParseFloat(strconv.FormatUint(d, 10)+"e"+strconv.Itoa(p), 64)
				if err != nil {
					t.Fatal(err)
				}
				want = appendFixedRef(make([]byte, 3, 64), neg, d, p)
				if string(want[3:]) != strconv.FormatFloat(math.Copysign(abs, 1-2*float64(trial%2)), 'f', -1, 64) {
					// The digits are not that float's shortest: no
					// float64 sends them, and the writer may assume one.
					continue
				}
				// Room for it, and a full buffer that has to grow.
				got = appendFixed(make([]byte, 3, 64), neg, abs, d, p)
				if string(got) != string(want) {
					t.Fatalf("appendFixed(%v, %d, %d) = %q, want %q", neg, d, p, got[3:], want[3:])
				}
				full := make([]byte, 5)
				got = appendFixed(full[:5:5], neg, abs, d, p)
				if string(got[:5]) != string(full) || string(got[5:]) != string(want[3:]) {
					t.Fatalf("appendFixed(%v, %d, %d) into a full buffer = %q, want %q", neg, d, p, got[5:], want[3:])
				}
			}
		}
	}
}

func BenchmarkAppendFloatShort(b *testing.B) {
	vals := []float64{40.8, -0.1, 0.1, 12.99, 139.69171}
	buf := make([]byte, 0, 64)
	for b.Loop() {
		for _, v := range vals {
			buf, _ = AppendFloat(buf[:0], v, 64)
		}
	}
}

func BenchmarkAppendFloatRef(b *testing.B) {
	vals := []float64{40.8, -0.1, 0.1, 12.99, 139.69171}
	buf := make([]byte, 0, 64)
	for b.Loop() {
		for _, v := range vals {
			buf = appendFloatRef(buf[:0], v, 64)
		}
	}
}

// Full precision coordinates, the shape canada.json is made of.
var benchCoords = func() []float64 {
	r := rand.New(rand.NewPCG(1, 2))
	v := make([]float64, 1024)
	for i := range v {
		v[i] = r.Float64()*360 - 180
	}
	return v
}()

func BenchmarkAppendFloatFull(b *testing.B) {
	buf := make([]byte, 0, 64)
	for b.Loop() {
		for _, v := range benchCoords {
			buf, _ = AppendFloat(buf[:0], v, 64)
		}
	}
}

func BenchmarkAppendFloatFullRef(b *testing.B) {
	buf := make([]byte, 0, 64)
	for b.Loop() {
		for _, v := range benchCoords {
			buf = appendFloatRef(buf[:0], v, 64)
		}
	}
}

// Values in exponent notation, a third of the numbers shape.
var benchExp = func() []float64 {
	r := rand.New(rand.NewPCG(3, 4))
	v := make([]float64, 1024)
	for i := range v {
		v[i] = math.Pow(10, float64(r.IntN(600)-300)) * r.Float64()
		if a := math.Abs(v[i]); a >= 1e-6 && a < 1e21 {
			v[i] *= 1e30
		}
	}
	return v
}()

func BenchmarkAppendFloatExp(b *testing.B) {
	buf := make([]byte, 0, 64)
	for b.Loop() {
		for _, v := range benchExp {
			buf, _ = AppendFloat(buf[:0], v, 64)
		}
	}
}

func BenchmarkAppendFloatExpRef(b *testing.B) {
	buf := make([]byte, 0, 64)
	for b.Loop() {
		for _, v := range benchExp {
			buf = appendFloatRef(buf[:0], v, 64)
		}
	}
}

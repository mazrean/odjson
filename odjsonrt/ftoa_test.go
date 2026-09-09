package odjsonrt

import (
	"math"
	"math/rand/v2"
	"strconv"
	"testing"
)

// appendFloatRef is AppendFloat's general path, the answer the short decimal
// path must reproduce byte for byte.
func appendFloatRef(dst []byte, v float64) []byte {
	abs := math.Abs(v)
	format := byte('f')
	if abs != 0 && (abs < 1e-6 || abs >= 1e21) {
		format = 'e'
	}
	dst = strconv.AppendFloat(dst, v, format, -1, 64)
	if format == 'e' {
		n := len(dst)
		if n >= 4 && dst[n-4] == 'e' && dst[n-3] == '-' && dst[n-2] == '0' {
			dst[n-2] = dst[n-1]
			dst = dst[:n-1]
		}
	}
	return dst
}

// TestAppendFloatMatchesStrconv checks the short decimal path against
// strconv on the values it is built for, every short decimal and the floats
// next to each, and on random bit patterns across the whole range it can
// see. A single byte of difference is a failure: the path must print
// exactly what strconv prints or decline.
func TestAppendFloatMatchesStrconv(t *testing.T) {
	var buf, ref []byte
	fails := 0
	check := func(v float64) {
		buf, ref = buf[:0], ref[:0]
		var err error
		if buf, err = AppendFloat(buf, v, 64); err != nil {
			t.Fatalf("%v: %v", v, err)
		}
		ref = appendFloatRef(ref, v)
		if string(buf) != string(ref) {
			if fails++; fails < 20 {
				t.Errorf("AppendFloat(%v %#x) = %q, want %q", v, math.Float64bits(v), buf, ref)
			}
		}
	}
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
	if fails > 0 {
		t.Errorf("%d mismatches", fails)
	}
}

// TestAppendShortFloatCoverage pins that the common shapes really do take
// the short path, since the benchmark gain rests on it.
func TestAppendShortFloatCoverage(t *testing.T) {
	for _, v := range []float64{40.8, -0.1, 0.1, 12.99, 139.69171, 35.6895, 0.001, 1234567.5, 99.99} {
		if _, ok := appendShortFloat(nil, v); !ok {
			t.Errorf("%v declined", v)
		}
	}
	for _, v := range []float64{0.30000000000000004, math.Pi, 1e22, 1e-6 * 1.000000000001} {
		if out, ok := appendShortFloat(nil, v); ok {
			t.Errorf("%v accepted as %q", v, out)
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
			buf = appendFloatRef(buf[:0], v)
		}
	}
}

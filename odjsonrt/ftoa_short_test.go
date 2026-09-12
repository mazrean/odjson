package odjsonrt

import (
	"math"
	"math/rand/v2"
	"testing"
)

// TestAppendShortFloatCoverage pins that short decimals take the short
// path and that what is not one is turned away, since the benchmark gain
// on small documents rests on the former and correctness on the latter.
func TestAppendShortFloatCoverage(t *testing.T) {
	for _, v := range []float64{40.8, 0.1, 12.99, 139.69171, 35.6895, 0.001, 1234567.5, 99.99, 0.0000012, 12345678.9} {
		if _, ok := appendShortFloat(nil, false, v); !ok {
			t.Errorf("%v declined", v)
		}
	}
	for _, v := range []float64{0.30000000000000004, math.Pi, 123456789.5, 0.12345678, 1e-6 * 1.000000000001} {
		if out, ok := appendShortFloat(nil, false, v); ok {
			t.Errorf("%v accepted as %q", v, out)
		}
	}
	r := rand.New(rand.NewPCG(7, 8))
	declined := 0
	for range 1000000 {
		f := 1 + r.IntN(maxShortFrac)
		n := 1 + r.Int64N(int64(pow10u[15-f]))
		if n%10 == 0 {
			continue
		}
		if v := float64(n) / pow10[f]; v < 1e8 {
			if _, ok := appendShortFloat(nil, false, v); !ok {
				declined++
			}
		}
	}
	if declined > 0 {
		t.Errorf("%d of 1000000 short decimals declined", declined)
	}
}

package odjsonrt

import (
	"strconv"
	"testing"
)

// TestAppendIntMatchesStrconv pins the digit writer against strconv over
// every length, every power-of-ten boundary and a pseudo-random spread: the
// shift that drops digits8's leading zeros is indexed by the digit count, so
// a wrong count at a boundary is exactly the bug to catch.
func TestAppendIntMatchesStrconv(t *testing.T) {
	var vals []uint64
	for p := range uint64(20) {
		var pow uint64 = 1
		for range p {
			pow *= 10
		}
		vals = append(vals, pow-1, pow, pow+1)
	}
	vals = append(vals, 0, 1<<63, 1<<63-1, ^uint64(0), ^uint64(0)-1)
	for x := uint64(1); len(vals) < 4000; x = x*6364136223846793005 + 1442695040888963407 {
		vals = append(vals, x, x>>13, x>>29, x>>47)
	}
	for _, v := range vals {
		if got, want := string(AppendUint(nil, v)), strconv.FormatUint(v, 10); got != want {
			t.Errorf("AppendUint(%d) = %q, want %q", v, got, want)
		}
		s := int64(v)
		if got, want := string(AppendInt(nil, s)), strconv.FormatInt(s, 10); got != want {
			t.Errorf("AppendInt(%d) = %q, want %q", s, got, want)
		}
	}
	// Every value below ten thousand, which covers the two short paths and
	// the shortest shifts of the first store.
	for v := range uint64(10000) {
		if got, want := string(AppendUint(nil, v)), strconv.FormatUint(v, 10); got != want {
			t.Fatalf("AppendUint(%d) = %q, want %q", v, got, want)
		}
	}
}

package odjsonrt

import (
	"encoding/binary"
	"math/rand/v2"
	"testing"
)

// htmlStopByte is the byte-at-a-time definition swarUnsafeHTML must agree
// with: what JSON syntax escapes, plus the three bytes encoding/json escapes
// for HTML. Non-ASCII bytes are not stops.
func htmlStopByte(b byte) bool {
	return b == '"' || b == '\\' || b < 0x20 || b == '<' || b == '>' || b == '&'
}

func firstHTMLStop(w uint64) int {
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], w)
	for i, c := range b {
		if htmlStopByte(c) {
			return i
		}
	}
	return 8
}

func checkHTMLStop(t *testing.T, w uint64) {
	t.Helper()
	want := firstHTMLStop(w)
	m := swarUnsafeHTML(w)
	switch {
	case want == 8 && m != 0:
		t.Fatalf("swarUnsafeHTML(%#016x) = %#016x, want 0", w, m)
	case want < 8 && m == 0:
		t.Fatalf("swarUnsafeHTML(%#016x) = 0, want a hit at lane %d", w, want)
	case want < 8 && swarIndex(m) != want:
		t.Fatalf("swarUnsafeHTML(%#016x): lowest lane %d, want %d", w, swarIndex(m), want)
	}
}

// TestSWARUnsafeHTMLExhaustive checks every pair of byte values in the two
// lowest lanes (which covers the borrow a hit in lane 0 sends into lane 1),
// every single value in every lane, and random words, against the byte
// definition: the mask is zero exactly when no lane stops, and otherwise its
// lowest set lane is the first stop byte.
func TestSWARUnsafeHTMLExhaustive(t *testing.T) {
	const fill = 0x6161616161616161
	for b0 := range 256 {
		for b1 := range 256 {
			checkHTMLStop(t, fill&^0xFFFF|uint64(b1)<<8|uint64(b0))
		}
	}
	for lane := range 8 {
		for b := range 256 {
			checkHTMLStop(t, fill&^(0xFF<<(8*lane))|uint64(b)<<(8*lane))
		}
	}
	r := rand.New(rand.NewPCG(3, 4))
	for range 1_000_000 {
		checkHTMLStop(t, r.Uint64())
	}
}

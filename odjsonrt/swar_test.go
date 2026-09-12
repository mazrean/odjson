package odjsonrt

import (
	"encoding/binary"
	"math/rand/v2"
	"testing"
)

// stopByte is the byte-at-a-time definition the word tests must agree with:
// a quote, a backslash or a control byte ends a run of ordinary characters,
// and a non-ASCII byte does not.
func stopByte(b byte) bool { return b == '"' || b == '\\' || b < 0x20 }

// firstStop returns the index of the first stop byte in w, or 8.
func firstStop(w uint64) int {
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], w)
	for i, c := range b {
		if stopByte(c) {
			return i
		}
	}
	return 8
}

// checkStop verifies the two properties every caller relies on: the mask is
// zero exactly when no lane stops, and when one does, the lowest set lane is
// the first stop byte (a borrow may set lanes above it, never below).
func checkStop(t *testing.T, name string, f func(uint64) uint64, w uint64) {
	t.Helper()
	want := firstStop(w)
	m := f(w)
	switch {
	case want == 8 && m != 0:
		t.Fatalf("%s(%#016x) = %#016x, want 0", name, w, m)
	case want < 8 && m == 0:
		t.Fatalf("%s(%#016x) = 0, want a hit at lane %d", name, w, want)
	case want < 8 && swarIndex(m) != want:
		t.Fatalf("%s(%#016x): lowest lane %d, want %d", name, w, swarIndex(m), want)
	}
}

// TestSWARStringStopExhaustive runs every pair of byte values through the two
// lowest lanes, with the lanes above them ordinary, then every single value
// in every lane, then random words. The pair covers the borrow from a hit in
// lane 0 into lane 1, which is where a fused test can go wrong.
func TestSWARStringStopExhaustive(t *testing.T) {
	fns := map[string]func(uint64) uint64{"swarStringStop": swarStringStop, "swarUnsafe": swarUnsafe}
	const fill = 0x6161616161616161 // 'a' in every lane
	for name, f := range fns {
		for b0 := range 256 {
			for b1 := range 256 {
				w := fill&^0xFFFF | uint64(b1)<<8 | uint64(b0)
				checkStop(t, name, f, w)
			}
		}
		for lane := range 8 {
			for b := range 256 {
				w := fill&^(0xFF<<(8*lane)) | uint64(b)<<(8*lane)
				checkStop(t, name, f, w)
			}
		}
		r := rand.New(rand.NewPCG(1, 2))
		for range 1_000_000 {
			checkStop(t, name, f, r.Uint64())
		}
	}
}

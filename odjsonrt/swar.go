package odjsonrt

import (
	"encoding/binary"
	"math/bits"
)

// Word-at-a-time scanning helpers.
//
// The decode and encode loops spend most of their time looking for the next
// byte that needs attention: the end of a string, a byte that must be escaped,
// the end of a run of indentation. Checking eight bytes per iteration instead
// of one is worth a large fraction of both directions on real documents.
//
// All of these use the classic SWAR idioms. They may report a hit one byte
// early when a borrow crosses a lane boundary, but never miss one, and every
// caller falls back to a byte loop on a hit, so an early hit is harmless.

// allSpaces is a word of eight ' ' bytes, the shape of JSON indentation.
const allSpaces = 0x2020202020202020

// swarHasZero reports which lanes of w are zero.
func swarHasZero(w uint64) uint64 { return (w - swarLo) &^ w & swarHi }

// swarHasByte reports which lanes of w equal b.
func swarHasByte(w uint64, b byte) uint64 { return swarHasZero(w ^ (swarLo * uint64(b))) }

// swarHasControl reports which lanes of w are below 0x20. Lanes >= 0x80 are
// never reported, which is what JSON scanning wants: they are continuation
// bytes, not control characters.
func swarHasControl(w uint64) uint64 { return (w - swarLo*0x20) &^ w & swarHi }

// swarStringStop reports which lanes of w end a JSON string literal's run of
// ordinary characters: a quote, a backslash or a control byte.
//
// The three tests are fused rather than composed from the helpers above: each
// helper masks with swarHi on its own, and folding that into a single mask at
// the end removes three ANDs from the hottest loop in the decoder.
func swarStringStop(w uint64) uint64 {
	q := w ^ (swarLo * '"')
	e := w ^ (swarLo * '\\')
	return ((w-swarLo*0x20)&^w | (q-swarLo)&^q | (e-swarLo)&^e) & swarHi
}

// swarIndex returns the byte offset of the lowest set lane of a mask produced
// by the helpers above, for a word loaded little-endian.
//
// A borrow can set a lane above a genuine hit but never below one, so the
// lowest set lane is always at or before the first byte of interest; landing
// early simply sends the caller through its byte path for one more character.
func swarIndex(mask uint64) int { return bits.TrailingZeros64(mask) / 8 }

// swarBelow masks off every lane at or above offset n.
func swarBelow(w uint64, n int) uint64 {
	if n >= 8 {
		return w
	}
	return w & (uint64(1)<<(8*uint(n)) - 1)
}

// ASCII reports whether b contains only bytes below 0x80.
//
// Generated decoders use it to decide whether a member name that missed the
// exact match can still fold to a field name of a different byte length: only
// a non-ASCII name can, since the two runes that fold across byte lengths
// (U+017F and U+212A) are multi-byte.
func ASCII(b []byte) bool {
	i := 0
	for ; i+8 <= len(b); i += 8 {
		if binary.LittleEndian.Uint64(b[i:])&swarHi != 0 {
			return false
		}
	}
	for ; i < len(b); i++ {
		if b[i] >= 0x80 {
			return false
		}
	}
	return true
}

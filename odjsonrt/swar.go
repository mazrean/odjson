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
// The control and quote tests share one subtraction: XORing a lane with 0x02
// maps '"' (0x22) onto 0x20 and leaves 0x00-0x1F inside 0x00-0x1F, so a lane
// is below 0x21 afterwards exactly when it held a control byte or a quote;
// 0x20 and 0x21 land on 0x22 and 0x23 and stay clear. The backslash test is
// the usual equality, and the two borrow masks (&^w) and the lane mask are
// applied once to the OR of both, since every constant involved is below
// 0x80. That is seven operations where the composed form costs eleven, in
// the hottest loop of the decoder and the encoder alike.
func swarStringStop(w uint64) uint64 {
	cq := (w ^ (swarLo * 0x02)) - swarLo*0x21
	e := (w ^ (swarLo * '\\')) - swarLo
	return (cq | e) &^ w & swarHi
}

// swarLatin reports whether every non-ASCII byte of w belongs to a complete
// two byte sequence with a lead of C2-DF that lies inside the word: the shape
// of Latin text with accents, and of Greek, Cyrillic, Hebrew and Arabic. A
// scan that would otherwise stop at each such letter and validate it on its
// own can then take the whole word. The word tests are exact: a lane's top
// bit in nz is set when the lane is nonzero, computed without a borrow
// crossing into the next lane.
func swarLatin(w uint64) bool {
	const lo7 = 0x7F7F7F7F7F7F7F7F
	hi := w & swarHi
	// Lanes of the form 110xxxxx, then lanes of the form 10xxxxxx.
	x := w&0xE0E0E0E0E0E0E0E0 ^ 0xC0C0C0C0C0C0C0C0
	lead := ^((x&lo7 + lo7) | x) & swarHi
	y := w&0xC0C0C0C0C0C0C0C0 ^ 0x8080808080808080
	cont := ^((y&lo7 + lo7) | y) & swarHi
	// Every high byte is a lead or a continuation, and the continuations
	// are exactly the lanes after the leads. A lead in the top lane would
	// shift out of the comparison, so it is refused on its own; a
	// continuation in the bottom lane has no lead before it and fails.
	if hi != lead|cont || cont != lead<<8 || lead>>63 != 0 {
		return false
	}
	// A lead below C2 (C0 or C1, the overlong forms) has bits 1-4 clear.
	z := w & 0x1E1E1E1E1E1E1E1E
	nz := ((z&lo7 + lo7) | z) & swarHi
	return nz&lead == lead
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

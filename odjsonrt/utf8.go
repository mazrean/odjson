package odjsonrt

import (
	"unicode/utf8"
)

// UTF-8 validation folded into the scans.
//
// encoding/json/v2 refuses invalid UTF-8 in both directions, and on the direct
// path (see direct.go) nobody but odjson looks at the bytes. Checking a string
// with utf8.Valid after scanning it for escapes is a second pass over every
// byte, and on a document full of non-ASCII text that second pass costs as
// much as the first. The scans therefore stop at the first non-ASCII byte and
// hand the run that starts there to skipNonASCII, which validates it in
// place: a validated multi-byte sequence never holds a byte that JSON escapes
// or that ends a literal, so the scan can pick up again at the first ASCII
// byte after it with nothing else to look at.

// skipNonASCII validates the run of non-ASCII bytes that starts at i, which
// must be at a byte >= 0x80, and returns the index of the first ASCII byte at
// or after it, or len(s). It returns -1 when the run is not valid UTF-8, under
// exactly utf8.Valid's rules: overlong forms, surrogates and code points above
// U+10FFFF are invalid, and so is a sequence the input ends in the middle of.
//
// Whole words are settled first, for the scripts a document is usually full
// of: two three byte sequences with leads E1-EC or EE-EF (CJK), four two byte
// sequences (Latin-1 supplement, Greek, Cyrillic, Hebrew, Arabic) or two four
// byte sequences with leads F0-F3 (emoji). Everything else, one sequence at a
// time, is decoded by the table below, which is utf8.Valid's: the second byte
// of E0, ED, F0 and F4 has a narrower range than 80-BF, and C0, C1 and F5-FF
// never lead anything.
func skipNonASCII(s []byte, i int) int {
	for uint(i) < uint(len(s)) {
		b := s[i]
		if b < utf8.RuneSelf {
			return i
		}
		if i+8 <= len(s) {
			w := load64(s, i)
			// Two three byte sequences. The mask keeps the top nibble of
			// each lead and the top two bits of each continuation byte;
			// the range test on the lead then excludes E0 and ED, whose
			// second byte is restricted.
			if w&0xC0C0F0C0C0F0 == 0x8080E08080E0 && cjkLead(b) && cjkLead(byte(w>>24)) {
				i += 6
				// A run of such text is long: the words after the
				// first are taken here, two sequences each, without
				// the tests above for the other scripts.
				for i+8 <= len(s) {
					w = load64(s, i)
					if w&0xC0C0F0C0C0F0 != 0x8080E08080E0 || !cjkLead(byte(w)) || !cjkLead(byte(w>>24)) {
						break
					}
					i += 6
				}
				continue
			}
			// Four two byte sequences: leads at the even bytes, which must
			// be C2-DF, continuations at the odd ones. A lead of the form
			// 110xxxxx is C0 or C1 exactly when its low five bits are 0 or
			// 1, so the test is that bits 1-4 are not all clear in any lane.
			if w&0xC0E0C0E0C0E0C0E0 == 0x80C080C080C080C0 {
				t := w & 0x001E001E001E001E
				if (t-0x0001000100010001)&^t&0x8000800080008000 == 0 {
					i += 8
					continue
				}
			}
			// Two four byte sequences with leads F0-F3. F0's second byte
			// must be 90-BF; F1-F3 accept any continuation byte.
			if w&0xC0C0C0FCC0C0C0FC == 0x808080F0808080F0 &&
				(b != 0xF0 || byte(w>>8) >= 0x90) && (byte(w>>32) != 0xF0 || byte(w>>40) >= 0x90) {
				i += 8
				continue
			}
			if w&0xC0C0F0 == 0x8080E0 && cjkLead(b) {
				i += 3
				continue
			}
		} else if i+4 <= len(s) {
			w := load32(s, i)
			if w&0xC0C0F0 == 0x8080E0 && cjkLead(b) {
				i += 3
				continue
			}
		}
		// One sequence, by its lead byte.
		n := len(s) - i
		switch {
		case b < 0xC2:
			// A stray continuation byte, or the overlong leads C0 and C1.
			return -1
		case b < 0xE0:
			if n < 2 || s[i+1]&0xC0 != 0x80 {
				return -1
			}
			i += 2
		case b < 0xF0:
			if n < 3 || s[i+2]&0xC0 != 0x80 {
				return -1
			}
			lo, hi := byte(0x80), byte(0xBF)
			switch b {
			case 0xE0:
				lo = 0xA0 // below is overlong
			case 0xED:
				hi = 0x9F // above is a surrogate
			}
			if c := s[i+1]; c < lo || c > hi {
				return -1
			}
			i += 3
		case b < 0xF5:
			if n < 4 || s[i+2]&0xC0 != 0x80 || s[i+3]&0xC0 != 0x80 {
				return -1
			}
			lo, hi := byte(0x80), byte(0xBF)
			switch b {
			case 0xF0:
				lo = 0x90 // below is overlong
			case 0xF4:
				hi = 0x8F // above is past U+10FFFF
			}
			if c := s[i+1]; c < lo || c > hi {
				return -1
			}
			i += 4
		default:
			return -1
		}
	}
	return i
}

// cjkLead reports whether b leads a three byte sequence that accepts every
// continuation byte as its second: E1-EC or EE-EF.
func cjkLead(b byte) bool { return b-0xE1 <= 0xEC-0xE1 || b|1 == 0xEF }

// cjkWord reports whether w holds two complete three byte sequences whose
// leads accept every continuation byte, which is what a word of CJK text
// looks like: the shape skipNonASCII's inner loop takes six bytes at a time.
//
// The lead range is tested without a branch. Both leads are known to be E0-EF
// once the mask compare passes, so what is left is that neither low nibble is
// 0 (E0, whose second byte is restricted) or D (ED, above which lies the
// surrogate range). A nibble plus fifteen carries into the bit above it
// exactly when the nibble is not zero, and the two lead nibbles sit far
// enough apart that neither carry reaches the other; XORing D in first turns
// the second test into the same one. The two results and the mask compare
// then fold into one comparison against zero, so a word of text costs no
// branch but the loop's own.
func cjkWord(w uint64) bool {
	const (
		nibbles = 0x0F00000F // the two leads' low nibbles
		carries = 0x10000010 // the bit above each of them
		notED   = 0x0D00000D
	)
	t := w & nibbles
	nonzero := (t + nibbles) & carries
	notD := ((t ^ notED) + nibbles) & carries
	return ((w&0xC0C0F0C0C0F0)^0x8080E08080E0)|((nonzero&notD)^carries) == 0
}

// cjkSeq is [cjkWord] for the one three byte sequence in the low three
// lanes of w: the upper lanes are replaced by a sequence that passes, so
// the same test judges the one that is left.
func cjkSeq(w uint64) bool { return cjkWord(w&0xFFFFFF | 0x8080E1<<24) }

// copyNonASCII is [skipNonASCII] with the copy folded in: it validates the
// run of non-ASCII bytes that starts at i and stores it into d at n, and
// returns the offset past what it wrote and the index of the first ASCII
// byte at or after the run, or -1 when the run is not valid UTF-8.
//
// The encoder needs both, and doing them in two passes — validate, then copy
// what was validated — reads a quarter of a document like twitter.json
// twice. A dense run of CJK text, which is what twitter.json's runs are, is
// taken six bytes a turn by the cjkWord loop, as skipNonASCII takes it.
// Where that loop stops — at the run's last sequence, or at the ASCII
// between the words of a document whose strings are Japanese, Korean or
// Thai from end to end — the word is taken whole by swarMixed, eight bytes
// at a fixed stride, the run's end and the next word's start in one store,
// and the next word goes back to the cjkWord loop if it is dense text again
// or on through swarMixed if it is not; the first word without a non-ASCII
// byte is the caller's, whose ASCII scan is cheaper. Anything the two tests
// refuse — a four byte sequence, an invalid byte, the last bytes of the
// string — goes to skipNonASCII, and what that validated is copied after
// it.
//
// d must have room for eight bytes past n, as [appendStringBodyV2]'s
// reservation guarantees: a word case stores the whole word it loaded even
// when the sequences in it are shorter, and the bytes past them are the ones
// the next store writes anyway.
func copyNonASCII(d []byte, n int, s []byte, i int) (int, int) {
	if i+8 <= len(s) {
		// carry is the lanes of the next word that the sequence the
		// previous word left unfinished still owes (see swarMixed), prev
		// that word; a word the cjkWord loop takes starts on a lead, so
		// it never runs while a sequence is owed.
		var carry, prev uint64
		for {
			// The two tests below need a three byte lead in the first
			// lane, so a word that starts on anything else — the ASCII
			// between two words of such text, or the continuation a
			// sequence still owed — skips them. The loop is skipNonASCII's
			// inner loop with the store added, and nothing else: a word
			// carried around the outer loop cost it four register moves
			// a turn, on the loop twitter.json's strings spend most of
			// their time in.
			if s[i]&0xF0 == 0xE0 {
				for i+8 <= len(s) {
					w := load64(s, i)
					if !cjkWord(w) {
						break
					}
					store64(d, n, w)
					n += 6
					i += 6
				}
				if i+8 > len(s) {
					goto rest
				}
				// One more sequence with the run's ASCII after it, which
				// is how a run ends half the time: the loop's own test,
				// with the upper lanes filled in, and the store is the
				// whole word.
				if w := load64(s, i); cjkSeq(w) {
					store64(d, n, w)
					n += 3
					i += 3
					if i+8 > len(s) {
						goto rest
					}
				}
			}
			w := load64(s, i)
			// Nothing but ASCII in the word means the run has ended, unless
			// a sequence is still owed, which the table below reports.
			if w&swarHi == 0 {
				if carry != 0 {
					break
				}
				return n, i
			}
			// The escape test first: a run that ends on a control byte, as
			// twitter.json's do at every line break, is settled by it alone.
			if swarUnsafe(w) != 0 {
				break
			}
			lead, lead3, bad := swarMixed(w, carry)
			if bad != 0 || swarMixedRange(w, lead3) != 0 {
				break
			}
			store64(d, n, w)
			n += 8
			i += 8
			carry = (lead3>>48)&0x8080 | (lead>>56)&0x80
			prev = w
			if i+8 > len(s) {
				break
			}
		}
		if carry != 0 {
			// Step back onto the lead of the sequence the last word left
			// unfinished, so that the table judges it whole: the last
			// byte when it is that lead, the one before when the last
			// byte is its first continuation. Both are stored already.
			back := 1
			if byte(prev>>56) < 0xC0 {
				back = 2
			}
			n -= back
			i -= back
		} else if uint(i) >= uint(len(s)) || s[i] < utf8.RuneSelf {
			// The run ended in the last word taken: the caller's scan
			// takes it from here.
			return n, i
		}
	}
rest:
	// The last bytes of the string, fewer than a word: the three byte
	// sequences among them are settled from four byte loads, which is how
	// most of twitter.json's runs end, and what is left is a run the
	// table settles: skipNonASCII takes all of it, and what it validated
	// is copied after it.
	for i+4 <= len(s) {
		w := load32(s, i)
		if !cjkSeq(uint64(w)) {
			break
		}
		store32(d, n, w)
		n += 3
		i += 3
	}
	if uint(i) >= uint(len(s)) {
		return n, i
	}
	j := skipNonASCII(s, i)
	if j < 0 {
		return n, -1
	}
	return copyRun(d, n, s, i, j), j
}

// swarMixed judges a word of text in which safe ASCII, two byte and three
// byte sequences may be mixed: it reports the lanes of w that hold a lead
// (11xxxxxx), the ones that hold a three byte lead (1110xxxx), and a word
// that is nonzero when the sequences are not laid out as such text — a four
// byte lead, a continuation with no lead before it or a lead without its
// continuations, or an overlong two byte lead (C0, C1). A sequence is
// allowed to straddle the word's end: a lead in the last lane, or a three
// byte lead in the last two, needs no continuation inside the word, and
// carry, the lanes of w that the previous word's unfinished sequence still
// owes (lane 0 and perhaps lane 1), says which of the first lanes have to
// be continuations. What it does not judge is the second byte of a three
// byte sequence and the escapes among the ASCII: those are
// [swarMixedRange]'s and [swarUnsafe]'s, so that each part stays inside
// the inliner's budget.
func swarMixed(w, carry uint64) (lead, lead3, bad uint64) {
	hi := w & swarHi
	// Bits 6, 5 and 4 of a non-ASCII byte, in turn: lead or continuation,
	// three bytes or more, four bytes or more.
	lead = (w << 1) & hi
	cont := hi ^ lead
	lead3 = (w << 2) & lead
	// A two byte lead below C2 has bits 1-4 clear: adding 0x7E to those
	// four bits reaches the lane's top bit exactly when they are not all
	// zero, and never carries out of the lane.
	nz := (w&0x1E1E1E1E1E1E1E1E + 0x7E7E7E7E7E7E7E7E) & swarHi
	// The continuations are exactly the lanes after the leads: one after
	// a two byte lead, two after a three byte one, the ones a shift drops
	// off the top owed to the next word and the ones the previous word
	// owed to this one added at the bottom.
	bad = (w<<3)&lead3 | (cont ^ (lead<<8 | lead3<<16 | carry)) | (lead^lead3)&^nz
	return lead, lead3, bad
}

// swarMixedRange is the rest of [swarMixed]'s judgement: nonzero when a
// three byte lead's second byte is out of its range — an E0 whose second
// byte is below A0 (overlong) or an ED whose second byte is above 9F (a
// surrogate), and an E0 or ED in the top lane, whose second byte is out of
// reach. The escapes among the ASCII are [swarUnsafe]'s to find.
//
// The lead's low nibble is tested as cjkWord tests it: a nibble plus
// fifteen carries into the bit above it exactly when it is not zero,
// XORing D in first turns that into a test for D, and bit 5 of the next
// lane's byte, brought down onto the same bit, is what tells the two
// halves of the second byte's range apart. E0 needs the high half and ED
// the low.
func swarMixedRange(w, lead3 uint64) uint64 {
	const (
		nibbles = 0x0F0F0F0F0F0F0F0F
		carries = 0x1010101010101010
		notED   = 0x0D0D0D0D0D0D0D0D
		top     = 0x10 << 56 // the carry bit of the last lane
	)
	nib := w & nibbles
	nonzero := (nib + nibbles) & carries
	notD := ((nib ^ notED) + nibbles) & carries
	high := (w >> 9) & carries
	ok := (nonzero | high) & (notD | (^high &^ top))
	return lead3 &^ (ok << 3)
}

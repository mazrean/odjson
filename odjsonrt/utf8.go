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

// copyNonASCII is [skipNonASCII] with the copy folded in: it validates the
// run of non-ASCII bytes that starts at i and stores it into d at n, and
// returns the offset past what it wrote and the index of the first ASCII
// byte at or after the run, or -1 when the run is not valid UTF-8.
//
// The encoder needs both, and doing them in two passes — validate, then copy
// what was validated — reads a quarter of a document like twitter.json
// twice. The word cases below, which are what a document full of one script
// is made of, store the word they judged; a run that needs the byte-at-a-time
// table hands the rest to skipNonASCII and copies what that validated, since
// those sequences are rare enough not to be worth a second copy of the table.
//
// d must have room for eight bytes past n, as [appendStringBodyV2]'s
// reservation guarantees: a word case stores the whole word it loaded even
// when the sequences in it are shorter, and the bytes past them are the ones
// the next store writes anyway.
func copyNonASCII(d []byte, n int, s []byte, i int) (int, int) {
	for uint(i) < uint(len(s)) {
		b := s[i]
		if b < utf8.RuneSelf {
			return n, i
		}
		if i+8 <= len(s) {
			w := load64(s, i)
			if cjkWord(w) {
				store64(d, n, w)
				n += 6
				i += 6
				for i+8 <= len(s) {
					w = load64(s, i)
					if !cjkWord(w) {
						break
					}
					store64(d, n, w)
					n += 6
					i += 6
				}
				continue
			}
			if w&0xC0E0C0E0C0E0C0E0 == 0x80C080C080C080C0 {
				t := w & 0x001E001E001E001E
				if (t-0x0001000100010001)&^t&0x8000800080008000 == 0 {
					store64(d, n, w)
					n += 8
					i += 8
					continue
				}
			}
			if w&0xC0C0C0FCC0C0C0FC == 0x808080F0808080F0 &&
				(b != 0xF0 || byte(w>>8) >= 0x90) && (byte(w>>32) != 0xF0 || byte(w>>40) >= 0x90) {
				store64(d, n, w)
				n += 8
				i += 8
				continue
			}
			if w&0xC0C0F0 == 0x8080E0 && cjkLead(b) {
				store64(d, n, w)
				n += 3
				i += 3
				continue
			}
		}
		// What is left is a sequence the table settles, and after it a
		// run that may be anything: skipNonASCII takes all of it.
		j := skipNonASCII(s, i)
		if j < 0 {
			return n, -1
		}
		return copyRun(d, n, s, i, j), j
	}
	return n, i
}

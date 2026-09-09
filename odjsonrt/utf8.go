package odjsonrt

import (
	"encoding/binary"
	"unicode/utf8"
)

// UTF-8 validation folded into the scans.
//
// encoding/json/v2 refuses invalid UTF-8 in both directions, and on the direct
// path (see direct.go) nobody but odjson looks at the bytes. Checking a string
// with utf8.Valid after scanning it for escapes is a second pass over every
// byte, and on a document full of CJK text that second pass costs as much as
// the first. The scans therefore stop at the first non-ASCII byte and hand the
// run that starts there to skipNonASCII, which validates it in place: a
// validated multi-byte sequence never holds a byte that JSON escapes or that
// ends a literal, so the scan can pick up again at the first ASCII byte after
// it with nothing else to look at.

// skipNonASCII validates the run of non-ASCII bytes that starts at i, which
// must be at a byte >= 0x80, and returns the index of the first ASCII byte at
// or after it, or len(s). It returns -1 when the run is not valid UTF-8, under
// exactly utf8.Valid's rules: overlong forms, surrogates and code points above
// U+10FFFF are invalid, and so is a sequence the input ends in the middle of.
//
// The one shape it settles on its own is a three byte sequence whose lead
// byte, E1-EC or EE-EF, accepts every continuation byte as its second: that is
// all of CJK, and the whole of most non-ASCII text. Everything else, the
// boundary leads E0 and ED, two and four byte sequences and the last bytes of
// s, is left to utf8.DecodeRune, so the rules there are the standard
// library's.
func skipNonASCII(s []byte, i int) int {
	for i < len(s) {
		b := s[i]
		if b < utf8.RuneSelf {
			return i
		}
		// One load covers the lead byte and both continuation bytes of a
		// sequence, or of two of them. The mask keeps the top nibble of
		// each lead and the top two bits of each continuation byte; the
		// range test on the lead then excludes E0 and ED, whose second byte
		// is restricted.
		if i+8 <= len(s) {
			w := binary.LittleEndian.Uint64(s[i:])
			if w&0xC0C0F0C0C0F0 == 0x8080E08080E0 && cjkLead(b) && cjkLead(byte(w>>24)) {
				i += 6
				continue
			}
			if w&0xC0C0F0 == 0x8080E0 && cjkLead(b) {
				i += 3
				continue
			}
		} else if i+4 <= len(s) {
			w := binary.LittleEndian.Uint32(s[i:])
			if w&0xC0C0F0 == 0x8080E0 && cjkLead(b) {
				i += 3
				continue
			}
		}
		r, size := utf8.DecodeRune(s[i:])
		if r == utf8.RuneError && size <= 1 {
			return -1
		}
		i += size
	}
	return i
}

// cjkLead reports whether b leads a three byte sequence that accepts every
// continuation byte as its second: E1-EC or EE-EF.
func cjkLead(b byte) bool { return b-0xE1 <= 0xEC-0xE1 || b|1 == 0xEF }

// validUTF8 reports whether s is valid UTF-8, through the same fast path
// the scans use. It exists for the callers that hold a string body already
// scanned by the legacy scanner and only need the verdict.
func validUTF8(s []byte) bool {
	i := 0
	for i < len(s) {
		if s[i] < utf8.RuneSelf {
			i++
			for i+8 <= len(s) && binary.LittleEndian.Uint64(s[i:])&swarHi == 0 {
				i += 8
			}
			continue
		}
		if i = skipNonASCII(s, i); i < 0 {
			return false
		}
	}
	return true
}

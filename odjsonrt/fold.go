package odjsonrt

import (
	"unicode"
	"unicode/utf8"
)

// EqualFold reports whether key and name are equal under encoding/json's
// case-insensitive field matching rules: ASCII case folding plus Unicode
// simple folding. It is equivalent to bytes.EqualFold but takes the field
// name as a string so that generated code needs no conversion.
func EqualFold(key []byte, name string) bool {
	for len(key) > 0 && len(name) > 0 {
		var kr, nr rune
		if c := key[0]; c < utf8.RuneSelf {
			kr, key = rune(c), key[1:]
		} else {
			r, size := utf8.DecodeRune(key)
			kr, key = r, key[size:]
		}
		if c := name[0]; c < utf8.RuneSelf {
			nr, name = rune(c), name[1:]
		} else {
			r, size := utf8.DecodeRuneInString(name)
			nr, name = r, name[size:]
		}

		if kr == nr {
			continue
		}
		// Make kr the smaller of the two runes.
		if nr < kr {
			kr, nr = nr, kr
		}
		if nr < utf8.RuneSelf {
			// ASCII only, sr/tr must be upper/lower case.
			if 'A' <= kr && kr <= 'Z' && nr == kr+'a'-'A' {
				continue
			}
			return false
		}
		// General case. SimpleFold(x) returns the next equivalent rune > x
		// or wraps around to the smallest one.
		r := unicode.SimpleFold(kr)
		for r != kr && r < nr {
			r = unicode.SimpleFold(r)
		}
		if r == nr {
			continue
		}
		return false
	}
	return len(key) == 0 && len(name) == 0
}

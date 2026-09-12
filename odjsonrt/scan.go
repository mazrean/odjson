package odjsonrt

import (
	"math/bits"
)

// inlineDepth is the number of nesting levels the scanner can track without
// touching the heap.
const inlineDepth = 128

// SkipSpace returns the index of the first byte at or after p that is not
// JSON whitespace.
func SkipSpace(data []byte, p int) int {
	// Every JSON whitespace byte is <= ' ' and every byte that can start a
	// value is greater, so one comparison settles the common case. Keeping
	// this body tiny matters: it is called between every token, and it stops
	// being inlined the moment it grows.
	if uint(p) < uint(len(data)) && data[p] > ' ' {
		return p
	}
	return skipSpaceSlow(data, p)
}

// skipSpaceSlow consumes an actual run of whitespace.
func skipSpaceSlow(data []byte, p int) int {
	// An indented document is mostly a newline followed by a run of
	// spaces. Two words cover a run of up to sixteen, and the length is
	// computed rather than branched on: the lowest lane that differs from
	// a space is the first non-space byte, exactly, a word of nothing but
	// spaces reports eight, and the second word counts only when the first
	// was all spaces. Run lengths vary from line to line, so a loop that
	// tests each word mispredicts on most of them; this does not. The one
	// space after a colon, once the commonest run, no longer arrives here:
	// AfterName settles it in every caller.
	if uint(p+17) <= uint(len(data)) && data[p] == '\n' {
		n0 := bits.TrailingZeros64(load64(data, p+1)^allSpaces) / 8
		n1 := bits.TrailingZeros64(load64(data, p+9)^allSpaces) / 8
		p += 1 + n0 + n1&-(n0>>3)
		// Whatever the run ended on, one compare says whether it starts
		// a token, which it nearly always does; the table lookup below
		// is for the rest.
		if uint(p) < uint(len(data)) && data[p] > ' ' {
			return p
		}
	}
	for uint(p) < uint(len(data)) && spaceSet[data[p]] {
		p++
		for p+8 <= len(data) {
			n := bits.TrailingZeros64(load64(data, p)^allSpaces) / 8
			p += n
			if n < 8 {
				break
			}
		}
	}
	return p
}

// spaceSet marks the four bytes JSON accepts as whitespace.
var spaceSet = func() (t [256]bool) {
	t[' '], t['\t'], t['\n'], t['\r'] = true, true, true, true
	return
}()

// SkipValue scans the single JSON value that starts at p and returns the index
// just past it. Leading whitespace must already have been consumed. The scan
// is iterative and rejects documents nested deeper than [MaxDepth].
func SkipValue(data []byte, p int) (int, error) {
	var inline [inlineDepth]byte
	stack := inline[:0]

	for {
		if uint(p) >= uint(len(data)) {
			return p, errUnexpectedEnd(p)
		}
		switch c := data[p]; c {
		case '{':
			if len(stack) >= MaxDepth {
				return p, ErrSyntax(data, p, "exceeded max depth")
			}
			stack = append(stack, '}')
			p = SkipSpace(data, p+1)
			if uint(p) < uint(len(data)) && data[p] == '}' {
				p++
				stack = stack[:len(stack)-1]
				break
			}
			var err error
			if p, err = scanKey(data, p); err != nil {
				return p, err
			}
			continue
		case '[':
			if len(stack) >= MaxDepth {
				return p, ErrSyntax(data, p, "exceeded max depth")
			}
			stack = append(stack, ']')
			p = SkipSpace(data, p+1)
			if uint(p) < uint(len(data)) && data[p] == ']' {
				p++
				stack = stack[:len(stack)-1]
				break
			}
			continue
		case '"':
			end, _, _, err := scanString(data, p)
			if err != nil {
				return end, err
			}
			p = end
		case 't':
			if !isTrue(data, p) {
				return p, errBeginValue(data, p)
			}
			p += 4
		case 'f':
			if !isFalse(data, p) {
				return p, errBeginValue(data, p)
			}
			p += 5
		case 'n':
			if !isNull(data, p) {
				return p, errBeginValue(data, p)
			}
			p += 4
		default:
			if c != '-' && (c < '0' || c > '9') {
				return p, errBeginValue(data, p)
			}
			end, err := scanNumber(data, p)
			if err != nil {
				return end, err
			}
			p = end
		}

		// A value has been consumed; close as many containers as possible.
		for {
			if len(stack) == 0 {
				return p, nil
			}
			p = SkipSpace(data, p)
			if uint(p) >= uint(len(data)) {
				return p, errUnexpectedEnd(p)
			}
			closer := stack[len(stack)-1]
			switch data[p] {
			case ',':
				p = SkipSpace(data, p+1)
				if closer == '}' {
					var err error
					if p, err = scanKey(data, p); err != nil {
						return p, err
					}
				}
			case closer:
				p++
				stack = stack[:len(stack)-1]
				continue
			default:
				if closer == '}' {
					return p, errChar(data, p, "after object key:value pair")
				}
				return p, errChar(data, p, "after array element")
			}
			break
		}
	}
}

// scanKey scans an object member name followed by its colon and returns the
// index of the first byte of the member value.
func scanKey(data []byte, p int) (int, error) {
	if uint(p) >= uint(len(data)) {
		return p, errUnexpectedEnd(p)
	}
	if data[p] != '"' {
		return p, errChar(data, p, "looking for beginning of object key string")
	}
	end, _, _, err := scanString(data, p)
	if err != nil {
		return end, err
	}
	if next := AfterName(data, end); next > 0 {
		return next, nil
	}
	return afterKeySlow(data, end)
}

// scanString scans the JSON string literal that starts at p (which must hold a
// double quote) and returns the index just past the closing quote. hasEscape
// reports whether the literal contains a backslash escape and nonASCII whether
// it contains any byte >= 0x80.
func scanString(data []byte, p int) (end int, hasEscape, nonASCII bool, err error) {
	i := p + 1
	for uint(i) < uint(len(data)) {
		// Consume runs of ordinary characters a word at a time. The mask also
		// locates the byte that ended the run, so a short string costs one
		// word test instead of a byte loop. The high bits of the consumed
		// lanes are accumulated rather than branched on: they only say, once
		// the run is over, whether it contained non-ASCII.
		var hi uint64
		for i+8 <= len(data) {
			w := load64(data, i)
			if m := swarStringStop(w); m != 0 {
				k := swarIndex(m)
				hi |= swarBelow(w, k)
				i += k
				break
			}
			hi |= w
			i += 8
		}
		nonASCII = nonASCII || hi&swarHi != 0
		if uint(i) >= uint(len(data)) {
			break
		}
		switch c := data[i]; {
		case c == '"':
			return i + 1, hasEscape, nonASCII, nil
		case c == '\\':
			hasEscape = true
			i++
			if uint(i) >= uint(len(data)) {
				return i, hasEscape, nonASCII, errUnexpectedEnd(i)
			}
			switch data[i] {
			case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
				i++
			case 'u':
				if uint(i+4) >= uint(len(data)) {
					return len(data), hasEscape, nonASCII, errUnexpectedEnd(len(data))
				}
				for k := 1; k <= 4; k++ {
					if !isHex(data[i+k]) {
						return i + k, hasEscape, nonASCII,
							errChar(data, i+k, "in \\u hexadecimal character escape")
					}
				}
				i += 5
			default:
				return i, hasEscape, nonASCII, errChar(data, i, "in string escape code")
			}
		case c < 0x20:
			return i, hasEscape, nonASCII, errChar(data, i, "in string literal")
		default:
			if c >= 0x80 {
				nonASCII = true
			}
			i++
		}
	}
	return i, hasEscape, nonASCII, errUnexpectedEnd(i)
}

// scanNumber scans the JSON number literal that starts at p and returns the
// index just past it.
func scanNumber(data []byte, p int) (int, error) {
	end, ok := scanNumberIn(data, p)
	if !ok {
		if end >= len(data) {
			return end, errUnexpectedEnd(end)
		}
		return end, errChar(data, end, "in numeric literal")
	}
	return end, nil
}

// scanNumberIn validates the RFC 8259 number grammar (an optional minus sign,
// an integer part without leading zeros, an optional fraction and an optional
// exponent) and reports the index just past the literal. When ok is false the
// returned index points at the offending byte, or at len(data) if the literal
// is truncated.
func scanNumberIn[Bytes []byte | string](data Bytes, p int) (end int, ok bool) {
	i := p
	if uint(i) < uint(len(data)) && data[i] == '-' {
		i++
	}
	// Integer part.
	switch {
	case i >= len(data):
		return len(data), false
	case data[i] == '0':
		i++
	case data[i] >= '1' && data[i] <= '9':
		i++
		for uint(i) < uint(len(data)) && data[i] >= '0' && data[i] <= '9' {
			i++
		}
	default:
		return i, false
	}
	// Fraction.
	if uint(i) < uint(len(data)) && data[i] == '.' {
		i++
		if uint(i) >= uint(len(data)) {
			return len(data), false
		}
		if data[i] < '0' || data[i] > '9' {
			return i, false
		}
		for uint(i) < uint(len(data)) && data[i] >= '0' && data[i] <= '9' {
			i++
		}
	}
	// Exponent.
	if uint(i) < uint(len(data)) && (data[i] == 'e' || data[i] == 'E') {
		i++
		if uint(i) < uint(len(data)) && (data[i] == '+' || data[i] == '-') {
			i++
		}
		if uint(i) >= uint(len(data)) {
			return len(data), false
		}
		if data[i] < '0' || data[i] > '9' {
			return i, false
		}
		for uint(i) < uint(len(data)) && data[i] >= '0' && data[i] <= '9' {
			i++
		}
	}
	return i, true
}

// EndOfDocument reports an error unless only whitespace remains at or after p.
func EndOfDocument(data []byte, p int) error {
	p = SkipSpace(data, p)
	if p != len(data) {
		return errChar(data, p, "after top-level value")
	}
	return nil
}

// Validate reports whether data holds exactly one JSON value, optionally
// surrounded by whitespace.
func Validate(data []byte) error {
	p := SkipSpace(data, 0)
	p, err := SkipValue(data, p)
	if err != nil {
		return err
	}
	return EndOfDocument(data, p)
}

// isTrue, isFalse and isNull report whether the literal stands at p. Each
// is one length test and one word compare once inlined: the compiler
// expands a compare against a short constant into loads, where a loop over
// the literal's bytes, however short, ran a byte at a time.
func isTrue(data []byte, p int) bool  { return p+4 <= len(data) && string(data[p:p+4]) == "true" }
func isFalse(data []byte, p int) bool { return p+5 <= len(data) && string(data[p:p+5]) == "false" }
func isNull(data []byte, p int) bool  { return p+4 <= len(data) && string(data[p:p+4]) == "null" }

// isHex reports whether c is an ASCII hexadecimal digit.
func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

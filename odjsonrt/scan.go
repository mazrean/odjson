package odjsonrt

import (
	"encoding/binary"
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
	if p < len(data) && data[p] > ' ' {
		return p
	}
	return skipSpaceSlow(data, p)
}

// skipSpaceSlow consumes an actual run of whitespace.
func skipSpaceSlow(data []byte, p int) int {
	// The one space after a colon is the commonest run by far, and it is
	// settled by the next byte: p+1 is a constant offset, not a value
	// computed from the data, so nothing waits on the word scan below.
	if p+1 < len(data) && data[p] == ' ' && data[p+1] > ' ' {
		return p + 1
	}
	// An indented document is mostly a newline followed by a run of
	// spaces. Two words cover a run of up to sixteen, and the length is
	// computed rather than branched on: the lowest lane that differs from
	// a space is the first non-space byte, exactly, a word of nothing but
	// spaces reports eight, and the second word counts only when the first
	// was all spaces. Run lengths vary from line to line, so a loop that
	// tests each word mispredicts on most of them; this does not. Whatever
	// the run ends on, the loop below takes over, and it is a run of
	// nothing but spaces when it finds a non-space byte right away.
	if p+17 <= len(data) && data[p] == '\n' {
		n0 := bits.TrailingZeros64(binary.LittleEndian.Uint64(data[p+1:])^allSpaces) / 8
		n1 := bits.TrailingZeros64(binary.LittleEndian.Uint64(data[p+9:])^allSpaces) / 8
		p += 1 + n0 + n1&-(n0>>3)
	}
	for p < len(data) && spaceSet[data[p]] {
		p++
		for p+8 <= len(data) {
			n := bits.TrailingZeros64(binary.LittleEndian.Uint64(data[p:])^allSpaces) / 8
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
		if p >= len(data) {
			return p, errUnexpectedEnd(p)
		}
		switch c := data[p]; c {
		case '{':
			if len(stack) >= MaxDepth {
				return p, ErrSyntax(data, p, "exceeded max depth")
			}
			stack = append(stack, '}')
			p = SkipSpace(data, p+1)
			if p < len(data) && data[p] == '}' {
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
			if p < len(data) && data[p] == ']' {
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
			if !hasLiteral(data, p, "true") {
				return p, errBeginValue(data, p)
			}
			p += 4
		case 'f':
			if !hasLiteral(data, p, "false") {
				return p, errBeginValue(data, p)
			}
			p += 5
		case 'n':
			if !hasLiteral(data, p, "null") {
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
			if p >= len(data) {
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
	if p >= len(data) {
		return p, errUnexpectedEnd(p)
	}
	if data[p] != '"' {
		return p, errChar(data, p, "looking for beginning of object key string")
	}
	end, _, _, err := scanString(data, p)
	if err != nil {
		return end, err
	}
	p = SkipSpace(data, end)
	if p >= len(data) {
		return p, errUnexpectedEnd(p)
	}
	if data[p] != ':' {
		return p, errChar(data, p, "after object key")
	}
	return SkipSpace(data, p+1), nil
}

// scanString scans the JSON string literal that starts at p (which must hold a
// double quote) and returns the index just past the closing quote. hasEscape
// reports whether the literal contains a backslash escape and nonASCII whether
// it contains any byte >= 0x80.
func scanString(data []byte, p int) (end int, hasEscape, nonASCII bool, err error) {
	i := p + 1
	for i < len(data) {
		// Consume runs of ordinary characters a word at a time. The mask also
		// locates the byte that ended the run, so a short string costs one
		// word test instead of a byte loop. The high bits of the consumed
		// lanes are accumulated rather than branched on: they only say, once
		// the run is over, whether it contained non-ASCII.
		var hi uint64
		for i+8 <= len(data) {
			w := binary.LittleEndian.Uint64(data[i:])
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
		if i >= len(data) {
			break
		}
		switch c := data[i]; {
		case c == '"':
			return i + 1, hasEscape, nonASCII, nil
		case c == '\\':
			hasEscape = true
			i++
			if i >= len(data) {
				return i, hasEscape, nonASCII, errUnexpectedEnd(i)
			}
			switch data[i] {
			case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
				i++
			case 'u':
				if i+4 >= len(data) {
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
	if i < len(data) && data[i] == '-' {
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
		for i < len(data) && data[i] >= '0' && data[i] <= '9' {
			i++
		}
	default:
		return i, false
	}
	// Fraction.
	if i < len(data) && data[i] == '.' {
		i++
		if i >= len(data) {
			return len(data), false
		}
		if data[i] < '0' || data[i] > '9' {
			return i, false
		}
		for i < len(data) && data[i] >= '0' && data[i] <= '9' {
			i++
		}
	}
	// Exponent.
	if i < len(data) && (data[i] == 'e' || data[i] == 'E') {
		i++
		if i < len(data) && (data[i] == '+' || data[i] == '-') {
			i++
		}
		if i >= len(data) {
			return len(data), false
		}
		if data[i] < '0' || data[i] > '9' {
			return i, false
		}
		for i < len(data) && data[i] >= '0' && data[i] <= '9' {
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

// hasLiteral reports whether data continues with lit at p.
func hasLiteral(data []byte, p int, lit string) bool {
	if len(data)-p < len(lit) {
		return false
	}
	for i := 0; i < len(lit); i++ {
		if data[p+i] != lit[i] {
			return false
		}
	}
	return true
}

// isHex reports whether c is an ASCII hexadecimal digit.
func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

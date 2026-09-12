package odjsonrt

import (
	"bytes"
	"encoding"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"unicode/utf16"
	"unicode/utf8"
)

// Strict parsing.
//
// The generated decoders behind UnmarshalJSONFrom parse bytes under
// encoding/json/v2's rules, and on the direct path (see direct.go) nobody has
// looked at those bytes before them. json/v2 rejects two things encoding/json
// tolerates: invalid UTF-8, which encoding/json replaces with U+FFFD, and a
// duplicate object member name, of which encoding/json keeps the last. The
// helpers here are the byte oriented parsers with those two rules added; the
// generated struct decoders enforce the duplicate rule for their own members
// with a bitset and hand everything they do not decode to [SkipValueStrict].

// errInvalidUTF8 is the error json/v2 reports for a string that is not UTF-8.
func errInvalidUTF8(data []byte, p int) error {
	return ErrSyntax(data, p, "invalid UTF-8 within string")
}

// ErrDuplicateName reports that an object holds name twice, which json/v2
// rejects and encoding/json does not.
func ErrDuplicateName(data []byte, p int, name []byte) error {
	return ErrSyntax(data, p, "duplicate object member name "+quoteName(name))
}

func quoteName(name []byte) string {
	b := appendQuoted(make([]byte, 0, len(name)+2), name, false, true)
	return string(b)
}

// parseStringBytesStrict is [ParseStringBytes] under json/v2's rules.
func parseStringBytesStrict(data []byte, p int) (s []byte, aliased bool, next int, err error) {
	if p >= len(data) {
		return nil, false, p, errUnexpectedEnd(p)
	}
	if data[p] != '"' {
		return nil, false, p, ErrType(data, p, "string")
	}
	end, hasEscape, _, err := scanStringStrict(data, p)
	if err != nil {
		return nil, false, end, err
	}
	body := data[p+1 : end-1]
	if !hasEscape {
		return body, true, end, nil
	}
	out, ok := unquote(body, true)
	if !ok {
		return nil, false, p, ErrSyntax(data, p, "invalid string literal")
	}
	return out, false, end, nil
}

// skipStringStrict scans the string literal at p under json/v2's rules
// without producing it: the body must be UTF-8 and every \u escape must
// decode, which for a surrogate means being one half of a pair. It is what
// [SkipValueStrict] uses, so a skipped string with escapes is checked in
// place instead of being unescaped into a buffer nobody reads.
func skipStringStrict(data []byte, p int) (int, error) {
	end, hasEscape, _, err := scanStringStrict(data, p)
	if err != nil {
		return end, err
	}
	if hasEscape && !validEscapes(data[p+1:end-1]) {
		return p, ErrSyntax(data, p, "invalid string literal")
	}
	return end, nil
}

// scanStringStrict is [scanString] under json/v2's rules: the literal's
// non-ASCII bytes are validated as UTF-8 in the same pass, by skipNonASCII,
// instead of by a second pass over the body. err is [errInvalidUTF8], at p,
// for a body that is not UTF-8.
func scanStringStrict(data []byte, p int) (end int, hasEscape, nonASCII bool, err error) {
	i := p + 1
	for i < len(data) {
		// The run of ordinary ASCII is consumed a word at a time; the mask
		// also stops at the first non-ASCII byte, which starts a run for
		// skipNonASCII.
		for i+8 <= len(data) {
			w := binary.LittleEndian.Uint64(data[i:])
			if m := swarStringStop(w) | w&swarHi; m != 0 {
				i += swarIndex(m)
				break
			}
			i += 8
		}
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
		case c >= utf8.RuneSelf:
			nonASCII = true
			// A two byte sequence is settled here without the call, and
			// the words after it are taken whole while they are accented
			// Latin text (see swarLatin), which would otherwise stop the
			// scan at every letter.
			if c-0xC2 < 0x1E && i+1 < len(data) && data[i+1]&0xC0 == 0x80 {
				i += 2
				if i < len(data) && data[i] >= utf8.RuneSelf {
					// A dense run (Cyrillic, Greek): skipNonASCII takes
					// it a word at a time; the ordinary path below
					// reports what it refuses.
				} else {
					for i+8 <= len(data) {
						w := binary.LittleEndian.Uint64(data[i:])
						if w&swarHi == 0 || swarStringStop(w) != 0 || !swarLatin(w) {
							break
						}
						i += 8
					}
					continue
				}
			}
			if next := skipNonASCII(data, i); next >= 0 {
				i = next
			} else if bytes.IndexByte(data[i:], '"') < 0 {
				// The sequence is cut off by the end of the input, not
				// malformed: no closing quote follows it.
				return len(data), hasEscape, nonASCII, errUnexpectedEnd(len(data))
			} else {
				return p, hasEscape, nonASCII, errInvalidUTF8(data, p)
			}
		default:
			i++
		}
	}
	return i, hasEscape, nonASCII, errUnexpectedEnd(i)
}

// validEscapes reports whether every \u escape in a string body that
// scanString has accepted decodes under json/v2's rules. Only surrogates can
// fail: a high surrogate must be followed by a low one, and a low one may not
// stand alone. The other escapes were settled by scanString.
func validEscapes(s []byte) bool {
	for {
		n := bytes.IndexByte(s, '\\')
		if n < 0 {
			return true
		}
		s = s[n:]
		if len(s) < 2 {
			return false
		}
		if s[1] != 'u' {
			s = s[2:]
			continue
		}
		r := getu4(s)
		if r < 0 {
			return false
		}
		s = s[6:]
		if utf16.IsSurrogate(r) {
			if utf16.DecodeRune(r, getu4(s)) == utf8.RuneError {
				return false
			}
			s = s[6:]
		}
	}
}

// ParseStringStrict is [ParseString] under json/v2's rules: invalid UTF-8 and
// an unpaired surrogate escape are errors rather than U+FFFD. The result is
// interned through c when it is not nil.
func ParseStringStrict(data []byte, p int, c *StringCache) (string, int, error) {
	if p >= len(data) {
		return "", p, errUnexpectedEnd(p)
	}
	if data[p] != '"' {
		return "", p, ErrType(data, p, "string")
	}
	end, hasEscape, nonASCII, err := scanStringStrict(data, p)
	if err != nil {
		return "", end, err
	}
	body := data[p+1 : end-1]
	if !hasEscape {
		if !nonASCII {
			return c.Make(body), end, nil
		}
		// The scan has validated the body, so the entry can be marked
		// for the callers that would otherwise check it again.
		return c.MakeValid(body), end, nil
	}
	out, ok := unquote(body, true)
	if !ok {
		return "", p, ErrSyntax(data, p, "invalid string literal")
	}
	return adoptString(out, false), end, nil
}

// ParseStringInnerStrict is [ParseStringInner] under json/v2's rules.
func ParseStringInnerStrict(data []byte, p int) ([]byte, int, error) {
	b, _, next, err := parseStringBytesStrict(data, p)
	return b, next, err
}

// ParseKeyStrict is [ParseKey] under json/v2's rules.
func ParseKeyStrict(data []byte, p int) (key []byte, next int, err error) {
	if p >= len(data) {
		return nil, p, errUnexpectedEnd(p)
	}
	if data[p] != '"' {
		return nil, p, errChar(data, p, "looking for beginning of object key string")
	}
	key, _, next, err = parseStringBytesStrict(data, p)
	if err != nil {
		return nil, next, err
	}
	next = SkipSpace(data, next)
	if next >= len(data) {
		return nil, next, errUnexpectedEnd(next)
	}
	if data[next] != ':' {
		return nil, next, errChar(data, next, "after object key")
	}
	return key, SkipSpace(data, next+1), nil
}

// ParseBase64Strict is [ParseBase64] under json/v2's rules.
func ParseBase64Strict(data []byte, p int) ([]byte, int, error) {
	s, _, next, err := parseStringBytesStrict(data, p)
	if err != nil {
		return nil, next, err
	}
	out := make([]byte, base64.StdEncoding.DecodedLen(len(s)))
	n, derr := base64.StdEncoding.Decode(out, s)
	if derr != nil {
		return nil, p, derr
	}
	return out[:n], next, nil
}

// ParseNumberStringStrict is [ParseNumberString] under json/v2's rules.
func ParseNumberStringStrict(data []byte, p int) (string, int, error) {
	if p < len(data) && data[p] == '"' {
		s, _, next, err := parseStringBytesStrict(data, p)
		if err != nil {
			return "", next, err
		}
		if end, ok := scanNumberIn(s, 0); !ok || end != len(s) {
			return "", p, &TypeError{Value: "string", Type: "json.Number", Offset: int64(p)}
		}
		return string(s), next, nil
	}
	return ParseNumberString(data, p)
}

// ParseTextUnmarshalerStrict is [ParseTextUnmarshaler] under json/v2's rules.
func ParseTextUnmarshalerStrict(data []byte, p int, u encoding.TextUnmarshaler) (int, error) {
	if p >= len(data) {
		return p, errUnexpectedEnd(p)
	}
	if data[p] != '"' {
		return p, ErrType(data, p, "encoding.TextUnmarshaler")
	}
	s, _, next, err := parseStringBytesStrict(data, p)
	if err != nil {
		return next, err
	}
	if err := u.UnmarshalText(s); err != nil {
		return next, err
	}
	return next, nil
}

// ParseRawStrict is [ParseRaw] under json/v2's rules: the value is checked
// for invalid UTF-8 and duplicate names before it is returned.
func ParseRawStrict(data []byte, p int) (raw []byte, next int, err error) {
	end, err := SkipValueStrict(data, p)
	if err != nil {
		return nil, end, err
	}
	return data[p:end], end, nil
}

// ParseUnmarshalerStrict is [ParseUnmarshaler] under json/v2's rules.
func ParseUnmarshalerStrict(data []byte, p int, u json.Unmarshaler) (int, error) {
	raw, next, err := ParseRawStrict(data, p)
	if err != nil {
		return next, err
	}
	if err := u.UnmarshalJSON(raw); err != nil {
		return next, err
	}
	return next, nil
}

// ParseIntoV2 is [ParseInto] with encoding/json/v2 as the fallback decoder.
func ParseIntoV2(data []byte, p int, v any, strict bool) (int, error) {
	raw, next, err := ParseRawV2(data, p, strict)
	if err != nil {
		return next, err
	}
	if err := jsonv2.Unmarshal(raw, v); err != nil {
		return next, err
	}
	return next, nil
}

// ParseAnyStrict is [ParseAny] under json/v2's rules, interning strings
// through c when it is not nil.
func ParseAnyStrict(data []byte, p int, c *StringCache) (any, int, error) {
	return parseAny(data, p, c, true, false)
}

// strictLevel is what [SkipValueStrict] keeps per open object: where that
// object's names start in the shared list, and a 256 bit filter over the
// names it has seen so far. A name whose filter bit is clear is known to be
// new, so only a hit pays for the scan; with a few dozen names per object
// that turns the k²/2 comparisons of a plain scan into a handful.
type strictLevel struct {
	mark   int
	filter [4]uint64
}

// nameHash is a cheap hash of a member name for [strictLevel]'s filter. The
// first and last words and the length are enough to keep sibling names that
// share a long prefix (profile_sidebar_fill_color, profile_sidebar_border_color)
// apart.
func nameHash(b []byte) uint64 {
	var h uint64
	switch n := len(b); {
	case n >= 8:
		h = binary.LittleEndian.Uint64(b) ^ binary.LittleEndian.Uint64(b[n-8:])*0x9e3779b97f4a7c15
	case n >= 4:
		h = uint64(binary.LittleEndian.Uint32(b)) ^ uint64(binary.LittleEndian.Uint32(b[n-4:]))*0x9e3779b97f4a7c15
	case n >= 2:
		h = uint64(binary.LittleEndian.Uint16(b)) ^ uint64(binary.LittleEndian.Uint16(b[n-2:]))*0x9e3779b97f4a7c15
	case n == 1:
		h = uint64(b[0]) * 0x9e3779b97f4a7c15
	}
	h ^= uint64(len(b)) * 0xff51afd7ed558ccd
	return h ^ h>>29
}

// strictKey parses the member name at p and appends it to names, unless the
// current object, whose names start at names[lv.mark:], already holds it.
func strictKey(data []byte, p int, names [][]byte, lv *strictLevel) ([][]byte, int, error) {
	// ParseKeyStrict's work, without its layers: the name is scanned in
	// place, and only an escaped one is decoded.
	if p >= len(data) {
		return names, p, errUnexpectedEnd(p)
	}
	if data[p] != '"' {
		return names, p, errChar(data, p, "looking for beginning of object key string")
	}
	end, hasEscape, _, err := scanStringStrict(data, p)
	if err != nil {
		return names, end, err
	}
	name := data[p+1 : end-1]
	if hasEscape {
		out, ok := unquote(name, true)
		if !ok {
			return names, p, ErrSyntax(data, p, "invalid string literal")
		}
		name = out
	}
	next := SkipSpace(data, end)
	if next >= len(data) {
		return names, next, errUnexpectedEnd(next)
	}
	if data[next] != ':' {
		return names, next, errChar(data, next, "after object key")
	}
	next = SkipSpace(data, next+1)
	h := nameHash(name)
	w, bit := h>>62, uint64(1)<<(h>>56&63)
	if lv.filter[w]&bit != 0 {
		for _, n := range names[lv.mark:] {
			if string(n) == string(name) {
				return names, p, ErrDuplicateName(data, p, name)
			}
		}
	}
	lv.filter[w] |= bit
	return append(names, name), next, nil
}

// SkipValueStrict is [SkipValue] under json/v2's rules: strings must be valid
// UTF-8 and no object may hold a name twice, at any depth.
//
// The names of every open object are kept back to back in one list, with a
// [strictLevel] per non-empty object saying where that object's names start
// and which of them might already be present. Both live in small arrays on
// the stack, so skipping a scalar or a small object allocates nothing; only a
// wide or deep object spills them to the heap.
func SkipValueStrict(data []byte, p int) (int, error) {
	var inline [inlineDepth]byte
	stack := inline[:0]
	var namesBuf [64][]byte
	var levelsBuf [16]strictLevel
	names := namesBuf[:0]
	levels := levelsBuf[:0]

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
			levels = append(levels, strictLevel{mark: len(names)})
			var err error
			if names, p, err = strictKey(data, p, names, &levels[len(levels)-1]); err != nil {
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
			end, err := skipStringStrict(data, p)
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
					if names, p, err = strictKey(data, p, names, &levels[len(levels)-1]); err != nil {
						return p, err
					}
				}
			case closer:
				p++
				stack = stack[:len(stack)-1]
				if closer == '}' {
					names, levels = names[:levels[len(levels)-1].mark], levels[:len(levels)-1]
				}
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

// The V2 helpers are what the generated odjsonParseV2 calls. Its strict
// argument says whether the bytes still need json/v2's checks: true on the
// direct path, where nothing has looked at them, and false when they came out
// of a jsontext.Decoder, which has already applied whatever the caller asked
// for, including AllowDuplicateNames and AllowInvalidUTF8. Checking again in
// that case would refuse what the caller explicitly allowed.

// ParseStringV2 is [ParseStringStrict] under strict and [ParseStringWith]
// otherwise. It is one body rather than a call to either, so that a string
// on the direct path costs the generated decoder a single call.
func ParseStringV2(data []byte, p int, c *StringCache, strict bool) (string, int, error) {
	if strict {
		return ParseStringStrict(data, p, c)
	}
	return ParseStringWith(data, p, c)
}

// ParseStringInnerV2 is [ParseStringInnerStrict] under strict and
// [ParseStringInner] otherwise.
func ParseStringInnerV2(data []byte, p int, strict bool) ([]byte, int, error) {
	if strict {
		return ParseStringInnerStrict(data, p)
	}
	return ParseStringInner(data, p)
}

// ParseKeyV2 is [ParseKeyStrict] under strict and [ParseKey] otherwise.
func ParseKeyV2(data []byte, p int, strict bool) (key []byte, next int, err error) {
	if strict {
		return ParseKeyStrict(data, p)
	}
	key, _, next, err = ParseKey(data, p)
	return key, next, err
}

// ParseBase64V2 is [ParseBase64Strict] under strict and [ParseBase64]
// otherwise.
func ParseBase64V2(data []byte, p int, strict bool) ([]byte, int, error) {
	if strict {
		return ParseBase64Strict(data, p)
	}
	return ParseBase64(data, p)
}

// ParseNumberStringV2 is [ParseNumberStringStrict] under strict and
// [ParseNumberString] otherwise.
func ParseNumberStringV2(data []byte, p int, strict bool) (string, int, error) {
	if strict {
		return ParseNumberStringStrict(data, p)
	}
	return ParseNumberString(data, p)
}

// ParseTextUnmarshalerV2 is [ParseTextUnmarshalerStrict] under strict and
// [ParseTextUnmarshaler] otherwise.
func ParseTextUnmarshalerV2(data []byte, p int, u encoding.TextUnmarshaler, strict bool) (int, error) {
	if strict {
		return ParseTextUnmarshalerStrict(data, p, u)
	}
	return ParseTextUnmarshaler(data, p, u)
}

// ParseRawV2 is [ParseRawStrict] under strict and [ParseRaw] otherwise.
func ParseRawV2(data []byte, p int, strict bool) (raw []byte, next int, err error) {
	if strict {
		return ParseRawStrict(data, p)
	}
	return ParseRaw(data, p)
}

// ParseUnmarshalerV2 is [ParseUnmarshalerStrict] under strict and
// [ParseUnmarshaler] otherwise.
func ParseUnmarshalerV2(data []byte, p int, u json.Unmarshaler, strict bool) (int, error) {
	if strict {
		return ParseUnmarshalerStrict(data, p, u)
	}
	return ParseUnmarshaler(data, p, u)
}

// ParseAnyV2 is [ParseAnyStrict] under strict and [ParseAnyWith] otherwise.
func ParseAnyV2(data []byte, p int, c *StringCache, strict bool) (any, int, error) {
	return parseAny(data, p, c, strict, false)
}

// SkipValueV2 is [SkipValueStrict] under strict and [SkipValue] otherwise.
func SkipValueV2(data []byte, p int, strict bool) (int, error) {
	if strict {
		return SkipValueStrict(data, p)
	}
	return SkipValue(data, p)
}

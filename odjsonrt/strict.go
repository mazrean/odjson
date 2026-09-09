package odjsonrt

import (
	"encoding"
	"encoding/base64"
	"encoding/json"
	jsonv2 "encoding/json/v2"
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
	b := appendQuoted(make([]byte, 0, len(name)+2), name, false)
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
	end, hasEscape, nonASCII, err := scanString(data, p)
	if err != nil {
		return nil, false, end, err
	}
	body := data[p+1 : end-1]
	if nonASCII && !utf8.Valid(body) {
		return nil, false, p, errInvalidUTF8(data, p)
	}
	if !hasEscape {
		return body, true, end, nil
	}
	out, ok := unquote(body, true)
	if !ok {
		return nil, false, p, ErrSyntax(data, p, "invalid string literal")
	}
	return out, false, end, nil
}

// ParseStringStrict is [ParseString] under json/v2's rules: invalid UTF-8 and
// an unpaired surrogate escape are errors rather than U+FFFD. The result is
// interned through c when it is not nil.
func ParseStringStrict(data []byte, p int, c *StringCache) (string, int, error) {
	b, aliased, next, err := parseStringBytesStrict(data, p)
	if err != nil {
		return "", next, err
	}
	if aliased {
		return c.Make(b), next, nil
	}
	return adoptString(b, false), next, nil
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
	return parseAny(data, p, c, parseStrict)
}

// strictKey parses the member name at p and appends it to names, unless the
// current object, whose names start at names[mark:], already holds it.
func strictKey(data []byte, p int, names [][]byte, mark int) ([][]byte, int, error) {
	name, next, err := ParseKeyStrict(data, p)
	if err != nil {
		return names, next, err
	}
	for _, n := range names[mark:] {
		if string(n) == string(name) {
			return names, p, ErrDuplicateName(data, p, name)
		}
	}
	return append(names, name), next, nil
}

// SkipValueStrict is [SkipValue] under json/v2's rules: strings must be valid
// UTF-8 and no object may hold a name twice, at any depth.
//
// The names of every open object are kept back to back in one list, with a
// mark per level saying where that level's names start. Both live in small
// arrays on the stack, so skipping a scalar or a small object allocates
// nothing; only a wide or deep object spills them to the heap.
func SkipValueStrict(data []byte, p int) (int, error) {
	var inline [inlineDepth]byte
	stack := inline[:0]
	var namesBuf [64][]byte
	var marksBuf [16]int
	names := namesBuf[:0]
	marks := marksBuf[:0]

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
			marks = append(marks, len(names))
			p = SkipSpace(data, p+1)
			if p < len(data) && data[p] == '}' {
				p++
				stack = stack[:len(stack)-1]
				names, marks = names[:marks[len(marks)-1]], marks[:len(marks)-1]
				break
			}
			var err error
			if names, p, err = strictKey(data, p, names, marks[len(marks)-1]); err != nil {
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
			_, _, end, err := parseStringBytesStrict(data, p)
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
					if names, p, err = strictKey(data, p, names, marks[len(marks)-1]); err != nil {
						return p, err
					}
				}
			case closer:
				p++
				stack = stack[:len(stack)-1]
				if closer == '}' {
					names, marks = names[:marks[len(marks)-1]], marks[:len(marks)-1]
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
// otherwise.
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
	if strict {
		return parseAny(data, p, c, parseStrict)
	}
	return parseAny(data, p, c, parseTrusted)
}

// SkipValueV2 is [SkipValueStrict] under strict and [SkipValue] otherwise.
func SkipValueV2(data []byte, p int, strict bool) (int, error) {
	if strict {
		return SkipValueStrict(data, p)
	}
	return SkipValue(data, p)
}

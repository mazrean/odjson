package odjsonrt

import (
	"encoding"
	"encoding/base64"
	"encoding/json"
	"strconv"
	"unicode/utf16"
	"unicode/utf8"
	"unsafe"
)

// ParseString parses the JSON string that starts at p and returns its
// unescaped content as a Go string.
func ParseString(data []byte, p int) (string, int, error) {
	b, aliased, next, err := ParseStringBytes(data, p)
	if err != nil {
		return "", next, err
	}
	return adoptString(b, aliased), next, nil
}

// adoptString turns the result of [ParseStringBytes] into a string. When the
// bytes are not a view into the input they were allocated for this call alone
// and nothing else refers to them, so the string can take them over instead of
// copying a buffer that was just built.
func adoptString(b []byte, aliased bool) string {
	if aliased {
		return string(b)
	}
	if len(b) == 0 {
		return ""
	}
	return unsafe.String(unsafe.SliceData(b), len(b))
}

// ParseStringBytes parses the JSON string that starts at p and returns its
// unescaped content. When the literal contains neither escapes nor invalid
// UTF-8 the result is a sub-slice of data and aliased is true; the caller must
// copy it before retaining it. Otherwise a fresh slice is returned.
//
// Escape handling matches encoding/json: unpaired surrogates and invalid
// UTF-8 bytes decode to U+FFFD.
func ParseStringBytes(data []byte, p int) (s []byte, aliased bool, next int, err error) {
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
	if !hasEscape && (!nonASCII || utf8.Valid(body)) {
		return body, true, end, nil
	}
	out, ok := unquote(body)
	if !ok {
		return nil, false, p, ErrSyntax(data, p, "invalid string literal")
	}
	return out, false, end, nil
}

// ParseStringInner parses the JSON string that starts at p and returns its
// unescaped content so that the caller can re-parse it as JSON. It implements
// the `,string` struct tag option. The returned slice may alias data and must
// not be retained.
func ParseStringInner(data []byte, p int) (inner []byte, next int, err error) {
	b, _, next, err := ParseStringBytes(data, p)
	return b, next, err
}

// ParseKey parses an object member name and the ':' that follows it. The
// returned index points at the first byte of the member value, with leading
// whitespace already consumed. As with [ParseStringBytes], aliased reports
// whether key is a sub-slice of data.
func ParseKey(data []byte, p int) (key []byte, aliased bool, next int, err error) {
	if p >= len(data) {
		return nil, false, p, errUnexpectedEnd(p)
	}
	if data[p] != '"' {
		return nil, false, p, errChar(data, p, "looking for beginning of object key string")
	}
	key, aliased, next, err = ParseStringBytes(data, p)
	if err != nil {
		return nil, false, next, err
	}
	next = SkipSpace(data, next)
	if next >= len(data) {
		return nil, false, next, errUnexpectedEnd(next)
	}
	if data[next] != ':' {
		return nil, false, next, errChar(data, next, "after object key")
	}
	return key, aliased, SkipSpace(data, next+1), nil
}

// ParseInt parses the JSON number at p into a signed integer of the given bit
// size (8, 16, 32 or 64). A literal with a fraction or exponent, or one that
// does not fit, produces a [TypeError], matching encoding/json.
func ParseInt(data []byte, p int, bits int) (int64, int, error) {
	end, err := numberLiteral(data, p, intTypeName(bits))
	if err != nil {
		return 0, p, err
	}
	v, cerr := strconv.ParseInt(asString(data[p:end]), 10, intBits(bits))
	if cerr != nil {
		return 0, p, &TypeError{Value: "number", Type: intTypeName(bits), Offset: int64(p)}
	}
	return v, end, nil
}

// ParseUint parses the JSON number at p into an unsigned integer of the given
// bit size (8, 16, 32 or 64). A negative, fractional or out of range literal
// produces a [TypeError].
func ParseUint(data []byte, p int, bits int) (uint64, int, error) {
	end, err := numberLiteral(data, p, uintTypeName(bits))
	if err != nil {
		return 0, p, err
	}
	v, cerr := strconv.ParseUint(asString(data[p:end]), 10, intBits(bits))
	if cerr != nil {
		return 0, p, &TypeError{Value: "number", Type: uintTypeName(bits), Offset: int64(p)}
	}
	return v, end, nil
}

// ParseFloat parses the JSON number at p into a float of the given bit size
// (32 or 64). Literals outside the representable range produce a [TypeError],
// matching encoding/json.
func ParseFloat(data []byte, p int, bits int) (float64, int, error) {
	end, err := numberLiteral(data, p, floatTypeName(bits))
	if err != nil {
		return 0, p, err
	}
	v, cerr := strconv.ParseFloat(asString(data[p:end]), floatBits(bits))
	if cerr != nil {
		return 0, p, &TypeError{Value: "number", Type: floatTypeName(bits), Offset: int64(p)}
	}
	return v, end, nil
}

// ParseNumberString parses the value at p into a json.Number. A JSON number is
// taken verbatim; a JSON string is unescaped and accepted when its content is
// itself a valid JSON number, which is what encoding/json does for json.Number
// fields. Any other value, or a string that is not a number, is a [TypeError].
func ParseNumberString(data []byte, p int) (string, int, error) {
	if p >= len(data) {
		return "", p, errUnexpectedEnd(p)
	}
	if data[p] == '"' {
		s, _, next, err := ParseStringBytes(data, p)
		if err != nil {
			return "", next, err
		}
		if end, ok := scanNumberIn(s, 0); !ok || end != len(s) {
			return "", p, &TypeError{Value: "string", Type: "json.Number", Offset: int64(p)}
		}
		return string(s), next, nil
	}
	end, err := numberLiteral(data, p, "json.Number")
	if err != nil {
		return "", p, err
	}
	return string(data[p:end]), end, nil
}

// numberLiteral validates that a JSON number starts at p and returns the index
// just past it. A value of another kind produces a [TypeError] naming goType.
func numberLiteral(data []byte, p int, goType string) (int, error) {
	if p >= len(data) {
		return p, errUnexpectedEnd(p)
	}
	if c := data[p]; c != '-' && (c < '0' || c > '9') {
		return p, ErrType(data, p, goType)
	}
	return scanNumber(data, p)
}

// ParseBool parses a JSON boolean at p.
func ParseBool(data []byte, p int) (bool, int, error) {
	if p >= len(data) {
		return false, p, errUnexpectedEnd(p)
	}
	switch data[p] {
	case 't':
		if !hasLiteral(data, p, "true") {
			return false, p, errBeginValue(data, p)
		}
		return true, p + 4, nil
	case 'f':
		if !hasLiteral(data, p, "false") {
			return false, p, errBeginValue(data, p)
		}
		return false, p + 5, nil
	}
	return false, p, ErrType(data, p, "bool")
}

// ParseNull consumes a null literal at p. When the value at p is not null, ok
// is false and next equals p.
func ParseNull(data []byte, p int) (next int, ok bool) {
	// The leading byte settles it for every value that is not null, which is
	// almost all of them; keeping that test in the caller's inlined body is
	// worth several percent of a decode.
	if p < len(data) && data[p] == 'n' && hasLiteral(data, p, "null") {
		return p + 4, true
	}
	return p, false
}

// ParseRaw returns the sub-slice of data spanning exactly the JSON value that
// starts at p. The result aliases data.
func ParseRaw(data []byte, p int) (raw []byte, next int, err error) {
	end, err := SkipValue(data, p)
	if err != nil {
		return nil, end, err
	}
	return data[p:end], end, nil
}

// ParseBase64 parses a JSON string at p and decodes its standard base64
// content, matching how encoding/json unmarshals into []byte.
func ParseBase64(data []byte, p int) ([]byte, int, error) {
	s, _, next, err := ParseStringBytes(data, p)
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

// ParseUnmarshaler hands the raw bytes of the value at p to u.UnmarshalJSON.
// As in encoding/json the slice aliases data and must not be retained by u.
func ParseUnmarshaler(data []byte, p int, u json.Unmarshaler) (int, error) {
	raw, next, err := ParseRaw(data, p)
	if err != nil {
		return next, err
	}
	if err := u.UnmarshalJSON(raw); err != nil {
		return next, err
	}
	return next, nil
}

// ParseTextUnmarshaler parses a JSON string at p and hands its unescaped
// content to u.UnmarshalText. The slice may alias data and must not be
// retained by u.
func ParseTextUnmarshaler(data []byte, p int, u encoding.TextUnmarshaler) (int, error) {
	if p >= len(data) {
		return p, errUnexpectedEnd(p)
	}
	if data[p] != '"' {
		return p, ErrType(data, p, "encoding.TextUnmarshaler")
	}
	s, _, next, err := ParseStringBytes(data, p)
	if err != nil {
		return next, err
	}
	if err := u.UnmarshalText(s); err != nil {
		return next, err
	}
	return next, nil
}

// ParseInto decodes the value at p into v using encoding/json. It is the
// reflection based fallback used by generated code for types it cannot decode
// directly.
func ParseInto(data []byte, p int, v any) (int, error) {
	raw, next, err := ParseRaw(data, p)
	if err != nil {
		return next, err
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return next, err
	}
	return next, nil
}

// anyFrame is one level of the container stack used by [ParseAny]. A frame
// with a non-nil obj describes an object, otherwise it describes an array.
type anyFrame struct {
	arr []any
	obj map[string]any
	key string
}

// ParseAny decodes the value at p the way encoding/json decodes into an
// interface{}: objects become map[string]any, arrays []any, numbers float64,
// strings string, booleans bool and null nil. The decoder is iterative and
// rejects documents nested deeper than [MaxDepth].
func ParseAny(data []byte, p int) (any, int, error) {
	var stack []anyFrame
	var v any

	for {
		if p >= len(data) {
			return nil, p, errUnexpectedEnd(p)
		}
		switch c := data[p]; c {
		case '{':
			if len(stack) >= MaxDepth {
				return nil, p, ErrSyntax(data, p, "exceeded max depth")
			}
			obj := make(map[string]any)
			p = SkipSpace(data, p+1)
			if p < len(data) && data[p] == '}' {
				p++
				v = obj
				break
			}
			key, next, err := parseKeyString(data, p)
			if err != nil {
				return nil, next, err
			}
			stack = append(stack, anyFrame{obj: obj, key: key})
			p = next
			continue
		case '[':
			if len(stack) >= MaxDepth {
				return nil, p, ErrSyntax(data, p, "exceeded max depth")
			}
			p = SkipSpace(data, p+1)
			if p < len(data) && data[p] == ']' {
				p++
				v = []any{}
				break
			}
			stack = append(stack, anyFrame{arr: []any{}})
			continue
		case '"':
			s, next, err := ParseString(data, p)
			if err != nil {
				return nil, next, err
			}
			v, p = s, next
		case 't':
			if !hasLiteral(data, p, "true") {
				return nil, p, errBeginValue(data, p)
			}
			v, p = true, p+4
		case 'f':
			if !hasLiteral(data, p, "false") {
				return nil, p, errBeginValue(data, p)
			}
			v, p = false, p+5
		case 'n':
			if !hasLiteral(data, p, "null") {
				return nil, p, errBeginValue(data, p)
			}
			v, p = nil, p+4
		default:
			if c != '-' && (c < '0' || c > '9') {
				return nil, p, errBeginValue(data, p)
			}
			f, next, err := ParseFloat(data, p, 64)
			if err != nil {
				return nil, next, err
			}
			v, p = f, next
		}

		// A value has been decoded; attach it and close finished containers.
		for {
			if len(stack) == 0 {
				return v, p, nil
			}
			f := &stack[len(stack)-1]
			if f.obj != nil {
				f.obj[f.key] = v
			} else {
				f.arr = append(f.arr, v)
			}
			p = SkipSpace(data, p)
			if p >= len(data) {
				return nil, p, errUnexpectedEnd(p)
			}
			isObj := f.obj != nil
			switch {
			case data[p] == ',':
				p = SkipSpace(data, p+1)
				if isObj {
					key, next, err := parseKeyString(data, p)
					if err != nil {
						return nil, next, err
					}
					f.key = key
					p = next
				}
			case isObj && data[p] == '}':
				v = f.obj
				p++
				stack = stack[:len(stack)-1]
				continue
			case !isObj && data[p] == ']':
				v = f.arr
				p++
				stack = stack[:len(stack)-1]
				continue
			case isObj:
				return nil, p, errChar(data, p, "after object key:value pair")
			default:
				return nil, p, errChar(data, p, "after array element")
			}
			break
		}
	}
}

// parseKeyString is [ParseKey] returning the member name as a Go string.
func parseKeyString(data []byte, p int) (string, int, error) {
	key, _, next, err := ParseKey(data, p)
	if err != nil {
		return "", next, err
	}
	return string(key), next, nil
}

// unquote decodes the body of a JSON string literal (the bytes between the
// quotes) that has already been validated by scanString. It mirrors
// encoding/json's unquoteBytes: escapes are expanded, unpaired surrogates and
// invalid UTF-8 become U+FFFD.
func unquote(s []byte) ([]byte, bool) {
	b := make([]byte, len(s)+2*utf8.UTFMax)
	w := 0
	r := 0
	for r < len(s) {
		if w >= len(b)-2*utf8.UTFMax {
			grown := make([]byte, (len(b)+utf8.UTFMax)*2)
			copy(grown, b[:w])
			b = grown
		}
		switch c := s[r]; {
		case c == '\\':
			r++
			if r >= len(s) {
				return nil, false
			}
			switch s[r] {
			case '"', '\\', '/':
				b[w] = s[r]
				r++
				w++
			case 'b':
				b[w] = '\b'
				r++
				w++
			case 'f':
				b[w] = '\f'
				r++
				w++
			case 'n':
				b[w] = '\n'
				r++
				w++
			case 'r':
				b[w] = '\r'
				r++
				w++
			case 't':
				b[w] = '\t'
				r++
				w++
			case 'u':
				r--
				rr := getu4(s[r:])
				if rr < 0 {
					return nil, false
				}
				r += 6
				if utf16.IsSurrogate(rr) {
					rr1 := getu4(s[r:])
					if dec := utf16.DecodeRune(rr, rr1); dec != utf8.RuneError {
						// A valid surrogate pair; consume the second escape.
						r += 6
						w += utf8.EncodeRune(b[w:], dec)
						break
					}
					rr = utf8.RuneError
				}
				w += utf8.EncodeRune(b[w:], rr)
			default:
				return nil, false
			}
		case c == '"', c < ' ':
			return nil, false
		case c < utf8.RuneSelf:
			b[w] = c
			r++
			w++
		default:
			rr, size := utf8.DecodeRune(s[r:])
			r += size
			w += utf8.EncodeRune(b[w:], rr)
		}
	}
	return b[:w], true
}

// getu4 decodes the 4 hexadecimal digits of a \uXXXX escape at the start of s,
// or returns -1 if s does not begin with a well formed escape.
func getu4(s []byte) rune {
	if len(s) < 6 || s[0] != '\\' || s[1] != 'u' {
		return -1
	}
	var r rune
	for _, c := range s[2:6] {
		switch {
		case c >= '0' && c <= '9':
			c = c - '0'
		case c >= 'a' && c <= 'f':
			c = c - 'a' + 10
		case c >= 'A' && c <= 'F':
			c = c - 'A' + 10
		default:
			return -1
		}
		r = r*16 + rune(c)
	}
	return r
}

// asString views b as a string without copying it. The result must not outlive
// b and b must not be modified while it is in use.
func asString(b []byte) string {
	return unsafe.String(unsafe.SliceData(b), len(b))
}

// intBits normalises a signed/unsigned bit size for strconv.
func intBits(bits int) int {
	switch bits {
	case 8, 16, 32:
		return bits
	default:
		return 64
	}
}

// floatBits normalises a float bit size for strconv.
func floatBits(bits int) int {
	if bits == 32 {
		return 32
	}
	return 64
}

// intTypeName names the Go type reported in errors for a signed integer.
func intTypeName(bits int) string {
	switch bits {
	case 8:
		return "int8"
	case 16:
		return "int16"
	case 32:
		return "int32"
	default:
		return "int64"
	}
}

// uintTypeName names the Go type reported in errors for an unsigned integer.
func uintTypeName(bits int) string {
	switch bits {
	case 8:
		return "uint8"
	case 16:
		return "uint16"
	case 32:
		return "uint32"
	default:
		return "uint64"
	}
}

// floatTypeName names the Go type reported in errors for a float.
func floatTypeName(bits int) string {
	if bits == 32 {
		return "float32"
	}
	return "float64"
}

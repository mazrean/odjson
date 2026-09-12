package odjsonrt

import (
	"bytes"
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

// ParseStringCached is [ParseString] with a string cache: a literal that
// needed no unescaping is interned through c, which may be nil.
func ParseStringCached(data []byte, p int, c *StringCache) (string, int, error) {
	if p >= len(data) {
		return "", p, errUnexpectedEnd(p)
	}
	if data[p] != '"' {
		return "", p, ErrType(data, p, "string")
	}
	end, hasEscape, nonASCII, err := scanString(data, p)
	if err != nil {
		return "", end, err
	}
	body := data[p+1 : end-1]
	if !hasEscape {
		if !nonASCII {
			return c.Make(body), end, nil
		}
		// Invalid UTF-8 becomes U+FFFD below, which is unquote's job.
		if s, ok := c.MakeUTF8(body); ok {
			return s, end, nil
		}
	}
	out, ok := unquote(body, false)
	if !ok {
		return "", p, ErrSyntax(data, p, "invalid string literal")
	}
	return adoptString(out, false), end, nil
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
	out, ok := unquote(body, false)
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
	key, aliased, end, err := ParseStringBytes(data, p)
	if err != nil {
		return nil, false, end, err
	}
	if next = AfterName(data, end); next == 0 {
		if next, err = afterKeySlow(data, end); err != nil {
			return nil, false, next, err
		}
	}
	return key, aliased, next, nil
}

// AfterKey consumes the colon that follows an object member name ending
// just before p, with any whitespace around it, and returns the index of the
// member value's first byte. Generated decoders call it after matching a
// name against the document's raw bytes; the errors are [ParseKey]'s.
func AfterKey(data []byte, p int) (int, error) {
	if p < len(data) && data[p] == ':' {
		return SkipSpace(data, p+1), nil
	}
	return afterKeySlow(data, p)
}

// AfterName is [AfterKey] for the two shapes nearly every document has:
// the colon followed by the value, or by one space and then the value. It
// returns the index of the value's first byte, or 0 when the bytes are
// anything else, which [AfterKey] then settles; a value never starts at 0
// after a name. It has no call in it, so it is inlined into the generated
// decoder, where the one space of an indented document used to reach
// skipSpaceSlow on every member.
func AfterName(data []byte, p int) int {
	// Two bytes past the colon exist in any document that is not cut off,
	// since a comma or a bracket follows the value.
	if p+2 < len(data) && data[p] == ':' {
		if data[p+1] > ' ' {
			return p + 1
		}
		if data[p+1] == ' ' && data[p+2] > ' ' {
			return p + 2
		}
	}
	return 0
}

func afterKeySlow(data []byte, p int) (int, error) {
	p = SkipSpace(data, p)
	if p >= len(data) {
		return p, errUnexpectedEnd(p)
	}
	if data[p] != ':' {
		return p, errChar(data, p, "after object key")
	}
	return SkipSpace(data, p+1), nil
}

// ParseInt parses the JSON number at p into a signed integer of the given bit
// size (8, 16, 32 or 64). A literal with a fraction or exponent, or one that
// does not fit, produces a [TypeError], matching encoding/json.
func ParseInt(data []byte, p int, bits int) (int64, int, error) {
	// bits is a constant at every generated call site, so once this is
	// inlined the range test folds away for int64 and the slow path is the
	// only call left.
	if v, end, ok := ParseDecimal(data, p); ok && (bits == 64 || (v >= -1<<(bits-1) && v <= 1<<(bits-1)-1)) {
		return v, end, nil
	}
	return parseIntSlow(data, p, bits)
}

// parseIntSlow is [ParseInt] for the literals ParseDecimal declines and the
// ones that do not fit: strconv settles both and produces the error.
func parseIntSlow(data []byte, p int, bits int) (int64, int, error) {
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

// ParseDecimal reads the integer literal at p in one pass, accumulating the
// digits while it scans for the end of the number. It handles the common
// shape, an optional minus sign and up to eighteen digits with nothing after
// them, which can neither overflow nor need strconv's range checks. Anything
// else, including a fraction, an exponent or a leading zero followed by more
// digits, is left to the general path, which also produces the right error.
func ParseDecimal(data []byte, p int) (v int64, end int, ok bool) {
	i := p
	neg := i < len(data) && data[i] == '-'
	if neg {
		i++
	}
	start := i
	var u uint64
	for i < len(data) {
		c := data[i] - '0'
		if c > 9 {
			break
		}
		u = u*10 + uint64(c)
		i++
	}
	n := i - start
	if n == 0 || n > 18 || (data[start] == '0' && n > 1) {
		return 0, p, false
	}
	if i < len(data) && (data[i] == '.' || data[i] == 'e' || data[i] == 'E') {
		return 0, p, false
	}
	if neg {
		return -int64(u), i, true
	}
	return int64(u), i, true
}

// ParseUint parses the JSON number at p into an unsigned integer of the given
// bit size (8, 16, 32 or 64). A negative, fractional or out of range literal
// produces a [TypeError].
func ParseUint(data []byte, p int, bits int) (uint64, int, error) {
	// A minus sign is rejected by strconv even before a zero, so "-0" has to
	// take the general path to produce that error.
	if v, end, ok := ParseDecimal(data, p); ok && data[p] != '-' && (bits == 64 || v <= 1<<bits-1) {
		return uint64(v), end, nil
	}
	return parseUintSlow(data, p, bits)
}

// parseUintSlow is [ParseUint] for the literals ParseDecimal declines and the
// ones that do not fit.
func parseUintSlow(data []byte, p int, bits int) (uint64, int, error) {
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
	if v, end, ok := ParseSimpleFloat(data, p, bits); ok {
		return v, end, nil
	}
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

// pow10 holds the powers of ten a float64 represents exactly.
var pow10 = [...]float64{
	1e0, 1e1, 1e2, 1e3, 1e4, 1e5, 1e6, 1e7, 1e8, 1e9, 1e10, 1e11,
	1e12, 1e13, 1e14, 1e15, 1e16, 1e17, 1e18, 1e19, 1e20, 1e21, 1e22,
}

// pow10f32 holds the powers of ten a float32 represents exactly.
var pow10f32 = [...]float32{1e0, 1e1, 1e2, 1e3, 1e4, 1e5, 1e6, 1e7, 1e8, 1e9, 1e10}

// ParseSimpleFloat reads the number at p in one pass when it has the shape
// almost every real document uses, an optional sign, digits and an optional
// fraction with no exponent, and its digits fit the mantissa exactly. Then
// the value is one correctly rounded division of two exact floats (Clinger's
// fast path, the same one strconv takes after its own scan), so the result
// is bit for bit strconv.ParseFloat's. Everything else, including every
// malformed literal, is declined and left to the general path.
func ParseSimpleFloat(data []byte, p int, bits int) (float64, int, bool) {
	i := p
	neg := i < len(data) && data[i] == '-'
	if neg {
		i++
	}
	start := i
	var m uint64
	for i < len(data) {
		c := data[i] - '0'
		if c > 9 {
			break
		}
		m = m*10 + uint64(c)
		i++
	}
	digits := i - start
	if digits == 0 || (data[start] == '0' && digits > 1) {
		return 0, p, false
	}
	frac := 0
	if i < len(data) && data[i] == '.' {
		i++
		fs := i
		for i < len(data) {
			c := data[i] - '0'
			if c > 9 {
				break
			}
			m = m*10 + uint64(c)
			i++
		}
		frac = i - fs
		if frac == 0 {
			return 0, p, false
		}
		digits += frac
	}
	if digits > 19 || i < len(data) && (data[i] == 'e' || data[i] == 'E') {
		return 0, p, false
	}
	var f float64
	if bits == 32 {
		if m >= 1<<24 || frac >= len(pow10f32) {
			return 0, p, false
		}
		f = float64(float32(m) / pow10f32[frac])
	} else {
		if m >= 1<<53 || frac >= len(pow10) {
			return 0, p, false
		}
		f = float64(m) / pow10[frac]
	}
	if neg {
		f = -f
	}
	return f, i, true
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
	// Each literal is one word compare once this is inlined; only the
	// errors need a call.
	if p+4 <= len(data) && string(data[p:p+4]) == "true" {
		return true, p + 4, nil
	}
	if p+5 <= len(data) && string(data[p:p+5]) == "false" {
		return false, p + 5, nil
	}
	return parseBoolSlow(data, p)
}

func parseBoolSlow(data []byte, p int) (bool, int, error) {
	if p >= len(data) {
		return false, p, errUnexpectedEnd(p)
	}
	switch data[p] {
	case 't':
		if !isTrue(data, p) {
			return false, p, errBeginValue(data, p)
		}
		return true, p + 4, nil
	case 'f':
		if !isFalse(data, p) {
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
	if p < len(data) && data[p] == 'n' && isNull(data, p) {
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

// smallAny holds the float64 values 0 through 999 boxed as any, so that the
// small integers real documents are full of (array indices, counts, flags)
// do not each allocate when decoded into an any.
var smallAny = func() (t [1000]any) {
	for i := range t {
		t[i] = float64(i)
	}
	return
}()

// anyFrame is one level of the container stack used by [ParseAny]. A frame
// with a non-nil obj describes an object, otherwise it describes an array.
type anyFrame struct {
	arr []any
	obj map[string]any
	key string
	// keyPos is where key stands in the document, for the duplicate
	// error, which is raised when the value is attached rather than when
	// the name is read: a lookup then and an insert later would hash the
	// name twice, and the insert alone says whether the name was new.
	keyPos int
}

// ParseAny decodes the value at p the way encoding/json decodes into an
// interface{}: objects become map[string]any, arrays []any, numbers float64,
// strings string, booleans bool and null nil. The decoder is iterative and
// rejects documents nested deeper than [MaxDepth].
func ParseAny(data []byte, p int) (any, int, error) {
	return parseAny(data, p, nil, false, true)
}

// ParseAnyCached is [ParseAny] with a string cache for the member names and
// string values it produces; c may be nil.
func ParseAnyCached(data []byte, p int, c *StringCache) (any, int, error) {
	return parseAny(data, p, c, false, true)
}

// parseAny is the implementation of [ParseAny], [ParseAnyWith] and
// [ParseAnyStrict]. sc, when not nil, interns the strings the result holds.
//
// Two flags say which rules apply to strings and object names. legacy is
// encoding/json: invalid UTF-8 becomes U+FFFD and the last of two equal
// names wins. strict is encoding/json/v2 on unvalidated input: invalid
// UTF-8 and duplicate names are errors. Neither is input a jsontext.Decoder
// has validated, where nothing can occur that needs checking. They are two
// booleans rather than one mode so that [ParseAnyV2] stays a single call
// the compiler inlines into generated code.
func parseAny(data []byte, p int, sc *StringCache, strict, legacy bool) (any, int, error) {
	// The values that reach an interface are shallow: an object or two
	// with an array of numbers inside. Room for a few levels on the stack
	// keeps the container stack itself from being an allocation per value.
	var inline [4]anyFrame
	stack := inline[:0]
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
			key, next, err := parseKeyString(data, p, sc, strict)
			if err != nil {
				return nil, next, err
			}
			stack = append(stack, anyFrame{obj: obj, key: key, keyPos: p})
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
			// Room for a few elements up front: the arrays that reach an
			// any are mostly short, and this makes each one allocation.
			stack = append(stack, anyFrame{arr: make([]any, 0, 4)})
			continue
		case '"':
			var s string
			var next int
			var err error
			switch {
			case legacy:
				s, next, err = ParseStringCached(data, p, sc)
			case strict:
				s, next, err = ParseStringStrict(data, p, sc)
			default:
				s, next, err = ParseStringWith(data, p, sc)
			}
			if err != nil {
				return nil, next, err
			}
			v, p = s, next
		case 't':
			if !isTrue(data, p) {
				return nil, p, errBeginValue(data, p)
			}
			v, p = true, p+4
		case 'f':
			if !isFalse(data, p) {
				return nil, p, errBeginValue(data, p)
			}
			v, p = false, p+5
		case 'n':
			if !isNull(data, p) {
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
			if i := int(f); f >= 0 && f < float64(len(smallAny)) && float64(i) == f && data[p] != '-' {
				// A small non-negative integer, however it was spelled:
				// boxed once at init instead of once per value. The sign
				// test keeps -0 its own value.
				v = smallAny[i]
			} else {
				v = f
			}
			p = next
		}

		// A value has been decoded; attach it and close finished containers.
		for {
			if len(stack) == 0 {
				return v, p, nil
			}
			f := &stack[len(stack)-1]
			if f.obj != nil {
				n := len(f.obj)
				f.obj[f.key] = v
				if strict && len(f.obj) == n {
					// The insert found the name already there. The
					// value it replaced was the earlier member's, which
					// no longer matters: the document is refused.
					return nil, f.keyPos, ErrDuplicateName(data, f.keyPos, []byte(f.key))
				}
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
					key, next, err := parseKeyString(data, p, sc, strict)
					if err != nil {
						return nil, next, err
					}
					f.key, f.keyPos = key, p
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

// parseKeyString is [ParseKey] returning the member name as a Go string,
// interned through c when it is not nil.
func parseKeyString(data []byte, p int, c *StringCache, strict bool) (string, int, error) {
	var key []byte
	var next int
	var err error
	if strict {
		key, next, err = ParseKeyStrict(data, p)
	} else {
		key, _, next, err = ParseKey(data, p)
	}
	if err != nil {
		return "", next, err
	}
	return c.Make(key), next, nil
}

// unquote decodes the body of a JSON string literal (the bytes between the
// quotes) that has already been validated by scanString. It mirrors
// encoding/json's unquoteBytes: escapes are expanded, unpaired surrogates and
// invalid UTF-8 become U+FFFD. Under strict an unpaired surrogate is instead
// an error, as it is for encoding/json/v2, and s must already be known to be
// valid UTF-8: the strict callers have checked the whole body, so the runs
// between escapes are copied without being looked at again.
//
// The runs between escapes are copied whole: a run that is valid UTF-8, which
// is nearly every one, costs one validation pass and one copy instead of a
// decode and an encode per rune.
func unquote(s []byte, strict bool) ([]byte, bool) {
	b := make([]byte, 0, len(s)+2*utf8.UTFMax)
	r := 0
	for r < len(s) {
		n := bytes.IndexByte(s[r:], '\\')
		if n < 0 {
			n = len(s) - r
		}
		if run := s[r : r+n]; strict || utf8.Valid(run) {
			b = append(b, run...)
		} else {
			b = appendReplacing(b, run)
		}
		r += n
		if r >= len(s) {
			break
		}
		// s[r] is a backslash.
		r++
		if r >= len(s) {
			return nil, false
		}
		switch s[r] {
		case '"', '\\', '/':
			b = append(b, s[r])
			r++
		case 'b':
			b = append(b, '\b')
			r++
		case 'f':
			b = append(b, '\f')
			r++
		case 'n':
			b = append(b, '\n')
			r++
		case 'r':
			b = append(b, '\r')
			r++
		case 't':
			b = append(b, '\t')
			r++
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
					b = utf8.AppendRune(b, dec)
					break
				}
				if strict {
					return nil, false
				}
				rr = utf8.RuneError
			}
			b = utf8.AppendRune(b, rr)
		default:
			return nil, false
		}
	}
	return b, true
}

// appendReplacing appends run to b with every invalid UTF-8 byte replaced by
// U+FFFD, as encoding/json does.
func appendReplacing(b, run []byte) []byte {
	for len(run) > 0 {
		rr, size := utf8.DecodeRune(run)
		b = utf8.AppendRune(b, rr)
		run = run[size:]
	}
	return b
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

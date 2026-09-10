package odjsonrt

import (
	"bytes"
	"encoding"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math"
	"slices"
	"strconv"
	"unicode/utf8"
	"unsafe"
)

// hexDigits is the lowercase alphabet used by \uXXXX escapes.
const hexDigits = "0123456789abcdef"

// replacementChar is the UTF-8 encoding of U+FFFD, which encoding/json
// substitutes for invalid UTF-8 bytes when encoding a string.
const replacementChar = "\xef\xbf\xbd"

// safeSet reports, for every byte value, whether it can be copied into a JSON
// string literal verbatim. htmlSafeSet is the same but additionally escapes
// '<', '>' and '&'. Both mirror the tables in encoding/json, extended to 256
// entries so that the copy loop stops on non-ASCII bytes and hands them to the
// UTF-8 decoder.
var safeSet, htmlSafeSet = func() (safe, html [256]bool) {
	for c := range utf8.RuneSelf {
		ok := c >= 0x20 && c != '"' && c != '\\'
		safe[c] = ok
		html[c] = ok && c != '<' && c != '>' && c != '&'
	}
	return
}()

// safeSetUTF8 and htmlSafeSetUTF8 are the same tables for input that is known
// to be valid UTF-8. Every byte >= 0x80 is then safe to copy verbatim except
// 0xE2, which leads the only multi-byte sequences that still need escaping
// (U+2028 and U+2029), so whole runs of non-ASCII text move in one copy
// instead of being decoded rune by rune.
var safeSetUTF8, htmlSafeSetUTF8 = func() (safe, html [256]bool) {
	safe, html = safeSet, htmlSafeSet
	for c := utf8.RuneSelf; c < 256; c++ {
		ok := c != 0xE2
		safe[c] = ok
		html[c] = ok
	}
	return
}()

// AppendString appends s to dst as a quoted JSON string.
//
// Control bytes, '"' and '\\' are always escaped, U+2028 and U+2029 are always
// written as their \u escapes, and invalid UTF-8 is replaced with U+FFFD so
// that the result is always valid JSON. When escapeHTML is true, '<', '>' and
// '&' are escaped as well. This reproduces encoding/json byte for byte.
func AppendString(dst []byte, s string, escapeHTML bool) []byte {
	return appendQuotedString(dst, s, escapeHTML)
}

// AppendStringBytes is [AppendString] for a byte slice.
func AppendStringBytes(dst []byte, s []byte, escapeHTML bool) []byte {
	return appendQuoted(dst, s, escapeHTML)
}

// appendQuoted is the shared implementation of [AppendString] and
// [AppendStringBytes].
//
// It starts with a table in which every byte >= 0x80 is escaping-relevant, so
// pure ASCII input never pays for anything else. The first non-ASCII byte
// triggers a single vectorised UTF-8 check of the remainder: if that passes,
// the loop switches to a table that copies non-ASCII runs wholesale and only
// stops on 0xE2 (the lead byte of U+2028 and U+2029), otherwise it falls back
// to decoding rune by rune so that invalid bytes become U+FFFD the way
// encoding/json does.
func appendQuoted(dst []byte, src []byte, escapeHTML bool) []byte {
	safe, safeUTF8 := &safeSet, &safeSetUTF8
	if escapeHTML {
		safe, safeUTF8 = &htmlSafeSet, &htmlSafeSetUTF8
	}
	checked, valid := false, false

	dst = append(dst, '"')
	start := 0
	for i := 0; i < len(src); {
		// Copy runs of bytes that need no escaping in one go. A word at a
		// time scan was tried here and measured no faster: the escape set is
		// wide enough that computing the mask costs about what eight table
		// lookups do, and the CPU pipelines the lookups well.
		for i < len(src) && safe[src[i]] {
			i++
		}
		if i >= len(src) {
			break
		}
		if !checked && src[i] >= utf8.RuneSelf {
			checked = true
			if valid = utf8.Valid(src[i:]); valid {
				safe = safeUTF8
				continue
			}
		}
		if b := src[i]; b < utf8.RuneSelf {
			dst = append(dst, src[start:i]...)
			// NOTE: encoding/json emits the short \b and \f forms; the byte
			// for byte comparison tests against json.Marshal depend on it.
			switch b {
			case '\\', '"':
				dst = append(dst, '\\', b)
			case '\b':
				dst = append(dst, '\\', 'b')
			case '\f':
				dst = append(dst, '\\', 'f')
			case '\n':
				dst = append(dst, '\\', 'n')
			case '\r':
				dst = append(dst, '\\', 'r')
			case '\t':
				dst = append(dst, '\\', 't')
			default:
				dst = append(dst, '\\', 'u', '0', '0', hexDigits[b>>4], hexDigits[b&0xF])
			}
			i++
			start = i
			continue
		}
		if valid {
			// The only byte >= 0x80 the table stops on is 0xE2, which always
			// leads a three byte sequence here. U+2028 and U+2029 encode as
			// E2 80 A8 and E2 80 A9.
			if i+2 < len(src) && src[i+1] == 0x80 && src[i+2]&^1 == 0xA8 {
				dst = append(dst, src[start:i]...)
				dst = append(dst, '\\', 'u', '2', '0', '2', hexDigits[src[i+2]&0xF])
				i += 3
				start = i
				continue
			}
			i += 3
			continue
		}
		n := min(len(src)-i, utf8.UTFMax)
		c, size := utf8.DecodeRune(src[i : i+n])
		if c == utf8.RuneError && size == 1 {
			dst = append(dst, src[start:i]...)
			dst = append(dst, replacementChar...)
			i += size
			start = i
			continue
		}
		if c == 0x2028 || c == 0x2029 {
			dst = append(dst, src[start:i]...)
			dst = append(dst, '\\', 'u', '2', '0', '2', hexDigits[c&0xF])
			i += size
			start = i
			continue
		}
		i += size
	}
	dst = append(dst, src[start:]...)
	return append(dst, '"')
}

// appendQuotedString is [appendQuoted] for a string. The conversion is a view,
// not a copy, and it keeps the hot loop out of a generic function: a generic
// one would have to type switch on any(src) to read words, which boxes the
// string header on every call.
func appendQuotedString(dst []byte, s string, escapeHTML bool) []byte {
	return appendQuoted(dst, unsafe.Slice(unsafe.StringData(s), len(s)), escapeHTML)
}

// AppendStringQuoted appends s as a JSON string whose content is itself a JSON
// string. It implements the `,string` struct tag option for string fields:
// the value is escaped once with the requested HTML escaping and the result is
// escaped a second time without it, exactly like encoding/json.
func AppendStringQuoted(dst []byte, s string, escapeHTML bool) []byte {
	buf := AcquireBuffer()
	buf = appendQuotedString(buf, s, escapeHTML)
	dst = appendQuoted(dst, buf, false)
	ReleaseBuffer(buf)
	return dst
}

// AppendInt appends v as a JSON number.
func AppendInt(dst []byte, v int64) []byte {
	return strconv.AppendInt(dst, v, 10)
}

// AppendUint appends v as a JSON number.
func AppendUint(dst []byte, v uint64) []byte {
	return strconv.AppendUint(dst, v, 10)
}

// AppendFloat appends v as a JSON number using encoding/json's formatting
// rules: the 'e' format is used when the magnitude is below 1e-6 or at least
// 1e21 and the 'f' format otherwise, both with the shortest representation
// that round-trips, and a two digit negative exponent is trimmed to one digit.
// bits must be 32 or 64; any other value is treated as 64. NaN and infinities
// cannot be represented in JSON and produce an error.
func AppendFloat(dst []byte, v float64, bits int) ([]byte, error) {
	if bits != 32 {
		bits = 64
	}
	// Numbers decoded from JSON into an interface arrive as float64 even when
	// they are identifiers, and the shortest-representation algorithm is
	// expensive. Any value that is exactly an integer of at most fifteen
	// digits prints the same either way, so print it as one.
	// float32 is excluded: its shortest representation is computed at 32 bit
	// precision, so an integral value does not necessarily print as that
	// integer.
	if bits == 64 && v > -1e15 && v < 1e15 {
		if i := int64(v); float64(i) == v && (i != 0 || !math.Signbit(v)) {
			return strconv.AppendInt(dst, i, 10), nil
		}
	}
	if math.IsInf(v, 0) || math.IsNaN(v) {
		return dst, errors.New("json: unsupported value: " + strconv.FormatFloat(v, 'g', -1, bits))
	}

	abs := math.Abs(v)
	format := byte('f')
	if abs != 0 {
		if bits == 64 && (abs < 1e-6 || abs >= 1e21) ||
			bits == 32 && (float32(abs) < 1e-6 || float32(abs) >= 1e21) {
			format = 'e'
		}
	}
	if format == 'f' && bits == 64 {
		// A short decimal is printed without Ryu; see ftoa.go.
		if out, ok := appendShortFloat(dst, v); ok {
			return out, nil
		}
	}
	dst = strconv.AppendFloat(dst, v, format, -1, bits)
	if format == 'e' {
		// Clean up e-09 to e-9.
		n := len(dst)
		if n >= 4 && dst[n-4] == 'e' && dst[n-3] == '-' && dst[n-2] == '0' {
			dst[n-2] = dst[n-1]
			dst = dst[:n-1]
		}
	}
	return dst, nil
}

// AppendBool appends v as a JSON boolean.
func AppendBool(dst []byte, v bool) []byte {
	if v {
		return append(dst, "true"...)
	}
	return append(dst, "false"...)
}

// AppendBase64 appends b as a base64 encoded JSON string, matching how
// encoding/json marshals []byte. A nil slice is encoded as null.
func AppendBase64(dst []byte, b []byte) []byte {
	if b == nil {
		return append(dst, "null"...)
	}
	dst = append(dst, '"')
	n := base64.StdEncoding.EncodedLen(len(b))
	if cap(dst)-len(dst) < n+1 {
		grown := make([]byte, len(dst), len(dst)+n+1)
		copy(grown, dst)
		dst = grown
	}
	end := len(dst) + n
	base64.StdEncoding.Encode(dst[len(dst):end], b)
	return append(dst[:end], '"')
}

// AppendNumber appends a json.Number literal, validating it first. The empty
// string is encoded as 0, matching encoding/json.
func AppendNumber(dst []byte, n string) ([]byte, error) {
	if n == "" {
		return append(dst, '0'), nil
	}
	if end, ok := scanNumberIn(n, 0); !ok || end != len(n) {
		return dst, errors.New("json: invalid number literal " + strconv.Quote(n))
	}
	return append(dst, n...), nil
}

// AppendRaw appends a json.RawMessage: raw is validated and compacted, and
// U+2028 and U+2029 are always escaped and, when escapeHTML is true, '<',
// '>' and '&' are escaped as well, just like encoding/json. A nil slice is
// encoded as null.
func AppendRaw(dst []byte, raw []byte, escapeHTML bool) ([]byte, error) {
	if raw == nil {
		return append(dst, "null"...), nil
	}
	if err := Validate(raw); err != nil {
		return dst, err
	}
	return appendCompact(dst, raw, escapeHTML), nil
}

// appendCompact copies the already validated JSON value src to dst, dropping
// insignificant whitespace and applying HTML escaping when requested.
func appendCompact(dst, src []byte, escapeHTML bool) []byte {
	start := 0
	inString := false
	for i := 0; i < len(src); i++ {
		c := src[i]
		if escapeHTML && (c == '<' || c == '>' || c == '&') {
			dst = append(dst, src[start:i]...)
			dst = append(dst, '\\', 'u', '0', '0', hexDigits[c>>4], hexDigits[c&0xF])
			start = i + 1
			continue
		}
		// U+2028 and U+2029 encode as E2 80 A8 and E2 80 A9. They are escaped
		// unconditionally, matching the JS-safe escaping encoding/json applies
		// to string literals.
		if c == 0xE2 && i+2 < len(src) && src[i+1] == 0x80 && src[i+2]&^1 == 0xA8 {
			dst = append(dst, src[start:i]...)
			dst = append(dst, '\\', 'u', '2', '0', '2', hexDigits[src[i+2]&0xF])
			i += 2
			start = i + 1
			continue
		}
		if inString {
			switch c {
			case '"':
				inString = false
			case '\\':
				i++
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case ' ', '\t', '\n', '\r':
			dst = append(dst, src[start:i]...)
			start = i + 1
		}
	}
	return append(dst, src[start:]...)
}

// AppendAny appends an arbitrary Go value. The shapes that decoding produces
// for interface{} fields (nil, bool, string, float64, []any, map[string]any)
// and the remaining built-in scalar types are encoded directly; anything else
// falls back to encoding/json and therefore to reflection.
func AppendAny(dst []byte, v any, escapeHTML bool) ([]byte, error) {
	m := ModePlain
	if escapeHTML {
		m = ModeHTML
	}
	return AppendAnyMode(dst, v, m)
}

// AppendAnyMode is [AppendAny] under an explicit [StringMode]. Generated code
// uses it so that a dynamic value inside a struct is escaped by the same rules
// as the struct's static fields — in particular so that a value destined for a
// jsontext.Encoder does not pay for UTF-8 validation the encoder repeats.
func AppendAnyMode(dst []byte, v any, m StringMode) ([]byte, error) {
	out, err := appendAny(dst, v, m, 0)
	if err != nil {
		return dst, err
	}
	return out, nil
}

// appendAny is [AppendAny] with a recursion depth, so that self-referential
// values are handed to encoding/json (which detects cycles) instead of
// overflowing the stack.
func appendAny(dst []byte, v any, m StringMode, depth int) ([]byte, error) {
	if depth >= MaxDepth {
		return appendAnyReflect(dst, v, m.EscapeHTML())
	}
	switch x := v.(type) {
	case nil:
		return append(dst, "null"...), nil
	case bool:
		return AppendBool(dst, x), nil
	case string:
		return AppendStringChecked(dst, x, m)
	case float64:
		return AppendFloat(dst, x, 64)
	case float32:
		return AppendFloat(dst, float64(x), 32)
	case int:
		return AppendInt(dst, int64(x)), nil
	case int8:
		return AppendInt(dst, int64(x)), nil
	case int16:
		return AppendInt(dst, int64(x)), nil
	case int32:
		return AppendInt(dst, int64(x)), nil
	case int64:
		return AppendInt(dst, x), nil
	case uint:
		return AppendUint(dst, uint64(x)), nil
	case uint8:
		return AppendUint(dst, uint64(x)), nil
	case uint16:
		return AppendUint(dst, uint64(x)), nil
	case uint32:
		return AppendUint(dst, uint64(x)), nil
	case uint64:
		return AppendUint(dst, x), nil
	case uintptr:
		return AppendUint(dst, uint64(x)), nil
	case json.Number:
		return AppendNumber(dst, string(x))
	case json.RawMessage:
		return AppendRaw(dst, x, m.EscapeHTML())
	case []byte:
		return AppendBase64(dst, x), nil
	case []any:
		if x == nil {
			return append(dst, "null"...), nil
		}
		dst = append(dst, '[')
		for i, elem := range x {
			if i > 0 {
				dst = append(dst, ',')
			}
			var err error
			if dst, err = appendAny(dst, elem, m, depth+1); err != nil {
				return dst, err
			}
		}
		return append(dst, ']'), nil
	case map[string]any:
		if x == nil {
			return append(dst, "null"...), nil
		}
		if m.V2() {
			// encoding/json/v2 writes object members in map iteration order;
			// only encoding/json sorts them. Skipping the sort is both the
			// matching semantics and one less allocation per object.
			dst = append(dst, '{')
			first := true
			for k, elem := range x {
				if !first {
					dst = append(dst, ',')
				}
				first = false
				var err error
				if dst, err = AppendStringChecked(dst, k, m); err != nil {
					return dst, err
				}
				dst = append(dst, ':')
				if dst, err = appendAny(dst, elem, m, depth+1); err != nil {
					return dst, err
				}
			}
			return append(dst, '}'), nil
		}
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		dst = append(dst, '{')
		for i, k := range keys {
			if i > 0 {
				dst = append(dst, ',')
			}
			dst = AppendStringMode(dst, k, m)
			dst = append(dst, ':')
			var err error
			if dst, err = appendAny(dst, x[k], m, depth+1); err != nil {
				return dst, err
			}
		}
		return append(dst, '}'), nil
	default:
		return appendAnyReflect(dst, v, m.EscapeHTML())
	}
}

// appendAnyReflect encodes v with encoding/json.
func appendAnyReflect(dst []byte, v any, escapeHTML bool) ([]byte, error) {
	if escapeHTML {
		b, err := json.Marshal(v)
		if err != nil {
			return dst, err
		}
		return append(dst, b...), nil
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return dst, err
	}
	b := buf.Bytes()
	// Encode always terminates the value with a newline.
	if n := len(b); n > 0 && b[n-1] == '\n' {
		b = b[:n-1]
	}
	return append(dst, b...), nil
}

// AppendMarshaler appends the output of m.MarshalJSON, validated and
// compacted the same way encoding/json does. A nil interface encodes as null;
// a non-nil interface holding a nil pointer is the caller's responsibility.
func AppendMarshaler(dst []byte, m json.Marshaler, escapeHTML bool) ([]byte, error) {
	if m == nil {
		return append(dst, "null"...), nil
	}
	b, err := m.MarshalJSON()
	if err != nil {
		return dst, err
	}
	if err := Validate(b); err != nil {
		return dst, err
	}
	return appendCompact(dst, b, escapeHTML), nil
}

// AppendTextMarshaler appends the output of m.MarshalText as a JSON string.
// A nil interface encodes as null.
func AppendTextMarshaler(dst []byte, m encoding.TextMarshaler, escapeHTML bool) ([]byte, error) {
	if m == nil {
		return append(dst, "null"...), nil
	}
	b, err := m.MarshalText()
	if err != nil {
		return dst, err
	}
	return appendQuoted(dst, b, escapeHTML), nil
}

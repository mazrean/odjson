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
	return appendQuoted(dst, s, escapeHTML, true)
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
//
// quoted says whether to write the surrounding quotes; a generated encoder
// that has folded the opening quote into a longer literal, and will fold the
// closing one into the next, passes false. One parameter rather than a
// wrapper, so that a string still costs a single call.
func appendQuoted(dst []byte, src []byte, escapeHTML, quoted bool) []byte {
	if quoted {
		dst = append(dst, '"')
	}
	start := 0
	for i := 0; i < len(src); {
		// A word at a time, as ModeV2HTML's appender does: the mask stops
		// at every byte the table would, and at the first non-ASCII byte,
		// which starts a run for the paths below. The two loops differ
		// only in the mask, and the HTML one adds the angle brackets and
		// the ampersand to it.
		if escapeHTML {
			for i+8 <= len(src) {
				w := load64(src, i)
				if m := swarUnsafeHTML(w) | w&swarHi; m != 0 {
					i += swarIndex(m)
					goto found
				}
				i += 8
			}
			for uint(i) < uint(len(src)) && htmlSafeSet[src[i]] {
				i++
			}
		} else {
			for i+8 <= len(src) {
				w := load64(src, i)
				if m := swarUnsafe(w) | w&swarHi; m != 0 {
					i += swarIndex(m)
					goto found
				}
				i += 8
			}
			for uint(i) < uint(len(src)) && safeSet[src[i]] {
				i++
			}
		}
	found:
		if uint(i) >= uint(len(src)) {
			break
		}
		b := src[i]
		if b >= utf8.RuneSelf {
			// A two byte sequence followed by ASCII, then words of
			// accented Latin text, are settled without a call; neither
			// can hold a line separator.
			if b-0xC2 < 0x1E && uint(i+1) < uint(len(src)) && src[i+1]&0xC0 == 0x80 && (uint(i+2) >= uint(len(src)) || src[i+2] < utf8.RuneSelf) {
				i += 2
				for i+8 <= len(src) {
					w := load64(src, i)
					if w&swarHi == 0 || swarUnsafe(w) != 0 || (escapeHTML && swarHTMLOnly(w) != 0) || !swarLatin(w) {
						break
					}
					i += 8
				}
				continue
			}
			// A lone four byte sequence (an emoji among ASCII) likewise: F0
			// needs a second byte of 90-BF, F4 one of 80-8F, F1-F3 any.
			if b-0xF0 < 5 && uint(i+3) < uint(len(src)) && src[i+1]&0xC0 == 0x80 && src[i+2]&0xC0 == 0x80 && src[i+3]&0xC0 == 0x80 &&
				(b != 0xF0 || src[i+1] >= 0x90) && (b != 0xF4 || src[i+1] < 0x90) {
				i += 4
				continue
			}
			j := skipNonASCII(src, i)
			if j < 0 {
				// Not UTF-8 somewhere in this run. encoding/json decodes
				// rune by rune and writes U+FFFD for each byte it refuses,
				// escaping a line separator on the way; the rest of the
				// run is settled here, not by scanning it again from the
				// next rune on.
				for uint(i) < uint(len(src)) && src[i] >= utf8.RuneSelf {
					r, size := utf8.DecodeRune(src[i:])
					switch {
					case r == utf8.RuneError && size == 1:
						dst = append(dst, src[start:i]...)
						dst = append(dst, replacementChar...)
						start = i + 1
					case r == 0x2028 || r == 0x2029:
						dst = append(dst, src[start:i]...)
						dst = append(dst, '\\', 'u', '2', '0', '2', hexDigits[r&0xF])
						start = i + size
					}
					i += size
				}
				continue
			}
			// A valid run: only U+2028 and U+2029, E2 80 A8 and E2 80 A9,
			// need escaping in it.
			for k := i; k+2 < j; {
				n := bytes.IndexByte(src[k:j-2], 0xE2)
				if n < 0 {
					break
				}
				k += n
				if src[k+1] == 0x80 && src[k+2]&^1 == 0xA8 {
					dst = append(dst, src[start:k]...)
					dst = append(dst, '\\', 'u', '2', '0', '2', hexDigits[src[k+2]&0xF])
					k += 3
					start = k
					continue
				}
				k++
			}
			i = j
			continue
		}
		dst = append(dst, src[start:i]...)
		// NOTE: encoding/json emits the short \b and \f forms, which
		// appendEscape does; the byte for byte comparison tests against
		// json.Marshal depend on it. '<', '>' and '&' take its \u00XX
		// default.
		dst = appendEscape(dst, b)
		i++
		start = i
	}
	dst = append(dst, src[start:]...)
	if quoted {
		dst = append(dst, '"')
	}
	return dst
}

// appendQuotedString is [appendQuoted] for a string. The conversion is a view,
// not a copy, and it keeps the hot loop out of a generic function: a generic
// one would have to type switch on any(src) to read words, which boxes the
// string header on every call.
func appendQuotedString(dst []byte, s string, escapeHTML bool) []byte {
	return appendQuoted(dst, unsafe.Slice(unsafe.StringData(s), len(s)), escapeHTML, true)
}

// AppendStringQuoted appends s as a JSON string whose content is itself a JSON
// string. It implements the `,string` struct tag option for string fields:
// the value is escaped once with the requested HTML escaping and the result is
// escaped a second time without it, exactly like encoding/json.
func AppendStringQuoted(dst []byte, s string, escapeHTML bool) []byte {
	buf := AcquireBuffer()
	buf = appendQuotedString(buf, s, escapeHTML)
	dst = appendQuoted(dst, buf, false, true)
	ReleaseBuffer(buf)
	return dst
}

// AppendInt appends v as a JSON number.
//
// The sign travels as a flag rather than as a call of its own: a function
// with a call on each side of a branch costs more inline units than the
// budget allows, and then every signed member of every struct pays two
// calls instead of one.
func AppendInt(dst []byte, v int64) []byte {
	// The magnitude without a branch: XOR with the sign-extended sign bit
	// and subtract it, which is two's complement negation for a negative
	// value and nothing for a positive one. It is also what makes the
	// smallest int64 need no case of its own.
	mask := uint64(v >> 63)
	return appendUint(dst, mask != 0, (uint64(v)^mask)-mask)
}

// AppendUint appends v as a JSON number.
func AppendUint(dst []byte, v uint64) []byte {
	return appendUint(dst, false, v)
}

// smalls holds the two digit decimal representation of every value below a
// hundred, which is what most of a document's numbers are: counts, indices,
// lengths, the elements of an array of offsets.
const smalls = "00010203040506070809101112131415161718192021222324252627282930313233343536373839404142434445464748495051525354555657585960616263646566676869707172737475767778798081828384858687888990919293949596979899"

// appendUint writes v's digits into dst.
//
// strconv formats into a scratch array of its own and copies the digits out
// of it, which is a call, a store-forwarding stall and a runtime.memmove per
// number; on a document whose numbers are thousands of small counts and a
// few hundred eighteen digit ids that was a tenth of the encode. Here the
// digits are written where they belong. [digits8] turns up to eight of them
// into one word with three multiplications, and the shift drops the leading
// zeros of it, so a number of any length is one to three stores; the room
// for the widest one is taken once, which is also what lets a store land
// past the digits that follow it.
func appendUint(dst []byte, neg bool, v uint64) []byte {
	if neg {
		dst = append(dst, '-')
	}
	if v < 100 {
		if v < 10 {
			return append(dst, byte('0'+v))
		}
		return append(dst, smalls[v*2], smalls[v*2+1])
	}
	nd := numDigits(v)
	n := len(dst)
	// The widest uint64 is twenty digits, and a store writes eight.
	if cap(dst)-n < 28 {
		dst = slices.Grow(dst, 28)
	}
	d := dst[:cap(dst)]
	switch {
	case v < 1e8:
		store64(d, n, digits8(v)>>(8*uint(8-nd)))
	case v < 1e16:
		store64(d, n, digits8(v/1e8)>>(8*uint(16-nd)))
		store64(d, n+nd-8, digits8(v%1e8))
	default:
		store64(d, n, digits8(v/1e16)>>(8*uint(24-nd)))
		store64(d, n+nd-16, digits8(v/1e8%1e8))
		store64(d, n+nd-8, digits8(v%1e8))
	}
	return dst[:n+nd]
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
			return AppendInt(dst, i), nil
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
	// The number is written here rather than by strconv; see ftoa.go.
	if format == 'f' && bits == 64 && abs != 0 {
		if out, ok := appendShortFloat(dst, v < 0, abs); ok {
			return out, nil
		}
	}
	return appendFloatSearch(dst, v, bits, format), nil
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
		// The quotes are written here and the body called directly: the
		// quoted helper is one unit over the inlining budget, and a string
		// inside an any is as common as a string member on the documents
		// that matter.
		dst, err := AppendStringBodyChecked(append(dst, '"'), x, m)
		if err != nil {
			return dst, err
		}
		return append(dst, '"'), nil
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
				if dst, err = AppendStringBodyChecked(append(dst, '"'), k, m); err != nil {
					return dst, err
				}
				dst = append(dst, '"', ':')
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
	return appendQuoted(dst, b, escapeHTML, true), nil
}

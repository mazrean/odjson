package odjsonrt

import (
	"bytes"
	"encoding/binary"
	"errors"
	"unicode/utf8"
	"unsafe"
)

// StringMode selects the escaping rules used when a JSON string is appended.
//
// The distinction exists because generated code feeds two different consumers.
// [ModeHTML] and [ModePlain] produce bytes that are handed to a caller as-is
// and must therefore be self-sufficient: invalid UTF-8 is replaced with U+FFFD
// and U+2028/U+2029 are escaped, exactly like encoding/json. [ModeStream]
// produces bytes that are immediately handed to a
// [encoding/json/jsontext.Encoder], which validates and re-escapes them; doing
// that work twice is measurable, so this mode does neither.
type StringMode uint8

const (
	// ModeHTML escapes '<', '>' and '&', which is encoding/json's default.
	ModeHTML StringMode = iota
	// ModePlain is ModeHTML without the HTML escaping.
	ModePlain
	// ModeStream escapes only what JSON syntax requires and copies every
	// byte >= 0x80 verbatim. It is only correct when the result is passed to
	// a jsontext.Encoder, which rejects invalid UTF-8 itself.
	ModeStream
	// ModeV2 is encoding/json/v2's own output: ModeStream's escaping, with
	// invalid UTF-8 reported as an error since nothing downstream will. It
	// is what the direct path (see direct.go) writes into the encoder.
	ModeV2
)

// EscapeHTML reports whether the mode escapes '<', '>' and '&'.
func (m StringMode) EscapeHTML() bool { return m == ModeHTML }

// V2 reports whether the mode follows encoding/json/v2's semantics for
// everything but string escaping: nil containers encode as empty ones and
// omitempty keeps zero numbers and bools.
func (m StringMode) V2() bool { return m == ModeStream || m == ModeV2 }

// AppendStringChecked is [AppendStringMode] with the error [ModeV2] can
// report: a string that is not valid UTF-8. The other modes never fail.
func AppendStringChecked(dst []byte, s string, m StringMode) ([]byte, error) {
	// A single call, so that this inlines into generated code and a string
	// under ModeV2, the common case, costs one call rather than two.
	return appendStringChecked(dst, unsafe.Slice(unsafe.StringData(s), len(s)), m)
}

// ErrInvalidUTF8 is reported by [AppendStringChecked] under [ModeV2] for a
// string that is not valid UTF-8, which encoding/json/v2 refuses to encode.
var ErrInvalidUTF8 = errors.New("odjson: invalid UTF-8 in string")

// streamSafeSet marks the bytes ModeStream copies verbatim: every byte that
// JSON does not require to be escaped, including all of 0x80-0xFF. U+2028 and
// U+2029 are left alone because encoding/json/v2 does not escape them either.
var streamSafeSet = func() (t [256]bool) {
	for c := range t {
		t[c] = c >= 0x20 && c != '"' && c != '\\'
	}
	for c := utf8.RuneSelf; c < 256; c++ {
		t[c] = true
	}
	return
}()

// AppendStringMode appends s to dst as a quoted JSON string under mode m.
// Under [ModeV2] it cannot report invalid UTF-8; use [AppendStringChecked].
func AppendStringMode(dst []byte, s string, m StringMode) []byte {
	if m.V2() {
		return appendQuotedStreamString(dst, s)
	}
	return appendQuotedString(dst, s, m == ModeHTML)
}

// AppendStringBytesMode is [AppendStringMode] for a byte slice.
func AppendStringBytesMode(dst []byte, s []byte, m StringMode) []byte {
	if m.V2() {
		return appendQuotedStream(dst, s)
	}
	return appendQuoted(dst, s, m == ModeHTML)
}

// AppendStringQuotedMode writes s as a JSON string whose content is itself a
// JSON string, which is what the ",string" struct tag option asks for.
func AppendStringQuotedMode(dst []byte, s string, m StringMode) []byte {
	if !m.V2() {
		return AppendStringQuoted(dst, s, m == ModeHTML)
	}
	buf := GetBuffer()
	buf.B = appendQuotedStreamString(buf.B, s)
	dst = appendQuotedStream(dst, buf.B)
	PutBuffer(buf)
	return dst
}

// Word-at-a-time constants for the escape scan below.
const (
	swarLo = 0x0101010101010101
	swarHi = 0x8080808080808080
)

// swarUnsafe reports, for eight packed bytes, whether any of them needs
// escaping under ModeStream: a control byte, a quote or a backslash. Bytes
// >= 0x80 never match, because the &^w term clears their high bit, which is
// exactly the behaviour ModeStream wants.
func swarUnsafe(w uint64) uint64 {
	ctrl := (w - swarLo*0x20) &^ w
	quote := w ^ (swarLo * '"')
	quote = (quote - swarLo) &^ quote
	esc := w ^ (swarLo * '\\')
	esc = (esc - swarLo) &^ esc
	return (ctrl | quote | esc) & swarHi
}

// appendQuotedStreamString is [appendQuotedStream] for a string. Viewing the
// string as a byte slice keeps the implementation single: a generic function
// would have to type switch on any(src), which boxes the string header on
// every call and costs more than the scan for short values.
func appendQuotedStreamString(dst []byte, s string) []byte {
	return appendQuotedStream(dst, unsafe.Slice(unsafe.StringData(s), len(s)))
}

// appendQuotedStream is the ModeStream implementation: eight bytes are checked
// per iteration, whole safe runs are copied at once, and no UTF-8 decoding
// happens at all.
func appendQuotedStream(dst []byte, src []byte) []byte {
	dst = append(dst, '"')
	start := 0
	for i := 0; i < len(src); {
		// Two words per iteration: the loads are independent, so the CPU
		// overlaps them and the scan runs close to load throughput.
		for i+16 <= len(src) {
			w0 := binary.LittleEndian.Uint64(src[i:])
			w1 := binary.LittleEndian.Uint64(src[i+8:])
			if m0 := swarUnsafe(w0); m0 != 0 {
				i += swarIndex(m0)
				goto found
			}
			if m1 := swarUnsafe(w1); m1 != 0 {
				i += 8 + swarIndex(m1)
				goto found
			}
			i += 16
		}
		for i+8 <= len(src) {
			w := binary.LittleEndian.Uint64(src[i:])
			if m := swarUnsafe(w); m != 0 {
				i += swarIndex(m)
				goto found
			}
			i += 8
		}
		for i < len(src) && streamSafeSet[src[i]] {
			i++
		}
	found:
		if i >= len(src) {
			break
		}
		dst = append(dst, src[start:i]...)
		dst = appendEscape(dst, src[i])
		i++
		start = i
	}
	dst = append(dst, src[start:]...)
	return append(dst, '"')
}

// appendEscape appends the escape sequence for b, a byte that JSON syntax
// does not allow verbatim in a string literal.
func appendEscape(dst []byte, b byte) []byte {
	switch b {
	case '\\', '"':
		return append(dst, '\\', b)
	case '\b':
		return append(dst, '\\', 'b')
	case '\f':
		return append(dst, '\\', 'f')
	case '\n':
		return append(dst, '\\', 'n')
	case '\r':
		return append(dst, '\\', 'r')
	case '\t':
		return append(dst, '\\', 't')
	default:
		return append(dst, '\\', 'u', '0', '0', hexDigits[b>>4], hexDigits[b&0xF])
	}
}

// appendStringChecked is [AppendStringChecked] on a byte slice. Its body is
// the ModeV2 implementation: [appendQuotedStream]'s escaping with json/v2's
// UTF-8 rule folded into the same pass. The word scan stops at a byte that
// needs escaping or at the first non-ASCII byte; a non-ASCII run is
// validated in place by skipNonASCII, which also finds where the scan
// resumes. A string that is not valid UTF-8 is reported as [ErrInvalidUTF8]
// with dst as it was. The other modes are handed on.
func appendStringChecked(dst []byte, src []byte, m StringMode) ([]byte, error) {
	switch m {
	case ModeStream:
		return appendQuotedStream(dst, src), nil
	case ModeHTML, ModePlain:
		return appendQuoted(dst, src, m == ModeHTML), nil
	}
	mark := len(dst)
	dst = append(dst, '"')
	start := 0
	for i := 0; i < len(src); {
		for i+16 <= len(src) {
			w0 := binary.LittleEndian.Uint64(src[i:])
			w1 := binary.LittleEndian.Uint64(src[i+8:])
			if m0 := swarUnsafe(w0) | w0&swarHi; m0 != 0 {
				i += swarIndex(m0)
				goto found
			}
			if m1 := swarUnsafe(w1) | w1&swarHi; m1 != 0 {
				i += 8 + swarIndex(m1)
				goto found
			}
			i += 16
		}
		for i+8 <= len(src) {
			w := binary.LittleEndian.Uint64(src[i:])
			if m := swarUnsafe(w) | w&swarHi; m != 0 {
				i += swarIndex(m)
				goto found
			}
			i += 8
		}
		// safeSet is false for every byte >= 0x80, so this stops where the
		// word scan would have.
		for i < len(src) && safeSet[src[i]] {
			i++
		}
	found:
		if i >= len(src) {
			break
		}
		if b := src[i]; b >= utf8.RuneSelf {
			// The run stays part of the pending copy: a valid sequence
			// holds nothing that needs escaping.
			if i = skipNonASCII(src, i); i < 0 {
				return dst[:mark], ErrInvalidUTF8
			}
			continue
		}
		dst = append(dst, src[start:i]...)
		dst = appendEscape(dst, src[i])
		i++
		start = i
	}
	dst = append(dst, src[start:]...)
	return append(dst, '"'), nil
}

// ParseStringTrusted is [ParseString] for input whose UTF-8 has already been
// validated, which is the case for every value a
// [encoding/json/jsontext.Decoder] hands to an UnmarshalJSONFrom method. It
// skips the validation pass ParseString performs over non-ASCII content, and
// therefore does not substitute U+FFFD: invalid input must have been rejected
// by the decoder before it gets here.
func ParseStringTrusted(data []byte, p int) (string, int, error) {
	if p >= len(data) {
		return "", p, errUnexpectedEnd(p)
	}
	if data[p] != '"' {
		return "", p, ErrType(data, p, "string")
	}
	end, hasEscape, _, err := scanString(data, p)
	if err != nil {
		return "", end, err
	}
	body := data[p+1 : end-1]
	if !hasEscape {
		return string(body), end, nil
	}
	out, ok := unquote(body, false)
	if !ok {
		return "", p, ErrSyntax(data, p, "invalid string literal")
	}
	return adoptString(out, false), end, nil
}

// UnquoteName returns the unescaped content of the quoted object member name
// that a [encoding/json/jsontext.Decoder] returned. The result may alias name.
func UnquoteName(name []byte) ([]byte, bool) {
	if len(name) < 2 || name[0] != '"' || name[len(name)-1] != '"' {
		return nil, false
	}
	body := name[1 : len(name)-1]
	// bytes.IndexByte rather than a hand-rolled loop or slices.Contains: this
	// runs once per member name, and only IndexByte is vectorised.
	if bytes.IndexByte(body, '\\') >= 0 {
		return unquote(body, false)
	}
	return body, true
}

// ErrKind reports that a jsontext.Decoder produced a value of the wrong shape
// for the Go type being decoded into. kind is the byte returned by
// jsontext.Decoder.PeekKind.
func ErrKind(kind byte, goType string) error {
	return &TypeError{Value: kindName(kind), Type: goType}
}

func kindName(kind byte) string {
	switch kind {
	case 'n':
		return "null"
	case 'f', 't':
		return "bool"
	case '"':
		return "string"
	case '0':
		return "number"
	case '{':
		return "object"
	case '[':
		return "array"
	case 0:
		return "end of input"
	default:
		return "invalid token"
	}
}

// AppendNilSlice, AppendNilMap and AppendNilBytes write the representation of
// a nil container under mode m.
//
// encoding/json writes null for all three. encoding/json/v2 writes an empty
// array, an empty object and an empty string, so generated code that feeds a
// jsontext.Encoder must follow suit: a drop-in accelerator has to preserve the
// output of the library it is dropped into.
func AppendNilSlice(dst []byte, m StringMode) []byte {
	if m.V2() {
		return append(dst, '[', ']')
	}
	return append(dst, "null"...)
}

// AppendNilMap writes a nil map under mode m.
func AppendNilMap(dst []byte, m StringMode) []byte {
	if m.V2() {
		return append(dst, '{', '}')
	}
	return append(dst, "null"...)
}

// AppendNilBytes writes a nil byte slice under mode m.
func AppendNilBytes(dst []byte, m StringMode) []byte {
	if m.V2() {
		return append(dst, '"', '"')
	}
	return append(dst, "null"...)
}

// ErrArrayLength reports that a JSON array had the wrong number of elements
// for a Go array. encoding/json pads or truncates; encoding/json/v2 rejects,
// and the generated jsontext driven decoders follow json/v2.
func ErrArrayLength(goType string, tooMany bool) error {
	which := "too few array elements"
	if tooMany {
		which = "too many array elements"
	}
	return &TypeError{Value: which, Type: goType}
}

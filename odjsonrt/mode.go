package odjsonrt

import (
	"encoding/binary"
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
)

// EscapeHTML reports whether the mode escapes '<', '>' and '&'.
func (m StringMode) EscapeHTML() bool { return m == ModeHTML }

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
func AppendStringMode(dst []byte, s string, m StringMode) []byte {
	if m == ModeStream {
		return appendQuotedStreamString(dst, s)
	}
	return appendQuotedString(dst, s, m == ModeHTML)
}

// AppendStringBytesMode is [AppendStringMode] for a byte slice.
func AppendStringBytesMode(dst []byte, s []byte, m StringMode) []byte {
	if m == ModeStream {
		return appendQuotedStream(dst, s)
	}
	return appendQuoted(dst, s, m == ModeHTML)
}

// AppendStringQuotedMode writes s as a JSON string whose content is itself a
// JSON string, which is what the ",string" struct tag option asks for.
func AppendStringQuotedMode(dst []byte, s string, m StringMode) []byte {
	if m != ModeStream {
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
			if m := swarUnsafe(binary.LittleEndian.Uint64(src[i:])); m != 0 {
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
		switch b := src[i]; b {
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
	}
	dst = append(dst, src[start:]...)
	return append(dst, '"')
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
	out, ok := unquote(body)
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
	for i := 0; i < len(body); i++ {
		if body[i] == '\\' {
			return unquote(body)
		}
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
	if m == ModeStream {
		return append(dst, '[', ']')
	}
	return append(dst, "null"...)
}

// AppendNilMap writes a nil map under mode m.
func AppendNilMap(dst []byte, m StringMode) []byte {
	if m == ModeStream {
		return append(dst, '{', '}')
	}
	return append(dst, "null"...)
}

// AppendNilBytes writes a nil byte slice under mode m.
func AppendNilBytes(dst []byte, m StringMode) []byte {
	if m == ModeStream {
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

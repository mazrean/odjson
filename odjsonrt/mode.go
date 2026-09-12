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
	// ModeV2HTML is what encoding/json's Marshal makes of a MarshalerTo:
	// json/v2's semantics for everything but the string escaping, which is
	// encoding/json's. It reproduces the reformat that call applies to
	// [ModeStream] bytes: '<', '>', '&', U+2028 and U+2029 are escaped and
	// invalid UTF-8 is copied as it is. The direct path writes it into an
	// encoder that call created.
	ModeV2HTML
)

// EscapeHTML reports whether the mode escapes '<', '>' and '&'.
func (m StringMode) EscapeHTML() bool { return m == ModeHTML || m == ModeV2HTML }

// V2 reports whether the mode follows encoding/json/v2's semantics for
// everything but string escaping: nil containers encode as empty ones and
// omitempty keeps zero numbers and bools.
func (m StringMode) V2() bool { return m >= ModeStream }

// AppendStringChecked is [AppendStringMode] with the error [ModeV2] can
// report: a string that is not valid UTF-8. The other modes never fail.
func AppendStringChecked(dst []byte, s string, m StringMode) ([]byte, error) {
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
	return AppendStringBytesMode(dst, unsafe.Slice(unsafe.StringData(s), len(s)), m)
}

// AppendStringBytesMode is [AppendStringMode] for a byte slice.
func AppendStringBytesMode(dst []byte, s []byte, m StringMode) []byte {
	switch m {
	case ModeStream, ModeV2:
		return appendQuotedStream(dst, s, true)
	case ModeV2HTML:
		return appendQuotedV2HTML(dst, s, true)
	}
	return appendQuoted(dst, s, m == ModeHTML, true)
}

// AppendStringQuotedMode writes s as a JSON string whose content is itself a
// JSON string, which is what the ",string" struct tag option asks for.
func AppendStringQuotedMode(dst []byte, s string, m StringMode) []byte {
	if !m.V2() {
		return AppendStringQuoted(dst, s, m == ModeHTML)
	}
	// The inner string is always ModeStream's: under ModeV2HTML the
	// reformat this mode reproduces only ever sees the outer one.
	buf := GetBuffer()
	buf.B = appendQuotedStreamString(buf.B, s)
	dst = AppendStringBytesMode(dst, buf.B, m)
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
	return appendQuotedStream(dst, unsafe.Slice(unsafe.StringData(s), len(s)), true)
}

// appendQuotedStream is the ModeStream implementation: eight bytes are checked
// per iteration, whole safe runs are copied at once, and no UTF-8 decoding
// happens at all.
//
// quoted says whether to write the surrounding quotes (see appendQuoted).
func appendQuotedStream(dst []byte, src []byte, quoted bool) []byte {
	if quoted {
		dst = append(dst, '"')
	}
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
	if quoted {
		dst = append(dst, '"')
	}
	return dst
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

// appendStringChecked is [AppendStringChecked] on a byte slice: the quotes
// around [appendStringBodyChecked]. It is one unit over the inlining budget,
// so the hot callers (generated code, appendAny) write the quotes themselves
// and call the body, which keeps a string at one call; this is for the
// exported entry point and the direct path's self-test. Under [ModeV2] a
// string that is not valid UTF-8 is reported as [ErrInvalidUTF8] with dst as
// it was.
func appendStringChecked(dst []byte, src []byte, m StringMode) ([]byte, error) {
	mark := len(dst)
	dst, err := appendStringBodyChecked(append(dst, '"'), src, m)
	if err != nil {
		return dst[:mark], err
	}
	return append(dst, '"'), nil
}

// appendStringBodyChecked is [appendStringChecked] without the quotes: the
// caller has written the opening quote, as the tail of a longer literal in a
// generated encoder, and writes the closing one as the head of the next.
// Under [ModeV2], the mode the direct path writes, the body is here rather
// than behind a further call: a string costs the one call from the caller,
// as it did when the quotes were part of it. Invalid UTF-8 is reported as
// [ErrInvalidUTF8] with dst as it was, the opening quote included, so the
// caller's buffer is left exactly as the failed member found it. The other
// modes are handed on.
func appendStringBodyChecked(dst []byte, src []byte, m StringMode) ([]byte, error) {
	switch m {
	case ModeStream:
		return appendQuotedStream(dst, src, false), nil
	case ModeHTML, ModePlain:
		return appendQuoted(dst, src, m == ModeHTML, false), nil
	case ModeV2HTML:
		return appendQuotedV2HTML(dst, src, false), nil
	}
	mark := len(dst)
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
			// holds nothing that needs escaping. A two byte sequence is
			// settled here without the call, and the words after it are
			// taken whole while they are accented Latin text (see
			// swarLatin), which would otherwise stop the scan at every
			// letter; the first word without a non-ASCII byte hands back
			// to the scan above.
			if b-0xC2 < 0x1E && i+1 < len(src) && src[i+1]&0xC0 == 0x80 {
				i += 2
				if i < len(src) && src[i] >= utf8.RuneSelf {
					// A dense run (Cyrillic, Greek): skipNonASCII takes
					// it a word at a time.
					if i = skipNonASCII(src, i); i < 0 {
						return dst[:mark], ErrInvalidUTF8
					}
					continue
				}
				for i+8 <= len(src) {
					w := binary.LittleEndian.Uint64(src[i:])
					if w&swarHi == 0 || swarUnsafe(w) != 0 || !swarLatin(w) {
						break
					}
					i += 8
				}
				continue
			}
			// A lone four byte sequence (an emoji among ASCII) likewise: F0
			// needs a second byte of 90-BF, F4 one of 80-8F, F1-F3 any.
			if b-0xF0 < 5 && i+3 < len(src) && src[i+1]&0xC0 == 0x80 && src[i+2]&0xC0 == 0x80 && src[i+3]&0xC0 == 0x80 &&
				(b != 0xF0 || src[i+1] >= 0x90) && (b != 0xF4 || src[i+1] < 0x90) {
				i += 4
				continue
			}
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
	return append(dst, src[start:]...), nil
}

// AppendStringBodyChecked is [AppendStringChecked] without the quotes, for
// generated code that folds a string member's quotes into the member name
// literal before it and the one after it. Under [ModeV2] it reports
// [ErrInvalidUTF8] with dst unchanged.
func AppendStringBodyChecked(dst []byte, s string, m StringMode) ([]byte, error) {
	return appendStringBodyChecked(dst, unsafe.Slice(unsafe.StringData(s), len(s)), m)
}

// appendQuotedV2HTML is the ModeV2HTML implementation: [appendQuotedStream]'s
// escaping, plus what encoding/json's reformat adds to it under
// PreserveRawStrings, EscapeForHTML and EscapeForJS. That reformat walks the
// literal rune by rune, escapes '<', '>', '&', U+2028 and U+2029, and copies
// everything else as it is, an invalid byte included, so this does the same:
// a non-ASCII run is validated in place by skipNonASCII and searched for the
// two line separators, and a run it refuses is copied one byte at a time.
//
// quoted says whether to write the surrounding quotes (see appendQuoted).
func appendQuotedV2HTML(dst []byte, src []byte, quoted bool) []byte {
	if quoted {
		dst = append(dst, '"')
	}
	start := 0
	for i := 0; i < len(src); {
		for i+8 <= len(src) {
			w := binary.LittleEndian.Uint64(src[i:])
			if m := swarUnsafe(w) | swarHasByte(w, '<') | swarHasByte(w, '>') | swarHasByte(w, '&') | w&swarHi; m != 0 {
				i += swarIndex(m)
				goto found
			}
			i += 8
		}
		for i < len(src) && htmlSafeSet[src[i]] {
			i++
		}
	found:
		if i >= len(src) {
			break
		}
		b := src[i]
		if b >= utf8.RuneSelf {
			// A two byte sequence followed by ASCII, then words of
			// accented Latin text, are settled as in appendStringChecked;
			// neither can hold a line separator.
			if b-0xC2 < 0x1E && i+1 < len(src) && src[i+1]&0xC0 == 0x80 && (i+2 >= len(src) || src[i+2] < utf8.RuneSelf) {
				i += 2
				for i+8 <= len(src) {
					w := binary.LittleEndian.Uint64(src[i:])
					if w&swarHi == 0 || swarUnsafe(w)|swarHasByte(w, '<')|swarHasByte(w, '>')|swarHasByte(w, '&') != 0 || !swarLatin(w) {
						break
					}
					i += 8
				}
				continue
			}
			// A lone four byte sequence (an emoji among ASCII) likewise: F0
			// needs a second byte of 90-BF, F4 one of 80-8F, F1-F3 any.
			if b-0xF0 < 5 && i+3 < len(src) && src[i+1]&0xC0 == 0x80 && src[i+2]&0xC0 == 0x80 && src[i+3]&0xC0 == 0x80 &&
				(b != 0xF0 || src[i+1] >= 0x90) && (b != 0xF4 || src[i+1] < 0x90) {
				i += 4
				continue
			}
			j := skipNonASCII(src, i)
			if j < 0 {
				// Not UTF-8 somewhere in this run. The reformat decodes
				// rune by rune, so a line separator before the bad byte
				// is still escaped, and the bad byte itself stays in the
				// pending copy. The rest of the run is settled here, not
				// by scanning it again from the next rune on.
				for i < len(src) && src[i] >= utf8.RuneSelf {
					r, size := utf8.DecodeRune(src[i:])
					if r == 0x2028 || r == 0x2029 {
						dst = append(dst, src[start:i]...)
						dst = append(dst, '\\', 'u', '2', '0', '2', hexDigits[r&0xF])
						start = i + size
					}
					i += size
				}
				continue
			}
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
		if b == '<' || b == '>' || b == '&' {
			dst = append(dst, '\\', 'u', '0', '0', hexDigits[b>>4], hexDigits[b&0xF])
		} else {
			dst = appendEscape(dst, b)
		}
		i++
		start = i
	}
	dst = append(dst, src[start:]...)
	if quoted {
		dst = append(dst, '"')
	}
	return dst
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

package odjsonrt

import (
	"bytes"
	"encoding"
	"encoding/base64"
	"encoding/json"
	"math/bits"
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

// ParseStringCached is [ParseString] with a string cache: the result is
// carved from c's slab, which may be nil, and not interned (see
// [StringCache]).
func ParseStringCached(data []byte, p int, c *StringCache) (string, int, error) {
	if uint(p) >= uint(len(data)) {
		return "", p, errUnexpectedEnd(p)
	}
	if data[p] != '"' {
		return "", p, ErrType(data, p, "string")
	}
	if uint(p+9) <= uint(len(data)) {
		if end := shortString(load64(data, p+1), p); end > 0 {
			return c.alloc(data[p+1 : end-1]), end, nil
		}
	}
	end, hasEscape, nonASCII, err := scanString(data, p)
	if err != nil {
		return "", end, err
	}
	body := data[p+1 : end-1]
	if !hasEscape {
		// Invalid UTF-8 becomes U+FFFD below, which is unquote's job.
		if !nonASCII || utf8.Valid(body) {
			return c.alloc(body), end, nil
		}
	}
	s, ok := c.unquoteString(body, false)
	if !ok {
		return "", p, ErrSyntax(data, p, "invalid string literal")
	}
	return s, end, nil
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
	if uint(p) >= uint(len(data)) {
		return nil, false, p, errUnexpectedEnd(p)
	}
	if data[p] != '"' {
		return nil, false, p, ErrType(data, p, "string")
	}
	if uint(p+9) <= uint(len(data)) {
		if end := shortString(load64(data, p+1), p); end > 0 {
			return data[p+1 : end-1], true, end, nil
		}
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
	if uint(p) >= uint(len(data)) {
		return nil, false, p, errUnexpectedEnd(p)
	}
	if data[p] != '"' {
		return nil, false, p, errChar(data, p, "looking for beginning of object key string")
	}
	end := shortName(data, p)
	if end > 0 {
		key, aliased = data[p+1:end-1], true
	} else if key, aliased, end, err = ParseStringBytes(data, p); err != nil {
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
	if uint(p) < uint(len(data)) && data[p] == ':' {
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
	if uint(p+2) < uint(len(data)) && data[p] == ':' {
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
	if uint(p) >= uint(len(data)) {
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
// shape, an optional minus sign and up to nineteen digits with nothing after
// them, whose value fits an int64: nineteen digits cannot overflow the
// accumulator, so the one range test at the end is all that stands between
// the digits and the value. Anything else, including a fraction, an
// exponent, a leading zero followed by more digits or a magnitude beyond
// int64, is left to the general path, which also produces the right error.
func ParseDecimal(data []byte, p int) (v int64, end int, ok bool) {
	i := p
	neg := uint(i) < uint(len(data)) && data[i] == '-'
	if neg {
		i++
	}
	start := i
	var u uint64
	// The first eight bytes as one word: the digits among them are found
	// and folded together, which settles most integers in one step where
	// the loop below took one per digit. A number of more than eight
	// digits, or one near the end of the input, continues in the loop.
	if uint(i+8) <= uint(len(data)) {
		var n int
		if u, n = wordDigits(load64(data, i)); n == 0 {
			return 0, p, false
		}
		i += n
	}
	for uint(i) < uint(len(data)) {
		c := data[i] - '0'
		if c > 9 {
			break
		}
		u = u*10 + uint64(c)
		i++
	}
	n := i - start
	if n == 0 || n > 19 || (data[start] == '0' && n > 1) {
		return 0, p, false
	}
	if uint(i) < uint(len(data)) && (data[i] == '.' || data[i] == 'e' || data[i] == 'E') {
		return 0, p, false
	}
	if neg {
		// 1<<63 is the one magnitude the negative side has and the
		// positive side has not; -int64(1<<63) wraps to math.MinInt64,
		// which is the value.
		if u > 1<<63 {
			return 0, p, false
		}
		return -int64(u), i, true
	}
	if u > 1<<63-1 {
		return 0, p, false
	}
	return int64(u), i, true
}

// wordDigits reads the run of decimal digits at the start of the word w, the
// eight bytes of a document loaded little-endian, and returns their value
// and their number: 8 when every byte is a digit, and 0 when the first is
// not. The digits move to the top lanes and the rest are zero, which fold
// to the value of the digits alone; a shift by the word's width is zero,
// so n = 0 folds to 0. Spelled into the generated decoders, with the load
// and the tests around it, it cost the small payload's row more than the
// call it saved: its integers are eight and ten digits, which the word
// does not settle, and the byte loop then read them a second time.
func wordDigits(w uint64) (u uint64, n int) {
	t := w ^ digitZeros
	nz := (t + 0x7676767676767676 | t) & 0x8080808080808080
	if nz == 0 {
		return fold8(t), 8
	}
	n = bits.TrailingZeros64(nz) >> 3
	return fold8(t << (8 * uint(8-n))), n
}

// ParseUnsigned is [ParseDecimal] for an unsigned integer: no sign, and up to
// twenty digits, the width of a uint64. The first nineteen are accumulated
// without a check, which they cannot overflow; only a twentieth digit is
// tested against what the accumulator has room for.
func ParseUnsigned(data []byte, p int) (v uint64, end int, ok bool) {
	i := p
	var u uint64
	// The first eight bytes as one word, as ParseDecimal reads them.
	if uint(i+8) <= uint(len(data)) {
		var n int
		if u, n = wordDigits(load64(data, i)); n == 0 {
			return 0, p, false
		}
		i += n
	}
	for uint(i) < uint(len(data)) && i-p < 19 {
		c := data[i] - '0'
		if c > 9 {
			break
		}
		u = u*10 + uint64(c)
		i++
	}
	n := i - p
	if n == 0 || (data[p] == '0' && n > 1) {
		return 0, p, false
	}
	if uint(i) < uint(len(data)) {
		if c := data[i] - '0'; c <= 9 {
			// The twentieth digit, and the only step that can overflow.
			if n < 19 || u > (1<<64-1-uint64(c))/10 {
				return 0, p, false
			}
			u = u*10 + uint64(c)
			i++
			if uint(i) < uint(len(data)) && data[i]-'0' <= 9 {
				return 0, p, false
			}
		}
	}
	if uint(i) < uint(len(data)) && (data[i] == '.' || data[i] == 'e' || data[i] == 'E') {
		return 0, p, false
	}
	return u, i, true
}

// ParseUint parses the JSON number at p into an unsigned integer of the given
// bit size (8, 16, 32 or 64). A negative, fractional or out of range literal
// produces a [TypeError].
func ParseUint(data []byte, p int, bits int) (uint64, int, error) {
	// ParseUnsigned takes no sign, so "-0" takes the general path, where
	// strconv rejects the sign even before a zero and produces that error.
	if v, end, ok := ParseUnsigned(data, p); ok && (bits == 64 || v <= 1<<bits-1) {
		return v, end, nil
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

// ParseNumberString parses the value at p into a json.Number. A JSON number is
// taken verbatim; a JSON string is unescaped and accepted when its content is
// itself a valid JSON number, which is what encoding/json does for json.Number
// fields. Any other value, or a string that is not a number, is a [TypeError].
func ParseNumberString(data []byte, p int) (string, int, error) {
	if uint(p) >= uint(len(data)) {
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
	if uint(p) >= uint(len(data)) {
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
	if uint(p) >= uint(len(data)) {
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
	if uint(p) < uint(len(data)) && data[p] == 'n' && isNull(data, p) {
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
	if uint(p) >= uint(len(data)) {
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

// ParseAny decodes the value at p the way encoding/json decodes into an
// interface{}: objects become map[string]any, arrays []any, numbers float64,
// strings string, booleans bool and null nil. The decoder rejects documents
// nested deeper than [MaxDepth].
func ParseAny(data []byte, p int) (any, int, error) {
	return parseAny(data, p, nil, false, true)
}

// ParseAnyCached is [ParseAny] with a string cache for the member names and
// string values it produces; c may be nil.
func ParseAnyCached(data []byte, p int, c *StringCache) (any, int, error) {
	return parseAny(data, p, c, false, true)
}

// anyEntry is a member of an object the any decoder has open: its name,
// its value, and where the name stands, for the duplicate error.
type anyEntry struct {
	key    string
	val    any
	keyPos int
}

// anyState is what the any decoder carries through its recursion: the
// document, the cache, the two rule flags, and the two scratch stacks — the
// members of the objects it has open and the elements of the arrays —
// from which each container is made once, at its close, sized exactly. It
// lives on parseAny's frame and is reached through a pointer, so that a
// push is a store into it and never a typed copy through the write
// barrier; the stacks themselves are the cache's, kept from one document
// to the next, and given back at the end.
type anyState struct {
	data    []byte
	sc      *StringCache
	entries []anyEntry
	anys    []any
	strict  bool
	legacy  bool
}

// parseAny is the implementation of [ParseAny], [ParseAnyWith] and
// [ParseAnyStrict]. sc, when not nil, interns the member names the result
// holds; the values are carved from its slab without the table.
//
// Two flags say which rules apply to strings and object names. legacy is
// encoding/json: invalid UTF-8 becomes U+FFFD and the last of two equal
// names wins. strict is encoding/json/v2 on unvalidated input: invalid
// UTF-8 and duplicate names are errors. Neither is input a jsontext.Decoder
// has validated, where nothing can occur that needs checking. They are two
// booleans rather than one mode so that [ParseAnyV2] stays a single call
// the compiler inlines into generated code.
//
// The decoder is recursive, a function per kind of container, the way
// go-json's is, rather than one loop over a stack of frames: the loop kept
// every container's state behind a pointer into the stack and attached
// each value through it, which cost the `generic` row twice what the
// recursion does. The depth is bounded by MaxDepth, as the stack was.
func parseAny(data []byte, p int, sc *StringCache, strict, legacy bool) (any, int, error) {
	st := anyState{data: data, sc: sc, strict: strict, legacy: legacy}
	var sl *slab
	if sc != nil {
		if sl = sc.slab; sl == nil {
			sl = new(slab)
			sc.slab = sl
		}
		st.entries, st.anys = sl.entries, sl.anys
	}
	eb, ab := len(st.entries), len(st.anys)
	v, p, err := st.value(p, 0)
	if err != nil {
		// The containers left open hold the values decoded so far.
		clear(st.entries[eb:])
		clear(st.anys[ab:])
	}
	if sl != nil {
		sl.entries, sl.anys = st.entries[:eb], st.anys[:ab]
	}
	return v, p, err
}

// value decodes the value at p, depth containers down.
func (st *anyState) value(p, depth int) (any, int, error) {
	data := st.data
	if uint(p) >= uint(len(data)) {
		return nil, p, errUnexpectedEnd(p)
	}
	switch c := data[p]; c {
	case '{':
		return st.object(p, depth)
	case '[':
		return st.array(p, depth)
	case '"':
		var s string
		var next int
		var err error
		switch {
		case st.legacy:
			s, next, err = ParseStringCached(data, p, st.sc)
		case st.strict:
			s, next, err = ParseStringStrict(data, p, st.sc)
		default:
			s, next, err = ParseStringWith(data, p, st.sc)
		}
		if err != nil {
			return nil, next, err
		}
		return st.sc.boxString(s), next, nil
	case 't':
		if !isTrue(data, p) {
			return nil, p, errBeginValue(data, p)
		}
		return true, p + 4, nil
	case 'f':
		if !isFalse(data, p) {
			return nil, p, errBeginValue(data, p)
		}
		return false, p + 5, nil
	case 'n':
		if !isNull(data, p) {
			return nil, p, errBeginValue(data, p)
		}
		return nil, p + 4, nil
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
			return smallAny[i], next, nil
		}
		return st.sc.boxFloat(f), next, nil
	}
}

// object decodes the object at p. Its members are pushed to the entry
// stack as they are read, and the map is made when the object closes,
// with room for all of them: a map filled a member at a time grows and
// rehashes on the way, which cost more than the members did. The
// duplicate error is raised at that insert rather than when the name is
// read: a lookup then and an insert later would hash the name twice, and
// the insert alone says whether the name was new. A filter per object
// that settled it at the name, the way UnknownName does, cost the generic
// row 3.5%; so where an object holds both a repeated name and a later
// fault, the fault is the error here and the name is jsontext's — a
// difference in which error, never in whether.
func (st *anyState) object(p, depth int) (any, int, error) {
	data := st.data
	if depth >= MaxDepth {
		return nil, p, ErrSyntax(data, p, "exceeded max depth")
	}
	p = SkipSpace(data, p+1)
	if uint(p) < uint(len(data)) && data[p] == '}' {
		return map[string]any{}, p + 1, nil
	}
	base := len(st.entries)
	for {
		kp := p
		key, next, err := parseKeyString(data, p, st.sc, st.strict)
		if err != nil {
			return nil, next, err
		}
		v, next, err := st.value(next, depth+1)
		if err != nil {
			return nil, next, err
		}
		// The entry is written into its slot field by field: an append
		// of the literal builds it on the stack and moves it in through
		// the write barrier's typed copy, a call per member while the
		// collector runs.
		n := len(st.entries)
		if n == cap(st.entries) {
			st.entries = growEntries(st.entries)
		}
		st.entries = st.entries[:n+1]
		e := &st.entries[n]
		e.key, e.val, e.keyPos = key, v, kp
		p = SkipSpace(data, next)
		if uint(p) >= uint(len(data)) {
			return nil, p, errUnexpectedEnd(p)
		}
		switch data[p] {
		case ',':
			p = SkipSpace(data, p+1)
		case '}':
			members := st.entries[base:]
			m := make(map[string]any, len(members))
			for i := range members {
				e := &members[i]
				m[e.key] = e.val
				if st.strict && len(m) != i+1 {
					// The insert found the name already there. The
					// value it replaced was the earlier member's, which
					// no longer matters: the document is refused.
					return nil, e.keyPos, ErrDuplicateName(data, e.keyPos, []byte(e.key))
				}
			}
			// The entries hold the values, which the map now does; a
			// pooled cache must not keep them alive.
			clear(members)
			st.entries = st.entries[:base]
			return m, p + 1, nil
		default:
			return nil, p, errChar(data, p, "after object key:value pair")
		}
	}
}

// array decodes the array at p. Its elements go to the element stack and
// are copied into a slice of their number when it closes: one allocation
// of the exact size, where a slice grown as it is appended to costs its
// growths.
func (st *anyState) array(p, depth int) (any, int, error) {
	data := st.data
	if depth >= MaxDepth {
		return nil, p, ErrSyntax(data, p, "exceeded max depth")
	}
	p = SkipSpace(data, p+1)
	if uint(p) < uint(len(data)) && data[p] == ']' {
		return st.sc.boxSlice([]any{}), p + 1, nil
	}
	base := len(st.anys)
	for {
		v, next, err := st.value(p, depth+1)
		if err != nil {
			return nil, next, err
		}
		st.anys = append(st.anys, v)
		p = SkipSpace(data, next)
		if uint(p) >= uint(len(data)) {
			return nil, p, errUnexpectedEnd(p)
		}
		switch data[p] {
		case ',':
			p = SkipSpace(data, p+1)
		case ']':
			elems := st.anys[base:]
			a := make([]any, len(elems))
			copy(a, elems)
			clear(elems)
			st.anys = st.anys[:base]
			return st.sc.boxSlice(a), p + 1, nil
		default:
			return nil, p, errChar(data, p, "after array element")
		}
	}
}

// growEntries is the growth of the entry stack, kept out of the loop.
//
//go:noinline
func growEntries(entries []anyEntry) []anyEntry {
	return append(entries, anyEntry{})[:len(entries)]
}

// parseKeyString is [ParseKey] returning the member name as a Go string,
// interned through c when it is not nil.
func parseKeyString(data []byte, p int, c *StringCache, strict bool) (string, int, error) {
	// A plain name of up to fifteen bytes and the colon after it are
	// settled here, by the two words shortName reads and AfterName's
	// inline test; that is most names, and it spares them the general
	// path's three calls. The scan there takes the rest.
	if uint(p) < uint(len(data)) && data[p] == '"' {
		if end := shortName(data, p); end > 0 {
			if next := AfterName(data, end); next > 0 {
				return c.Make(data[p+1 : end-1]), next, nil
			}
		}
	}
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
	return unquoteAppend(make([]byte, 0, len(s)+2*utf8.UTFMax), s, strict)
}

// unquoteAppend is [unquote] appending to b, which a caller with a buffer
// to reuse passes; the result is b's array when it fits.
func unquoteAppend(b, s []byte, strict bool) ([]byte, bool) {
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

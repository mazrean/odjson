package odjsonrt

import (
	"bytes"
	"encoding/binary"
	"encoding/json/jsontext"
	"strconv"
	"sync"
	"unicode/utf8"
)

// This file holds the helpers behind the generated UnmarshalJSONFrom methods,
// which drive a [jsontext.Decoder] token by token. Everything the decoder hands
// back has been validated by jsontext: the grammar, the UTF-8 and the
// uniqueness of object names. The helpers here therefore parse without
// re-validating, which is what separates them from their byte oriented
// counterparts in decode.go.

// NextKind reports the kind of the next token in dec without the cost of
// [jsontext.Decoder.PeekKind].
//
// PeekKind is a full state machine step: it invalidates the previous read,
// consumes the delimiter, checks it against the state and caches the result.
// The generated decoders only need to know whether the next byte closes the
// container they are in, and they follow every peek with a Read call that
// performs the same validation anyway. So this looks at the unread buffer
// directly and leaves the checking to that Read. When the buffer holds no
// complete answer, which only happens on a streaming decoder that has run
// dry, it falls back to PeekKind, which fetches more input.
func NextKind(dec *jsontext.Decoder) jsontext.Kind {
	b := dec.UnreadBuffer()
	p := SkipSpace(b, 0)
	if p < len(b) && (b[p] == ',' || b[p] == ':') {
		p = SkipSpace(b, p+1)
	}
	if p < len(b) {
		c := b[p]
		if c == '-' || (c >= '0' && c <= '9') {
			return '0'
		}
		return jsontext.Kind(c)
	}
	return dec.PeekKind()
}

// smallValue is the size below which a value is read out of the decoder
// whole. It is measured on the small benchmark payload: the crossover between
// the two strategies depends on how many bytes a document carries per member,
// and a few kilobytes is where per call overhead has stopped dominating.
const smallValue = 4 << 10

// WholeValue reports whether the generated decoder should read the next value
// out of dec with a single [jsontext.Decoder.ReadValue] and parse the bytes
// itself, instead of driving the decoder member by member.
//
// A call per member costs more than a second scan of the bytes when the value
// is small, so this says yes when the unread buffer is at most a few kilobytes
// and holds a complete value, which is recognisable by the closing bracket at
// its end. A decoder over a byte slice always exposes the whole document, so
// for it the answer is exact. A streaming decoder exposes whatever its last
// read fetched; a chunk that happens to end in a bracket is read whole and
// makes ReadValue fetch the rest of the value, which is correct, only
// buffered rather than streamed.
func WholeValue(dec *jsontext.Decoder) bool {
	b := dec.UnreadBuffer()
	if len(b) > smallValue {
		return false
	}
	i := len(b) - 1
	for i >= 0 && spaceSet[b[i]] {
		i--
	}
	return i >= 0 && (b[i] == '}' || b[i] == ']')
}

// ErrKindFrom reports that the next value in dec has the wrong shape for the
// Go type being decoded into. When the decoder is instead holding a syntax
// error, that error is returned, since it is the real cause.
func ErrKindFrom(dec *jsontext.Decoder, goType string) error {
	k := dec.PeekKind()
	if k == 0 {
		if _, err := dec.ReadValue(); err != nil {
			return err
		}
	}
	return ErrKind(byte(k), goType)
}

// StringCache remembers recently decoded strings so that a value that occurs
// many times in a document, or across documents, is allocated once.
//
// encoding/json/v2 keeps the same kind of cache inside its decoder state, and
// a decoder that lacks one allocates a string per member where json/v2 does
// not. The cache is a direct mapped table keyed by a hash of the string's
// first and last bytes, so a lookup costs the same for every length.
type StringCache struct {
	s [stringCacheSize]string
	// valid marks the entries known to be UTF-8, which is what
	// [StringCache.MakeUTF8] stores and what it can then skip checking. An
	// entry [StringCache.Make] stored is not marked: a decoder run with
	// jsontext.AllowInvalidUTF8 hands it bytes nobody has validated.
	valid [stringCacheSize / 64]uint64
	// names is scratch for the strict struct decoders: the unknown member
	// names of every object currently open, back to back, so that the
	// duplicate check on them allocates nothing and costs no zeroing per
	// object. See [UnknownNames]. It is held through a pointer, allocated
	// on first use, so that StringCache stays a comparable type.
	names *[][]byte
}

const stringCacheSize = 256

// UnknownNames hands a strict struct decoder the list it appends its
// object's unknown member names to, and the index its own names start at:
// the entries before it belong to the objects enclosing this one. A nil
// cache yields a nil list, which append then allocates.
func UnknownNames(c *StringCache) (names [][]byte, mark int) {
	if c == nil {
		return nil, 0
	}
	if c.names == nil {
		c.names = new([][]byte)
	}
	return *c.names, len(*c.names)
}

// AddUnknownName appends name to the list an object took from
// [UnknownNames] and publishes the result through c, so that an object
// nested in a later member starts its own names after this one, in the same
// backing array, rather than over it. Only a member the struct does not
// know reaches this, so the store is off the path every member takes.
func AddUnknownName(c *StringCache, names [][]byte, name []byte) [][]byte {
	names = append(names, name)
	if c != nil {
		*c.names = names
	}
	return names
}

// EndUnknownNames gives the list back once the object is closed, with this
// object's names dropped. Only a decoder that returns normally calls it; a
// failed decode leaves its names for [PutStringCache] to clear.
func EndUnknownNames(c *StringCache, names [][]byte, mark int) {
	if c != nil {
		// The names alias the document; clearing them keeps a pooled
		// cache from holding on to it.
		clear(names[mark:])
		*c.names = names[:mark]
	}
}

var stringCachePool sync.Pool

// GetStringCache returns a cache from an internal pool. Generated
// UnmarshalJSONFrom methods take one for the duration of a decode and hand it
// back with [PutStringCache].
func GetStringCache() *StringCache {
	if v := stringCachePool.Get(); v != nil {
		return v.(*StringCache)
	}
	return new(StringCache)
}

// PutStringCache returns a cache obtained from [GetStringCache].
func PutStringCache(c *StringCache) {
	if c.names != nil && len(*c.names) > 0 {
		// A decoder that failed left the names of its open objects, and
		// they alias its document.
		clear(*c.names)
		*c.names = (*c.names)[:0]
	}
	stringCachePool.Put(c)
}

// Make returns b as a string, reusing an earlier result when the cache holds
// an equal string. A nil cache simply allocates.
func (c *StringCache) Make(b []byte) string {
	i, ok := c.slot(b)
	if !ok {
		return string(b)
	}
	if s := c.s[i]; s == string(b) {
		return s
	}
	s := string(b)
	c.s[i] = s
	c.valid[i/64] &^= 1 << (i % 64)
	return s
}

// MakeUTF8 is [Make] for bytes that have to be valid UTF-8 and have not been
// checked yet. A hit on an entry this method stored is known to be valid, so
// it settles the question without a second pass; a miss pays for the
// validation once. ok is false when b is not UTF-8, and nothing is stored.
func (c *StringCache) MakeUTF8(b []byte) (s string, ok bool) {
	i, cacheable := c.slot(b)
	if cacheable {
		if s := c.s[i]; s == string(b) {
			if c.valid[i/64]&(1<<(i%64)) != 0 {
				return s, true
			}
			if !utf8.Valid(b) {
				return "", false
			}
			c.valid[i/64] |= 1 << (i % 64)
			return s, true
		}
	}
	if !utf8.Valid(b) {
		return "", false
	}
	s = string(b)
	if cacheable {
		c.s[i] = s
		c.valid[i/64] |= 1 << (i % 64)
	}
	return s, true
}

// MakeValid is [Make] for bytes the caller has already validated as UTF-8:
// the entry is marked valid, so a later [StringCache.MakeUTF8] hit on it
// settles without a check of its own.
func (c *StringCache) MakeValid(b []byte) string {
	i, ok := c.slot(b)
	if !ok {
		return string(b)
	}
	if s := c.s[i]; s == string(b) {
		c.valid[i/64] |= 1 << (i % 64)
		return s
	}
	s := string(b)
	c.s[i] = s
	c.valid[i/64] |= 1 << (i % 64)
	return s
}

// slot returns the table index for b, or false when b is not cached: a nil
// cache, a string too short to be worth it or too long to keep.
func (c *StringCache) slot(b []byte) (uint64, bool) {
	const (
		minCached = 2   // one byte strings are interned by the runtime already
		maxCached = 256 // long enough for identifiers, hashes and URLs
	)
	if c == nil || len(b) < minCached || len(b) > maxCached {
		return 0, false
	}
	var h uint64
	switch {
	case len(b) >= 8:
		h = binary.LittleEndian.Uint64(b) ^ binary.LittleEndian.Uint64(b[len(b)-8:])*0x9e3779b97f4a7c15
	case len(b) >= 4:
		h = uint64(binary.LittleEndian.Uint32(b)) ^ uint64(binary.LittleEndian.Uint32(b[len(b)-4:]))*0x9e3779b97f4a7c15
	default:
		h = uint64(binary.LittleEndian.Uint16(b)) ^ uint64(binary.LittleEndian.Uint16(b[len(b)-2:]))*0x9e3779b97f4a7c15
	}
	h ^= uint64(len(b)) * 0xff51afd7ed558ccd
	return (h >> 32) % stringCacheSize, true
}

// ParseStringValue decodes the complete JSON value val, as returned by
// [jsontext.Decoder.ReadValue], as a Go string. It is a [TypeError] when val
// is not a string.
func ParseStringValue(val []byte, c *StringCache) (string, error) {
	if len(val) < 2 || val[0] != '"' {
		return "", ErrType(val, 0, "string")
	}
	body := val[1 : len(val)-1]
	if bytes.IndexByte(body, '\\') < 0 {
		return c.Make(body), nil
	}
	out, ok := unquote(body, false)
	if !ok {
		return "", ErrSyntax(val, 0, "invalid string literal")
	}
	return adoptString(out, false), nil
}

// ParseStringWith is [ParseStringTrusted] with a string cache.
func ParseStringWith(data []byte, p int, c *StringCache) (string, int, error) {
	if p >= len(data) {
		return "", p, errUnexpectedEnd(p)
	}
	if data[p] != '"' {
		return "", p, ErrType(data, p, "string")
	}
	if uint(p+9) <= uint(len(data)) {
		if end := shortString(load64(data, p+1), p); end > 0 {
			return c.Make(data[p+1 : end-1]), end, nil
		}
	}
	end, hasEscape, _, err := scanString(data, p)
	if err != nil {
		return "", end, err
	}
	body := data[p+1 : end-1]
	if !hasEscape {
		return c.Make(body), end, nil
	}
	out, ok := unquote(body, false)
	if !ok {
		return "", p, ErrSyntax(data, p, "invalid string literal")
	}
	return adoptString(out, false), end, nil
}

// ParseFloatValue decodes the complete JSON value val as a float of the given
// bit size. The grammar has been checked by the decoder, so only the kind and
// the range are.
func ParseFloatValue(val []byte, bits int) (float64, error) {
	if len(val) == 0 || (val[0] != '-' && (val[0] < '0' || val[0] > '9')) {
		return 0, ErrType(val, 0, floatTypeName(bits))
	}
	v, err := strconv.ParseFloat(asString(val), floatBits(bits))
	if err != nil {
		return 0, &TypeError{Value: "number", Type: floatTypeName(bits)}
	}
	return v, nil
}

// ParseAnyWith is [ParseAny] for input a [jsontext.Decoder] has validated,
// with a string cache for the member names and string values it produces.
func ParseAnyWith(data []byte, p int, c *StringCache) (any, int, error) {
	return parseAny(data, p, c, false, false)
}

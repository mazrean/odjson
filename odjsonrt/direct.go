//go:build !odjson_safe

package odjsonrt

import (
	jsonv1 "encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"unsafe"
)

// The direct path.
//
// encoding/json/v2's own codecs do not go through jsontext's public API. They
// append into the encoder's buffer and read from the decoder's buffer, and
// they update the two state machines by hand, through an internal export that
// the linker refuses to let anyone else reach. A MarshalerTo or UnmarshalerFrom
// implemented with WriteValue and ReadValue instead pays, on every object
// member, a duplicate-name namespace insert, a whitespace scan and a second
// pass over every string, and bench/ab shows that floor alone is above what
// reflection costs divided by 1.3.
//
// This file reaches the same state through reflect-computed field offsets and
// unsafe. It is deliberately narrow: only a buffered encoder or decoder (no
// io.Writer or io.Reader), and only one whose option flags are exactly those
// of a plain json.Marshal / json.Unmarshal, from either encoding/json/v2 or
// encoding/json. Under those two flag words the state machine's rules for one
// value are the same at every depth: a value may not stand where an object
// name is due, it is preceded by ':' after a name and by ',' after an earlier
// member or element, and it counts as one more element of the innermost
// object or array. So the value can be a top-level one, an element of a slice
// or map, or a struct field, and the state after it is the state before it
// plus one. Everything else takes the public API path, which stays correct on
// its own.
//
// It is guarded three times over. The layout is looked up by field name and
// checked by type at init, and any mismatch disables it. It is only enabled
// on the Go minor version it was verified against. And before it is used, a
// probe runs the whole path through json/v2 and encoding/json, at the top
// level and nested, and compares the result with the public API's; a
// difference disables it. Building with the odjson_safe tag removes it
// altogether. The generated code carries the public API path in every case,
// so disabling costs speed and nothing else.

// directLayout holds everything the direct path learned at init.
type directLayout struct {
	ok bool

	encBuf, encLast, encWr, encFlags             uintptr
	decBuf, decPrevEnd, decPrevStart, decPeekPos uintptr
	decLast, decRd, decFlags                     uintptr
	encDefaultFlags, decDefaultFlags             [2]uint64
	// encV1Flags is the flag word encoding/json's Marshal carries, and v1
	// says whether it was learned. The generated method then writes
	// ModeV2HTML, which is what that call's reformat would have made of
	// the value.
	encV1Flags [2]uint64
	v1         bool
}

var direct directLayout

// directReason says which check disabled the direct path, for the tests and
// for whoever widens verifiedGoMinors; empty when it is enabled.
var directReason string

func directFail(why string) directLayout {
	directReason = why
	return directLayout{}
}

func init() {
	direct = calibrateDirect()
}

// jsontext's stateEntry, as far as the direct path reads it: the top bit says
// object rather than array, the next two are the namespace's disabled and
// invalid marks, and the rest counts the names and values written so far.
// The probes below check every one of these against the real coder.
const (
	stateTypeObject     uint64 = 1 << 63
	stateInvalidNS      uint64 = 1 << 61
	stateCountMask      uint64 = 1<<61 - 1
	directMaxStackDepth        = 10000
)

// needName reports whether the state expects an object member name next.
func needName(e uint64) bool { return e&(stateTypeObject|1) == stateTypeObject }

// needValue reports whether the state expects an object member value next.
func needValue(e uint64) bool { return e&(stateTypeObject|1) == stateTypeObject|1 }

func u64At(p unsafe.Pointer, off uintptr) *uint64      { return (*uint64)(unsafe.Add(p, off)) }
func bytesAt(p unsafe.Pointer, off uintptr) *[]byte    { return (*[]byte)(unsafe.Add(p, off)) }
func intAt(p unsafe.Pointer, off uintptr) *int         { return (*int)(unsafe.Add(p, off)) }
func ifaceAt(p unsafe.Pointer, off uintptr) *any       { return (*any)(unsafe.Add(p, off)) }
func flagsAt(p unsafe.Pointer, off uintptr) *[2]uint64 { return (*[2]uint64)(unsafe.Add(p, off)) }

// fieldOffset walks t by field name, through embedded structs, and returns the
// leaf's offset and type.
func fieldOffset(t reflect.Type, path ...string) (uintptr, reflect.Type, bool) {
	var off uintptr
	for _, name := range path {
		if t.Kind() != reflect.Struct {
			return 0, nil, false
		}
		f, ok := t.FieldByName(name)
		if !ok {
			return 0, nil, false
		}
		for _, i := range f.Index {
			sf := t.Field(i)
			off += sf.Offset
			t = sf.Type
		}
	}
	return off, t, true
}

// want looks a field up and checks that it has the expected shape.
func want(t reflect.Type, kind reflect.Kind, elem reflect.Kind, path ...string) (uintptr, bool) {
	off, ft, ok := fieldOffset(t, path...)
	if !ok || ft.Kind() != kind {
		return 0, false
	}
	if kind == reflect.Slice && ft.Elem().Kind() != elem {
		return 0, false
	}
	return off, true
}

// wantFlags checks the jsonopts.Struct.Flags shape: two uint64 words.
func wantFlags(t reflect.Type, path ...string) (uintptr, bool) {
	off, ft, ok := fieldOffset(t, path...)
	if !ok || ft.Kind() != reflect.Struct || ft.NumField() != 2 || ft.Size() != 16 {
		return 0, false
	}
	for i := range 2 {
		if ft.Field(i).Type.Kind() != reflect.Uint64 {
			return 0, false
		}
	}
	return off, true
}

// verifiedGoMinors names the Go minor versions whose jsontext this file's
// layout has been checked against. Adding one is what turns the direct path on
// for a new release, and it must never be done blind: re-read jsontext's
// source for that release, then let .github/workflows/go-minor.yml — or the
// same steps by hand — prove the layout and the self-tests still hold on that
// toolchain. The declaration stays on one line; the workflow rewrites it.
var verifiedGoMinors = []string{"go1.27"}

// goMinorVerified reports whether v, a runtime.Version() string, names one of
// them. The spellings are "go1.27", "go1.27.1" and "go1.27rc1", so whatever
// follows the minor has to be anything but another digit: a bare prefix test
// would also accept a later "go1.270".
func goMinorVerified(v string) bool {
	for _, m := range verifiedGoMinors {
		if rest, ok := strings.CutPrefix(v, m); ok && (rest == "" || rest[0] < '0' || rest[0] > '9') {
			return true
		}
	}
	return false
}

func calibrateDirect() (l directLayout) {
	// The layout below is that of the jsontext those releases ship. A later
	// one has to be re-verified before it joins them.
	if !goMinorVerified(runtime.Version()) {
		return directFail("go minor not verified")
	}
	encT := reflect.TypeFor[jsontext.Encoder]()
	decT := reflect.TypeFor[jsontext.Decoder]()
	var ok [11]bool
	l.encBuf, ok[0] = want(encT, reflect.Slice, reflect.Uint8, "s", "Buf")
	l.encLast, ok[1] = want(encT, reflect.Uint64, 0, "s", "Tokens", "Last")
	l.encWr, ok[2] = want(encT, reflect.Interface, 0, "s", "wr")
	l.encFlags, ok[3] = wantFlags(encT, "s", "Flags")
	l.decBuf, ok[4] = want(decT, reflect.Slice, reflect.Uint8, "s", "buf")
	l.decPrevStart, ok[5] = want(decT, reflect.Int, 0, "s", "prevStart")
	l.decPrevEnd, ok[6] = want(decT, reflect.Int, 0, "s", "prevEnd")
	l.decPeekPos, ok[7] = want(decT, reflect.Int, 0, "s", "peekPos")
	l.decLast, ok[8] = want(decT, reflect.Uint64, 0, "s", "Tokens", "Last")
	l.decRd, ok[9] = want(decT, reflect.Interface, 0, "s", "rd")
	l.decFlags, ok[10] = wantFlags(decT, "s", "Flags")
	for _, o := range ok {
		if !o {
			return directFail("layout")
		}
	}

	// Learn the option flags a plain Marshal / Unmarshal carries, and check
	// the state machine's shape around one value, from inside the real
	// calls, through the public API: at the top level, as the second
	// element of an array and as the value of an object member.
	const doc = `{"a":[1,"b",null]}`
	p := &directProbe{l: &l}
	probe = p
	defer func() { probe = nil }()
	out, err := jsonv2.Marshal(directProbeV{})
	if err != nil || string(out) != doc || !p.encOK {
		return directFail("top-level marshal probe")
	}
	if err := jsonv2.Unmarshal([]byte(" "+doc+" "), &directProbeV{}); err != nil || !p.decOK {
		return directFail("top-level unmarshal probe")
	}
	l.encDefaultFlags, l.decDefaultFlags = p.encFlags, p.decFlags
	// Nested, the state before the value must read as an array with one
	// element, then as an object with a name pending, and the value must
	// add exactly one to the count in both cases.
	p.reset()
	if out, err := jsonv2.Marshal([]any{1, directProbeV{}}); err != nil || string(out) != "[1,"+doc+"]" || !p.encOK ||
		p.encBefore != 1 || p.encAfter != 2 || p.encFlags != l.encDefaultFlags {
		return directFail("nested array marshal probe")
	}
	p.reset()
	if out, err := jsonv2.Marshal(map[string]directProbeV{"k": {}}); err != nil || string(out) != `{"k":`+doc+`}` || !p.encOK ||
		!needValue(p.encBefore) || p.encAfter != p.encBefore+1 || p.encFlags != l.encDefaultFlags {
		return directFail("nested map marshal probe")
	}
	p.reset()
	if err := jsonv2.Unmarshal([]byte("[1, "+doc+"]"), &[]directProbeV{}); err != nil || !p.decNestedOK || !p.decPeeked ||
		p.decBefore != 1 || p.decAfter != 2 || p.decFlags != l.decDefaultFlags {
		return directFail("nested array unmarshal probe")
	}
	p.reset()
	if err := jsonv2.Unmarshal([]byte(`{"k": `+doc+`}`), &map[string]directProbeV{}); err != nil || !p.decNestedOK || p.decPeeked ||
		!needValue(p.decBefore) || p.decAfter != p.decBefore+1 || p.decFlags != l.decDefaultFlags {
		return directFail("nested map unmarshal probe")
	}
	// encoding/json's Marshal calls the same method with its own flag word.
	// The value it makes of the bytes is checked by the self-test below;
	// here only the word is recorded. Its decode is left to the public
	// path: those flags allow what the strict parsers refuse.
	p.reset()
	if out, err := jsonv1.Marshal(directProbeV{}); err == nil && string(out) == doc && p.encOK && p.encFlags != l.encDefaultFlags {
		l.encV1Flags, l.v1 = p.encFlags, true
	}

	// Now run the direct path itself and require the same answers as the
	// public one, including the checks json/v2 makes after the call, at the
	// top level and nested, and the public path's refusals.
	l.ok = true
	direct = l
	defer func() { direct = directLayout{} }()
	f := &directFast{}
	if out, err := jsonv2.Marshal(f); err != nil || string(out) != doc || f.took != 1 {
		return directFail("top-level marshal")
	}
	if err := jsonv2.Unmarshal([]byte(" "+doc+" \n"), f); err != nil || f.took != 2 {
		return directFail("top-level unmarshal")
	}
	if err := jsonv2.Unmarshal([]byte(doc+" x"), f); err == nil {
		return directFail("trailing data accepted")
	}
	if err := jsonv2.Unmarshal([]byte(doc[:5]), f); err == nil {
		return directFail("truncated input accepted")
	}
	type outer struct {
		A []*directFast          `json:"a"`
		M map[string]*directFast `json:"m"`
		V directFast             `json:"v"`
	}
	f.took = 0
	nested := `{"a":[` + doc + `,` + doc + `],"m":{"k":` + doc + `},"v":` + doc + `}`
	if out, err := jsonv2.Marshal(outer{A: []*directFast{f, f}, M: map[string]*directFast{"k": f}, V: *f}); err != nil ||
		string(out) != nested || f.took != 3 {
		return directFail("nested marshal")
	}
	f.took = 0
	var o outer
	if err := jsonv2.Unmarshal([]byte(strings.ReplaceAll(nested, ",", " , ")), &o); err != nil || f.took != 0 ||
		len(o.A) != 2 || o.A[0].took != 1 || o.A[1].took != 1 || o.M["k"].took != 1 || o.V.took != 1 {
		return directFail("nested unmarshal")
	}
	// Malformed nested input is refused, by the public path or by the
	// parser, never accepted.
	for _, bad := range []string{`[` + doc + doc + `]`, `[` + doc + `,]`, `{"k"` + doc + `}`, `[` + doc[:5] + `]`, `[` + doc + `,` + doc + `,` + doc[:9] + `]`} {
		var a []*directFast
		if err := jsonv2.Unmarshal([]byte(bad), &a); err == nil {
			return directFail("malformed array accepted")
		}
		var m map[string]*directFast
		if err := jsonv2.Unmarshal([]byte(bad), &m); err == nil {
			return directFail("malformed map accepted")
		}
	}
	if l.v1 {
		// encoding/json's own reformat of what the public path writes is
		// the reference for ModeV2HTML; the streaming encoder never takes
		// the direct path, so it still produces that reference.
		v := struct {
			S string `json:"s"`
		}{"<&> \u2028\xff\u2029\xe2\x80 \xffé日 <\u2028"}
		want, err := jsonv1.Marshal(jsonv1.RawMessage(append(appendQuotedStream([]byte(`{"s":`), []byte(v.S)), '}')))
		if err != nil {
			return directFail("v1 reference")
		}
		s := &directString{S: v.S}
		got, err := jsonv1.Marshal(s)
		if err != nil || string(got) != string(want) || s.took != 1 {
			why := "v1 top-level marshal: got " + string(got) + ", want " + string(want) + ", took " + strconv.Itoa(s.took)
			if err != nil {
				why += ", err " + err.Error()
			}
			return directFail(why)
		}
		s.took = 0
		if got, err := jsonv1.Marshal([]*directString{s, s}); err != nil || string(got) != "["+string(want)+","+string(want)+"]" || s.took != 2 {
			return directFail("v1 nested marshal")
		}
	}
	return l
}

// directProbe records, from inside json/v2's own calls, what the coder looks
// like before and after a single value. directProbeV is the type whose
// methods are called; it records into probe, so that the same probe sees the
// value wherever the caller allocates it.
type directProbe struct {
	l                   *directLayout
	encBefore, encAfter uint64
	decBefore, decAfter uint64
	encFlags, decFlags  [2]uint64
	encOK, decOK        bool
	decNestedOK         bool
	decPeeked           bool
}

var probe *directProbe

type directProbeV struct{}

func (p *directProbe) reset() {
	*p = directProbe{l: p.l}
}

func (directProbeV) MarshalJSONTo(enc *jsontext.Encoder) error {
	p := probe
	base := unsafe.Pointer(enc)
	p.encBefore = *u64At(base, p.l.encLast)
	p.encFlags = *flagsAt(base, p.l.encFlags)
	wrNil := *ifaceAt(base, p.l.encWr) == nil
	if err := enc.WriteValue([]byte(`{"a":[1,"b",null]}`)); err != nil {
		return err
	}
	p.encAfter = *u64At(base, p.l.encLast)
	p.encOK = wrNil && p.encAfter == p.encBefore+1
	return nil
}

func (*directProbeV) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	p := probe
	base := unsafe.Pointer(dec)
	p.decBefore = *u64At(base, p.l.decLast)
	p.decFlags = *flagsAt(base, p.l.decFlags)
	rdNil := *ifaceAt(base, p.l.decRd) == nil
	peek := *intAt(base, p.l.decPeekPos)
	prevEnd := *intAt(base, p.l.decPrevEnd)
	unread := len(dec.UnreadBuffer())
	v, err := dec.ReadValue()
	if err != nil {
		return err
	}
	p.decAfter = *u64At(base, p.l.decLast)
	buf := *bytesAt(base, p.l.decBuf)
	// The decoder over a byte slice exposes the whole input, and after the
	// value its read offset sits right behind it.
	p.decOK = rdNil && peek == 0 && dec.StackDepth() == 0 && p.decAfter == p.decBefore+1 &&
		unread == len(buf) && *intAt(base, p.l.decPrevEnd) == len(buf)-1 &&
		*intAt(base, p.l.decPrevStart) == len(buf)-1-len(v)
	// Nested, the unread buffer starts at the previous value's end. When
	// the caller has peeked (a slice element), peekPos names the value's
	// first byte, past the delimiter; when it has not (a map value), the
	// whitespace and the delimiter are still ahead. Afterwards the value's
	// bounds are recorded and the cached peek is gone.
	pos := peek
	if pos <= 0 {
		pos = SkipSpace(buf, prevEnd)
		if pos < len(buf) && (buf[pos] == ':' || buf[pos] == ',') {
			pos = SkipSpace(buf, pos+1)
		}
	}
	p.decPeeked = peek > 0
	p.decNestedOK = rdNil && pos < len(buf) && buf[pos] == '{' && unread == len(buf)-prevEnd &&
		p.decAfter == p.decBefore+1 && *intAt(base, p.l.decPeekPos) == 0 &&
		*intAt(base, p.l.decPrevStart) == pos && *intAt(base, p.l.decPrevEnd) == pos+len(v)
	return nil
}

// directFast exercises the Begin/End pairs the way generated code does, and
// counts how often the direct path was taken.
type directFast struct{ took int }

func (f *directFast) MarshalJSONTo(enc *jsontext.Encoder) error {
	if buf, m, ok := BeginDirectEncodeMode(enc); ok && m == ModeV2 {
		f.took++
		EndDirectEncode(enc, append(buf, `{"a":[1,"b",null]}`...))
		return nil
	}
	return enc.WriteValue([]byte(`{"a":[1,"b",null]}`))
}

func (f *directFast) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	if data, p, ok := BeginDirectDecodeAt(dec); ok {
		f.took++
		p = SkipSpace(data, p)
		end, err := SkipValueStrict(data, p)
		if err != nil {
			return err
		}
		EndDirectDecodeAt(dec, p, end)
		return nil
	}
	_, err := dec.ReadValue()
	return err
}

// directString writes one string member under whatever mode the direct path
// selects, to check ModeV2HTML against encoding/json's reformat.
type directString struct {
	S    string
	took int
}

func (d *directString) MarshalJSONTo(enc *jsontext.Encoder) error {
	if buf, m, ok := BeginDirectEncodeMode(enc); ok {
		if m == ModeV2HTML {
			d.took++
		}
		buf, err := AppendStringChecked(append(buf, `{"s":`...), d.S, m)
		if err != nil {
			return err
		}
		EndDirectEncode(enc, append(buf, '}'))
		return nil
	}
	return enc.WriteValue(append(appendQuotedStream([]byte(`{"s":`), []byte(d.S)), '}'))
}

// BeginDirectEncodeMode reports whether the next value can be appended
// straight into enc's buffer, and returns that buffer with any delimiter the
// value needs already in it, together with the [StringMode] the value must
// be written in: [ModeV2] under a plain encoding/json/v2 Marshal, [ModeV2HTML]
// under a plain encoding/json Marshal. It says yes at any depth, for a value
// that is not an object member name, on a buffered encoder carrying exactly
// one of those two calls' options; the caller appends the value and hands
// the result to [EndDirectEncode].
func BeginDirectEncodeMode(enc *jsontext.Encoder) ([]byte, StringMode, bool) {
	if !direct.ok {
		return nil, 0, false
	}
	p := unsafe.Pointer(enc)
	m := ModeV2
	switch *flagsAt(p, direct.encFlags) {
	case direct.encDefaultFlags:
	case direct.encV1Flags:
		if !direct.v1 {
			return nil, 0, false
		}
		m = ModeV2HTML
	default:
		return nil, 0, false
	}
	if *ifaceAt(p, direct.encWr) != nil {
		return nil, 0, false
	}
	e := *u64At(p, direct.encLast)
	depth := enc.StackDepth()
	if needName(e) || e&stateInvalidNS != 0 || depth >= directMaxStackDepth {
		return nil, 0, false
	}
	buf := *bytesAt(p, direct.encBuf)
	switch {
	case needValue(e):
		buf = append(buf, ':')
	case e&stateCountMask != 0 && depth != 0:
		buf = append(buf, ',')
	}
	return buf, m, true
}

// BeginDirectEncode is [BeginDirectEncodeMode] restricted to a top-level
// value under encoding/json/v2's options, the only case generated code before
// odjson 0.3 knew.
func BeginDirectEncode(enc *jsontext.Encoder) ([]byte, bool) {
	if enc.StackDepth() != 0 {
		return nil, false
	}
	buf, m, ok := BeginDirectEncodeMode(enc)
	if !ok || m != ModeV2 {
		return nil, false
	}
	return buf, true
}

// EndDirectEncode stores the buffer a [BeginDirectEncodeMode] caller appended
// a complete value to, and advances the encoder past that value.
func EndDirectEncode(enc *jsontext.Encoder, buf []byte) {
	p := unsafe.Pointer(enc)
	*bytesAt(p, direct.encBuf) = buf
	*u64At(p, direct.encLast)++
}

// BeginDirectDecodeAt reports whether the next value can be parsed straight
// out of dec's buffer. It returns the whole input and the offset in it at
// which the value starts, past any whitespace and the delimiter before it,
// so that an offset in any error the caller reports is the document's. It
// says yes at any depth, for a value that is not an object member name, on
// a buffered decoder that json.Unmarshal created with no options, and only
// when the delimiter is the one the state machine expects: anything else is
// left to the public path, which reports it. The caller parses one value
// from that offset and reports its end to [EndDirectDecodeAt]. The bytes
// have not been validated by anyone: the caller must reject what jsontext
// would have, invalid UTF-8 and duplicate names included.
func BeginDirectDecodeAt(dec *jsontext.Decoder) ([]byte, int, bool) {
	buf, _, pos, ok := beginDirectDecode(dec)
	return buf, pos, ok
}

// beginDirectDecode is the check behind [BeginDirectDecodeAt]; it also
// returns the end of the previous value, where the unread input begins.
func beginDirectDecode(dec *jsontext.Decoder) (buf []byte, base, pos int, ok bool) {
	if !direct.ok {
		return nil, 0, 0, false
	}
	p := unsafe.Pointer(dec)
	if *flagsAt(p, direct.decFlags) != direct.decDefaultFlags || *ifaceAt(p, direct.decRd) != nil {
		return nil, 0, 0, false
	}
	e := *u64At(p, direct.decLast)
	depth := dec.StackDepth()
	if needName(e) || e&stateInvalidNS != 0 || depth >= directMaxStackDepth {
		return nil, 0, 0, false
	}
	buf = *bytesAt(p, direct.decBuf)
	base = *intAt(p, direct.decPrevEnd)
	pos = *intAt(p, direct.decPeekPos)
	switch {
	case pos < 0:
		// A cached peek error: the public path reports it.
		return nil, 0, 0, false
	case pos > 0:
		// The caller peeked, so the decoder has already consumed the
		// whitespace and the delimiter and checked the latter.
	default:
		// Consume what the decoder would: whitespace, then the delimiter
		// the state calls for, then whitespace.
		pos = SkipSpace(buf, base)
		var delim byte
		if pos < len(buf) && (buf[pos] == ',' || buf[pos] == ':') {
			delim = buf[pos]
			pos = SkipSpace(buf, pos+1)
		}
		var want byte
		switch {
		case needValue(e):
			want = ':'
		case e&stateCountMask != 0 && depth != 0:
			want = ','
		}
		if delim != want {
			return nil, 0, 0, false
		}
	}
	// The value has to start here; a closing bracket, or nothing, is the
	// public path's to report.
	if pos >= len(buf) || buf[pos] == '}' || buf[pos] == ']' {
		return nil, 0, 0, false
	}
	return buf, base, pos, true
}

// BeginDirectDecode is [BeginDirectDecodeAt] restricted to a top-level value,
// the only case generated code before odjson 0.3 knew. The returned input
// begins with whatever whitespace precedes the value.
func BeginDirectDecode(dec *jsontext.Decoder) ([]byte, bool) {
	if dec.StackDepth() != 0 {
		return nil, false
	}
	buf, base, _, ok := beginDirectDecode(dec)
	if !ok {
		return nil, false
	}
	return buf[base:], true
}

// EndDirectDecodeAt advances dec past the value a [BeginDirectDecodeAt]
// caller parsed, which started at offset start and ended at offset end of
// the returned input.
func EndDirectDecodeAt(dec *jsontext.Decoder, start, end int) {
	p := unsafe.Pointer(dec)
	*intAt(p, direct.decPrevStart) = start
	*intAt(p, direct.decPrevEnd) = end
	*intAt(p, direct.decPeekPos) = 0
	*u64At(p, direct.decLast)++
}

// EndDirectDecode is [EndDirectDecodeAt] for a [BeginDirectDecode] caller,
// whose offsets count from the beginning of the input that call returned.
func EndDirectDecode(dec *jsontext.Decoder, end int) {
	base := *intAt(unsafe.Pointer(dec), direct.decPrevEnd)
	EndDirectDecodeAt(dec, base, base+end)
}

// DirectEnabled reports whether the direct path is active in this binary.
func DirectEnabled() bool { return direct.ok }

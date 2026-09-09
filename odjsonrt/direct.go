//go:build !odjson_safe

package odjsonrt

import (
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"reflect"
	"runtime"
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
// unsafe. It is deliberately narrow: only a top-level value, only an encoder
// or decoder that json.Marshal / json.Unmarshal created with no options, and
// only a buffered one (no io.Writer or io.Reader). In that situation the
// state machine before the value is known exactly, the state after it is a
// constant, and nothing else in the coder needs to change. Everything else
// takes the public API path, which stays correct on its own.
//
// It is guarded three times over. The layout is looked up by field name and
// checked by type at init, and any mismatch disables it. It is only enabled
// on the Go minor version it was verified against. And before it is used, a
// probe runs the whole path through json/v2 and compares the result with the
// public API's; a difference disables it. Building with the odjson_safe tag
// removes it altogether. The generated code carries the public API path in
// every case, so disabling costs speed and nothing else.

// directLayout holds everything the direct path learned at init.
type directLayout struct {
	ok bool

	encBuf, encLast, encWr, encFlags             uintptr
	decBuf, decPrevEnd, decPrevStart, decPeekPos uintptr
	decLast, decRd, decFlags                     uintptr
	encBefore, encAfter, decBefore, decAfter     uint64
	encDefaultFlags, decDefaultFlags             [2]uint64
}

var direct directLayout

func init() {
	direct = calibrateDirect()
}

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

func calibrateDirect() (l directLayout) {
	// The layout below is that of Go 1.27's jsontext. A later release has to
	// be re-verified before the gate is widened.
	if !strings.HasPrefix(runtime.Version(), "go1.27") && !strings.HasPrefix(runtime.Version(), "go1.27.") {
		return l
	}
	encT := reflect.TypeOf(jsontext.Encoder{})
	decT := reflect.TypeOf(jsontext.Decoder{})
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
			return l
		}
	}

	// Learn the state machine values around one top-level value, and the
	// option flags a plain Marshal / Unmarshal carries, from inside the
	// real calls, through the public API.
	const doc = `{"a":[1,"b",null]}`
	p := &directProbe{l: &l}
	out, err := jsonv2.Marshal(p)
	if err != nil || string(out) != doc || !p.encOK {
		return l
	}
	if err := jsonv2.Unmarshal([]byte(" "+doc+" "), p); err != nil || !p.decOK {
		return l
	}
	l.encBefore, l.encAfter = p.encBefore, p.encAfter
	l.decBefore, l.decAfter = p.decBefore, p.decAfter
	l.encDefaultFlags, l.decDefaultFlags = p.encFlags, p.decFlags

	// Now run the direct path itself and require the same answers as the
	// public one, including the checks json/v2 makes after the call.
	l.ok = true
	direct = l
	defer func() { direct = directLayout{} }()
	f := &directFast{}
	if out, err := jsonv2.Marshal(f); err != nil || string(out) != doc || !f.took {
		return directLayout{}
	}
	f.took = false
	if err := jsonv2.Unmarshal([]byte(" "+doc+" \n"), f); err != nil || !f.took {
		return directLayout{}
	}
	if err := jsonv2.Unmarshal([]byte(doc+" x"), f); err == nil {
		return directLayout{}
	}
	if err := jsonv2.Unmarshal([]byte(doc[:5]), f); err == nil {
		return directLayout{}
	}
	// Nested, the direct path must stand aside.
	f.took = false
	if out, err := jsonv2.Marshal([]*directFast{f}); err != nil || string(out) != "["+doc+"]" || f.took {
		return directLayout{}
	}
	if err := jsonv2.Unmarshal([]byte("["+doc+"]"), &[]*directFast{f}); err != nil || f.took {
		return directLayout{}
	}
	return l
}

// directProbe records, from inside json/v2's own calls, what the coder looks
// like before and after a single top-level value.
type directProbe struct {
	l                   *directLayout
	encBefore, encAfter uint64
	decBefore, decAfter uint64
	encFlags, decFlags  [2]uint64
	encOK, decOK        bool
}

func (p *directProbe) MarshalJSONTo(enc *jsontext.Encoder) error {
	base := unsafe.Pointer(enc)
	p.encBefore = *u64At(base, p.l.encLast)
	p.encFlags = *flagsAt(base, p.l.encFlags)
	wrNil := *ifaceAt(base, p.l.encWr) == nil
	if err := enc.WriteValue([]byte(`{"a":[1,"b",null]}`)); err != nil {
		return err
	}
	p.encAfter = *u64At(base, p.l.encLast)
	p.encOK = wrNil && enc.StackDepth() == 0 && p.encBefore != p.encAfter
	return nil
}

func (p *directProbe) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	base := unsafe.Pointer(dec)
	p.decBefore = *u64At(base, p.l.decLast)
	p.decFlags = *flagsAt(base, p.l.decFlags)
	rdNil := *ifaceAt(base, p.l.decRd) == nil
	peek := *intAt(base, p.l.decPeekPos)
	unread := len(dec.UnreadBuffer())
	v, err := dec.ReadValue()
	if err != nil {
		return err
	}
	p.decAfter = *u64At(base, p.l.decLast)
	buf := *bytesAt(base, p.l.decBuf)
	// The decoder over a byte slice exposes the whole input, and after the
	// value its read offset sits right behind it.
	p.decOK = rdNil && peek == 0 && dec.StackDepth() == 0 && p.decBefore != p.decAfter &&
		unread == len(buf) && *intAt(base, p.l.decPrevEnd) == len(buf)-1 &&
		*intAt(base, p.l.decPrevStart) == len(buf)-1-len(v)
	return nil
}

// directFast exercises BeginDirect* / EndDirect* the way generated code does.
type directFast struct{ took bool }

func (f *directFast) MarshalJSONTo(enc *jsontext.Encoder) error {
	if buf, ok := BeginDirectEncode(enc); ok {
		f.took = true
		EndDirectEncode(enc, append(buf, `{"a":[1,"b",null]}`...))
		return nil
	}
	return enc.WriteValue([]byte(`{"a":[1,"b",null]}`))
}

func (f *directFast) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	if data, ok := BeginDirectDecode(dec); ok {
		f.took = true
		p := SkipSpace(data, 0)
		end, err := SkipValueStrict(data, p)
		if err != nil {
			return err
		}
		EndDirectDecode(dec, end)
		return nil
	}
	_, err := dec.ReadValue()
	return err
}

// BeginDirectEncode reports whether the next value can be appended straight
// into enc's buffer, and returns that buffer. It says yes only for a top-level
// value on a buffered encoder that json.Marshal created with no options; the
// caller appends the value and hands the result to [EndDirectEncode].
func BeginDirectEncode(enc *jsontext.Encoder) ([]byte, bool) {
	if !direct.ok || enc.StackDepth() != 0 {
		return nil, false
	}
	p := unsafe.Pointer(enc)
	if *u64At(p, direct.encLast) != direct.encBefore || *ifaceAt(p, direct.encWr) != nil ||
		*flagsAt(p, direct.encFlags) != direct.encDefaultFlags {
		return nil, false
	}
	return *bytesAt(p, direct.encBuf), true
}

// EndDirectEncode stores the buffer a [BeginDirectEncode] caller appended a
// complete value to, and advances the encoder past that value.
func EndDirectEncode(enc *jsontext.Encoder, buf []byte) {
	p := unsafe.Pointer(enc)
	*bytesAt(p, direct.encBuf) = buf
	*u64At(p, direct.encLast) = direct.encAfter
}

// BeginDirectDecode reports whether the next value can be parsed straight out
// of dec's buffer, and returns the unread input. It says yes only for a
// top-level value on a buffered decoder that json.Unmarshal created with no
// options; the caller parses one value and reports its end to
// [EndDirectDecode]. The bytes have not been validated by anyone: the caller
// must reject what jsontext would have, invalid UTF-8 and duplicate names
// included.
func BeginDirectDecode(dec *jsontext.Decoder) ([]byte, bool) {
	if !direct.ok || dec.StackDepth() != 0 {
		return nil, false
	}
	p := unsafe.Pointer(dec)
	if *u64At(p, direct.decLast) != direct.decBefore || *ifaceAt(p, direct.decRd) != nil ||
		*intAt(p, direct.decPeekPos) != 0 || *flagsAt(p, direct.decFlags) != direct.decDefaultFlags {
		return nil, false
	}
	return dec.UnreadBuffer(), true
}

// EndDirectDecode advances dec past the value a [BeginDirectDecode] caller
// parsed, which ended at offset end of the returned input.
func EndDirectDecode(dec *jsontext.Decoder, end int) {
	p := unsafe.Pointer(dec)
	start := *intAt(p, direct.decPrevEnd)
	*intAt(p, direct.decPrevStart) = start
	*intAt(p, direct.decPrevEnd) = start + end
	*u64At(p, direct.decLast) = direct.decAfter
}

// DirectEnabled reports whether the direct path is active in this binary.
func DirectEnabled() bool { return direct.ok }

package codegen

import (
	"fmt"
	"strconv"

	"github.com/mazrean/odjson/internal/analyzer"
)

// The jsontext driven decoders exist because encoding/json/v2 hands
// UnmarshalJSONFrom a decoder, not bytes. They follow json/v2's own semantics
// rather than encoding/json's: a null zeroes whatever it lands on, an array of
// the wrong length is rejected, and member names match case-sensitively.
//
// Which of the decoder's two read calls is used where is a measured choice:
//
//   - Objects are driven member by member. ReadValue returns the name without
//     copying it, and the value is then read with one more call. Reading a
//     whole object with ReadValue and re-parsing it would make odjson walk
//     the whitespace, names and unknown members a second time, which on an
//     indented document with many unknown members costs more than the calls
//     it saves.
//   - Containers that hold no struct — slices of numbers or strings, arrays,
//     maps of scalars, interface values — are read with a single ReadValue
//     and handed to the byte oriented fragments, in trusted mode. A call per
//     element costs more than odjson's scan of a short array.
//   - The next kind is read straight from the decoder's unread buffer
//     (odjsonrt.NextKind) instead of PeekKind; the Read call that follows
//     validates the same thing, so the peek only has to be a hint.
//   - Unknown members are consumed with ReadValue rather than SkipValue. The
//     two validate identically, but SkipValue walks the value token by token
//     through the state machine.

// cache is the identifier of the string cache threaded through the jsontext
// driven decoders. It plays the role encoding/json/v2's own string cache plays
// for the reflection decoder.
const cache = "sc"

// decodeStructFrom writes the body of a struct's jsontext driven decoder.
func (g *generator) decodeStructFrom(s *analyzer.StructInfo) {
	g.pf("\tvar err error")
	g.pf("\t_ = err")
	// A small value is read whole and handed to the byte oriented decoder:
	// below a few kilobytes the decoder's per call cost outweighs a second
	// scan of the bytes. See odjsonrt.WholeValue for how small is decided.
	g.pf("\tif odjsonrt.WholeValue(dec) {")
	g.pf("\t\tvar val jsontext.Value")
	g.pf("\t\tif val, err = dec.ReadValue(); err != nil {")
	g.pf("\t\t\treturn err")
	g.pf("\t\t}")
	if s.Local {
		g.pf("\t\t_, err = v.odjsonParseV2(val, 0, %s)", cache)
	} else {
		g.pf("\t\t_, err = %sParseV2(val, v, 0, %s)", s.Helper, cache)
	}
	g.pf("\t\treturn err")
	g.pf("\t}")
	g.pf("\tswitch odjsonrt.NextKind(dec) {")
	g.pf("\tcase 'n':")
	g.pf("\t\tif _, err = dec.ReadToken(); err != nil {")
	g.pf("\t\t\treturn err")
	g.pf("\t\t}")
	g.pf("\t\t*v = %s{}", s.Expr)
	g.pf("\t\treturn nil")
	g.pf("\tcase '{':")
	g.pf("\tdefault:")
	g.pf("\t\treturn odjsonrt.ErrKindFrom(dec, %s)", strconv.Quote(s.Expr))
	g.pf("\t}")
	g.pf("\tif _, err = dec.ReadToken(); err != nil {")
	g.pf("\t\treturn err")
	g.pf("\t}")
	g.pf("\tfor odjsonrt.NextKind(dec) != '}' {")
	g.pf("\t\tvar key jsontext.Value")
	g.pf("\t\tkey, err = dec.ReadValue()")
	g.pf("\t\tif err != nil {")
	g.pf("\t\t\treturn err")
	g.pf("\t\t}")
	g.pf("\t\tidx := -1")
	if len(s.Fields) > 0 {
		g.pkg.Imports.Add("bytes", "bytes")
		// Fast path: compare against the member name exactly as it appears
		// in the document, which avoids unescaping it at all.
		g.pf("\t\tswitch string(key) {")
		for i, f := range s.Fields {
			g.pf("\t\tcase %s:", strconv.Quote(jsonString(f.JSONName, false)))
			g.pf("\t\t\tidx = %d", i)
		}
		g.pf("\t\t}")
		// Only the escaped spelling of a name needs unquoting; unlike
		// encoding/json, json/v2 does not fall back to a case-insensitive
		// match, so neither does this decoder.
		g.pf("\t\tif idx < 0 && bytes.IndexByte(key, '\\\\') >= 0 {")
		g.pf("\t\t\tif name, ok := odjsonrt.UnquoteName(key); ok {")
		g.pf("\t\t\t\tswitch string(name) {")
		for i, f := range s.Fields {
			g.pf("\t\t\t\tcase %s:", strconv.Quote(f.JSONName))
			g.pf("\t\t\t\t\tidx = %d", i)
		}
		g.pf("\t\t\t\t}")
		g.pf("\t\t\t}")
		g.pf("\t\t}")
	}
	g.pf("\t\tswitch idx {")
	for i, f := range s.Fields {
		g.pf("\t\tcase %d:", i)
		g.allocSteps(f, 3)
		if f.AsString {
			g.leafFrom(f.Type, selector(f), 3, true)
		} else {
			g.decodeFrom(f.Type, selector(f), 3)
		}
	}
	g.pf("\t\tdefault:")
	g.pf("\t\t\tif _, err = dec.ReadValue(); err != nil {")
	g.pf("\t\t\t\treturn err")
	g.pf("\t\t\t}")
	g.pf("\t\t}")
	g.pf("\t}")
	g.pf("\t_, err = dec.ReadToken()")
	g.pf("\treturn err")
}

// containsStruct reports whether decoding a value of type t involves a struct
// codec somewhere inside it. Such values are driven through the decoder token
// by token; everything else is read whole.
func containsStruct(t *analyzer.Type) bool {
	switch t.Kind {
	case analyzer.KindStruct:
		return true
	case analyzer.KindPointer, analyzer.KindSlice, analyzer.KindArray, analyzer.KindMap:
		return containsStruct(t.Elem)
	}
	return false
}

// scalarFast reports whether t is decoded by one of the runtime's value
// helpers, which parse a complete value the decoder has already validated
// without scanning it again.
func scalarFast(t *analyzer.Type) bool {
	if t.Interface || t.Unmarshaler || t.PtrUnmarshaler || t.TextUnmarshaler || t.PtrTextUnmarshaler {
		return false
	}
	switch t.Kind {
	case analyzer.KindBool, analyzer.KindInt, analyzer.KindUint, analyzer.KindFloat, analyzer.KindString:
		return true
	}
	return false
}

// zeroLit renders the zero value of t, which is what json/v2 stores for null.
func zeroLit(t *analyzer.Type) string {
	switch t.Kind {
	case analyzer.KindBool:
		return "false"
	case analyzer.KindInt, analyzer.KindUint, analyzer.KindFloat:
		return "0"
	case analyzer.KindString, analyzer.KindNumber:
		return `""`
	case analyzer.KindArray, analyzer.KindStruct:
		return t.Expr + "{}"
	case analyzer.KindAny:
		if t.Interface {
			return "nil"
		}
		return "*new(" + t.Expr + ")"
	}
	return "nil"
}

// leafFrom reads one complete value out of the decoder and decodes it. Scalars
// go through the value helpers; anything else is handed to the byte oriented
// emitter in trusted mode, since jsontext has already validated it.
func (g *generator) leafFrom(t *analyzer.Type, target string, n int, quoted bool) {
	val := g.tmp("val")
	g.pf("%svar %s jsontext.Value", ind(n), val)
	g.pf("%s%s, err = dec.ReadValue()", ind(n), val)
	g.pf("%sif err != nil {", ind(n))
	g.pf("%sreturn err", ind(n+1))
	g.pf("%s}", ind(n))
	if !quoted && scalarFast(t) {
		g.scalarFrom(t, target, val, n)
		return
	}
	pos := g.tmp("vp")
	g.pf("%s%s := 0", ind(n), pos)
	c := ctx{data: val, pos: pos, single: true, trusted: true, trimmed: true, v2: true, cache: cache}
	if quoted {
		g.decodeQuoted(t, target, c, n)
	} else {
		g.decode(t, target, c, n)
	}
	// The decoder has already delimited the value, so where the fragment
	// stopped inside it does not matter. Marking the offset used keeps the
	// generated file free of dead stores.
	g.pf("%s_ = %s", ind(n), pos)
}

// scalarFrom decodes the complete value in val into a scalar target. A null
// stores the zero value, as json/v2 does.
func (g *generator) scalarFrom(t *analyzer.Type, target, val string, n int) {
	x := g.tmp("x")
	g.pf("%sif %s[0] == 'n' {", ind(n), val)
	g.pf("%s%s = %s", ind(n+1), target, zeroLit(t))
	g.pf("%s} else {", ind(n))
	var native string
	switch t.Kind {
	case analyzer.KindBool:
		native = "bool"
		g.pf("%svar %s bool", ind(n+1), x)
		g.pf("%s%s, _, err = odjsonrt.ParseBool(%s, 0)", ind(n+1), x, val)
	case analyzer.KindInt:
		native = "int64"
		g.pf("%svar %s int64", ind(n+1), x)
		g.pf("%s%s, _, err = odjsonrt.ParseInt(%s, 0, %d)", ind(n+1), x, val, t.Bits)
	case analyzer.KindUint:
		native = "uint64"
		g.pf("%svar %s uint64", ind(n+1), x)
		g.pf("%s%s, _, err = odjsonrt.ParseUint(%s, 0, %d)", ind(n+1), x, val, t.Bits)
	case analyzer.KindFloat:
		native = "float64"
		g.pf("%svar %s float64", ind(n+1), x)
		g.pf("%s%s, err = odjsonrt.ParseFloatValue(%s, %d)", ind(n+1), x, val, t.Bits)
	case analyzer.KindString:
		native = "string"
		g.pf("%svar %s string", ind(n+1), x)
		g.pf("%s%s, err = odjsonrt.ParseStringValue(%s, %s)", ind(n+1), x, val, cache)
	}
	g.pf("%sif err != nil {", ind(n+1))
	g.pf("%sreturn err", ind(n+2))
	g.pf("%s}", ind(n+1))
	if t.Expr == native {
		g.pf("%s%s = %s", ind(n+1), target, x)
	} else {
		g.pf("%s%s = %s(%s)", ind(n+1), target, t.Expr, x)
	}
	g.pf("%s}", ind(n))
}

// decodeFrom writes the statements decoding one value out of the decoder into
// target.
func (g *generator) decodeFrom(t *analyzer.Type, target string, n int) {
	if !containsStruct(t) {
		g.leafFrom(t, target, n, false)
		return
	}
	switch t.Kind {
	case analyzer.KindPointer:
		g.pf("%sif odjsonrt.NextKind(dec) == 'n' {", ind(n))
		g.pf("%sif _, err = dec.ReadToken(); err != nil {", ind(n+1))
		g.pf("%sreturn err", ind(n+2))
		g.pf("%s}", ind(n+1))
		g.pf("%s%s = nil", ind(n+1), target)
		g.pf("%s} else {", ind(n))
		g.pf("%sif %s == nil {", ind(n+1), target)
		g.pf("%s%s = new(%s)", ind(n+2), target, t.Elem.Expr)
		g.pf("%s}", ind(n+1))
		g.decodeFrom(t.Elem, "(*"+target+")", n+1)
		g.pf("%s}", ind(n))
	case analyzer.KindStruct:
		if t.Struct.Local {
			g.pf("%sif err = %s.odjsonParseFrom(dec, %s); err != nil {", ind(n), target, cache)
		} else {
			g.pf("%sif err = %sParseFrom(dec, %s, %s); err != nil {", ind(n), t.Struct.Helper, addr(target), cache)
		}
		g.pf("%sreturn err", ind(n+1))
		g.pf("%s}", ind(n))
	case analyzer.KindSlice:
		g.sliceFrom(t, target, n)
	case analyzer.KindArray:
		g.arrayFrom(t, target, n)
	case analyzer.KindMap:
		g.mapFrom(t, target, n)
	}
}

// openFrom emits the null and kind checks shared by the container decoders and
// consumes the opening delimiter. A null zeroes the target, as json/v2 does.
func (g *generator) openFrom(open byte, t *analyzer.Type, target string, n int) {
	g.pf("%sswitch odjsonrt.NextKind(dec) {", ind(n))
	g.pf("%scase 'n':", ind(n))
	g.pf("%sif _, err = dec.ReadToken(); err != nil {", ind(n+1))
	g.pf("%sreturn err", ind(n+2))
	g.pf("%s}", ind(n+1))
	g.pf("%s%s = %s", ind(n+1), target, zeroLit(t))
	g.pf("%scase '%c':", ind(n), open)
	g.pf("%sif _, err = dec.ReadToken(); err != nil {", ind(n+1))
	g.pf("%sreturn err", ind(n+2))
	g.pf("%s}", ind(n+1))
}

func (g *generator) sliceFrom(t *analyzer.Type, target string, n int) {
	s, e := g.tmp("s"), g.tmp("e")
	g.openFrom('[', t, target, n)
	g.pf("%s%s := %s[:0]", ind(n+1), s, target)
	g.pf("%sif odjsonrt.NextKind(dec) != ']' && cap(%s) == 0 {", ind(n+1), s)
	g.pf("%s%s = make(%s, 0, 4)", ind(n+2), s, t.Expr)
	g.pf("%s}", ind(n+1))
	g.pf("%sfor odjsonrt.NextKind(dec) != ']' {", ind(n+1))
	g.pf("%svar %s %s", ind(n+2), e, t.Elem.Expr)
	g.decodeFrom(t.Elem, e, n+2)
	g.pf("%s%s = append(%s, %s)", ind(n+2), s, s, e)
	g.pf("%s}", ind(n+1))
	g.pf("%sif _, err = dec.ReadToken(); err != nil {", ind(n+1))
	g.pf("%sreturn err", ind(n+2))
	g.pf("%s}", ind(n+1))
	g.pf("%sif %s == nil {", ind(n+1), s)
	g.pf("%s%s = %s{}", ind(n+2), s, t.Expr)
	g.pf("%s}", ind(n+1))
	g.pf("%s%s = %s", ind(n+1), target, s)
	g.closeFrom(t.Expr, n)
}

func (g *generator) arrayFrom(t *analyzer.Type, target string, n int) {
	i := g.tmp("i")
	g.openFrom('[', t, target, n)
	g.pf("%s%s := 0", ind(n+1), i)
	g.pf("%sfor odjsonrt.NextKind(dec) != ']' {", ind(n+1))
	// json/v2 rejects an array whose length does not match the Go array,
	// where encoding/json silently pads or truncates.
	g.pf("%sif %s >= %d {", ind(n+2), i, t.Len)
	g.pf("%sreturn odjsonrt.ErrArrayLength(%s, true)", ind(n+3), strconv.Quote(t.Expr))
	g.pf("%s}", ind(n+2))
	g.decodeFrom(t.Elem, fmt.Sprintf("%s[%s]", target, i), n+2)
	g.pf("%s%s++", ind(n+2), i)
	g.pf("%s}", ind(n+1))
	g.pf("%sif %s != %d {", ind(n+1), i, t.Len)
	g.pf("%sreturn odjsonrt.ErrArrayLength(%s, false)", ind(n+2), strconv.Quote(t.Expr))
	g.pf("%s}", ind(n+1))
	g.pf("%sif _, err = dec.ReadToken(); err != nil {", ind(n+1))
	g.pf("%sreturn err", ind(n+2))
	g.pf("%s}", ind(n+1))
	g.closeFrom(t.Expr, n)
}

func (g *generator) mapFrom(t *analyzer.Type, target string, n int) {
	m, k, name, ok, key, mv := g.tmp("m"), g.tmp("k"), g.tmp("name"), g.tmp("ok"), g.tmp("key"), g.tmp("mv")
	g.openFrom('{', t, target, n)
	g.pf("%s%s := %s", ind(n+1), m, target)
	g.pf("%sif %s == nil {", ind(n+1), m)
	g.pf("%s%s = make(%s)", ind(n+2), m, t.Expr)
	g.pf("%s}", ind(n+1))
	g.pf("%sfor odjsonrt.NextKind(dec) != '}' {", ind(n+1))
	g.pf("%svar %s jsontext.Value", ind(n+2), k)
	g.pf("%s%s, err = dec.ReadValue()", ind(n+2), k)
	g.pf("%sif err != nil {", ind(n+2))
	g.pf("%sreturn err", ind(n+3))
	g.pf("%s}", ind(n+2))
	g.pf("%s%s, %s := odjsonrt.UnquoteName(%s)", ind(n+2), name, ok, k)
	g.pf("%sif !%s {", ind(n+2), ok)
	g.pf("%sreturn odjsonrt.ErrSyntax(%s, 0, \"invalid object name\")", ind(n+3), k)
	g.pf("%s}", ind(n+2))
	// The name is only valid until the next read, and a streaming decoder
	// deliberately clobbers it then, so it becomes a string before the
	// member value is read.
	g.pf("%s%s := %s", ind(n+2), key, convert(t.Key.Expr, cache+".Make("+name+")"))
	g.pf("%svar %s %s", ind(n+2), mv, t.Elem.Expr)
	g.decodeFrom(t.Elem, mv, n+2)
	g.pf("%s%s[%s] = %s", ind(n+2), m, key, mv)
	g.pf("%s}", ind(n+1))
	g.pf("%sif _, err = dec.ReadToken(); err != nil {", ind(n+1))
	g.pf("%sreturn err", ind(n+2))
	g.pf("%s}", ind(n+1))
	g.pf("%s%s = %s", ind(n+1), target, m)
	g.closeFrom(t.Expr, n)
}

func (g *generator) closeFrom(expr string, n int) {
	g.pf("%sdefault:", ind(n))
	g.pf("%sreturn odjsonrt.ErrKindFrom(dec, %s)", ind(n+1), strconv.Quote(expr))
	g.pf("%s}", ind(n))
}

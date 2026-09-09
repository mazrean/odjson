package codegen

import (
	"fmt"
	"strconv"

	"github.com/mazrean/odjson/internal/analyzer"
)

// Unknown values are consumed with Decoder.ReadValue rather than
// Decoder.SkipValue. The two validate identically — both push an object
// namespace and reject duplicate names — but SkipValue walks the value token by
// token through the state machine, while ReadValue hands it to the raw
// consumer. On a document with many unknown members that is worth a fifth of
// the decode.
//
// The jsontext driven decoders exist because encoding/json/v2 hands
// UnmarshalJSONFrom a decoder, not bytes. Reading the whole value out of it and
// parsing that with the byte oriented decoder means the document is parsed
// twice: once by jsontext to find where the value ends, once by odjson. Driving
// the decoder token by token parses it once, and lets odjson skip the UTF-8
// validation jsontext has already done.

// decodeStructFrom writes the body of a struct's jsontext driven decoder.
func (g *generator) decodeStructFrom(s *analyzer.StructInfo) {
	g.pf("\tvar err error")
	g.pf("\t_ = err")
	g.pf("\tswitch k := dec.PeekKind(); k {")
	g.pf("\tcase 'n':")
	g.pf("\t\t_, err = dec.ReadToken()")
	g.pf("\t\treturn err")
	g.pf("\tcase '{':")
	g.pf("\tdefault:")
	g.pf("\t\treturn odjsonrt.ErrKind(byte(k), %s)", strconv.Quote(s.Expr))
	g.pf("\t}")
	g.pf("\tif _, err = dec.ReadToken(); err != nil {")
	g.pf("\t\treturn err")
	g.pf("\t}")
	g.pf("\tfor dec.PeekKind() != '}' {")
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

// leafFrom reads one complete value out of the decoder and decodes it with the
// byte oriented emitter. jsontext has already validated it, so the fragment
// runs in trusted mode.
func (g *generator) leafFrom(t *analyzer.Type, target string, n int, quoted bool) {
	val, pos := g.tmp("val"), g.tmp("vp")
	g.pf("%svar %s jsontext.Value", ind(n), val)
	g.pf("%s%s, err = dec.ReadValue()", ind(n), val)
	g.pf("%sif err != nil {", ind(n))
	g.pf("%sreturn err", ind(n+1))
	g.pf("%s}", ind(n))
	g.pf("%s%s := 0", ind(n), pos)
	c := ctx{data: val, pos: pos, single: true, trusted: true, trimmed: true}
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

// decodeFrom writes the statements decoding one value out of the decoder into
// target.
func (g *generator) decodeFrom(t *analyzer.Type, target string, n int) {
	switch t.Kind {
	case analyzer.KindPointer:
		g.pf("%sif dec.PeekKind() == 'n' {", ind(n))
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
			g.pf("%sif err = %s.odjsonParseFrom(dec); err != nil {", ind(n), target)
		} else {
			g.pf("%sif err = %sParseFrom(dec, %s); err != nil {", ind(n), t.Struct.Helper, addr(target))
		}
		g.pf("%sreturn err", ind(n+1))
		g.pf("%s}", ind(n))
	case analyzer.KindSlice:
		g.sliceFrom(t, target, n)
	case analyzer.KindArray:
		g.arrayFrom(t, target, n)
	case analyzer.KindMap:
		g.mapFrom(t, target, n)
	default:
		g.leafFrom(t, target, n, false)
	}
}

// openFrom emits the null and kind checks shared by the container decoders and
// consumes the opening delimiter.
func (g *generator) openFrom(open byte, expr, target string, n int, nullTarget bool) {
	g.pf("%sswitch k := dec.PeekKind(); k {", ind(n))
	g.pf("%scase 'n':", ind(n))
	g.pf("%sif _, err = dec.ReadToken(); err != nil {", ind(n+1))
	g.pf("%sreturn err", ind(n+2))
	g.pf("%s}", ind(n+1))
	if nullTarget {
		g.pf("%s%s = nil", ind(n+1), target)
	}
	g.pf("%scase '%c':", ind(n), open)
	g.pf("%sif _, err = dec.ReadToken(); err != nil {", ind(n+1))
	g.pf("%sreturn err", ind(n+2))
	g.pf("%s}", ind(n+1))
}

func (g *generator) sliceFrom(t *analyzer.Type, target string, n int) {
	s, e := g.tmp("s"), g.tmp("e")
	g.openFrom('[', t.Expr, target, n, true)
	g.pf("%s%s := %s[:0]", ind(n+1), s, target)
	g.pf("%sif dec.PeekKind() != ']' && cap(%s) == 0 {", ind(n+1), s)
	g.pf("%s%s = make(%s, 0, 4)", ind(n+2), s, t.Expr)
	g.pf("%s}", ind(n+1))
	g.pf("%sfor dec.PeekKind() != ']' {", ind(n+1))
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
	g.openFrom('[', t.Expr, target, n, false)
	g.pf("%s%s := 0", ind(n+1), i)
	g.pf("%sfor dec.PeekKind() != ']' {", ind(n+1))
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
	m, k, name, ok, mv := g.tmp("m"), g.tmp("k"), g.tmp("name"), g.tmp("ok"), g.tmp("mv")
	g.openFrom('{', t.Expr, target, n, true)
	g.pf("%s%s := %s", ind(n+1), m, target)
	g.pf("%sif %s == nil {", ind(n+1), m)
	g.pf("%s%s = make(%s)", ind(n+2), m, t.Expr)
	g.pf("%s}", ind(n+1))
	g.pf("%sfor dec.PeekKind() != '}' {", ind(n+1))
	g.pf("%svar %s jsontext.Value", ind(n+2), k)
	g.pf("%s%s, err = dec.ReadValue()", ind(n+2), k)
	g.pf("%sif err != nil {", ind(n+2))
	g.pf("%sreturn err", ind(n+3))
	g.pf("%s}", ind(n+2))
	g.pf("%s%s, %s := odjsonrt.UnquoteName(%s)", ind(n+2), name, ok, k)
	g.pf("%sif !%s {", ind(n+2), ok)
	g.pf("%sreturn odjsonrt.ErrSyntax(%s, 0, \"invalid object name\")", ind(n+3), k)
	g.pf("%s}", ind(n+2))
	g.pf("%svar %s %s", ind(n+2), mv, t.Elem.Expr)
	g.decodeFrom(t.Elem, mv, n+2)
	g.pf("%s%s[%s] = %s", ind(n+2), m, convert(t.Key.Expr, "string("+name+")"), mv)
	g.pf("%s}", ind(n+1))
	g.pf("%sif _, err = dec.ReadToken(); err != nil {", ind(n+1))
	g.pf("%sreturn err", ind(n+2))
	g.pf("%s}", ind(n+1))
	g.pf("%s%s = %s", ind(n+1), target, m)
	g.closeFrom(t.Expr, n)
}

func (g *generator) closeFrom(expr string, n int) {
	g.pf("%sdefault:", ind(n))
	g.pf("%sreturn odjsonrt.ErrKind(byte(k), %s)", ind(n+1), strconv.Quote(expr))
	g.pf("%s}", ind(n))
}

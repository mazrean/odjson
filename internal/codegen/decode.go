package codegen

import (
	"fmt"
	"strconv"

	"github.com/mazrean/odjson/internal/analyzer"
)

func (g *generator) decErr(c ctx, n int) {
	g.pf("%sif err != nil {", ind(n))
	g.pf("%sreturn %s, err", ind(n+1), c.ret)
	g.pf("%s}", ind(n))
}

func (g *generator) decodeStruct(s *analyzer.StructInfo) {
	c := ctx{data: "data", pos: "p", ret: "p"}
	g.pf("\tvar err error")
	g.pf("\t_ = err")
	g.pf("\tp = odjsonrt.SkipSpace(data, p)")
	g.pf("\tif np, ok := odjsonrt.ParseNull(data, p); ok {")
	g.pf("\t\treturn np, nil")
	g.pf("\t}")
	g.pf("\tif p >= len(data) || data[p] != '{' {")
	g.pf("\t\treturn p, odjsonrt.ErrType(data, p, %s)", strconv.Quote(s.Expr))
	g.pf("\t}")
	g.pf("\tp++")
	g.pf("\tp = odjsonrt.SkipSpace(data, p)")
	g.pf("\tif p < len(data) && data[p] == '}' {")
	g.pf("\t\treturn p + 1, nil")
	g.pf("\t}")
	g.pf("\tfor {")
	g.pf("\t\tvar key []byte")
	g.pf("\t\tp = odjsonrt.SkipSpace(data, p)")
	g.pf("\t\tkey, _, p, err = odjsonrt.ParseKey(data, p)")
	g.pf("\t\tif err != nil {")
	g.pf("\t\t\treturn p, err")
	g.pf("\t\t}")
	g.pf("\t\tp = odjsonrt.SkipSpace(data, p)")
	g.pf("\t\tidx := -1")
	if len(s.Fields) > 0 {
		g.pf("\t\tswitch string(key) {")
		for i, f := range s.Fields {
			g.pf("\t\tcase %s:", strconv.Quote(f.JSONName))
			g.pf("\t\t\tidx = %d", i)
		}
		g.pf("\t\t}")
		if g.opts.CaseInsensitive {
			g.pf("\t\tif idx < 0 {")
			g.pf("\t\t\tswitch {")
			for i, f := range s.Fields {
				g.pf("\t\t\tcase odjsonrt.EqualFold(key, %s):", strconv.Quote(f.JSONName))
				g.pf("\t\t\t\tidx = %d", i)
			}
			g.pf("\t\t\t}")
			g.pf("\t\t}")
		}
	}
	g.pf("\t\tswitch idx {")
	for i, f := range s.Fields {
		g.pf("\t\tcase %d:", i)
		g.allocSteps(f, 3)
		if f.AsString {
			g.decodeQuoted(f.Type, selector(f), c, 3)
		} else {
			g.decode(f.Type, selector(f), c, 3)
		}
	}
	g.pf("\t\tdefault:")
	g.pf("\t\t\tp, err = odjsonrt.SkipValue(data, p)")
	g.pf("\t\t\tif err != nil {")
	g.pf("\t\t\t\treturn p, err")
	g.pf("\t\t\t}")
	g.pf("\t\t}")
	g.pf("\t\tp = odjsonrt.SkipSpace(data, p)")
	g.pf("\t\tif p >= len(data) {")
	g.pf("\t\t\treturn p, odjsonrt.ErrSyntax(data, p, \"unexpected end of JSON input\")")
	g.pf("\t\t}")
	g.pf("\t\tswitch data[p] {")
	g.pf("\t\tcase ',':")
	g.pf("\t\t\tp++")
	g.pf("\t\tcase '}':")
	g.pf("\t\t\treturn p + 1, nil")
	g.pf("\t\tdefault:")
	g.pf("\t\t\treturn p, odjsonrt.ErrSyntax(data, p, \"after object key:value pair\")")
	g.pf("\t\t}")
	g.pf("\t}")
}

// allocSteps materialises the embedded pointers on the path to f.
func (g *generator) allocSteps(f *analyzer.Field, n int) {
	path := "v"
	for _, s := range f.Steps {
		path += "." + s.Name
		if s.Ptr {
			g.pf("%sif %s == nil {", ind(n), path)
			g.pf("%s%s = new(%s)", ind(n+1), path, s.Elem)
			g.pf("%s}", ind(n))
		}
	}
}

// decodeQuoted implements the ",string" tag option on the decoding side.
func (g *generator) decodeQuoted(t *analyzer.Type, target string, c ctx, n int) {
	if t.Kind == analyzer.KindPointer {
		np, ok := g.tmp("np"), g.tmp("ok")
		g.pf("%s%s = odjsonrt.SkipSpace(%s, %s)", ind(n), c.pos, c.data, c.pos)
		g.pf("%sif %s, %s := odjsonrt.ParseNull(%s, %s); %s {", ind(n), np, ok, c.data, c.pos, ok)
		g.pf("%s%s = nil", ind(n+1), target)
		g.pf("%s%s = %s", ind(n+1), c.pos, np)
		g.pf("%s} else {", ind(n))
		g.pf("%sif %s == nil {", ind(n+1), target)
		g.pf("%s%s = new(%s)", ind(n+2), target, t.Elem.Expr)
		g.pf("%s}", ind(n+1))
		g.decodeQuoted(t.Elem, "(*"+target+")", c, n+1)
		g.pf("%s}", ind(n))
		return
	}

	inner, sp, np, ok := g.tmp("inner"), g.tmp("sp"), g.tmp("np"), g.tmp("ok")
	g.pf("%s%s = odjsonrt.SkipSpace(%s, %s)", ind(n), c.pos, c.data, c.pos)
	g.pf("%sif %s, %s := odjsonrt.ParseNull(%s, %s); %s {", ind(n), np, ok, c.data, c.pos, ok)
	g.pf("%s%s = %s", ind(n+1), c.pos, np)
	g.pf("%s} else {", ind(n))
	g.pf("%svar %s []byte", ind(n+1), inner)
	g.pf("%s%s, %s, err = odjsonrt.ParseStringInner(%s, %s)", ind(n+1), inner, c.pos, c.data, c.pos)
	g.decErr(c, n+1)
	g.pf("%s%s := 0", ind(n+1), sp)
	sub := ctx{data: inner, pos: sp, ret: c.ret}
	g.decode(t, target, sub, n+1)
	g.pf("%sif err = odjsonrt.EndOfDocument(%s, %s); err != nil {", ind(n+1), inner, sp)
	g.pf("%sreturn %s, err", ind(n+2), c.ret)
	g.pf("%s}", ind(n+1))
	g.pf("%s}", ind(n))
}

// nullZero reports whether decoding a JSON null into the type resets it,
// matching encoding/json's literalStore.
func nullZero(t *analyzer.Type) bool {
	switch t.Kind {
	case analyzer.KindPointer, analyzer.KindSlice, analyzer.KindBytes,
		analyzer.KindMap:
		return true
	case analyzer.KindAny:
		return t.Interface
	default:
		return false
	}
}

// decode writes the statements parsing one JSON value into target.
func (g *generator) decode(t *analyzer.Type, target string, c ctx, n int) {
	switch t.Kind {
	case analyzer.KindRawMessage:
		raw := g.tmp("raw")
		g.pf("%s%s = odjsonrt.SkipSpace(%s, %s)", ind(n), c.pos, c.data, c.pos)
		g.pf("%svar %s []byte", ind(n), raw)
		g.pf("%s%s, %s, err = odjsonrt.ParseRaw(%s, %s)", ind(n), raw, c.pos, c.data, c.pos)
		g.decErr(c, n)
		g.pf("%s%s = append(%s[:0], %s...)", ind(n), target, target, raw)
		return
	case analyzer.KindPointer:
		np, ok := g.tmp("np"), g.tmp("ok")
		g.pf("%s%s = odjsonrt.SkipSpace(%s, %s)", ind(n), c.pos, c.data, c.pos)
		g.pf("%sif %s, %s := odjsonrt.ParseNull(%s, %s); %s {", ind(n), np, ok, c.data, c.pos, ok)
		g.pf("%s%s = nil", ind(n+1), target)
		g.pf("%s%s = %s", ind(n+1), c.pos, np)
		g.pf("%s} else {", ind(n))
		g.pf("%sif %s == nil {", ind(n+1), target)
		g.pf("%s%s = new(%s)", ind(n+2), target, t.Elem.Expr)
		g.pf("%s}", ind(n+1))
		g.decode(t.Elem, "(*"+target+")", c, n+1)
		g.pf("%s}", ind(n))
		return
	}

	if !t.Interface {
		switch {
		case t.Unmarshaler || t.PtrUnmarshaler:
			g.pf("%s%s, err = odjsonrt.ParseUnmarshaler(%s, %s, %s)", ind(n), c.pos, c.data, c.pos, addr(target))
			g.decErr(c, n)
			return
		case t.TextUnmarshaler || t.PtrTextUnmarshaler:
			// encoding/json never hands a null to UnmarshalText; it leaves
			// the value alone. json.Unmarshaler, in contrast, does receive
			// the literal null, so only this branch skips it.
			np, ok := g.tmp("np"), g.tmp("ok")
			g.pf("%s%s = odjsonrt.SkipSpace(%s, %s)", ind(n), c.pos, c.data, c.pos)
			g.pf("%sif %s, %s := odjsonrt.ParseNull(%s, %s); %s {", ind(n), np, ok, c.data, c.pos, ok)
			g.pf("%s%s = %s", ind(n+1), c.pos, np)
			g.pf("%s} else {", ind(n))
			g.pf("%s%s, err = odjsonrt.ParseTextUnmarshaler(%s, %s, %s)", ind(n+1), c.pos, c.data, c.pos, addr(target))
			g.decErr(c, n+1)
			g.pf("%s}", ind(n))
			return
		}
	}

	g.pf("%s%s = odjsonrt.SkipSpace(%s, %s)", ind(n), c.pos, c.data, c.pos)
	body := n
	np, ok := g.tmp("np"), g.tmp("ok")
	g.pf("%sif %s, %s := odjsonrt.ParseNull(%s, %s); %s {", ind(n), np, ok, c.data, c.pos, ok)
	g.pf("%s%s = %s", ind(n+1), c.pos, np)
	if nullZero(t) {
		g.pf("%s%s = nil", ind(n+1), target)
	}
	g.pf("%s} else {", ind(n))
	body = n + 1

	switch t.Kind {
	case analyzer.KindBool:
		g.parseInto(c, body, target, t.Expr, "bool", "odjsonrt.ParseBool(%s, %s)")
	case analyzer.KindInt:
		g.parseInto(c, body, target, t.Expr, "int64", fmt.Sprintf("odjsonrt.ParseInt(%%s, %%s, %d)", t.Bits))
	case analyzer.KindUint:
		g.parseInto(c, body, target, t.Expr, "uint64", fmt.Sprintf("odjsonrt.ParseUint(%%s, %%s, %d)", t.Bits))
	case analyzer.KindFloat:
		g.parseInto(c, body, target, t.Expr, "float64", fmt.Sprintf("odjsonrt.ParseFloat(%%s, %%s, %d)", t.Bits))
	case analyzer.KindString:
		g.parseInto(c, body, target, t.Expr, "string", "odjsonrt.ParseString(%s, %s)")
	case analyzer.KindNumber:
		g.parseInto(c, body, target, t.Expr, "string", "odjsonrt.ParseNumberString(%s, %s)")
	case analyzer.KindBytes:
		g.parseInto(c, body, target, t.Expr, "[]byte", "odjsonrt.ParseBase64(%s, %s)")
	case analyzer.KindSlice:
		g.decodeSlice(t, target, c, body)
	case analyzer.KindArray:
		g.decodeArray(t, target, c, body)
	case analyzer.KindMap:
		g.decodeMap(t, target, c, body)
	case analyzer.KindStruct:
		if t.Struct.Local {
			g.pf("%s%s, err = %s.odjsonParse(%s, %s)", ind(body), c.pos, target, c.data, c.pos)
		} else {
			g.pf("%s%s, err = %sParse(%s, %s, %s)", ind(body), c.pos, t.Struct.Helper, c.data, addr(target), c.pos)
		}
		g.decErr(c, body)
	default:
		if t.EmptyInterface {
			// ParseAny builds map[string]any / []any trees directly; the
			// reflection fallback below would cost an encoding/json call
			// per field.
			a := g.tmp("a")
			g.pf("%svar %s any", ind(body), a)
			g.pf("%s%s, %s, err = odjsonrt.ParseAny(%s, %s)", ind(body), a, c.pos, c.data, c.pos)
			g.decErr(c, body)
			g.pf("%s%s = %s", ind(body), target, a)
			break
		}
		g.pf("%s%s, err = odjsonrt.ParseInto(%s, %s, %s)", ind(body), c.pos, c.data, c.pos, addr(target))
		g.decErr(c, body)
	}
	g.pf("%s}", ind(n))
}

// parseInto emits the call/convert/assign triple shared by the scalar kinds.
func (g *generator) parseInto(c ctx, n int, target, expr, native, call string) {
	tv := g.tmp("x")
	g.pf("%svar %s %s", ind(n), tv, native)
	g.pf("%s%s, %s, err = %s", ind(n), tv, c.pos, fmt.Sprintf(call, c.data, c.pos))
	g.decErr(c, n)
	if expr == native {
		g.pf("%s%s = %s", ind(n), target, tv)
		return
	}
	g.pf("%s%s = %s(%s)", ind(n), target, expr, tv)
}

func (g *generator) expectByte(c ctx, n int, b byte, what string) {
	g.pf("%sif %s >= len(%s) || %s[%s] != '%c' {", ind(n), c.pos, c.data, c.data, c.pos, b)
	g.pf("%sreturn %s, odjsonrt.ErrType(%s, %s, %s)", ind(n+1), c.ret, c.data, c.pos, strconv.Quote(what))
	g.pf("%s}", ind(n))
	g.pf("%s%s++", ind(n), c.pos)
}

// separator emits the comma / closing bracket handling shared by arrays and
// objects.
func (g *generator) separator(c ctx, n int, closing byte, what string) {
	g.pf("%s%s = odjsonrt.SkipSpace(%s, %s)", ind(n), c.pos, c.data, c.pos)
	g.pf("%sif %s >= len(%s) {", ind(n), c.pos, c.data)
	g.pf("%sreturn %s, odjsonrt.ErrSyntax(%s, %s, \"unexpected end of JSON input\")", ind(n+1), c.ret, c.data, c.pos)
	g.pf("%s}", ind(n))
	g.pf("%sif %s[%s] == ',' {", ind(n), c.data, c.pos)
	g.pf("%s%s++", ind(n+1), c.pos)
	g.pf("%scontinue", ind(n+1))
	g.pf("%s}", ind(n))
	g.pf("%sif %s[%s] == '%c' {", ind(n), c.data, c.pos, closing)
	g.pf("%s%s++", ind(n+1), c.pos)
	g.pf("%sbreak", ind(n+1))
	g.pf("%s}", ind(n))
	g.pf("%sreturn %s, odjsonrt.ErrSyntax(%s, %s, %s)", ind(n), c.ret, c.data, c.pos, strconv.Quote(what))
}

func (g *generator) decodeSlice(t *analyzer.Type, target string, c ctx, n int) {
	s, e := g.tmp("s"), g.tmp("e")
	g.expectByte(c, n, '[', t.Expr)
	g.pf("%s%s := %s[:0]", ind(n), s, target)
	g.pf("%s%s = odjsonrt.SkipSpace(%s, %s)", ind(n), c.pos, c.data, c.pos)
	g.pf("%sif %s < len(%s) && %s[%s] == ']' {", ind(n), c.pos, c.data, c.data, c.pos)
	g.pf("%s%s++", ind(n+1), c.pos)
	g.pf("%s} else {", ind(n))
	g.pf("%sfor {", ind(n+1))
	g.pf("%svar %s %s", ind(n+2), e, t.Elem.Expr)
	g.decode(t.Elem, e, c, n+2)
	g.pf("%s%s = append(%s, %s)", ind(n+2), s, s, e)
	g.separator(c, n+2, ']', "after array element")
	g.pf("%s}", ind(n+1))
	g.pf("%s}", ind(n))
	g.pf("%sif %s == nil {", ind(n), s)
	g.pf("%s%s = %s{}", ind(n+1), s, t.Expr)
	g.pf("%s}", ind(n))
	g.pf("%s%s = %s", ind(n), target, s)
}

func (g *generator) decodeArray(t *analyzer.Type, target string, c ctx, n int) {
	i, z := g.tmp("i"), g.tmp("z")
	g.expectByte(c, n, '[', t.Expr)
	g.pf("%s%s := 0", ind(n), i)
	g.pf("%s%s = odjsonrt.SkipSpace(%s, %s)", ind(n), c.pos, c.data, c.pos)
	g.pf("%sif %s < len(%s) && %s[%s] == ']' {", ind(n), c.pos, c.data, c.data, c.pos)
	g.pf("%s%s++", ind(n+1), c.pos)
	g.pf("%s} else {", ind(n))
	g.pf("%sfor {", ind(n+1))
	g.pf("%sif %s < %d {", ind(n+2), i, t.Len)
	g.decode(t.Elem, fmt.Sprintf("%s[%s]", target, i), c, n+3)
	g.pf("%s} else {", ind(n+2))
	g.pf("%s%s = odjsonrt.SkipSpace(%s, %s)", ind(n+3), c.pos, c.data, c.pos)
	g.pf("%s%s, err = odjsonrt.SkipValue(%s, %s)", ind(n+3), c.pos, c.data, c.pos)
	g.decErr(c, n+3)
	g.pf("%s}", ind(n+2))
	g.pf("%s%s++", ind(n+2), i)
	g.separator(c, n+2, ']', "after array element")
	g.pf("%s}", ind(n+1))
	g.pf("%s}", ind(n))
	g.pf("%sfor ; %s < %d; %s++ {", ind(n), i, t.Len, i)
	g.pf("%svar %s %s", ind(n+1), z, t.Elem.Expr)
	g.pf("%s%s[%s] = %s", ind(n+1), target, i, z)
	g.pf("%s}", ind(n))
}

func (g *generator) decodeMap(t *analyzer.Type, target string, c ctx, n int) {
	m, k, mv := g.tmp("m"), g.tmp("k"), g.tmp("mv")
	g.expectByte(c, n, '{', t.Expr)
	g.pf("%s%s := %s", ind(n), m, target)
	g.pf("%sif %s == nil {", ind(n), m)
	g.pf("%s%s = make(%s)", ind(n+1), m, t.Expr)
	g.pf("%s}", ind(n))
	g.pf("%s%s = odjsonrt.SkipSpace(%s, %s)", ind(n), c.pos, c.data, c.pos)
	g.pf("%sif %s < len(%s) && %s[%s] == '}' {", ind(n), c.pos, c.data, c.data, c.pos)
	g.pf("%s%s++", ind(n+1), c.pos)
	g.pf("%s} else {", ind(n))
	g.pf("%sfor {", ind(n+1))
	g.pf("%svar %s []byte", ind(n+2), k)
	g.pf("%s%s = odjsonrt.SkipSpace(%s, %s)", ind(n+2), c.pos, c.data, c.pos)
	g.pf("%s%s, _, %s, err = odjsonrt.ParseKey(%s, %s)", ind(n+2), k, c.pos, c.data, c.pos)
	g.decErr(c, n+2)
	g.pf("%svar %s %s", ind(n+2), mv, t.Elem.Expr)
	g.decode(t.Elem, mv, c, n+2)
	g.pf("%s%s[%s] = %s", ind(n+2), m, convert(t.Key.Expr, "string("+k+")"), mv)
	g.separator(c, n+2, '}', "after object key:value pair")
	g.pf("%s}", ind(n+1))
	g.pf("%s}", ind(n))
	g.pf("%s%s = %s", ind(n), target, m)
}

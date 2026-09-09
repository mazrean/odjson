package codegen

import (
	"fmt"
	"strconv"

	"github.com/mazrean/odjson/internal/analyzer"
)

func (g *generator) decErr(c ctx, n int) {
	g.pf("%sif err != nil {", ind(n))
	g.pf("%s%s", ind(n+1), c.fail("err"))
	g.pf("%s}", ind(n))
}

// decodeStruct writes the body of a struct's byte oriented decoder. Under
// c.v2 it follows encoding/json/v2: a null zeroes the struct and member names
// match case-sensitively.
func (g *generator) decodeStruct(s *analyzer.StructInfo, c ctx) {
	g.pf("\tvar err error")
	g.pf("\t_ = err")
	g.pf("\tp = odjsonrt.SkipSpace(data, p)")
	g.pf("\tif np, ok := odjsonrt.ParseNull(data, p); ok {")
	if c.v2 {
		g.pf("\t\t*v = %s{}", s.Expr)
	}
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
	if c.strict {
		// json/v2 rejects a member name that occurs twice. Known members
		// are tracked in a bitset, as json/v2's own decoder does; unknown
		// ones, which are rare, in a list.
		g.pf("\tvar seen [%d]uint64", (max(len(s.Fields), 1)+63)/64)
		g.pf("\t_ = seen")
		// The list lives on the stack until an object carries more unknown
		// members than the array holds.
		g.pf("\tvar unknownBuf [8][]byte")
		g.pf("\tunknown := unknownBuf[:0]")
	}
	g.pf("\tfor {")
	g.pf("\t\tvar key []byte")
	g.pf("\t\tp = odjsonrt.SkipSpace(data, p)")
	if c.strict {
		g.pf("\t\tkp := p")
		g.pf("\t\tkey, p, err = odjsonrt.ParseKeyV2(data, p, strict)")
	} else {
		g.pf("\t\tkey, _, p, err = odjsonrt.ParseKey(data, p)")
	}
	g.pf("\t\tif err != nil {")
	g.pf("\t\t\treturn p, err")
	g.pf("\t\t}")
	g.pf("\t\tidx := -1")
	if len(s.Fields) > 0 {
		g.pf("\t\tswitch string(key) {")
		for i, f := range s.Fields {
			g.pf("\t\tcase %s:", strconv.Quote(f.JSONName))
			g.pf("\t\t\tidx = %d", i)
		}
		g.pf("\t\t}")
		if g.opts.CaseInsensitive && !c.v2 {
			// An unmatched name is compared case-insensitively against every
			// field. Guarding each comparison by length keeps that from being
			// a call per field: only a non-ASCII name can fold to a name of a
			// different byte length.
			g.pf("\t\tif idx < 0 {")
			g.pf("\t\t\tfold := !odjsonrt.ASCII(key)")
			g.pf("\t\t\tswitch {")
			for i, f := range s.Fields {
				g.pf("\t\t\tcase (len(key) == %d || fold) && odjsonrt.EqualFold(key, %s):",
					len(f.JSONName), strconv.Quote(f.JSONName))
				g.pf("\t\t\t\tidx = %d", i)
			}
			g.pf("\t\t\t}")
			g.pf("\t\t}")
		}
	}
	g.pf("\t\tswitch idx {")
	for i, f := range s.Fields {
		g.pf("\t\tcase %d:", i)
		if c.strict {
			g.pf("\t\t\tif strict && seen[%d]&(1<<%d) != 0 {", i/64, i%64)
			g.pf("\t\t\t\treturn kp, odjsonrt.ErrDuplicateName(data, kp, key)")
			g.pf("\t\t\t}")
			g.pf("\t\t\tseen[%d] |= 1 << %d", i/64, i%64)
		}
		g.allocSteps(f, 3)
		if f.AsString {
			g.decodeQuoted(f.Type, selector(f), c, 3)
		} else {
			g.decode(f.Type, selector(f), c, 3)
		}
	}
	g.pf("\t\tdefault:")
	if c.strict {
		g.pf("\t\t\tif strict {")
		g.pf("\t\t\t\tfor _, u := range unknown {")
		g.pf("\t\t\t\t\tif string(u) == string(key) {")
		g.pf("\t\t\t\t\t\treturn kp, odjsonrt.ErrDuplicateName(data, kp, key)")
		g.pf("\t\t\t\t\t}")
		g.pf("\t\t\t\t}")
		g.pf("\t\t\t\tunknown = append(unknown, key)")
		g.pf("\t\t\t}")
		g.pf("\t\t\tp = odjsonrt.SkipSpace(data, p)")
		g.pf("\t\t\tp, err = odjsonrt.SkipValueV2(data, p, strict)")
	} else {
		g.pf("\t\t\tp = odjsonrt.SkipSpace(data, p)")
		g.pf("\t\t\tp, err = odjsonrt.SkipValue(data, p)")
	}
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
	if c.v2 {
		g.pf("%s%s = %s", ind(n+1), target, zeroLit(t))
	}
	g.pf("%s} else {", ind(n))
	g.pf("%svar %s []byte", ind(n+1), inner)
	g.pf("%s%s, %s, err = odjsonrt.%s(%s, %s%s)", ind(n+1), inner, c.pos, strictName("ParseStringInner", c), c.data, c.pos, strictArg(c))
	g.decErr(c, n+1)
	g.pf("%s%s := 0", ind(n+1), sp)
	sub := c
	sub.data, sub.pos, sub.trusted, sub.trimmed, sub.cache = inner, sp, false, false, ""
	sub.strict = false // the inner bytes were validated as a string already
	g.decode(t, target, sub, n+1)
	g.pf("%sif err = odjsonrt.EndOfDocument(%s, %s); err != nil {", ind(n+1), inner, sp)
	g.pf("%s%s", ind(n+2), c.fail("err"))
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
	// lead emits the fragment's leading whitespace skip, unless the caller has
	// already positioned the offset on the value. Only the first call in a
	// fragment can be elided; everything nested has to look for itself.
	lead := func() {
		if c.trimmed {
			c.trimmed = false
			return
		}
		g.pf("%s%s = odjsonrt.SkipSpace(%s, %s)", ind(n), c.pos, c.data, c.pos)
	}
	switch t.Kind {
	case analyzer.KindRawMessage:
		raw := g.tmp("raw")
		lead()
		g.pf("%svar %s []byte", ind(n), raw)
		g.pf("%s%s, %s, err = odjsonrt.%s(%s, %s%s)", ind(n), raw, c.pos, strictName("ParseRaw", c), c.data, c.pos, strictArg(c))
		g.decErr(c, n)
		g.pf("%s%s = append(%s[:0], %s...)", ind(n), target, target, raw)
		return
	case analyzer.KindStruct:
		// The struct's own decoder skips leading whitespace and handles a
		// null itself, so wrapping the call in another of each is wasted
		// work on every nested object.
		switch {
		case c.v2 && t.Struct.Local:
			g.pf("%s%s, err = %s.odjsonParseV2(%s, %s, %s, %s)", ind(n), c.pos, target, c.data, c.pos, cacheExpr(c), strictExpr(c))
		case c.v2:
			g.pf("%s%s, err = %sParseV2(%s, %s, %s, %s, %s)", ind(n), c.pos, t.Struct.Helper, c.data, addr(target), c.pos, cacheExpr(c), strictExpr(c))
		case t.Struct.Local:
			g.pf("%s%s, err = %s.odjsonParse(%s, %s)", ind(n), c.pos, target, c.data, c.pos)
		default:
			g.pf("%s%s, err = %sParse(%s, %s, %s)", ind(n), c.pos, t.Struct.Helper, c.data, addr(target), c.pos)
		}
		g.decErr(c, n)
		return
	case analyzer.KindPointer:
		np, ok := g.tmp("np"), g.tmp("ok")
		lead()
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
			if c.v2 && t.Expr == "time.Time" {
				// json/v2 does not call time.Time's UnmarshalJSON; it has
				// its own codec for it, and that one zeroes on null where
				// the method leaves the value alone.
				np, ok := g.tmp("np"), g.tmp("ok")
				lead()
				g.pf("%sif %s, %s := odjsonrt.ParseNull(%s, %s); %s {", ind(n), np, ok, c.data, c.pos, ok)
				g.pf("%s%s = %s", ind(n+1), c.pos, np)
				g.pf("%s%s = time.Time{}", ind(n+1), target)
				g.pf("%s} else {", ind(n))
				g.pf("%s%s, err = odjsonrt.%s(%s, %s, %s%s)", ind(n+1), c.pos, strictName("ParseUnmarshaler", c), c.data, c.pos, addr(target), strictArg(c))
				g.decErr(c, n+1)
				g.pf("%s}", ind(n))
				return
			}
			g.pf("%s%s, err = odjsonrt.%s(%s, %s, %s%s)", ind(n), c.pos, strictName("ParseUnmarshaler", c), c.data, c.pos, addr(target), strictArg(c))
			g.decErr(c, n)
			return
		case t.TextUnmarshaler || t.PtrTextUnmarshaler:
			// encoding/json never hands a null to UnmarshalText; it leaves
			// the value alone. json.Unmarshaler, in contrast, does receive
			// the literal null, so only this branch skips it.
			np, ok := g.tmp("np"), g.tmp("ok")
			lead()
			g.pf("%sif %s, %s := odjsonrt.ParseNull(%s, %s); %s {", ind(n), np, ok, c.data, c.pos, ok)
			g.pf("%s%s = %s", ind(n+1), c.pos, np)
			if c.v2 {
				g.pf("%s%s = %s", ind(n+1), target, zeroLit(t))
			}
			g.pf("%s} else {", ind(n))
			g.pf("%s%s, err = odjsonrt.%s(%s, %s, %s%s)", ind(n+1), c.pos, strictName("ParseTextUnmarshaler", c), c.data, c.pos, addr(target), strictArg(c))
			g.decErr(c, n+1)
			g.pf("%s}", ind(n))
			return
		}
	}

	lead()
	np, ok := g.tmp("np"), g.tmp("ok")
	g.pf("%sif %s, %s := odjsonrt.ParseNull(%s, %s); %s {", ind(n), np, ok, c.data, c.pos, ok)
	g.pf("%s%s = %s", ind(n+1), c.pos, np)
	if c.v2 {
		g.pf("%s%s = %s", ind(n+1), target, zeroLit(t))
	} else if nullZero(t) {
		g.pf("%s%s = nil", ind(n+1), target)
	}
	g.pf("%s} else {", ind(n))
	body := n + 1

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
		call := "odjsonrt.ParseString(%s, %s)"
		switch {
		case c.strict:
			call = "odjsonrt.ParseStringV2(%s, %s, " + cacheExpr(c) + ", strict)"
		case c.cache != "":
			call = "odjsonrt.ParseStringWith(%s, %s, " + c.cache + ")"
		case c.trusted:
			call = "odjsonrt.ParseStringTrusted(%s, %s)"
		}
		g.parseInto(c, body, target, t.Expr, "string", call)
	case analyzer.KindNumber:
		g.parseInto(c, body, target, t.Expr, "string", "odjsonrt."+strictName("ParseNumberString", c)+"(%s, %s"+strictArg(c)+")")
	case analyzer.KindBytes:
		g.parseInto(c, body, target, t.Expr, "[]byte", "odjsonrt."+strictName("ParseBase64", c)+"(%s, %s"+strictArg(c)+")")
	case analyzer.KindSlice:
		g.decodeSlice(t, target, c, body)
	case analyzer.KindArray:
		g.decodeArray(t, target, c, body)
	case analyzer.KindMap:
		g.decodeMap(t, target, c, body)
	default:
		if t.EmptyInterface {
			// ParseAny builds map[string]any / []any trees directly; the
			// reflection fallback below would cost an encoding/json call
			// per field.
			a := g.tmp("a")
			g.pf("%svar %s any", ind(body), a)
			switch {
			case c.strict:
				g.pf("%s%s, %s, err = odjsonrt.ParseAnyV2(%s, %s, %s, strict)", ind(body), a, c.pos, c.data, c.pos, cacheExpr(c))
			case c.cache != "":
				g.pf("%s%s, %s, err = odjsonrt.ParseAnyWith(%s, %s, %s)", ind(body), a, c.pos, c.data, c.pos, c.cache)
			default:
				g.pf("%s%s, %s, err = odjsonrt.ParseAny(%s, %s)", ind(body), a, c.pos, c.data, c.pos)
			}
			g.decErr(c, body)
			g.pf("%s%s = %s", ind(body), target, a)
			break
		}
		if c.v2 {
			g.pf("%s%s, err = odjsonrt.ParseIntoV2(%s, %s, %s, %s)", ind(body), c.pos, c.data, c.pos, addr(target), strictExpr(c))
		} else {
			g.pf("%s%s, err = odjsonrt.ParseInto(%s, %s, %s)", ind(body), c.pos, c.data, c.pos, addr(target))
		}
		g.decErr(c, body)
	}
	g.pf("%s}", ind(n))
}

// strictName picks the V2 variant of a runtime parser under c.strict; that
// variant takes the flag [strictArg] renders.
func strictName(name string, c ctx) string {
	if c.strict {
		return name + "V2"
	}
	return name
}

// strictArg renders the trailing strict argument of a V2 runtime parser, or
// nothing when c does not select one.
func strictArg(c ctx) string {
	if c.strict {
		return ", strict"
	}
	return ""
}

// strictExpr renders the strict flag handed to a nested V2 parser: the
// enclosing parser's own flag inside odjsonParseV2, and false anywhere else,
// since there the bytes have come through a jsontext.Decoder.
func strictExpr(c ctx) string {
	if c.strict {
		return "strict"
	}
	return "false"
}

// cacheExpr renders the string cache argument for c.
func cacheExpr(c ctx) string {
	if c.cache == "" {
		return "nil"
	}
	return c.cache
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
	g.pf("%s%s", ind(n+1), c.fail(fmt.Sprintf("odjsonrt.ErrType(%s, %s, %s)", c.data, c.pos, strconv.Quote(what))))
	g.pf("%s}", ind(n))
	g.pf("%s%s++", ind(n), c.pos)
}

// separator emits the comma / closing bracket handling shared by arrays and
// objects.
func (g *generator) separator(c ctx, n int, closing byte, what string) {
	g.pf("%s%s = odjsonrt.SkipSpace(%s, %s)", ind(n), c.pos, c.data, c.pos)
	g.pf("%sif %s >= len(%s) {", ind(n), c.pos, c.data)
	g.pf("%s%s", ind(n+1), c.fail(fmt.Sprintf("odjsonrt.ErrSyntax(%s, %s, \"unexpected end of JSON input\")", c.data, c.pos)))
	g.pf("%s}", ind(n))
	g.pf("%sif %s[%s] == ',' {", ind(n), c.data, c.pos)
	g.pf("%s%s++", ind(n+1), c.pos)
	g.pf("%scontinue", ind(n+1))
	g.pf("%s}", ind(n))
	g.pf("%sif %s[%s] == '%c' {", ind(n), c.data, c.pos, closing)
	g.pf("%s%s++", ind(n+1), c.pos)
	g.pf("%sbreak", ind(n+1))
	g.pf("%s}", ind(n))
	g.pf("%s%s", ind(n), c.fail(fmt.Sprintf("odjsonrt.ErrSyntax(%s, %s, %s)", c.data, c.pos, strconv.Quote(what))))
}

func (g *generator) decodeSlice(t *analyzer.Type, target string, c ctx, n int) {
	s, e := g.tmp("s"), g.tmp("e")
	g.expectByte(c, n, '[', t.Expr)
	g.pf("%s%s := %s[:0]", ind(n), s, target)
	g.pf("%s%s = odjsonrt.SkipSpace(%s, %s)", ind(n), c.pos, c.data, c.pos)
	g.pf("%sif %s < len(%s) && %s[%s] == ']' {", ind(n), c.pos, c.data, c.data, c.pos)
	g.pf("%s%s++", ind(n+1), c.pos)
	g.pf("%s} else {", ind(n))
	// Growing from nothing costs an allocation and a copy per doubling, so
	// start with room for a few elements — but only once the array is known
	// to be non-empty, since empty arrays are common and must stay free.
	g.pf("%sif cap(%s) == 0 {", ind(n+1), s)
	g.pf("%s%s = make(%s, 0, 4)", ind(n+2), s, t.Expr)
	g.pf("%s}", ind(n+1))
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
	if c.v2 {
		// json/v2 rejects an array whose length does not match the Go
		// array, where encoding/json silently pads or truncates.
		g.pf("%s%s", ind(n+3), c.fail(fmt.Sprintf("odjsonrt.ErrArrayLength(%s, true)", strconv.Quote(t.Expr))))
	} else {
		g.pf("%s%s = odjsonrt.SkipSpace(%s, %s)", ind(n+3), c.pos, c.data, c.pos)
		g.pf("%s%s, err = odjsonrt.SkipValue(%s, %s)", ind(n+3), c.pos, c.data, c.pos)
		g.decErr(c, n+3)
	}
	g.pf("%s}", ind(n+2))
	g.pf("%s%s++", ind(n+2), i)
	g.separator(c, n+2, ']', "after array element")
	g.pf("%s}", ind(n+1))
	g.pf("%s}", ind(n))
	if c.v2 {
		g.pf("%sif %s != %d {", ind(n), i, t.Len)
		g.pf("%s%s", ind(n+1), c.fail(fmt.Sprintf("odjsonrt.ErrArrayLength(%s, false)", strconv.Quote(t.Expr))))
		g.pf("%s}", ind(n))
	} else {
		g.pf("%sfor ; %s < %d; %s++ {", ind(n), i, t.Len, i)
		g.pf("%svar %s %s", ind(n+1), z, t.Elem.Expr)
		g.pf("%s%s[%s] = %s", ind(n+1), target, i, z)
		g.pf("%s}", ind(n))
	}
}

func (g *generator) decodeMap(t *analyzer.Type, target string, c ctx, n int) {
	m, k, mv := g.tmp("m"), g.tmp("k"), g.tmp("mv")
	g.expectByte(c, n, '{', t.Expr)
	g.pf("%s%s := %s", ind(n), m, target)
	g.pf("%sif %s == nil {", ind(n), m)
	g.pf("%s%s = make(%s)", ind(n+1), m, t.Expr)
	g.pf("%s}", ind(n))
	var seen string
	if c.strict {
		// json/v2 rejects a name that occurs twice. A map that was empty
		// when decoding began detects that by itself; one that was not
		// needs the names of this document tracked separately.
		seen = g.tmp("seen")
		g.pf("%svar %s map[string]struct{}", ind(n), seen)
		g.pf("%sif strict && len(%s) > 0 {", ind(n), m)
		g.pf("%s%s = make(map[string]struct{})", ind(n+1), seen)
		g.pf("%s}", ind(n))
	}
	g.pf("%s%s = odjsonrt.SkipSpace(%s, %s)", ind(n), c.pos, c.data, c.pos)
	g.pf("%sif %s < len(%s) && %s[%s] == '}' {", ind(n), c.pos, c.data, c.data, c.pos)
	g.pf("%s%s++", ind(n+1), c.pos)
	g.pf("%s} else {", ind(n))
	g.pf("%sfor {", ind(n+1))
	g.pf("%svar %s []byte", ind(n+2), k)
	g.pf("%s%s = odjsonrt.SkipSpace(%s, %s)", ind(n+2), c.pos, c.data, c.pos)
	if c.strict {
		kp := g.tmp("kp")
		g.pf("%s%s := %s", ind(n+2), kp, c.pos)
		g.pf("%s%s, %s, err = odjsonrt.ParseKeyV2(%s, %s, strict)", ind(n+2), k, c.pos, c.data, c.pos)
		g.decErr(c, n+2)
		g.pf("%sif !strict {", ind(n+2))
		g.pf("%s} else if %s == nil {", ind(n+2), seen)
		g.pf("%sif _, dup := %s[%s]; dup {", ind(n+3), m, convert(t.Key.Expr, "string("+k+")"))
		g.pf("%s%s", ind(n+4), c.fail(fmt.Sprintf("odjsonrt.ErrDuplicateName(%s, %s, %s)", c.data, kp, k)))
		g.pf("%s}", ind(n+3))
		g.pf("%s} else {", ind(n+2))
		g.pf("%sif _, dup := %s[string(%s)]; dup {", ind(n+3), seen, k)
		g.pf("%s%s", ind(n+4), c.fail(fmt.Sprintf("odjsonrt.ErrDuplicateName(%s, %s, %s)", c.data, kp, k)))
		g.pf("%s}", ind(n+3))
		g.pf("%s%s[string(%s)] = struct{}{}", ind(n+3), seen, k)
		g.pf("%s}", ind(n+2))
	} else {
		g.pf("%s%s, _, %s, err = odjsonrt.ParseKey(%s, %s)", ind(n+2), k, c.pos, c.data, c.pos)
		g.decErr(c, n+2)
	}
	g.pf("%svar %s %s", ind(n+2), mv, t.Elem.Expr)
	g.decode(t.Elem, mv, c, n+2)
	key := "string(" + k + ")"
	if c.cache != "" {
		key = c.cache + ".Make(" + k + ")"
	}
	g.pf("%s%s[%s] = %s", ind(n+2), m, convert(t.Key.Expr, key), mv)
	g.separator(c, n+2, '}', "after object key:value pair")
	g.pf("%s}", ind(n+1))
	g.pf("%s}", ind(n))
	g.pf("%s%s = %s", ind(n), target, m)
}

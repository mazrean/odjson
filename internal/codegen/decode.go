package codegen

import (
	"fmt"
	"strconv"
	"unicode/utf8"

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
	}
	g.pf("\t\tidx := -1")
	g.rawKeys(s, c, 2)
	g.pf("\t\tif idx >= 0 {")
	// A compact document has the colon right after the name; anything
	// else (whitespace, or an error) is AfterKey's.
	g.pf("\t\t\tif p < len(data) && data[p] == ':' {")
	g.pf("\t\t\t\tp = odjsonrt.SkipSpace(data, p+1)")
	g.pf("\t\t\t} else if p, err = odjsonrt.AfterKey(data, p); err != nil {")
	g.pf("\t\t\t\treturn p, err")
	g.pf("\t\t\t}")
	g.pf("\t\t} else {")
	if c.strict {
		g.pf("\t\t\tkey, p, err = odjsonrt.ParseKeyV2(data, p, strict)")
	} else {
		g.pf("\t\t\tkey, _, p, err = odjsonrt.ParseKey(data, p)")
	}
	g.pf("\t\t\tif err != nil {")
	g.pf("\t\t\t\treturn p, err")
	g.pf("\t\t\t}")
	if len(s.Fields) > 0 {
		g.pf("\t\t\tswitch string(key) {")
		for i, f := range s.Fields {
			g.pf("\t\t\tcase %s:", strconv.Quote(f.JSONName))
			g.pf("\t\t\t\tidx = %d", i)
		}
		g.pf("\t\t\t}")
		if g.opts.CaseInsensitive && !c.v2 {
			// An unmatched name is compared case-insensitively against every
			// field. Guarding each comparison by length keeps that from being
			// a call per field: only a non-ASCII name can fold to a name of a
			// different byte length.
			g.pf("\t\t\tif idx < 0 {")
			g.pf("\t\t\t\tfold := !odjsonrt.ASCII(key)")
			g.pf("\t\t\t\tswitch {")
			for i, f := range s.Fields {
				g.pf("\t\t\t\tcase (len(key) == %d || fold) && odjsonrt.EqualFold(key, %s):",
					len(f.JSONName), strconv.Quote(f.JSONName))
				g.pf("\t\t\t\t\tidx = %d", i)
			}
			g.pf("\t\t\t\t}")
			g.pf("\t\t\t}")
		}
	}
	g.pf("\t\t}")
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
			// Both key paths leave p on the value's first byte.
			mc := c
			mc.trimmed = true
			g.decode(f.Type, selector(f), mc, 3)
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

// rawKeys emits the fast path of member name matching: every field whose
// name can appear verbatim in a document is compared, quotes included,
// against the bytes at p, dispatched on the name's first byte. A hit sets
// idx (and key, where the strict path needs it for its duplicate error) and
// moves p past the closing quote, having scanned nothing;
// anything else (an escaped or folded spelling, an unknown name, a
// malformed key) leaves idx at -1 for the general path. The comparisons are
// against constants of a few bytes, which the compiler turns into word
// loads, so a known name costs one byte switch and one or two compares.
func (g *generator) rawKeys(s *analyzer.StructInfo, c ctx, n int) {
	type cand struct {
		idx  int
		name string
	}
	var order []byte
	groups := map[byte][]cand{}
	for i, f := range s.Fields {
		if !rawKeyable(f.JSONName) {
			continue
		}
		b := f.JSONName[0]
		if _, ok := groups[b]; !ok {
			order = append(order, b)
		}
		groups[b] = append(groups[b], cand{i, f.JSONName})
	}
	if len(order) == 0 {
		return
	}
	g.pf("%sif rest := data[p:]; len(rest) > 1 {", ind(n))
	g.pf("%sswitch rest[1] {", ind(n+1))
	for _, b := range order {
		g.pf("%scase %s:", ind(n+1), strconv.QuoteRune(rune(b)))
		g.pf("%sswitch {", ind(n+2))
		for _, k := range groups[b] {
			lit := strconv.Quote(`"` + k.name + `"`)
			l := len(k.name) + 2
			g.pf("%scase len(rest) >= %d && string(rest[:%d]) == %s:", ind(n+2), l, l, lit)
			if c.strict {
				g.pf("%sidx, key, p = %d, rest[1:%d], p+%d", ind(n+3), k.idx, l-1, l)
			} else {
				g.pf("%sidx, p = %d, p+%d", ind(n+3), k.idx, l)
			}
		}
		g.pf("%s}", ind(n+2))
	}
	g.pf("%s}", ind(n+1))
	g.pf("%s}", ind(n))
}

// rawKeyable reports whether name can be compared against a document's
// bytes without decoding: it must be non-empty, valid UTF-8, and free of
// the bytes a JSON string literal cannot hold verbatim. A name that fails
// still decodes correctly; it just always takes the general path.
func rawKeyable(name string) bool {
	if name == "" || !utf8.ValidString(name) {
		return false
	}
	for i := 0; i < len(name); i++ {
		if c := name[i]; c < 0x20 || c == '"' || c == '\\' {
			return false
		}
	}
	return true
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
			g.pf("%s%s, err = %s.odjsonParse(%s, %s, %s)", ind(n), c.pos, target, c.data, c.pos, cacheExpr(c))
		default:
			g.pf("%s%s, err = %sParse(%s, %s, %s, %s)", ind(n), c.pos, t.Struct.Helper, c.data, addr(target), c.pos, cacheExpr(c))
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

	// The scalar kinds test for the common spelling inline, so that a
	// well-formed value costs no call at all; the runtime parser behind the
	// else branch handles everything else, errors included.
	switch t.Kind {
	case analyzer.KindBool:
		g.pf("%sif %s+4 <= len(%s) && string(%s[%s:%s+4]) == \"true\" {", ind(body), c.pos, c.data, c.data, c.pos, c.pos)
		g.pf("%s%s = true", ind(body+1), target)
		g.pf("%s%s += 4", ind(body+1), c.pos)
		g.pf("%s} else if %s+5 <= len(%s) && string(%s[%s:%s+5]) == \"false\" {", ind(body), c.pos, c.data, c.data, c.pos, c.pos)
		g.pf("%s%s = false", ind(body+1), target)
		g.pf("%s%s += 5", ind(body+1), c.pos)
		g.pf("%s} else {", ind(body))
		g.parseInto(c, body+1, target, t.Expr, "bool", "odjsonrt.ParseBool(%s, %s)")
		g.pf("%s}", ind(body))
	case analyzer.KindInt:
		x, np, ok := g.tmp("x"), g.tmp("np"), g.tmp("ok")
		fits := ""
		if t.Bits < 64 {
			fits = fmt.Sprintf(" && %s >= %d && %s <= %d", x, -1<<(t.Bits-1), x, 1<<(t.Bits-1)-1)
		}
		g.pf("%sif %s, %s, %s := odjsonrt.ParseDecimal(%s, %s); %s%s {", ind(body), x, np, ok, c.data, c.pos, ok, fits)
		g.pf("%s%s = %s", ind(body+1), target, convert(t.Expr, x))
		g.pf("%s%s = %s", ind(body+1), c.pos, np)
		g.pf("%s} else {", ind(body))
		g.parseInto(c, body+1, target, t.Expr, "int64", fmt.Sprintf("odjsonrt.ParseInt(%%s, %%s, %d)", t.Bits))
		g.pf("%s}", ind(body))
	case analyzer.KindUint:
		x, np, ok := g.tmp("x"), g.tmp("np"), g.tmp("ok")
		fits := ""
		if t.Bits < 64 {
			fits = fmt.Sprintf(" && %s <= %d", x, uint64(1)<<t.Bits-1)
		}
		// ParseDecimal reads at most eighteen digits, so a non-negative
		// result always fits a uint64; the sign is the only other check.
		g.pf("%sif %s, %s, %s := odjsonrt.ParseDecimal(%s, %s); %s && %s[%s] != '-'%s {", ind(body), x, np, ok, c.data, c.pos, ok, c.data, c.pos, fits)
		g.pf("%s%s = %s", ind(body+1), target, convert(t.Expr, x))
		g.pf("%s%s = %s", ind(body+1), c.pos, np)
		g.pf("%s} else {", ind(body))
		g.parseInto(c, body+1, target, t.Expr, "uint64", fmt.Sprintf("odjsonrt.ParseUint(%%s, %%s, %d)", t.Bits))
		g.pf("%s}", ind(body))
	case analyzer.KindFloat:
		x, np, ok := g.tmp("x"), g.tmp("np"), g.tmp("ok")
		g.pf("%sif %s, %s, %s := odjsonrt.ParseSimpleFloat(%s, %s, %d); %s {", ind(body), x, np, ok, c.data, c.pos, t.Bits, ok)
		g.pf("%s%s = %s", ind(body+1), target, convert(t.Expr, x))
		g.pf("%s%s = %s", ind(body+1), c.pos, np)
		g.pf("%s} else {", ind(body))
		g.parseInto(c, body+1, target, t.Expr, "float64", fmt.Sprintf("odjsonrt.ParseFloat(%%s, %%s, %d)", t.Bits))
		g.pf("%s}", ind(body))
	case analyzer.KindString:
		call := "odjsonrt.ParseString(%s, %s)"
		switch {
		case c.strict:
			call = "odjsonrt.ParseStringV2(%s, %s, " + cacheExpr(c) + ", strict)"
		case c.trusted && c.cache != "":
			call = "odjsonrt.ParseStringWith(%s, %s, " + c.cache + ")"
		case c.trusted:
			call = "odjsonrt.ParseStringTrusted(%s, %s)"
		case c.cache != "":
			call = "odjsonrt.ParseStringCached(%s, %s, " + c.cache + ")"
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
			case c.trusted && c.cache != "":
				g.pf("%s%s, %s, err = odjsonrt.ParseAnyWith(%s, %s, %s)", ind(body), a, c.pos, c.data, c.pos, c.cache)
			case c.cache != "":
				g.pf("%s%s, %s, err = odjsonrt.ParseAnyCached(%s, %s, %s)", ind(body), a, c.pos, c.data, c.pos, c.cache)
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

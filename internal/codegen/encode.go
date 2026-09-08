package codegen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/mazrean/odjson/internal/analyzer"
)

func ind(n int) string { return strings.Repeat("\t", n) }

// jsonString renders s as a JSON string literal at generation time, so member
// names become compile time constants in the generated code.
func jsonString(s string, escapeHTML bool) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(escapeHTML)
	if err := enc.Encode(s); err != nil {
		return strconv.Quote(s)
	}
	return strings.TrimRight(b.String(), "\n")
}

func (g *generator) htmlLit() string {
	if g.opts.EscapeHTML {
		return "true"
	}
	return "false"
}

func (g *generator) encodeStruct(s *analyzer.StructInfo) {
	g.pf("\tvar err error")
	g.pf("\t_ = err")
	g.pf("\tstart := len(dst)")
	for _, f := range s.Fields {
		var conds []string
		if gd := guard(f); gd != "" {
			conds = append(conds, gd)
		}
		if f.OmitZero {
			if c := g.nonZeroExpr(f.Type, selector(f)); c != "true" {
				conds = append(conds, c)
			}
		}
		if f.OmitEmpty {
			if c := g.nonEmptyExpr(f.Type, selector(f)); c != "true" {
				conds = append(conds, c)
			}
		}
		n := 1
		if len(conds) > 0 {
			g.pf("\tif %s {", strings.Join(conds, " && "))
			n = 2
		}
		g.pf("%sdst = append(dst, %s...)", ind(n), quoteBytes(","+jsonString(f.JSONName, g.opts.EscapeHTML)+":"))
		if f.AsString {
			g.encodeQuoted(f.Type, selector(f), true, n)
		} else {
			g.encode(f.Type, selector(f), true, n)
		}
		if len(conds) > 0 {
			g.pf("\t}")
		}
	}
	g.pf("\tif len(dst) == start {")
	g.pf("\t\tdst = append(dst, '{', '}')")
	g.pf("\t} else {")
	g.pf("\t\tdst[start] = '{'")
	g.pf("\t\tdst = append(dst, '}')")
	g.pf("\t}")
	g.pf("\treturn dst, nil")
}

// encodeQuoted implements the ",string" tag option.
func (g *generator) encodeQuoted(t *analyzer.Type, src string, addressable bool, n int) {
	switch t.Kind {
	case analyzer.KindPointer:
		g.pf("%sif %s == nil {", ind(n), src)
		g.pf("%sdst = append(dst, 'n', 'u', 'l', 'l')", ind(n+1))
		g.pf("%s} else {", ind(n))
		g.encodeQuoted(t.Elem, "(*"+src+")", true, n+1)
		g.pf("%s}", ind(n))
	case analyzer.KindString:
		g.pf("%sdst = odjsonrt.AppendStringQuoted(dst, string(%s), %s)", ind(n), src, g.htmlLit())
	default:
		g.pf("%sdst = append(dst, '\"')", ind(n))
		g.encode(t, src, addressable, n)
		g.pf("%sdst = append(dst, '\"')", ind(n))
	}
}

func (g *generator) encErr(n int) {
	g.pf("%sif err != nil {", ind(n))
	g.pf("%sreturn nil, err", ind(n+1))
	g.pf("%s}", ind(n))
}

// encode writes the statements appending the JSON encoding of src to dst.
func (g *generator) encode(t *analyzer.Type, src string, addressable bool, n int) {
	switch t.Kind {
	case analyzer.KindRawMessage:
		g.pf("%sdst, err = odjsonrt.AppendRaw(dst, []byte(%s), %s)", ind(n), src, g.htmlLit())
		g.encErr(n)
		return
	case analyzer.KindNumber:
		g.pf("%sdst, err = odjsonrt.AppendNumber(dst, string(%s))", ind(n), src)
		g.encErr(n)
		return
	case analyzer.KindPointer:
		g.pf("%sif %s == nil {", ind(n), src)
		g.pf("%sdst = append(dst, 'n', 'u', 'l', 'l')", ind(n+1))
		g.pf("%s} else {", ind(n))
		g.encode(t.Elem, "(*"+src+")", true, n+1)
		g.pf("%s}", ind(n))
		return
	}

	switch {
	case t.Marshaler:
		g.pf("%sdst, err = odjsonrt.AppendMarshaler(dst, %s, %s)", ind(n), src, g.htmlLit())
		g.encErr(n)
		return
	case t.PtrMarshaler && addressable:
		g.pf("%sdst, err = odjsonrt.AppendMarshaler(dst, %s, %s)", ind(n), addr(src), g.htmlLit())
		g.encErr(n)
		return
	case t.TextMarshaler:
		g.pf("%sdst, err = odjsonrt.AppendTextMarshaler(dst, %s, %s)", ind(n), src, g.htmlLit())
		g.encErr(n)
		return
	case t.PtrTextMarshaler && addressable:
		g.pf("%sdst, err = odjsonrt.AppendTextMarshaler(dst, %s, %s)", ind(n), addr(src), g.htmlLit())
		g.encErr(n)
		return
	}

	switch t.Kind {
	case analyzer.KindBool:
		g.pf("%sdst = odjsonrt.AppendBool(dst, bool(%s))", ind(n), src)
	case analyzer.KindInt:
		g.pf("%sdst = odjsonrt.AppendInt(dst, int64(%s))", ind(n), src)
	case analyzer.KindUint:
		g.pf("%sdst = odjsonrt.AppendUint(dst, uint64(%s))", ind(n), src)
	case analyzer.KindFloat:
		g.pf("%sdst, err = odjsonrt.AppendFloat(dst, float64(%s), %d)", ind(n), src, t.Bits)
		g.encErr(n)
	case analyzer.KindString:
		g.pf("%sdst = odjsonrt.AppendString(dst, string(%s), %s)", ind(n), src, g.htmlLit())
	case analyzer.KindBytes:
		g.pf("%sdst = odjsonrt.AppendBase64(dst, []byte(%s))", ind(n), src)
	case analyzer.KindSlice:
		i := g.tmp("i")
		g.pf("%sif %s == nil {", ind(n), src)
		g.pf("%sdst = append(dst, 'n', 'u', 'l', 'l')", ind(n+1))
		g.pf("%s} else {", ind(n))
		g.pf("%sdst = append(dst, '[')", ind(n+1))
		g.pf("%sfor %s := range %s {", ind(n+1), i, src)
		g.pf("%sif %s > 0 {", ind(n+2), i)
		g.pf("%sdst = append(dst, ',')", ind(n+3))
		g.pf("%s}", ind(n+2))
		g.encode(t.Elem, fmt.Sprintf("%s[%s]", src, i), true, n+2)
		g.pf("%s}", ind(n+1))
		g.pf("%sdst = append(dst, ']')", ind(n+1))
		g.pf("%s}", ind(n))
	case analyzer.KindArray:
		i := g.tmp("i")
		g.pf("%sdst = append(dst, '[')", ind(n))
		g.pf("%sfor %s := 0; %s < %d; %s++ {", ind(n), i, i, t.Len, i)
		g.pf("%sif %s > 0 {", ind(n+1), i)
		g.pf("%sdst = append(dst, ',')", ind(n+2))
		g.pf("%s}", ind(n+1))
		g.encode(t.Elem, fmt.Sprintf("%s[%s]", src, i), true, n+1)
		g.pf("%s}", ind(n))
		g.pf("%sdst = append(dst, ']')", ind(n))
	case analyzer.KindMap:
		g.pkg.Imports.Add("slices", "slices")
		keys, k, i, mv := g.tmp("keys"), g.tmp("k"), g.tmp("i"), g.tmp("mv")
		g.pf("%sif %s == nil {", ind(n), src)
		g.pf("%sdst = append(dst, 'n', 'u', 'l', 'l')", ind(n+1))
		g.pf("%s} else {", ind(n))
		g.pf("%s%s := make([]string, 0, len(%s))", ind(n+1), keys, src)
		g.pf("%sfor %s := range %s {", ind(n+1), k, src)
		g.pf("%s%s = append(%s, string(%s))", ind(n+2), keys, keys, k)
		g.pf("%s}", ind(n+1))
		g.pf("%sslices.Sort(%s)", ind(n+1), keys)
		g.pf("%sdst = append(dst, '{')", ind(n+1))
		g.pf("%sfor %s, %s := range %s {", ind(n+1), i, k, keys)
		g.pf("%sif %s > 0 {", ind(n+2), i)
		g.pf("%sdst = append(dst, ',')", ind(n+3))
		g.pf("%s}", ind(n+2))
		g.pf("%sdst = odjsonrt.AppendString(dst, %s, %s)", ind(n+2), k, g.htmlLit())
		g.pf("%sdst = append(dst, ':')", ind(n+2))
		g.pf("%s%s := %s[%s]", ind(n+2), mv, src, convert(t.Key.Expr, k))
		g.encode(t.Elem, mv, true, n+2)
		g.pf("%s}", ind(n+1))
		g.pf("%sdst = append(dst, '}')", ind(n+1))
		g.pf("%s}", ind(n))
	case analyzer.KindStruct:
		if !addressable {
			tv := g.tmp("sv")
			g.pf("%s%s := %s", ind(n), tv, src)
			src = tv
		}
		if t.Struct.Local {
			g.pf("%sdst, err = %s.odjsonAppend(dst)", ind(n), src)
		} else {
			g.pf("%sdst, err = %sAppend(dst, %s)", ind(n), t.Struct.Helper, addr(src))
		}
		g.encErr(n)
	default:
		if t.Interface {
			// A nil interface is by far the most common case in decoded
			// documents; keeping it out of the reflection fallback is what
			// makes wide, sparsely populated structs fast.
			g.pf("%sif %s == nil {", ind(n), src)
			g.pf("%sdst = append(dst, 'n', 'u', 'l', 'l')", ind(n+1))
			g.pf("%s} else {", ind(n))
			g.pf("%sdst, err = odjsonrt.AppendAny(dst, %s, %s)", ind(n+1), src, g.htmlLit())
			g.encErr(n + 1)
			g.pf("%s}", ind(n))
			return
		}
		g.pf("%sdst, err = odjsonrt.AppendAny(dst, %s, %s)", ind(n), src, g.htmlLit())
		g.encErr(n)
	}
}

// convert renders a conversion of expr to the named type, avoiding a no-op
// conversion when the type is already the plain builtin.
func convert(expr, val string) string {
	if expr == "string" {
		return val
	}
	return expr + "(" + val + ")"
}

// nonEmptyExpr renders the condition under which "omitempty" keeps the field,
// following encoding/json's definition of empty.
func (g *generator) nonEmptyExpr(t *analyzer.Type, src string) string {
	switch t.Kind {
	case analyzer.KindBool:
		return "bool(" + src + ")"
	case analyzer.KindInt, analyzer.KindUint, analyzer.KindFloat:
		return src + " != 0"
	case analyzer.KindString, analyzer.KindBytes, analyzer.KindSlice,
		analyzer.KindMap, analyzer.KindRawMessage, analyzer.KindNumber:
		return "len(" + src + ") != 0"
	case analyzer.KindPointer:
		return src + " != nil"
	case analyzer.KindAny:
		if t.Interface {
			return src + " != nil"
		}
		return "true"
	case analyzer.KindArray:
		if t.Len == 0 {
			return "false"
		}
		return "true"
	default:
		return "true"
	}
}

// nonZeroExpr renders the condition under which "omitzero" keeps the field.
func (g *generator) nonZeroExpr(t *analyzer.Type, src string) string {
	switch {
	case t.Kind == analyzer.KindPointer || t.Interface:
		return src + " != nil"
	case t.Kind == analyzer.KindSlice || t.Kind == analyzer.KindMap || t.Kind == analyzer.KindBytes:
		return src + " != nil"
	case t.HasIsZero:
		return "!" + src + ".IsZero()"
	case t.Comparable:
		return src + " != " + zeroLiteral(t)
	default:
		g.pkg.Imports.Add("reflect", "reflect")
		return "!reflect.ValueOf(" + src + ").IsZero()"
	}
}

func zeroLiteral(t *analyzer.Type) string {
	switch t.Kind {
	case analyzer.KindBool:
		return "false"
	case analyzer.KindInt, analyzer.KindUint, analyzer.KindFloat:
		return "0"
	case analyzer.KindString, analyzer.KindNumber:
		return `""`
	default:
		// Parenthesised: a bare composite literal is not allowed in an if
		// condition.
		return "(" + t.Expr + "{})"
	}
}

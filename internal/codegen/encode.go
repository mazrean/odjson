package codegen

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/token"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/mazrean/odjson/internal/analyzer"
)

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

// mode is the StringMode parameter threaded through the generated encoders.
var mode = id("m")

// htmlLit renders the escapeHTML argument for the runtime helpers that still
// take a bool.
func (g *generator) htmlLit() ast.Expr { return call(sel(mode, "EscapeHTML")) }

// v1Mode is the StringMode the encoding/json entry points use.
func (g *generator) v1Mode() ast.Expr {
	if g.opts.EscapeHTML {
		return rt("ModeHTML")
	}
	return rt("ModePlain")
}

// appendBytes renders dst = append(dst, args...).
func appendBytes(args ...ast.Expr) ast.Stmt {
	return assign(dst, call(id("append"), append([]ast.Expr{dst}, args...)...))
}

// appendChars renders dst = append(dst, 'a', 'b', ...).
func appendChars(chars string) ast.Stmt {
	var args []ast.Expr
	for i := 0; i < len(chars); i++ {
		args = append(args, chr(chars[i]))
	}
	return appendBytes(args...)
}

// appendChecked renders dst, err = call.
func appendChecked(call ast.Expr) ast.Stmt {
	return assignN(token.ASSIGN, []ast.Expr{dst, errV}, call)
}

// owed is what the members written so far still owe the output: literal
// text, and, when the last of them has two shapes, that member itself.
//
// A struct's encoder writes its braces, separators, member names and the
// quotes around its strings as literals, and every append of one is a
// capacity test and a length update; carried forward rather than written as
// soon as it is known, all the text between one runtime call and the next
// goes out as a single append. A bool, a nil-able pointer, slice, map,
// []byte or interface is written by a branch, so it cannot take text on the
// way a literal can: it is held back instead, with the text before it, and
// flushed as an if/else whose two arms each write that text, their own shape
// and the text that came due after them. That is what lets a bool or a
// null cost one append rather than two, and a run of them one each.
type owed struct {
	// text is the literal owed before def, or all of it when def is nil.
	text string
	def  *deferred
	// tail is the literal owed after def.
	tail string
}

// deferred is a member with two shapes, held back until the text after it
// is known. yes and no write the two arms, each preceded by pre and
// followed by tail.
type deferred struct {
	cond    ast.Expr
	yes, no func(b *block, pre, tail string)
}

// add takes on text that is owed after everything so far.
func (o *owed) add(s string) {
	if o.def == nil {
		o.text += s
	} else {
		o.tail += s
	}
}

// flush writes what o owes.
func (g *generator) flush(b *block, o owed) {
	if o.def == nil {
		g.appendLit(b, o.text)
		return
	}
	s := g.ifStmt(b, nil, o.def.cond, func(b *block) { o.def.yes(b, o.text, o.tail) })
	g.elseBlock(s, func(b *block) { o.def.no(b, o.text, o.tail) })
}

// hold takes a two-shaped member on. Only literal text can be folded into
// its arms, so a deferred member already pending is flushed first, with the
// new member's name as its tail.
func (g *generator) hold(b *block, pending owed, d *deferred) owed {
	if pending.def != nil {
		g.flush(b, pending)
		return owed{def: d}
	}
	return owed{text: pending.text, def: d}
}

// litArm renders an arm that writes nothing but literal text.
func (g *generator) litArm(text string) func(b *block, pre, tail string) {
	return func(b *block, pre, tail string) { g.appendLit(b, pre+text+tail) }
}

// nilArm renders the arm for a nil slice, map or []byte: encoding/json
// writes null, encoding/json/v2 an empty container.
func (g *generator) nilArm(v2 string) func(b *block, pre, tail string) {
	return func(b *block, pre, tail string) {
		s := g.ifStmt(b, nil, call(sel(mode, "V2")), func(b *block) {
			g.appendLit(b, pre+v2+tail)
		})
		g.elseBlock(s, func(b *block) {
			g.appendLit(b, pre+"null"+tail)
		})
	}
}

func (g *generator) encodeStruct(b *block, s *analyzer.StructInfo) {
	g.emit(b, varDecl("err", id("error")))
	g.emit(b, assign(id("_"), errV))
	conds := make([][]ast.Expr, len(s.Fields))
	fixed := true
	for i, f := range s.Fields {
		conds[i] = g.memberConds(f)
		fixed = fixed && len(conds[i]) == 0
	}
	// Every member is written or none is, so the shape of the object is
	// known here: the opening brace goes out with the first member's name
	// and the closing one is appended unconditionally. The general form
	// below writes a comma before every member and patches the first one
	// into a brace afterwards, which costs a branch and a store per
	// object; a struct without omitempty, omitzero or a nil-able embedded
	// pointer never needs that, and most structs are that.
	// Literal text is carried forward rather than written as soon as it is
	// known (see owed): pending is what the members written so far still
	// owe the output. Only an unconditional member can take it on, since a
	// conditional one may write nothing at all.
	if fixed {
		if len(s.Fields) == 0 {
			g.emit(b, appendChars("{}"))
			g.emit(b, ret(dst, nilV))
			return
		}
		p := g.encodeFixed(b, s, v, owed{text: "{"})
		p.add("}")
		g.flush(b, p)
		g.emit(b, ret(dst, nilV))
		return
	}
	start := id("start")
	g.emit(b, define(start, call(id("len"), dst)))
	var pending owed
	written := false
	for i, f := range s.Fields {
		if len(conds[i]) == 0 {
			pending.add(",")
			pending = g.encodeFusedMember(b, f, v, pending)
			written = true
			continue
		}
		g.flush(b, pending)
		pending = owed{}
		g.ifStmt(b, nil, and(conds[i]...), func(b *block) {
			g.flush(b, g.encodeFusedMember(b, f, v, owed{text: ","}))
		})
	}
	if written {
		// An unconditional member was written, so the object is not empty.
		g.emit(b, assign(index(dst, start), chr('{')))
		pending.add("}")
		g.flush(b, pending)
		g.emit(b, ret(dst, nilV))
		return
	}
	s0 := g.ifStmt(b, nil, bin(call(id("len"), dst), token.EQL, start), func(b *block) {
		g.emit(b, appendChars("{}"))
	})
	g.elseBlock(s0, func(b *block) {
		g.emit(b, assign(index(dst, start), chr('{')))
		g.emit(b, appendChars("}"))
	})
	g.emit(b, ret(dst, nilV))
}

// encodeFixed writes every member of s, none of which may be conditional,
// reading the fields from base. pending is what is owed before the first
// member (the opening brace, and whatever the caller still had pending);
// the result is what is owed after the last one.
func (g *generator) encodeFixed(b *block, s *analyzer.StructInfo, base ast.Expr, pending owed) owed {
	for i, f := range s.Fields {
		if i > 0 {
			pending.add(",")
		}
		pending = g.encodeFusedMember(b, f, base, pending)
	}
	return pending
}

// encodeFusedMember writes member f of base, preceded by what is owed so far
// (separator included) and the member's name, and returns what is owed
// afterwards: a closing quote for a plain string, the closing brace and
// whatever the last member owed for a nested object that was spliced in,
// the member itself when it has two shapes, and nothing otherwise.
func (g *generator) encodeFusedMember(b *block, f *analyzer.Field, base ast.Expr, pending owed) owed {
	pending.add(jsonString(f.JSONName, g.opts.EscapeHTML) + ":")
	src := selectorFrom(base, f)
	if f.AsString {
		g.flush(b, pending)
		g.encodeQuoted(b, f.Type, src, true)
		return owed{}
	}
	return g.encodeFused(b, f.Type, src, true, pending)
}

// maxInlineMembers bounds the nested structs whose members are spliced into
// the parent's encoder instead of being reached through a call: the call
// itself costs next to nothing, what the splice buys is the fusion of the
// literals across the boundary, and a wide struct's body repeated in every
// parent would cost more code than that is worth.
const maxInlineMembers = 4

// inlinableStruct reports whether a value of type t is a small, locally
// generated struct with no conditional member and no marshaler of its own,
// so that its encoder body can be spliced into the caller. Only one level
// is spliced: a nested struct inside a spliced one goes through its call,
// which is also what keeps a self-referential type finite.
func (g *generator) inlinableStruct(t *analyzer.Type) bool {
	if g.inlineDepth > 0 || t.Kind != analyzer.KindStruct || t.Struct == nil || !t.Struct.Local {
		return false
	}
	if t.Marshaler || t.PtrMarshaler || t.TextMarshaler || t.PtrTextMarshaler {
		return false
	}
	s := t.Struct
	if len(s.Fields) == 0 || len(s.Fields) > maxInlineMembers {
		return false
	}
	for _, f := range s.Fields {
		if len(g.memberConds(f)) != 0 {
			return false
		}
	}
	return true
}

// memberConds renders the conditions under which f is written at all: the
// embedded pointers on its path being non-nil, and omitzero / omitempty.
// An empty result means the member is always written.
func (g *generator) memberConds(f *analyzer.Field) []ast.Expr {
	var conds []ast.Expr
	if gd := guard(f); gd != nil {
		conds = append(conds, gd)
	}
	if f.OmitZero {
		if c := g.nonZeroExpr(f.Type, selector(f)); c != nil {
			conds = append(conds, c)
		}
	}
	if f.OmitEmpty {
		if c := nonEmptyExpr(f.Type, selector(f)); c != nil {
			// encoding/json omits a zero number or a false bool;
			// encoding/json/v2 only omits values that encode as "",
			// [], {} or null, so under ModeStream they stay.
			if alwaysKeptByV2(f.Type) {
				c = paren(bin(call(sel(mode, "V2")), token.LOR, c))
			}
			conds = append(conds, c)
		}
	}
	return conds
}

// appendLit emits dst = append(dst, lit...), in pieces of at most sixteen
// bytes. The compiler turns an append of a constant that long into a couple
// of moves it emits inline; a longer one becomes a call to memmove, and a
// struct's member names are appended often enough for those calls to show
// in the encode profile.
func (g *generator) appendLit(b *block, lit string) {
	const piece = 16
	for len(lit) > 0 {
		n := min(len(lit), piece)
		// Back off to a rune boundary, so that a non-ASCII name is still
		// readable in the generated file.
		for n < len(lit) && !utf8.RuneStart(lit[n]) {
			n--
		}
		g.emit(b, assign(dst, spread(id("append"), dst, str(lit[:n]))))
		lit = lit[n:]
	}
}

// encodeQuoted implements the ",string" tag option.
func (g *generator) encodeQuoted(b *block, t *analyzer.Type, src ast.Expr, addressable bool) {
	switch t.Kind {
	case analyzer.KindPointer:
		s := g.ifStmt(b, nil, bin(src, token.EQL, nilV), func(b *block) {
			g.emit(b, appendChars("null"))
		})
		g.elseBlock(s, func(b *block) {
			g.encodeQuoted(b, t.Elem, deref(src), true)
		})
	case analyzer.KindString:
		g.emit(b, assign(dst, callRT("AppendStringQuotedMode", dst, call(id("string"), src), mode)))
	default:
		p := g.encodeFused(b, t, src, addressable, owed{text: `"`})
		p.add(`"`)
		g.flush(b, p)
	}
}

func (g *generator) encErr(b *block) {
	g.ifStmt(b, nil, bin(errV, token.NEQ, nilV), func(b *block) {
		g.emit(b, ret(nilV, errV))
	})
}

// encode writes the statements appending the JSON encoding of src to dst.
func (g *generator) encode(b *block, t *analyzer.Type, src ast.Expr, addressable bool) {
	g.flush(b, g.encodeFused(b, t, src, addressable, owed{}))
}

// leaf reports whether t is written by a runtime helper that takes no
// literal on: a number, a raw message, or a type with a marshaler of its
// own. A pointer is not, whatever it points to: its nil test is a shape of
// its own.
func leaf(t *analyzer.Type, addressable bool) bool {
	switch t.Kind {
	case analyzer.KindPointer:
		return false
	case analyzer.KindRawMessage, analyzer.KindNumber, analyzer.KindInt, analyzer.KindUint, analyzer.KindFloat:
		return true
	}
	if t.Marshaler || t.TextMarshaler || addressable && (t.PtrMarshaler || t.PtrTextMarshaler) {
		return true
	}
	return t.Kind == analyzer.KindAny && !t.Interface
}

// encodeLeaf writes a value leaf reports for.
func (g *generator) encodeLeaf(b *block, t *analyzer.Type, src ast.Expr, addressable bool) {
	switch {
	case t.Kind == analyzer.KindRawMessage:
		g.emit(b, appendChecked(callRT("AppendRaw", dst, call(sliceType(id("byte")), src), g.htmlLit())))
		g.encErr(b)
	case t.Kind == analyzer.KindNumber:
		g.emit(b, appendChecked(callRT("AppendNumber", dst, call(id("string"), src))))
		g.encErr(b)
	case t.Marshaler:
		g.emit(b, appendChecked(callRT("AppendMarshaler", dst, src, g.htmlLit())))
		g.encErr(b)
	case t.PtrMarshaler && addressable:
		g.emit(b, appendChecked(callRT("AppendMarshaler", dst, addr(src), g.htmlLit())))
		g.encErr(b)
	case t.TextMarshaler:
		g.emit(b, appendChecked(callRT("AppendTextMarshaler", dst, src, g.htmlLit())))
		g.encErr(b)
	case t.PtrTextMarshaler && addressable:
		g.emit(b, appendChecked(callRT("AppendTextMarshaler", dst, addr(src), g.htmlLit())))
		g.encErr(b)
	case t.Kind == analyzer.KindInt:
		g.emit(b, assign(dst, callRT("AppendInt", dst, call(id("int64"), src))))
	case t.Kind == analyzer.KindUint:
		g.emit(b, assign(dst, callRT("AppendUint", dst, call(id("uint64"), src))))
	case t.Kind == analyzer.KindFloat:
		g.emit(b, appendChecked(callRT("AppendFloat", dst, call(id("float64"), src), num(int64(t.Bits)))))
		g.encErr(b)
	default:
		g.emit(b, appendChecked(callRT("AppendAnyMode", dst, src, mode)))
		g.encErr(b)
	}
}

// encodeFused writes the value src of type t, preceded by what pending
// owes, and returns what is owed afterwards (see owed).
func (g *generator) encodeFused(b *block, t *analyzer.Type, src ast.Expr, addressable bool, pending owed) owed {
	if leaf(t, addressable) {
		g.flush(b, pending)
		g.encodeLeaf(b, t, src, addressable)
		return owed{}
	}
	switch t.Kind {
	case analyzer.KindPointer:
		return g.hold(b, pending, &deferred{
			cond: bin(src, token.EQL, nilV),
			yes:  g.litArm("null"),
			no: func(b *block, pre, tail string) {
				p := g.encodeFused(b, t.Elem, deref(src), true, owed{text: pre})
				p.add(tail)
				g.flush(b, p)
			},
		})
	case analyzer.KindBool:
		return g.hold(b, pending, &deferred{
			cond: call(id("bool"), src),
			yes:  g.litArm("true"),
			no:   g.litArm("false"),
		})
	case analyzer.KindString:
		// The body helper rather than the quoted one, so that the quotes
		// join the literals on either side; a string is as common as a
		// member gets, and a wrapper around the body would be a second
		// call for each.
		pending.add(`"`)
		g.flush(b, pending)
		g.emit(b, appendChecked(callRT("AppendStringBodyChecked", dst, call(id("string"), src), mode)))
		g.encErr(b)
		return owed{text: `"`}
	case analyzer.KindBytes:
		return g.hold(b, pending, &deferred{
			cond: bin(src, token.EQL, nilV),
			yes:  g.nilArm(`""`),
			no: func(b *block, pre, tail string) {
				g.appendLit(b, pre)
				g.emit(b, assign(dst, callRT("AppendBase64", dst, call(sliceType(id("byte")), src))))
				g.appendLit(b, tail)
			},
		})
	case analyzer.KindSlice:
		i := id(g.tmp("i"))
		return g.hold(b, pending, &deferred{
			cond: bin(src, token.EQL, nilV),
			yes:  g.nilArm("[]"),
			no: func(b *block, pre, tail string) {
				g.appendLit(b, pre+"[")
				g.rangeStmt(b, i, nil, src, func(b *block) {
					g.ifStmt(b, nil, bin(i, token.GTR, num(0)), func(b *block) {
						g.emit(b, appendChars(","))
					})
					g.encode(b, t.Elem, index(src, i), true)
				})
				g.appendLit(b, "]"+tail)
			},
		})
	case analyzer.KindArray:
		i := id(g.tmp("i"))
		pending.add("[")
		g.flush(b, pending)
		g.rangeStmt(b, i, nil, num(t.Len), func(b *block) {
			g.ifStmt(b, nil, bin(i, token.GTR, num(0)), func(b *block) {
				g.emit(b, appendChars(","))
			})
			g.encode(b, t.Elem, index(src, i), true)
		})
		return owed{text: "]"}
	case analyzer.KindMap:
		g.pkg.Imports.Add("slices", "slices")
		keys, k, i, mv := id(g.tmp("keys")), id(g.tmp("k")), id(g.tmp("i")), id(g.tmp("mv"))
		return g.hold(b, pending, &deferred{
			cond: bin(src, token.EQL, nilV),
			yes:  g.nilArm("{}"),
			no: func(b *block, pre, tail string) {
				g.emit(b, define(keys, call(id("make"), sliceType(id("string")), num(0), call(id("len"), src))))
				g.rangeStmt(b, k, nil, src, func(b *block) {
					g.emit(b, assign(keys, call(id("append"), keys, call(id("string"), k))))
				})
				g.emit(b, expr(call(sel(id("slices"), "Sort"), keys)))
				g.appendLit(b, pre+"{")
				g.rangeStmt(b, i, k, keys, func(b *block) {
					g.ifStmt(b, nil, bin(i, token.GTR, num(0)), func(b *block) {
						g.emit(b, appendChars(","))
					})
					g.emit(b, appendChars("\""))
					g.emit(b, appendChecked(callRT("AppendStringBodyChecked", dst, k, mode)))
					g.encErr(b)
					g.emit(b, define(mv, index(src, conv(t.Key.Expr, k))))
					g.flush(b, g.encodeFused(b, t.Elem, mv, true, owed{text: `":`}))
				})
				g.appendLit(b, "}"+tail)
			},
		})
	case analyzer.KindStruct:
		if !addressable {
			tv := id(g.tmp("sv"))
			g.emit(b, define(tv, src))
			src = tv
		}
		if g.inlinableStruct(t) {
			// The nested object's members are written here, so its
			// braces and its first member's name join the literal before
			// it, and its last member's closing quote, if any, the one
			// after.
			g.inlineDepth++
			pending.add("{")
			p := g.encodeFixed(b, t.Struct, src, pending)
			g.inlineDepth--
			p.add("}")
			return p
		}
		g.flush(b, pending)
		if t.Struct.Local {
			g.emit(b, appendChecked(call(sel(src, "odjsonAppend"), dst, mode)))
		} else {
			g.emit(b, appendChecked(call(id(t.Struct.Helper+"Append"), dst, addr(src), mode)))
		}
		g.encErr(b)
		return owed{}
	default:
		// An interface. A nil one is by far the most common case in
		// decoded documents; keeping it out of the reflection fallback is
		// what makes wide, sparsely populated structs fast.
		return g.hold(b, pending, &deferred{
			cond: bin(src, token.EQL, nilV),
			yes:  g.litArm("null"),
			no: func(b *block, pre, tail string) {
				g.appendLit(b, pre)
				g.emit(b, appendChecked(callRT("AppendAnyMode", dst, src, mode)))
				g.encErr(b)
				g.appendLit(b, tail)
			},
		})
	}
}

// nonEmptyExpr renders the condition under which "omitempty" keeps the field,
// following encoding/json's definition of empty, or nil when the field is
// always kept.
func nonEmptyExpr(t *analyzer.Type, src ast.Expr) ast.Expr {
	switch t.Kind {
	case analyzer.KindBool:
		return call(id("bool"), src)
	case analyzer.KindInt, analyzer.KindUint, analyzer.KindFloat:
		return bin(src, token.NEQ, num(0))
	case analyzer.KindString, analyzer.KindBytes, analyzer.KindSlice,
		analyzer.KindMap, analyzer.KindRawMessage, analyzer.KindNumber:
		return bin(call(id("len"), src), token.NEQ, num(0))
	case analyzer.KindPointer:
		return bin(src, token.NEQ, nilV)
	case analyzer.KindAny:
		if t.Interface {
			return bin(src, token.NEQ, nilV)
		}
		return nil
	case analyzer.KindArray:
		if t.Len == 0 {
			return id("false")
		}
		return nil
	default:
		return nil
	}
}

// alwaysKeptByV2 reports whether encoding/json/v2 keeps a field that
// encoding/json would drop under omitempty.
func alwaysKeptByV2(t *analyzer.Type) bool {
	switch t.Kind {
	case analyzer.KindBool, analyzer.KindInt, analyzer.KindUint, analyzer.KindFloat:
		return true
	default:
		return false
	}
}

// nonZeroExpr renders the condition under which "omitzero" keeps the field.
func (g *generator) nonZeroExpr(t *analyzer.Type, src ast.Expr) ast.Expr {
	switch {
	case t.Kind == analyzer.KindPointer || t.Interface:
		return bin(src, token.NEQ, nilV)
	case t.Kind == analyzer.KindSlice || t.Kind == analyzer.KindMap || t.Kind == analyzer.KindBytes:
		return bin(src, token.NEQ, nilV)
	case t.HasIsZero:
		return not(call(sel(src, "IsZero")))
	case t.Comparable:
		return bin(src, token.NEQ, zeroLiteral(t))
	default:
		g.pkg.Imports.Add("reflect", "reflect")
		return not(call(sel(call(sel(id("reflect"), "ValueOf"), src), "IsZero")))
	}
}

func zeroLiteral(t *analyzer.Type) ast.Expr {
	switch t.Kind {
	case analyzer.KindBool:
		return id("false")
	case analyzer.KindInt, analyzer.KindUint, analyzer.KindFloat:
		return num(0)
	case analyzer.KindString, analyzer.KindNumber:
		return str("")
	default:
		// Parenthesised: a bare composite literal is not allowed in an if
		// condition.
		return paren(composite(typ(t.Expr)))
	}
}

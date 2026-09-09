package codegen

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/token"
	"strconv"
	"strings"

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

func (g *generator) encodeStruct(b *block, s *analyzer.StructInfo) {
	start := id("start")
	g.emit(b, varDecl("err", id("error")))
	g.emit(b, assign(id("_"), errV))
	g.emit(b, define(start, call(id("len"), dst)))
	for _, f := range s.Fields {
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
		body := func(b *block) {
			g.appendLit(b, ","+jsonString(f.JSONName, g.opts.EscapeHTML)+":")
			if f.AsString {
				g.encodeQuoted(b, f.Type, selector(f), true)
			} else {
				g.encode(b, f.Type, selector(f), true)
			}
		}
		if len(conds) > 0 {
			g.ifStmt(b, nil, and(conds...), body)
		} else {
			body(b)
		}
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

// appendLit emits dst = append(dst, lit...), in pieces of at most sixteen
// bytes. The compiler turns an append of a constant that long into a couple
// of moves it emits inline; a longer one becomes a call to memmove, and a
// struct's member names are appended often enough for those calls to show
// in the encode profile.
func (g *generator) appendLit(b *block, lit string) {
	const piece = 16
	for len(lit) > 0 {
		n := min(len(lit), piece)
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
		g.emit(b, appendChars(`"`))
		g.encode(b, t, src, addressable)
		g.emit(b, appendChars(`"`))
	}
}

func (g *generator) encErr(b *block) {
	g.ifStmt(b, nil, bin(errV, token.NEQ, nilV), func(b *block) {
		g.emit(b, ret(nilV, errV))
	})
}

// encode writes the statements appending the JSON encoding of src to dst.
func (g *generator) encode(b *block, t *analyzer.Type, src ast.Expr, addressable bool) {
	switch t.Kind {
	case analyzer.KindRawMessage:
		g.emit(b, appendChecked(callRT("AppendRaw", dst, call(sliceType(id("byte")), src), g.htmlLit())))
		g.encErr(b)
		return
	case analyzer.KindNumber:
		g.emit(b, appendChecked(callRT("AppendNumber", dst, call(id("string"), src))))
		g.encErr(b)
		return
	case analyzer.KindPointer:
		s := g.ifStmt(b, nil, bin(src, token.EQL, nilV), func(b *block) {
			g.emit(b, appendChars("null"))
		})
		g.elseBlock(s, func(b *block) {
			g.encode(b, t.Elem, deref(src), true)
		})
		return
	}

	switch {
	case t.Marshaler:
		g.emit(b, appendChecked(callRT("AppendMarshaler", dst, src, g.htmlLit())))
		g.encErr(b)
		return
	case t.PtrMarshaler && addressable:
		g.emit(b, appendChecked(callRT("AppendMarshaler", dst, addr(src), g.htmlLit())))
		g.encErr(b)
		return
	case t.TextMarshaler:
		g.emit(b, appendChecked(callRT("AppendTextMarshaler", dst, src, g.htmlLit())))
		g.encErr(b)
		return
	case t.PtrTextMarshaler && addressable:
		g.emit(b, appendChecked(callRT("AppendTextMarshaler", dst, addr(src), g.htmlLit())))
		g.encErr(b)
		return
	}

	switch t.Kind {
	case analyzer.KindBool:
		g.emit(b, assign(dst, callRT("AppendBool", dst, call(id("bool"), src))))
	case analyzer.KindInt:
		g.emit(b, assign(dst, callRT("AppendInt", dst, call(id("int64"), src))))
	case analyzer.KindUint:
		g.emit(b, assign(dst, callRT("AppendUint", dst, call(id("uint64"), src))))
	case analyzer.KindFloat:
		g.emit(b, appendChecked(callRT("AppendFloat", dst, call(id("float64"), src), num(int64(t.Bits)))))
		g.encErr(b)
	case analyzer.KindString:
		g.emit(b, appendChecked(callRT("AppendStringChecked", dst, call(id("string"), src), mode)))
		g.encErr(b)
	case analyzer.KindBytes:
		s := g.ifStmt(b, nil, bin(src, token.EQL, nilV), func(b *block) {
			g.emit(b, assign(dst, callRT("AppendNilBytes", dst, mode)))
		})
		g.elseBlock(s, func(b *block) {
			g.emit(b, assign(dst, callRT("AppendBase64", dst, call(sliceType(id("byte")), src))))
		})
	case analyzer.KindSlice:
		i := id(g.tmp("i"))
		s := g.ifStmt(b, nil, bin(src, token.EQL, nilV), func(b *block) {
			g.emit(b, assign(dst, callRT("AppendNilSlice", dst, mode)))
		})
		g.elseBlock(s, func(b *block) {
			g.emit(b, appendChars("["))
			g.rangeStmt(b, i, nil, src, func(b *block) {
				g.ifStmt(b, nil, bin(i, token.GTR, num(0)), func(b *block) {
					g.emit(b, appendChars(","))
				})
				g.encode(b, t.Elem, index(src, i), true)
			})
			g.emit(b, appendChars("]"))
		})
	case analyzer.KindArray:
		i := id(g.tmp("i"))
		g.emit(b, appendChars("["))
		g.forStmt(b, define(i, num(0)), bin(i, token.LSS, num(t.Len)), incr(i), func(b *block) {
			g.ifStmt(b, nil, bin(i, token.GTR, num(0)), func(b *block) {
				g.emit(b, appendChars(","))
			})
			g.encode(b, t.Elem, index(src, i), true)
		})
		g.emit(b, appendChars("]"))
	case analyzer.KindMap:
		g.pkg.Imports.Add("slices", "slices")
		keys, k, i, mv := id(g.tmp("keys")), id(g.tmp("k")), id(g.tmp("i")), id(g.tmp("mv"))
		s := g.ifStmt(b, nil, bin(src, token.EQL, nilV), func(b *block) {
			g.emit(b, assign(dst, callRT("AppendNilMap", dst, mode)))
		})
		g.elseBlock(s, func(b *block) {
			g.emit(b, define(keys, call(id("make"), sliceType(id("string")), num(0), call(id("len"), src))))
			g.rangeStmt(b, k, nil, src, func(b *block) {
				g.emit(b, assign(keys, call(id("append"), keys, call(id("string"), k))))
			})
			g.emit(b, expr(call(sel(id("slices"), "Sort"), keys)))
			g.emit(b, appendChars("{"))
			g.rangeStmt(b, i, k, keys, func(b *block) {
				g.ifStmt(b, nil, bin(i, token.GTR, num(0)), func(b *block) {
					g.emit(b, appendChars(","))
				})
				g.emit(b, appendChecked(callRT("AppendStringChecked", dst, k, mode)))
				g.encErr(b)
				g.emit(b, appendChars(":"))
				g.emit(b, define(mv, index(src, conv(t.Key.Expr, k))))
				g.encode(b, t.Elem, mv, true)
			})
			g.emit(b, appendChars("}"))
		})
	case analyzer.KindStruct:
		if !addressable {
			tv := id(g.tmp("sv"))
			g.emit(b, define(tv, src))
			src = tv
		}
		if t.Struct.Local {
			g.emit(b, appendChecked(call(sel(src, "odjsonAppend"), dst, mode)))
		} else {
			g.emit(b, appendChecked(call(id(t.Struct.Helper+"Append"), dst, addr(src), mode)))
		}
		g.encErr(b)
	default:
		if t.Interface {
			// A nil interface is by far the most common case in decoded
			// documents; keeping it out of the reflection fallback is what
			// makes wide, sparsely populated structs fast.
			s := g.ifStmt(b, nil, bin(src, token.EQL, nilV), func(b *block) {
				g.emit(b, appendChars("null"))
			})
			g.elseBlock(s, func(b *block) {
				g.emit(b, appendChecked(callRT("AppendAnyMode", dst, src, mode)))
				g.encErr(b)
			})
			return
		}
		g.emit(b, appendChecked(callRT("AppendAnyMode", dst, src, mode)))
		g.encErr(b)
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

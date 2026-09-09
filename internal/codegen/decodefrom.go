package codegen

import (
	"go/ast"
	"go/token"

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

// Identifiers the jsontext driven decoders share.
var (
	dec    = id("dec")
	cacheV = id(cache)
)

// readToken renders if _, err = dec.ReadToken(); err != nil { return err }.
func (g *generator) readToken(b *block) {
	g.ifStmt(b, assignN(token.ASSIGN, []ast.Expr{id("_"), errV}, call(sel(dec, "ReadToken"))), bin(errV, token.NEQ, nilV), func(b *block) {
		g.emit(b, ret(errV))
	})
}

// nextKind renders odjsonrt.NextKind(dec).
func nextKind() ast.Expr { return callRT("NextKind", dec) }

// jsontextValue renders jsontext.Value.
func jsontextValue() ast.Expr { return sel(id("jsontext"), "Value") }

// decodeStructFrom writes the body of a struct's jsontext driven decoder.
func (g *generator) decodeStructFrom(b *block, s *analyzer.StructInfo) {
	g.emit(b, varDecl("err", id("error")))
	g.emit(b, assign(id("_"), errV))
	// A small value is read whole and handed to the byte oriented decoder:
	// below a few kilobytes the decoder's per call cost outweighs a second
	// scan of the bytes. See odjsonrt.WholeValue for how small is decided.
	g.ifStmt(b, nil, callRT("WholeValue", dec), func(b *block) {
		val := id("val")
		g.emit(b, varDecl("val", jsontextValue()))
		g.ifStmt(b, assignN(token.ASSIGN, []ast.Expr{val, errV}, call(sel(dec, "ReadValue"))), bin(errV, token.NEQ, nilV), func(b *block) {
			g.emit(b, ret(errV))
		})
		if s.Local {
			g.emit(b, assignN(token.ASSIGN, []ast.Expr{id("_"), errV}, call(sel(v, "odjsonParseV2"), val, num(0), cacheV, id("false"))))
		} else {
			g.emit(b, assignN(token.ASSIGN, []ast.Expr{id("_"), errV}, call(id(s.Helper+"ParseV2"), val, v, num(0), cacheV, id("false"))))
		}
		g.emit(b, ret(errV))
	})
	g.switchStmt(b, nextKind(), func(sw *block) {
		g.caseClause(sw, []ast.Expr{chr('n')}, func(b *block) {
			g.readToken(b)
			g.emit(b, assign(ptr(v), composite(typ(s.Expr))))
			g.emit(b, ret(nilV))
		})
		g.caseClause(sw, []ast.Expr{chr('{')}, func(b *block) {})
		g.defaultClause(sw, func(b *block) {
			g.emit(b, ret(callRT("ErrKindFrom", dec, str(s.Expr))))
		})
	})
	g.readToken(b)
	g.forStmt(b, nil, bin(nextKind(), token.NEQ, chr('}')), nil, func(b *block) {
		g.emit(b, varDecl("key", jsontextValue()))
		g.emit(b, assignN(token.ASSIGN, []ast.Expr{key, errV}, call(sel(dec, "ReadValue"))))
		g.ifStmt(b, nil, bin(errV, token.NEQ, nilV), func(b *block) {
			g.emit(b, ret(errV))
		})
		g.emit(b, define(idx, num(-1)))
		if len(s.Fields) > 0 {
			g.pkg.Imports.Add("bytes", "bytes")
			// Fast path: compare against the member name exactly as it appears
			// in the document, which avoids unescaping it at all.
			g.switchStmt(b, call(id("string"), key), func(sw *block) {
				for i, f := range s.Fields {
					g.caseClause(sw, []ast.Expr{str(jsonString(f.JSONName, false))}, func(b *block) {
						g.emit(b, assign(idx, num(int64(i))))
					})
				}
			})
			// Only the escaped spelling of a name needs unquoting; unlike
			// encoding/json, json/v2 does not fall back to a case-insensitive
			// match, so neither does this decoder.
			name, ok := id("name"), id("ok")
			g.ifStmt(b, nil, and(bin(idx, token.LSS, num(0)), bin(call(sel(id("bytes"), "IndexByte"), key, chr('\\')), token.GEQ, num(0))), func(b *block) {
				g.ifStmt(b, assignN(token.DEFINE, []ast.Expr{name, ok}, callRT("UnquoteName", key)), ok, func(b *block) {
					g.switchStmt(b, call(id("string"), name), func(sw *block) {
						for i, f := range s.Fields {
							g.caseClause(sw, []ast.Expr{str(f.JSONName)}, func(b *block) {
								g.emit(b, assign(idx, num(int64(i))))
							})
						}
					})
				})
			})
		}
		g.switchStmt(b, idx, func(sw *block) {
			for i, f := range s.Fields {
				g.caseClause(sw, []ast.Expr{num(int64(i))}, func(b *block) {
					g.allocSteps(b, f)
					if f.AsString {
						g.leafFrom(b, f.Type, selector(f), true)
					} else {
						g.decodeFrom(b, f.Type, selector(f))
					}
				})
			}
			g.defaultClause(sw, func(b *block) {
				g.ifStmt(b, assignN(token.ASSIGN, []ast.Expr{id("_"), errV}, call(sel(dec, "ReadValue"))), bin(errV, token.NEQ, nilV), func(b *block) {
					g.emit(b, ret(errV))
				})
			})
		})
	})
	g.emit(b, assignN(token.ASSIGN, []ast.Expr{id("_"), errV}, call(sel(dec, "ReadToken"))))
	g.emit(b, ret(errV))
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
func zeroLit(t *analyzer.Type) ast.Expr {
	switch t.Kind {
	case analyzer.KindBool:
		return id("false")
	case analyzer.KindInt, analyzer.KindUint, analyzer.KindFloat:
		return num(0)
	case analyzer.KindString, analyzer.KindNumber:
		return str("")
	case analyzer.KindArray, analyzer.KindStruct:
		return composite(typ(t.Expr))
	case analyzer.KindAny:
		if t.Interface {
			return nilV
		}
		return &ast.StarExpr{X: call(id("new"), typ(t.Expr))}
	}
	return nilV
}

// leafFrom reads one complete value out of the decoder and decodes it. Scalars
// go through the value helpers; anything else is handed to the byte oriented
// emitter in trusted mode, since jsontext has already validated it.
func (g *generator) leafFrom(b *block, t *analyzer.Type, target ast.Expr, quoted bool) {
	val := id(g.tmp("val"))
	g.emit(b, varDecl(val.Name, jsontextValue()))
	g.emit(b, assignN(token.ASSIGN, []ast.Expr{val, errV}, call(sel(dec, "ReadValue"))))
	g.ifStmt(b, nil, bin(errV, token.NEQ, nilV), func(b *block) {
		g.emit(b, ret(errV))
	})
	if !quoted && scalarFast(t) {
		g.scalarFrom(b, t, target, val)
		return
	}
	pos := id(g.tmp("vp"))
	g.emit(b, define(pos, num(0)))
	c := ctx{data: val.Name, pos: pos.Name, single: true, trusted: true, trimmed: true, v2: true, cache: cache}
	if quoted {
		g.decodeQuoted(b, t, target, c)
	} else {
		g.decode(b, t, target, c)
	}
	// The decoder has already delimited the value, so where the fragment
	// stopped inside it does not matter. Marking the offset used keeps the
	// generated file free of dead stores.
	g.emit(b, assign(id("_"), pos))
}

// scalarFrom decodes the complete value in val into a scalar target. A null
// stores the zero value, as json/v2 does.
func (g *generator) scalarFrom(b *block, t *analyzer.Type, target ast.Expr, val *ast.Ident) {
	x := id(g.tmp("x"))
	s := g.ifStmt(b, nil, bin(index(val, num(0)), token.EQL, chr('n')), func(b *block) {
		g.emit(b, assign(target, zeroLit(t)))
	})
	g.elseBlock(s, func(b *block) {
		var native string
		switch t.Kind {
		case analyzer.KindBool:
			native = "bool"
			g.emit(b, varDecl(x.Name, id("bool")))
			g.emit(b, assignN(token.ASSIGN, []ast.Expr{x, id("_"), errV}, callRT("ParseBool", val, num(0))))
		case analyzer.KindInt:
			native = "int64"
			g.emit(b, varDecl(x.Name, id("int64")))
			g.emit(b, assignN(token.ASSIGN, []ast.Expr{x, id("_"), errV}, callRT("ParseInt", val, num(0), num(int64(t.Bits)))))
		case analyzer.KindUint:
			native = "uint64"
			g.emit(b, varDecl(x.Name, id("uint64")))
			g.emit(b, assignN(token.ASSIGN, []ast.Expr{x, id("_"), errV}, callRT("ParseUint", val, num(0), num(int64(t.Bits)))))
		case analyzer.KindFloat:
			native = "float64"
			g.emit(b, varDecl(x.Name, id("float64")))
			g.emit(b, assignN(token.ASSIGN, []ast.Expr{x, errV}, callRT("ParseFloatValue", val, num(int64(t.Bits)))))
		case analyzer.KindString:
			native = "string"
			g.emit(b, varDecl(x.Name, id("string")))
			g.emit(b, assignN(token.ASSIGN, []ast.Expr{x, errV}, callRT("ParseStringValue", val, cacheV)))
		}
		g.ifStmt(b, nil, bin(errV, token.NEQ, nilV), func(b *block) {
			g.emit(b, ret(errV))
		})
		if t.Expr == native {
			g.emit(b, assign(target, x))
		} else {
			g.emit(b, assign(target, call(typ(t.Expr), x)))
		}
	})
}

// decodeFrom writes the statements decoding one value out of the decoder into
// target.
func (g *generator) decodeFrom(b *block, t *analyzer.Type, target ast.Expr) {
	if !containsStruct(t) {
		g.leafFrom(b, t, target, false)
		return
	}
	switch t.Kind {
	case analyzer.KindPointer:
		s := g.ifStmt(b, nil, bin(nextKind(), token.EQL, chr('n')), func(b *block) {
			g.readToken(b)
			g.emit(b, assign(target, nilV))
		})
		g.elseBlock(s, func(b *block) {
			g.ifStmt(b, nil, bin(target, token.EQL, nilV), func(b *block) {
				g.emit(b, assign(target, call(id("new"), typ(t.Elem.Expr))))
			})
			g.decodeFrom(b, t.Elem, deref(target))
		})
	case analyzer.KindStruct:
		var parse ast.Expr
		if t.Struct.Local {
			parse = call(sel(target, "odjsonParseFrom"), dec, cacheV)
		} else {
			parse = call(id(t.Struct.Helper+"ParseFrom"), dec, addr(target), cacheV)
		}
		g.ifStmt(b, assign(errV, parse), bin(errV, token.NEQ, nilV), func(b *block) {
			g.emit(b, ret(errV))
		})
	case analyzer.KindSlice:
		g.sliceFrom(b, t, target)
	case analyzer.KindArray:
		g.arrayFrom(b, t, target)
	case analyzer.KindMap:
		g.mapFrom(b, t, target)
	}
}

// containerFrom emits the null and kind checks shared by the container
// decoders around body, which decodes the container once its opening
// delimiter has been consumed. A null zeroes the target, as json/v2 does.
func (g *generator) containerFrom(b *block, open byte, t *analyzer.Type, target ast.Expr, body func(*block)) {
	g.switchStmt(b, nextKind(), func(sw *block) {
		g.caseClause(sw, []ast.Expr{chr('n')}, func(b *block) {
			g.readToken(b)
			g.emit(b, assign(target, zeroLit(t)))
		})
		g.caseClause(sw, []ast.Expr{chr(open)}, func(b *block) {
			g.readToken(b)
			body(b)
		})
		g.defaultClause(sw, func(b *block) {
			g.emit(b, ret(callRT("ErrKindFrom", dec, str(t.Expr))))
		})
	})
}

func (g *generator) sliceFrom(b *block, t *analyzer.Type, target ast.Expr) {
	s, e := id(g.tmp("s")), id(g.tmp("e"))
	g.containerFrom(b, '[', t, target, func(b *block) {
		g.emit(b, define(s, slice(target, nil, num(0))))
		g.ifStmt(b, nil, and(bin(nextKind(), token.NEQ, chr(']')), bin(call(id("cap"), s), token.EQL, num(0))), func(b *block) {
			g.emit(b, assign(s, call(id("make"), typ(t.Expr), num(0), num(4))))
		})
		g.forStmt(b, nil, bin(nextKind(), token.NEQ, chr(']')), nil, func(b *block) {
			g.emit(b, varDecl(e.Name, typ(t.Elem.Expr)))
			g.decodeFrom(b, t.Elem, e)
			g.emit(b, assign(s, call(id("append"), s, e)))
		})
		g.readToken(b)
		g.ifStmt(b, nil, bin(s, token.EQL, nilV), func(b *block) {
			g.emit(b, assign(s, composite(typ(t.Expr))))
		})
		g.emit(b, assign(target, s))
	})
}

func (g *generator) arrayFrom(b *block, t *analyzer.Type, target ast.Expr) {
	i := id(g.tmp("i"))
	g.containerFrom(b, '[', t, target, func(b *block) {
		g.emit(b, define(i, num(0)))
		g.forStmt(b, nil, bin(nextKind(), token.NEQ, chr(']')), nil, func(b *block) {
			// json/v2 rejects an array whose length does not match the Go array,
			// where encoding/json silently pads or truncates.
			g.ifStmt(b, nil, bin(i, token.GEQ, num(t.Len)), func(b *block) {
				g.emit(b, ret(callRT("ErrArrayLength", str(t.Expr), id("true"))))
			})
			g.decodeFrom(b, t.Elem, index(target, i))
			g.emit(b, incr(i))
		})
		g.ifStmt(b, nil, bin(i, token.NEQ, num(t.Len)), func(b *block) {
			g.emit(b, ret(callRT("ErrArrayLength", str(t.Expr), id("false"))))
		})
		g.readToken(b)
	})
}

func (g *generator) mapFrom(b *block, t *analyzer.Type, target ast.Expr) {
	m, k, name, ok, mk, mv := id(g.tmp("m")), id(g.tmp("k")), id(g.tmp("name")), id(g.tmp("ok")), id(g.tmp("key")), id(g.tmp("mv"))
	g.containerFrom(b, '{', t, target, func(b *block) {
		g.emit(b, define(m, target))
		g.ifStmt(b, nil, bin(m, token.EQL, nilV), func(b *block) {
			g.emit(b, assign(m, call(id("make"), typ(t.Expr))))
		})
		g.forStmt(b, nil, bin(nextKind(), token.NEQ, chr('}')), nil, func(b *block) {
			g.emit(b, varDecl(k.Name, jsontextValue()))
			g.emit(b, assignN(token.ASSIGN, []ast.Expr{k, errV}, call(sel(dec, "ReadValue"))))
			g.ifStmt(b, nil, bin(errV, token.NEQ, nilV), func(b *block) {
				g.emit(b, ret(errV))
			})
			g.emit(b, assignN(token.DEFINE, []ast.Expr{name, ok}, callRT("UnquoteName", k)))
			g.ifStmt(b, nil, not(ok), func(b *block) {
				g.emit(b, ret(callRT("ErrSyntax", k, num(0), str("invalid object name"))))
			})
			// The name is only valid until the next read, and a streaming decoder
			// deliberately clobbers it then, so it becomes a string before the
			// member value is read.
			g.emit(b, define(mk, conv(t.Key.Expr, call(sel(cacheV, "Make"), name))))
			g.emit(b, varDecl(mv.Name, typ(t.Elem.Expr)))
			g.decodeFrom(b, t.Elem, mv)
			g.emit(b, assign(index(m, mk), mv))
		})
		g.readToken(b)
		g.emit(b, assign(target, m))
	})
}

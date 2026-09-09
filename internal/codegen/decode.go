package codegen

import (
	"go/ast"
	"go/token"
	"unicode/utf8"

	"github.com/mazrean/odjson/internal/analyzer"
)

// Identifiers the byte oriented decoders share.
var (
	key    = id("key")
	idx    = id("idx")
	strict = id("strict")
)

// dataV and posV render the variables a fragment reads from.
func (c ctx) dataV() *ast.Ident { return id(c.data) }
func (c ctx) posV() *ast.Ident  { return id(c.pos) }

// skipSpace renders pos = odjsonrt.SkipSpace(data, pos).
func (c ctx) skipSpace() ast.Stmt {
	return assign(c.posV(), callRT("SkipSpace", c.dataV(), c.posV()))
}

// parseNull renders np, ok := odjsonrt.ParseNull(data, pos).
func (c ctx) parseNull(np, ok *ast.Ident) ast.Stmt {
	return assignN(token.DEFINE, []ast.Expr{np, ok}, callRT("ParseNull", c.dataV(), c.posV()))
}

// at renders data[pos].
func (c ctx) at() ast.Expr { return index(c.dataV(), c.posV()) }

// atEnd renders pos >= len(data).
func (c ctx) atEnd() ast.Expr {
	return bin(c.posV(), token.GEQ, call(id("len"), c.dataV()))
}

// errSyntax renders odjsonrt.ErrSyntax(data, pos, msg).
func (c ctx) errSyntax(msg string) ast.Expr {
	return callRT("ErrSyntax", c.dataV(), c.posV(), str(msg))
}

func (g *generator) decErr(b *block, c ctx) {
	g.ifStmt(b, nil, bin(errV, token.NEQ, nilV), func(b *block) {
		g.emit(b, c.fail(errV))
	})
}

// decodeStruct writes the body of a struct's byte oriented decoder. Under
// c.v2 it follows encoding/json/v2: a null zeroes the struct and member names
// match case-sensitively.
func (g *generator) decodeStruct(b *block, s *analyzer.StructInfo, c ctx) {
	np, ok, kp := id("np"), id("ok"), id("kp")
	seen, unknown, unknownBuf := id("seen"), id("unknown"), id("unknownBuf")
	g.emit(b, varDecl("err", id("error")))
	g.emit(b, assign(id("_"), errV))
	g.emit(b, c.skipSpace())
	g.ifStmt(b, c.parseNull(np, ok), ok, func(b *block) {
		if c.v2 {
			g.emit(b, assign(ptr(v), composite(typ(s.Expr))))
		}
		g.emit(b, ret(np, nilV))
	})
	g.ifStmt(b, nil, bin(c.atEnd(), token.LOR, bin(c.at(), token.NEQ, chr('{'))), func(b *block) {
		g.emit(b, ret(p, callRT("ErrType", data, p, str(s.Expr))))
	})
	g.emit(b, incr(p))
	g.emit(b, c.skipSpace())
	g.ifStmt(b, nil, and(bin(p, token.LSS, call(id("len"), data)), bin(c.at(), token.EQL, chr('}'))), func(b *block) {
		g.emit(b, ret(bin(p, token.ADD, num(1)), nilV))
	})
	if c.strict {
		// json/v2 rejects a member name that occurs twice. Known members
		// are tracked in a bitset, as json/v2's own decoder does; unknown
		// ones, which are rare, in a list.
		g.emit(b, varDecl("seen", arrayType(int64((max(len(s.Fields), 1)+63)/64), id("uint64"))))
		g.emit(b, assign(id("_"), seen))
		// The list lives on the stack until an object carries more unknown
		// members than the array holds.
		g.emit(b, varDecl("unknownBuf", arrayType(8, sliceType(id("byte")))))
		g.emit(b, define(unknown, slice(unknownBuf, nil, num(0))))
	}
	g.loop(b, func(b *block) {
		g.emit(b, varDecl("key", sliceType(id("byte"))))
		g.emit(b, c.skipSpace())
		if c.strict {
			g.emit(b, define(kp, p))
		}
		g.emit(b, define(idx, num(-1)))
		g.rawKeys(b, s, c)
		s0 := g.ifStmt(b, nil, bin(idx, token.GEQ, num(0)), func(b *block) {
			// A compact document has the colon right after the name; anything
			// else (whitespace, or an error) is AfterKey's.
			s1 := g.ifStmt(b, nil, and(bin(p, token.LSS, call(id("len"), data)), bin(c.at(), token.EQL, chr(':'))), func(b *block) {
				g.emit(b, assign(p, callRT("SkipSpace", data, bin(p, token.ADD, num(1)))))
			})
			g.elseIf(s1, assignN(token.ASSIGN, []ast.Expr{p, errV}, callRT("AfterKey", data, p)), bin(errV, token.NEQ, nilV), func(b *block) {
				g.emit(b, ret(p, errV))
			})
		})
		g.elseBlock(s0, func(b *block) {
			if c.strict {
				g.emit(b, assignN(token.ASSIGN, []ast.Expr{key, p, errV}, callRT("ParseKeyV2", data, p, strict)))
			} else {
				g.emit(b, assignN(token.ASSIGN, []ast.Expr{key, id("_"), p, errV}, callRT("ParseKey", data, p)))
			}
			g.ifStmt(b, nil, bin(errV, token.NEQ, nilV), func(b *block) {
				g.emit(b, ret(p, errV))
			})
			if len(s.Fields) > 0 {
				g.switchStmt(b, call(id("string"), key), func(sw *block) {
					for i, f := range s.Fields {
						g.caseClause(sw, []ast.Expr{str(f.JSONName)}, func(b *block) {
							g.emit(b, assign(idx, num(int64(i))))
						})
					}
				})
				if g.opts.CaseInsensitive && !c.v2 {
					// An unmatched name is compared case-insensitively against every
					// field. Guarding each comparison by length keeps that from being
					// a call per field: only a non-ASCII name can fold to a name of a
					// different byte length.
					fold := id("fold")
					g.ifStmt(b, nil, bin(idx, token.LSS, num(0)), func(b *block) {
						g.emit(b, define(fold, not(callRT("ASCII", key))))
						g.switchStmt(b, nil, func(sw *block) {
							for i, f := range s.Fields {
								cond := bin(
									paren(bin(bin(call(id("len"), key), token.EQL, num(int64(len(f.JSONName)))), token.LOR, fold)),
									token.LAND,
									callRT("EqualFold", key, str(f.JSONName)))
								g.caseClause(sw, []ast.Expr{cond}, func(b *block) {
									g.emit(b, assign(idx, num(int64(i))))
								})
							}
						})
					})
				}
			}
		})
		g.switchStmt(b, idx, func(sw *block) {
			for i, f := range s.Fields {
				g.caseClause(sw, []ast.Expr{num(int64(i))}, func(b *block) {
					if c.strict {
						word, bit := num(int64(i/64)), num(int64(i%64))
						g.ifStmt(b, nil,
							bin(strict, token.LAND, bin(bin(index(seen, word), token.AND, paren(bin(num(1), token.SHL, bit))), token.NEQ, num(0))),
							func(b *block) {
								g.emit(b, ret(kp, callRT("ErrDuplicateName", data, kp, key)))
							})
						g.emit(b, assignN(token.OR_ASSIGN, []ast.Expr{index(seen, word)}, bin(num(1), token.SHL, bit)))
					}
					g.allocSteps(b, f)
					if f.AsString {
						g.decodeQuoted(b, f.Type, selector(f), c)
					} else {
						// Both key paths leave p on the value's first byte.
						mc := c
						mc.trimmed = true
						g.decode(b, f.Type, selector(f), mc)
					}
				})
			}
			g.defaultClause(sw, func(b *block) {
				if c.strict {
					u := id("u")
					g.ifStmt(b, nil, strict, func(b *block) {
						g.rangeStmt(b, id("_"), u, unknown, func(b *block) {
							g.ifStmt(b, nil, bin(call(id("string"), u), token.EQL, call(id("string"), key)), func(b *block) {
								g.emit(b, ret(kp, callRT("ErrDuplicateName", data, kp, key)))
							})
						})
						g.emit(b, assign(unknown, call(id("append"), unknown, key)))
					})
					g.emit(b, c.skipSpace())
					g.emit(b, assignN(token.ASSIGN, []ast.Expr{p, errV}, callRT("SkipValueV2", data, p, strict)))
				} else {
					g.emit(b, c.skipSpace())
					g.emit(b, assignN(token.ASSIGN, []ast.Expr{p, errV}, callRT("SkipValue", data, p)))
				}
				g.ifStmt(b, nil, bin(errV, token.NEQ, nilV), func(b *block) {
					g.emit(b, ret(p, errV))
				})
			})
		})
		g.emit(b, c.skipSpace())
		g.ifStmt(b, nil, c.atEnd(), func(b *block) {
			g.emit(b, ret(p, c.errSyntax("unexpected end of JSON input")))
		})
		g.switchStmt(b, c.at(), func(sw *block) {
			g.caseClause(sw, []ast.Expr{chr(',')}, func(b *block) {
				g.emit(b, incr(p))
			})
			g.caseClause(sw, []ast.Expr{chr('}')}, func(b *block) {
				g.emit(b, ret(bin(p, token.ADD, num(1)), nilV))
			})
			g.defaultClause(sw, func(b *block) {
				g.emit(b, ret(p, c.errSyntax("after object key:value pair")))
			})
		})
	})
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
func (g *generator) rawKeys(b *block, s *analyzer.StructInfo, c ctx) {
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
		first := f.JSONName[0]
		if _, ok := groups[first]; !ok {
			order = append(order, first)
		}
		groups[first] = append(groups[first], cand{i, f.JSONName})
	}
	if len(order) == 0 {
		return
	}
	rest := id("rest")
	g.ifStmt(b, define(rest, slice(data, p, nil)), bin(call(id("len"), rest), token.GTR, num(1)), func(b *block) {
		g.switchStmt(b, index(rest, num(1)), func(sw *block) {
			for _, by := range order {
				g.caseClause(sw, []ast.Expr{chr(by)}, func(b *block) {
					g.switchStmt(b, nil, func(sw *block) {
						for _, k := range groups[by] {
							l := int64(len(k.name) + 2)
							cond := and(
								bin(call(id("len"), rest), token.GEQ, num(l)),
								bin(call(id("string"), slice(rest, nil, num(l))), token.EQL, str(`"`+k.name+`"`)))
							g.caseClause(sw, []ast.Expr{cond}, func(b *block) {
								if c.strict {
									g.emit(b, assignN(token.ASSIGN, []ast.Expr{idx, key, p},
										num(int64(k.idx)), slice(rest, num(1), num(l-1)), bin(p, token.ADD, num(l))))
								} else {
									g.emit(b, assignN(token.ASSIGN, []ast.Expr{idx, p},
										num(int64(k.idx)), bin(p, token.ADD, num(l))))
								}
							})
						}
					})
				})
			}
		})
	})
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
func (g *generator) allocSteps(b *block, f *analyzer.Field) {
	var path ast.Expr = v
	for _, s := range f.Steps {
		path = sel(path, s.Name)
		if s.Ptr {
			g.ifStmt(b, nil, bin(path, token.EQL, nilV), func(b *block) {
				g.emit(b, assign(path, call(id("new"), typ(s.Elem))))
			})
		}
	}
}

// decodeQuoted implements the ",string" tag option on the decoding side.
func (g *generator) decodeQuoted(b *block, t *analyzer.Type, target ast.Expr, c ctx) {
	if t.Kind == analyzer.KindPointer {
		np, ok := id(g.tmp("np")), id(g.tmp("ok"))
		g.emit(b, c.skipSpace())
		s := g.ifStmt(b, c.parseNull(np, ok), ok, func(b *block) {
			g.emit(b, assign(target, nilV))
			g.emit(b, assign(c.posV(), np))
		})
		g.elseBlock(s, func(b *block) {
			g.ifStmt(b, nil, bin(target, token.EQL, nilV), func(b *block) {
				g.emit(b, assign(target, call(id("new"), typ(t.Elem.Expr))))
			})
			g.decodeQuoted(b, t.Elem, deref(target), c)
		})
		return
	}

	inner, sp, np, ok := id(g.tmp("inner")), id(g.tmp("sp")), id(g.tmp("np")), id(g.tmp("ok"))
	g.emit(b, c.skipSpace())
	s := g.ifStmt(b, c.parseNull(np, ok), ok, func(b *block) {
		g.emit(b, assign(c.posV(), np))
		if c.v2 {
			g.emit(b, assign(target, zeroLit(t)))
		}
	})
	g.elseBlock(s, func(b *block) {
		g.emit(b, varDecl(inner.Name, sliceType(id("byte"))))
		g.emit(b, assignN(token.ASSIGN, []ast.Expr{inner, c.posV(), errV},
			callRT(strictName("ParseStringInner", c), strictArgs(c, c.dataV(), c.posV())...)))
		g.decErr(b, c)
		g.emit(b, define(sp, num(0)))
		sub := c
		sub.data, sub.pos, sub.trusted, sub.trimmed, sub.cache = inner.Name, sp.Name, false, false, ""
		sub.strict = false // the inner bytes were validated as a string already
		g.decode(b, t, target, sub)
		g.ifStmt(b, assign(errV, callRT("EndOfDocument", inner, sp)), bin(errV, token.NEQ, nilV), func(b *block) {
			g.emit(b, c.fail(errV))
		})
	})
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
func (g *generator) decode(b *block, t *analyzer.Type, target ast.Expr, c ctx) {
	// lead emits the fragment's leading whitespace skip, unless the caller has
	// already positioned the offset on the value. Only the first call in a
	// fragment can be elided; everything nested has to look for itself.
	lead := func() {
		if c.trimmed {
			c.trimmed = false
			return
		}
		g.emit(b, c.skipSpace())
	}
	// parseErr renders pos, err = odjsonrt.name(data, pos, args...).
	parseErr := func(name string, args ...ast.Expr) ast.Stmt {
		return assignN(token.ASSIGN, []ast.Expr{c.posV(), errV},
			callRT(name, append([]ast.Expr{c.dataV(), c.posV()}, args...)...))
	}
	switch t.Kind {
	case analyzer.KindRawMessage:
		raw := id(g.tmp("raw"))
		lead()
		g.emit(b, varDecl(raw.Name, sliceType(id("byte"))))
		g.emit(b, assignN(token.ASSIGN, []ast.Expr{raw, c.posV(), errV},
			callRT(strictName("ParseRaw", c), strictArgs(c, c.dataV(), c.posV())...)))
		g.decErr(b, c)
		g.emit(b, assign(target, spread(id("append"), slice(target, nil, num(0)), raw)))
		return
	case analyzer.KindStruct:
		// The struct's own decoder skips leading whitespace and handles a
		// null itself, so wrapping the call in another of each is wasted
		// work on every nested object.
		var parse ast.Expr
		switch {
		case c.v2 && t.Struct.Local:
			parse = call(sel(target, "odjsonParseV2"), c.dataV(), c.posV(), cacheExpr(c), strictExpr(c))
		case c.v2:
			parse = call(id(t.Struct.Helper+"ParseV2"), c.dataV(), addr(target), c.posV(), cacheExpr(c), strictExpr(c))
		case t.Struct.Local:
			parse = call(sel(target, "odjsonParse"), c.dataV(), c.posV(), cacheExpr(c))
		default:
			parse = call(id(t.Struct.Helper+"Parse"), c.dataV(), addr(target), c.posV(), cacheExpr(c))
		}
		g.emit(b, assignN(token.ASSIGN, []ast.Expr{c.posV(), errV}, parse))
		g.decErr(b, c)
		return
	case analyzer.KindPointer:
		np, ok := id(g.tmp("np")), id(g.tmp("ok"))
		lead()
		s := g.ifStmt(b, c.parseNull(np, ok), ok, func(b *block) {
			g.emit(b, assign(target, nilV))
			g.emit(b, assign(c.posV(), np))
		})
		g.elseBlock(s, func(b *block) {
			g.ifStmt(b, nil, bin(target, token.EQL, nilV), func(b *block) {
				g.emit(b, assign(target, call(id("new"), typ(t.Elem.Expr))))
			})
			g.decode(b, t.Elem, deref(target), c)
		})
		return
	}

	if !t.Interface {
		switch {
		case t.Unmarshaler || t.PtrUnmarshaler:
			if c.v2 && t.Expr == "time.Time" {
				// json/v2 does not call time.Time's UnmarshalJSON; it has
				// its own codec for it, and that one zeroes on null where
				// the method leaves the value alone.
				np, ok := id(g.tmp("np")), id(g.tmp("ok"))
				lead()
				s := g.ifStmt(b, c.parseNull(np, ok), ok, func(b *block) {
					g.emit(b, assign(c.posV(), np))
					g.emit(b, assign(target, composite(sel(id("time"), "Time"))))
				})
				g.elseBlock(s, func(b *block) {
					g.emit(b, parseErr(strictName("ParseUnmarshaler", c), strictArgs(c, addr(target))...))
					g.decErr(b, c)
				})
				return
			}
			g.emit(b, parseErr(strictName("ParseUnmarshaler", c), strictArgs(c, addr(target))...))
			g.decErr(b, c)
			return
		case t.TextUnmarshaler || t.PtrTextUnmarshaler:
			// encoding/json never hands a null to UnmarshalText; it leaves
			// the value alone. json.Unmarshaler, in contrast, does receive
			// the literal null, so only this branch skips it.
			np, ok := id(g.tmp("np")), id(g.tmp("ok"))
			lead()
			s := g.ifStmt(b, c.parseNull(np, ok), ok, func(b *block) {
				g.emit(b, assign(c.posV(), np))
				if c.v2 {
					g.emit(b, assign(target, zeroLit(t)))
				}
			})
			g.elseBlock(s, func(b *block) {
				g.emit(b, parseErr(strictName("ParseTextUnmarshaler", c), strictArgs(c, addr(target))...))
				g.decErr(b, c)
			})
			return
		}
	}

	lead()
	np, ok := id(g.tmp("np")), id(g.tmp("ok"))
	s := g.ifStmt(b, c.parseNull(np, ok), ok, func(b *block) {
		g.emit(b, assign(c.posV(), np))
		if c.v2 {
			g.emit(b, assign(target, zeroLit(t)))
		} else if nullZero(t) {
			g.emit(b, assign(target, nilV))
		}
	})
	g.elseBlock(s, func(b *block) {
		// The scalar kinds test for the common spelling inline, so that a
		// well-formed value costs no call at all; the runtime parser behind the
		// else branch handles everything else, errors included.
		switch t.Kind {
		case analyzer.KindBool:
			// pos+n <= len(data) && string(data[pos:pos+n]) == lit
			spelled := func(lit string) ast.Expr {
				n := num(int64(len(lit)))
				return and(
					bin(bin(c.posV(), token.ADD, n), token.LEQ, call(id("len"), c.dataV())),
					bin(call(id("string"), slice(c.dataV(), c.posV(), bin(c.posV(), token.ADD, n))), token.EQL, str(lit)))
			}
			s := g.ifStmt(b, nil, spelled("true"), func(b *block) {
				g.emit(b, assign(target, id("true")))
				g.emit(b, assignN(token.ADD_ASSIGN, []ast.Expr{c.posV()}, num(4)))
			})
			s = g.elseIf(s, nil, spelled("false"), func(b *block) {
				g.emit(b, assign(target, id("false")))
				g.emit(b, assignN(token.ADD_ASSIGN, []ast.Expr{c.posV()}, num(5)))
			})
			g.elseBlock(s, func(b *block) {
				g.parseInto(b, c, target, t.Expr, "bool", callRT("ParseBool", c.dataV(), c.posV()))
			})
		case analyzer.KindInt:
			x, np, ok := id(g.tmp("x")), id(g.tmp("np")), id(g.tmp("ok"))
			var cond ast.Expr = ok
			if t.Bits < 64 {
				cond = and(ok,
					bin(x, token.GEQ, num(-1<<(t.Bits-1))),
					bin(x, token.LEQ, num(1<<(t.Bits-1)-1)))
			}
			s := g.ifStmt(b, assignN(token.DEFINE, []ast.Expr{x, np, ok}, callRT("ParseDecimal", c.dataV(), c.posV())), cond, func(b *block) {
				g.emit(b, assign(target, conv(t.Expr, x)))
				g.emit(b, assign(c.posV(), np))
			})
			g.elseBlock(s, func(b *block) {
				g.parseInto(b, c, target, t.Expr, "int64", callRT("ParseInt", c.dataV(), c.posV(), num(int64(t.Bits))))
			})
		case analyzer.KindUint:
			x, np, ok := id(g.tmp("x")), id(g.tmp("np")), id(g.tmp("ok"))
			// ParseDecimal reads at most eighteen digits, so a non-negative
			// result always fits a uint64; the sign is the only other check.
			conds := []ast.Expr{ok, bin(c.at(), token.NEQ, chr('-'))}
			if t.Bits < 64 {
				conds = append(conds, bin(x, token.LEQ, unum(uint64(1)<<t.Bits-1)))
			}
			s := g.ifStmt(b, assignN(token.DEFINE, []ast.Expr{x, np, ok}, callRT("ParseDecimal", c.dataV(), c.posV())), and(conds...), func(b *block) {
				g.emit(b, assign(target, conv(t.Expr, x)))
				g.emit(b, assign(c.posV(), np))
			})
			g.elseBlock(s, func(b *block) {
				g.parseInto(b, c, target, t.Expr, "uint64", callRT("ParseUint", c.dataV(), c.posV(), num(int64(t.Bits))))
			})
		case analyzer.KindFloat:
			x, np, ok := id(g.tmp("x")), id(g.tmp("np")), id(g.tmp("ok"))
			s := g.ifStmt(b, assignN(token.DEFINE, []ast.Expr{x, np, ok}, callRT("ParseSimpleFloat", c.dataV(), c.posV(), num(int64(t.Bits)))), ok, func(b *block) {
				g.emit(b, assign(target, conv(t.Expr, x)))
				g.emit(b, assign(c.posV(), np))
			})
			g.elseBlock(s, func(b *block) {
				g.parseInto(b, c, target, t.Expr, "float64", callRT("ParseFloat", c.dataV(), c.posV(), num(int64(t.Bits))))
			})
		case analyzer.KindString:
			var parse ast.Expr
			switch {
			case c.strict:
				parse = callRT("ParseStringV2", c.dataV(), c.posV(), cacheExpr(c), strict)
			case c.trusted && c.cache != "":
				parse = callRT("ParseStringWith", c.dataV(), c.posV(), id(c.cache))
			case c.trusted:
				parse = callRT("ParseStringTrusted", c.dataV(), c.posV())
			case c.cache != "":
				parse = callRT("ParseStringCached", c.dataV(), c.posV(), id(c.cache))
			default:
				parse = callRT("ParseString", c.dataV(), c.posV())
			}
			g.parseInto(b, c, target, t.Expr, "string", parse)
		case analyzer.KindNumber:
			g.parseInto(b, c, target, t.Expr, "string", callRT(strictName("ParseNumberString", c), strictArgs(c, c.dataV(), c.posV())...))
		case analyzer.KindBytes:
			g.parseInto(b, c, target, t.Expr, "[]byte", callRT(strictName("ParseBase64", c), strictArgs(c, c.dataV(), c.posV())...))
		case analyzer.KindSlice:
			g.decodeSlice(b, t, target, c)
		case analyzer.KindArray:
			g.decodeArray(b, t, target, c)
		case analyzer.KindMap:
			g.decodeMap(b, t, target, c)
		default:
			if t.EmptyInterface {
				// ParseAny builds map[string]any / []any trees directly; the
				// reflection fallback below would cost an encoding/json call
				// per field.
				a := id(g.tmp("a"))
				g.emit(b, varDecl(a.Name, id("any")))
				var parse ast.Expr
				switch {
				case c.strict:
					parse = callRT("ParseAnyV2", c.dataV(), c.posV(), cacheExpr(c), strict)
				case c.trusted && c.cache != "":
					parse = callRT("ParseAnyWith", c.dataV(), c.posV(), id(c.cache))
				case c.cache != "":
					parse = callRT("ParseAnyCached", c.dataV(), c.posV(), id(c.cache))
				default:
					parse = callRT("ParseAny", c.dataV(), c.posV())
				}
				g.emit(b, assignN(token.ASSIGN, []ast.Expr{a, c.posV(), errV}, parse))
				g.decErr(b, c)
				g.emit(b, assign(target, a))
				break
			}
			if c.v2 {
				g.emit(b, parseErr("ParseIntoV2", addr(target), strictExpr(c)))
			} else {
				g.emit(b, parseErr("ParseInto", addr(target)))
			}
			g.decErr(b, c)
		}
	})
}

// strictName picks the V2 variant of a runtime parser under c.strict; that
// variant takes the flag [strictArgs] appends.
func strictName(name string, c ctx) string {
	if c.strict {
		return name + "V2"
	}
	return name
}

// strictArgs appends the trailing strict argument of a V2 runtime parser to
// args, or returns them unchanged when c does not select one.
func strictArgs(c ctx, args ...ast.Expr) []ast.Expr {
	if c.strict {
		return append(args, strict)
	}
	return args
}

// strictExpr renders the strict flag handed to a nested V2 parser: the
// enclosing parser's own flag inside odjsonParseV2, and false anywhere else,
// since there the bytes have come through a jsontext.Decoder.
func strictExpr(c ctx) ast.Expr {
	if c.strict {
		return strict
	}
	return id("false")
}

// cacheExpr renders the string cache argument for c.
func cacheExpr(c ctx) ast.Expr {
	if c.cache == "" {
		return nilV
	}
	return id(c.cache)
}

// parseInto emits the call/convert/assign triple shared by the scalar kinds.
func (g *generator) parseInto(b *block, c ctx, target ast.Expr, expr, native string, parse ast.Expr) {
	tv := id(g.tmp("x"))
	g.emit(b, varDecl(tv.Name, typ(native)))
	g.emit(b, assignN(token.ASSIGN, []ast.Expr{tv, c.posV(), errV}, parse))
	g.decErr(b, c)
	if expr == native {
		g.emit(b, assign(target, tv))
		return
	}
	g.emit(b, assign(target, call(typ(expr), tv)))
}

func (g *generator) expectByte(b *block, c ctx, by byte, what string) {
	g.ifStmt(b, nil, bin(c.atEnd(), token.LOR, bin(c.at(), token.NEQ, chr(by))), func(b *block) {
		g.emit(b, c.fail(callRT("ErrType", c.dataV(), c.posV(), str(what))))
	})
	g.emit(b, incr(c.posV()))
}

// separator emits the comma / closing bracket handling shared by arrays and
// objects.
func (g *generator) separator(b *block, c ctx, closing byte, what string) {
	g.emit(b, c.skipSpace())
	g.ifStmt(b, nil, c.atEnd(), func(b *block) {
		g.emit(b, c.fail(c.errSyntax("unexpected end of JSON input")))
	})
	g.ifStmt(b, nil, bin(c.at(), token.EQL, chr(',')), func(b *block) {
		g.emit(b, incr(c.posV()))
		g.emit(b, branch(token.CONTINUE))
	})
	g.ifStmt(b, nil, bin(c.at(), token.EQL, chr(closing)), func(b *block) {
		g.emit(b, incr(c.posV()))
		g.emit(b, branch(token.BREAK))
	})
	g.emit(b, c.fail(c.errSyntax(what)))
}

func (g *generator) decodeSlice(b *block, t *analyzer.Type, target ast.Expr, c ctx) {
	s, e := id(g.tmp("s")), id(g.tmp("e"))
	g.expectByte(b, c, '[', t.Expr)
	g.emit(b, define(s, slice(target, nil, num(0))))
	g.emit(b, c.skipSpace())
	s0 := g.ifStmt(b, nil, and(bin(c.posV(), token.LSS, call(id("len"), c.dataV())), bin(c.at(), token.EQL, chr(']'))), func(b *block) {
		g.emit(b, incr(c.posV()))
	})
	g.elseBlock(s0, func(b *block) {
		// Growing from nothing costs an allocation and a copy per doubling, so
		// start with room for a few elements — but only once the array is known
		// to be non-empty, since empty arrays are common and must stay free.
		g.ifStmt(b, nil, bin(call(id("cap"), s), token.EQL, num(0)), func(b *block) {
			g.emit(b, assign(s, call(id("make"), typ(t.Expr), num(0), num(4))))
		})
		g.loop(b, func(b *block) {
			g.emit(b, varDecl(e.Name, typ(t.Elem.Expr)))
			g.decode(b, t.Elem, e, c)
			g.emit(b, assign(s, call(id("append"), s, e)))
			g.separator(b, c, ']', "after array element")
		})
	})
	g.ifStmt(b, nil, bin(s, token.EQL, nilV), func(b *block) {
		g.emit(b, assign(s, composite(typ(t.Expr))))
	})
	g.emit(b, assign(target, s))
}

func (g *generator) decodeArray(b *block, t *analyzer.Type, target ast.Expr, c ctx) {
	i, z := id(g.tmp("i")), id(g.tmp("z"))
	g.expectByte(b, c, '[', t.Expr)
	g.emit(b, define(i, num(0)))
	g.emit(b, c.skipSpace())
	s0 := g.ifStmt(b, nil, and(bin(c.posV(), token.LSS, call(id("len"), c.dataV())), bin(c.at(), token.EQL, chr(']'))), func(b *block) {
		g.emit(b, incr(c.posV()))
	})
	g.elseBlock(s0, func(b *block) {
		g.loop(b, func(b *block) {
			s1 := g.ifStmt(b, nil, bin(i, token.LSS, num(t.Len)), func(b *block) {
				g.decode(b, t.Elem, index(target, i), c)
			})
			g.elseBlock(s1, func(b *block) {
				if c.v2 {
					// json/v2 rejects an array whose length does not match the Go
					// array, where encoding/json silently pads or truncates.
					g.emit(b, c.fail(callRT("ErrArrayLength", str(t.Expr), id("true"))))
				} else {
					g.emit(b, c.skipSpace())
					g.emit(b, assignN(token.ASSIGN, []ast.Expr{c.posV(), errV}, callRT("SkipValue", c.dataV(), c.posV())))
					g.decErr(b, c)
				}
			})
			g.emit(b, incr(i))
			g.separator(b, c, ']', "after array element")
		})
	})
	if c.v2 {
		g.ifStmt(b, nil, bin(i, token.NEQ, num(t.Len)), func(b *block) {
			g.emit(b, c.fail(callRT("ErrArrayLength", str(t.Expr), id("false"))))
		})
	} else {
		g.forStmt(b, nil, bin(i, token.LSS, num(t.Len)), incr(i), func(b *block) {
			g.emit(b, varDecl(z.Name, typ(t.Elem.Expr)))
			g.emit(b, assign(index(target, i), z))
		})
	}
}

func (g *generator) decodeMap(b *block, t *analyzer.Type, target ast.Expr, c ctx) {
	m, k, mv := id(g.tmp("m")), id(g.tmp("k")), id(g.tmp("mv"))
	g.expectByte(b, c, '{', t.Expr)
	g.emit(b, define(m, target))
	g.ifStmt(b, nil, bin(m, token.EQL, nilV), func(b *block) {
		g.emit(b, assign(m, call(id("make"), typ(t.Expr))))
	})
	var seen *ast.Ident
	if c.strict {
		// json/v2 rejects a name that occurs twice. A map that was empty
		// when decoding began detects that by itself; one that was not
		// needs the names of this document tracked separately.
		seen = id(g.tmp("seen"))
		g.emit(b, varDecl(seen.Name, mapType(id("string"), emptyStruct())))
		g.ifStmt(b, nil, and(strict, bin(call(id("len"), m), token.GTR, num(0))), func(b *block) {
			g.emit(b, assign(seen, call(id("make"), mapType(id("string"), emptyStruct()))))
		})
	}
	g.emit(b, c.skipSpace())
	s0 := g.ifStmt(b, nil, and(bin(c.posV(), token.LSS, call(id("len"), c.dataV())), bin(c.at(), token.EQL, chr('}'))), func(b *block) {
		g.emit(b, incr(c.posV()))
	})
	g.elseBlock(s0, func(b *block) {
		g.loop(b, func(b *block) {
			g.emit(b, varDecl(k.Name, sliceType(id("byte"))))
			g.emit(b, c.skipSpace())
			if c.strict {
				kp := id(g.tmp("kp"))
				g.emit(b, define(kp, c.posV()))
				g.emit(b, assignN(token.ASSIGN, []ast.Expr{k, c.posV(), errV}, callRT("ParseKeyV2", c.dataV(), c.posV(), strict)))
				g.decErr(b, c)
				dup := id("dup")
				s1 := g.ifStmt(b, nil, not(strict), func(b *block) {})
				s1 = g.elseIf(s1, nil, bin(seen, token.EQL, nilV), func(b *block) {
					g.ifStmt(b, assignN(token.DEFINE, []ast.Expr{id("_"), dup}, index(m, conv(t.Key.Expr, call(id("string"), k)))), dup, func(b *block) {
						g.emit(b, c.fail(callRT("ErrDuplicateName", c.dataV(), kp, k)))
					})
				})
				g.elseBlock(s1, func(b *block) {
					g.ifStmt(b, assignN(token.DEFINE, []ast.Expr{id("_"), dup}, index(seen, call(id("string"), k))), dup, func(b *block) {
						g.emit(b, c.fail(callRT("ErrDuplicateName", c.dataV(), kp, k)))
					})
					g.emit(b, assign(index(seen, call(id("string"), k)), composite(emptyStruct())))
				})
			} else {
				g.emit(b, assignN(token.ASSIGN, []ast.Expr{k, id("_"), c.posV(), errV}, callRT("ParseKey", c.dataV(), c.posV())))
				g.decErr(b, c)
			}
			g.emit(b, varDecl(mv.Name, typ(t.Elem.Expr)))
			g.decode(b, t.Elem, mv, c)
			var mk ast.Expr = call(id("string"), k)
			if c.cache != "" {
				mk = call(sel(id(c.cache), "Make"), k)
			}
			g.emit(b, assign(index(m, conv(t.Key.Expr, mk)), mv))
			g.separator(b, c, '}', "after object key:value pair")
		})
	})
	g.emit(b, assign(target, m))
}

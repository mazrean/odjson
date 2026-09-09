package codegen

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
)

// The constructors below build the syntax tree the generator emits. None of
// them assigns a position: the block builder in emit.go copies every
// statement as it is emitted and positions the copy, so an expression value
// may safely appear in any number of statements.

// ph is the placeholder a constructor stores in a position whose validity
// alone changes the printed syntax, such as the ellipsis of a variadic call.
// The block builder replaces it with the real position when the node is
// emitted; see rebase.
const ph = token.Pos(1)

func id(name string) *ast.Ident { return &ast.Ident{Name: name} }

// sel renders x.name.
func sel(x ast.Expr, name string) *ast.SelectorExpr {
	return &ast.SelectorExpr{X: x, Sel: id(name)}
}

// rt renders odjsonrt.name.
func rt(name string) *ast.SelectorExpr { return sel(id("odjsonrt"), name) }

func call(fn ast.Expr, args ...ast.Expr) *ast.CallExpr {
	return &ast.CallExpr{Fun: fn, Args: args}
}

// callRT renders odjsonrt.name(args...).
func callRT(name string, args ...ast.Expr) *ast.CallExpr {
	return call(rt(name), args...)
}

// spread renders fn(args...) with the last argument spread.
func spread(fn ast.Expr, args ...ast.Expr) *ast.CallExpr {
	return &ast.CallExpr{Fun: fn, Args: args, Ellipsis: ph}
}

func lit(kind token.Token, value string) *ast.BasicLit {
	return &ast.BasicLit{Kind: kind, Value: value}
}

// str renders s as a Go string literal.
func str(s string) *ast.BasicLit { return lit(token.STRING, strconv.Quote(s)) }

// chr renders b as a Go rune literal.
func chr(b byte) *ast.BasicLit { return lit(token.CHAR, strconv.QuoteRune(rune(b))) }

// num renders n, negating a literal rather than spelling a signed one so the
// tree matches what the parser builds from "-128".
func num(n int64) ast.Expr {
	if n < 0 {
		return &ast.UnaryExpr{Op: token.SUB, X: lit(token.INT, strconv.FormatInt(-n, 10))}
	}
	return lit(token.INT, strconv.FormatInt(n, 10))
}

func unum(n uint64) ast.Expr { return lit(token.INT, strconv.FormatUint(n, 10)) }

func bin(x ast.Expr, op token.Token, y ast.Expr) *ast.BinaryExpr {
	return &ast.BinaryExpr{X: x, Op: op, Y: y}
}

// chain folds xs with op from the left, as the parser does for a && b && c.
func chain(op token.Token, xs ...ast.Expr) ast.Expr {
	x := xs[0]
	for _, y := range xs[1:] {
		x = bin(x, op, y)
	}
	return x
}

func and(xs ...ast.Expr) ast.Expr { return chain(token.LAND, xs...) }

func not(x ast.Expr) *ast.UnaryExpr { return &ast.UnaryExpr{Op: token.NOT, X: x} }

// ref renders &x.
func ref(x ast.Expr) *ast.UnaryExpr { return &ast.UnaryExpr{Op: token.AND, X: x} }

func paren(x ast.Expr) *ast.ParenExpr { return &ast.ParenExpr{X: x} }

// deref renders (*x), parenthesised so it can be selected from and indexed.
func deref(x ast.Expr) *ast.ParenExpr { return paren(&ast.StarExpr{X: x}) }

func ptr(t ast.Expr) *ast.StarExpr { return &ast.StarExpr{X: t} }

func index(x, i ast.Expr) *ast.IndexExpr { return &ast.IndexExpr{X: x, Index: i} }

// slice renders x[lo:hi]; either bound may be nil.
func slice(x, lo, hi ast.Expr) *ast.SliceExpr {
	return &ast.SliceExpr{X: x, Low: lo, High: hi}
}

// composite renders t{}.
func composite(t ast.Expr) *ast.CompositeLit { return &ast.CompositeLit{Type: t} }

// typeError is what typ panics with; Generate turns it back into an error.
type typeError struct {
	expr string
	err  error
}

func (e *typeError) Error() string {
	return fmt.Sprintf("parse type expression %q: %v", e.expr, e.err)
}

// typ lifts the analyser's type expression into syntax. The analyser renders
// every type with go/types, so a failure here is a generator bug; it is
// raised as a panic so that the constructors stay expressions, and Generate
// reports it as an error.
func typ(expr string) ast.Expr {
	e, err := parser.ParseExpr(expr)
	if err != nil {
		panic(&typeError{expr, err})
	}
	return e
}

// conv renders a conversion of x to the named type, avoiding a no-op
// conversion when the type is already the plain builtin.
func conv(expr string, x ast.Expr) ast.Expr {
	if expr == "string" {
		return x
	}
	return call(typ(expr), x)
}

// emptyStruct renders struct{}.
func emptyStruct() *ast.StructType {
	return &ast.StructType{Fields: &ast.FieldList{}}
}

func mapType(key, elem ast.Expr) *ast.MapType { return &ast.MapType{Key: key, Value: elem} }

func sliceType(elem ast.Expr) *ast.ArrayType { return &ast.ArrayType{Elt: elem} }

func arrayType(n int64, elem ast.Expr) *ast.ArrayType {
	return &ast.ArrayType{Len: num(n), Elt: elem}
}

// Statements.

func assign(lhs, rhs ast.Expr) *ast.AssignStmt { return assignN(token.ASSIGN, []ast.Expr{lhs}, rhs) }

func define(lhs, rhs ast.Expr) *ast.AssignStmt { return assignN(token.DEFINE, []ast.Expr{lhs}, rhs) }

// assignN renders lhs... tok rhs...
func assignN(tok token.Token, lhs []ast.Expr, rhs ...ast.Expr) *ast.AssignStmt {
	return &ast.AssignStmt{Lhs: lhs, Tok: tok, Rhs: rhs}
}

func ret(results ...ast.Expr) *ast.ReturnStmt { return &ast.ReturnStmt{Results: results} }

func expr(x ast.Expr) *ast.ExprStmt { return &ast.ExprStmt{X: x} }

func incr(x ast.Expr) *ast.IncDecStmt { return &ast.IncDecStmt{X: x, Tok: token.INC} }

func branch(tok token.Token) *ast.BranchStmt { return &ast.BranchStmt{Tok: tok} }

// varDecl renders var name t.
func varDecl(name string, t ast.Expr) *ast.DeclStmt {
	return &ast.DeclStmt{Decl: &ast.GenDecl{
		Tok:   token.VAR,
		Specs: []ast.Spec{&ast.ValueSpec{Names: []*ast.Ident{id(name)}, Type: t}},
	}}
}

// Signatures.

func field(name string, t ast.Expr) *ast.Field {
	f := &ast.Field{Type: t}
	if name != "" {
		f.Names = []*ast.Ident{id(name)}
	}
	return f
}

func fields(list ...*ast.Field) *ast.FieldList { return &ast.FieldList{List: list} }

// signature renders a function type with the given parameters and unnamed
// results.
func signature(params []*ast.Field, results ...ast.Expr) *ast.FuncType {
	ft := &ast.FuncType{Params: fields(params...)}
	if len(results) > 0 {
		ft.Results = &ast.FieldList{}
		for _, r := range results {
			ft.Results.List = append(ft.Results.List, field("", r))
		}
	}
	return ft
}

package codegen

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/printer"
	"go/token"
	"reflect"
	"strings"
)

// Positions.
//
// go/printer lays a file out from the positions of its nodes: a statement
// starts on the line its first token claims, a closing brace on the line it
// claims, and a comment is interleaved with the tokens by comparing offsets.
// Nodes without positions print fine on their own, but a comment cannot be
// placed among them, and a struct or interface type whose braces have no
// position is printed on several lines. So the builder gives every emitted
// statement a line of its own, counting lines the way the printer will lay
// them out: one per statement, one per closing brace, one per case label,
// one per comment line. Everything within a statement shares its line.
//
// While the file is being built a position is simply its line number. render
// spreads the lines out afterwards so that no line's text can run into the
// offset of the next one, which is what keeps the comments where they were
// put.

// block collects the statements of one brace delimited list.
type block struct {
	list []ast.Stmt
}

// nextLine claims the next line of the file.
func (g *generator) nextLine() token.Pos {
	g.line++
	return token.Pos(g.line)
}

// emit appends a copy of s to b on a line of its own.
func (g *generator) emit(b *block, s ast.Stmt) {
	s = clone(s)
	rebase(s, g.nextLine())
	b.list = append(b.list, s)
}

// comment writes one comment line per entry ahead of whatever is emitted
// into b next. Comments are placed by line rather than attached to a
// statement, so b only documents where the comment belongs.
func (g *generator) comment(_ *block, lines ...string) {
	g.comments = append(g.comments, g.commentGroup(lines))
}

// commentGroup claims one line per entry.
func (g *generator) commentGroup(lines []string) *ast.CommentGroup {
	cg := &ast.CommentGroup{}
	for _, l := range lines {
		cg.List = append(cg.List, &ast.Comment{Slash: g.nextLine(), Text: l})
	}
	return cg
}

// ifStmt emits if init; cond { body }. The result accepts an else branch.
func (g *generator) ifStmt(b *block, init ast.Stmt, cond ast.Expr, body func(*block)) *ast.IfStmt {
	s := &ast.IfStmt{Cond: clone(cond), Body: &ast.BlockStmt{}}
	if init != nil {
		s.Init = clone(init)
	}
	pos := g.nextLine()
	s.If = pos
	rebase(s.Init, pos)
	rebase(s.Cond, pos)
	g.blockBody(s.Body, pos, body)
	b.list = append(b.list, s)
	return s
}

// elseBlock attaches else { body } to s.
func (g *generator) elseBlock(s *ast.IfStmt, body func(*block)) {
	blk := &ast.BlockStmt{}
	g.blockBody(blk, s.Body.Rbrace, body)
	s.Else = blk
}

// elseIf attaches else if init; cond { body } to s and returns the new
// branch, which accepts a further else.
func (g *generator) elseIf(s *ast.IfStmt, init ast.Stmt, cond ast.Expr, body func(*block)) *ast.IfStmt {
	e := &ast.IfStmt{Cond: clone(cond), Body: &ast.BlockStmt{}}
	if init != nil {
		e.Init = clone(init)
	}
	pos := s.Body.Rbrace
	e.If = pos
	rebase(e.Init, pos)
	rebase(e.Cond, pos)
	g.blockBody(e.Body, pos, body)
	s.Else = e
	return e
}

// forStmt emits for init; cond; post { body }; any clause may be nil.
func (g *generator) forStmt(b *block, init ast.Stmt, cond ast.Expr, post ast.Stmt, body func(*block)) {
	s := &ast.ForStmt{Body: &ast.BlockStmt{}}
	if init != nil {
		s.Init = clone(init)
	}
	if cond != nil {
		s.Cond = clone(cond)
	}
	if post != nil {
		s.Post = clone(post)
	}
	pos := g.nextLine()
	s.For = pos
	rebase(s.Init, pos)
	rebase(s.Cond, pos)
	rebase(s.Post, pos)
	g.blockBody(s.Body, pos, body)
	b.list = append(b.list, s)
}

// loop emits for { body }.
func (g *generator) loop(b *block, body func(*block)) {
	g.forStmt(b, nil, nil, nil, body)
}

// rangeStmt emits for key, value := range x { body }; value may be nil.
func (g *generator) rangeStmt(b *block, key, value, x ast.Expr, body func(*block)) {
	s := &ast.RangeStmt{Key: clone(key), Tok: token.DEFINE, X: clone(x), Body: &ast.BlockStmt{}}
	if value != nil {
		s.Value = clone(value)
	}
	pos := g.nextLine()
	s.For = pos
	rebase(s.Key, pos)
	rebase(s.Value, pos)
	rebase(s.X, pos)
	g.blockBody(s.Body, pos, body)
	b.list = append(b.list, s)
}

// switchStmt emits switch tag { clauses }; tag may be nil. The callback adds
// clauses with caseClause and defaultClause.
func (g *generator) switchStmt(b *block, tag ast.Expr, clauses func(*block)) {
	s := &ast.SwitchStmt{Body: &ast.BlockStmt{}}
	if tag != nil {
		s.Tag = clone(tag)
	}
	pos := g.nextLine()
	s.Switch = pos
	rebase(s.Tag, pos)
	g.blockBody(s.Body, pos, clauses)
	b.list = append(b.list, s)
}

// caseClause adds case list...: body to a switch body.
func (g *generator) caseClause(sw *block, list []ast.Expr, body func(*block)) {
	c := &ast.CaseClause{}
	for _, x := range list {
		c.List = append(c.List, clone(x))
	}
	pos := g.nextLine()
	c.Case, c.Colon = pos, pos
	for _, x := range c.List {
		rebase(x, pos)
	}
	var inner block
	body(&inner)
	c.Body = inner.list
	sw.list = append(sw.list, c)
}

// defaultClause adds default: body to a switch body.
func (g *generator) defaultClause(sw *block, body func(*block)) {
	g.caseClause(sw, nil, body)
}

// blockBody opens blk on the line of pos, fills it and closes it on a line
// of its own.
func (g *generator) blockBody(blk *ast.BlockStmt, pos token.Pos, body func(*block)) {
	blk.Lbrace = pos
	var inner block
	body(&inner)
	blk.List = inner.list
	blk.Rbrace = g.nextLine()
}

// Declarations.

// funcDecl emits a function or, when recv is not nil, a method, preceded by
// a blank line and its doc comment.
func (g *generator) funcDecl(doc []string, recv *ast.Field, name string, ft *ast.FuncType, body func(*block)) {
	g.nextLine() // the blank line between declarations
	d := &ast.FuncDecl{Doc: g.commentGroup(doc), Name: id(name), Type: clone(ft), Body: &ast.BlockStmt{}}
	if recv != nil {
		d.Recv = fields(clone(recv))
	}
	g.comments = append(g.comments, d.Doc)
	pos := g.nextLine()
	rebase(d.Recv, pos)
	rebase(d.Name, pos)
	rebase(d.Type, pos)
	g.blockBody(d.Body, pos, body)
	g.decls = append(g.decls, d)
}

// varDeclTop emits a package level var name t, preceded by a blank line and
// its doc comment.
func (g *generator) varDeclTop(doc []string, name string, t ast.Expr) {
	g.nextLine()
	d := &ast.GenDecl{
		Doc:   g.commentGroup(doc),
		Tok:   token.VAR,
		Specs: []ast.Spec{&ast.ValueSpec{Names: []*ast.Ident{id(name)}, Type: t}},
	}
	pos := g.nextLine()
	d.TokPos = pos
	rebase(d.Specs[0], pos)
	g.comments = append(g.comments, d.Doc)
	g.decls = append(g.decls, d)
}

// clone deep copies a node so that positioning the copy leaves the original,
// which may be shared, untouched.
func clone[N ast.Node](n N) N {
	c, _ := cloneNode(n).(N)
	return c
}

var nodeType = reflect.TypeFor[ast.Node]()

func cloneNode(n ast.Node) ast.Node {
	v := reflect.ValueOf(n)
	if !v.IsValid() || v.IsNil() {
		return n
	}
	c := reflect.New(v.Elem().Type())
	c.Elem().Set(v.Elem())
	for i := 0; i < c.Elem().NumField(); i++ {
		f := c.Elem().Field(i)
		switch f.Kind() {
		case reflect.Pointer, reflect.Interface:
			if !f.IsNil() && f.Type().Implements(nodeType) {
				f.Set(reflect.ValueOf(cloneNode(f.Interface().(ast.Node))))
			}
		case reflect.Slice:
			if f.IsNil() || !f.Type().Elem().Implements(nodeType) {
				continue
			}
			s := reflect.MakeSlice(f.Type(), f.Len(), f.Len())
			for j := 0; j < f.Len(); j++ {
				e := f.Index(j)
				if !e.IsNil() {
					s.Index(j).Set(reflect.ValueOf(cloneNode(e.Interface().(ast.Node))))
				}
			}
			f.Set(s)
		}
	}
	return c.Interface().(ast.Node)
}

var posType = reflect.TypeFor[token.Pos]()

// optionalPos lists the positions whose validity alone changes the printed
// syntax. rebase leaves those alone unless the constructor set them.
var optionalPos = map[string]bool{
	"CallExpr.Ellipsis": true,
	"GenDecl.Lparen":    true,
	"GenDecl.Rparen":    true,
	"TypeSpec.Assign":   true,
}

// rebase moves every position in n to pos. A nil n is fine.
func rebase(n ast.Node, pos token.Pos) {
	if n == nil || reflect.ValueOf(n).IsNil() {
		return
	}
	ast.Inspect(n, func(n ast.Node) bool {
		if n == nil {
			return false
		}
		if _, ok := n.(*ast.CommentGroup); ok {
			return false
		}
		v := reflect.ValueOf(n).Elem()
		t := v.Type()
		for i := 0; i < v.NumField(); i++ {
			f := v.Field(i)
			if f.Type() != posType {
				continue
			}
			if optionalPos[t.Name()+"."+t.Field(i).Name] && f.Int() == 0 {
				continue
			}
			f.SetInt(int64(pos))
		}
		return true
	})
}

// mapPos applies f to every valid position in n, comments excepted.
func mapPos(n ast.Node, f func(token.Pos) token.Pos) {
	ast.Inspect(n, func(n ast.Node) bool {
		if n == nil {
			return false
		}
		if _, ok := n.(*ast.CommentGroup); ok {
			return false
		}
		v := reflect.ValueOf(n).Elem()
		for i := 0; i < v.NumField(); i++ {
			if fv := v.Field(i); fv.Type() == posType && fv.Int() != 0 {
				fv.SetInt(int64(f(token.Pos(fv.Int()))))
			}
		}
		return true
	})
}

// render lays the generated declarations out behind the file header and the
// import block and prints the result as gofmt would.
func (g *generator) render(header []string, imports []string) ([]byte, error) {
	// Where a line's text ends must never reach the offset of the next
	// line, or a comment there would surface early. Every token on a line
	// shares the line's offset, so the longest token bounds the overrun.
	width := 256
	for _, d := range g.decls {
		ast.Inspect(d, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.Ident:
				width = max(width, len(n.Name)+64)
			case *ast.BasicLit:
				width = max(width, len(n.Value)+64)
			}
			return true
		})
	}

	// The header comes first in the file but is known last, once the
	// imports have been pruned; the body's lines move down to make room.
	shift := len(header) + 2 // the header, a blank line, and the package clause
	if len(imports) > 0 {
		shift += 3 + len(imports) // a blank line, the parentheses, and the imports
	}
	fset := token.NewFileSet()
	base := fset.Base()
	pos := func(line int) token.Pos { return token.Pos(base + (line-1)*width) }
	for _, d := range g.decls {
		mapPos(d, func(p token.Pos) token.Pos { return pos(int(p) + shift) })
	}
	for _, cg := range g.comments {
		for _, c := range cg.List {
			c.Slash = pos(int(c.Slash) + shift)
		}
	}
	lines := g.line + shift
	f := fset.AddFile("odjson_gen.go", base, lines*width)
	offsets := make([]int, lines)
	for i := range offsets {
		offsets[i] = i * width
	}
	f.SetLines(offsets)

	doc := &ast.CommentGroup{}
	for i, l := range header {
		doc.List = append(doc.List, &ast.Comment{Slash: pos(i + 1), Text: l})
	}
	file := &ast.File{
		Doc:      doc,
		Package:  pos(len(header) + 2),
		Name:     id(g.pkg.Name),
		Comments: append([]*ast.CommentGroup{doc}, g.comments...),
	}
	if len(imports) > 0 {
		line := len(header) + 4
		d := &ast.GenDecl{Tok: token.IMPORT, TokPos: pos(line), Lparen: pos(line)}
		for i, imp := range imports {
			spec := &ast.ImportSpec{}
			name, path, aliased := strings.Cut(imp, " ")
			if !aliased {
				path = imp
			} else {
				spec.Name = &ast.Ident{Name: name, NamePos: pos(line + 1 + i)}
			}
			spec.Path = &ast.BasicLit{Kind: token.STRING, Value: path, ValuePos: pos(line + 1 + i)}
			d.Specs = append(d.Specs, spec)
		}
		d.Rparen = pos(line + 1 + len(imports))
		file.Decls = append(file.Decls, d)
	}
	file.Decls = append(file.Decls, g.decls...)

	var buf bytes.Buffer
	cfg := printer.Config{Mode: printer.UseSpaces | printer.TabIndent, Tabwidth: 8}
	if err := cfg.Fprint(&buf, fset, file); err != nil {
		return nil, fmt.Errorf("print generated source: %w", err)
	}
	// gofmt sorts the imports and, by parsing, checks that the tree printed
	// as valid Go.
	src, err := format.Source(buf.Bytes())
	if err != nil {
		return buf.Bytes(), fmt.Errorf("format generated source: %w", err)
	}
	return src, nil
}

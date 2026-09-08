// Package analyzer loads Go packages and turns the struct types it finds into
// the intermediate representation odjson's code generator consumes.
package analyzer

import (
	"fmt"
	"go/ast"
	"go/types"
	"sort"
	"strings"
	"unicode"

	"golang.org/x/tools/go/packages"
)

// RuntimePath is the import path of odjson's runtime support package, which
// every generated file imports.
const RuntimePath = "github.com/mazrean/odjson/odjsonrt"

// JSONTextPath is the import path backing encoding/json/v2's streaming
// marshaler and unmarshaler interfaces.
const JSONTextPath = "encoding/json/jsontext"

// Options controls which types are analysed.
type Options struct {
	// Dir is the directory to load the package from.
	Dir string
	// Pattern is the go/packages pattern identifying the package.
	Pattern string
	// Types restricts generation to the named types. When empty every
	// exported struct type declared in the package is used.
	Types []string
	// Recursive pulls in struct types referenced by the selected types so
	// that nested values are encoded without falling back to reflection.
	Recursive bool
	// Overlay replaces the contents of the named files during type checking
	// without touching the filesystem.
	Overlay map[string][]byte
}

// Analyzer resolves Go types into odjson's intermediate representation.
type Analyzer struct {
	pkgPath string
	imports *Imports
	cache   map[types.Type]*Type
	structs []*StructInfo
	helpers map[string]bool
}

// Load analyses the package selected by opts.
func Load(opts Options) (*Package, error) {
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax |
			packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports |
			packages.NeedDeps,
		Dir:     opts.Dir,
		Overlay: opts.Overlay,
	}
	pattern := opts.Pattern
	if pattern == "" {
		pattern = "."
	}
	pkgs, err := packages.Load(cfg, pattern)
	if err != nil {
		return nil, fmt.Errorf("load package: %w", err)
	}
	if len(pkgs) == 0 {
		return nil, fmt.Errorf("no package matched %q", pattern)
	}
	if len(pkgs) > 1 {
		return nil, fmt.Errorf("pattern %q matched %d packages, expected exactly one", pattern, len(pkgs))
	}
	pkg := pkgs[0]
	if n := packages.PrintErrors(pkgs); n > 0 {
		return nil, fmt.Errorf("package %s has %d errors", pkg.PkgPath, n)
	}

	dir := opts.Dir
	if len(pkg.GoFiles) > 0 {
		dir = dirOf(pkg.GoFiles[0])
	}

	a := &Analyzer{
		pkgPath: pkg.Types.Path(),
		imports: NewImports(pkg.Types.Path(), "dst", "data", "err", "key", "idx", "start", "p", "v"),
		cache:   map[types.Type]*Type{},
		helpers: map[string]bool{},
	}

	// The generated file refers to these two packages by fixed identifiers.
	a.imports.Bind(RuntimePath, "odjsonrt")
	a.imports.Bind(JSONTextPath, "jsontext")

	roots, err := a.roots(pkg, opts.Types)
	if err != nil {
		return nil, err
	}
	for _, named := range roots {
		info, err := a.requestStruct(named)
		if err != nil {
			return nil, err
		}
		info.Requested = true
	}
	if !opts.Recursive {
		var kept []*StructInfo
		for _, s := range a.structs {
			if s.Requested {
				kept = append(kept, s)
			}
		}
		a.structs = kept
	}

	return &Package{
		Name:    pkg.Types.Name(),
		Path:    pkg.Types.Path(),
		Dir:     dir,
		Structs: a.structs,
		Imports: a.imports,
	}, nil
}

func dirOf(file string) string {
	if i := strings.LastIndexByte(file, '/'); i >= 0 {
		return file[:i]
	}
	return "."
}

// roots resolves the named struct types generation starts from.
func (a *Analyzer) roots(pkg *packages.Package, want []string) ([]*types.Named, error) {
	scope := pkg.Types.Scope()
	if len(want) > 0 {
		out := make([]*types.Named, 0, len(want))
		for _, name := range want {
			obj := scope.Lookup(name)
			if obj == nil {
				return nil, fmt.Errorf("type %s not found in package %s", name, pkg.PkgPath)
			}
			tn, ok := obj.(*types.TypeName)
			if !ok {
				return nil, fmt.Errorf("%s is not a type", name)
			}
			named, ok := tn.Type().(*types.Named)
			if !ok {
				return nil, fmt.Errorf("%s is not a named type", name)
			}
			if _, ok := underlyingStruct(named); !ok {
				return nil, fmt.Errorf("%s is not a struct type", name)
			}
			if reason := a.skipReason(named); reason != "" {
				return nil, fmt.Errorf("cannot generate for %s: %s", name, reason)
			}
			out = append(out, named)
		}
		return out, nil
	}

	// Default: every exported struct type declared in the package, in
	// source order.
	var out []*types.Named
	seen := map[string]bool{}
	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, spec := range gd.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok || ts.Assign.IsValid() || !ts.Name.IsExported() || seen[ts.Name.Name] {
					continue
				}
				obj := scope.Lookup(ts.Name.Name)
				tn, ok := obj.(*types.TypeName)
				if !ok {
					continue
				}
				named, ok := tn.Type().(*types.Named)
				if !ok || named.TypeParams() != nil {
					continue
				}
				if _, ok := underlyingStruct(named); !ok {
					continue
				}
				if a.skipReason(named) != "" {
					continue
				}
				seen[ts.Name.Name] = true
				out = append(out, named)
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("package %s declares no eligible struct types", pkg.PkgPath)
	}
	return out, nil
}

// skipReason explains why a struct type must not receive generated methods, or
// returns "" when generation is safe.
func (a *Analyzer) skipReason(named *types.Named) string {
	if named.TypeParams() != nil {
		return "generic types are not supported"
	}
	if v, p := implements(named, jsonMarshaler); v || p {
		return "it already implements json.Marshaler"
	}
	if v, p := implements(named, jsonUnmarshaler); v || p {
		return "it already implements json.Unmarshaler"
	}
	if v, p := implements(named, textMarshaler); v || p {
		return "it already implements encoding.TextMarshaler"
	}
	if v, p := implements(named, textUnmarshaler); v || p {
		return "it already implements encoding.TextUnmarshaler"
	}
	return ""
}

// isLeaf reports whether a type carries its own JSON representation and so must
// not be taken apart field by field.
func (a *Analyzer) isLeaf(t types.Type) bool {
	for _, iface := range []*types.Interface{jsonMarshaler, jsonUnmarshaler, textMarshaler, textUnmarshaler} {
		if v, p := implements(t, iface); v || p {
			return true
		}
	}
	return false
}

func (a *Analyzer) requestStruct(named *types.Named) (*StructInfo, error) {
	t := a.resolve(named)
	if t.Kind != KindStruct || t.Struct == nil {
		return nil, fmt.Errorf("cannot generate a codec for %s", types.TypeString(named, a.imports.Qualifier()))
	}
	return t.Struct, nil
}

// resolve maps a Go type onto odjson's intermediate representation.
func (a *Analyzer) resolve(t types.Type) *Type {
	if r, ok := a.cache[t]; ok {
		return r
	}
	r := &Type{
		Expr:       types.TypeString(t, a.imports.Qualifier()),
		Comparable: types.Comparable(t),
		typ:        t,
	}
	a.cache[t] = r

	if iface, ok := t.Underlying().(*types.Interface); ok {
		r.Interface = true
		r.EmptyInterface = iface.NumMethods() == 0
	}
	r.Marshaler, r.PtrMarshaler = implements(t, jsonMarshaler)
	r.Unmarshaler, r.PtrUnmarshaler = implements(t, jsonUnmarshaler)
	r.TextMarshaler, r.PtrTextMarshaler = implements(t, textMarshaler)
	r.TextUnmarshaler, r.PtrTextUnmarshaler = implements(t, textUnmarshaler)
	if v, p := implements(t, isZeroer); v || p {
		r.HasIsZero = true
	}

	if named, ok := t.(*types.Named); ok {
		if obj := named.Obj(); obj != nil && obj.Pkg() != nil {
			switch {
			case obj.Pkg().Path() == "encoding/json" && obj.Name() == "RawMessage",
				obj.Pkg().Path() == "encoding/json/jsontext" && obj.Name() == "Value":
				r.Kind = KindRawMessage
				return r
			case obj.Pkg().Path() == "encoding/json" && obj.Name() == "Number":
				r.Kind = KindNumber
				return r
			}
		}
	}

	switch u := t.Underlying().(type) {
	case *types.Basic:
		switch info := u.Info(); {
		case info&types.IsBoolean != 0:
			r.Kind = KindBool
		case info&types.IsUnsigned != 0:
			r.Kind, r.Bits = KindUint, basicBits(u.Kind())
		case info&types.IsInteger != 0:
			r.Kind, r.Bits = KindInt, basicBits(u.Kind())
		case info&types.IsFloat != 0:
			r.Kind, r.Bits = KindFloat, basicBits(u.Kind())
		case info&types.IsString != 0:
			r.Kind = KindString
		default:
			r.Kind = KindAny
		}
	case *types.Pointer:
		// A pointer is always handled structurally: the nil check belongs to
		// the pointer, and whatever the element needs — a marshaler, a text
		// marshaler or the reflection fallback — is decided when it is
		// encoded in its own right.
		r.Kind = KindPointer
		r.Elem = a.resolve(u.Elem())
	case *types.Slice:
		if isByte(u.Elem()) {
			r.Kind = KindBytes
			break
		}
		r.Kind = KindSlice
		r.Elem = a.resolve(u.Elem())
	case *types.Array:
		r.Kind = KindArray
		r.Len = u.Len()
		r.Elem = a.resolve(u.Elem())
	case *types.Map:
		key := a.resolve(u.Key())
		if key.Kind != KindString {
			r.Kind = KindAny
			break
		}
		r.Kind = KindMap
		r.Key = key
		r.Elem = a.resolve(u.Elem())
	case *types.Interface:
		r.Kind = KindAny
	case *types.Struct:
		named, ok := t.(*types.Named)
		if !ok || a.isLeaf(t) {
			r.Kind = KindAny
			break
		}
		info, ok := a.newStruct(named, r.Expr)
		if !ok {
			r.Kind = KindAny
			break
		}
		r.Kind = KindStruct
		r.Struct = info
	default:
		r.Kind = KindAny
	}
	return r
}

func (a *Analyzer) newStruct(named *types.Named, expr string) (*StructInfo, bool) {
	if named.TypeParams() != nil || named.TypeArgs() != nil {
		// Generic types cannot receive generated methods, and an
		// instantiation has no name the generated file could declare a
		// helper for. Fall back to reflection.
		return nil, false
	}
	obj := named.Obj()
	local := obj.Pkg() != nil && obj.Pkg().Path() == a.pkgPath
	if !local && !obj.Exported() {
		return nil, false
	}
	info := &StructInfo{
		Name:  obj.Name(),
		Expr:  expr,
		Local: local,
	}
	if !local {
		info.Helper = a.helperName(obj.Pkg().Name(), obj.Name())
	}
	a.structs = append(a.structs, info)
	info.Fields = a.structFields(named)
	if !local {
		for _, f := range info.Fields {
			if !ast.IsExported(f.Name) {
				return a.dropStruct(info)
			}
			for _, s := range f.Steps {
				if !ast.IsExported(s.Name) {
					return a.dropStruct(info)
				}
			}
		}
	}
	return info, true
}

func (a *Analyzer) dropStruct(info *StructInfo) (*StructInfo, bool) {
	for i, s := range a.structs {
		if s == info {
			a.structs = append(a.structs[:i], a.structs[i+1:]...)
			break
		}
	}
	return nil, false
}

func (a *Analyzer) helperName(pkgName, typeName string) string {
	base := "odjson" + upperFirst(sanitizeIdent(pkgName)) + sanitizeIdent(typeName)
	name := base
	for i := 2; a.helpers[name]; i++ {
		name = base + fmt.Sprint(i)
	}
	a.helpers[name] = true
	return name
}

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

func basicBits(k types.BasicKind) int {
	switch k {
	case types.Int8, types.Uint8:
		return 8
	case types.Int16, types.Uint16:
		return 16
	case types.Int32, types.Uint32, types.Float32:
		return 32
	case types.Int, types.Uint, types.Uintptr, types.Int64, types.Uint64, types.Float64:
		return 64
	default:
		return 64
	}
}

func isByte(t types.Type) bool {
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Kind() == types.Uint8
}

// SortStructs orders generated types so the output file is stable across runs.
func SortStructs(s []*StructInfo) {
	sort.SliceStable(s, func(i, j int) bool {
		if s[i].Requested != s[j].Requested {
			return s[i].Requested
		}
		return false
	})
}

// ListDirs resolves a go/packages pattern to the directories holding the
// matched packages. It is used to stage the generator over several packages
// before any of them are type checked.
func ListDirs(dir, pattern string) ([]string, error) {
	cfg := &packages.Config{Mode: packages.NeedName | packages.NeedFiles, Dir: dir}
	pkgs, err := packages.Load(cfg, pattern)
	if err != nil {
		return nil, fmt.Errorf("load packages: %w", err)
	}
	seen := map[string]bool{}
	var out []string
	for _, p := range pkgs {
		if len(p.GoFiles) == 0 {
			continue
		}
		d := dirOf(p.GoFiles[0])
		if seen[d] {
			continue
		}
		seen[d] = true
		out = append(out, d)
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil, fmt.Errorf("no Go packages matched %q", pattern)
	}
	return out, nil
}

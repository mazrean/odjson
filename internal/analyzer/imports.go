package analyzer

import (
	"go/types"
	"sort"
	"strconv"
	"unicode"
)

// Imports tracks the packages referenced by a generated file and assigns a
// unique identifier to each of them.
type Imports struct {
	self  string
	alias map[string]string
	name  map[string]string
	taken map[string]bool
	used  map[string]bool
}

// NewImports creates an import set for a file generated into the package with
// the given import path. Identifiers listed in reserved are never handed out
// as an import alias.
func NewImports(self string, reserved ...string) *Imports {
	im := &Imports{
		self:  self,
		alias: map[string]string{},
		name:  map[string]string{},
		taken: map[string]bool{},
		used:  map[string]bool{},
	}
	for _, r := range reserved {
		im.taken[r] = true
	}
	return im
}

// Bind reserves an identifier for a package without importing it yet. The
// generated file uses these identifiers verbatim, so they must never be handed
// to another package.
func (im *Imports) Bind(path, name string) {
	im.alias[path] = name
	im.name[path] = name
	im.taken[name] = true
}

// Add registers the package at path (whose package name is name) and returns
// the identifier the generated file must use to refer to it.
func (im *Imports) Add(path, name string) string {
	if path == im.self {
		return ""
	}
	if a, ok := im.alias[path]; ok {
		im.used[path] = true
		return a
	}
	base := sanitizeIdent(name)
	a := base
	for i := 2; im.taken[a]; i++ {
		a = base + strconv.Itoa(i)
	}
	im.taken[a] = true
	im.alias[path] = a
	im.name[path] = name
	im.used[path] = true
	return a
}

// Qualifier returns a types.Qualifier that renders type expressions using this
// import set, registering every package it encounters.
func (im *Imports) Qualifier() types.Qualifier {
	return func(p *types.Package) string {
		if p == nil {
			return ""
		}
		return im.Add(p.Path(), p.Name())
	}
}

// Lines returns the import block entries for the packages that were actually
// referenced, sorted by import path. Each entry is already formatted as it
// should appear inside an import declaration.
func (im *Imports) Lines() []string { return im.lines(nil) }

// LinesFor is [Imports.Lines] restricted to the packages whose identifier
// appears in used.
func (im *Imports) LinesFor(used map[string]bool) []string { return im.lines(used) }

func (im *Imports) lines(used map[string]bool) []string {
	paths := make([]string, 0, len(im.used))
	for p := range im.used {
		if used != nil && !used[im.alias[p]] {
			continue
		}
		paths = append(paths, p)
	}
	sort.Strings(paths)
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if im.alias[p] == im.name[p] {
			out = append(out, strconv.Quote(p))
			continue
		}
		out = append(out, im.alias[p]+" "+strconv.Quote(p))
	}
	return out
}

func sanitizeIdent(s string) string {
	r := []rune(s)
	out := make([]rune, 0, len(r))
	for i, c := range r {
		switch {
		case unicode.IsLetter(c) || c == '_':
			out = append(out, c)
		case unicode.IsDigit(c) && i > 0:
			out = append(out, c)
		default:
			out = append(out, '_')
		}
	}
	if len(out) == 0 {
		return "pkg"
	}
	return string(out)
}

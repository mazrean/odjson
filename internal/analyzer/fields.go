package analyzer

import (
	"go/types"
	"reflect"
	"sort"
	"strings"
	"unicode"
)

type scanEntry struct {
	name  string
	index []int
	steps []Step
	typ   *types.Struct
	named types.Type
}

// structFields reimplements encoding/json's typeFields for go/types: it walks
// embedded structs breadth first, promoting fields and resolving name
// conflicts with the same dominance rules the standard library uses.
func (a *Analyzer) structFields(root types.Type) []*Field {
	st, ok := underlyingStruct(root)
	if !ok {
		return nil
	}

	var (
		fields    []*Field
		current   []scanEntry
		next      = []scanEntry{{typ: st, named: root}}
		count     = map[types.Type]int{}
		nextCount = map[types.Type]int{}
		visited   = map[types.Type]bool{}
	)

	for len(next) > 0 {
		current, next = next, current[:0]
		count, nextCount = nextCount, map[types.Type]int{}

		for _, e := range current {
			if visited[e.named] {
				continue
			}
			visited[e.named] = true

			for i := range e.typ.NumFields() {
				sf := e.typ.Field(i)
				ftype := sf.Type()

				if sf.Embedded() {
					t := ftype
					if p, ok := t.Underlying().(*types.Pointer); ok {
						t = p.Elem()
					}
					if !sf.Exported() {
						if _, ok := t.Underlying().(*types.Struct); !ok {
							continue
						}
					}
				} else if !sf.Exported() {
					continue
				}

				tag := reflect.StructTag(e.typ.Tag(i)).Get("json")
				if tag == "-" {
					continue
				}
				name, opts := parseTag(tag)
				if !isValidTag(name) {
					name = ""
				}

				index := make([]int, len(e.index)+1)
				copy(index, e.index)
				index[len(e.index)] = i

				ft := ftype
				isPtr := false
				if p, ok := ft.(*types.Pointer); ok {
					// Unnamed pointer type: encoding/json follows it.
					ft = p.Elem()
					isPtr = true
				}
				ftStruct, ftIsStruct := underlyingStruct(ft)

				if name != "" || !sf.Embedded() || !ftIsStruct || a.isLeaf(ftype) {
					tagged := name != ""
					if name == "" {
						name = sf.Name()
					}
					f := &Field{
						Name:      sf.Name(),
						JSONName:  name,
						Steps:     e.steps,
						OmitEmpty: opts.Contains("omitempty"),
						OmitZero:  opts.Contains("omitzero"),
						AsString:  opts.Contains("string"),
						index:     index,
						tagged:    tagged,
					}
					f.Type = a.resolve(sf.Type())
					if f.AsString && !stringable(f.Type) {
						f.AsString = false
					}
					fields = append(fields, f)
					if count[e.named] > 1 {
						// Multiple embeddings of the same type: force a
						// conflict so the field is dropped.
						dup := *f
						fields = append(fields, &dup)
					}
					continue
				}

				nextCount[ft]++
				if nextCount[ft] == 1 {
					steps := make([]Step, len(e.steps)+1)
					copy(steps, e.steps)
					steps[len(e.steps)] = Step{
						Name: sf.Name(),
						Ptr:  isPtr,
						Elem: types.TypeString(ft, a.imports.Qualifier()),
					}
					next = append(next, scanEntry{
						name:  sf.Name(),
						index: index,
						steps: steps,
						typ:   ftStruct,
						named: ft,
					})
				}
			}
		}
	}

	sort.Slice(fields, func(i, j int) bool {
		x, y := fields[i], fields[j]
		if x.JSONName != y.JSONName {
			return x.JSONName < y.JSONName
		}
		if len(x.index) != len(y.index) {
			return len(x.index) < len(y.index)
		}
		if x.tagged != y.tagged {
			return x.tagged
		}
		return byIndex(x.index, y.index)
	})

	out := fields[:0]
	for i, n := 0, len(fields); i < n; {
		f := fields[i]
		j := i + 1
		for j < n && fields[j].JSONName == f.JSONName {
			j++
		}
		if dominant, ok := dominantField(fields[i:j]); ok {
			out = append(out, dominant)
		}
		i = j
	}
	fields = out

	sort.Slice(fields, func(i, j int) bool { return byIndex(fields[i].index, fields[j].index) })
	return fields
}

// dominantField applies encoding/json's conflict resolution: the shallowest
// field wins, a tagged field beats an untagged one at the same depth, and any
// remaining tie means the name is dropped entirely.
func dominantField(fields []*Field) (*Field, bool) {
	if len(fields) > 1 &&
		len(fields[0].index) == len(fields[1].index) &&
		fields[0].tagged == fields[1].tagged {
		return nil, false
	}
	return fields[0], true
}

func byIndex(x, y []int) bool {
	for i, xi := range x {
		if i >= len(y) {
			return false
		}
		if xi != y[i] {
			return xi < y[i]
		}
	}
	return len(x) < len(y)
}

func underlyingStruct(t types.Type) (*types.Struct, bool) {
	st, ok := t.Underlying().(*types.Struct)
	return st, ok
}

type tagOptions string

func parseTag(tag string) (string, tagOptions) {
	name, opt, _ := strings.Cut(tag, ",")
	return name, tagOptions(opt)
}

func (o tagOptions) Contains(name string) bool {
	s := string(o)
	for s != "" {
		var cur string
		cur, s, _ = strings.Cut(s, ",")
		if cur == name {
			return true
		}
	}
	return false
}

func isValidTag(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		switch {
		case strings.ContainsRune("!#$%&()*+-./:;<=>?@[]^_{|}~ ", c):
		case !unicode.IsLetter(c) && !unicode.IsDigit(c):
			return false
		}
	}
	return true
}

// stringable reports whether the ",string" tag option applies to the type.
func stringable(t *Type) bool {
	switch t.Kind {
	case KindBool, KindInt, KindUint, KindFloat, KindString:
		return true
	case KindPointer:
		return stringable(t.Elem)
	default:
		return false
	}
}

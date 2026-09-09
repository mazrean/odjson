// Package plainref bridges a fixture type to its reflection-only twin.
//
// The generated codecs implement json.Marshaler and json.Unmarshaler, so
// json.Marshal on a fixture type no longer reflects over it — it calls the very
// code under test. The parity fixtures therefore declare each type twice: once
// in the generated package, and once in a sibling package that odjson never
// runs on. The two declarations are field for field identical, so a value of
// one can be read as the other, and encoding/json has something to reflect
// over again.
//
// Of does that reinterpretation and SameLayout is what makes it safe: every
// test that uses Of asserts the two declarations have not drifted apart. It is
// the same bargain odjsonrt.calibrateDirect makes, checked the same way.
package plainref

import (
	"fmt"
	"reflect"
	"unsafe"
)

// Of reinterprets v as its twin R. SameLayout(reflect.TypeFor[T](),
// reflect.TypeFor[R]()) must hold.
func Of[T, R any](v *T) *R { return (*R)(unsafe.Pointer(v)) }

// SameLayout reports why a and b are not field for field identical, or nil
// when they are.
func SameLayout(a, b reflect.Type) error {
	return same(a, b, "", map[[2]reflect.Type]bool{})
}

func same(a, b reflect.Type, path string, seen map[[2]reflect.Type]bool) error {
	if a == b {
		return nil
	}
	if seen[[2]reflect.Type{a, b}] {
		return nil
	}
	seen[[2]reflect.Type{a, b}] = true

	at := func(what string, args ...any) error {
		return fmt.Errorf("%s: %s", path, fmt.Sprintf(what, args...))
	}
	if a.Kind() != b.Kind() {
		return at("%s is a %s, %s is a %s", a, a.Kind(), b, b.Kind())
	}
	if a.Size() != b.Size() {
		return at("%s is %d bytes, %s is %d", a, a.Size(), b, b.Size())
	}

	switch a.Kind() {
	case reflect.Struct:
		if a.NumField() != b.NumField() {
			return at("%s has %d fields, %s has %d", a, a.NumField(), b, b.NumField())
		}
		for i := range a.NumField() {
			fa, fb := a.Field(i), b.Field(i)
			switch {
			case fa.Name != fb.Name:
				return at("field %d is %s, not %s", i, fa.Name, fb.Name)
			case fa.Offset != fb.Offset:
				return at("field %s is at %d, not %d", fa.Name, fa.Offset, fb.Offset)
			case fa.Tag != fb.Tag:
				return at("field %s is tagged %q, not %q", fa.Name, fa.Tag, fb.Tag)
			case fa.Anonymous != fb.Anonymous:
				return at("field %s is embedded in one and not the other", fa.Name)
			}
			if err := same(fa.Type, fb.Type, path+"."+fa.Name, seen); err != nil {
				return err
			}
		}
	case reflect.Pointer, reflect.Slice, reflect.Array:
		return same(a.Elem(), b.Elem(), path+"[]", seen)
	case reflect.Map:
		if err := same(a.Key(), b.Key(), path+"[key]", seen); err != nil {
			return err
		}
		return same(a.Elem(), b.Elem(), path+"[]", seen)
	default:
		// Two differently named types of the same basic kind and size hold
		// the same bits, which is all the reinterpretation needs.
	}
	return nil
}

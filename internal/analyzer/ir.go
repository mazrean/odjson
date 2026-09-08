package analyzer

import "go/types"

// Kind classifies how a Go type is encoded to and decoded from JSON.
type Kind uint8

// The set of type kinds odjson can generate code for. KindAny is the fallback
// that defers to encoding/json's reflection based implementation.
const (
	KindInvalid Kind = iota
	KindBool
	KindInt
	KindUint
	KindFloat
	KindString
	KindBytes      // []byte and named slice-of-byte types: base64
	KindArray      // fixed size array
	KindSlice      // slice of anything but byte
	KindMap        // map with a string key
	KindStruct     // named struct type odjson generates a codec for
	KindPointer    // pointer to a supported type
	KindRawMessage // encoding/json.RawMessage
	KindNumber     // encoding/json.Number
	KindAny        // interface types and anything unsupported
)

// Type is the analysed shape of a Go type as odjson's code generator sees it.
type Type struct {
	Kind Kind
	// Expr is the Go source expression naming this type inside the generated
	// file, e.g. "[]*time.Duration".
	Expr string
	// Bits is the width of an integer or floating point type.
	Bits int
	// Len is the element count of an array type.
	Len int64
	// Elem is the element type of a pointer, slice, array or map.
	Elem *Type
	// Key is the key type of a map.
	Key *Type
	// Struct is the generated codec description for KindStruct.
	Struct *StructInfo

	// Marshaler and friends record which of the encoding interfaces the type
	// (or a pointer to it) implements. The generator prefers these over the
	// structural encoding, exactly like encoding/json does.
	Marshaler, PtrMarshaler             bool
	Unmarshaler, PtrUnmarshaler         bool
	TextMarshaler, PtrTextMarshaler     bool
	TextUnmarshaler, PtrTextUnmarshaler bool

	// Interface reports whether the type's underlying type is an interface,
	// in which case taking its address does not yield a json.Unmarshaler.
	Interface bool
	// EmptyInterface narrows Interface to the method-less `any`, which the
	// runtime decodes and encodes without reflection.
	EmptyInterface bool
	// Comparable reports whether the type may be compared with ==.
	Comparable bool
	// HasIsZero reports whether the type has an IsZero() bool method, which
	// omitzero consults.
	HasIsZero bool
	// Addressable is set for types whose values the generator can take the
	// address of. Struct fields always are.
	typ types.Type
}

// Step is one hop through an embedded field on the way to a promoted field.
type Step struct {
	// Name is the embedded field's Go name.
	Name string
	// Ptr reports whether the embedded field is a pointer to a struct.
	Ptr bool
	// Elem is the type expression of the embedded struct, used to allocate
	// the intermediate value while decoding.
	Elem string
}

// Field is a single JSON member produced by a struct type.
type Field struct {
	// Name is the Go field name.
	Name string
	// JSONName is the member name in the JSON document.
	JSONName string
	// Steps is the chain of embedded fields traversed to reach this field.
	Steps []Step
	// Type is the field's analysed type.
	Type *Type
	// OmitEmpty, OmitZero and AsString mirror the struct tag options.
	OmitEmpty bool
	OmitZero  bool
	AsString  bool

	index  []int
	tagged bool
}

// StructInfo describes one struct type that odjson generates a codec for.
type StructInfo struct {
	// Name is the type's Go name.
	Name string
	// Expr is the type expression used inside the generated file.
	Expr string
	// Local reports whether the type is declared in the package being
	// generated into. Local types receive methods; foreign types receive
	// package level helper functions.
	Local bool
	// Helper is the base identifier used for the generated helpers of a
	// foreign type.
	Helper string
	// Requested reports whether the user asked for this type explicitly, as
	// opposed to it being pulled in because another generated type refers to
	// it.
	Requested bool
	// Fields are the JSON members, in encoding/json's field order.
	Fields []*Field
}

// Package is the result of analysing one Go package.
type Package struct {
	// Name is the package clause of the generated file.
	Name string
	// Path is the package's import path.
	Path string
	// Dir is the directory holding the package's Go files.
	Dir string
	// Structs are the types to generate codecs for, in a stable order.
	Structs []*StructInfo
	// Imports is the import set the generated file must declare.
	Imports *Imports
}

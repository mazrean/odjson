// Package plain declares testfixture's types a second time, with no generated
// codec attached. It is the reflection oracle: encoding/json marshals and
// unmarshals these declarations the ordinary way, and testfixture's tests
// compare that against what the generated methods do. See
// [github.com/mazrean/odjson/internal/testfixture/plainref].
package plain

import (
	"encoding/json"
	"time"
)

// Color is a named string type.
type Color string

// Level is a named integer type.
type Level int

// Flags is a named byte slice, which JSON represents as base64.
type Flags []byte

// Tags is a named slice of strings.
type Tags []string

// Meta is a named map.
type Meta map[string]int

// Inner is a nested struct reached through a value field, a pointer field and
// a slice.
type Inner struct {
	ID   int    `json:"id"`
	Note string `json:"note,omitempty"`
}

// Base is embedded by value into Scalars.
type Base struct {
	BaseID   int    `json:"base_id"`
	BaseName string `json:"base_name,omitempty"`
}

// PtrBase is embedded by pointer into Scalars.
type PtrBase struct {
	PtrField string `json:"ptr_field"`
}

// Scalars covers the primitive field kinds and the tag options.
type Scalars struct {
	Base
	*PtrBase

	Bool    bool    `json:"bool"`
	Int     int     `json:"int"`
	Int8    int8    `json:"int8"`
	Int16   int16   `json:"int16"`
	Int32   int32   `json:"int32"`
	Int64   int64   `json:"int64"`
	Uint    uint    `json:"uint"`
	Uint8   uint8   `json:"uint8"`
	Uint16  uint16  `json:"uint16"`
	Uint32  uint32  `json:"uint32"`
	Uint64  uint64  `json:"uint64"`
	Float32 float32 `json:"float32"`
	Float64 float64 `json:"float64"`
	String  string  `json:"string"`

	Named      Color `json:"named"`
	NamedLevel Level `json:"named_level"`

	QuotedInt    int     `json:"qint,string"`
	QuotedBool   bool    `json:"qbool,string"`
	QuotedString string  `json:"qstring,string"`
	QuotedFloat  float64 `json:"qfloat,string"`
	QuotedPtr    *int    `json:"qptr,string"`

	Skipped  string `json:"-"`
	Renamed  string `json:"renamed"`
	Untagged string
	private  string //nolint:unused

	OmitEmptyString string  `json:"oe_string,omitempty"`
	OmitEmptyInt    int     `json:"oe_int,omitempty"`
	OmitEmptyPtr    *string `json:"oe_ptr,omitempty"`
	OmitEmptySlice  []int   `json:"oe_slice,omitempty"`

	OmitZeroTime   time.Time `json:"oz_time,omitzero"`
	OmitZeroInner  Inner     `json:"oz_inner,omitzero"`
	OmitZeroString string    `json:"oz_string,omitzero"`
}

// Composites covers the aggregate field kinds.
type Composites struct {
	Bytes     []byte            `json:"bytes"`
	NamedFlag Flags             `json:"named_flags"`
	ByteArray [4]byte           `json:"byte_array"`
	IntArray  [3]int            `json:"int_array"`
	Ints      []int             `json:"ints"`
	Strings   Tags              `json:"strings"`
	Inners    []Inner           `json:"inners"`
	InnerPtrs []*Inner          `json:"inner_ptrs"`
	Nested    [][]int           `json:"nested"`
	StringMap map[string]string `json:"string_map"`
	InnerMap  map[string]Inner  `json:"inner_map"`
	NamedMap  Meta              `json:"named_map"`
	ColorMap  map[Color]int     `json:"color_map"`

	Inner    Inner   `json:"inner"`
	InnerPtr *Inner  `json:"inner_ptr"`
	DeepPtr  **Inner `json:"deep_ptr"`

	Any     any             `json:"any"`
	Anys    []any           `json:"anys"`
	Raw     json.RawMessage `json:"raw"`
	Number  json.Number     `json:"number"`
	Time    time.Time       `json:"time"`
	TimePtr *time.Time      `json:"time_ptr"`
	Dur     time.Duration   `json:"dur"`
}

// Recursive exercises a self referential type.
type Recursive struct {
	Name     string       `json:"name"`
	Children []*Recursive `json:"children,omitempty"`
}

// Memberless has no JSON members: its only field is skipped by its tag.
type Memberless struct {
	Skipped string `json:"-"`
}

// Unit is the other shape with nothing to match: no fields at all.
type Unit struct{}

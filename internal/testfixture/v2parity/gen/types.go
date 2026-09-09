// Package gen holds the type zoo used to check that odjson's generated
// encoding/json/v2 methods produce exactly what json/v2's reflection path
// produces. This copy carries the generated methods.
package gen

import (
	"encoding/json"
	"time"
)

//go:generate go run github.com/mazrean/odjson

// Color is a named string type.
type Color string

// Bag is a named byte slice.
type Bag []byte

// Nested is reached through a value, a pointer, a slice and a map.
type Nested struct {
	ID   int    `json:"id"`
	Note string `json:"note,omitempty"`
}

// Zoo covers every field shape whose encoding differs between encoding/json
// and encoding/json/v2.
type Zoo struct {
	Bool    bool    `json:"bool"`
	Int     int     `json:"int"`
	Uint64  uint64  `json:"uint64"`
	Float64 float64 `json:"float64"`
	String  string  `json:"string"`
	Named   Color   `json:"named"`

	Slice    []int             `json:"slice"`
	Strings  []string          `json:"strings"`
	Bytes    []byte            `json:"bytes"`
	NamedBag Bag               `json:"named_bag"`
	Array    [3]int            `json:"array"`
	Map      map[string]int    `json:"map"`
	StrMap   map[string]string `json:"str_map"`

	Nested    Nested            `json:"nested"`
	NestedPtr *Nested           `json:"nested_ptr"`
	Nesteds   []Nested          `json:"nesteds"`
	NestedMap map[string]Nested `json:"nested_map"`

	Any    any             `json:"any"`
	Anys   []any           `json:"anys"`
	Raw    json.RawMessage `json:"raw"`
	Number json.Number     `json:"number"`
	Time   time.Time       `json:"time"`
	Ptr    *int            `json:"ptr"`
	Quoted int             `json:"quoted,string"`

	OmitBool   bool           `json:"omit_bool,omitempty"`
	OmitInt    int            `json:"omit_int,omitempty"`
	OmitFloat  float64        `json:"omit_float,omitempty"`
	OmitString string         `json:"omit_string,omitempty"`
	OmitSlice  []int          `json:"omit_slice,omitempty"`
	OmitMap    map[string]int `json:"omit_map,omitempty"`
	OmitPtr    *int           `json:"omit_ptr,omitempty"`

	ZeroTime   time.Time `json:"zero_time,omitzero"`
	ZeroNested Nested    `json:"zero_nested,omitzero"`
}

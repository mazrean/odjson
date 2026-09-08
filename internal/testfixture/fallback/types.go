// Package fallback covers the field shapes odjson cannot generate a direct
// codec for and therefore hands to encoding/json, plus the types that carry
// their own JSON representation.
package fallback

import (
	"errors"
	"fmt"
	"strconv"
)

//go:generate go run github.com/mazrean/odjson

// Box is generic. Generated methods cannot be attached to a generic type, so
// fields of this type fall back to reflection.
type Box[T any] struct {
	Value T `json:"value"`
}

// Custom carries its own JSON representation through json.Marshaler.
type Custom struct {
	N int
}

// MarshalJSON implements json.Marshaler.
func (c Custom) MarshalJSON() ([]byte, error) {
	return []byte(`{"custom":` + strconv.Itoa(c.N) + `}`), nil
}

// UnmarshalJSON implements json.Unmarshaler.
func (c *Custom) UnmarshalJSON(data []byte) error {
	var v struct {
		Custom *int `json:"custom"`
	}
	if err := jsonUnmarshal(data, &v); err != nil {
		return err
	}
	if v.Custom == nil {
		return errors.New("fallback: missing custom field")
	}
	c.N = *v.Custom
	return nil
}

// Textual carries its own representation through encoding.TextMarshaler.
type Textual struct {
	S string
}

// MarshalText implements encoding.TextMarshaler.
func (t Textual) MarshalText() ([]byte, error) {
	if t.S == "!" {
		return nil, fmt.Errorf("fallback: refusing to encode %q", t.S)
	}
	return []byte("text:" + t.S), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
func (t *Textual) UnmarshalText(data []byte) error {
	s := string(data)
	if len(s) < 5 || s[:5] != "text:" {
		return fmt.Errorf("fallback: bad text %q", s)
	}
	t.S = s[5:]
	return nil
}

// Fallbacks gathers one field of every shape that leaves the generated fast
// path.
type Fallbacks struct {
	Generic    Box[int]     `json:"generic"`
	GenericPtr *Box[string] `json:"generic_ptr"`
	Anon       struct {
		A int `json:"a"`
	} `json:"anon"`
	IntMap    map[int]string `json:"int_map"`
	Custom    Custom         `json:"custom"`
	CustomPtr *Custom        `json:"custom_ptr"`
	Text      Textual        `json:"text"`
	TextPtr   *Textual       `json:"text_ptr"`
	Plain     string         `json:"plain"`
}

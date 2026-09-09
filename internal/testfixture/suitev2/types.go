// Package suitev2 mirrors package suite, but with the encoding/json/v2 methods
// generated, so the JSON Test Suite can be run through the jsontext driven
// decoder and compared against json/v2's own reflection path.
package suitev2

import "encoding/json"

//go:generate go run github.com/mazrean/odjson

// Raw captures the wrapped value verbatim.
type Raw struct {
	X json.RawMessage `json:"x"`
}

// Value decodes the wrapped value into ordinary Go values.
type Value struct {
	X any `json:"x"`
}

// Typed exercises the generated scalar, slice and map decoders.
type Typed struct {
	X Inner `json:"x"`
}

// Inner is the object the Typed decoder expects.
type Inner struct {
	S string            `json:"s"`
	N float64           `json:"n"`
	B bool              `json:"b"`
	L []int             `json:"l"`
	M map[string]string `json:"m"`
	A any               `json:"a"`
	P *Inner            `json:"p"`
}

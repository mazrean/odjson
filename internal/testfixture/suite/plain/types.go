// Package plain declares suite's types a second time, with no generated codec
// attached, so encoding/json stays the reference the suite is measured against.
// See [github.com/mazrean/odjson/internal/testfixture/plainref].
package plain

import "encoding/json"

// Raw captures the wrapped value verbatim, exercising the json.RawMessage path.
type Raw struct {
	X json.RawMessage `json:"x"`
}

// Value decodes the wrapped value into ordinary Go values, exercising the
// generated any decoder.
type Value struct {
	X any `json:"x"`
}

// Typed exercises the generated scalar, slice and map decoders, which reject
// most cases on type grounds rather than on syntax.
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

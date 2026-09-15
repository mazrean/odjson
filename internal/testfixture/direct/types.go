// Package direct covers the -direct flag: the package level Marshal, Append
// and Unmarshal functions generated per struct type, alongside the four
// standard methods rather than instead of them.
//
// Their oracle is the standard entry points on the very same types. A
// -direct function is only worth having if it is the same codec with the
// library taken out of the way, so MarshalRoot is pinned against
// encoding/json's Marshal and UnmarshalRoot against encoding/json/v2's
// Unmarshal, which is the pair of semantics the generated methods follow.
package direct

//go:generate go run github.com/mazrean/odjson -direct

// Root is exported, so its direct functions are exported too.
type Root struct {
	Name string `json:"name"`
	// Count is omitempty on a number, which json/v2 keeps at zero and
	// encoding/json v1 drops.
	Count int `json:"count,omitempty"`
	// Tags is left nil by some of the cases, which json/v2 encodes as []
	// and v1 as null.
	Tags   []string `json:"tags"`
	Ratio  float64  `json:"ratio"`
	Nested nested   `json:"nested"`
	Ptr    *nested  `json:"ptr"`
}

// nested is unexported, so its direct functions are unexported too:
// marshalNested, appendNested and unmarshalNested.
type nested struct {
	ID   int               `json:"id"`
	Meta map[string]string `json:"meta"`
	On   bool              `json:"on,omitempty"`
}

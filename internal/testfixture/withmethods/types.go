// Package withmethods exercises the generated MarshalJSON/UnmarshalJSON and
// encoding/json/v2 methods, which make every JSON library route through
// odjson's generated codec.
package withmethods

//go:generate go run github.com/mazrean/odjson -methods

// Address is nested inside Person.
type Address struct {
	Street string `json:"street"`
	City   string `json:"city,omitempty"`
}

// Person is the type under test.
type Person struct {
	Name    string   `json:"name"`
	Age     int      `json:"age"`
	Email   *string  `json:"email,omitempty"`
	Tags    []string `json:"tags,omitempty"`
	Address Address  `json:"address"`
}

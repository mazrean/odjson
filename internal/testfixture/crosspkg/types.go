// Package crosspkg references struct types declared in another package, which
// odjson encodes through generated helper functions rather than methods.
package crosspkg

import "github.com/mazrean/odjson/internal/testfixture/other"

//go:generate go run github.com/mazrean/odjson

// Holder is the type under test.
type Holder struct {
	Thing   other.Thing            `json:"thing"`
	Ptr     *other.Thing           `json:"ptr"`
	Wrapper other.Wrapper          `json:"wrapper"`
	List    []other.Thing          `json:"list"`
	Map     map[string]other.Thing `json:"map"`
	Deep    []*other.Wrapper       `json:"deep"`
}

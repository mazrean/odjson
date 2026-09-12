//go:build odjson_safe

package odjsonrt

// The plain conversions, for a build without the assembled interfaces of
// box.go.

type boxes struct{}

func (c *StringCache) boxString(s string) any { return s }
func (c *StringCache) boxSlice(a []any) any   { return a }
func (c *StringCache) boxFloat(f float64) any { return f }

// Package gen holds the same vendored payload types as package plain, but with
// odjson's generated codecs attached. Comparing the two packages isolates the
// effect of the generated code from every other difference.
package gen

// The benchmarks measure both modes, so the methods are generated here even
// though they are not the default.
//go:generate go run github.com/mazrean/odjson -methods

//go:build odjson_safe

package odjsonrt

// One allocation per scalar slice, for a build without the carved slices
// of slab.go.

// Scalar is the element types a decoder may carve from a chunk: the
// ones without pointers.
type Scalar interface {
	~bool | ~int | ~int8 | ~int16 | ~int32 | ~int64 |
		~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr |
		~float32 | ~float64
}

// sliceMin is the capacity a scalar slice starts with.
const sliceMin = 4

// SliceFrom returns an empty slice of T to append a JSON array's elements
// to. See slab.go for the build that carves it from a chunk.
func SliceFrom[T Scalar](c *StringCache) []T { return make([]T, 0, sliceMin) }

// SliceDone finishes a slice [SliceFrom] began.
func SliceDone[T Scalar](c *StringCache, s []T) []T { return s }

//go:build !odjson_safe

package odjsonrt

import (
	"slices"
	"unsafe"
)

// Scalar slices without an allocation each.
//
// A slice of numbers or booleans nested in another slice, the points of a
// GeoJSON ring or the rows of a matrix, is one allocation per element of
// the outer slice, most of them a few bytes, and each is an object for the
// collector: canada.json is 55k of them. The elements have no pointers, so
// their bytes can come from the same chunks [StringCache.alloc] carves
// strings from. A generated decoder asks [SliceFrom] for an empty slice
// whose capacity is the free tail of the current chunk, appends the
// elements to it, and hands it to [SliceDone], which clips it to its
// length, so that an append by the caller reallocates instead of writing
// into the neighbour, and gives the rest of the tail back. A slice that
// outgrew the tail was moved to the heap by append and comes back as it
// is, and the whole tail returns to the chunk. The trade is the string
// slab's: two floats keep their chunk alive for as long as they are
// reachable. The odjson_safe tag selects an allocation per slice
// (slab_safe.go).

// Scalar is the element types a decoder may carve from a chunk: the
// ones without pointers.
type Scalar interface {
	~bool | ~int | ~int8 | ~int16 | ~int32 | ~int64 |
		~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr |
		~float32 | ~float64
}

// sliceMin is the fewest elements a tail is worth carving for; a shorter
// tail is left behind for a fresh chunk.
const sliceMin = 4

// SliceFrom returns an empty slice of T to append a JSON array's elements
// to, with the free tail of c's current chunk as its capacity. The tail is
// reserved until [SliceDone] hands the slice back, which must happen
// before the cache carves anything else; a decoder that fails in between
// leaves the tail behind. A nil cache allocates.
func SliceFrom[T Scalar](c *StringCache) []T {
	if c == nil {
		return make([]T, 0, sliceMin)
	}
	sl := c.slab
	if sl == nil {
		sl = new(slab)
		c.slab = sl
	}
	var zero T
	size, align := int(unsafe.Sizeof(zero)), uintptr(unsafe.Alignof(zero))
	pad := int(-uintptr(unsafe.Pointer(unsafe.SliceData(sl.free))) & (align - 1))
	if len(sl.free) < pad+sliceMin*size || sl.reserved != nil {
		sl.free = make([]byte, slabSize)
		pad = int(-uintptr(unsafe.Pointer(unsafe.SliceData(sl.free))) & (align - 1))
	}
	tail := sl.free[pad:]
	sl.reserved = tail
	sl.free = nil
	return unsafe.Slice((*T)(unsafe.Pointer(unsafe.SliceData(tail))), len(tail)/size)[:0]
}

// CarveStrings is [CarveSlice] for a []string, whose elements point at
// bytes: they come from a chunk of string headers of their own, which the
// collector scans, rather than from the byte chunk. A nil cache and a
// request larger than a chunk allocate.
func CarveStrings(c *StringCache, n int) []string {
	if c == nil || n > carveStrings {
		return make([]string, 0, n)
	}
	sl := c.slab
	if sl == nil {
		sl = new(slab)
		c.slab = sl
	}
	if len(sl.strs) < n {
		sl.strs = make([]string, carveStrings)
	}
	s := sl.strs[:0:n]
	sl.strs = sl.strs[n:]
	return s
}

// carveStrings is the size of a chunk of string headers, 4 KiB.
const carveStrings = 256

// CarveElems is [CarveSlice] for a T the collector has to scan — a struct
// with a string in it — at the capacity the field's hint h gives, carved
// from a 4 KiB chunk of T the cache keeps per field, so that a document's
// []Author costs a bump of that chunk's tail rather than an object. A nil
// cache and a capacity larger than a chunk allocate.
func CarveElems[T any](c *StringCache, h *CapHint) []T {
	var zero T
	size := int(unsafe.Sizeof(zero))
	n := CapFor[T](h)
	if c == nil || size == 0 || n*size > carveElemsBytes {
		return make([]T, 0, n)
	}
	sl := c.slab
	if sl == nil {
		sl = new(slab)
		c.slab = sl
	}
	i := h.elemSlot()
	if i >= len(sl.typed) {
		sl.typed = append(sl.typed, make([]typedChunk, i+1-len(sl.typed))...)
	}
	ch := &sl.typed[i]
	if ch.size-ch.used < n {
		chunk := make([]T, carveElemsBytes/size)
		ch.base = unsafe.Pointer(unsafe.SliceData(chunk))
		ch.used = 0
		ch.size = len(chunk)
	}
	s := unsafe.Slice((*T)(unsafe.Add(ch.base, ch.used*size)), n)[:0]
	ch.used += n
	return s
}

// carveElemsBytes is the size of a chunk of T, 4 KiB.
const carveElemsBytes = 4096

// CarveSlice returns an empty slice of T with room for n elements, carved
// from c's current chunk: the slices a struct's fields hold are a few
// elements each, and each was an allocation and an object for the
// collector where a carve is a bump of the chunk's tail. The capacity is
// the caller's [CapFor] hint, so nothing is reserved and nothing given
// back: an array longer than the hint moves to the heap by append, as any
// slice does. A nil cache, a request the chunk's size cannot hold, and a
// chunk whose tail [SliceFrom] has reserved allocate instead.
func CarveSlice[T Scalar](c *StringCache, n int) []T {
	if c == nil {
		return make([]T, 0, n)
	}
	var zero T
	size := int(unsafe.Sizeof(zero))
	need := n * size
	if need > slabMax {
		return make([]T, 0, n)
	}
	sl := c.slab
	if sl == nil {
		sl = new(slab)
		c.slab = sl
	}
	if sl.reserved != nil {
		return make([]T, 0, n)
	}
	// Every scalar's alignment divides eight, and a fresh chunk is at
	// least that aligned; only a tail left by a string may not be.
	pad := int(-uintptr(unsafe.Pointer(unsafe.SliceData(sl.free))) & (uintptr(unsafe.Alignof(zero)) - 1))
	if len(sl.free) < pad+need {
		sl.free = make([]byte, slabSize)
		pad = 0
	}
	tail := sl.free[pad:]
	sl.free = tail[need:]
	return unsafe.Slice((*T)(unsafe.Pointer(unsafe.SliceData(tail))), n)[:0]
}

// SliceDone finishes a slice [SliceFrom] began: the elements appended to
// it are kept, clipped to their length, and the rest of the reserved tail
// returns to the chunk. A slice append moved to the heap, and any other
// slice, comes back unchanged.
func SliceDone[T Scalar](c *StringCache, s []T) []T {
	if c == nil || c.slab == nil || c.slab.reserved == nil {
		return s
	}
	sl := c.slab
	tail := sl.reserved
	sl.reserved = nil
	if cap(s) == 0 || unsafe.Pointer(unsafe.SliceData(s)) != unsafe.Pointer(unsafe.SliceData(tail)) {
		sl.free = tail
		return s
	}
	var zero T
	sl.free = tail[len(s)*int(unsafe.Sizeof(zero)):]
	return slices.Clip(s)
}

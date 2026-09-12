package odjsonrt

import (
	"sync/atomic"
	"unsafe"
)

// SizeHint remembers how large a type's JSON encoding has been so far, so that
// the next encoding can be written into a buffer allocated once at the right
// size.
//
// Generated Marshal functions must hand their result to the caller, so they
// cannot borrow a pooled buffer and return it. Sizing the single allocation
// correctly is the next best thing: it removes both the growth reallocations
// of an undersized buffer and the copy a pooled buffer would need.
//
// The zero value is ready to use. A SizeHint is safe for concurrent use.
type SizeHint struct {
	n atomic.Int64
}

// minHint is the smallest buffer New hands out; small enough not to waste
// memory on tiny structs, large enough that they never grow.
const minHint = 64

// New returns an empty buffer sized for the largest encoding recorded so far,
// with an eighth of headroom.
func (h *SizeHint) New() []byte {
	n := int(h.n.Load())
	n += n / 8
	if n < minHint {
		n = minHint
	}
	return make([]byte, 0, n)
}

// Need reports the capacity a buffer must have for the largest encoding seen
// so far to fit without growing.
func (h *SizeHint) Need() int {
	n := int(h.n.Load())
	n += n / 8
	if n < minHint {
		n = minHint
	}
	return n
}

// Record notes the length of a finished encoding. The hint only ever grows, so
// a single large value does not make every later call allocate small buffers
// and grow them again.
func (h *SizeHint) Record(b []byte) {
	n := int64(len(b))
	for {
		old := h.n.Load()
		if n <= old {
			return
		}
		if h.n.CompareAndSwap(old, n) {
			return
		}
	}
}

// CapHint remembers how many elements a slice field has held, so that the
// next decode of that field can allocate the slice once at that size. Grown
// by appending from nothing, a slice of a hundred large structs costs five
// reallocations and copies every element but the last; with the hint it
// costs one allocation and no copy.
//
// The hint follows the largest length seen, and it comes back down when a
// decode finds it more than four times too large, so one outlying document
// does not make every later value of the field carry its capacity. Between
// those two moves a decode only reads it. The zero value is ready to use and
// safe for concurrent use.
type CapHint struct {
	n atomic.Int64
}

const (
	// minCap is the capacity a slice gets before its field has a hint, and
	// the least a hint ever asks for.
	minCap = 4
	// maxCapBytes bounds what a hint alone can make a decode allocate.
	maxCapBytes = 1 << 20
)

// CapFor returns the capacity to allocate for a slice of T about to be
// decoded into the field h describes.
func CapFor[T any](h *CapHint) int {
	n := int(h.n.Load())
	if n <= minCap {
		return minCap
	}
	// Sizeof does not evaluate its operand, so nothing is allocated here.
	if size := int(unsafe.Sizeof(*new(T))); size > 0 && n > maxCapBytes/size {
		return max(maxCapBytes/size, minCap)
	}
	return n
}

// Record notes the length of a non-empty slice a decode has just filled in.
// A larger length raises the hint; a length below a quarter of it lowers the
// hint to twice that length. Each is a single attempt: losing the race to
// another decode of the same field only means that decode's length stands.
func (h *CapHint) Record(n int) {
	old := h.n.Load()
	switch l := int64(n); {
	case l > old:
		h.n.CompareAndSwap(old, l)
	case l*4 < old && old > minCap:
		h.n.CompareAndSwap(old, l*2)
	}
}

package odjsonrt

import "sync/atomic"

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

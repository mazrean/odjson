package odjsonrt

import (
	"testing"
	"unsafe"
)

// TestSliceFromDone checks the contract a generated decoder relies on:
// consecutive slices are distinct, each is clipped to its length so an
// append by the caller cannot reach the next, a slice that outgrows the
// tail is a heap slice, and the tail comes back for the strings.
func TestSliceFromDone(t *testing.T) {
	c := new(StringCache)
	a := SliceFrom[float64](c)
	if len(a) != 0 || cap(a) < sliceMin {
		t.Fatalf("SliceFrom: len %d cap %d", len(a), cap(a))
	}
	a = append(a, 1.5, 2.5)
	a = SliceDone(c, a)
	if len(a) != 2 || cap(a) != 2 || a[0] != 1.5 || a[1] != 2.5 {
		t.Fatalf("SliceDone: %v len %d cap %d", a, len(a), cap(a))
	}
	b := SliceFrom[float64](c)
	b = append(b, 3.5)
	b = SliceDone(c, b)
	if unsafe.Pointer(unsafe.SliceData(b)) != unsafe.Add(unsafe.Pointer(unsafe.SliceData(a)), 16) {
		t.Errorf("second slice does not follow the first in the chunk")
	}
	// An append by the caller reallocates rather than writing into b.
	a = append(a, 9)
	if b[0] != 3.5 {
		t.Fatalf("append into a clipped slice reached its neighbour: %v", b)
	}
	if len(a) != 3 || a[2] != 9 {
		t.Fatalf("append after SliceDone: %v", a)
	}

	// Mixed element types keep their alignment.
	bs := SliceFrom[bool](c)
	bs = append(bs, true, false, true)
	bs = SliceDone(c, bs)
	fs := SliceFrom[float64](c)
	if uintptr(unsafe.Pointer(unsafe.SliceData(fs)))%8 != 0 {
		t.Errorf("float64 slice misaligned after a bool slice")
	}
	fs = append(fs, 7)
	fs = SliceDone(c, fs)
	if len(bs) != 3 || !bs[0] || bs[1] || !bs[2] || fs[0] != 7 {
		t.Errorf("bs %v fs %v", bs, fs)
	}
	i8 := SliceFrom[int8](c)
	i8 = append(i8, -1, 2)
	i8 = SliceDone(c, i8)
	if len(i8) != 2 || cap(i8) != 2 || i8[0] != -1 || i8[1] != 2 {
		t.Errorf("int8 slice %v cap %d", i8, cap(i8))
	}

	// A slice that outgrows the tail is moved to the heap by append and
	// comes back as is; the tail is returned whole.
	free := len(c.slab.free)
	big := SliceFrom[float64](c)
	for i := range slabSize {
		big = append(big, float64(i))
	}
	big = SliceDone(c, big)
	if len(big) != slabSize || big[slabSize-1] != float64(slabSize-1) {
		t.Fatalf("big slice: len %d", len(big))
	}
	if got := len(c.slab.free); got > free || got < free-7 {
		// The tail comes back whole, less the bytes that aligned it.
		t.Errorf("tail after an outgrown slice: %d, want %d less alignment", got, free)
	}
	if c.slab.reserved != nil {
		t.Error("tail still reserved")
	}

	// Strings carved after a slice do not overlap it.
	s := c.alloc([]byte("after"))
	if s != "after" || fs[0] != 7 || i8[0] != -1 {
		t.Errorf("string carve disturbed a slice: %q %v %v", s, fs, i8)
	}

	// A nil cache allocates, and a slice that is not the cache's comes
	// back untouched.
	n := SliceFrom[int](nil)
	n = append(n, 1)
	n = SliceDone(nil, n)
	if len(n) != 1 || n[0] != 1 {
		t.Errorf("nil cache: %v", n)
	}
	own := make([]int, 1, 8)
	if got := SliceDone(c, own); cap(got) != 8 {
		t.Errorf("foreign slice clipped: cap %d", cap(got))
	}
}

// TestSliceFromAbandoned checks that a decoder failing between SliceFrom
// and SliceDone costs the tail and nothing else: the next slice starts a
// fresh chunk, and a pooled cache gets the tail back.
func TestSliceFromAbandoned(t *testing.T) {
	c := new(StringCache)
	a := SliceFrom[float64](c)
	a = append(a, 1)
	b := SliceFrom[float64](c)
	b = append(b, 2)
	b = SliceDone(c, b)
	if a[0] != 1 || b[0] != 2 {
		t.Fatalf("a %v b %v", a, b)
	}
	if unsafe.SliceData(a) == unsafe.SliceData(b) {
		t.Fatal("an abandoned slice shares its tail with the next")
	}
	x := SliceFrom[int32](c)
	x = append(x, 3)
	PutStringCache(c)
	if c.slab.reserved != nil || len(c.slab.free) == 0 {
		t.Error("PutStringCache left the tail reserved")
	}
	if x[0] != 3 {
		t.Error("abandoned elements disturbed")
	}
}

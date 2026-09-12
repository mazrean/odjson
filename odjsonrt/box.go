//go:build !odjson_safe

package odjsonrt

import "unsafe"

// Boxing without an allocation per value.
//
// An interface value is two words, the type and a pointer to the value, and
// converting a string, a slice or a float to any allocates the value it
// points to: sixteen bytes for a string header, twenty-four for a slice
// header, eight for a float. The any decoder produces hundreds of those per
// document, and each is an object for the collector. The boxes here point
// into chunks instead: a chunk of string headers, one of slice headers, one
// of floats, carved forward the way [StringCache.alloc] carves bytes, and
// the interface is assembled from the type word of a real any and the
// address of the slot. The runtime's own conversion does exactly this with
// a fresh allocation for the slot; the layout it relies on is the one
// reflect and every package that reads an interface's words rely on. It is
// checked at init all the same, and the odjson_safe tag selects the plain
// conversions in box_safe.go, as it does for the direct path.

// eface is the runtime's empty interface, as far as the boxes need it.
type eface struct {
	typ  unsafe.Pointer
	data unsafe.Pointer
}

// The type words, taken from real interface values.
var (
	stringType = (*eface)(unsafe.Pointer(&stringAny)).typ
	sliceType  = (*eface)(unsafe.Pointer(&sliceAny)).typ
	floatType  = (*eface)(unsafe.Pointer(&floatAny)).typ

	stringAny any = ""
	sliceAny  any = []any(nil)
	floatAny  any = float64(0)
)

// boxOK says whether the assembled interfaces come out as the conversions
// would have; when they do not, the boxes convert.
var boxOK = func() bool {
	if unsafe.Sizeof(any(nil)) != 2*unsafe.Sizeof(uintptr(0)) {
		return false
	}
	s := "probe"
	f := 1.5
	a := []any{1}
	var bs, bf, ba any
	*(*eface)(unsafe.Pointer(&bs)) = eface{stringType, unsafe.Pointer(&s)}
	*(*eface)(unsafe.Pointer(&bf)) = eface{floatType, unsafe.Pointer(&f)}
	*(*eface)(unsafe.Pointer(&ba)) = eface{sliceType, unsafe.Pointer(&a)}
	gs, oks := bs.(string)
	gf, okf := bf.(float64)
	ga, oka := ba.([]any)
	return oks && gs == s && okf && gf == f && oka && len(ga) == 1 && bs == any(s) && bf == any(f)
}()

// boxes is the free tail of each header chunk.
type boxes struct {
	strs   []string
	slices [][]any
	floats []float64
}

// boxString returns s as an any whose header lives in a chunk.
func (c *StringCache) boxString(s string) any {
	if !boxOK || c == nil {
		return s
	}
	b := c.boxes()
	if len(b.strs) == 0 {
		b.strs = make([]string, boxStrings)
	}
	p := &b.strs[0]
	*p = s
	b.strs = b.strs[1:]
	var v any
	*(*eface)(unsafe.Pointer(&v)) = eface{stringType, unsafe.Pointer(p)}
	return v
}

// boxSlice returns a as an any whose header lives in a chunk.
func (c *StringCache) boxSlice(a []any) any {
	if !boxOK || c == nil {
		return a
	}
	b := c.boxes()
	if len(b.slices) == 0 {
		b.slices = make([][]any, boxSlices)
	}
	p := &b.slices[0]
	*p = a
	b.slices = b.slices[1:]
	var v any
	*(*eface)(unsafe.Pointer(&v)) = eface{sliceType, unsafe.Pointer(p)}
	return v
}

// boxFloat returns f as an any whose value lives in a chunk.
func (c *StringCache) boxFloat(f float64) any {
	if !boxOK || c == nil {
		return f
	}
	b := c.boxes()
	if len(b.floats) == 0 {
		b.floats = make([]float64, boxFloats)
	}
	p := &b.floats[0]
	*p = f
	b.floats = b.floats[1:]
	var v any
	*(*eface)(unsafe.Pointer(&v)) = eface{floatType, unsafe.Pointer(p)}
	return v
}

// boxes returns the cache's header chunks, allocated on first use.
func (c *StringCache) boxes() *boxes {
	sl := c.slab
	if sl == nil {
		sl = new(slab)
		c.slab = sl
	}
	if sl.boxes == nil {
		sl.boxes = new(boxes)
	}
	return sl.boxes
}

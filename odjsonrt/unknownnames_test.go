package odjsonrt

import "testing"

func TestUnknownNames(t *testing.T) {
	names, mark := UnknownNames(nil)
	if names != nil || mark != 0 {
		t.Fatalf("nil cache: got %v, %d", names, mark)
	}
	names = AddUnknownName(nil, names, []byte("a"))
	if len(names) != 1 {
		t.Fatalf("nil cache append: got %d names", len(names))
	}
	EndUnknownNames(nil, names, mark)

	c := new(StringCache)
	// Warm the scratch, as a pooled cache would be: room for names in a
	// backing array that a later decode's objects will share.
	warm, wm := UnknownNames(c)
	for _, n := range []string{"a", "b", "c", "d"} {
		warm = AddUnknownName(c, warm, []byte(n))
	}
	EndUnknownNames(c, warm, wm)

	outer, om := UnknownNames(c)
	outer = AddUnknownName(c, outer, []byte("x"))
	outer = AddUnknownName(c, outer, []byte("y"))
	// A nested object's names start after the outer one's, which the
	// outer published as it added them; without that the nested object
	// would write over "x" in the shared backing array.
	inner, im := UnknownNames(c)
	if im != 2 {
		t.Fatalf("nested mark: got %d, want 2", im)
	}
	inner = AddUnknownName(c, inner, []byte("z"))
	EndUnknownNames(c, inner, im)
	if len(*c.names) != 2 || string(outer[0]) != "x" || string(outer[1]) != "y" {
		t.Fatalf("after inner: list %q, outer %q", *c.names, outer)
	}
	EndUnknownNames(c, outer, om)
	if len(*c.names) != 0 || cap(*c.names) == 0 {
		t.Fatalf("after outer: len %d cap %d", len(*c.names), cap(*c.names))
	}
	for _, n := range (*c.names)[:cap(*c.names)] {
		if n != nil {
			t.Fatalf("a name was kept after the object closed: %q", n)
		}
	}

	// A decoder that fails leaves its names; the pool clears them.
	*c.names = append(*c.names, []byte("left"))
	PutStringCache(c)
	if len(*c.names) != 0 || (*c.names)[:1][0] != nil {
		t.Fatalf("PutStringCache kept a name: %v", (*c.names)[:1])
	}

	// Generated code before this change never touched the scratch, and a
	// cache is a comparable type that callers may compare.
	var zero StringCache
	if zero != (StringCache{}) {
		t.Fatal("StringCache must stay comparable")
	}
}

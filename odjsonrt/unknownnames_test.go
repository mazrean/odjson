package odjsonrt

import "testing"

func TestUnknownNames(t *testing.T) {
	names, mark := UnknownNames(nil)
	if names != nil || mark != 0 {
		t.Fatalf("nil cache: got %v, %d", names, mark)
	}
	EndUnknownNames(nil, append(names, []byte("a")), mark)

	c := new(StringCache)
	outer, om := UnknownNames(c)
	outer = append(outer, []byte("x"), []byte("y"))
	// A nested object starts after the outer one's names.
	inner, im := UnknownNames(c)
	if im != 0 {
		// The outer object has not handed its list back yet, so the
		// nested one sees an empty cache list: that is the contract, and
		// the outer names stay in the outer decoder's own slice.
		t.Fatalf("nested mark: got %d, want 0", im)
	}
	inner = append(inner, []byte("z"))
	EndUnknownNames(c, inner, im)
	if len(*c.names) != 0 {
		t.Fatalf("after inner: %d names kept", len(*c.names))
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

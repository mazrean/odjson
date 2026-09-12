package odjsonrt

import (
	"strings"
	"testing"
)

// TestSlabStringsSurviveReuse pins the rule the chunk allocator rests on: a
// string carved from a chunk is never written again, whatever the cache
// decodes afterwards, through the pool and across chunk boundaries.
func TestSlabStringsSurviveReuse(t *testing.T) {
	first := []string{"alpha", "beta\\n", "日本語", strings.Repeat("x", slabMax), strings.Repeat("y", slabMax+1), ""}
	var doc []byte
	for _, s := range first {
		doc = append(doc, '"')
		doc = append(doc, s...)
		doc = append(doc, '"', ' ')
	}
	c := GetStringCache()
	var got []string
	for p := 0; p < len(doc); {
		s, next, err := ParseStringStrict(doc, p, c)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, s)
		p = SkipSpace(doc, next)
	}
	PutStringCache(c)

	// Enough distinct strings to walk through several chunks, four times
	// over with the pool warm.
	var filler []byte
	for i := range 4 * slabSize / 16 {
		filler = append(filler, '"')
		filler = append(filler, strings.Repeat(string(rune('a'+i%26)), 8+i%5)...)
		filler = append(filler, '"', ' ')
	}
	for range 4 {
		c := GetStringCache()
		for p := 0; p < len(filler); {
			_, next, err := ParseStringStrict(filler, p, c)
			if err != nil {
				t.Fatal(err)
			}
			p = SkipSpace(filler, next)
		}
		PutStringCache(c)
	}

	want := []string{"alpha", "beta\n", "日本語", strings.Repeat("x", slabMax), strings.Repeat("y", slabMax+1), ""}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("string %d changed: got %q, want %q", i, got[i], want[i])
		}
	}
}

// TestSlabAllocEdges covers the shapes alloc treats specially.
func TestSlabAllocEdges(t *testing.T) {
	var nilCache *StringCache
	if s := nilCache.alloc([]byte("abc")); s != "abc" {
		t.Errorf("nil cache: %q", s)
	}
	c := new(StringCache)
	if s := c.alloc(nil); s != "" {
		t.Errorf("empty: %q", s)
	}
	long := make([]byte, slabMax+1)
	if s := c.alloc(long); len(s) != slabMax+1 || c.slab != nil {
		t.Errorf("a string past slabMax must be allocated on its own")
	}
	a := c.alloc([]byte("first"))
	// Use the chunk up until slabMax bytes no longer fit, then carve
	// slabMax: that must start a new chunk and leave the tail behind.
	for len(c.slab.free) >= slabMax {
		c.alloc(make([]byte, slabMax))
	}
	b := c.alloc(make([]byte, slabMax))
	if len(c.slab.free) != slabSize-slabMax {
		t.Errorf("a string that does not fit must start a new chunk: free %d", len(c.slab.free))
	}
	if a != "first" || len(b) != slabMax {
		t.Errorf("carved strings changed: %q %d", a, len(b))
	}
	if s, ok := c.unquoteString([]byte(`aé\n`), true); !ok || s != "aé\n" {
		t.Errorf("unquoteString: %q %v", s, ok)
	}
	if _, ok := c.unquoteString([]byte(`\ud800`), true); ok {
		t.Errorf("unquoteString accepted an unpaired surrogate under strict")
	}
}

package odjsonrt

import (
	"reflect"
	"runtime"
	"testing"
)

// TestBoxedValuesMatchConversions decodes a document through the any
// decoder with a cache, whose interface values are assembled by hand, and
// without one, whose values are the language's conversions, and requires
// the two results to be indistinguishable: equal by reflection, of the
// same dynamic types, and still so after the collector has run.
func TestBoxedValuesMatchConversions(t *testing.T) {
	doc := []byte(`{"s":"a string","f":1.5,"big":123456789,"n":7,"a":["x",2.5,[],[1,"y"]],"o":{"k":"v","e":[]},"b":true,"z":null}`)
	c := GetStringCache()
	got, _, err := ParseAnyStrict(doc, 0, c)
	if err != nil {
		t.Fatal(err)
	}
	PutStringCache(c)
	want, _, err := ParseAnyStrict(doc, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	check := func() {
		t.Helper()
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("boxed values differ:\n got %#v\nwant %#v", got, want)
		}
		m := got.(map[string]any)
		if s, ok := m["s"].(string); !ok || s != "a string" {
			t.Errorf("s: %T %v", m["s"], m["s"])
		}
		if f, ok := m["f"].(float64); !ok || f != 1.5 {
			t.Errorf("f: %T %v", m["f"], m["f"])
		}
		if a, ok := m["a"].([]any); !ok || len(a) != 4 || a[0] != any("x") || a[1] != any(2.5) {
			t.Errorf("a: %T %v", m["a"], m["a"])
		}
		if e, ok := m["o"].(map[string]any)["e"].([]any); !ok || len(e) != 0 {
			t.Errorf("e: %T %v", m["o"].(map[string]any)["e"], e)
		}
		if m["s"] != any("a string") || m["f"] != any(1.5) {
			t.Errorf("boxed values do not compare equal to conversions")
		}
	}
	check()
	// Fill the header chunks a few times over with the pool warm, then
	// collect: the values above must be untouched and still reachable
	// through their chunks.
	for range 4 {
		c := GetStringCache()
		for range 2 * boxStrings {
			if _, _, err := ParseAnyStrict([]byte(`["filler", 2.25, [3.75]]`), 0, c); err != nil {
				t.Fatal(err)
			}
		}
		PutStringCache(c)
	}
	runtime.GC()
	runtime.GC()
	check()
}

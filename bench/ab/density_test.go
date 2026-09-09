package ab

import (
	jsonv1 "encoding/json"
	jsonv2 "encoding/json/v2"
	"testing"

	"github.com/mazrean/odjson/bench/plain"
)

// TestDensity reports the member density of the fixtures.
//
// It exists because density decides which shape of MarshalJSONTo could win: the
// value driven form odjson generates pays per byte, since jsontext reformats
// the whole value, while a token driven form would pay per member instead.
// bench/proto measures both at two densities — at 16 bytes per member the
// token form is 18% slower than the value form, at 1600 bytes per member it is
// 40% faster. This test says where the real fixtures sit between those points;
// twitter lands at ~34 bytes per member, close enough to the dense end that
// switching would not pay, which is why the generator emits the value form.
func TestDensity(t *testing.T) {
	count := func(b []byte) (members, arrays int) {
		var walk func(any)
		walk = func(v any) {
			switch x := v.(type) {
			case map[string]any:
				members += len(x)
				for _, e := range x {
					walk(e)
				}
			case []any:
				arrays++
				for _, e := range x {
					walk(e)
				}
			}
		}
		var v any
		if err := jsonv1.Unmarshal(b, &v); err != nil {
			t.Fatal(err)
		}
		walk(v)
		return
	}

	tw := new(plain.TwitterStruct)
	if err := jsonv1.Unmarshal(twitterJSON, tw); err != nil {
		t.Fatal(err)
	}
	out, err := jsonv2.Marshal(tw)
	if err != nil {
		t.Fatal(err)
	}
	m, a := count(out)
	t.Logf("twitter: %d bytes, %d members, %d arrays -> %.1f bytes/member", len(out), m, a, float64(len(out))/float64(m))

	bk := new(plain.Book)
	if err := jsonv1.Unmarshal(plain.SmallPayload(), bk); err != nil {
		t.Fatal(err)
	}
	out, err = jsonv2.Marshal(bk)
	if err != nil {
		t.Fatal(err)
	}
	m, a = count(out)
	t.Logf("small:   %d bytes, %d members, %d arrays -> %.1f bytes/member", len(out), m, a, float64(len(out))/float64(m))
}

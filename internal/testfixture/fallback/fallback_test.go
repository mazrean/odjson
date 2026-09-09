package fallback

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

// plainFallbacks is Fallbacks without the generated methods, so encoding/json
// reflects over it the way it did before odjson ran. None of the field types
// carries a generated codec of its own — Custom and Textual carry hand written
// ones, which the oracle is meant to use — so a defined type is enough here;
// no separate package is needed.
type plainFallbacks Fallbacks

// marshalParity compares the generated MarshalJSON against encoding/json. The
// method is called directly because json.Marshal prefers MarshalJSONTo, which
// follows json/v2's semantics instead.
func marshalParity(t *testing.T, name string, v Fallbacks) {
	t.Helper()
	want, wantErr := json.Marshal(plainFallbacks(v))
	got, gotErr := v.MarshalJSON()
	if (wantErr != nil) != (gotErr != nil) {
		t.Errorf("%s: error mismatch: encoding/json=%v odjson=%v", name, wantErr, gotErr)
		return
	}
	if wantErr != nil {
		return
	}
	if !bytes.Equal(want, got) {
		t.Errorf("%s:\n encoding/json: %s\n odjson:        %s", name, want, got)
	}
}

func unmarshalParity(t *testing.T, in string) {
	t.Helper()
	var a, b Fallbacks
	errA := json.Unmarshal([]byte(in), (*plainFallbacks)(&a))
	errB := b.UnmarshalJSON([]byte(in))
	if (errA != nil) != (errB != nil) {
		t.Errorf("error mismatch for %s: encoding/json=%v odjson=%v", in, errA, errB)
		return
	}
	if errA == nil && !reflect.DeepEqual(a, b) {
		t.Errorf("value mismatch for %s:\n encoding/json: %#v\n odjson:        %#v", in, a, b)
	}
}

func TestMarshalFallbacks(t *testing.T) {
	full := Fallbacks{
		Generic:    Box[int]{Value: 3},
		GenericPtr: &Box[string]{Value: "s"},
		IntMap:     map[int]string{2: "two", 1: "one", -1: "minus"},
		Custom:     Custom{N: 5},
		CustomPtr:  &Custom{N: 6},
		Text:       Textual{S: "hello"},
		TextPtr:    &Textual{S: "world"},
		Plain:      "plain",
	}
	full.Anon.A = 9
	marshalParity(t, "full", full)
	marshalParity(t, "zero", Fallbacks{})

	failing := full
	failing.Text = Textual{S: "!"}
	marshalParity(t, "text-marshal-error", failing)
}

func TestUnmarshalFallbacks(t *testing.T) {
	for _, in := range []string{
		`{}`,
		`{"generic":{"value":4},"generic_ptr":{"value":"s"},"anon":{"a":1}}`,
		`{"int_map":{"1":"one","2":"two"}}`,
		`{"custom":{"custom":7},"custom_ptr":{"custom":8}}`,
		`{"custom":{"nope":1}}`,
		`{"custom":null}`,
		`{"custom_ptr":null}`,
		`{"text":"text:abc","text_ptr":"text:def"}`,
		`{"text":"bad"}`,
		`{"text":null}`,
		`{"plain":"p","unknown":[1,2,3]}`,
	} {
		unmarshalParity(t, in)
	}
}

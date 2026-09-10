package unexported

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

// The oracles: a defined type strips the generated methods, so encoding/json
// reflects over it the way it did before odjson ran. They live in a _test.go
// file, which go/packages does not show the generator, so odjson never
// generates for them and a sibling plain package is not needed.
type (
	plainSecret secret
	plainLone   lone
	plainHolder Holder
)

// The compile-time assertions are the point of the fixture: an unexported type
// only carries these methods when the default selection includes it.
var (
	_ json.Marshaler   = secret{}
	_ json.Unmarshaler = &secret{}
	_ json.Marshaler   = lone{}
	_ json.Unmarshaler = &lone{}
)

func TestSecret(t *testing.T) {
	cases := map[string]secret{
		"zero": {},
		"full": {ID: 7, Name: "s"},
		"name": {Name: "only"},
	}
	for name, v := range cases {
		want, err := json.Marshal(plainSecret(v))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		got, err := v.MarshalJSON()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !bytes.Equal(want, got) {
			t.Errorf("%s:\n encoding/json: %s\n odjson:        %s", name, want, got)
		}
	}

	for _, in := range []string{
		`{}`,
		`{"id":1,"name":"a"}`,
		`{"name":"b"}`,
		`{"id":null}`,
		`{"id":"nope"}`,
	} {
		var a, b secret
		errA := json.Unmarshal([]byte(in), (*plainSecret)(&a))
		errB := b.UnmarshalJSON([]byte(in))
		if (errA != nil) != (errB != nil) {
			t.Errorf("error mismatch for %s: encoding/json=%v odjson=%v", in, errA, errB)
			continue
		}
		if errA == nil && !reflect.DeepEqual(a, b) {
			t.Errorf("value mismatch for %s:\n encoding/json: %#v\n odjson:        %#v", in, a, b)
		}
	}
}

func TestLone(t *testing.T) {
	for _, v := range []lone{{}, {Flag: true}} {
		want, err := json.Marshal(plainLone(v))
		if err != nil {
			t.Fatalf("%v: %v", v, err)
		}
		got, err := v.MarshalJSON()
		if err != nil {
			t.Fatalf("%v: %v", v, err)
		}
		if !bytes.Equal(want, got) {
			t.Errorf("%v:\n encoding/json: %s\n odjson:        %s", v, want, got)
		}
	}

	for _, in := range []string{`{}`, `{"flag":true}`, `{"flag":null}`, `{"flag":1}`} {
		var a, b lone
		errA := json.Unmarshal([]byte(in), (*plainLone)(&a))
		errB := b.UnmarshalJSON([]byte(in))
		if (errA != nil) != (errB != nil) {
			t.Errorf("error mismatch for %s: encoding/json=%v odjson=%v", in, errA, errB)
			continue
		}
		if errA == nil && a != b {
			t.Errorf("value mismatch for %s: encoding/json=%#v odjson=%#v", in, a, b)
		}
	}
}

// TestHolder checks the exported wrapper. Its oracle reflects over plainHolder,
// whose fields still reach secret's generated codec — which is what
// encoding/json would do at any call site too, and secret's own parity against
// reflection is what TestSecret pins.
func TestHolder(t *testing.T) {
	cases := map[string]Holder{
		"zero": {},
		"full": {Secret: secret{ID: 1, Name: "a"}, Ptr: &secret{ID: 2}, List: []secret{{ID: 3}, {}}},
	}
	for name, v := range cases {
		want, err := json.Marshal(plainHolder(v))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		got, err := v.MarshalJSON()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !bytes.Equal(want, got) {
			t.Errorf("%s:\n encoding/json: %s\n odjson:        %s", name, want, got)
		}
	}

	for _, in := range []string{
		`{}`,
		`{"secret":{"id":1,"name":"a"},"ptr":{"id":2}}`,
		`{"list":[{"id":3},{}]}`,
		`{"ptr":null}`,
		`{"secret":"nope"}`,
	} {
		var a, b Holder
		errA := json.Unmarshal([]byte(in), (*plainHolder)(&a))
		errB := b.UnmarshalJSON([]byte(in))
		if (errA != nil) != (errB != nil) {
			t.Errorf("error mismatch for %s: encoding/json=%v odjson=%v", in, errA, errB)
			continue
		}
		if errA == nil && !reflect.DeepEqual(a, b) {
			t.Errorf("value mismatch for %s:\n encoding/json: %#v\n odjson:        %#v", in, a, b)
		}
	}
}

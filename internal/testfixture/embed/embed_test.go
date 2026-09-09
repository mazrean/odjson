package embed

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/mazrean/odjson/internal/testfixture/embed/plain"
	"github.com/mazrean/odjson/internal/testfixture/plainref"
)

// The generated methods stand between json.Marshal and these types, so package
// plain's identical declarations are what encoding/json is left to promote
// fields on. TestSameLayout keeps the two in step.
var (
	promotedRef    = plainref.Of[Promoted, plain.Promoted]
	shallowWinsRef = plainref.Of[ShallowWins, plain.ShallowWins]
)

func TestSameLayout(t *testing.T) {
	cases := []struct {
		name string
		a, b reflect.Type
	}{
		{"Promoted", reflect.TypeFor[Promoted](), reflect.TypeFor[plain.Promoted]()},
		{"ShallowWins", reflect.TypeFor[ShallowWins](), reflect.TypeFor[plain.ShallowWins]()},
	}
	for _, tc := range cases {
		if err := plainref.SameLayout(tc.a, tc.b); err != nil {
			t.Errorf("%s: %v", tc.name, err)
		}
	}
}

// marshalParity compares the generated MarshalJSON against encoding/json's own
// promotion rules. The method is called directly because json.Marshal prefers
// MarshalJSONTo, which follows json/v2's semantics instead.
func marshalParity[T, R any](t *testing.T, name string, v T, ref func(*T) *R) {
	t.Helper()
	want, err := json.Marshal(ref(&v))
	if err != nil {
		t.Fatalf("%s: encoding/json: %v", name, err)
	}
	got, err := any(v).(json.Marshaler).MarshalJSON()
	if err != nil {
		t.Fatalf("%s: odjson: %v", name, err)
	}
	if !bytes.Equal(want, got) {
		t.Errorf("%s:\n encoding/json: %s\n odjson:        %s", name, want, got)
	}
}

func unmarshalParity[T, R any](t *testing.T, name, in string, ref func(*T) *R) {
	t.Helper()
	var a, b T
	errA := json.Unmarshal([]byte(in), ref(&a))
	errB := any(&b).(json.Unmarshaler).UnmarshalJSON([]byte(in))
	if (errA != nil) != (errB != nil) {
		t.Errorf("%s: error mismatch for %s: encoding/json=%v odjson=%v", name, in, errA, errB)
		return
	}
	if errA == nil && !reflect.DeepEqual(a, b) {
		t.Errorf("%s: value mismatch for %s:\n encoding/json: %#v\n odjson:        %#v", name, in, a, b)
	}
}

func sample() Promoted {
	p := Promoted{
		Mid: Mid{
			Deep:    Deep{DeepOnly: "d", Shared: "deep-shared"},
			Shared:  "mid-shared",
			MidOnly: 1,
		},
		LeftConflict:  LeftConflict{Clash: "l", Left: "left"},
		RightConflict: RightConflict{Clash: "r", Right: "right"},
		UntaggedSide:  UntaggedSide{Winner: "untagged"},
		TaggedSide:    TaggedSide{Winner: "tagged"},
		PtrPart:       &PtrPart{PtrOnly: 7},
		Label:         "label",
		Named:         Named{Value: 3},
		Own:           "own",
	}
	return p
}

func TestMarshalPromoted(t *testing.T) {
	marshalParity(t, "full", sample(), promotedRef)

	nilPtr := sample()
	nilPtr.PtrPart = nil
	marshalParity(t, "nil-embedded-pointer", nilPtr, promotedRef)

	marshalParity(t, "zero", Promoted{}, promotedRef)
	marshalParity(t, "shallow-wins", ShallowWins{Deep: Deep{DeepOnly: "d", Shared: "ignored"}, Shared: 5}, shallowWinsRef)
}

func TestUnmarshalPromoted(t *testing.T) {
	inputs := []string{
		`{}`,
		`{"deep_only":"d","shared":"s","mid_only":2,"left":"l","right":"r","Winner":"w","ptr_only":9,"Label":"lb","named":{"value":4},"own":"o"}`,
		`{"Clash":"dropped"}`,
		`{"ptr_only":1}`,
		`{"named":null}`,
		`{"Label":"x"}`,
	}
	for _, in := range inputs {
		unmarshalParity(t, "promoted", in, promotedRef)
	}
	unmarshalParity(t, "shallow", `{"shared":3,"deep_only":"d"}`, shallowWinsRef)
}

// TestFieldSetMatchesEncodingJSON is a direct read of the promoted member set:
// the generated encoder and encoding/json must agree on which names exist.
func TestFieldSetMatchesEncodingJSON(t *testing.T) {
	v := sample()
	b, err := json.Marshal(promotedRef(&v))
	if err != nil {
		t.Fatal(err)
	}
	var want map[string]json.RawMessage
	if err := json.Unmarshal(b, &want); err != nil {
		t.Fatal(err)
	}
	if _, ok := want["Clash"]; ok {
		t.Error("encoding/json kept the conflicting name; the fixture no longer tests what it claims")
	}
	if string(want["Winner"]) != `"tagged"` {
		t.Errorf("expected the tagged field to win, got %s", want["Winner"])
	}
	if string(want["shared"]) != `"mid-shared"` {
		t.Errorf("expected the shallower field to win, got %s", want["shared"])
	}
}

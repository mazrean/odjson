package gen_test

import (
	"bytes"
	jsonv2 "encoding/json/v2"
	"reflect"
	"testing"

	"github.com/mazrean/odjson/internal/testfixture/v2parity/gen"
	"github.com/mazrean/odjson/internal/testfixture/v2parity/plain"
)

// documents cover the shapes whose encoding differs between encoding/json and
// encoding/json/v2: nil versus empty containers, zero numbers and bools under
// omitempty, and the leaf types that carry their own representation.
var documents = []string{
	`{}`,
	`{"bool":true,"int":-7,"uint64":18446744073709551615,"float64":1.5,"string":"héllo <&> \u2028","named":"red"}`,
	`{"slice":[1,2,3],"strings":["a","b"],"bytes":"AAEC","named_bag":"aGk=","array":[1,2,3],"map":{"b":2,"a":1},"str_map":{"k":"v"}}`,
	`{"slice":[],"strings":[],"bytes":"","map":{},"str_map":{}}`,
	`{"slice":null,"strings":null,"bytes":null,"map":null,"str_map":null,"nested_ptr":null,"ptr":null}`,
	`{"nested":{"id":1,"note":"n"},"nested_ptr":{"id":2},"nesteds":[{"id":3},{"id":4,"note":"x"}],"nested_map":{"k":{"id":5}}}`,
	`{"any":{"a":[1,2,{"b":null}],"c":true},"anys":[1,"two",null,false],"raw":{"x":[1,2]},"number":"12345678901234567890"}`,
	`{"time":"2023-11-14T22:13:20Z","zero_time":"2024-01-02T03:04:05Z","zero_nested":{"id":9}}`,
	`{"omit_bool":false,"omit_int":0,"omit_float":0,"omit_string":"","omit_slice":[],"omit_map":{},"omit_ptr":null}`,
	`{"omit_bool":true,"omit_int":3,"omit_float":1.5,"omit_string":"s","omit_slice":[1],"omit_map":{"a":1},"omit_ptr":4}`,
	`{"ptr":7,"any":null}`,
}

// TestMarshalMatchesJSONV2 decodes the same document into both copies of the
// type and requires the generated MarshalJSONTo to produce exactly the bytes
// json/v2's reflection encoder produces for the reference copy.
func TestMarshalMatchesJSONV2(t *testing.T) {
	for _, doc := range documents {
		var g gen.Zoo
		var p plain.Zoo
		if err := jsonv2.Unmarshal([]byte(doc), &g); err != nil {
			t.Fatalf("%s: odjson decode: %v", doc, err)
		}
		if err := jsonv2.Unmarshal([]byte(doc), &p); err != nil {
			t.Fatalf("%s: json/v2 decode: %v", doc, err)
		}

		got, err := jsonv2.Marshal(g)
		if err != nil {
			t.Fatalf("%s: odjson encode: %v", doc, err)
		}
		want, err := jsonv2.Marshal(p)
		if err != nil {
			t.Fatalf("%s: json/v2 encode: %v", doc, err)
		}
		if !equivalent(t, got, want) {
			t.Errorf("%s:\n json/v2: %s\n odjson:  %s", doc, want, got)
		}
	}
}

// equivalent compares two encodings as JSON values rather than as bytes.
// json/v2 writes map members in Go's randomised map iteration order, so its
// own output is not byte-stable; odjson sorts them, which is deterministic and
// encodes the same value.
func equivalent(t *testing.T, a, b []byte) bool {
	t.Helper()
	var va, vb any
	if err := jsonv2.Unmarshal(a, &va); err != nil {
		t.Fatalf("re-decode %s: %v", a, err)
	}
	if err := jsonv2.Unmarshal(b, &vb); err != nil {
		t.Fatalf("re-decode %s: %v", b, err)
	}
	return reflect.DeepEqual(va, vb)
}

// TestUnmarshalMatchesJSONV2 requires the generated UnmarshalJSONFrom to accept
// what json/v2 accepts and to decode to the same value, which is compared
// through json/v2's own encoding of the reference copy.
func TestUnmarshalMatchesJSONV2(t *testing.T) {
	inputs := append([]string{
		`null`,
		`{"unknown":[1,{"deep":null}],"int":1}`,
		`{"INT":5,"NeStEd":{"ID":6}}`,
		`{"int":"nope"}`,
		`{"array":[1]}`,
		`{"array":[1,2,3,4,5]}`,
		`{"time":"not a time"}`,
		`{"bytes":"!!!!"}`,
		`{"raw":null}`,
		`{"int":1,}`,
	}, documents...)

	for _, doc := range inputs {
		var g gen.Zoo
		var p plain.Zoo
		errGot := jsonv2.Unmarshal([]byte(doc), &g)
		errWant := jsonv2.Unmarshal([]byte(doc), &p)
		if (errGot != nil) != (errWant != nil) {
			t.Errorf("%s: acceptance mismatch: json/v2=%v odjson=%v", doc, errWant, errGot)
			continue
		}
		if errWant != nil {
			continue
		}
		got, err := jsonv2.Marshal(g)
		if err != nil {
			t.Fatal(err)
		}
		want, err := jsonv2.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		if !equivalent(t, got, want) {
			t.Errorf("%s: decoded to different values:\n json/v2: %s\n odjson:  %s", doc, want, got)
		}
	}
}

// TestV1MethodsKeepV1Semantics checks the other half of the contract: the
// encoding/json methods on the same type still behave like encoding/json, so
// enabling -methods does not change what either library produces.
func TestV1MethodsKeepV1Semantics(t *testing.T) {
	var g gen.Zoo
	if err := jsonv2.Unmarshal([]byte(`{"slice":null,"map":null,"bytes":null,"omit_int":0}`), &g); err != nil {
		t.Fatal(err)
	}
	direct, err := gen.MarshalZoo(&g)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"slice":null`, `"map":null`, `"bytes":null`} {
		if !bytes.Contains(direct, []byte(want)) {
			t.Errorf("encoding/json output is missing %s: %s", want, direct)
		}
	}
	if bytes.Contains(direct, []byte(`"omit_int"`)) {
		t.Errorf("encoding/json should omit a zero int under omitempty: %s", direct)
	}
}

// TestDuplicateNamesRejectedLikeJSONV2 guards the decision to consume unknown
// values with Decoder.ReadValue instead of Decoder.SkipValue: the two must
// agree on duplicate object names, including inside values the target type
// never looks at, because json/v2 rejects those and a drop-in codec has to as
// well.
func TestDuplicateNamesRejectedLikeJSONV2(t *testing.T) {
	for _, doc := range []string{
		`{"unknown":{"a":1,"a":2},"int":1}`,
		`{"unknown":[{"a":1,"a":2}],"int":1}`,
		`{"unknown":{"deep":{"nested":[{"b":1,"b":2}]}}}`,
		`{"unknown":{"a":1,"a":2}}`,
		`{"int":1,"int":2}`,
		`{"nested":{"id":1,"id":2}}`,
		`{"unknown":{"a":1,"b":2},"int":1}`,
	} {
		var g gen.Zoo
		var p plain.Zoo
		errGot := jsonv2.Unmarshal([]byte(doc), &g)
		errWant := jsonv2.Unmarshal([]byte(doc), &p)
		if (errGot != nil) != (errWant != nil) {
			t.Errorf("%s: json/v2=%v odjson=%v", doc, errWant, errGot)
		}
	}
}

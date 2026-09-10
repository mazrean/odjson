package gen_test

import (
	"bytes"
	jsonv1 "encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"fmt"
	"io"
	"reflect"
	"strings"
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
	`{"quoted":"42"}`,
}

// filled is a document that sets every field, so that decoding something else
// into the resulting value shows what the second decode leaves alone.
const filled = `{"bool":true,"int":-7,"uint64":18446744073709551615,"float64":1.5,"string":"héllo","named":"red",` +
	`"slice":[1,2,3],"strings":["a","b"],"bytes":"AAEC","named_bag":"aGk=","array":[1,2,3],"map":{"a":1},"str_map":{"k":"v"},` +
	`"nested":{"id":1,"note":"n"},"nested_ptr":{"id":2},"nesteds":[{"id":3}],"nested_map":{"k":{"id":5}},` +
	`"any":{"a":1},"anys":[1],"raw":{"x":1},"number":"12","time":"2023-11-14T22:13:20Z","ptr":7,"quoted":"42",` +
	`"omit_bool":true,"omit_int":3,"omit_float":1.5,"omit_string":"s","omit_slice":[1],"omit_map":{"a":1},"omit_ptr":4,` +
	`"zero_time":"2024-01-02T03:04:05Z","zero_nested":{"id":9}}`

// sameValue decodes doc into both copies of the type, after optionally filling
// them from filled, and requires json/v2 to see the same value in both.
func sameValue(t *testing.T, doc string, fill bool, decode func([]byte, any) error) {
	t.Helper()
	var g gen.Zoo
	var p plain.Zoo
	if fill {
		if err := jsonv2.Unmarshal([]byte(filled), &g); err != nil {
			t.Fatalf("fill odjson: %v", err)
		}
		if err := jsonv2.Unmarshal([]byte(filled), &p); err != nil {
			t.Fatalf("fill json/v2: %v", err)
		}
	}
	errGot := decode([]byte(doc), &g)
	errWant := decode([]byte(doc), &p)
	if (errGot != nil) != (errWant != nil) {
		t.Errorf("%s: acceptance mismatch: json/v2=%v odjson=%v", doc, errWant, errGot)
		return
	}
	if errWant != nil {
		return
	}
	got, err := jsonv2.Marshal(g, jsontext.AllowInvalidUTF8(true))
	if err != nil {
		t.Fatal(err)
	}
	want, err := jsonv2.Marshal(p, jsontext.AllowInvalidUTF8(true))
	if err != nil {
		t.Fatal(err)
	}
	if !equivalent(t, got, want) {
		t.Errorf("%s: decoded to different values:\n json/v2: %s\n odjson:  %s", doc, want, got)
	}
}

// TestNullZeroesLikeJSONV2 checks the semantics that differ most from
// encoding/json: json/v2 stores the zero value for a null whatever the
// target, where encoding/json leaves scalars untouched. The decode starts from
// a fully populated value so that "untouched" would be visible.
func TestNullZeroesLikeJSONV2(t *testing.T) {
	for _, doc := range []string{
		`{"bool":null,"int":null,"uint64":null,"float64":null,"string":null,"named":null}`,
		`{"slice":null,"strings":null,"bytes":null,"named_bag":null,"array":null,"map":null,"str_map":null}`,
		`{"nested":null,"nested_ptr":null,"nesteds":null,"nested_map":null}`,
		`{"any":null,"anys":null,"raw":null,"number":null,"time":null,"ptr":null,"quoted":null}`,
		`{"nesteds":[null],"nested_map":{"k":null},"anys":[null,[null]],"strings":[null],"slice":[null]}`,
		`null`,
		`{"int":1}`,
	} {
		sameValue(t, doc, true, func(b []byte, v any) error { return jsonv2.Unmarshal(b, v) })
	}
}

// TestLenientOptionsReachTheFallback checks that what the caller allowed stays
// allowed on every path the generated decoder can take. The direct path
// declines a decoder with options, and the path it falls back to must not
// then reject what jsontext, configured by the caller, has already accepted:
// duplicate names under AllowDuplicateNames, invalid UTF-8 under
// AllowInvalidUTF8, and both under encoding/json, whose decoder carries both.
func TestLenientOptionsReachTheFallback(t *testing.T) {
	docs := []string{
		`{"int":1,"int":2}`,
		`{"nested":{"id":1,"id":2}}`,
		`{"unknown":{"a":1,"a":2},"int":3}`,
		`{"map":{"a":1,"a":2}}`,
		`{"any":{"a":1,"a":2}}`,
		`{"nesteds":[{"id":1,"id":2}]}`,
		"{\"string\":\"a\xffb\"}",
		"{\"strings\":[\"\xff\"]}",
		"{\"nested\":{\"note\":\"\xff\"}}",
		"{\"unknown\":\"\xff\",\"int\":4}",
		`{"string":"\ud800"}`,
	}
	for _, doc := range docs {
		sameValue(t, doc, false, func(b []byte, v any) error {
			return jsonv2.Unmarshal(b, v, jsontext.AllowDuplicateNames(true), jsontext.AllowInvalidUTF8(true))
		})
		sameValue(t, doc, false, func(b []byte, v any) error { return jsonv1.Unmarshal(b, v) })
	}
	// Without the options, the same documents are rejected on every path.
	for _, doc := range docs {
		sameValue(t, doc, false, func(b []byte, v any) error { return jsonv2.Unmarshal(b, v) })
		var g gen.Zoo
		if err := jsonv2.Unmarshal([]byte(doc), &g); err == nil {
			t.Errorf("%q accepted without lenient options", doc)
		}
	}
}

// chunkReader hands out its bytes n at a time, so that a streaming decoder's
// buffer boundaries fall in the middle of tokens.
type chunkReader struct {
	b []byte
	n int
}

func (r *chunkReader) Read(p []byte) (int, error) {
	if len(r.b) == 0 {
		return 0, io.EOF
	}
	n := min(r.n, len(p), len(r.b))
	copy(p, r.b[:n])
	r.b = r.b[n:]
	return n, nil
}

// TestStreamingDecodeMatchesJSONV2 drives the generated decoder from an
// io.Reader that delivers a few bytes per call. The generated code peeks at
// the decoder's unread buffer and reads small values whole, and both of those
// have to behave when the buffer is a partial chunk of the document.
func TestStreamingDecodeMatchesJSONV2(t *testing.T) {
	// A document larger than the size below which values are read whole.
	var big strings.Builder
	big.WriteString(`{"int":1,"nesteds":[`)
	for i := range 400 {
		if i > 0 {
			big.WriteString(",")
		}
		fmt.Fprintf(&big, `{"id":%d,"note":"note %d"}`, i, i)
	}
	big.WriteString(`],"strings":["after","the","big","one"],"nested":{"id":9}}`)

	docs := append([]string{big.String(), filled}, documents...)
	for _, n := range []int{1, 7, 64, 4096, 1 << 20} {
		for _, doc := range docs {
			sameValue(t, doc, false, func(b []byte, v any) error {
				return jsonv2.UnmarshalRead(&chunkReader{b: b, n: n}, v)
			})
		}
	}
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
// the generated file does not change what a v1 call site produces.
func TestV1MethodsKeepV1Semantics(t *testing.T) {
	var g gen.Zoo
	if err := jsonv2.Unmarshal([]byte(`{"slice":null,"map":null,"bytes":null,"omit_int":0}`), &g); err != nil {
		t.Fatal(err)
	}
	direct, err := g.MarshalJSON()
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
		// The same name spelled two ways, so one match is raw and the
		// other decoded; and the raw match followed by whitespace.
		`{"int":1,"i\u006et":2}`,
		`{"i\u006et":1,"int":2}`,
		`{"int" : 1, "int":2}`,
		`{"Int":1,"int":2}`,
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

// TestMemberlessMatchesJSONV2 covers the two struct shapes with nothing to
// match: every member a document carries is unknown, so the decoder never
// looks at a name. Documents on both sides of odjsonrt.WholeValue's threshold
// are used, because only the large one is driven token by token.
func TestMemberlessMatchesJSONV2(t *testing.T) {
	var big strings.Builder
	big.WriteString(`{"unknown":[`)
	for i := range 400 {
		if i > 0 {
			big.WriteString(",")
		}
		fmt.Fprintf(&big, `{"id":%d,"note":"note %d"}`, i, i)
	}
	big.WriteString(`],"another":{"a":1}}`)

	docs := []string{
		`{}`,
		`null`,
		`{"skipped":"v"}`,
		`{"a":1,"b":[2,{"c":3}],"d":null}`,
		`{"a":1,"a":2}`,
		`{"a" : 1 }`,
		`[]`,
		`{`,
		big.String(),
	}
	for _, doc := range docs {
		memberlessParity[gen.Memberless, plain.Memberless](t, doc)
		memberlessParity[gen.Unit, plain.Unit](t, doc)
	}
}

// memberlessParity requires the generated methods on G to accept what json/v2
// accepts for its oracle P and to encode to the same bytes, through the whole
// value path, the streaming path and the encoder.
func memberlessParity[G, P any](t *testing.T, doc string) {
	t.Helper()
	decoders := map[string]func([]byte, any) error{
		"Unmarshal": func(b []byte, v any) error { return jsonv2.Unmarshal(b, v) },
		"chunked": func(b []byte, v any) error {
			return jsonv2.UnmarshalRead(&chunkReader{b: b, n: 7}, v)
		},
	}
	for name, decode := range decoders {
		var g G
		var p P
		errGot := decode([]byte(doc), &g)
		errWant := decode([]byte(doc), &p)
		if (errGot != nil) != (errWant != nil) {
			t.Errorf("%T/%s %s: acceptance mismatch: json/v2=%v odjson=%v", g, name, doc, errWant, errGot)
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
		if !bytes.Equal(got, want) {
			t.Errorf("%T/%s %s:\n json/v2: %s\n odjson:  %s", g, name, doc, want, got)
		}
	}
}

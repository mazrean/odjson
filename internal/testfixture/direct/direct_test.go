package direct

import (
	"bytes"
	jsonv1 "encoding/json"
	jsonv2 "encoding/json/v2"
	"reflect"
	"testing"
)

// values covers the shapes where the -direct encoder could drift from the
// standard one: the two v2 semantics (a nil slice, a zero omitempty number),
// the three escaping decisions (HTML runes, U+2028, invalid UTF-8) and a
// pointer that is nil.
func values() []struct {
	name string
	v    Root
} {
	return []struct {
		name string
		v    Root
	}{
		{name: "zero"},
		{
			name: "full",
			v: Root{
				Name:   "hiro",
				Count:  7,
				Tags:   []string{"a", "b"},
				Ratio:  1.5,
				Nested: nested{ID: 1, Meta: map[string]string{"k": "v", "j": "w"}, On: true},
				Ptr:    &nested{ID: 2},
			},
		},
		{
			name: "nil slice and zero omitempty",
			v:    Root{Name: "n", Tags: nil},
		},
		{
			name: "empty slice",
			v:    Root{Tags: []string{}},
		},
		{
			name: "html runes",
			v:    Root{Name: `<a href="x">&</a>`},
		},
		{
			name: "line separators",
			v:    Root{Name: "a b c"},
		},
		{
			name: "invalid utf8",
			v:    Root{Name: "a\xffb"},
		},
		{
			name: "control runes",
			v:    Root{Name: "a\x00\t\nb"},
		},
	}
}

// TestMarshalMatchesEncodingJSON pins the -direct encoder against
// encoding/json's Marshal on the very same value. That call reaches
// MarshalJSONTo, so the two differ only in how they get to odjsonAppend --
// which is the whole claim -direct makes.
func TestMarshalMatchesEncodingJSON(t *testing.T) {
	for _, tc := range values() {
		t.Run(tc.name, func(t *testing.T) {
			v := tc.v
			want, wantErr := jsonv1.Marshal(&v)
			got, gotErr := MarshalRoot(&v)

			if (gotErr != nil) != (wantErr != nil) {
				t.Fatalf("MarshalRoot error = %v, json.Marshal error = %v", gotErr, wantErr)
			}
			if gotErr != nil {
				return
			}
			if !bytes.Equal(got, want) {
				t.Errorf("MarshalRoot = %s\njson.Marshal = %s", got, want)
			}
		})
	}
}

// TestAppendRoot checks the buffer contract: the encoding is appended to
// whatever is already there, and the caller's bytes are left alone.
func TestAppendRoot(t *testing.T) {
	for _, tc := range values() {
		t.Run(tc.name, func(t *testing.T) {
			v := tc.v
			want, err := MarshalRoot(&v)
			if err != nil {
				t.Skipf("MarshalRoot: %v", err)
			}

			prefix := []byte("prefix:")
			got, err := AppendRoot(prefix, &v)
			if err != nil {
				t.Fatalf("AppendRoot: %v", err)
			}
			if !bytes.Equal(got, append([]byte("prefix:"), want...)) {
				t.Errorf("AppendRoot = %s, want prefix:%s", got, want)
			}
			if !bytes.Equal(prefix, []byte("prefix:")) {
				t.Errorf("AppendRoot overwrote the caller's bytes: %s", prefix)
			}
		})
	}
}

// documents are the inputs the -direct decoder must agree with json/v2 on,
// acceptance included: nothing has validated these bytes before it sees them.
var documents = []struct {
	name string
	data string
}{
	{name: "empty object", data: `{}`},
	{name: "null", data: `null`},
	{name: "full", data: `{"name":"hiro","count":7,"tags":["a","b"],"ratio":1.5,"nested":{"id":1,"meta":{"k":"v"},"on":true},"ptr":{"id":2,"meta":null}}`},
	{name: "null members", data: `{"name":null,"tags":null,"ptr":null,"nested":null}`},
	{name: "unknown member", data: `{"name":"x","extra":{"a":[1,2,{"b":null}]}}`},
	{name: "leading and trailing space", data: "  {\"name\":\"x\"}\n"},
	{name: "escapes", data: `{"name":"aA\t\"\\ b"}`},
	{name: "surrogate pair", data: `{"name":"😀"}`},
	{name: "wrong case", data: `{"Name":"x"}`},

	// Everything below this line json/v2 refuses, and so must the -direct
	// decoder: it is the only thing looking at these bytes.
	{name: "duplicate name", data: `{"name":"a","name":"b"}`},
	{name: "duplicate name nested", data: `{"nested":{"id":1,"id":2}}`},
	{name: "duplicate name in unknown member", data: `{"extra":{"a":1,"a":2}}`},
	{name: "invalid utf8", data: "{\"name\":\"a\xffb\"}"},
	{name: "unpaired surrogate", data: `{"name":"\ud800"}`},
	{name: "trailing garbage", data: `{"name":"x"} }`},
	{name: "trailing comma", data: `{"name":"x",}`},
	{name: "truncated", data: `{"name":"x"`},
	{name: "wrong kind", data: `{"count":"seven"}`},
	{name: "array", data: `[1,2]`},
	{name: "empty", data: ``},
}

// TestUnmarshalMatchesJSONV2 pins the -direct decoder against
// encoding/json/v2's Unmarshal, which reaches UnmarshalJSONFrom and, through
// it, the very same odjsonParseV2 with strict set.
func TestUnmarshalMatchesJSONV2(t *testing.T) {
	for _, tc := range documents {
		t.Run(tc.name, func(t *testing.T) {
			var want Root
			wantErr := jsonv2.Unmarshal([]byte(tc.data), &want)

			var got Root
			gotErr := UnmarshalRoot([]byte(tc.data), &got)

			if (gotErr != nil) != (wantErr != nil) {
				t.Fatalf("UnmarshalRoot(%q) error = %v, json/v2 error = %v", tc.data, gotErr, wantErr)
			}
			if gotErr != nil {
				return
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("UnmarshalRoot(%q) = %+v, json/v2 = %+v", tc.data, got, want)
			}
		})
	}
}

// TestRoundTrip checks the two halves of the direct API against each other,
// which the parity tests above never do: they each have one foot in the
// standard library.
func TestRoundTrip(t *testing.T) {
	for _, tc := range values() {
		t.Run(tc.name, func(t *testing.T) {
			v := tc.v
			data, err := MarshalRoot(&v)
			if err != nil {
				t.Skipf("MarshalRoot: %v", err)
			}

			var got Root
			if err := UnmarshalRoot(data, &got); err != nil {
				// The encoder under encoding/json's flags passes
				// invalid UTF-8 through, and the strict decoder
				// refuses it. That asymmetry is the standard
				// library's own, so json/v2 has to refuse these
				// bytes too.
				if jsonv2.Unmarshal(data, new(Root)) == nil {
					t.Fatalf("UnmarshalRoot(%s) = %v, json/v2 accepted it", data, err)
				}
				t.Skipf("neither decoder accepts %s", data)
			}

			// Comparing the re-encoding rather than the value sidesteps
			// the nil-versus-empty question json/v2 settles on the way
			// out: a nil slice or map is encoded as an empty one and
			// comes back empty.
			again, err := MarshalRoot(&got)
			if err != nil {
				t.Fatalf("re-encode: %v", err)
			}
			if !bytes.Equal(again, data) {
				t.Errorf("round trip re-encoded as %s, want %s", again, data)
			}
		})
	}
}

// TestUnexportedTypeGetsUnexportedFunctions is a compile time assertion that
// an unexported struct's direct functions are spelled marshalNested rather
// than MarshalNested, so -direct never widens a package's API beyond what the
// types themselves already expose.
func TestUnexportedTypeGetsUnexportedFunctions(t *testing.T) {
	var (
		_ func(*nested) ([]byte, error)         = marshalNested
		_ func([]byte, *nested) ([]byte, error) = appendNested
		_ func([]byte, *nested) error           = unmarshalNested
	)

	n := nested{ID: 3, Meta: map[string]string{"a": "b"}}
	data, err := marshalNested(&n)
	if err != nil {
		t.Fatalf("marshalNested: %v", err)
	}

	var got nested
	if err := unmarshalNested(data, &got); err != nil {
		t.Fatalf("unmarshalNested: %v", err)
	}
	if !reflect.DeepEqual(got, n) {
		t.Errorf("round trip = %+v, want %+v", got, n)
	}
}

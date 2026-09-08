package suite

import (
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// suitePath is the JSON Test Suite fixture vendored from sonic, shared with
// the odjsonrt scanner tests.
const suitePath = "../../../odjsonrt/testdata/JSONTestSuite/testdata.json.gz"

func load(t *testing.T) map[string]string {
	t.Helper()
	f, err := os.Open(filepath.FromSlash(suitePath))
	if err != nil {
		t.Fatalf("open the test suite: %v", err)
	}
	defer f.Close() //nolint:errcheck

	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gunzip the test suite: %v", err)
	}
	defer gz.Close() //nolint:errcheck

	var cases map[string]string
	if err := json.NewDecoder(gz).Decode(&cases); err != nil {
		t.Fatalf("decode the test suite: %v", err)
	}
	if len(cases) == 0 {
		t.Fatal("the test suite is empty")
	}
	return cases
}

// TestSuiteThroughGeneratedDecoders feeds every case of the JSON Test Suite to
// the generated decoders and requires them to accept exactly what
// encoding/json accepts, and to produce the same value when they accept.
func TestSuiteThroughGeneratedDecoders(t *testing.T) {
	cases := load(t)

	decoders := []struct {
		name string
		run  func([]byte) (any, error)
		ref  func([]byte) (any, error)
	}{
		{
			name: "RawMessage",
			run:  func(b []byte) (any, error) { var v Raw; return &v, UnmarshalRaw(b, &v) },
			ref:  func(b []byte) (any, error) { var v Raw; return &v, json.Unmarshal(b, &v) },
		},
		{
			name: "any",
			run:  func(b []byte) (any, error) { var v Value; return &v, UnmarshalValue(b, &v) },
			ref:  func(b []byte) (any, error) { var v Value; return &v, json.Unmarshal(b, &v) },
		},
		{
			name: "typed",
			run:  func(b []byte) (any, error) { var v Typed; return &v, UnmarshalTyped(b, &v) },
			ref:  func(b []byte) (any, error) { var v Typed; return &v, json.Unmarshal(b, &v) },
		},
	}

	for _, d := range decoders {
		t.Run(d.name, func(t *testing.T) {
			for name, text := range cases {
				// Wrap the case as the value of a single member: the
				// generated decoders consume objects, and the suite's cases
				// are whole documents of every shape.
				wrapped := append(append([]byte(`{"x":`), text...), '}')

				gotV, gotErr := d.run(wrapped)
				wantV, wantErr := d.ref(wrapped)

				if (gotErr != nil) != (wantErr != nil) {
					t.Errorf("%s: acceptance mismatch: encoding/json=%v odjson=%v", name, wantErr, gotErr)
					continue
				}
				if wantErr != nil {
					continue
				}
				if !reflect.DeepEqual(gotV, wantV) {
					t.Errorf("%s: value mismatch:\n encoding/json: %#v\n odjson:        %#v", name, wantV, gotV)
				}
			}
		})
	}
}

// TestSuiteRoundTripsThroughGeneratedEncoder re-encodes every case odjson
// accepted and requires the result to match encoding/json byte for byte.
func TestSuiteRoundTripsThroughGeneratedEncoder(t *testing.T) {
	for name, text := range load(t) {
		wrapped := append(append([]byte(`{"x":`), text...), '}')

		var v Value
		if err := UnmarshalValue(wrapped, &v); err != nil {
			continue
		}
		got, err := AppendValue(nil, &v)
		if err != nil {
			t.Errorf("%s: re-encode: %v", name, err)
			continue
		}
		want, err := json.Marshal(v)
		if err != nil {
			t.Errorf("%s: encoding/json re-encode: %v", name, err)
			continue
		}
		if string(got) != string(want) {
			t.Errorf("%s:\n encoding/json: %s\n odjson:        %s", name, want, got)
		}
	}
}

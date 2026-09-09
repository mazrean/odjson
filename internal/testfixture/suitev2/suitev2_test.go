package suitev2

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/mazrean/odjson/internal/testfixture/suite"
)

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
	return cases
}

// TestSuiteThroughGeneratedV2Decoders runs every case of the JSON Test Suite
// through the generated UnmarshalJSONFrom and requires it to accept exactly
// what encoding/json/v2's reflection decoder accepts, and to produce the same
// value. Package suite holds the same types without generated methods, so it
// is decoded by reflection and serves as the oracle.
func TestSuiteThroughGeneratedV2Decoders(t *testing.T) {
	for name, text := range load(t) {
		wrapped := append(append([]byte(`{"x":`), text...), '}')

		t.Run("raw/"+name, func(t *testing.T) {
			var got Raw
			var want suite.Raw
			errGot := jsonv2.Unmarshal(wrapped, &got)
			errWant := jsonv2.Unmarshal(wrapped, &want)
			if (errGot != nil) != (errWant != nil) {
				t.Fatalf("acceptance mismatch: json/v2=%v odjson=%v", errWant, errGot)
			}
			if errWant == nil && !bytes.Equal(got.X, want.X) {
				t.Errorf("value mismatch:\n json/v2: %s\n odjson:  %s", want.X, got.X)
			}
		})

		t.Run("any/"+name, func(t *testing.T) {
			var got Value
			var want suite.Value
			errGot := jsonv2.Unmarshal(wrapped, &got)
			errWant := jsonv2.Unmarshal(wrapped, &want)
			if (errGot != nil) != (errWant != nil) {
				t.Fatalf("acceptance mismatch: json/v2=%v odjson=%v", errWant, errGot)
			}
			if errWant == nil && !reflect.DeepEqual(got.X, want.X) {
				t.Errorf("value mismatch:\n json/v2: %#v\n odjson:  %#v", want.X, got.X)
			}
		})

		t.Run("typed/"+name, func(t *testing.T) {
			var got Typed
			var want suite.Typed
			errGot := jsonv2.Unmarshal(wrapped, &got)
			errWant := jsonv2.Unmarshal(wrapped, &want)
			if (errGot != nil) != (errWant != nil) {
				t.Fatalf("acceptance mismatch: json/v2=%v odjson=%v", errWant, errGot)
			}
			if errWant != nil {
				return
			}
			// The two types differ only in having generated methods, so the
			// canonical encodings of the decoded values must match.
			gotJSON, err := jsonv2.Marshal(got)
			if err != nil {
				t.Fatalf("re-encode odjson value: %v", err)
			}
			wantJSON, err := jsonv2.Marshal(want)
			if err != nil {
				t.Fatalf("re-encode json/v2 value: %v", err)
			}
			if !bytes.Equal(gotJSON, wantJSON) {
				t.Errorf("value mismatch:\n json/v2: %s\n odjson:  %s", wantJSON, gotJSON)
			}
		})
	}
}

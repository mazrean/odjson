package odjsonrt_test

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/mazrean/odjson/odjsonrt"
)

// knownStdlibDivergences lists JSONTestSuite cases whose verdict the suite
// requires but encoding/json (and therefore odjsonrt, which is specified to
// match it byte for byte) does not honour.
//
// Every entry is a "n_string_*" case that the suite reclassified from
// "either pass or fail" to "must fail" because RFC 8259 section 8.1 requires
// JSON text to be valid UTF-8 (see testdata/JSONTestSuite/README.md).
// Go's encoding/json does not validate UTF-8 while scanning: the scanner only
// rejects unescaped control bytes and malformed escapes, and invalid bytes are
// replaced with U+FFFD when a string is actually decoded. Rejecting these
// inputs would break parity with the standard library, which is the stronger
// requirement for odjson, so they are recorded here instead.
//
// The list is exhaustive and was determined empirically: no "y_*" case is
// rejected by encoding/json, so there is no second category.
var knownStdlibDivergences = map[string]string{
	"n_string_UTF-8_invalid_sequence":         "invalid UTF-8 continuation byte inside a string",
	"n_string_UTF8_surrogate_U+D800":          "UTF-8 encoded surrogate half inside a string",
	"n_string_invalid_utf-8":                  "lone 0xE5 lead byte inside a string",
	"n_string_iso_latin_1":                    "ISO-8859-1 byte inside a string",
	"n_string_lone_utf8_continuation_byte":    "lone 0x81 continuation byte inside a string",
	"n_string_not_in_unicode_range":           "4-byte sequence beyond U+10FFFF inside a string",
	"n_string_overlong_sequence_2_bytes":      "overlong 2-byte encoding inside a string",
	"n_string_overlong_sequence_6_bytes":      "overlong 6-byte encoding inside a string",
	"n_string_overlong_sequence_6_bytes_null": "overlong 6-byte encoding of NUL inside a string",
	"n_string_truncated-utf-8":                "truncated multi-byte sequence inside a string",
}

// loadJSONTestSuite loads the gzipped case map shipped in testdata.
func loadJSONTestSuite(t *testing.T) map[string]string {
	t.Helper()

	f, err := os.Open("testdata/JSONTestSuite/testdata.json.gz")
	if err != nil {
		t.Fatalf("open testdata: %v", err)
	}
	defer f.Close()

	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	defer zr.Close()

	raw, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}

	cases := map[string]string{}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatalf("unmarshal testdata: %v", err)
	}
	if len(cases) == 0 {
		t.Fatal("no test cases loaded")
	}
	return cases
}

// decodeDocument runs the full-document ParseAny path.
func decodeDocument(b []byte) (any, error) {
	p := odjsonrt.SkipSpace(b, 0)
	v, p, err := odjsonrt.ParseAny(b, p)
	if err != nil {
		return nil, err
	}
	if err := odjsonrt.EndOfDocument(b, p); err != nil {
		return nil, err
	}
	return v, nil
}

func TestJSONTestSuite(t *testing.T) {
	cases := loadJSONTestSuite(t)

	names := make([]string, 0, len(cases))
	for name := range cases {
		names = append(names, name)
	}
	sort.Strings(names)

	usedDivergences := map[string]bool{}

	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			b := []byte(cases[name])

			// Primary assertion: parity with encoding/json.
			gotValidate := odjsonrt.Validate(b)
			wantValidate := json.Unmarshal(b, new(json.RawMessage))
			if (gotValidate != nil) != (wantValidate != nil) {
				t.Fatalf("Validate parity: odjsonrt=%v encoding/json=%v for %q",
					gotValidate, wantValidate, cases[name])
			}

			gotValue, gotErr := decodeDocument(b)
			var wantValue any
			wantErr := json.Unmarshal(b, &wantValue)
			if (gotErr != nil) != (wantErr != nil) {
				t.Fatalf("ParseAny parity: odjsonrt=%v encoding/json=%v for %q",
					gotErr, wantErr, cases[name])
			}
			if gotErr == nil && !reflect.DeepEqual(gotValue, wantValue) {
				t.Fatalf("ParseAny value mismatch:\n got %#v\nwant %#v\nfor %q",
					gotValue, wantValue, cases[name])
			}

			// Re-encoding parity: every document encoding/json accepts must
			// compact through AppendRaw exactly like encoding/json does.
			if wantValidate == nil {
				for _, escapeHTML := range []bool{true, false} {
					gotRaw, err := odjsonrt.AppendRaw(nil, b, escapeHTML)
					if err != nil {
						t.Fatalf("AppendRaw(escapeHTML=%v): %v", escapeHTML, err)
					}
					var wantRaw []byte
					if escapeHTML {
						wantRaw, err = json.Marshal(json.RawMessage(b))
					} else {
						var buf bytes.Buffer
						enc := json.NewEncoder(&buf)
						enc.SetEscapeHTML(false)
						err = enc.Encode(json.RawMessage(b))
						wantRaw = bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
					}
					if err != nil {
						t.Fatalf("oracle AppendRaw(escapeHTML=%v): %v", escapeHTML, err)
					}
					if !bytes.Equal(gotRaw, wantRaw) {
						t.Fatalf("AppendRaw(escapeHTML=%v) = %s, want %s", escapeHTML, gotRaw, wantRaw)
					}
				}
			}

			// Round-trip parity: re-encoding the decoded value through
			// AppendAny must match encoding/json.
			if gotErr == nil {
				for _, escapeHTML := range []bool{true, false} {
					gotEnc, err := odjsonrt.AppendAny(nil, gotValue, escapeHTML)
					if err != nil {
						t.Fatalf("AppendAny(escapeHTML=%v): %v", escapeHTML, err)
					}
					var wantEnc []byte
					if escapeHTML {
						wantEnc, err = json.Marshal(wantValue)
					} else {
						var buf bytes.Buffer
						enc := json.NewEncoder(&buf)
						enc.SetEscapeHTML(false)
						err = enc.Encode(wantValue)
						wantEnc = bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
					}
					if err != nil {
						t.Fatalf("oracle AppendAny(escapeHTML=%v): %v", escapeHTML, err)
					}
					if !bytes.Equal(gotEnc, wantEnc) {
						t.Fatalf("AppendAny(escapeHTML=%v) = %s, want %s", escapeHTML, gotEnc, wantEnc)
					}
				}
			}

			// Secondary assertion: the suite's own verdicts.
			reason, diverges := knownStdlibDivergences[name]
			switch {
			case strings.HasPrefix(name, "y_"):
				if diverges {
					usedDivergences[name] = true
					t.Skipf("known divergence: %s", reason)
				}
				if gotValidate != nil {
					t.Fatalf("must accept, got %v", gotValidate)
				}
			case strings.HasPrefix(name, "n_"):
				if diverges {
					usedDivergences[name] = true
					if gotValidate == nil {
						return // expected divergence: accepted anyway
					}
					t.Fatalf("case %q no longer diverges from the suite verdict; "+
						"remove it from knownStdlibDivergences", name)
				}
				if gotValidate == nil {
					t.Fatal("must reject, but the document validated")
				}
			case strings.HasPrefix(name, "i_"):
				// Implementation defined: parity with encoding/json is enough.
			default:
				t.Fatalf("unexpected case name prefix: %q", name)
			}
		})
	}

	for name := range knownStdlibDivergences {
		if !usedDivergences[name] {
			t.Errorf("knownStdlibDivergences contains %q, which is not in the suite", name)
		}
	}
}

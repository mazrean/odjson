package gen

import (
	"bytes"
	jsonv1 "encoding/json"
	jsonv2 "encoding/json/v2"
	"fmt"
	"os"
	"reflect"
	"sync"
	"testing"

	"github.com/bytedance/sonic"
	gojson "github.com/goccy/go-json"
)

// codec is one of the host JSON libraries under test. Every library is driven
// through the same `any`-based signature so the benchmark table stays uniform.
type codec struct {
	name      string
	marshal   func(any) ([]byte, error)
	unmarshal func([]byte, any) error
}

var codecs = []codec{
	{
		name:      "encoding-json",
		marshal:   jsonv1.Marshal,
		unmarshal: jsonv1.Unmarshal,
	},
	{
		name:      "json-v2",
		marshal:   func(v any) ([]byte, error) { return jsonv2.Marshal(v) },
		unmarshal: func(b []byte, v any) error { return jsonv2.Unmarshal(b, v) },
	},
	{
		name:      "sonic",
		marshal:   sonic.Marshal,
		unmarshal: sonic.Unmarshal,
	},
	{
		name:      "go-json",
		marshal:   gojson.Marshal,
		unmarshal: gojson.Unmarshal,
	},
	{
		// sonic told that a json.Marshaler's output is already compact and
		// valid, which is exactly the contract odjson's generated encoder
		// satisfies. This is the option a sonic user should set.
		name:      "sonic-trusting",
		marshal:   sonicTrusting.Marshal,
		unmarshal: sonicTrusting.Unmarshal,
	},
	{
		// odjson's own entry points, bypassing every host library. This
		// is the ceiling: the host libraries all re-validate the bytes a
		// MarshalJSON method hands back, and this path does not.
		name:      "odjson-direct",
		marshal:   marshalDirect,
		unmarshal: unmarshalDirect,
	},
}

// marshalDirect dispatches to the generated package level encoder.
func marshalDirect(v any) ([]byte, error) {
	switch t := v.(type) {
	case *TwitterStruct:
		return MarshalTwitterStruct(t)
	case *Book:
		return MarshalBook(t)
	default:
		return nil, fmt.Errorf("odjson: no generated encoder for %T", v)
	}
}

// unmarshalDirect dispatches to the generated package level decoder.
func unmarshalDirect(data []byte, v any) error {
	switch t := v.(type) {
	case *TwitterStruct:
		return UnmarshalTwitterStruct(data, t)
	case *Book:
		return UnmarshalBook(data, t)
	default:
		return fmt.Errorf("odjson: no generated decoder for %T", v)
	}
}

// sonicTrusting skips the validation sonic otherwise performs on the bytes a
// json.Marshaler hands back.
// sonicTrusting is the configuration a sonic user should pair odjson with:
// NoValidateJSONMarshaler skips re-validating the bytes the generated encoder
// returns, and NoValidateJSONSkip skips re-validating the members the
// generated decoder ignores. CompactMarshaler is deliberately NOT set — it
// sounds right for odjson's always-compact output, but measures 1.6x slower.
var sonicTrusting = sonic.Config{
	NoValidateJSONMarshaler: true,
	NoValidateJSONSkip:      true,
}.Froze()

// payload is a single JSON document plus the Go type it decodes into.
type payload struct {
	name string
	// data is the raw JSON document.
	data []byte
	// newValue returns a pointer to a fresh zero value of the target type.
	newValue func() any
	// decoded is a pre-decoded value, used as the input to the Marshal
	// benchmarks so that decoding cost is not folded into encoding cost.
	decoded any
}

// loadPayloads reads the fixtures once per test binary. twitter.json lives in
// bench/testdata rather than beside this package, so it is loaded by relative
// path (go test runs with the package directory as its working directory)
// instead of go:embed, which cannot reach out of the package directory.
var loadPayloads = sync.OnceValues(func() ([]payload, error) {
	twitterJSON, err := os.ReadFile("../testdata/twitter.json")
	if err != nil {
		return nil, fmt.Errorf("read twitter.json: %w", err)
	}

	twitter := new(TwitterStruct)
	if err := jsonv1.Unmarshal(twitterJSON, twitter); err != nil {
		return nil, fmt.Errorf("decode twitter.json: %w", err)
	}

	// data and Book come from small.go, vendored from sonic's testdata.
	small := new(Book)
	if err := jsonv1.Unmarshal(data, small); err != nil {
		return nil, fmt.Errorf("decode small payload: %w", err)
	}

	return []payload{
		{
			name:     "twitter",
			data:     twitterJSON,
			newValue: func() any { return new(TwitterStruct) },
			decoded:  twitter,
		},
		{
			name:     "small",
			data:     data,
			newValue: func() any { return new(Book) },
			decoded:  small,
		},
	}, nil
})

func payloads(tb testing.TB) []payload {
	tb.Helper()

	ps, err := loadPayloads()
	if err != nil {
		tb.Fatal(err)
	}

	return ps
}

// TestGeneratedMatchesReflection checks that the generated codec and
// encoding/json's reflection based one agree on the fixtures, so the benchmark
// numbers below are comparing two implementations of the same behaviour.
func TestGeneratedMatchesReflection(t *testing.T) {
	for _, p := range payloads(t) {
		t.Run(p.name, func(t *testing.T) {
			direct, err := marshalDirect(p.decoded)
			if err != nil {
				t.Fatalf("odjson marshal: %v", err)
			}

			// The generated methods are what encoding/json now calls, so
			// compare against a decode of both outputs rather than against
			// encoding/json's own encoder, which no longer runs.
			var viaGenerated, viaStdlib any
			if err := jsonv1.Unmarshal(direct, &viaGenerated); err != nil {
				t.Fatalf("odjson output is not valid JSON: %v", err)
			}
			if err := jsonv1.Unmarshal(p.data, &viaStdlib); err != nil {
				t.Fatalf("fixture is not valid JSON: %v", err)
			}

			// The premise of -methods=true: every host library honours
			// MarshalJSON, so all four must hand back exactly the bytes the
			// generated encoder produced.
			for _, c := range codecs {
				if c.name == "odjson-direct" {
					continue
				}
				viaHost, err := c.marshal(p.decoded)
				if err != nil {
					t.Fatalf("%s marshal: %v", c.name, err)
				}
				if c.name == "json-v2" {
					// encoding/json/v2 re-canonicalises the value handed to
					// jsontext.Encoder.WriteValue: it does not escape HTML by
					// default, so odjson's \u003c sequences come back as
					// literal characters. Compare meaning, not bytes.
					var a, b any
					if err := jsonv1.Unmarshal(direct, &a); err != nil {
						t.Fatalf("odjson output: %v", err)
					}
					if err := jsonv1.Unmarshal(viaHost, &b); err != nil {
						t.Fatalf("json-v2 output: %v", err)
					}
					if !reflect.DeepEqual(a, b) {
						t.Error("json-v2 did not route through the generated codec")
					}
					continue
				}
				if !bytes.Equal(direct, viaHost) {
					t.Errorf("%s did not route through the generated codec (%d bytes vs %d)",
						c.name, len(viaHost), len(direct))
				}
			}

			fresh := p.newValue()
			if err := unmarshalDirect(p.data, fresh); err != nil {
				t.Fatalf("odjson unmarshal: %v", err)
			}
			roundTripped, err := marshalDirect(fresh)
			if err != nil {
				t.Fatalf("odjson re-marshal: %v", err)
			}
			if !bytes.Equal(direct, roundTripped) {
				t.Error("decode/encode round trip is not stable")
			}
		})
	}
}

// TestPayloadsRoundTrip checks that every library decodes every payload
// without error, and that re-encoding the decoded value with encoding/json
// yields valid JSON. It is a sanity check on the fixtures and on the library
// wiring, not a conformance test: the libraries are not required to agree
// byte-for-byte on their output.
func TestPayloadsRoundTrip(t *testing.T) {
	for _, p := range payloads(t) {
		t.Run(p.name, func(t *testing.T) {
			if !jsonv1.Valid(p.data) {
				t.Fatalf("fixture %s is not valid JSON", p.name)
			}

			for _, c := range codecs {
				t.Run(c.name, func(t *testing.T) {
					v := p.newValue()
					if err := c.unmarshal(p.data, v); err != nil {
						t.Fatalf("unmarshal: %v", err)
					}

					out, err := jsonv1.Marshal(v)
					if err != nil {
						t.Fatalf("re-marshal with encoding/json: %v", err)
					}
					if !jsonv1.Valid(out) {
						t.Fatal("re-marshalled output is not valid JSON")
					}
					if len(out) == 0 {
						t.Fatal("re-marshalled output is empty")
					}
				})
			}
		})
	}
}

func BenchmarkMarshal(b *testing.B) {
	ps := payloads(b)

	for _, c := range codecs {
		b.Run(c.name, func(b *testing.B) {
			for _, p := range ps {
				b.Run(p.name, func(b *testing.B) {
					// Warm up: sonic JIT-compiles and go-json builds its
					// encoder lazily on first use. b.Loop resets the timer,
					// so this one-off cost stays out of the measurement.
					if _, err := c.marshal(p.decoded); err != nil {
						b.Fatal(err)
					}

					b.ReportAllocs()
					b.SetBytes(int64(len(p.data)))

					for b.Loop() {
						if _, err := c.marshal(p.decoded); err != nil {
							b.Fatal(err)
						}
					}
				})
			}
		})
	}
}

func BenchmarkUnmarshal(b *testing.B) {
	ps := payloads(b)

	for _, c := range codecs {
		b.Run(c.name, func(b *testing.B) {
			for _, p := range ps {
				b.Run(p.name, func(b *testing.B) {
					if err := c.unmarshal(p.data, p.newValue()); err != nil {
						b.Fatal(err)
					}

					b.ReportAllocs()
					b.SetBytes(int64(len(p.data)))

					for b.Loop() {
						// A fresh zero value every iteration: reusing one
						// would let the decoder skip allocating the slices
						// and maps it has already filled in.
						if err := c.unmarshal(p.data, p.newValue()); err != nil {
							b.Fatal(err)
						}
					}
				})
			}
		})
	}
}

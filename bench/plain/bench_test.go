package plain

import (
	jsonv1 "encoding/json"
	jsonv2 "encoding/json/v2"
	"fmt"
	"os"
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
		// sonic's default configuration neither escapes HTML nor validates
		// UTF-8, so it is not producing encoding/json's bytes. ConfigStd does
		// both, which is the semantics odjson's generated codec implements;
		// this row makes the comparison like for like.
		name:      "sonic-std",
		marshal:   sonic.ConfigStd.Marshal,
		unmarshal: sonic.ConfigStd.Unmarshal,
	},
}

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

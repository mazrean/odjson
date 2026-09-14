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
	jsoniter "github.com/json-iterator/go"
	simdjson "github.com/minio/simdjson-go"
	segmentio "github.com/segmentio/encoding/json"
	"github.com/sugawarayuuta/sonnet"
	"github.com/wI2L/jettison"
)

// codec is one of the host JSON libraries under test. Every library is driven
// through the same `any`-based signature so the benchmark table stays uniform.
// A library that only does one direction leaves the other side nil, and the
// tests and benchmarks skip that side rather than fail on it.
type codec struct {
	name      string
	marshal   func(any) ([]byte, error)
	unmarshal func([]byte, any) error
	// skip, when set and returning a reason, says why this codec cannot run
	// on this machine.
	skip func() string
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
	{
		// ConfigCompatibleWithStandardLibrary rather than ConfigDefault: the
		// default neither sorts map keys nor escapes HTML, and the row is
		// about the drop-in replacement people reach for.
		name:      "json-iterator",
		marshal:   jsoniter.ConfigCompatibleWithStandardLibrary.Marshal,
		unmarshal: jsoniter.ConfigCompatibleWithStandardLibrary.Unmarshal,
	},
	{
		name:      "segmentio",
		marshal:   segmentio.Marshal,
		unmarshal: segmentio.Unmarshal,
	},
	{
		// jettison is an encoder only; it has no Unmarshal.
		name:    "jettison",
		marshal: jettison.Marshal,
	},
	{
		// A drop-in for encoding/json written without unsafe. It has no
		// tagged release; the row is its latest commit (2023-10). Measured
		// but kept out of the chart on purpose: it lands mid-pack among the
		// reflection libraries and would add a row without adding a point.
		name:      "sonnet",
		marshal:   sonnet.Marshal,
		unmarshal: sonnet.Unmarshal,
	},
	{
		// simdjson-go is a parser only, and it parses into a tape rather
		// than into a struct. The row walks that tape into the target type
		// (see simdjson.go), so it does the same job as every other
		// Unmarshal row; the parse alone would be a different measurement.
		name:      "simdjson-go",
		unmarshal: simdjsonUnmarshal,
		skip: func() string {
			if !simdjson.SupportedCPU() {
				return "simdjson-go needs AVX2 and CLMUL"
			}
			return ""
		},
	},
}

// skipUnsupported skips the test or benchmark when the codec cannot run here.
func (c codec) skipUnsupported(tb testing.TB) {
	tb.Helper()
	if c.skip != nil {
		if reason := c.skip(); reason != "" {
			tb.Skip(reason)
		}
	}
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

// loadPayloads reads the fixtures once per test binary. twitter.json and
// medium.json live in bench/testdata rather than beside this package, so they
// are loaded by relative path (go test runs with the package directory as its working directory)
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

	// medium.json is sonic's "Medium" input: the same shape as twitter.json,
	// at four statuses instead of a hundred.
	mediumJSON, err := os.ReadFile("../testdata/medium.json")
	if err != nil {
		return nil, fmt.Errorf("read medium.json: %w", err)
	}

	medium := new(TwitterStruct)
	if err := jsonv1.Unmarshal(mediumJSON, medium); err != nil {
		return nil, fmt.Errorf("decode medium.json: %w", err)
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
			name:     "medium",
			data:     mediumJSON,
			newValue: func() any { return new(TwitterStruct) },
			decoded:  medium,
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
// byte-for-byte on their output. An encode-only library is checked the other
// way round: its output must be valid JSON that encoding/json reads back.
func TestPayloadsRoundTrip(t *testing.T) {
	for _, p := range payloads(t) {
		t.Run(p.name, func(t *testing.T) {
			if !jsonv1.Valid(p.data) {
				t.Fatalf("fixture %s is not valid JSON", p.name)
			}

			for _, c := range codecs {
				t.Run(c.name, func(t *testing.T) {
					c.skipUnsupported(t)
					if c.unmarshal == nil {
						out, err := c.marshal(p.decoded)
						if err != nil {
							t.Fatalf("marshal: %v", err)
						}
						if err := jsonv1.Unmarshal(out, p.newValue()); err != nil {
							t.Fatalf("encoding/json cannot read the output back: %v", err)
						}
						return
					}

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
		if c.marshal == nil {
			continue
		}
		b.Run(c.name, func(b *testing.B) {
			c.skipUnsupported(b)
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
		if c.unmarshal == nil {
			continue
		}
		b.Run(c.name, func(b *testing.B) {
			c.skipUnsupported(b)
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

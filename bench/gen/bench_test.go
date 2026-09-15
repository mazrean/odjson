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
	jsoniter "github.com/json-iterator/go"
	segmentio "github.com/segmentio/encoding/json"
	"github.com/sugawarayuuta/sonnet"
	"github.com/wI2L/jettison"
)

// codec is one of the host JSON libraries under test. Every library is driven
// through the same `any`-based signature so the benchmark table stays uniform.
// A library that only does one direction leaves the other side nil, and the
// tests and benchmarks skip that side rather than fail on it.
//
// Every library here honours json.Marshaler / json.Unmarshaler, so every row
// reaches the generated codec; TestGeneratedMatchesReflection checks that it
// does. easyjson, gojay and simdjson-go do not — they have entry points of
// their own — so they have no row here and are measured in their own
// packages instead.
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
		// sonic configured to produce encoding/json's bytes, which is what
		// odjson's generated codec produces. sonic's default configuration
		// neither escapes HTML nor validates UTF-8, so the default row is not
		// comparing like with like.
		name:      "sonic-std",
		marshal:   sonic.ConfigStd.Marshal,
		unmarshal: sonic.ConfigStd.Unmarshal,
	},
	{
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
		name:      "sonnet",
		marshal:   sonnet.Marshal,
		unmarshal: sonnet.Unmarshal,
	},
}

// marshalV1 calls the generated MarshalJSON directly, which is the exact byte
// stream sonic and go-json receive from it.
func marshalV1(v any) ([]byte, error) {
	m, ok := v.(jsonv1.Marshaler)
	if !ok {
		return nil, fmt.Errorf("odjson: %T has no generated MarshalJSON", v)
	}
	return m.MarshalJSON()
}

// unmarshalV1 calls the generated UnmarshalJSON directly.
func unmarshalV1(data []byte, v any) error {
	u, ok := v.(jsonv1.Unmarshaler)
	if !ok {
		return fmt.Errorf("odjson: %T has no generated UnmarshalJSON", v)
	}
	return u.UnmarshalJSON(data)
}

// sonicTrusting skips the validation sonic otherwise performs on the bytes a
// json.Marshaler hands back.
// sonicTrusting is the configuration a sonic user should pair odjson with:
// NoValidateJSONMarshaler skips re-validating the bytes the generated encoder
// returns, and NoValidateJSONSkip skips re-validating the members the
// generated decoder ignores. CompactMarshaler is deliberately NOT set — it
// sounds right for odjson's always-compact output, but measures 1.7x slower.
var sonicTrusting = sonic.Config{
	NoValidateJSONMarshaler: true,
	NoValidateJSONSkip:      true,
}.Froze()

// unmarshalDirectTwitter and unmarshalDirectBook are the decode half of the
// -direct API, wrapped so a payload can name one. They allocate a fresh value
// per call, as the `any` based entries do: reusing one would let the decoder
// skip allocating the slices it has already filled in.
func unmarshalDirectTwitter(b []byte) (any, error) {
	v := new(TwitterStruct)
	return v, UnmarshalTwitterStruct(b, v)
}

func unmarshalDirectBook(b []byte) (any, error) {
	v := new(Book)
	return v, UnmarshalBook(b, v)
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
	// marshalDirect and unmarshalDirect call the package level functions
	// -direct generates for this payload's type. Those signatures are
	// typed, so unlike the libraries above they cannot share one `any`
	// based table entry; the closure is per payload instead.
	// unmarshalDirect allocates the value it decodes into, the way the
	// `any` based entries do through newValue, and hands it back so the
	// parity test can look at what it decoded.
	marshalDirect   func() ([]byte, error)
	unmarshalDirect func([]byte) (any, error)
	// appendDirect is the other half of the -direct encoder: it writes into
	// a buffer the caller owns, which is the only row in this file that
	// does not allocate its result. See BenchmarkAppendDirect.
	appendDirect func([]byte) ([]byte, error)
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
			name:            "twitter",
			data:            twitterJSON,
			newValue:        func() any { return new(TwitterStruct) },
			decoded:         twitter,
			marshalDirect:   func() ([]byte, error) { return MarshalTwitterStruct(twitter) },
			unmarshalDirect: unmarshalDirectTwitter,
			appendDirect:    func(dst []byte) ([]byte, error) { return AppendTwitterStruct(dst, twitter) },
		},
		{
			name:            "medium",
			data:            mediumJSON,
			newValue:        func() any { return new(TwitterStruct) },
			decoded:         medium,
			marshalDirect:   func() ([]byte, error) { return MarshalTwitterStruct(medium) },
			unmarshalDirect: unmarshalDirectTwitter,
			appendDirect:    func(dst []byte) ([]byte, error) { return AppendTwitterStruct(dst, medium) },
		},
		{
			name:            "small",
			data:            data,
			newValue:        func() any { return new(Book) },
			decoded:         small,
			marshalDirect:   func() ([]byte, error) { return MarshalBook(small) },
			unmarshalDirect: unmarshalDirectBook,
			appendDirect:    func(dst []byte) ([]byte, error) { return AppendBook(dst, small) },
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
			direct, err := marshalV1(p.decoded)
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

			// odjson's premise: every host library honours the generated
			// codec. sonic, go-json, json-iterator, segmentio, jettison and
			// sonnet call MarshalJSON, so their bytes must equal that
			// method's exactly. encoding/json and encoding/json/v2
			// are both json/v2 on this toolchain and call MarshalJSONTo, which
			// deliberately follows json/v2's own semantics (nil slices encode
			// as [], not null), so for those the check is that the value
			// survives a round trip through the generated pair.
			for _, c := range codecs {
				switch c.name {
				case "encoding-json", "json-v2":
					viaHost, err := c.marshal(p.decoded)
					if err != nil {
						t.Fatalf("%s marshal: %v", c.name, err)
					}
					again := p.newValue()
					if err := c.unmarshal(viaHost, again); err != nil {
						t.Fatalf("%s re-decode: %v", c.name, err)
					}
					stable, err := c.marshal(again)
					if err != nil {
						t.Fatalf("%s re-encode: %v", c.name, err)
					}
					// Compare as values, not bytes: json/v2 writes object
					// members in map iteration order, so neither its own
					// output nor odjson's ModeStream output is byte stable
					// for a document containing maps.
					var a, b2 any
					if err := jsonv1.Unmarshal(viaHost, &a); err != nil {
						t.Fatalf("%s output: %v", c.name, err)
					}
					if err := jsonv1.Unmarshal(stable, &b2); err != nil {
						t.Fatalf("%s output: %v", c.name, err)
					}
					if !reflect.DeepEqual(a, b2) {
						t.Errorf("%s is not stable across the generated pair", c.name)
					}
				default:
					viaHost, err := c.marshal(p.decoded)
					if err != nil {
						t.Fatalf("%s marshal: %v", c.name, err)
					}
					if !bytes.Equal(direct, viaHost) {
						t.Errorf("%s did not route through the generated codec (%d bytes vs %d)",
							c.name, len(viaHost), len(direct))
					}
				}
			}

			// The -direct functions follow json/v2's semantics with
			// encoding/json's escaping, which is what an encoding/json
			// Marshal makes of MarshalJSONTo. If that ever stops holding,
			// the odjson-direct rows below are measuring a different
			// encoder from the rest of the table.
			//
			// The comparison is by value rather than by byte: twitter.json
			// carries []interface{} members, so both outputs write the
			// map[string]any they decoded into in iteration order and
			// neither is byte stable. Byte equality against encoding/json
			// is pinned on a map free type in
			// internal/testfixture/direct instead.
			if p.marshalDirect != nil {
				viaDirect, err := p.marshalDirect()
				if err != nil {
					t.Fatalf("odjson -direct marshal: %v", err)
				}
				viaLibrary, err := jsonv1.Marshal(p.decoded)
				if err != nil {
					t.Fatalf("encoding/json marshal: %v", err)
				}
				var asDirect, asLibrary any
				if err := jsonv1.Unmarshal(viaDirect, &asDirect); err != nil {
					t.Fatalf("-direct output is not valid JSON: %v", err)
				}
				if err := jsonv1.Unmarshal(viaLibrary, &asLibrary); err != nil {
					t.Fatalf("encoding/json output: %v", err)
				}
				if !reflect.DeepEqual(asDirect, asLibrary) {
					t.Error("-direct encoded a different document from encoding/json's Marshal")
				}

				// The decode half is pinned the same way: json/v2's
				// Unmarshal reaches UnmarshalJSONFrom and, through it, the
				// very parser UnmarshalT calls.
				decodedDirect, err := p.unmarshalDirect(p.data)
				if err != nil {
					t.Fatalf("odjson -direct unmarshal: %v", err)
				}
				viaV2 := p.newValue()
				if err := jsonv2.Unmarshal(p.data, viaV2); err != nil {
					t.Fatalf("json/v2 unmarshal: %v", err)
				}
				if !reflect.DeepEqual(decodedDirect, viaV2) {
					t.Error("-direct decoded a different value from json/v2's Unmarshal")
				}
			}

			fresh := p.newValue()
			if err := unmarshalV1(p.data, fresh); err != nil {
				t.Fatalf("odjson unmarshal: %v", err)
			}
			roundTripped, err := marshalV1(fresh)
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

	// odjson-direct is the -direct API: the same generated encoder with
	// encoding/json taken out of the call. It is not a codecs entry because
	// its signature is typed, but the benchmark name keeps the
	// <codec>/<payload> shape the rest of the table has.
	b.Run("odjson-direct", func(b *testing.B) {
		for _, p := range ps {
			if p.marshalDirect == nil {
				continue
			}
			b.Run(p.name, func(b *testing.B) {
				if _, err := p.marshalDirect(); err != nil {
					b.Fatal(err)
				}

				b.ReportAllocs()
				b.SetBytes(int64(len(p.data)))

				for b.Loop() {
					if _, err := p.marshalDirect(); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	})
}

// BenchmarkAppendDirect measures AppendT into a buffer the caller keeps.
//
// It is deliberately not a row in BenchmarkMarshal: every row there allocates
// the []byte it returns, and this one does not, so putting them in one table
// would invite a comparison of two different jobs. What it isolates is the
// encoder itself, with both the entry point and the result allocation out of
// the way -- which on a large document is where MarshalT's time actually
// goes, since a returned buffer has to be sized by odjsonrt.SizeHint and that
// hint carries an eighth of headroom.
func BenchmarkAppendDirect(b *testing.B) {
	for _, p := range payloads(b) {
		if p.appendDirect == nil {
			continue
		}
		b.Run(p.name, func(b *testing.B) {
			buf, err := p.appendDirect(nil)
			if err != nil {
				b.Fatal(err)
			}
			buf = buf[:0]

			b.ReportAllocs()
			b.SetBytes(int64(len(p.data)))

			for b.Loop() {
				buf, err = p.appendDirect(buf[:0])
				if err != nil {
					b.Fatal(err)
				}
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

	b.Run("odjson-direct", func(b *testing.B) {
		for _, p := range ps {
			if p.unmarshalDirect == nil {
				continue
			}
			b.Run(p.name, func(b *testing.B) {
				if _, err := p.unmarshalDirect(p.data); err != nil {
					b.Fatal(err)
				}

				b.ReportAllocs()
				b.SetBytes(int64(len(p.data)))

				for b.Loop() {
					if _, err := p.unmarshalDirect(p.data); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	})
}

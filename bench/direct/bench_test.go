package direct

import (
	"bytes"
	jsonv1 "encoding/json"
	jsonv2 "encoding/json/v2"
	"fmt"
	"os"
	"reflect"
	"sync"
	"testing"
)

// payload is a JSON document, the value it decodes into, and the two ways of
// encoding and decoding that value: through encoding/json/v2, which reaches
// the generated methods, and through the package level functions -direct
// generated for the same type.
type payload struct {
	name    string
	data    []byte
	decoded any
	// hasMaps says the encoding is not byte stable across calls, because
	// the type carries an interface{} member whose map[string]any the
	// encoder writes in iteration order. Only the parity test cares.
	hasMaps bool

	newValue        func() any
	marshalDirect   func() ([]byte, error)
	appendDirect    func([]byte) ([]byte, error)
	unmarshalDirect func([]byte) (any, error)
}

func unmarshalDirectTwitter(b []byte) (any, error) {
	v := new(TwitterStruct)
	return v, UnmarshalTwitterStruct(b, v)
}

func unmarshalDirectBook(b []byte) (any, error) {
	v := new(Book)
	return v, UnmarshalBook(b, v)
}

var loadPayloads = sync.OnceValues(func() ([]payload, error) {
	twitterJSON, err := os.ReadFile("../testdata/twitter.json")
	if err != nil {
		return nil, fmt.Errorf("read twitter.json: %w", err)
	}
	twitter := new(TwitterStruct)
	if err := jsonv1.Unmarshal(twitterJSON, twitter); err != nil {
		return nil, fmt.Errorf("decode twitter.json: %w", err)
	}

	mediumJSON, err := os.ReadFile("../testdata/medium.json")
	if err != nil {
		return nil, fmt.Errorf("read medium.json: %w", err)
	}
	medium := new(TwitterStruct)
	if err := jsonv1.Unmarshal(mediumJSON, medium); err != nil {
		return nil, fmt.Errorf("decode medium.json: %w", err)
	}

	small := new(Book)
	if err := jsonv1.Unmarshal(data, small); err != nil {
		return nil, fmt.Errorf("decode small payload: %w", err)
	}

	return []payload{
		{
			name:            "twitter",
			data:            twitterJSON,
			decoded:         twitter,
			hasMaps:         true,
			newValue:        func() any { return new(TwitterStruct) },
			marshalDirect:   func() ([]byte, error) { return MarshalTwitterStruct(twitter) },
			appendDirect:    func(dst []byte) ([]byte, error) { return AppendTwitterStruct(dst, twitter) },
			unmarshalDirect: unmarshalDirectTwitter,
		},
		{
			name:            "medium",
			data:            mediumJSON,
			decoded:         medium,
			hasMaps:         true,
			newValue:        func() any { return new(TwitterStruct) },
			marshalDirect:   func() ([]byte, error) { return MarshalTwitterStruct(medium) },
			appendDirect:    func(dst []byte) ([]byte, error) { return AppendTwitterStruct(dst, medium) },
			unmarshalDirect: unmarshalDirectTwitter,
		},
		{
			name:            "small",
			data:            data,
			decoded:         small,
			newValue:        func() any { return new(Book) },
			marshalDirect:   func() ([]byte, error) { return MarshalBook(small) },
			appendDirect:    func(dst []byte) ([]byte, error) { return AppendBook(dst, small) },
			unmarshalDirect: unmarshalDirectBook,
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

// TestDirectMatchesJSONV2 is the reason this package exists. Unless the two
// sides write the same bytes, the benchmarks below are comparing two
// different jobs and their ratio means nothing.
//
// Book is byte comparable. TwitterStruct is not: it carries interface{}
// members, and the map[string]any behind them is written in iteration order,
// so two calls disagree with themselves. For those the check is the encoded
// length plus the decoded value -- length is what would move if one side
// escaped and the other did not, by thousands of bytes on a document this
// full of markup.
func TestDirectMatchesJSONV2(t *testing.T) {
	for _, p := range payloads(t) {
		t.Run(p.name, func(t *testing.T) {
			viaDirect, err := p.marshalDirect()
			if err != nil {
				t.Fatalf("-direct marshal: %v", err)
			}
			viaV2, err := jsonv2.Marshal(p.decoded)
			if err != nil {
				t.Fatalf("json/v2 marshal: %v", err)
			}

			if !p.hasMaps {
				if !bytes.Equal(viaDirect, viaV2) {
					t.Fatalf("-direct wrote different bytes from json/v2\n-direct: %s\njson/v2: %s",
						viaDirect, viaV2)
				}
				return
			}

			if len(viaDirect) != len(viaV2) {
				t.Errorf("-direct wrote %d bytes, json/v2 %d: the two are not escaping alike",
					len(viaDirect), len(viaV2))
			}
			var asDirect, asV2 any
			if err := jsonv1.Unmarshal(viaDirect, &asDirect); err != nil {
				t.Fatalf("-direct output is not valid JSON: %v", err)
			}
			if err := jsonv1.Unmarshal(viaV2, &asV2); err != nil {
				t.Fatalf("json/v2 output: %v", err)
			}
			if !reflect.DeepEqual(asDirect, asV2) {
				t.Error("-direct encoded a different document from json/v2")
			}
		})
	}
}

// TestDirectDecodesLikeJSONV2 does the same for the decode half, which has no
// escaping to differ over and so can always be compared as a value.
func TestDirectDecodesLikeJSONV2(t *testing.T) {
	for _, p := range payloads(t) {
		t.Run(p.name, func(t *testing.T) {
			viaDirect, err := p.unmarshalDirect(p.data)
			if err != nil {
				t.Fatalf("-direct unmarshal: %v", err)
			}
			viaV2 := p.newValue()
			if err := jsonv2.Unmarshal(p.data, viaV2); err != nil {
				t.Fatalf("json/v2 unmarshal: %v", err)
			}
			if !reflect.DeepEqual(viaDirect, viaV2) {
				t.Error("-direct decoded a different value from json/v2")
			}
		})
	}
}

// TestAppendMatchesMarshal checks that the zero-allocation half writes what
// the allocating half does, since only the latter is pinned above.
func TestAppendMatchesMarshal(t *testing.T) {
	for _, p := range payloads(t) {
		if p.hasMaps {
			continue
		}
		t.Run(p.name, func(t *testing.T) {
			want, err := p.marshalDirect()
			if err != nil {
				t.Fatal(err)
			}
			got, err := p.appendDirect([]byte("prefix:"))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, append([]byte("prefix:"), want...)) {
				t.Errorf("AppendT = %s, want prefix:%s", got, want)
			}
		})
	}
}

func BenchmarkMarshal(b *testing.B) {
	ps := payloads(b)

	b.Run("json-v2", func(b *testing.B) {
		for _, p := range ps {
			b.Run(p.name, func(b *testing.B) {
				if _, err := jsonv2.Marshal(p.decoded); err != nil {
					b.Fatal(err)
				}

				b.ReportAllocs()
				b.SetBytes(int64(len(p.data)))

				for b.Loop() {
					if _, err := jsonv2.Marshal(p.decoded); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	})

	b.Run("odjson-direct", func(b *testing.B) {
		for _, p := range ps {
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

// BenchmarkAppendDirect writes into a buffer the caller keeps, which is the
// only measurement here that does not allocate its result.
func BenchmarkAppendDirect(b *testing.B) {
	for _, p := range payloads(b) {
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

	b.Run("json-v2", func(b *testing.B) {
		for _, p := range ps {
			b.Run(p.name, func(b *testing.B) {
				if err := jsonv2.Unmarshal(p.data, p.newValue()); err != nil {
					b.Fatal(err)
				}

				b.ReportAllocs()
				b.SetBytes(int64(len(p.data)))

				for b.Loop() {
					if err := jsonv2.Unmarshal(p.data, p.newValue()); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	})

	b.Run("odjson-direct", func(b *testing.B) {
		for _, p := range ps {
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

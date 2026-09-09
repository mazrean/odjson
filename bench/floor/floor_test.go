// Package floor measures the cost a host library pays for routing through
// json.Marshaler / json.Unmarshaler at all, independently of how fast the
// generated codec is.
//
// The marshaler here returns a precomputed byte slice and the unmarshaler does
// nothing, so the numbers are the floor for *any* implementation of those
// interfaces. If the floor is already above what the library's own reflection
// path costs, then no code generator can beat it through that interface, no
// matter how fast its own encoding or decoding is.
package floor

import (
	jsonv1 "encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"os"
	"testing"

	"github.com/bytedance/sonic"
	gojson "github.com/goccy/go-json"

	"github.com/mazrean/odjson/bench/plain"
)

type fixture struct {
	name    string
	payload []byte
	encoded []byte
}

var fixtures []fixture

// current selects which fixture the free marshaler returns, since the method
// has no other way to be told.
var current *fixture

func TestMain(m *testing.M) {
	twitterJSON, err := os.ReadFile("../testdata/twitter.json")
	if err != nil {
		panic(err)
	}
	var tw plain.TwitterStruct
	if err := jsonv1.Unmarshal(twitterJSON, &tw); err != nil {
		panic(err)
	}
	// Encode with json/v2, which is the minimal escaping odjson's generated
	// MarshalJSONTo produces. Handing the encoder v1's HTML-escaped form
	// instead would make it unescape on the way through and overstate the
	// floor.
	twOut, err := jsonv2.Marshal(&tw)
	if err != nil {
		panic(err)
	}

	smallJSON := plain.SmallPayload()
	var sm plain.Book
	if err := jsonv1.Unmarshal(smallJSON, &sm); err != nil {
		panic(err)
	}
	smOut, err := jsonv2.Marshal(&sm)
	if err != nil {
		panic(err)
	}

	fixtures = []fixture{
		{name: "twitter", payload: twitterJSON, encoded: twOut},
		{name: "small", payload: smallJSON, encoded: smOut},
	}
	os.Exit(m.Run())
}

// freeMarshaler hands back an already encoded document. Its MarshalJSON costs
// nothing, so whatever the host library takes on top is pure interface
// overhead: the call, the validation of the returned bytes and the copy.
type freeMarshaler struct{}

func (freeMarshaler) MarshalJSON() ([]byte, error) { return current.encoded, nil }

// freeUnmarshaler throws its input away, so whatever the host library takes is
// the cost of locating the value and dispatching to the method.
type freeUnmarshaler struct{}

func (*freeUnmarshaler) UnmarshalJSON([]byte) error { return nil }

// freeMarshalerTo and freeUnmarshalerFrom are the same idea for
// encoding/json/v2's streaming interfaces, which odjson also generates and
// which both encoding/json and encoding/json/v2 prefer on Go 1.27. Writing an
// already encoded value and skipping the incoming one is the least any
// implementation can do.
type freeMarshalerTo struct{}

func (freeMarshalerTo) MarshalJSONTo(enc *jsontext.Encoder) error {
	return enc.WriteValue(current.encoded)
}

type freeUnmarshalerFrom struct{}

func (*freeUnmarshalerFrom) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	return dec.SkipValue()
}

func BenchmarkFloorMarshal(b *testing.B) {
	libs := []struct {
		name string
		fn   func(any) ([]byte, error)
	}{
		{"encoding-json", jsonv1.Marshal},
		{"json-v2", func(v any) ([]byte, error) { return jsonv2.Marshal(v) }},
		{"sonic", sonic.Marshal},
		{"go-json", gojson.Marshal},
	}
	v := freeMarshaler{}
	for _, l := range libs {
		for i := range fixtures {
			b.Run(l.name+"/"+fixtures[i].name, func(b *testing.B) {
				current = &fixtures[i]
				if _, err := l.fn(v); err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.SetBytes(int64(len(current.payload)))
				for b.Loop() {
					if _, err := l.fn(v); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

// BenchmarkFloorStreamMarshal is BenchmarkFloorMarshal for the streaming
// interface. Only the two stdlib packages understand it.
func BenchmarkFloorStreamMarshal(b *testing.B) {
	libs := []struct {
		name string
		fn   func(any) ([]byte, error)
	}{
		{"encoding-json", jsonv1.Marshal},
		{"json-v2", func(v any) ([]byte, error) { return jsonv2.Marshal(v) }},
	}
	v := freeMarshalerTo{}
	for _, l := range libs {
		for i := range fixtures {
			b.Run(l.name+"/"+fixtures[i].name, func(b *testing.B) {
				current = &fixtures[i]
				if _, err := l.fn(v); err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.SetBytes(int64(len(current.payload)))
				for b.Loop() {
					if _, err := l.fn(v); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

// BenchmarkFloorStreamUnmarshal is BenchmarkFloorUnmarshal for the streaming
// interface.
func BenchmarkFloorStreamUnmarshal(b *testing.B) {
	libs := []struct {
		name string
		fn   func([]byte, any) error
	}{
		{"encoding-json", jsonv1.Unmarshal},
		{"json-v2", func(d []byte, v any) error { return jsonv2.Unmarshal(d, v) }},
	}
	for _, l := range libs {
		for i := range fixtures {
			f := &fixtures[i]
			b.Run(l.name+"/"+f.name, func(b *testing.B) {
				if err := l.fn(f.payload, new(freeUnmarshalerFrom)); err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.SetBytes(int64(len(f.payload)))
				for b.Loop() {
					if err := l.fn(f.payload, new(freeUnmarshalerFrom)); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

func BenchmarkFloorUnmarshal(b *testing.B) {
	libs := []struct {
		name string
		fn   func([]byte, any) error
	}{
		{"encoding-json", jsonv1.Unmarshal},
		{"json-v2", func(d []byte, v any) error { return jsonv2.Unmarshal(d, v) }},
		{"sonic", sonic.Unmarshal},
		{"go-json", gojson.Unmarshal},
	}
	for _, l := range libs {
		for i := range fixtures {
			f := &fixtures[i]
			b.Run(l.name+"/"+f.name, func(b *testing.B) {
				if err := l.fn(f.payload, new(freeUnmarshaler)); err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.SetBytes(int64(len(f.payload)))
				for b.Loop() {
					if err := l.fn(f.payload, new(freeUnmarshaler)); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

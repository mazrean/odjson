// Package ab measures the generated codec against the reflection baseline
// head to head.
//
// The other benchmark packages each measure one side, which makes comparing
// them a comparison across processes: different heaps, different GC state and
// several percent of drift between runs. The differences that matter here are
// smaller than that, so both sides are measured in one process, interleaved by
// the benchmark framework.
package ab

import (
	jsonv1 "encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"os"
	"testing"

	"github.com/bytedance/sonic"

	"github.com/mazrean/odjson/bench/gen"
	"github.com/mazrean/odjson/bench/plain"
)

var twitterJSON []byte

func TestMain(m *testing.M) {
	b, err := os.ReadFile("../testdata/twitter.json")
	if err != nil {
		panic(err)
	}
	twitterJSON = b
	os.Exit(m.Run())
}

type side struct {
	name string
	// value is a pre-decoded value of the side's own type.
	value any
	// fresh returns a pointer to a zero value of the side's own type.
	fresh func() any
}

func sides(tb testing.TB, payload []byte) []side {
	tb.Helper()
	g := new(gen.TwitterStruct)
	if err := jsonv1.Unmarshal(payload, g); err != nil {
		tb.Fatal(err)
	}
	p := new(plain.TwitterStruct)
	if err := jsonv1.Unmarshal(payload, p); err != nil {
		tb.Fatal(err)
	}
	return []side{
		{name: "gen", value: g, fresh: func() any { return new(gen.TwitterStruct) }},
		{name: "plain", value: p, fresh: func() any { return new(plain.TwitterStruct) }},
	}
}

func smallSides(tb testing.TB) []side {
	tb.Helper()
	data := plain.SmallPayload()
	g := new(gen.Book)
	if err := jsonv1.Unmarshal(data, g); err != nil {
		tb.Fatal(err)
	}
	p := new(plain.Book)
	if err := jsonv1.Unmarshal(data, p); err != nil {
		tb.Fatal(err)
	}
	return []side{
		{name: "gen", value: g, fresh: func() any { return new(gen.Book) }},
		{name: "plain", value: p, fresh: func() any { return new(plain.Book) }},
	}
}

func runMarshal(b *testing.B, ss []side, size int, fn func(any) ([]byte, error)) {
	for _, s := range ss {
		b.Run(s.name, func(b *testing.B) {
			if _, err := fn(s.value); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.SetBytes(int64(size))
			for b.Loop() {
				if _, err := fn(s.value); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func runUnmarshal(b *testing.B, ss []side, data []byte, fn func([]byte, any) error) {
	for _, s := range ss {
		b.Run(s.name, func(b *testing.B) {
			if err := fn(data, s.fresh()); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.SetBytes(int64(len(data)))
			for b.Loop() {
				if err := fn(data, s.fresh()); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// freeMarshalerTo writes an already encoded value, so whatever a library
// charges on top of it is the floor any MarshalerTo implementation pays. It is
// measured here rather than in bench/floor so that the floor, the generated
// codec and the reflection baseline all come from one process.
type freeMarshalerTo struct{}

var freeEncoded []byte

func (freeMarshalerTo) MarshalJSONTo(enc *jsontext.Encoder) error {
	return enc.WriteValue(freeEncoded)
}

func BenchmarkMarshalTwitterFloor(b *testing.B) {
	p := new(plain.TwitterStruct)
	if err := jsonv1.Unmarshal(twitterJSON, p); err != nil {
		b.Fatal(err)
	}
	out, err := jsonv2.Marshal(p)
	if err != nil {
		b.Fatal(err)
	}
	freeEncoded = out
	v := freeMarshalerTo{}
	for _, l := range []struct {
		name string
		fn   func(any) ([]byte, error)
	}{
		{"encoding-json", jsonv1.Marshal},
		{"json-v2", func(x any) ([]byte, error) { return jsonv2.Marshal(x) }},
	} {
		b.Run(l.name, func(b *testing.B) {
			if _, err := l.fn(v); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.SetBytes(int64(len(twitterJSON)))
			for b.Loop() {
				if _, err := l.fn(v); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// freeUnmarshalerFrom discards the value it is given, so whatever a library
// charges on top of it is the floor any UnmarshalerFrom implementation pays
// for locating the value.
type freeUnmarshalerFrom struct{}

func (*freeUnmarshalerFrom) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	return dec.SkipValue()
}

func BenchmarkMarshalSmallFloor(b *testing.B) {
	p := new(plain.Book)
	if err := jsonv1.Unmarshal(plain.SmallPayload(), p); err != nil {
		b.Fatal(err)
	}
	out, err := jsonv2.Marshal(p)
	if err != nil {
		b.Fatal(err)
	}
	freeEncoded = out
	v := freeMarshalerTo{}
	n := len(plain.SmallPayload())
	for _, l := range []struct {
		name string
		fn   func(any) ([]byte, error)
	}{
		{"encoding-json", jsonv1.Marshal},
		{"json-v2", func(x any) ([]byte, error) { return jsonv2.Marshal(x) }},
	} {
		b.Run(l.name, func(b *testing.B) {
			if _, err := l.fn(v); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.SetBytes(int64(n))
			for b.Loop() {
				if _, err := l.fn(v); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkUnmarshalTwitterFloor(b *testing.B) {
	for _, l := range []struct {
		name string
		fn   func([]byte, any) error
	}{
		{"encoding-json", jsonv1.Unmarshal},
		{"json-v2", func(d []byte, x any) error { return jsonv2.Unmarshal(d, x) }},
	} {
		b.Run(l.name, func(b *testing.B) {
			if err := l.fn(twitterJSON, new(freeUnmarshalerFrom)); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.SetBytes(int64(len(twitterJSON)))
			for b.Loop() {
				if err := l.fn(twitterJSON, new(freeUnmarshalerFrom)); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkMarshalTwitter(b *testing.B) {
	ss := sides(b, twitterJSON)
	b.Run("encoding-json", func(b *testing.B) { runMarshal(b, ss, len(twitterJSON), jsonv1.Marshal) })
	b.Run("json-v2", func(b *testing.B) {
		runMarshal(b, ss, len(twitterJSON), func(v any) ([]byte, error) { return jsonv2.Marshal(v) })
	})
}

func BenchmarkMarshalSmall(b *testing.B) {
	ss := smallSides(b)
	n := len(plain.SmallPayload())
	b.Run("encoding-json", func(b *testing.B) { runMarshal(b, ss, n, jsonv1.Marshal) })
	b.Run("json-v2", func(b *testing.B) {
		runMarshal(b, ss, n, func(v any) ([]byte, error) { return jsonv2.Marshal(v) })
	})
}

func BenchmarkUnmarshalTwitter(b *testing.B) {
	ss := sides(b, twitterJSON)
	b.Run("encoding-json", func(b *testing.B) { runUnmarshal(b, ss, twitterJSON, jsonv1.Unmarshal) })
	b.Run("json-v2", func(b *testing.B) {
		runUnmarshal(b, ss, twitterJSON, func(d []byte, v any) error { return jsonv2.Unmarshal(d, v) })
	})
}

func BenchmarkUnmarshalSmall(b *testing.B) {
	ss := smallSides(b)
	data := plain.SmallPayload()
	b.Run("encoding-json", func(b *testing.B) { runUnmarshal(b, ss, data, jsonv1.Unmarshal) })
	b.Run("json-v2", func(b *testing.B) {
		runUnmarshal(b, ss, data, func(d []byte, v any) error { return jsonv2.Unmarshal(d, v) })
	})
}

// BenchmarkDirectVsSonicStd puts odjson's direct decoder next to sonic
// configured to produce the same bytes, in the same process.
func BenchmarkDirectVsSonicStd(b *testing.B) {
	b.Run("twitter/odjson-direct", func(b *testing.B) {
		v := new(gen.TwitterStruct)
		if err := gen.UnmarshalTwitterStruct(twitterJSON, v); err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		b.SetBytes(int64(len(twitterJSON)))
		for b.Loop() {
			if err := gen.UnmarshalTwitterStruct(twitterJSON, new(gen.TwitterStruct)); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("twitter/sonic-std", func(b *testing.B) {
		if err := sonic.ConfigStd.Unmarshal(twitterJSON, new(plain.TwitterStruct)); err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		b.SetBytes(int64(len(twitterJSON)))
		for b.Loop() {
			if err := sonic.ConfigStd.Unmarshal(twitterJSON, new(plain.TwitterStruct)); err != nil {
				b.Fatal(err)
			}
		}
	})
}

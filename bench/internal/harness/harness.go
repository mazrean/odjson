// Package harness is what the single-library benchmark packages (easyjson,
// gojay) share: the fixtures, the benchmark loops named the way plain and gen
// name theirs, and a parity check against encoding/json for codecs whose
// code was written by hand or by another generator.
package harness

import (
	jsonv1 "encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

// Payload is one fixture document plus the Go type it decodes into.
type Payload struct {
	Name string
	// Data is the raw JSON document.
	Data []byte
	// New returns a pointer to a fresh zero value of the target type.
	New func() any
	// Decoded is the document decoded by encoding/json, the input to the
	// Marshal benchmarks so that decoding cost is not folded into encoding
	// cost, and the reference the parity check compares against.
	Decoded any
}

// Load reads the three fixtures — twitter, medium and small — and decodes each
// with encoding/json into the type newTwitter (twitter and medium) or newBook
// (small) returns.
func Load(tb testing.TB, newTwitter, newBook func() any) []Payload {
	tb.Helper()
	ps := []Payload{
		{Name: "twitter", Data: read(tb, "twitter.json"), New: newTwitter},
		{Name: "medium", Data: read(tb, "medium.json"), New: newTwitter},
		{Name: "small", Data: Small, New: newBook},
	}
	for i := range ps {
		p := &ps[i]
		p.Decoded = p.New()
		if err := jsonv1.Unmarshal(p.Data, p.Decoded); err != nil {
			tb.Fatalf("decode %s with encoding/json: %v", p.Name, err)
		}
	}
	return ps
}

// Small is the small fixture, the document bench/plain/small.go carries.
var Small = []byte(`{"id":12125925,"ids":[-2147483648,2147483647],"title":"未来简史-从智人到智神","titles":["hello","world"],"price":40.8,"prices":[-0.1,0.1],"hot":true,"hots":[true,true,true],"author":{"name":"json","age":99,"male":true},"authors":[{"name":"json","age":99,"male":true},{"name":"json","age":99,"male":true},{"name":"json","age":99,"male":true}],"weights":[]}`)

// read loads a file from bench/testdata, located from this source file rather
// than the working directory so every package reaches the same copy.
func read(tb testing.TB, name string) []byte {
	tb.Helper()
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		tb.Fatal("cannot locate the harness source directory")
	}
	b, err := os.ReadFile(filepath.Join(filepath.Dir(self), "..", "..", "testdata", name))
	if err != nil {
		tb.Fatal(err)
	}
	return b
}

// Marshal runs BenchmarkMarshal/<lib>/<payload> for every payload, the way
// bench/plain and bench/gen name theirs, so the chart can read all of them
// from one `go test -bench` output.
func Marshal(b *testing.B, lib string, ps []Payload, fn func(any) ([]byte, error)) {
	b.Run(lib, func(b *testing.B) {
		for _, p := range ps {
			b.Run(p.Name, func(b *testing.B) {
				// Warm up, so any lazily built encoder stays out of the
				// measurement; b.Loop resets the timer.
				if _, err := fn(p.Decoded); err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.SetBytes(int64(len(p.Data)))
				for b.Loop() {
					if _, err := fn(p.Decoded); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	})
}

// Unmarshal runs BenchmarkUnmarshal/<lib>/<payload> for every payload, into a
// fresh zero value each iteration: reusing one would let the decoder skip
// allocating the slices and maps it has already filled in.
func Unmarshal(b *testing.B, lib string, ps []Payload, fn func([]byte, any) error) {
	b.Run(lib, func(b *testing.B) {
		for _, p := range ps {
			b.Run(p.Name, func(b *testing.B) {
				if err := fn(p.Data, p.New()); err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.SetBytes(int64(len(p.Data)))
				for b.Loop() {
					if err := fn(p.Data, p.New()); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	})
}

// Parity checks that a codec decodes every payload to the value encoding/json
// decodes it to, and encodes that value to a document encoding/json reads back
// as the same value. Either side may be nil for a library that only does one
// direction. Values are compared in canonical form (see Canonical), so number
// representation and member order do not count as differences, while a nil
// slice against an empty one does.
func Parity(t *testing.T, ps []Payload, marshal func(any) ([]byte, error), unmarshal func([]byte, any) error) {
	t.Helper()
	for _, p := range ps {
		t.Run(p.Name, func(t *testing.T) {
			want := Canonical(t, p.Decoded)
			if unmarshal != nil {
				got := p.New()
				if err := unmarshal(p.Data, got); err != nil {
					t.Fatalf("unmarshal: %v", err)
				}
				if c := Canonical(t, got); !reflect.DeepEqual(c, want) {
					t.Errorf("decoded value differs from encoding/json's:\n got: %s\nwant: %s", trunc(c), trunc(want))
				}
			}
			if marshal != nil {
				out, err := marshal(p.Decoded)
				if err != nil {
					t.Fatalf("marshal: %v", err)
				}
				var got any
				if err := jsonv1.Unmarshal(out, &got); err != nil {
					t.Fatalf("marshal output is not valid JSON: %v", err)
				}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("encoded document differs from encoding/json's:\n got: %s\nwant: %s", trunc(got), trunc(want))
				}
			}
		})
	}
}

// Canonical is v as encoding/json sees it: encoded by encoding/json and read
// back into an untyped value, so that every number is a float64 and every
// object a map. Two values with the same JSON meaning compare equal in this
// form whatever Go types they were decoded into.
func Canonical(tb testing.TB, v any) any {
	tb.Helper()
	b, err := jsonv1.Marshal(v)
	if err != nil {
		tb.Fatal(err)
	}
	var out any
	if err := jsonv1.Unmarshal(b, &out); err != nil {
		tb.Fatal(err)
	}
	return out
}

func trunc(v any) string {
	s := fmt.Sprint(v)
	if len(s) > 400 {
		return s[:400] + "…"
	}
	return s
}

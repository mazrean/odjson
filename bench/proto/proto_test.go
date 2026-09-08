package proto

import (
	jsonv2 "encoding/json/v2"
	"reflect"
	"strings"
	"testing"
)

var payload = []byte(`{"id":12125925,"ids":[-2147483648,2147483647],"title":"未来简史-从智人到智神","titles":["hello","world"],"price":40.8,"prices":[-0.1,0.1],"hot":true,"hots":[true,true,true],"author":{"name":"json","age":99,"male":true},"authors":[{"name":"json","age":99,"male":true},{"name":"json","age":99,"male":true},{"name":"json","age":99,"male":true}],"weights":[]}`)

func decoded(tb testing.TB) Book {
	tb.Helper()
	var b Book
	if err := jsonv2.Unmarshal(payload, &b); err != nil {
		tb.Fatal(err)
	}
	return b
}

// TestVariantsAgree checks the three implementations produce the same JSON and
// decode to the same value, so the benchmarks below compare like with like.
func TestVariantsAgree(t *testing.T) {
	b := decoded(t)

	want, err := jsonv2.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	for name, v := range map[string]any{"blob": BlobBook(b), "token": TokBook(b)} {
		got, err := jsonv2.Marshal(v)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if string(got) != string(want) {
			t.Errorf("%s:\n reflection: %s\n %s:      %s", name, want, name, got)
		}
	}

	var blob BlobBook
	if err := jsonv2.Unmarshal(payload, &blob); err != nil {
		t.Fatal(err)
	}
	var tok TokBook
	if err := jsonv2.Unmarshal(payload, &tok); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(Book(blob), b) {
		t.Errorf("blob decode mismatch:\n want %#v\n got  %#v", b, Book(blob))
	}
	if !reflect.DeepEqual(Book(tok), b) {
		t.Errorf("token decode mismatch:\n want %#v\n got  %#v", b, Book(tok))
	}
}

// stringy returns a value whose bytes are dominated by long non-ASCII strings,
// the shape where jsontext's reformatting of a pre-built value is most
// expensive: it re-validates every string odjson already validated.
func stringy(tb testing.TB) Book {
	tb.Helper()
	v := decoded(tb)
	v.Title = strings.Repeat("未来简史-从智人到智神", 200)
	v.Titles = make([]string, 10)
	for i := range v.Titles {
		v.Titles[i] = strings.Repeat("从智人到智神", 100)
	}
	return v
}

func BenchmarkProtoMarshal(b *testing.B) {
	v := decoded(b)
	big := stringy(b)
	cases := []struct {
		name string
		val  any
	}{
		{"small/reflection", v},
		{"small/blob-WriteValue", BlobBook(v)},
		{"small/token-WriteToken", TokBook(v)},
		{"stringy/reflection", big},
		{"stringy/blob-WriteValue", BlobBook(big)},
		{"stringy/token-WriteToken", TokBook(big)},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			out, err := jsonv2.Marshal(c.val)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.SetBytes(int64(len(out)))
			for b.Loop() {
				if _, err := jsonv2.Marshal(c.val); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkProtoUnmarshal(b *testing.B) {
	bigPayload, err := jsonv2.Marshal(stringy(b))
	if err != nil {
		b.Fatal(err)
	}
	inputs := []struct {
		name string
		data []byte
	}{
		{"small", payload},
		{"stringy", bigPayload},
	}
	cases := []struct {
		name  string
		fresh func() any
	}{
		{"reflection", func() any { return new(Book) }},
		{"blob-ReadValue", func() any { return new(BlobBook) }},
		{"token-ReadToken", func() any { return new(TokBook) }},
	}
	for _, in := range inputs {
		for _, c := range cases {
			b.Run(in.name+"/"+c.name, func(b *testing.B) {
				if err := jsonv2.Unmarshal(in.data, c.fresh()); err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.SetBytes(int64(len(in.data)))
				for b.Loop() {
					if err := jsonv2.Unmarshal(in.data, c.fresh()); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

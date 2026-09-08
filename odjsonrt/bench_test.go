package odjsonrt_test

import (
	"testing"

	"github.com/mazrean/odjson/odjsonrt"
)

var benchDoc = []byte(`{"name":"gopher","tags":["alpha","beta","gamma"],"n":1234567,"f":1.5,"ok":true,"nested":{"a":[1,2,3],"b":null}}`)

func BenchmarkAppendString(b *testing.B) {
	buf := make([]byte, 0, 256)
	b.ReportAllocs()
	for b.Loop() {
		_ = odjsonrt.AppendString(buf, "a moderately long string value", true)
	}
}

func BenchmarkAppendFloat(b *testing.B) {
	buf := make([]byte, 0, 64)
	b.ReportAllocs()
	for b.Loop() {
		_, _ = odjsonrt.AppendFloat(buf, 1234.5678, 64)
	}
}

func BenchmarkSkipValue(b *testing.B) {
	b.SetBytes(int64(len(benchDoc)))
	b.ReportAllocs()
	for b.Loop() {
		_, _ = odjsonrt.SkipValue(benchDoc, 0)
	}
}

func BenchmarkValidate(b *testing.B) {
	b.SetBytes(int64(len(benchDoc)))
	b.ReportAllocs()
	for b.Loop() {
		_ = odjsonrt.Validate(benchDoc)
	}
}

func BenchmarkParseAny(b *testing.B) {
	b.SetBytes(int64(len(benchDoc)))
	b.ReportAllocs()
	for b.Loop() {
		_, _, _ = odjsonrt.ParseAny(benchDoc, 0)
	}
}

func BenchmarkEqualFold(b *testing.B) {
	key := []byte("SomeFieldName")
	b.ReportAllocs()
	for b.Loop() {
		_ = odjsonrt.EqualFold(key, "somefieldname")
	}
}

func BenchmarkAppendAny(b *testing.B) {
	v := map[string]any{
		"name":  "gopher",
		"tags":  []any{"alpha", "beta", "gamma"},
		"n":     1234567.0,
		"ok":    true,
		"inner": map[string]any{"a": []any{1.0, 2.0, 3.0}, "b": nil},
	}
	buf := make([]byte, 0, 512)
	b.ReportAllocs()
	for b.Loop() {
		_, _ = odjsonrt.AppendAny(buf, v, true)
	}
}

var benchStrings = map[string]string{
	"ascii":   "a moderately long string value with no escapes at all",
	"unicode": "日本語のツイートです。絵文字もあります \U0001f600 とても長いテキスト",
	"mixed":   "user さん said: \"hello\" <b>こんにちは</b> & more テキスト",
	"escapes": "line1\nline2\tvalue \"quoted\" and \\ backslash <html> & more",
	"invalid": "broken \xff sequence \xe6\x97 here \xed\xa0\x80 too",
}

func BenchmarkAppendStringKinds(b *testing.B) {
	buf := make([]byte, 0, 1024)
	for _, name := range []string{"ascii", "unicode", "mixed", "escapes", "invalid"} {
		s := benchStrings[name]
		b.Run(name, func(b *testing.B) {
			b.SetBytes(int64(len(s)))
			b.ReportAllocs()
			for b.Loop() {
				_ = odjsonrt.AppendString(buf, s, true)
			}
		})
	}
}

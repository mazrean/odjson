package odjsonrt

// Micro-benchmarks of the scanners over the twitter document, one per
// cost the decode profile names: the strict skip of the retweeted_status
// values, the whitespace runs, the string scan and the non-ASCII runs.
// They are what the third decode round (docs/internals.md) measured each
// change with, and they skip when bench/testdata is not there.

import (
	"bytes"
	"os"
	"testing"
)

func retweetedValues(t testing.TB) [][]byte {
	data, err := os.ReadFile("../bench/testdata/twitter.json")
	if err != nil {
		t.Skip(err)
	}
	var out [][]byte
	needle := []byte(`"retweeted_status": `)
	for i := 0; ; {
		j := bytes.Index(data[i:], needle)
		if j < 0 {
			break
		}
		p := i + j + len(needle)
		end, err := SkipValue(data, p)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, data[p:end])
		i = end
	}
	return out
}

func BenchmarkSkipRetweeted(b *testing.B) {
	vals := retweetedValues(b)
	var total int
	for _, v := range vals {
		total += len(v)
	}
	b.ReportMetric(float64(len(vals)), "values")
	b.ReportMetric(float64(total), "bytes")
	b.ResetTimer()
	for b.Loop() {
		for _, v := range vals {
			if _, err := SkipValueStrict(v, 0); err != nil {
				b.Fatal(err)
			}
		}
	}
}

// spaced rewrites an indented document the way json.dumps writes one: on
// one line, with ", " and ": " between its tokens. Strings are copied
// whole, so their contents are left alone.
func spaced(data []byte) []byte {
	out := make([]byte, 0, len(data))
	for i := 0; i < len(data); i++ {
		switch c := data[i]; {
		case c == '"':
			end, _, _, err := scanString(data, i)
			if err != nil {
				panic(err)
			}
			out = append(out, data[i:end]...)
			i = end - 1
		case c == ',' || c == ':':
			out = append(out, c, ' ')
		case spaceSet[c]:
		default:
			out = append(out, c)
		}
	}
	return out
}

func BenchmarkSkipSpaceTwitter(b *testing.B) {
	data, err := os.ReadFile("../bench/testdata/twitter.json")
	if err != nil {
		b.Skip(err)
	}
	b.Run("indented", func(b *testing.B) { benchSkipSpace(b, data) })
	b.Run("spaced", func(b *testing.B) { benchSkipSpace(b, spaced(data)) })
}

// benchSkipSpace calls SkipSpace at the start of every whitespace run that
// reaches skipSpaceSlow in a decode: the runs after a colon are settled by
// AfterName and left out.
func benchSkipSpace(b *testing.B, data []byte) {
	var starts []int
	for i := 1; i < len(data); i++ {
		if spaceSet[data[i]] && !spaceSet[data[i-1]] && data[i-1] != ':' {
			starts = append(starts, i)
		}
	}
	b.ResetTimer()
	for b.Loop() {
		s := 0
		for _, p := range starts {
			s += SkipSpace(data, p)
		}
		if s == 0 {
			b.Fatal()
		}
	}
}

func BenchmarkScanStringTwitter(b *testing.B) {
	data, err := os.ReadFile("../bench/testdata/twitter.json")
	if err != nil {
		b.Skip(err)
	}
	var starts []int
	for i := 1; i < len(data); i++ {
		if data[i] == '"' {
			end, _, _, err := scanString(data, i)
			if err != nil {
				b.Fatal(err)
			}
			starts = append(starts, i)
			i = end - 1
		}
	}
	b.ReportMetric(float64(len(starts)), "strings")
	b.ResetTimer()
	for b.Loop() {
		for _, p := range starts {
			if _, _, _, err := scanStringStrict(data, p); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkNonASCIITwitter(b *testing.B) {
	data, err := os.ReadFile("../bench/testdata/twitter.json")
	if err != nil {
		b.Skip(err)
	}
	var starts []int
	for i := 1; i < len(data); i++ {
		if data[i] >= 0x80 && data[i-1] < 0x80 {
			starts = append(starts, i)
		}
	}
	b.ResetTimer()
	for b.Loop() {
		s := 0
		for _, p := range starts {
			s += skipNonASCII(data, p)
		}
		if s == 0 {
			b.Fatal()
		}
	}
}

// BenchmarkAppendQuotedTwitter writes every string of the twitter document
// back out, once per StringMode: the encoder's counterpart of
// BenchmarkScanStringTwitter.
func BenchmarkAppendQuotedTwitter(b *testing.B) {
	data, err := os.ReadFile("../bench/testdata/twitter.json")
	if err != nil {
		b.Skip(err)
	}
	var strs []string
	for i := 1; i < len(data); i++ {
		if data[i] == '"' {
			s, end, err := ParseString(data, i)
			if err != nil {
				b.Fatal(err)
			}
			strs = append(strs, s)
			i = end - 1
		}
	}
	for _, m := range []struct {
		name string
		mode StringMode
	}{{"html", ModeHTML}, {"plain", ModePlain}, {"stream", ModeStream}, {"v2", ModeV2}, {"v2html", ModeV2HTML}} {
		b.Run(m.name, func(b *testing.B) {
			buf := make([]byte, 0, len(data))
			for b.Loop() {
				buf = buf[:0]
				for _, s := range strs {
					buf = AppendStringMode(buf, s, m.mode)
				}
			}
		})
	}
}

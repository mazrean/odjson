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

func BenchmarkSkipSpaceTwitter(b *testing.B) {
	data, err := os.ReadFile("../bench/testdata/twitter.json")
	if err != nil {
		b.Skip(err)
	}
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

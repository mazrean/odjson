package odjsonrt

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

func BenchmarkZZSkipRetweeted(b *testing.B) {
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

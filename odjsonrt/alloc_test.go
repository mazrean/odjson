//go:build !race

package odjsonrt_test

import (
	"testing"

	"github.com/mazrean/odjson/odjsonrt"
)

// TestNoAllocations pins the allocation behaviour of the helpers that
// generated code uses on its hot paths. It is skipped under the race detector,
// which perturbs the counts.
func TestNoAllocations(t *testing.T) {
	buf := make([]byte, 0, 4096)
	doc := []byte(`{"name":"gopher","tags":["a","b"],"n":12345,"f":1.5,"ok":true}`)
	str := []byte(`"a plain string with no escapes"`)
	num := []byte("12345")
	flt := []byte("1.5e10")
	unicodeStr := "\xe6\x97\xa5\xe6\x9c\xac\xe8\xaa\x9e \xe3\x81\xa7\xe3\x81\x99"
	invalidStr := "broken \xff sequence \xe6\x97 here"
	unicodeBytes := []byte(unicodeStr)

	cases := []struct {
		name string
		fn   func()
	}{
		{"AppendString", func() { _ = odjsonrt.AppendString(buf, "hello world", true) }},
		{"AppendStringUnicode", func() { _ = odjsonrt.AppendString(buf, unicodeStr, true) }},
		{"AppendStringInvalid", func() { _ = odjsonrt.AppendString(buf, invalidStr, true) }},
		{"AppendStringBytes", func() { _ = odjsonrt.AppendStringBytes(buf, unicodeBytes, true) }},
		{"AppendInt", func() { _ = odjsonrt.AppendInt(buf, -1234567) }},
		{"AppendUint", func() { _ = odjsonrt.AppendUint(buf, 1234567) }},
		{"AppendFloat", func() { _, _ = odjsonrt.AppendFloat(buf, 1.5, 64) }},
		{"AppendBool", func() { _ = odjsonrt.AppendBool(buf, true) }},
		{"AppendBase64", func() { _ = odjsonrt.AppendBase64(buf, []byte("hello")) }},
		{"SkipValue", func() { _, _ = odjsonrt.SkipValue(doc, 0) }},
		{"Validate", func() { _ = odjsonrt.Validate(doc) }},
		{"ParseStringBytes", func() { _, _, _, _ = odjsonrt.ParseStringBytes(str, 0) }},
		{"ParseKey", func() { _, _, _, _ = odjsonrt.ParseKey(doc, 1) }},
		{"ParseInt", func() { _, _, _ = odjsonrt.ParseInt(num, 0, 64) }},
		{"ParseUint", func() { _, _, _ = odjsonrt.ParseUint(num, 0, 64) }},
		{"ParseFloat", func() { _, _, _ = odjsonrt.ParseFloat(flt, 0, 64) }},
		{"ParseBool", func() { _, _, _ = odjsonrt.ParseBool(doc, 57) }},
		{"ParseRaw", func() { _, _, _ = odjsonrt.ParseRaw(doc, 0) }},
		{"EqualFold", func() { _ = odjsonrt.EqualFold([]byte("Name"), "name") }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if n := testing.AllocsPerRun(200, c.fn); n != 0 {
				t.Errorf("%s allocated %v times per run, want 0", c.name, n)
			}
		})
	}
}

package odjsonrt

import (
	"bytes"
	"encoding/json/jsontext"
	"io"
	"strings"
	"testing"
	"unsafe"
)

// oneByteReader delivers a single byte per Read, so a streaming decoder's
// unread buffer is as short as it can be.
type oneByteReader struct{ s string }

func (r *oneByteReader) Read(p []byte) (int, error) {
	if r.s == "" {
		return 0, io.EOF
	}
	p[0] = r.s[0]
	r.s = r.s[1:]
	return 1, nil
}

func TestNextKind(t *testing.T) {
	const doc = ` { "a" : [ 1 , "x" , true , null , {} ] , "b" : -2.5 } `
	// Every kind the generated decoders switch on, in document order,
	// alongside what PeekKind says at the same point.
	for _, mk := range []struct {
		name string
		dec  func() *jsontext.Decoder
	}{
		{"buffered", func() *jsontext.Decoder { return jsontext.NewDecoder(bytes.NewReader([]byte(doc))) }},
		{"one byte", func() *jsontext.Decoder { return jsontext.NewDecoder(&oneByteReader{doc}) }},
	} {
		t.Run(mk.name, func(t *testing.T) {
			dec := mk.dec()
			for {
				got := NextKind(dec)
				want := dec.PeekKind()
				if got != want {
					t.Fatalf("NextKind = %q, PeekKind = %q", got, want)
				}
				if _, err := dec.ReadToken(); err != nil {
					if err == io.EOF {
						return
					}
					t.Fatal(err)
				}
			}
		})
	}
}

func TestWholeValue(t *testing.T) {
	small := `{"a":1}`
	big := `{"a":"` + strings.Repeat("x", smallValue) + `"}`
	for _, tc := range []struct {
		doc  string
		want bool
	}{
		{small, true},
		{small + "  \n", true},
		{`[1,2]`, true},
		{`"s"`, false},
		{`{"a":1} x`, false},
		{big, false},
	} {
		// A bytes.Buffer is handed to the decoder whole on its first read,
		// which is the shape json/v2's Unmarshal gives the generated code.
		dec := jsontext.NewDecoder(bytes.NewBuffer([]byte(tc.doc)))
		dec.PeekKind()
		if got := WholeValue(dec); got != tc.want {
			t.Errorf("WholeValue(%.20q) = %v, want %v", tc.doc, got, tc.want)
		}
	}

	// On a streaming decoder the answer depends on the chunk, and either
	// answer has to leave the decoder usable.
	dec := jsontext.NewDecoder(&oneByteReader{small})
	if WholeValue(dec) {
		t.Error("an empty unread buffer must not be read whole")
	}
	v, err := dec.ReadValue()
	if err != nil || string(v) != small {
		t.Fatalf("ReadValue = %s, %v", v, err)
	}
}

func TestStringCacheMake(t *testing.T) {
	var c StringCache
	first := c.Make([]byte("hello"))
	again := c.Make([]byte("hello"))
	if first != "hello" || again != "hello" {
		t.Fatalf("Make = %q, %q", first, again)
	}
	if unsafe.StringData(first) != unsafe.StringData(again) {
		t.Error("the second Make of an equal string did not return the cached one")
	}
	// Strings that collide in the table are still returned correctly.
	for _, s := range []string{"", "a", "ab", "abc", "abcd", "abcdefg", "abcdefgh", "abcdefghi",
		strings.Repeat("y", 256), strings.Repeat("y", 257), "héllo", "hellp", "hallo"} {
		if got := c.Make([]byte(s)); got != s {
			t.Errorf("Make(%q) = %q", s, got)
		}
		if got := c.Make([]byte(s)); got != s {
			t.Errorf("Make(%q) second = %q", s, got)
		}
	}
	// A nil cache allocates and still returns the right string.
	var nilCache *StringCache
	if got := nilCache.Make([]byte("nil")); got != "nil" {
		t.Errorf("nil cache Make = %q", got)
	}
}

func TestParseStringValue(t *testing.T) {
	var c StringCache
	for _, tc := range []struct {
		val  string
		want string
		ok   bool
	}{
		{`""`, "", true},
		{`"plain"`, "plain", true},
		{`"héllo 世界"`, "héllo 世界", true},
		{`"a\"b\\c\/d\n"`, "a\"b\\c/d\n", true},
		{`"é😀"`, "é😀", true},
		{`"\ud800"`, "�", true},
		{`123`, "", false},
		{`"`, "", false},
	} {
		got, err := ParseStringValue([]byte(tc.val), &c)
		if (err == nil) != tc.ok {
			t.Errorf("ParseStringValue(%s) error = %v, want ok=%v", tc.val, err, tc.ok)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseStringValue(%s) = %q, want %q", tc.val, got, tc.want)
		}
	}
}

func TestParseFloatValue(t *testing.T) {
	for _, tc := range []struct {
		val  string
		bits int
		want float64
		ok   bool
	}{
		{"1.5", 64, 1.5, true},
		{"-0", 64, 0, true},
		{"1e3", 32, 1000, true},
		{"1e400", 64, 0, false},
		{"3.5e38", 32, 0, false},
		{`"1"`, 64, 0, false},
	} {
		got, err := ParseFloatValue([]byte(tc.val), tc.bits)
		if (err == nil) != tc.ok || got != tc.want {
			t.Errorf("ParseFloatValue(%s, %d) = %v, %v; want %v, ok=%v", tc.val, tc.bits, got, err, tc.want, tc.ok)
		}
	}
}

func TestParseIntFastPath(t *testing.T) {
	for _, tc := range []struct {
		in   string
		bits int
		want int64
		end  int
		ok   bool
	}{
		{"0", 64, 0, 1, true},
		{"-0", 64, 0, 2, true},
		{"42,", 64, 42, 2, true},
		{"-42]", 8, -42, 3, true},
		{"128", 8, 0, 0, false},
		{"-129", 8, 0, 0, false},
		{"32767}", 16, 32767, 5, true},
		{"999999999999999999", 64, 999999999999999999, 18, true},
		{"9223372036854775807", 64, 9223372036854775807, 19, true},
		{"9223372036854775808", 64, 0, 0, false},
		{"-9223372036854775808", 64, -9223372036854775808, 20, true},
		{"1.5", 64, 0, 0, false},
		{"1e2", 64, 0, 0, false},
		{"01", 64, 0, 1, true}, // the literal is "0"; the caller sees the stray digit
		{"-", 64, 0, 0, false},
		{"x", 64, 0, 0, false},
	} {
		got, end, err := ParseInt([]byte(tc.in), 0, tc.bits)
		if (err == nil) != tc.ok {
			t.Errorf("ParseInt(%q, %d) error = %v, want ok=%v", tc.in, tc.bits, err, tc.ok)
			continue
		}
		if tc.ok && (got != tc.want || end != tc.end) {
			t.Errorf("ParseInt(%q, %d) = %d, %d; want %d, %d", tc.in, tc.bits, got, end, tc.want, tc.end)
		}
	}
	for _, tc := range []struct {
		in   string
		bits int
		want uint64
		ok   bool
	}{
		{"0", 64, 0, true},
		{"255", 8, 255, true},
		{"256", 8, 0, false},
		{"-0", 64, 0, false},
		{"-1", 64, 0, false},
		{"18446744073709551615", 64, 18446744073709551615, true},
		{"18446744073709551616", 64, 0, false},
	} {
		got, _, err := ParseUint([]byte(tc.in), 0, tc.bits)
		if (err == nil) != tc.ok || got != tc.want {
			t.Errorf("ParseUint(%q, %d) = %d, %v; want %d, ok=%v", tc.in, tc.bits, got, err, tc.want, tc.ok)
		}
	}
}

func TestErrKindFrom(t *testing.T) {
	dec := jsontext.NewDecoder(bytes.NewReader([]byte(`"str"`)))
	if err := ErrKindFrom(dec, "T"); err == nil || !strings.Contains(err.Error(), "string") {
		t.Errorf("ErrKindFrom on a string = %v", err)
	}
	dec = jsontext.NewDecoder(bytes.NewReader([]byte(`}`)))
	if err := ErrKindFrom(dec, "T"); err == nil {
		t.Error("ErrKindFrom on a syntax error = nil")
	}
}

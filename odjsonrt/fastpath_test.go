package odjsonrt

import (
	"math"
	"strconv"
	"testing"
)

// TestParseSimpleFloatMatchesStrconv checks that every literal the fast path
// accepts produces exactly strconv.ParseFloat's bits, and that it declines
// everything outside its shape rather than guessing.
func TestParseSimpleFloatMatchesStrconv(t *testing.T) {
	accepted := []string{
		"0", "-0", "1", "-1", "40.8", "-0.1", "0.1", "123456789.123456789",
		"9007199254740991", "9007199254740991.0", "0.0000000000000000000001",
		"1.5", "2.25", "3.14159", "1234567890123456789", "0.30000000000000004",
		"100", "1e", // "1e" declines at the exponent, see below
	}
	for _, lit := range accepted {
		data := []byte(lit + ",")
		got, end, ok := ParseSimpleFloat(data, 0, 64)
		want, err := strconv.ParseFloat(lit, 64)
		if lit == "1e" {
			if ok {
				t.Errorf("%q: accepted a literal with an exponent", lit)
			}
			continue
		}
		if !ok {
			// Declining is allowed; the general path takes over. But the
			// common shapes must be handled.
			if len(lit) <= 16 {
				t.Errorf("%q: declined", lit)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%q: strconv: %v", lit, err)
		}
		if end != len(lit) {
			t.Errorf("%q: end = %d, want %d", lit, end, len(lit))
		}
		if math.Float64bits(got) != math.Float64bits(want) {
			t.Errorf("%q: got %v (%#x), want %v (%#x)", lit, got, math.Float64bits(got), want, math.Float64bits(want))
		}
	}

	declined := []string{"", "-", ".5", "1.", "01", "1e5", "1E5", "1.5e-3", "abc", "-.5",
		"12345678901234567890",      // twenty digits
		"0.00000000000000000000001", // twenty-three fraction digits
	}
	for _, lit := range declined {
		if _, _, ok := ParseSimpleFloat([]byte(lit), 0, 64); ok {
			t.Errorf("%q: accepted", lit)
		}
	}

	// float32 goes through a float32 division and must match strconv's
	// 32-bit rounding, not a double rounding through float64.
	for _, lit := range []string{"0.1", "3.4028235", "16777215.5", "1.00000012", "-2.5"} {
		got, _, ok := ParseSimpleFloat([]byte(lit), 0, 32)
		want, err := strconv.ParseFloat(lit, 32)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			continue
		}
		if math.Float64bits(got) != math.Float64bits(want) {
			t.Errorf("%q as float32: got %v, want %v", lit, got, want)
		}
	}
	if _, _, ok := ParseSimpleFloat([]byte("16777216.5"), 0, 32); ok {
		t.Error("a mantissa beyond 2^24 was accepted for float32")
	}
}

func TestParseFloatFastPathAgreesWithGeneralPath(t *testing.T) {
	for _, lit := range []string{"40.8", "-0.1", "0.1", "1e5", "1.5E-3", "12345678901234567890.5", "-0"} {
		data := []byte(lit)
		got, end, err := ParseFloat(data, 0, 64)
		if err != nil {
			t.Fatalf("%q: %v", lit, err)
		}
		want, _ := strconv.ParseFloat(lit, 64)
		if math.Float64bits(got) != math.Float64bits(want) || end != len(lit) {
			t.Errorf("%q: got %v/%d, want %v/%d", lit, got, end, want, len(lit))
		}
	}
	for _, lit := range []string{"1.", "-", "+1", ".5", "1e"} {
		if _, _, err := ParseFloat([]byte(lit), 0, 64); err == nil {
			t.Errorf("%q: accepted", lit)
		}
	}
}

func TestParseBoolFastPath(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want bool
		end  int
		ok   bool
	}{
		{"true", true, 4, true},
		{"false", false, 5, true},
		{"true,", true, 4, true},
		{"tru", false, 0, false},
		{"fals", false, 0, false},
		{"null", false, 0, false},
		{"", false, 0, false},
		{"True", false, 0, false},
	} {
		got, end, err := ParseBool([]byte(tc.in), 0)
		if (err == nil) != tc.ok {
			t.Errorf("%q: err = %v, want ok=%v", tc.in, err, tc.ok)
			continue
		}
		if tc.ok && (got != tc.want || end != tc.end) {
			t.Errorf("%q: got %v/%d, want %v/%d", tc.in, got, end, tc.want, tc.end)
		}
	}
}

func TestAfterKey(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want int
		ok   bool
	}{
		{":1", 1, true},
		{": 1", 2, true},
		{" : 1", 3, true},
		{"\t:\n\t1", 4, true},
		{"1", 0, false},
		{"", 0, false},
		{" ", 1, false},
		{"x:1", 0, false},
	} {
		got, err := AfterKey([]byte(tc.in), 0)
		if (err == nil) != tc.ok {
			t.Errorf("%q: err = %v, want ok=%v", tc.in, err, tc.ok)
			continue
		}
		if tc.ok && got != tc.want {
			t.Errorf("%q: got %d, want %d", tc.in, got, tc.want)
		}
	}
}

// TestStringCacheMakeUTF8 checks the validated bit: a string Make stored,
// which nobody has checked, is checked on its first MakeUTF8 hit, and an
// invalid one is never handed back as valid.
func TestStringCacheMakeUTF8(t *testing.T) {
	c := new(StringCache)
	bad := []byte("a\xffb")
	if s := c.Make(bad); s != string(bad) {
		t.Fatalf("Make = %q", s)
	}
	if s, ok := c.MakeUTF8(bad); ok {
		t.Fatalf("MakeUTF8 accepted invalid UTF-8 from the cache: %q", s)
	}
	good := []byte("日本語")
	s1, ok := c.MakeUTF8(good)
	if !ok || s1 != string(good) {
		t.Fatalf("MakeUTF8 = %q, %v", s1, ok)
	}
	s2, ok := c.MakeUTF8(good)
	if !ok || s2 != s1 {
		t.Fatalf("second MakeUTF8 = %q, %v", s2, ok)
	}
	if s, ok := c.MakeUTF8([]byte("\xff\xfe")); ok {
		t.Fatalf("MakeUTF8 accepted %q", s)
	}
	// A nil cache still validates.
	var nilCache *StringCache
	if _, ok := nilCache.MakeUTF8(bad); ok {
		t.Fatal("nil cache accepted invalid UTF-8")
	}
	if s, ok := nilCache.MakeUTF8(good); !ok || s != string(good) {
		t.Fatalf("nil cache: %q, %v", s, ok)
	}
}

func TestUnquoteBulkRuns(t *testing.T) {
	for _, tc := range []struct {
		in, want string
		strict   bool
		ok       bool
	}{
		{`plain`, "plain", false, true},
		{`a\/b`, "a/b", false, true},
		{`http:\/\/x.y\/z`, "http://x.y/z", false, true},
		{`日本\n語`, "日本\n語", false, true},
		{`\u00e9t\u00e9`, "été", false, true},
		{`\ud83d\ude00!`, "😀!", false, true},
		{`\ud83d!`, "\uFFFD!", false, true},
		{`\ud83d!`, "", true, false},
		{"a\xffb\\n", "a\uFFFDb\n", false, true},
		{`\x`, "", false, false},
		{`\`, "", false, false},
		{`\u12`, "", false, false},
		{`\uZZZZ`, "", false, false},
	} {
		got, ok := unquote([]byte(tc.in), tc.strict)
		if ok != tc.ok {
			t.Errorf("%q strict=%v: ok = %v, want %v", tc.in, tc.strict, ok, tc.ok)
			continue
		}
		if ok && string(got) != tc.want {
			t.Errorf("%q: got %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSkipSpaceRuns(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want int
	}{
		{"x", 0},
		{" x", 1},
		{"\n        x", 9},
		{"\n                 x", 18},
		{"        ", 8},
		{"       \t\r\n  x", 12},
		{"", 0},
		{"                x", 16},
	} {
		if got := SkipSpace([]byte(tc.in), 0); got != tc.want {
			t.Errorf("%q: got %d, want %d", tc.in, got, tc.want)
		}
	}
}

package odjsonrt

import (
	jsonv1 "encoding/json"
	"encoding/json/jsontext"
	"strings"
	"testing"
)

// rawValue hands a pre-encoded value to whatever encoder takes it, so that
// encoding/json's Marshal applies its own reformat to it. That reformat is
// what ModeV2HTML reproduces, which makes this the oracle for it.
// The field is exported only to satisfy the linter's rule about a struct
// handed to a marshaler; what actually encodes it is the method below.
type rawValue struct{ B []byte }

func (r rawValue) MarshalJSONTo(enc *jsontext.Encoder) error {
	return enc.WriteValue(r.B)
}

// v2htmlWant is what encoding/json makes of the string literal s written
// under ModeStream — the bytes a generated MarshalJSONTo hands the encoder
// when the direct path is not available.
func v2htmlWant(t *testing.T, s string) string {
	t.Helper()
	lit := appendQuotedStream(nil, []byte(s), true)
	out, err := jsonv1.Marshal(rawValue{lit})
	if err != nil {
		t.Fatalf("oracle Marshal(%q): %v", s, err)
	}
	return string(out)
}

// checkV2HTML asks that the body appender and the oracle agree.
func checkV2HTML(t *testing.T, s string) {
	t.Helper()
	got := string(appendQuotedV2HTML(append(make([]byte, 0, 1), '"'), []byte(s), false)) + `"`
	if want := v2htmlWant(t, s); got != want {
		t.Errorf("appendQuotedV2HTML(%q) = %q, want %q", s, got, want)
	}
}

// TestAppendQuotedV2HTMLMatchesV1Reformat walks the shapes the appender divides
// differently — a word loop, a tail read backwards from the end, a tail of
// two overlapping halves, a byte loop — over content of each kind it treats
// differently, and corrupts one byte of each prefix at a time.
//
// The corruption sweep is what pins the two paths a valid run cannot reach:
// the rune-by-rune walk of a run that is not UTF-8, and the rewind that a
// line separator inside a validated run causes.
func TestAppendQuotedV2HTMLMatchesV1Reformat(t *testing.T) {
	mixes := []string{
		"abcdefghijklmnopqrstuvwxyz0123456789",
		"a<b>c&d\"e\\f\ng\x01h<i>j&k",
		strings.Repeat("\u65e5\u672c\u8a9e", 12),
		"x" + strings.Repeat("\u65e5\u672c\u8a9e", 12),
		strings.Repeat("\u00e9\u00e0\u00fc", 12),
		strings.Repeat("\U0001f600", 10) + "<&>",
		// The two line separators, which a validated run holds and the
		// reformat escapes: alone, among ASCII, among CJK, twice over and at
		// each end. They are what the rewind after a copied run is for.
		"\u2028",
		"\u2029\u2028\u2029",
		"a\u2028b\u2029c" + strings.Repeat("\u65e5\u672c\u8a9e", 6),
		strings.Repeat("\u65e5\u672c", 4) + "\u2028" + strings.Repeat("\u672c\u8a9e", 4),
		"abcdefghijklmnop\u2028",
		// A separator on each side of a byte that is not UTF-8, which sends
		// the run through the rune-by-rune walk instead.
		"\u2028\xffx\u2029",
		"\u65e5\u672c\u2028\xc0\u2029\u8a9e",
	}
	bad := []byte{0x00, 0x1F, 0x22, 0x26, 0x3C, 0x3E, 0x5C, 0x80, 0xBF, 0xC0, 0xC2, 0xE0, 0xE2, 0xED, 0xF0, 0xF5, 0xFF}
	for _, mix := range mixes {
		for n := range len(mix) + 1 {
			s := mix[:n]
			checkV2HTML(t, s)
			for p := range len(s) {
				for _, b := range bad {
					m := []byte(s)
					m[p] = b
					checkV2HTML(t, string(m))
				}
			}
		}
	}
}

// TestAppendQuotedV2HTMLIntoFullBuffer pins the appender against a
// destination whose capacity ends at its length, as reserve_test.go does for
// the ModeV2 body.
func TestAppendQuotedV2HTMLIntoFullBuffer(t *testing.T) {
	for _, s := range []string{"", "a", "a<b", "abcdefgh", "日本語テキスト", "a b", "a\xffb"} {
		dst := make([]byte, 1, 1)
		dst[0] = '"'
		got := string(appendQuotedV2HTML(dst, []byte(s), false)) + `"`
		if want := v2htmlWant(t, s); got != want {
			t.Errorf("appendQuotedV2HTML(%q) into a full buffer = %q, want %q", s, got, want)
		}
	}
}

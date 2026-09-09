package odjsonrt

import (
	"bytes"
	jsonv2 "encoding/json/v2"
	"math/rand/v2"
	"strings"
	"testing"
	"unicode/utf8"
)

// utf8Cases are the strings every UTF-8 sensitive path is checked on: every
// sequence length, both boundary leads, the surrogate gap, the largest code
// point, and the invalid shapes around each of them.
var utf8Cases = []string{
	"", "a", "abc", strings.Repeat("x", 40),
	"日本語のツイートです。絵文字もあります \U0001f600 とても長いテキスト",
	"Ελληνικά и русский", "é", "\u0080", "\u07ff", // two byte
	"\u0800", "\u0fff", "\ud7ff", "\ue000", "\uffff", // three byte, E0 and ED at their edges
	"\U00010000", "\U0010ffff", "\U0001f600", // four byte, F0 and F4 at their edges
	"�", "\xef\xbf\xbd", // a literal replacement character is valid
	"\xff", "a\xffb", "\xc0\x80", "\xc1\xbf", // stray and overlong two byte
	"\xe0\x80\x80", "\xe0\x9f\xbf", // overlong three byte
	"\xed\xa0\x80", "\xed\xbf\xbf", // surrogates
	"\xf0\x80\x80\x80", "\xf0\x8f\xbf\xbf", // overlong four byte
	"\xf4\x90\x80\x80", "\xf5\x80\x80\x80", // above U+10FFFF
	"\xe6\x97", "日本\xe6", "日本\xe6\x97", "\xf0\x9f\x98", // truncated at the end
	"\xe6\x97x", "\xe6x\xa5", "\xf0\x9f\x98x", // truncated in the middle
	"\xe6\x97\xa5\xe6", "ab\xe6\x97\xa5\xe6\x97", // valid then truncated
	"日本語\"quoted\"", "日本\\語", "日本\n語", "日本\x00語", "日本\x1f語",
	"<b>こんにちは</b> & more テキスト", "  ",
}

func TestSkipNonASCIIMatchesUTF8Valid(t *testing.T) {
	check := func(s string) {
		t.Helper()
		b := []byte(s)
		if got, want := validUTF8(b), utf8.Valid(b); got != want {
			t.Errorf("validUTF8(%q) = %v, want %v", s, got, want)
		}
	}
	for _, s := range utf8Cases {
		check(s)
		check("prefix " + s)
		check(s + " suffix")
		check(strings.Repeat(s, 3))
	}
	// Random byte strings, and random valid strings with a byte disturbed.
	r := rand.New(rand.NewPCG(1, 2))
	for range 200000 {
		n := r.IntN(24)
		b := make([]byte, n)
		for i := range b {
			if r.IntN(2) == 0 {
				b[i] = byte(r.IntN(256))
			} else {
				b[i] = byte(0x80 + r.IntN(128))
			}
		}
		check(string(b))
		var sb strings.Builder
		for range r.IntN(8) {
			sb.WriteRune(rune(r.IntN(0x110000)))
		}
		s := []byte(sb.String())
		check(string(s))
		if len(s) > 0 {
			s[r.IntN(len(s))] = byte(r.IntN(256))
			check(string(s))
		}
	}
}

// TestAppendQuotedV2MatchesJSONV2 pins ModeV2's string output to
// encoding/json/v2's: the same bytes for every valid string, and a refusal
// for every invalid one.
func TestAppendQuotedV2MatchesJSONV2(t *testing.T) {
	check := func(s string) {
		t.Helper()
		got, err := AppendStringChecked([]byte("x"), s, ModeV2)
		want, werr := jsonv2.Marshal(s)
		if (werr != nil) != (err != nil) {
			t.Errorf("%q: err = %v, json/v2 err = %v", s, err, werr)
			return
		}
		if err != nil {
			if string(got) != "x" {
				t.Errorf("%q: dst not restored on error: %q", s, got)
			}
			return
		}
		if !bytes.Equal(got[1:], want) {
			t.Errorf("%q: got %q, want %q", s, got[1:], want)
		}
	}
	for _, s := range utf8Cases {
		check(s)
		check("prefix " + s)
		check(s + " suffix")
		check(strings.Repeat(s, 5))
		check(strings.Repeat("0123456789abcdef", 2) + s)
	}
	r := rand.New(rand.NewPCG(3, 4))
	for range 100000 {
		var sb strings.Builder
		for range r.IntN(12) {
			switch r.IntN(4) {
			case 0:
				sb.WriteByte(byte(r.IntN(128)))
			case 1:
				sb.WriteRune(rune(0x80 + r.IntN(0x800-0x80)))
			case 2:
				sb.WriteRune(rune(0x800 + r.IntN(0x10000-0x800)))
			default:
				sb.WriteRune(rune(0x10000 + r.IntN(0x110000-0x10000)))
			}
		}
		s := []byte(sb.String())
		check(string(s))
		if len(s) > 0 && r.IntN(2) == 0 {
			s[r.IntN(len(s))] = byte(r.IntN(256))
			check(string(s))
		}
	}
}

// TestParseStringStrictMatchesJSONV2 pins the strict decoders' UTF-8 rule to
// encoding/json/v2's on the same literals: the same acceptance, and the same
// value when accepted, whether the string is decoded, skipped or a key.
func TestParseStringStrictMatchesJSONV2(t *testing.T) {
	check := func(lit string) {
		t.Helper()
		var want string
		werr := jsonv2.Unmarshal([]byte(lit), &want)
		data := []byte(lit)
		got, end, err := ParseStringStrict(data, 0, nil)
		if (werr != nil) != (err != nil) {
			t.Errorf("ParseStringStrict(%q): err = %v, json/v2 err = %v", lit, err, werr)
		} else if err == nil && (got != want || end != len(lit)) {
			t.Errorf("ParseStringStrict(%q) = %q, %d; want %q, %d", lit, got, end, want, len(lit))
		}
		if _, serr := skipStringStrict(data, 0); (werr != nil) != (serr != nil) {
			t.Errorf("skipStringStrict(%q): err = %v, json/v2 err = %v", lit, serr, werr)
		}
		if _, verr := SkipValueStrict(data, 0); (werr != nil) != (verr != nil) {
			t.Errorf("SkipValueStrict(%q): err = %v, json/v2 err = %v", lit, verr, werr)
		}
		obj := []byte(`{` + lit + `:1}`)
		var m map[string]int
		merr := jsonv2.Unmarshal(obj, &m)
		if _, kerr := SkipValueStrict(obj, 0); (merr != nil) != (kerr != nil) {
			t.Errorf("SkipValueStrict(%q): err = %v, json/v2 err = %v", obj, kerr, merr)
		}
	}
	quote := func(s string) string {
		b, err := jsonv2.Marshal(s)
		if err != nil {
			return `"` + s + `"`
		}
		return string(b)
	}
	for _, s := range utf8Cases {
		check(quote(s))
		check(quote("prefix " + s))
		check(quote(s + " suffix"))
		check(quote(strings.Repeat(s, 5)))
		check(quote(strings.Repeat("0123456789abcdef", 2) + s))
		// The same content followed by an escape.
		q := quote(s)
		check(`"日` + q[1:len(q)-1] + `\n"`)
	}
	// Truncated input: json/v2 rejects it either way, and so must the
	// scanner, whatever the cut lands on.
	for _, s := range []string{`"日本語"`, `"日本語\n"`, `"ab日日"`} {
		for i := 1; i < len(s); i++ {
			check(s[:i])
		}
	}
	r := rand.New(rand.NewPCG(5, 6))
	for range 100000 {
		var sb strings.Builder
		for range r.IntN(12) {
			switch r.IntN(4) {
			case 0:
				// Printable ASCII other than the two bytes that would
				// end the literal early.
				if c := byte(0x20 + r.IntN(0x5f)); c != '"' && c != '\\' {
					sb.WriteByte(c)
				}
			case 1:
				sb.WriteRune(rune(0x80 + r.IntN(0x800-0x80)))
			case 2:
				sb.WriteRune(rune(0x800 + r.IntN(0x10000-0x800)))
			default:
				sb.WriteRune(rune(0x10000 + r.IntN(0x110000-0x10000)))
			}
		}
		b := []byte(`"` + sb.String() + `"`)
		check(string(b))
		if len(b) > 2 && r.IntN(2) == 0 {
			b[1+r.IntN(len(b)-2)] = byte(0x80 + r.IntN(128))
			check(string(b))
		}
	}
}

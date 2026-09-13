package odjsonrt

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// TestCopyNonASCIIRunsMatchUTF8Valid walks a run of three byte sequences of
// every length the word loops divide differently, corrupts one byte of it at
// a time, and asks that the encoder accept exactly what utf8.Valid does and
// write exactly what it was given.
//
// The twenty-four byte loop judges eight characters through three different
// lane layouts at once, so a mask written for the wrong lane shows up only
// when the bad byte falls in that lane: every position of every length is
// what pins it.
func TestCopyNonASCIIRunsMatchUTF8Valid(t *testing.T) {
	base := strings.Repeat("日本語", 20) // three byte sequences, lead E6/E8
	// Sequences whose second byte is restricted, and one of each other
	// width, so the lengths do not all divide by three.
	mixes := []string{base, "ࠀ" + base, "퟿" + base, "é" + base, "😀" + base, base + "a"}
	bad := []byte{0x00, 0x41, 0x7F, 0x80, 0xBF, 0xC0, 0xC1, 0xC2, 0xE0, 0xED, 0xEE, 0xF0, 0xF4, 0xF5, 0xFF, 0x22, 0x5C}
	for _, mix := range mixes {
		for n := 1; n <= 80 && n <= len(mix); n++ {
			s := mix[:n]
			check(t, s)
			for p := range len(s) {
				for _, b := range bad {
					m := []byte(s)
					m[p] = b
					check(t, string(m))
				}
			}
		}
	}
}

// check asks that the ModeV2 body accept s exactly when it is valid UTF-8 and
// leave it byte for byte when nothing in it needs escaping.
func check(t *testing.T, s string) {
	t.Helper()
	got, err := AppendStringBodyChecked([]byte("["), s, ModeV2)
	if valid := utf8.ValidString(s); valid != (err == nil) {
		t.Fatalf("AppendStringBodyChecked(%q) err = %v, utf8.ValidString = %v", s, err, valid)
	} else if !valid {
		if string(got) != "[" {
			t.Fatalf("AppendStringBodyChecked(%q) left %q behind", s, got)
		}
		return
	}
	want := "[" + escapeRef(s)
	if string(got) != want {
		t.Fatalf("AppendStringBodyChecked(%q) = %q, want %q", s, got, want)
	}
}

// escapeRef is ModeV2's escaping written the slow way.
func escapeRef(s string) string {
	var b strings.Builder
	for i := range len(s) {
		if c := s[i]; c < 0x20 || c == '"' || c == '\\' {
			b.Write(appendEscape(nil, c))
		} else {
			b.WriteByte(c)
		}
	}
	return b.String()
}

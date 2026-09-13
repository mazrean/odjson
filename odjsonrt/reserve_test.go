package odjsonrt

import (
	"strconv"
	"testing"
)

// The string body and the digit writer both take room for their whole result
// before they store anything, and then store whole words into it, up to seven
// bytes past what they are writing. The reservation is the only thing between
// those stores and a buffer's end, so it gets a test of its own: a
// destination whose capacity ends at its length must grow rather than let a
// store run past it.

// TestAppendStringBodyIntoFullBuffer covers the string body.
func TestAppendStringBodyIntoFullBuffer(t *testing.T) {
	for _, s := range []string{"", "a", "abc", "abcdefgh", "abcdefghijklmnop", "日本語テキスト", "a\"b", "\x01x"} {
		dst := make([]byte, 1, 1)
		dst[0] = '"'
		got, err := AppendStringBodyChecked(dst, s, ModeV2)
		if err != nil {
			t.Fatalf("AppendStringBodyChecked(%q) = %v", s, err)
		}
		want := `"` + stringBodyRef(t, s)
		if string(got) != want {
			t.Errorf("AppendStringBodyChecked(%q) into a full buffer = %q, want %q", s, got, want)
		}
	}
}

// TestAppendUintIntoFullBuffer covers the digit writer.
func TestAppendUintIntoFullBuffer(t *testing.T) {
	for _, v := range []uint64{100, 12345, 1234567890123456789, ^uint64(0)} {
		dst := make([]byte, 3, 3)
		copy(dst, "ab:")
		got := string(AppendUint(dst, v))
		if want := "ab:" + strconv.FormatUint(v, 10); got != want {
			t.Errorf("AppendUint(%d) into a full buffer = %q, want %q", v, got, want)
		}
	}
}

// stringBodyRef is the body of s under ModeV2, written into a buffer with room
// to spare, for the comparison above.
func stringBodyRef(t *testing.T, s string) string {
	t.Helper()
	b, err := AppendStringBodyChecked(make([]byte, 0, 64), s, ModeV2)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

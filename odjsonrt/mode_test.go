package odjsonrt

import (
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"testing"
)

// TestAppendStringModeStream checks that ModeStream output, once passed
// through a jsontext.Encoder the way generated code does, is identical to what
// ModePlain produces directly, for every input jsontext accepts.
func TestAppendStringModeStream(t *testing.T) {
	inputs := []string{
		"",
		"plain",
		`quote " backslash \ end`,
		"control\x00\x01\x1f\b\f\n\r\t",
		"html <>& chars",
		"日本語のテキスト",
		"emoji \U0001F600 tail",
		"line\u2028sep\u2029end",
		"\x7f delete",
	}
	for _, in := range inputs {
		stream := AppendStringMode(nil, in, ModeStream)

		var out []byte
		enc := jsontext.NewEncoder(writerFunc(func(p []byte) (int, error) {
			out = append(out, p...)
			return len(p), nil
		}))
		if err := enc.WriteValue(stream); err != nil {
			t.Errorf("%q: jsontext rejected ModeStream output %s: %v", in, stream, err)
			continue
		}
		got := trimNewline(out)

		// The oracle is json/v2 itself: ModeStream exists to feed a
		// jsontext.Encoder, so the bytes that come out the other side must be
		// the ones json/v2 would have written for the same string. That is
		// not always ModePlain's output: json/v2 leaves U+2028 and U+2029
		// unescaped, while encoding/json v1 escapes them.
		want, err := jsonv2.Marshal(in)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Errorf("%q:\n through jsontext: %s\n ModePlain:        %s", in, got, want)
		}
	}
}

// TestAppendStringModeStreamRejectsInvalidUTF8 pins the contract: ModeStream
// does not sanitise, it relies on the encoder to reject.
func TestAppendStringModeStreamRejectsInvalidUTF8(t *testing.T) {
	stream := AppendStringMode(nil, "bad \xff byte", ModeStream)
	enc := jsontext.NewEncoder(writerFunc(func(p []byte) (int, error) { return len(p), nil }))
	if err := enc.WriteValue(stream); err == nil {
		t.Error("expected jsontext to reject invalid UTF-8 from ModeStream")
	}
}

func TestStringModeEscapeHTML(t *testing.T) {
	for m, want := range map[StringMode]bool{ModeHTML: true, ModePlain: false, ModeStream: false} {
		if got := m.EscapeHTML(); got != want {
			t.Errorf("StringMode(%d).EscapeHTML() = %v, want %v", m, got, want)
		}
	}
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

func trimNewline(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}

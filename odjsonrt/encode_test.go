package odjsonrt_test

import (
	"bytes"
	"encoding"
	"encoding/json"
	"errors"
	"math"
	"math/rand"
	"strconv"
	"strings"
	"testing"

	"github.com/mazrean/odjson/odjsonrt"
)

// marshalHTML is the oracle for escapeHTML == true.
func marshalHTML(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal(%#v): %v", v, err)
	}
	return b
}

// marshalPlain is the oracle for escapeHTML == false.
func marshalPlain(t *testing.T, v any) []byte {
	t.Helper()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		t.Fatalf("Encode(%#v): %v", v, err)
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
}

// stringCorpus returns a wide set of string values, including every single
// byte value and a number of malformed UTF-8 sequences.
func stringCorpus() []string {
	corpus := []string{
		"",
		"hello",
		"a\"b\\c/d",
		"\x00\x01\x02\x1f",
		"\b\f\n\r\t",
		"<script>&</script>",
		"\x7f",
		"tab\tnewline\n",
		"\xe6\x97\xa5\xe6\x9c\xac\xe8\xaa\x9e",
		string(rune(0x2028)),
		string(rune(0x2029)),
		"before" + string(rune(0x2028)) + "after",
		"\xff",
		"a\xffb",
		"\xed\xa0\x80",     // UTF-8 encoded surrogate half
		"\xc0\x80",         // overlong NUL
		"\xf4\x90\x80\x80", // beyond U+10FFFF
		"\xe6\x97",         // truncated sequence
		"\x81",             // lone continuation byte
		"\xf0\x9f\x98\x80", // U+1F600
		strings.Repeat("x", 300) + "\xff",
	}
	for i := range 256 {
		corpus = append(corpus, string([]byte{byte(i)}))
		corpus = append(corpus, "a"+string([]byte{byte(i)})+"b")
	}
	return corpus
}

func TestAppendString(t *testing.T) {
	for _, s := range stringCorpus() {
		t.Run(strconv.Quote(s), func(t *testing.T) {
			if got, want := odjsonrt.AppendString(nil, s, true), marshalHTML(t, s); !bytes.Equal(got, want) {
				t.Errorf("escapeHTML=true: got %s want %s", got, want)
			}
			if got, want := odjsonrt.AppendString(nil, s, false), marshalPlain(t, s); !bytes.Equal(got, want) {
				t.Errorf("escapeHTML=false: got %s want %s", got, want)
			}
			// AppendStringBytes must agree with AppendString.
			if got, want := odjsonrt.AppendStringBytes(nil, []byte(s), true), odjsonrt.AppendString(nil, s, true); !bytes.Equal(got, want) {
				t.Errorf("AppendStringBytes: got %s want %s", got, want)
			}
		})
	}
}

func TestAppendStringPreservesPrefix(t *testing.T) {
	dst := []byte("prefix:")
	got := odjsonrt.AppendString(dst, "hi", true)
	if string(got) != `prefix:"hi"` {
		t.Fatalf("got %s", got)
	}
}

func TestAppendStringQuoted(t *testing.T) {
	type quoted struct {
		S string `json:"s,string"`
	}
	for _, s := range stringCorpus() {
		t.Run(strconv.Quote(s), func(t *testing.T) {
			got := odjsonrt.AppendStringQuoted(nil, s, true)
			want := bytes.TrimSuffix(bytes.TrimPrefix(marshalHTML(t, quoted{s}), []byte(`{"s":`)), []byte("}"))
			if !bytes.Equal(got, want) {
				t.Errorf("escapeHTML=true: got %s want %s", got, want)
			}
			got = odjsonrt.AppendStringQuoted(nil, s, false)
			want = bytes.TrimSuffix(bytes.TrimPrefix(marshalPlain(t, quoted{s}), []byte(`{"s":`)), []byte("}"))
			if !bytes.Equal(got, want) {
				t.Errorf("escapeHTML=false: got %s want %s", got, want)
			}
		})
	}
}

func TestAppendInt(t *testing.T) {
	for _, v := range []int64{0, 1, -1, math.MaxInt64, math.MinInt64, 1234567890} {
		if got, want := odjsonrt.AppendInt(nil, v), marshalHTML(t, v); !bytes.Equal(got, want) {
			t.Errorf("AppendInt(%d) = %s, want %s", v, got, want)
		}
	}
}

func TestAppendUint(t *testing.T) {
	for _, v := range []uint64{0, 1, math.MaxUint64, 1234567890} {
		if got, want := odjsonrt.AppendUint(nil, v), marshalHTML(t, v); !bytes.Equal(got, want) {
			t.Errorf("AppendUint(%d) = %s, want %s", v, got, want)
		}
	}
}

func TestAppendBool(t *testing.T) {
	if got := string(odjsonrt.AppendBool(nil, true)); got != "true" {
		t.Errorf("got %s", got)
	}
	if got := string(odjsonrt.AppendBool(nil, false)); got != "false" {
		t.Errorf("got %s", got)
	}
}

// float64Corpus returns the interesting float64 boundary values.
func float64Corpus() []float64 {
	return []float64{
		0, math.Copysign(0, -1), 1, -1, 0.5, 1.0 / 3.0,
		1e-6, -1e-6, 1e-7, 9.99e-7, 1.0000001e-6,
		1e20, 1e21, -1e21, 9.999999999999999e20,
		math.MaxFloat64, -math.MaxFloat64, math.SmallestNonzeroFloat64,
		1e100, 1e-100, 123456789012345678901.0, 3.4e-320,
		float64(math.MaxFloat32), float64(math.SmallestNonzeroFloat32),
		math.Nextafter(1e-6, 0), math.Nextafter(1e-6, 1),
		math.Nextafter(1e21, 0), math.Nextafter(1e21, math.Inf(1)),
	}
}

// float32Corpus returns the interesting float32 boundary values.
func float32Corpus() []float32 {
	return []float32{
		0, float32(math.Copysign(0, -1)), 1, -1, 0.5,
		1e-6, -1e-6, 1e-7, 9.99e-7,
		1e20, 1e21, -1e21,
		math.MaxFloat32, -math.MaxFloat32, math.SmallestNonzeroFloat32,
		math.Nextafter32(1e-6, 0), math.Nextafter32(1e-6, 1),
		math.Nextafter32(1e21, 0), math.Nextafter32(1e21, float32(math.Inf(1))),
	}
}

func TestAppendFloat64(t *testing.T) {
	check := func(t *testing.T, v float64) {
		t.Helper()
		got, err := odjsonrt.AppendFloat(nil, v, 64)
		if err != nil {
			t.Fatalf("AppendFloat(%v): %v", v, err)
		}
		if want := marshalHTML(t, v); !bytes.Equal(got, want) {
			t.Fatalf("AppendFloat(%v) = %s, want %s", v, got, want)
		}
	}
	for _, v := range float64Corpus() {
		check(t, v)
	}
	rng := rand.New(rand.NewSource(1))
	for range 50000 {
		v := math.Float64frombits(rng.Uint64())
		if math.IsNaN(v) || math.IsInf(v, 0) {
			continue
		}
		check(t, v)
	}
	// Also cover "normal" magnitudes densely.
	for range 20000 {
		v := (rng.Float64() - 0.5) * math.Pow(10, float64(rng.Intn(60)-30))
		check(t, v)
	}
}

func TestAppendFloat32(t *testing.T) {
	check := func(t *testing.T, v float32) {
		t.Helper()
		got, err := odjsonrt.AppendFloat(nil, float64(v), 32)
		if err != nil {
			t.Fatalf("AppendFloat(%v, 32): %v", v, err)
		}
		if want := marshalHTML(t, v); !bytes.Equal(got, want) {
			t.Fatalf("AppendFloat(%v, 32) = %s, want %s", v, got, want)
		}
	}
	for _, v := range float32Corpus() {
		check(t, v)
	}
	rng := rand.New(rand.NewSource(2))
	for range 50000 {
		v := math.Float32frombits(rng.Uint32())
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			continue
		}
		check(t, v)
	}
}

func TestAppendFloatNonFinite(t *testing.T) {
	for _, v := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, err := odjsonrt.AppendFloat(nil, v, 64); err == nil {
			t.Errorf("AppendFloat(%v) = nil error, want error", v)
		}
	}
}

func TestAppendBase64(t *testing.T) {
	cases := [][]byte{nil, {}, {1, 2, 3}, []byte("hello world"), bytes.Repeat([]byte{0xff}, 100)}
	for _, c := range cases {
		got := odjsonrt.AppendBase64(nil, c)
		want := marshalHTML(t, c)
		if !bytes.Equal(got, want) {
			t.Errorf("AppendBase64(%v) = %s, want %s", c, got, want)
		}
	}
	// Appending must not clobber an existing prefix.
	got := odjsonrt.AppendBase64([]byte("x"), []byte{1, 2, 3})
	if string(got) != `x"AQID"` {
		t.Errorf("got %s", got)
	}
}

func TestAppendNumber(t *testing.T) {
	valid := []string{"", "0", "-0", "1", "-1", "1.5", "1e10", "-1.5E-10", "0.0001", "123456789012345678901234567890"}
	for _, n := range valid {
		got, err := odjsonrt.AppendNumber(nil, n)
		if err != nil {
			t.Errorf("AppendNumber(%q): %v", n, err)
			continue
		}
		want := marshalHTML(t, json.Number(n))
		if !bytes.Equal(got, want) {
			t.Errorf("AppendNumber(%q) = %s, want %s", n, got, want)
		}
	}
	invalid := []string{"x", "01", "1.", ".1", "+1", "1e", "1e+", "--1", "1 ", " 1", "0x10", "Infinity", "NaN", "1.2.3"}
	for _, n := range invalid {
		if _, err := odjsonrt.AppendNumber(nil, n); err == nil {
			t.Errorf("AppendNumber(%q) = nil error, want error", n)
		}
		if _, err := json.Marshal(json.Number(n)); err == nil {
			t.Errorf("oracle accepted %q", n)
		}
	}
}

func TestAppendRaw(t *testing.T) {
	cases := []string{
		`null`, `1`, `"a"`, `{}`, `[]`,
		"  {  \"a\" : 1 , \"b\" : [ 1 , 2 ] }  ",
		`{"h":"<&>"}`,
		`"` + string(rune(0x2028)) + string(rune(0x2029)) + `"`,
		`"a<b"`,
	}
	for _, c := range cases {
		for _, escape := range []bool{true, false} {
			got, err := odjsonrt.AppendRaw(nil, []byte(c), escape)
			if err != nil {
				t.Errorf("AppendRaw(%q, %v): %v", c, escape, err)
				continue
			}
			var want []byte
			if escape {
				want = marshalHTML(t, json.RawMessage(c))
			} else {
				want = marshalPlain(t, json.RawMessage(c))
			}
			if !bytes.Equal(got, want) {
				t.Errorf("AppendRaw(%q, %v) = %s, want %s", c, escape, got, want)
			}
		}
	}
	if got, err := odjsonrt.AppendRaw(nil, nil, true); err != nil || string(got) != "null" {
		t.Errorf("AppendRaw(nil) = %s, %v", got, err)
	}
	for _, bad := range []string{"", "{", "[1,]", "1 2", "tru"} {
		if _, err := odjsonrt.AppendRaw(nil, []byte(bad), true); err == nil {
			t.Errorf("AppendRaw(%q) = nil error, want error", bad)
		}
	}
}

func TestAppendAny(t *testing.T) {
	values := []any{
		nil, true, false, "a<b", "",
		int(-1), int8(-8), int16(-16), int32(-32), int64(-64),
		uint(1), uint8(8), uint16(16), uint32(32), uint64(64), uintptr(128),
		float64(1.5), float32(1.5), float64(1e-7), float32(1e21),
		json.Number("123"), json.Number("-1.5e10"),
		json.RawMessage(`{ "a" : "<" }`), json.RawMessage(nil),
		[]byte(nil), []byte{}, []byte{1, 2, 3},
		[]any(nil), []any{}, []any{1.0, "two", nil, true, []any{}, map[string]any{}},
		map[string]any(nil), map[string]any{},
		map[string]any{"k": []any{1.0, "two", nil}},
		map[string]any{"b": 2.0, "a": 1.0, "C": 3.0, "<&>": 4.0, "": 5.0},
		// Keys where sorting the raw key and sorting the escaped key differ.
		map[string]any{"a\"": 1.0, "a#": 2.0},
		map[string]any{"a\n": 1.0, "a!": 2.0},
		map[string]any{string(rune(0x2028)): 1.0, "a": 2.0},
		map[string]any{"a\xff": 1.0, "a~": 2.0},
		map[string]any{"nested": map[string]any{"deep": []any{map[string]any{"x": nil}}}},
		"a" + string(rune(0x2028)) + "bÿ",
		struct {
			A int    `json:"a"`
			B string `json:"b"`
		}{1, "<"},
		[]int{1, 2, 3},
	}
	for _, v := range values {
		got, err := odjsonrt.AppendAny(nil, v, true)
		if err != nil {
			t.Fatalf("AppendAny(%#v): %v", v, err)
		}
		if want := marshalHTML(t, v); !bytes.Equal(got, want) {
			t.Errorf("AppendAny(%#v, true) = %s, want %s", v, got, want)
		}
		got, err = odjsonrt.AppendAny(nil, v, false)
		if err != nil {
			t.Fatalf("AppendAny(%#v): %v", v, err)
		}
		if want := marshalPlain(t, v); !bytes.Equal(got, want) {
			t.Errorf("AppendAny(%#v, false) = %s, want %s", v, got, want)
		}
	}
	if _, err := odjsonrt.AppendAny(nil, make(chan int), true); err == nil {
		t.Error("AppendAny(chan) = nil error, want error")
	}
	// An invalid json.Number or RawMessage nested in a container must fail.
	if _, err := odjsonrt.AppendAny(nil, []any{json.Number("nope")}, true); err == nil {
		t.Error("AppendAny(invalid Number) = nil error, want error")
	}
	if _, err := odjsonrt.AppendAny(nil, map[string]any{"a": json.RawMessage("{")}, true); err == nil {
		t.Error("AppendAny(invalid RawMessage) = nil error, want error")
	}
	if _, err := odjsonrt.AppendAny(nil, []any{math.Inf(1)}, true); err == nil {
		t.Error("AppendAny(+Inf) = nil error, want error")
	}
	// A self-referential value must not overflow the stack.
	cyclic := []any{nil}
	cyclic[0] = cyclic
	if _, err := odjsonrt.AppendAny(nil, cyclic, true); err == nil {
		t.Error("AppendAny(cyclic) = nil error, want error")
	}
	// Deeply nested values are still encoded correctly.
	var deep any = 1.0
	for range 500 {
		deep = []any{deep}
	}
	got, err := odjsonrt.AppendAny(nil, deep, true)
	if err != nil {
		t.Fatalf("AppendAny(deep): %v", err)
	}
	if want := marshalHTML(t, deep); !bytes.Equal(got, want) {
		t.Errorf("AppendAny(deep) = %s, want %s", got, want)
	}
	// The destination must be left untouched when encoding fails.
	dst := []byte("prefix")
	out, err := odjsonrt.AppendAny(dst, make(chan int), true)
	if err == nil || string(out) != "prefix" {
		t.Errorf("failed AppendAny returned %q, %v", out, err)
	}
}

// rawMarshaler returns fixed bytes from MarshalJSON.
type rawMarshaler struct {
	out []byte
	err error
}

func (m rawMarshaler) MarshalJSON() ([]byte, error) { return m.out, m.err }

func TestAppendMarshaler(t *testing.T) {
	src := []byte(`{ "a" : "<" }`)
	got, err := odjsonrt.AppendMarshaler(nil, rawMarshaler{out: src}, true)
	if err != nil {
		t.Fatalf("AppendMarshaler: %v", err)
	}
	if want := marshalHTML(t, json.RawMessage(src)); !bytes.Equal(got, want) {
		t.Errorf("escapeHTML=true: got %s want %s", got, want)
	}
	got, err = odjsonrt.AppendMarshaler(nil, rawMarshaler{out: src}, false)
	if err != nil {
		t.Fatalf("AppendMarshaler: %v", err)
	}
	if want := marshalPlain(t, json.RawMessage(src)); !bytes.Equal(got, want) {
		t.Errorf("escapeHTML=false: got %s want %s", got, want)
	}
	if got, err := odjsonrt.AppendMarshaler(nil, nil, true); err != nil || string(got) != "null" {
		t.Errorf("nil marshaler: got %s, %v", got, err)
	}
	if _, err := odjsonrt.AppendMarshaler(nil, rawMarshaler{out: []byte(`{`)}, true); err == nil {
		t.Error("invalid marshaler output accepted")
	}
	sentinel := errors.New("boom")
	if _, err := odjsonrt.AppendMarshaler(nil, rawMarshaler{err: sentinel}, true); !errors.Is(err, sentinel) {
		t.Errorf("got %v, want %v", err, sentinel)
	}
}

// textMarshaler returns fixed text from MarshalText.
type textMarshaler struct {
	out string
	err error
}

func (m textMarshaler) MarshalText() ([]byte, error) { return []byte(m.out), m.err }

func TestAppendTextMarshaler(t *testing.T) {
	var m encoding.TextMarshaler = textMarshaler{out: "a<b"}
	got, err := odjsonrt.AppendTextMarshaler(nil, m, true)
	if err != nil {
		t.Fatalf("AppendTextMarshaler: %v", err)
	}
	if want := marshalHTML(t, m); !bytes.Equal(got, want) {
		t.Errorf("got %s, want %s", got, want)
	}
	got, err = odjsonrt.AppendTextMarshaler(nil, m, false)
	if err != nil || string(got) != `"a<b"` {
		t.Errorf("got %s, %v", got, err)
	}
	if got, err := odjsonrt.AppendTextMarshaler(nil, nil, true); err != nil || string(got) != "null" {
		t.Errorf("nil text marshaler: got %s, %v", got, err)
	}
	sentinel := errors.New("boom")
	if _, err := odjsonrt.AppendTextMarshaler(nil, textMarshaler{err: sentinel}, true); !errors.Is(err, sentinel) {
		t.Errorf("got %v, want %v", err, sentinel)
	}
}

func TestBufferPool(t *testing.T) {
	for range 100 {
		b := odjsonrt.AcquireBuffer()
		if len(b) != 0 {
			t.Fatalf("AcquireBuffer returned length %d", len(b))
		}
		b = append(b, "hello"...)
		odjsonrt.ReleaseBuffer(b)
	}
	// Releasing an oversized buffer must not panic and must not be reused.
	odjsonrt.ReleaseBuffer(make([]byte, 0, 4<<20))
	odjsonrt.ReleaseBuffer(nil)
	if len(odjsonrt.AcquireBuffer()) != 0 {
		t.Fatal("AcquireBuffer returned a non-empty buffer")
	}
}

// FuzzAppendString checks that AppendString reproduces encoding/json byte for
// byte for arbitrary input, in both escaping modes.
func FuzzAppendString(f *testing.F) {
	for _, s := range stringCorpus() {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if got, want := odjsonrt.AppendString(nil, s, true), marshalHTML(t, s); !bytes.Equal(got, want) {
			t.Fatalf("escapeHTML=true: got %s want %s for %q", got, want, s)
		}
		if got, want := odjsonrt.AppendString(nil, s, false), marshalPlain(t, s); !bytes.Equal(got, want) {
			t.Fatalf("escapeHTML=false: got %s want %s for %q", got, want, s)
		}
		if got, want := odjsonrt.AppendStringBytes(nil, []byte(s), true), odjsonrt.AppendString(nil, s, true); !bytes.Equal(got, want) {
			t.Fatalf("AppendStringBytes: got %s want %s for %q", got, want, s)
		}
	})
}

// FuzzAppendFloat checks that AppendFloat reproduces encoding/json byte for
// byte for arbitrary finite floats.
func FuzzAppendFloat(f *testing.F) {
	for _, v := range float64Corpus() {
		f.Add(math.Float64bits(v))
	}
	f.Fuzz(func(t *testing.T, bits uint64) {
		v := math.Float64frombits(bits)
		if math.IsNaN(v) || math.IsInf(v, 0) {
			if _, err := odjsonrt.AppendFloat(nil, v, 64); err == nil {
				t.Fatalf("AppendFloat(%v) = nil error", v)
			}
			return
		}
		got, err := odjsonrt.AppendFloat(nil, v, 64)
		if err != nil {
			t.Fatalf("AppendFloat(%v): %v", v, err)
		}
		if want := marshalHTML(t, v); !bytes.Equal(got, want) {
			t.Fatalf("AppendFloat(%v) = %s, want %s", v, got, want)
		}
		f32 := float32(v)
		if math.IsInf(float64(f32), 0) {
			return
		}
		got, err = odjsonrt.AppendFloat(nil, float64(f32), 32)
		if err != nil {
			t.Fatalf("AppendFloat(%v, 32): %v", f32, err)
		}
		if want := marshalHTML(t, f32); !bytes.Equal(got, want) {
			t.Fatalf("AppendFloat(%v, 32) = %s, want %s", f32, got, want)
		}
	})
}

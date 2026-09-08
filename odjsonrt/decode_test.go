package odjsonrt_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/mazrean/odjson/odjsonrt"
)

func TestSkipSpace(t *testing.T) {
	data := []byte(" \t\r\n x")
	if got := odjsonrt.SkipSpace(data, 0); got != 5 {
		t.Errorf("SkipSpace = %d, want 5", got)
	}
	if got := odjsonrt.SkipSpace(data, 5); got != 5 {
		t.Errorf("SkipSpace = %d, want 5", got)
	}
	if got := odjsonrt.SkipSpace(nil, 0); got != 0 {
		t.Errorf("SkipSpace(nil) = %d, want 0", got)
	}
}

func TestSkipValue(t *testing.T) {
	cases := []struct {
		in   string
		next int
	}{
		{`null`, 4},
		{`true`, 4},
		{`false`, 5},
		{`0`, 1},
		{`-1.5e+10`, 8},
		{`"abc"`, 5},
		{`"a\"b"`, 6},
		{`[]`, 2},
		{`[ ]`, 3},
		{`[1,2,3]`, 7},
		{`{}`, 2},
		{`{ }`, 3},
		{`{"a":1,"b":[1,{"c":null}]}`, 26},
		{`[1] trailing`, 3},
		{`{"a":1} x`, 7},
		{"[\n\t1\n]", 6},
	}
	for _, c := range cases {
		got, err := odjsonrt.SkipValue([]byte(c.in), 0)
		if err != nil {
			t.Errorf("SkipValue(%q): %v", c.in, err)
			continue
		}
		if got != c.next {
			t.Errorf("SkipValue(%q) = %d, want %d", c.in, got, c.next)
		}
	}
}

func TestValidateParity(t *testing.T) {
	cases := []string{
		``, ` `, `null`, `nul`, `nulll`, `true`, `tru`, `false`, `0`, `-0`, `01`, `1.`, `.1`,
		`+1`, `1e`, `1e+`, `1e10`, `1E-10`, `0.0`, `-`, `--1`, `1 2`, `"a"`, `"a`, `"\q"`,
		`"\u12"`, `"\u123g"`, `"\ud800"`, `"` + "\x01" + `"`, `[]`, `[,]`, `[1,]`, `[1 2]`,
		`{}`, `{,}`, `{"a"}`, `{"a":}`, `{"a":1,}`, `{a:1}`, `{"a":1 "b":2}`, `[[[]]]`,
		`{"a":{"b":[1,2,{"c":"d"}]}}`, "\xef\xbb\xbf{}", `  [1]  `, `[1]]`, `}`, `]`,
		`"𐀀"`, `"\\"`, `"\/"`, `[null,true,false]`, `{"":1}`, `{"a":1,"a":2}`,
		"[1,\n2]", `1.0e1000000`, `-1e-99999`, `123456789012345678901234567890`,
	}
	for _, c := range cases {
		b := []byte(c)
		got := odjsonrt.Validate(b)
		want := json.Unmarshal(b, new(json.RawMessage))
		if (got != nil) != (want != nil) {
			t.Errorf("Validate(%q) = %v, encoding/json = %v", c, got, want)
		}
	}
}

func TestEndOfDocument(t *testing.T) {
	if err := odjsonrt.EndOfDocument([]byte(`1  `), 1); err != nil {
		t.Errorf("EndOfDocument: %v", err)
	}
	if err := odjsonrt.EndOfDocument([]byte(`1 x`), 1); err == nil {
		t.Error("EndOfDocument = nil, want error")
	}
}

func TestDepthLimit(t *testing.T) {
	for _, n := range []int{1, 128, 129, 9999, 10000, 10001, 100000} {
		arr := []byte(strings.Repeat("[", n) + strings.Repeat("]", n))
		obj := []byte(strings.Repeat(`{"a":`, n) + "1" + strings.Repeat("}", n))
		for _, b := range [][]byte{arr, obj} {
			want := json.Unmarshal(b, new(json.RawMessage)) != nil
			if got := odjsonrt.Validate(b) != nil; got != want {
				t.Errorf("depth %d: Validate rejected=%v, encoding/json rejected=%v", n, got, want)
			}
			var v any
			wantAny := json.Unmarshal(b, &v) != nil
			_, gotAnyErr := decodeDocument(b)
			if got := gotAnyErr != nil; got != wantAny {
				t.Errorf("depth %d: ParseAny rejected=%v, encoding/json rejected=%v", n, got, wantAny)
			}
		}
	}
	// The limit itself must be the documented one.
	if odjsonrt.MaxDepth != 10000 {
		t.Errorf("MaxDepth = %d, want 10000", odjsonrt.MaxDepth)
	}
}

func TestParseString(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		aliased bool
	}{
		{`""`, "", true},
		{`"abc"`, "abc", true},
		{`"a\"b"`, `a"b`, false},
		{`"a\\b"`, `a\b`, false},
		{`"a\/b"`, "a/b", false},
		{`"\b\f\n\r\t"`, "\b\f\n\r\t", false},
		{"\"\\u0041\"", "A", false},
		{`"é"`, "\xc3\xa9", true},
		{`"😀"`, "\xf0\x9f\x98\x80", true},
		{`"\ud800"`, "\xef\xbf\xbd", false},
		{`"\ud800A"`, "\xef\xbf\xbdA", false},
		{`"\ud800A"`, "\xef\xbf\xbdA", false},
		{`"\udd1e\ud834"`, "\xef\xbf\xbd\xef\xbf\xbd", false},
		{`"\ud800\ud800\n"`, "\xef\xbf\xbd\xef\xbf\xbd\n", false},
		{"\"\xe6\x97\xa5\"", "\xe6\x97\xa5", true},
		{"\"a\xffb\"", "a\xef\xbf\xbdb", false},
		{"\"\xed\xa0\x80\"", "\xef\xbf\xbd\xef\xbf\xbd\xef\xbf\xbd", false},
	}
	for _, c := range cases {
		t.Run(strconv.Quote(c.in), func(t *testing.T) {
			got, next, err := odjsonrt.ParseString([]byte(c.in), 0)
			if err != nil {
				t.Fatalf("ParseString: %v", err)
			}
			if got != c.want {
				t.Errorf("ParseString = %q, want %q", got, c.want)
			}
			if next != len(c.in) {
				t.Errorf("next = %d, want %d", next, len(c.in))
			}

			// The standard library is the authority.
			var oracle string
			if err := json.Unmarshal([]byte(c.in), &oracle); err != nil {
				t.Fatalf("oracle: %v", err)
			}
			if got != oracle {
				t.Errorf("ParseString = %q, encoding/json = %q", got, oracle)
			}

			_, aliased, _, err := odjsonrt.ParseStringBytes([]byte(c.in), 0)
			if err != nil {
				t.Fatalf("ParseStringBytes: %v", err)
			}
			if aliased != c.aliased {
				t.Errorf("aliased = %v, want %v", aliased, c.aliased)
			}
		})
	}
}

func TestParseStringBytesAliasing(t *testing.T) {
	data := []byte(`"abc"`)
	s, aliased, _, err := odjsonrt.ParseStringBytes(data, 0)
	if err != nil || !aliased {
		t.Fatalf("ParseStringBytes: %v, aliased=%v", err, aliased)
	}
	data[1] = 'z'
	if string(s) != "zbc" {
		t.Errorf("expected the result to alias data, got %q", s)
	}
}

func TestParseStringErrors(t *testing.T) {
	for _, c := range []string{``, `"`, `"abc`, "\"a\x01b\"", `"\q"`, `"\u12"`, `"\u123g"`, `"\`, `1`, `null`, `[`} {
		if _, _, err := odjsonrt.ParseString([]byte(c), 0); err == nil {
			t.Errorf("ParseString(%q) = nil error, want error", c)
		}
	}
	// A non-string value must be reported as a type error.
	_, _, err := odjsonrt.ParseString([]byte(`123`), 0)
	var te *odjsonrt.TypeError
	if !errors.As(err, &te) {
		t.Fatalf("got %T (%v), want *odjsonrt.TypeError", err, err)
	}
	if te.Value != "number" || te.Type != "string" {
		t.Errorf("TypeError = %+v", te)
	}
}

func TestParseStringInner(t *testing.T) {
	inner, next, err := odjsonrt.ParseStringInner([]byte(`"{\"a\":1}"`), 0)
	if err != nil {
		t.Fatalf("ParseStringInner: %v", err)
	}
	if string(inner) != `{"a":1}` {
		t.Errorf("inner = %q", inner)
	}
	if next != 11 {
		t.Errorf("next = %d, want 11", next)
	}
	if err := odjsonrt.Validate(inner); err != nil {
		t.Errorf("inner is not valid JSON: %v", err)
	}
}

func TestParseKey(t *testing.T) {
	data := []byte(`{ "key" : 42 }`)
	key, _, next, err := odjsonrt.ParseKey(data, 2)
	if err != nil {
		t.Fatalf("ParseKey: %v", err)
	}
	if string(key) != "key" {
		t.Errorf("key = %q", key)
	}
	if data[next] != '4' {
		t.Errorf("next points at %q", data[next])
	}
	for _, c := range []string{`x:1`, `"a" 1`, `"a"`, `"a`, ``} {
		if _, _, _, err := odjsonrt.ParseKey([]byte(c), 0); err == nil {
			t.Errorf("ParseKey(%q) = nil error, want error", c)
		}
	}
}

func TestParseInt(t *testing.T) {
	lits := []string{
		"0", "-0", "1", "-1", "127", "128", "-128", "-129", "32767", "32768",
		"2147483647", "2147483648", "9223372036854775807", "9223372036854775808",
		"-9223372036854775808", "-9223372036854775809", "1.0", "1e2", "1.5",
		"123456789012345678901234567890",
	}
	for _, lit := range lits {
		for _, bits := range []int{8, 16, 32, 64} {
			got, next, err := odjsonrt.ParseInt([]byte(lit), 0, bits)

			var wantErr error
			var want int64
			switch bits {
			case 8:
				var v int8
				wantErr = json.Unmarshal([]byte(lit), &v)
				want = int64(v)
			case 16:
				var v int16
				wantErr = json.Unmarshal([]byte(lit), &v)
				want = int64(v)
			case 32:
				var v int32
				wantErr = json.Unmarshal([]byte(lit), &v)
				want = int64(v)
			default:
				var v int64
				wantErr = json.Unmarshal([]byte(lit), &v)
				want = v
			}
			if (err != nil) != (wantErr != nil) {
				t.Errorf("ParseInt(%q, %d) err = %v, encoding/json = %v", lit, bits, err, wantErr)
				continue
			}
			if err != nil {
				continue
			}
			if got != want {
				t.Errorf("ParseInt(%q, %d) = %d, encoding/json = %d", lit, bits, got, want)
			}
			if next != len(lit) {
				t.Errorf("ParseInt(%q, %d) next = %d, want %d", lit, bits, next, len(lit))
			}
		}
	}
	// Non-numbers are type errors, malformed numbers are syntax errors.
	var te *odjsonrt.TypeError
	if _, _, err := odjsonrt.ParseInt([]byte(`"1"`), 0, 64); !errors.As(err, &te) {
		t.Errorf("got %T, want *TypeError", err)
	}
	// A leading zero terminates the literal: the caller is responsible for
	// rejecting whatever follows, exactly as SkipValue does.
	if v, next, err := odjsonrt.ParseInt([]byte(`01`), 0, 64); err != nil || v != 0 || next != 1 {
		t.Errorf("ParseInt(01) = %d, %d, %v; want 0, 1, nil", v, next, err)
	}
	var se *odjsonrt.SyntaxError
	if _, _, err := odjsonrt.ParseInt([]byte(`1.`), 0, 64); !errors.As(err, &se) {
		t.Errorf("ParseInt(1.) = %T, want *SyntaxError", err)
	}
	if _, _, err := odjsonrt.ParseInt([]byte(`-`), 0, 64); !errors.As(err, &se) {
		t.Errorf("got %T, want *SyntaxError", err)
	}
}

func TestParseUint(t *testing.T) {
	lits := []string{"0", "-0", "1", "-1", "255", "256", "65535", "65536",
		"4294967295", "4294967296", "18446744073709551615", "18446744073709551616", "1.0"}
	for _, lit := range lits {
		for _, bits := range []int{8, 16, 32, 64} {
			got, _, err := odjsonrt.ParseUint([]byte(lit), 0, bits)

			var wantErr error
			var want uint64
			switch bits {
			case 8:
				var v uint8
				wantErr = json.Unmarshal([]byte(lit), &v)
				want = uint64(v)
			case 16:
				var v uint16
				wantErr = json.Unmarshal([]byte(lit), &v)
				want = uint64(v)
			case 32:
				var v uint32
				wantErr = json.Unmarshal([]byte(lit), &v)
				want = uint64(v)
			default:
				var v uint64
				wantErr = json.Unmarshal([]byte(lit), &v)
				want = v
			}
			if (err != nil) != (wantErr != nil) {
				t.Errorf("ParseUint(%q, %d) err = %v, encoding/json = %v", lit, bits, err, wantErr)
				continue
			}
			if err == nil && got != want {
				t.Errorf("ParseUint(%q, %d) = %d, encoding/json = %d", lit, bits, got, want)
			}
		}
	}
}

func TestParseFloat(t *testing.T) {
	lits := []string{"0", "-0", "1", "-1", "1.5", "1e2", "1e-2", "-1.5e-10",
		"3.4028235e38", "3.4028236e38", "1e39", "1.7976931348623157e308",
		"1.8e308", "1e-400", "5e-324", "123456789012345678901234567890"}
	for _, lit := range lits {
		for _, bits := range []int{32, 64} {
			got, _, err := odjsonrt.ParseFloat([]byte(lit), 0, bits)

			var wantErr error
			var want float64
			if bits == 32 {
				var v float32
				wantErr = json.Unmarshal([]byte(lit), &v)
				want = float64(v)
			} else {
				var v float64
				wantErr = json.Unmarshal([]byte(lit), &v)
				want = v
			}
			if (err != nil) != (wantErr != nil) {
				t.Errorf("ParseFloat(%q, %d) err = %v, encoding/json = %v", lit, bits, err, wantErr)
				continue
			}
			if err == nil && got != want && !(math.IsNaN(got) && math.IsNaN(want)) {
				t.Errorf("ParseFloat(%q, %d) = %v, encoding/json = %v", lit, bits, got, want)
			}
		}
	}
}

func TestParseNumberString(t *testing.T) {
	got, next, err := odjsonrt.ParseNumberString([]byte(`-1.5e+10,`), 0)
	if err != nil {
		t.Fatalf("ParseNumberString: %v", err)
	}
	if got != "-1.5e+10" || next != 8 {
		t.Errorf("got %q, %d", got, next)
	}
	// encoding/json also accepts a JSON string whose content is a number.
	type numbered struct {
		N json.Number `json:"n"`
	}
	strCases := []string{`"123"`, `"1e5"`, `"-1.5"`, `"abc"`, `""`, `"01"`, `" 1"`, `"1 "`, `true`, `[1]`}
	for _, c := range strCases {
		got, _, err := odjsonrt.ParseNumberString([]byte(c), 0)
		var v numbered
		wantErr := json.Unmarshal([]byte(`{"n":`+c+`}`), &v)
		if (err != nil) != (wantErr != nil) {
			t.Errorf("ParseNumberString(%s) err = %v, encoding/json = %v", c, err, wantErr)
			continue
		}
		if err == nil && got != string(v.N) {
			t.Errorf("ParseNumberString(%s) = %q, encoding/json = %q", c, got, v.N)
		}
	}
}

func TestParseBool(t *testing.T) {
	if v, next, err := odjsonrt.ParseBool([]byte(`true`), 0); err != nil || !v || next != 4 {
		t.Errorf("got %v, %d, %v", v, next, err)
	}
	if v, next, err := odjsonrt.ParseBool([]byte(`false`), 0); err != nil || v || next != 5 {
		t.Errorf("got %v, %d, %v", v, next, err)
	}
	for _, c := range []string{`tru`, `fals`, ``, `1`, `null`, `"true"`} {
		if _, _, err := odjsonrt.ParseBool([]byte(c), 0); err == nil {
			t.Errorf("ParseBool(%q) = nil error, want error", c)
		}
	}
}

func TestParseNull(t *testing.T) {
	if next, ok := odjsonrt.ParseNull([]byte(`null`), 0); !ok || next != 4 {
		t.Errorf("got %d, %v", next, ok)
	}
	for _, c := range []string{`nul`, `1`, ``, `"null"`} {
		if next, ok := odjsonrt.ParseNull([]byte(c), 0); ok || next != 0 {
			t.Errorf("ParseNull(%q) = %d, %v", c, next, ok)
		}
	}
}

func TestParseRaw(t *testing.T) {
	data := []byte(`[{"a": [1, 2]}, 3]`)
	raw, next, err := odjsonrt.ParseRaw(data, 1)
	if err != nil {
		t.Fatalf("ParseRaw: %v", err)
	}
	if string(raw) != `{"a": [1, 2]}` {
		t.Errorf("raw = %q", raw)
	}
	if next != 14 {
		t.Errorf("next = %d, want 14", next)
	}
	if _, _, err := odjsonrt.ParseRaw([]byte(`[1`), 0); err == nil {
		t.Error("ParseRaw on a truncated value = nil error")
	}
}

func TestParseBase64(t *testing.T) {
	for _, c := range []string{`""`, `"AQID"`, `"aGVsbG8gd29ybGQ="`} {
		got, _, err := odjsonrt.ParseBase64([]byte(c), 0)
		if err != nil {
			t.Fatalf("ParseBase64(%q): %v", c, err)
		}
		var want []byte
		if err := json.Unmarshal([]byte(c), &want); err != nil {
			t.Fatalf("oracle: %v", err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("ParseBase64(%q) = %v, want %v", c, got, want)
		}
	}
	if _, _, err := odjsonrt.ParseBase64([]byte(`"!!!"`), 0); err == nil {
		t.Error("ParseBase64 on invalid base64 = nil error")
	}
	if _, _, err := odjsonrt.ParseBase64([]byte(`123`), 0); err == nil {
		t.Error("ParseBase64 on a number = nil error")
	}
}

// collector records the bytes handed to UnmarshalJSON / UnmarshalText.
type collector struct {
	json []byte
	text []byte
	err  error
}

func (c *collector) UnmarshalJSON(b []byte) error {
	c.json = append([]byte(nil), b...)
	return c.err
}

func (c *collector) UnmarshalText(b []byte) error {
	c.text = append([]byte(nil), b...)
	return c.err
}

func TestParseUnmarshaler(t *testing.T) {
	var c collector
	next, err := odjsonrt.ParseUnmarshaler([]byte(`[ {"a":1} , 2]`), 2, &c)
	if err != nil {
		t.Fatalf("ParseUnmarshaler: %v", err)
	}
	if string(c.json) != `{"a":1}` {
		t.Errorf("json = %q", c.json)
	}
	if next != 9 {
		t.Errorf("next = %d, want 9", next)
	}
	sentinel := errors.New("boom")
	if _, err := odjsonrt.ParseUnmarshaler([]byte(`1`), 0, &collector{err: sentinel}); !errors.Is(err, sentinel) {
		t.Errorf("got %v, want %v", err, sentinel)
	}
	if _, err := odjsonrt.ParseUnmarshaler([]byte(`[`), 0, &collector{}); err == nil {
		t.Error("ParseUnmarshaler on invalid JSON = nil error")
	}
}

func TestParseTextUnmarshaler(t *testing.T) {
	var c collector
	next, err := odjsonrt.ParseTextUnmarshaler([]byte(`"a\nb"`), 0, &c)
	if err != nil {
		t.Fatalf("ParseTextUnmarshaler: %v", err)
	}
	if string(c.text) != "a\nb" {
		t.Errorf("text = %q", c.text)
	}
	if next != 6 {
		t.Errorf("next = %d, want 6", next)
	}
	if _, err := odjsonrt.ParseTextUnmarshaler([]byte(`1`), 0, &collector{}); err == nil {
		t.Error("ParseTextUnmarshaler on a number = nil error")
	}
	sentinel := errors.New("boom")
	if _, err := odjsonrt.ParseTextUnmarshaler([]byte(`"x"`), 0, &collector{err: sentinel}); !errors.Is(err, sentinel) {
		t.Errorf("got %v, want %v", err, sentinel)
	}
}

func TestParseInto(t *testing.T) {
	var v struct {
		A int      `json:"a"`
		B []string `json:"b"`
	}
	next, err := odjsonrt.ParseInto([]byte(`  {"a":1,"b":["x"]} `), 2, &v)
	if err != nil {
		t.Fatalf("ParseInto: %v", err)
	}
	if v.A != 1 || !reflect.DeepEqual(v.B, []string{"x"}) {
		t.Errorf("v = %+v", v)
	}
	if next != 19 {
		t.Errorf("next = %d, want 19", next)
	}
	if _, err := odjsonrt.ParseInto([]byte(`"x"`), 0, &v); err == nil {
		t.Error("ParseInto with a mismatched type = nil error")
	}
}

func TestParseAny(t *testing.T) {
	cases := []string{
		`null`, `true`, `false`, `0`, `-1.5e2`, `"a\nb"`, `[]`, `{}`,
		`[1,"two",null,true,[],{}]`,
		`{"a":1,"b":{"c":[1,2,3]},"d":null}`,
		`{"a":1,"a":2}`,
		`  [ 1 , 2 ]  `,
		"\"\xff\"",
	}
	for _, c := range cases {
		got, err := decodeDocument([]byte(c))
		var want any
		wantErr := json.Unmarshal([]byte(c), &want)
		if (err != nil) != (wantErr != nil) {
			t.Errorf("ParseAny(%q) err = %v, encoding/json = %v", c, err, wantErr)
			continue
		}
		if err == nil && !reflect.DeepEqual(got, want) {
			t.Errorf("ParseAny(%q) = %#v, encoding/json = %#v", c, got, want)
		}
	}
	// Empty containers must be non-nil, like encoding/json.
	v, err := decodeDocument([]byte(`[]`))
	if err != nil {
		t.Fatal(err)
	}
	if s, ok := v.([]any); !ok || s == nil {
		t.Errorf("[] decoded to %#v", v)
	}
	v, err = decodeDocument([]byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if m, ok := v.(map[string]any); !ok || m == nil {
		t.Errorf("{} decoded to %#v", v)
	}
}

func TestEqualFold(t *testing.T) {
	cases := []struct {
		key, name string
	}{
		{"", ""},
		{"a", "a"},
		{"A", "a"},
		{"aBc", "AbC"},
		{"abc", "abd"},
		{"abc", "ab"},
		{"ab", "abc"},
		{"k", "K"},
		{"\xe2\x84\xaa", "k"}, // KELVIN SIGN folds to k
		{"\xc3\x9f", "\xc3\x9f"},
		{"\xc3\xa9", "\xc3\x89"},
		{"\xff", "\xff"},
		{"\xff", "a"},
		{"_", "-"},
		{"123", "123"},
	}
	for _, c := range cases {
		got := odjsonrt.EqualFold([]byte(c.key), c.name)
		want := bytes.EqualFold([]byte(c.key), []byte(c.name))
		if got != want {
			t.Errorf("EqualFold(%q, %q) = %v, bytes.EqualFold = %v", c.key, c.name, got, want)
		}
	}
}

func TestErrors(t *testing.T) {
	se := &odjsonrt.SyntaxError{Msg: "bad", Offset: 3}
	if se.Error() != "bad" {
		t.Errorf("SyntaxError.Error = %q", se.Error())
	}
	te := &odjsonrt.TypeError{Value: "string", Type: "int"}
	if want := "json: cannot unmarshal string into Go value of type int"; te.Error() != want {
		t.Errorf("TypeError.Error = %q, want %q", te.Error(), want)
	}
	te.Field = "S"
	if want := "json: cannot unmarshal string into Go struct field S of type int"; te.Error() != want {
		t.Errorf("TypeError.Error = %q, want %q", te.Error(), want)
	}

	if err := odjsonrt.ErrSyntax([]byte(`x`), 0, "boom"); err.Error() != "boom" {
		t.Errorf("ErrSyntax = %q", err.Error())
	}
	kinds := map[string]string{
		`"a"`: "string", `{`: "object", `[`: "array", `true`: "bool",
		`false`: "bool", `null`: "null", `1`: "number", `-1`: "number",
	}
	for in, kind := range kinds {
		err := odjsonrt.ErrType([]byte(in), 0, "T")
		var typeErr *odjsonrt.TypeError
		if !errors.As(err, &typeErr) {
			t.Errorf("ErrType(%q) = %T, want *TypeError", in, err)
			continue
		}
		if typeErr.Value != kind {
			t.Errorf("ErrType(%q).Value = %q, want %q", in, typeErr.Value, kind)
		}
	}
	// A byte that cannot start a value yields a syntax error instead.
	var syntaxErr *odjsonrt.SyntaxError
	if err := odjsonrt.ErrType([]byte(`x`), 0, "T"); !errors.As(err, &syntaxErr) {
		t.Errorf("ErrType(x) = %T, want *SyntaxError", err)
	}
	if err := odjsonrt.ErrTypeField([]byte(`1`), 0, "T", "F"); err.(*odjsonrt.TypeError).Field != "F" {
		t.Errorf("ErrTypeField dropped the field name")
	}

	var unknown *odjsonrt.UnknownFieldError
	err := odjsonrt.ErrUnknownField("oops")
	if !errors.As(err, &unknown) || unknown.Field != "oops" {
		t.Errorf("ErrUnknownField = %v", err)
	}
	if want := `json: unknown field "oops"`; err.Error() != want {
		t.Errorf("ErrUnknownField.Error = %q, want %q", err.Error(), want)
	}
}

// FuzzParity checks that Validate and ParseAny agree with encoding/json on
// arbitrary input.
func FuzzParity(f *testing.F) {
	seeds := []string{
		``, `null`, `{}`, `[]`, `1`, `"a"`, `[1,2,{"a":null}]`, `{"a":`,
		`"\ud800"`, "\xff", `[[[[[]]]]]`, "  \t\n1", `1e1000`,
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		gotValidate := odjsonrt.Validate(b) != nil
		wantValidate := json.Unmarshal(b, new(json.RawMessage)) != nil
		if gotValidate != wantValidate {
			t.Fatalf("Validate rejected=%v, encoding/json rejected=%v for %q", gotValidate, wantValidate, b)
		}

		got, gotErr := decodeDocument(b)
		var want any
		wantErr := json.Unmarshal(b, &want)
		if (gotErr != nil) != (wantErr != nil) {
			t.Fatalf("ParseAny err=%v, encoding/json err=%v for %q", gotErr, wantErr, b)
		}
		if gotErr == nil && !reflect.DeepEqual(got, want) {
			t.Fatalf("ParseAny = %#v, encoding/json = %#v for %q", got, want, b)
		}
	})
}

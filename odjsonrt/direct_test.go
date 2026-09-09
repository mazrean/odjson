package odjsonrt

import (
	"bytes"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"strings"
	"testing"
)

// directT takes the direct path when it is offered and records which path
// it took. The public API path is what the generated code falls back to.
type directT struct {
	direct bool
	value  string
}

func (t *directT) MarshalJSONTo(enc *jsontext.Encoder) error {
	if buf, ok := BeginDirectEncode(enc); ok {
		t.direct = true
		out, err := AppendStringChecked(buf, t.value, ModeV2)
		if err != nil {
			return err
		}
		EndDirectEncode(enc, out)
		return nil
	}
	t.direct = false
	return enc.WriteValue(AppendStringMode(nil, t.value, ModeStream))
}

func (t *directT) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	if data, ok := BeginDirectDecode(dec); ok {
		t.direct = true
		p := SkipSpace(data, 0)
		s, end, err := ParseStringStrict(data, p, nil)
		if err != nil {
			return err
		}
		t.value = s
		EndDirectDecode(dec, end)
		return nil
	}
	t.direct = false
	v, err := dec.ReadValue()
	if err != nil {
		return err
	}
	t.value, err = ParseStringValue(v, nil)
	return err
}

func TestDirectPathIsTakenOnlyAtTopLevel(t *testing.T) {
	if !DirectEnabled() {
		t.Skip("direct path disabled")
	}
	v := &directT{value: "héllo \"world\""}

	out, err := jsonv2.Marshal(v)
	if err != nil || string(out) != `"héllo \"world\""` || !v.direct {
		t.Fatalf("top-level Marshal = %s, %v, direct=%v", out, err, v.direct)
	}
	out, err = jsonv2.Marshal([]*directT{v})
	if err != nil || string(out) != `["héllo \"world\""]` || v.direct {
		t.Fatalf("nested Marshal = %s, %v, direct=%v", out, err, v.direct)
	}
	var w bytes.Buffer
	if err := jsonv2.MarshalWrite(&w, v); err != nil || w.String() != `"héllo \"world\""` || v.direct {
		t.Fatalf("MarshalWrite = %q, %v, direct=%v", w.String(), err, v.direct)
	}
	out, err = jsonv2.Marshal(v, jsontext.EscapeForHTML(true))
	if err != nil || v.direct {
		t.Fatalf("Marshal with options = %s, %v, direct=%v", out, err, v.direct)
	}

	var d directT
	if err := jsonv2.Unmarshal([]byte(`  "aéb"  `), &d); err != nil || d.value != "aéb" || !d.direct {
		t.Fatalf("top-level Unmarshal = %q, %v, direct=%v", d.value, err, d.direct)
	}
	var ds []*directT
	if err := jsonv2.Unmarshal([]byte(`["x"]`), &ds); err != nil || len(ds) != 1 || ds[0].value != "x" || ds[0].direct {
		t.Fatalf("nested Unmarshal = %v, %v", ds, err)
	}
	if err := jsonv2.UnmarshalRead(strings.NewReader(`"x"`), &d); err != nil || d.value != "x" || d.direct {
		t.Fatalf("UnmarshalRead = %q, %v, direct=%v", d.value, err, d.direct)
	}
	if err := jsonv2.Unmarshal([]byte(`"x"`), &d, jsontext.AllowInvalidUTF8(true)); err != nil || d.direct {
		t.Fatalf("Unmarshal with options = %v, direct=%v", err, d.direct)
	}
}

func TestDirectPathKeepsJSONV2Checks(t *testing.T) {
	if !DirectEnabled() {
		t.Skip("direct path disabled")
	}
	var d directT
	for _, doc := range []string{`"x" y`, `"x" "y"`, `"x`, `"\xff"`, `"\ud800"`, ``} {
		if err := jsonv2.Unmarshal([]byte(doc), &d); err == nil {
			t.Errorf("Unmarshal(%q) accepted", doc)
		}
	}
	if _, err := jsonv2.Marshal(&directT{value: "a\xffb"}); err == nil {
		t.Error("Marshal of invalid UTF-8 accepted")
	}
	// A decoder used for a stream of values takes the direct path only for
	// the first one, and the rest still decode correctly.
	dec := jsontext.NewDecoder(bytes.NewBuffer([]byte(`"a" "b" "c"`)))
	dec.PeekKind()
	var got []string
	for i := range 3 {
		if err := jsonv2.UnmarshalDecode(dec, &d); err != nil {
			t.Fatalf("value %d: %v", i, err)
		}
		got = append(got, d.value)
	}
	if strings.Join(got, "") != "abc" {
		t.Errorf("stream decoded to %q", got)
	}
}

func TestSkipValueStrict(t *testing.T) {
	for _, tc := range []struct {
		doc string
		ok  bool
	}{
		{`{"a":1,"b":2}`, true},
		{`{"a":1,"a":2}`, false},
		{`{"a":{"b":1},"c":{"b":2}}`, true},
		{`[{"a":1},{"a":2}]`, true},
		{`{"a":[{"b":1,"b":2}]}`, false},
		{`{"a":1,"a":2}`, false},
		{"{\"a\":\"\xff\"}", false},
		{`{"a":"\ud800"}`, false},
		{`{"a":"😀"}`, true},
		{`{}`, true},
		{`[]`, true},
		{`{"a":1,`, false},
	} {
		end, err := SkipValueStrict([]byte(tc.doc), 0)
		if (err == nil) != tc.ok {
			t.Errorf("SkipValueStrict(%s) = %d, %v; want ok=%v", tc.doc, end, err, tc.ok)
			continue
		}
		if tc.ok && end != len(tc.doc) {
			t.Errorf("SkipValueStrict(%s) stopped at %d", tc.doc, end)
		}
	}
}

func TestAppendStringChecked(t *testing.T) {
	for _, tc := range []struct {
		in   string
		mode StringMode
		want string
		ok   bool
	}{
		{"plain", ModeV2, `"plain"`, true},
		{"héllo   <b>&", ModeV2, "\"héllo   <b>&\"", true},
		{"a\xffb", ModeV2, "", false},
		{"a\xffb", ModeStream, "\"a\xffb\"", true},
		{"a\xffb", ModeHTML, `"a�b"`, true},
		{strings.Repeat("x", 40) + "é", ModeV2, `"` + strings.Repeat("x", 40) + `é"`, true},
		{strings.Repeat("x", 40) + "\xe9", ModeV2, "", false},
	} {
		got, err := AppendStringChecked(nil, tc.in, tc.mode)
		if (err == nil) != tc.ok || (tc.ok && string(got) != tc.want) {
			t.Errorf("AppendStringChecked(%q, %d) = %q, %v; want %q, ok=%v", tc.in, tc.mode, got, err, tc.want, tc.ok)
		}
	}
}

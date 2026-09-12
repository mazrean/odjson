package odjsonrt

import (
	"bytes"
	jsonv1 "encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"strings"
	"testing"
)

// directT takes the direct path when it is offered and records which path
// it took, and in which mode. The public API path is what the generated code
// falls back to.
type directT struct {
	direct bool
	mode   StringMode
	value  string
}

func (t *directT) MarshalJSONTo(enc *jsontext.Encoder) error {
	if buf, m, ok := BeginDirectEncodeMode(enc); ok {
		t.direct, t.mode = true, m
		out, err := AppendStringChecked(buf, t.value, m)
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
	if data, p, ok := BeginDirectDecodeAt(dec); ok {
		t.direct = true
		p = SkipSpace(data, p)
		s, end, err := ParseStringStrict(data, p, nil)
		if err != nil {
			return err
		}
		t.value = s
		EndDirectDecodeAt(dec, p, end)
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

// legacyT uses the entry points generated code before odjson 0.3 calls,
// which offer the direct path for a top-level value only.
type legacyT struct {
	direct bool
	value  string
}

func (t *legacyT) MarshalJSONTo(enc *jsontext.Encoder) error {
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

func (t *legacyT) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
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

func TestDirectPathIsTakenAtEveryDepth(t *testing.T) {
	if !DirectEnabled() {
		t.Skip("direct path disabled")
	}
	v := &directT{value: "héllo \"world\" <&>"}
	const want = `"héllo \"world\" <&>"`

	out, err := jsonv2.Marshal(v)
	if err != nil || string(out) != want || !v.direct || v.mode != ModeV2 {
		t.Fatalf("top-level Marshal = %s, %v, direct=%v mode=%v", out, err, v.direct, v.mode)
	}
	type outer struct {
		A []*directT          `json:"a"`
		M map[string]*directT `json:"m"`
		V directT             `json:"v"`
		P *directT            `json:"p,omitempty"`
	}
	// Every position below the top level takes it too, and the output is
	// what the public path writes.
	o := outer{A: []*directT{v, v}, M: map[string]*directT{"k": v}, V: *v}
	var w bytes.Buffer
	if err := jsonv2.MarshalWrite(&w, o); err != nil {
		t.Fatal(err)
	}
	out, err = jsonv2.Marshal(o)
	if err != nil || string(out) != w.String() {
		t.Fatalf("nested Marshal = %s, %v; MarshalWrite = %s", out, err, w.String())
	}
	if !v.direct || !o.V.direct {
		t.Errorf("nested Marshal: element direct=%v, field direct=%v", v.direct, o.V.direct)
	}
	// Any writer, and any option, keep it off.
	w.Reset()
	if err := jsonv2.MarshalWrite(&w, v); err != nil || w.String() != want || v.direct {
		t.Fatalf("MarshalWrite = %q, %v, direct=%v", w.String(), err, v.direct)
	}
	out, err = jsonv2.Marshal(v, jsontext.EscapeForHTML(true))
	if err != nil || v.direct {
		t.Fatalf("Marshal with options = %s, %v, direct=%v", out, err, v.direct)
	}

	// encoding/json's Marshal takes it in ModeV2HTML and writes what its
	// own streaming encoder, which never takes it, writes.
	w.Reset()
	if err := jsonv1.NewEncoder(&w).Encode(o); err != nil || v.direct {
		t.Fatalf("v1 Encode = %v, direct=%v", err, v.direct)
	}
	out, err = jsonv1.Marshal(o)
	if err != nil || string(out)+"\n" != w.String() || !v.direct || v.mode != ModeV2HTML {
		t.Fatalf("v1 Marshal = %s, %v, direct=%v mode=%v; Encode = %s", out, err, v.direct, v.mode, w.String())
	}
	if !strings.Contains(string(out), `\u003c\u0026\u003e`) {
		t.Errorf("v1 Marshal did not escape HTML: %s", out)
	}

	var d directT
	if err := jsonv2.Unmarshal([]byte(`  "aéb"  `), &d); err != nil || d.value != "aéb" || !d.direct {
		t.Fatalf("top-level Unmarshal = %q, %v, direct=%v", d.value, err, d.direct)
	}
	var got outer
	doc := `{"a" : [ "x" , "y" ] , "m" : { "k" : "z" } , "v" : "w" , "p" : "q" , "s" : "\"r\"" }`
	if err := jsonv2.Unmarshal([]byte(doc), &got); err != nil {
		t.Fatalf("nested Unmarshal: %v", err)
	}
	if len(got.A) != 2 || got.A[0].value != "x" || got.A[1].value != "y" || got.M["k"].value != "z" ||
		got.V.value != "w" || got.P.value != "q" {
		t.Fatalf("nested Unmarshal decoded %+v", got)
	}
	if !got.A[0].direct || !got.A[1].direct || !got.M["k"].direct || !got.V.direct || !got.P.direct {
		t.Errorf("nested Unmarshal paths: %+v", got)
	}
	var ref outer
	if err := jsonv2.UnmarshalRead(strings.NewReader(doc), &ref); err != nil || ref.V.direct {
		t.Fatalf("UnmarshalRead = %v, direct=%v", err, ref.V.direct)
	}
	if err := jsonv2.Unmarshal([]byte(`"x"`), &d, jsontext.AllowInvalidUTF8(true)); err != nil || d.direct {
		t.Fatalf("Unmarshal with options = %v, direct=%v", err, d.direct)
	}
	// encoding/json's Unmarshal allows what the strict parsers refuse, so
	// it stays on the public path.
	if err := jsonv1.Unmarshal([]byte(`["x"]`), &got.A); err != nil || got.A[0].direct {
		t.Fatalf("v1 Unmarshal = %v, direct=%v", err, got.A[0].direct)
	}

	// The pre-0.3 entry points still offer it for a top-level value only.
	l := &legacyT{value: "x"}
	if out, err := jsonv2.Marshal(l); err != nil || string(out) != `"x"` || !l.direct {
		t.Fatalf("legacy top-level Marshal = %s, %v, direct=%v", out, err, l.direct)
	}
	if out, err := jsonv2.Marshal([]*legacyT{l}); err != nil || string(out) != `["x"]` || l.direct {
		t.Fatalf("legacy nested Marshal = %s, %v, direct=%v", out, err, l.direct)
	}
	if err := jsonv2.Unmarshal([]byte(` "y" `), l); err != nil || l.value != "y" || !l.direct {
		t.Fatalf("legacy top-level Unmarshal = %q, %v, direct=%v", l.value, err, l.direct)
	}
	var ls []*legacyT
	if err := jsonv2.Unmarshal([]byte(`["z"]`), &ls); err != nil || ls[0].value != "z" || ls[0].direct {
		t.Fatalf("legacy nested Unmarshal = %v, %v", ls, err)
	}
}

// TestDirectPathNestedErrors requires malformed input at a nested position to
// be refused the way the public path refuses it: the same acceptance, and the
// decoder left where the public path leaves it.
func TestDirectPathNestedErrors(t *testing.T) {
	if !DirectEnabled() {
		t.Skip("direct path disabled")
	}
	for _, doc := range []string{
		`["a" "b"]`, `["a",]`, `["a",,"b"]`, `{"k" "v"}`, `{"k":"v",}`, `["a`, `["\xff"]`, `["\ud800"]`,
		`{"k":"a","k":"b"}`, `[]`, `{}`, `["a"]`, `{"k":"v"}`, ` [ "a" , "b" ] `, `[1]`, `{"k":1}`, `[null]`, `{"k":null}`,
	} {
		var a []*directT
		var m map[string]*directT
		errA := jsonv2.Unmarshal([]byte(doc), &a)
		errM := jsonv2.Unmarshal([]byte(doc), &m)
		var ra []*string
		var rm map[string]*string
		wantA := jsonv2.Unmarshal([]byte(doc), &ra)
		wantM := jsonv2.Unmarshal([]byte(doc), &rm)
		if (errA != nil) != (wantA != nil) {
			t.Errorf("slice %s: got %v, reflection %v", doc, errA, wantA)
		}
		if (errM != nil) != (wantM != nil) {
			t.Errorf("map %s: got %v, reflection %v", doc, errM, wantM)
		}
		if errA == nil && wantA == nil && len(a) != len(ra) {
			t.Errorf("slice %s: decoded %d values, reflection %d", doc, len(a), len(ra))
		}
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

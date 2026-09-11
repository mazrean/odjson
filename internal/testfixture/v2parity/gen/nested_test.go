package gen_test

import (
	"bytes"
	jsonv1 "encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/mazrean/odjson/internal/testfixture/v2parity/gen"
	"github.com/mazrean/odjson/internal/testfixture/v2parity/plain"
	"github.com/mazrean/odjson/odjsonrt"
)

// The direct path (odjsonrt/direct.go) writes into and reads from the coder's
// own buffer for a value at any depth under a plain json.Marshal /
// json.Unmarshal, from encoding/json/v2 or encoding/json. The tests here pin
// it to the public API path, which the streaming entry points always take:
// the same bytes out of Marshal as out of MarshalWrite, the same value and
// the same acceptance out of Unmarshal as out of UnmarshalRead, with the
// generated type at every position json/v2 can reach it from.

// nest holds a Zoo everywhere a struct can: as a value, behind a pointer, in
// a slice, in a map, in an interface, and in a slice of a wrapper.
type nest[Z any] struct {
	V  Z             `json:"v"`
	P  *Z            `json:"p"`
	O  *Z            `json:"o,omitempty"`
	S  []Z           `json:"s"`
	M  map[string]Z  `json:"m"`
	A  any           `json:"a"`
	W  []wrap[Z]     `json:"w"`
	MP map[string]*Z `json:"mp"`
}

type wrap[Z any] struct {
	Z Z `json:"z"`
}

// nestedDocs are the documents the wrappers are decoded from: each Zoo
// document from parity_test.go at every position, plus whitespace and empty
// containers.
func nestedDocs() []string {
	var docs []string
	for _, d := range documents {
		// json/v2 writes a map held in an interface in iteration order on
		// every path, so a multi-member one would make the byte
		// comparisons below meaningless; one member is enough.
		d = strings.ReplaceAll(d, `"any":{"a":[1,2,{"b":null}],"c":true}`, `"any":{"a":[1,2,{"b":null}]}`)
		docs = append(docs,
			`{"v":`+d+`,"p":`+d+`,"s":[`+d+`,`+d+`],"m":{"a":`+d+`},"a":`+d+`,"w":[{"z":`+d+`}],"mp":{"k":`+d+`}}`,
			` { "v" : `+d+` , "p" : null , "o" : `+d+` , "s" : [ ] , "m" : { } , "a" : [ `+d+` ] , "w" : [ ] , "mp" : { "k" : null } } `,
		)
	}
	docs = append(docs,
		`{}`,
		`{"s":[`+filled+`,`+filled+`,`+filled+`],"m":{"x":`+filled+`}}`,
		`{"v":`+filled+`}`,
		`[`+filled+`]`,
	)
	return docs
}

// decodeBoth decodes doc into the generated and the plain wrapper the same
// way and returns the two results and errors.
func decodeBoth(doc string, decode func([]byte, any) error) (g nest[gen.Zoo], p nest[plain.Zoo], errG, errP error) {
	errG = decode([]byte(doc), &g)
	errP = decode([]byte(doc), &p)
	return
}

func TestNestedUnmarshalMatchesReadPath(t *testing.T) {
	if !odjsonrt.DirectEnabled() {
		t.Skip("direct path disabled")
	}
	for _, doc := range nestedDocs() {
		direct, directPlain, errD, errDP := decodeBoth(doc, func(b []byte, v any) error { return jsonv2.Unmarshal(b, v) })
		stream, _, errS, _ := decodeBoth(doc, func(b []byte, v any) error { return jsonv2.UnmarshalRead(bytes.NewReader(b), v) })
		if (errD != nil) != (errS != nil) || (errD != nil) != (errDP != nil) {
			t.Errorf("%s: acceptance: Unmarshal=%v UnmarshalRead=%v reflection=%v", doc, errD, errS, errDP)
			continue
		}
		if errD != nil {
			continue
		}
		a, err := jsonv2.Marshal(direct)
		if err != nil {
			t.Fatal(err)
		}
		b, err := jsonv2.Marshal(stream)
		if err != nil {
			t.Fatal(err)
		}
		c, err := jsonv2.Marshal(directPlain)
		if err != nil {
			t.Fatal(err)
		}
		if !equivalent(t, a, b) || !equivalent(t, a, c) {
			t.Errorf("%s:\n Unmarshal:     %s\n UnmarshalRead: %s\n reflection:    %s", doc, a, b, c)
		}
	}
}

func TestNestedMarshalMatchesWritePath(t *testing.T) {
	if !odjsonrt.DirectEnabled() {
		t.Skip("direct path disabled")
	}
	for _, doc := range nestedDocs() {
		g, p, errG, errP := decodeBoth(doc, func(b []byte, v any) error { return jsonv2.Unmarshal(b, v) })
		if errG != nil || errP != nil {
			continue
		}
		// The interface member decoded to a map, whose members json/v2
		// writes in iteration order; for the byte comparison it holds a
		// generated value instead, reached through the interface.
		g.A, p.A = g.V, p.V
		for _, val := range []any{g, &g, []nest[gen.Zoo]{g, g}, map[string]nest[gen.Zoo]{"n": g}} {
			direct, err := jsonv2.Marshal(val)
			if err != nil {
				t.Fatalf("%s: Marshal: %v", doc, err)
			}
			var w bytes.Buffer
			if err := jsonv2.MarshalWrite(&w, val); err != nil {
				t.Fatalf("%s: MarshalWrite: %v", doc, err)
			}
			if !bytes.Equal(direct, w.Bytes()) {
				t.Errorf("%s: %T\n Marshal:      %s\n MarshalWrite: %s", doc, val, direct, w.Bytes())
			}
		}
		// And the same value as reflection sees it.
		direct, err := jsonv2.Marshal(g)
		if err != nil {
			t.Fatal(err)
		}
		ref, err := jsonv2.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		if !equivalent(t, direct, ref) {
			t.Errorf("%s:\n odjson:     %s\n reflection: %s", doc, direct, ref)
		}
	}
}

// TestV1MarshalMatchesEncoder pins what encoding/json's Marshal makes of the
// generated MarshalJSONTo, on the direct path in ModeV2HTML, to what its
// Encoder makes of it on the public path: byte for byte, at every depth, on
// the strings that path escapes (HTML, U+2028, U+2029) and on invalid UTF-8,
// which encoding/json allows.
func TestV1MarshalMatchesEncoder(t *testing.T) {
	if !odjsonrt.DirectEnabled() {
		t.Skip("direct path disabled")
	}
	docs := nestedDocs()
	docs = append(docs,
		`{"v":{"string":"<script>&amp;</script> \u2028 \u2029 é 日本 😀 \"q\" \\ \t \u0001","named":"<&>","strings":["<",">","&","\u2028x"],"str_map":{"<k>":"<v>"},"any":{"<a>":["<",{"b":"\u2029"}]},"raw":{"<x>":"<y>"}}}`,
	)
	for _, doc := range docs {
		var g nest[gen.Zoo]
		if err := jsonv2.Unmarshal([]byte(doc), &g); err != nil {
			continue
		}
		g.A = g.V
		vals := []any{g, []nest[gen.Zoo]{g}, map[string]nest[gen.Zoo]{"n": g}, g.V, &g.V}
		for _, val := range vals {
			direct, err := jsonv1.Marshal(val)
			if err != nil {
				t.Fatalf("%s: %T: Marshal: %v", doc, val, err)
			}
			var w bytes.Buffer
			if err := jsonv1.NewEncoder(&w).Encode(val); err != nil {
				t.Fatalf("%s: %T: Encode: %v", doc, val, err)
			}
			if want := strings.TrimSuffix(w.String(), "\n"); string(direct) != want {
				t.Errorf("%s: %T\n Marshal: %s\n Encode:  %s", doc, val, direct, want)
			}
		}
	}
	// Invalid UTF-8 cannot come in through a decode; set it directly.
	// The last two put the bad byte at the end of a long run, which is what
	// encoding/json lets through most often, once with a line separator in
	// the run that still has to be escaped.
	long := strings.Repeat("日本語の文字列", 1000)
	for _, s := range []string{"a\xffb", "\xe2\x80", "\u2028\xff\u2029", "<\xff>", "日本\xed\xa0\x80語", long + "\xff", long + "\u2028" + long + "\xe2\x80"} {
		z := gen.Zoo{String: s, Strings: []string{s}, StrMap: map[string]string{s: s}, Any: s}
		for _, val := range []any{z, []gen.Zoo{z}, nest[gen.Zoo]{V: z}} {
			direct, err := jsonv1.Marshal(val)
			if err != nil {
				t.Fatalf("%q: %T: Marshal: %v", s, val, err)
			}
			var w bytes.Buffer
			if err := jsonv1.NewEncoder(&w).Encode(val); err != nil {
				t.Fatalf("%q: %T: Encode: %v", s, val, err)
			}
			if want := strings.TrimSuffix(w.String(), "\n"); string(direct) != want {
				t.Errorf("%q: %T\n Marshal: %q\n Encode:  %q", s, val, direct, want)
			}
		}
	}
}

// TestNestedErrorsMatchReflection requires malformed input at a nested
// position to be refused whenever reflection refuses it, and accepted
// whenever reflection accepts it.
func TestNestedErrorsMatchReflection(t *testing.T) {
	if !odjsonrt.DirectEnabled() {
		t.Skip("direct path disabled")
	}
	z := `{"int":1}`
	for _, doc := range []string{
		`[` + z + z + `]`, `[` + z + `,]`, `[,` + z + `]`, `{"k"` + z + `}`, `{"k":` + z + `,}`, `[` + z[:4] + `]`,
		`[{"int":1,"int":2}]`, `{"k":{"int":1,"int":2}}`, "[{\"string\":\"\xff\"}]", `[{"string":"\ud800"}]`,
		`[` + z + `] x`, `[` + z + `,` + z + `,` + z[:6], `{"k":` + z + `}}`, `[[` + z + `]]`, `{"k":[` + z + `]}`,
		`[` + z + `,` + z + `]`, `{"a":` + z + `,"b":` + z + `}`, `[null,` + z + `,null]`, `{"k":null}`,
	} {
		var gs []gen.Zoo
		var ps []plain.Zoo
		errG := jsonv2.Unmarshal([]byte(doc), &gs)
		errP := jsonv2.Unmarshal([]byte(doc), &ps)
		if (errG != nil) != (errP != nil) {
			t.Errorf("slice %s: odjson %v, reflection %v", doc, errG, errP)
		} else if errG == nil && !reflect.DeepEqual(mustJSON(t, gs), mustJSON(t, ps)) {
			t.Errorf("slice %s: odjson %s, reflection %s", doc, mustJSON(t, gs), mustJSON(t, ps))
		}
		var gm map[string]gen.Zoo
		var pm map[string]plain.Zoo
		errG = jsonv2.Unmarshal([]byte(doc), &gm)
		errP = jsonv2.Unmarshal([]byte(doc), &pm)
		if (errG != nil) != (errP != nil) {
			t.Errorf("map %s: odjson %v, reflection %v", doc, errG, errP)
		}
	}
}

// TestNestedErrorOffsetsAreTheDocuments requires the offset in an error the
// generated parser reports at a nested position to count from the start of
// the document, as it does at the top level, not from the start of the
// value. json/v2 wraps the error with the value's own position, so the
// offset inside it is the only place the parser's byte position survives.
func TestNestedErrorOffsetsAreTheDocuments(t *testing.T) {
	if !odjsonrt.DirectEnabled() {
		t.Skip("direct path disabled")
	}
	offset := func(t *testing.T, err error) int64 {
		t.Helper()
		var te *odjsonrt.TypeError
		var se *odjsonrt.SyntaxError
		switch {
		case errors.As(err, &te):
			return te.Offset
		case errors.As(err, &se):
			return se.Offset
		}
		t.Fatalf("not an odjsonrt error: %v", err)
		return 0
	}
	for _, bad := range []string{`{"int":"x"}`, `{"int":1,"int":2}`, `{"int":1,}`, `{"string":"\ud800"}`} {
		var top gen.Zoo
		want := offset(t, jsonv2.Unmarshal([]byte(bad), &top))
		for _, prefix := range []string{`[{"int":1},`, `[ {"int":1} , `, `[{"int":1},{"int":2},`} {
			var gs []gen.Zoo
			err := jsonv2.Unmarshal([]byte(prefix+bad+`]`), &gs)
			if got := offset(t, err); got != want+int64(len(prefix)) {
				t.Errorf("%s%s]: offset %d, want %d (%v)", prefix, bad, got, want+int64(len(prefix)), err)
			}
		}
		for _, prefix := range []string{`{"a":{"int":1},"b":`, `{ "a" : {"int":1} , "b" : `} {
			var gm map[string]gen.Zoo
			err := jsonv2.Unmarshal([]byte(prefix+bad+`}`), &gm)
			if got := offset(t, err); got != want+int64(len(prefix)) {
				t.Errorf("%s%s}: offset %d, want %d (%v)", prefix, bad, got, want+int64(len(prefix)), err)
			}
		}
		var w nest[gen.Zoo]
		prefix := `{"v":{"int":1},"p":`
		err := jsonv2.Unmarshal([]byte(prefix+bad+`}`), &w)
		if got := offset(t, err); got != want+int64(len(prefix)) {
			t.Errorf("%s%s}: offset %d, want %d (%v)", prefix, bad, got, want+int64(len(prefix)), err)
		}
	}
}

// omitNest holds the two struct shapes that encode as {}, under omitempty.
// json/v2 drops such a member after encoding it, by unwriting the buffer;
// after a direct write that is the generated codec's bytes it unwrites.
type omitNest[U, M any] struct {
	U U   `json:"u,omitempty"`
	M M   `json:"m,omitempty"`
	N int `json:"n"`
}

// TestOmitEmptyUnwritesADirectWrite requires the member a generated codec
// wrote on the direct path to be dropped by omitempty exactly as reflection
// drops it: json/v2 unwrites an empty object, encoding/json never omits a
// struct, and both entry points have to agree with the reflection twin.
func TestOmitEmptyUnwritesADirectWrite(t *testing.T) {
	if !odjsonrt.DirectEnabled() {
		t.Skip("direct path disabled")
	}
	type G = omitNest[gen.Unit, gen.Memberless]
	type P = omitNest[plain.Unit, plain.Memberless]
	g, p := G{N: 1}, P{N: 1}
	for _, c := range []struct {
		gen, plain any
		want       string
	}{
		{g, p, `{"n":1}`},
		{[]G{g, g}, []P{p, p}, `[{"n":1},{"n":1}]`},
		{map[string]G{"k": g}, map[string]P{"k": p}, `{"k":{"n":1}}`},
	} {
		gv2, err := jsonv2.Marshal(c.gen)
		if err != nil {
			t.Fatal(err)
		}
		var w bytes.Buffer
		if err := jsonv2.MarshalWrite(&w, c.gen); err != nil {
			t.Fatal(err)
		}
		if string(gv2) != c.want || w.String() != c.want {
			t.Errorf("%T: Marshal %s, MarshalWrite %s, want %s", c.gen, gv2, w.String(), c.want)
		}
		gv1, err := jsonv1.Marshal(c.gen)
		if err != nil {
			t.Fatal(err)
		}
		pv1, err := jsonv1.Marshal(c.plain)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(gv1, pv1) || !strings.Contains(string(gv1), `"u":{},"m":{}`) {
			t.Errorf("%T: encoding/json %s, reflection %s", c.gen, gv1, pv1)
		}
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := jsonv2.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Package shapes measures the generated codec against the reflection baseline
// on document shapes the README's two payloads do not have.
//
// bench/gen and bench/plain measure twitter (616 KiB, indented, Japanese
// text, string heavy) and small (340 B). Everything the generator and
// odjsonrt were tuned on is one of those two, so this package is the check
// that the tuning generalises: each shape here changes one thing the two
// fixtures hold fixed, and the ratio between the gen and plain rows says
// whether the generated codec still pays for itself there. Like bench/ab it
// measures both sides in one process, so the ratios are not across-process
// comparisons.
//
// The other libraries bench/plain, bench/easyjson and bench/gojay measure as
// baselines run over the same shapes, so the README's comparison against
// them can be read past its three payloads: the reflection libraries on the
// plain declaration, easyjson on a declaration of its own that carries its
// generated code, and gojay and simdjson-go, whose code is written by hand
// per type, on the reference rows only.
//
// Two of the shapes are real corpora that are not committed, because their
// provenance is less clear than twitter.json's: set ODJSON_BENCH_CORPUS to a
// directory holding nativejson-benchmark's canada.json and citm_catalog.json
// and their rows run too; without it they are skipped.
package shapes

import (
	"bytes"
	jsonv1 "encoding/json"
	jsonv2 "encoding/json/v2"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bytedance/sonic"
	"github.com/francoispqt/gojay"
	gojson "github.com/goccy/go-json"
	jsoniter "github.com/json-iterator/go"
	ej "github.com/mailru/easyjson"
	simdjson "github.com/minio/simdjson-go"
	segmentio "github.com/segmentio/encoding/json"
	"github.com/sugawarayuuta/sonnet"
	"github.com/wI2L/jettison"

	"github.com/mazrean/odjson/odjsonrt"

	beasyjson "github.com/mazrean/odjson/bench/easyjson"
	bgen "github.com/mazrean/odjson/bench/gen"
	bgojay "github.com/mazrean/odjson/bench/gojay"
	"github.com/mazrean/odjson/bench/internal/harness"
	bplain "github.com/mazrean/odjson/bench/plain"
	"github.com/mazrean/odjson/bench/shapes/easyjson"
	"github.com/mazrean/odjson/bench/shapes/gen"
	"github.com/mazrean/odjson/bench/shapes/plain"
)

// codec is one JSON library, driven through the `any` signature that all of
// them share. A library that does one direction only leaves the other nil,
// and the tests and benchmarks skip that side rather than fail on it.
type codec struct {
	name      string
	marshal   func(any) ([]byte, error)
	unmarshal func([]byte, any) error
	// sides names the type declarations the codec is measured on. The four
	// hosts run on gen and plain, and the ratio between the two is what this
	// package is about; a reflection baseline runs on plain alone; a code
	// generator runs on the declaration that carries its code.
	sides []string
	// supports, when set, says why the codec cannot take a value of this
	// type: a code generator that has no code for it, or a hand-written
	// walker that only knows the reference rows' types.
	supports func(v any) string
	// skip, when set and returning a reason, says why this codec cannot run
	// on this machine.
	skip func() string
}

// host is a library that honours the generated methods and is measured on
// both declarations.
func host(name string, marshal func(any) ([]byte, error), unmarshal func([]byte, any) error) codec {
	return codec{name: name, marshal: marshal, unmarshal: unmarshal, sides: []string{"gen", "plain"}}
}

// baseline is a library measured as it ships, on the plain declaration only.
func baseline(name string, marshal func(any) ([]byte, error), unmarshal func([]byte, any) error) codec {
	return codec{name: name, marshal: marshal, unmarshal: unmarshal, sides: []string{"plain"}}
}

var codecs = []codec{
	host("encoding-json", jsonv1.Marshal, jsonv1.Unmarshal),
	host("json-v2", func(v any) ([]byte, error) { return jsonv2.Marshal(v) }, func(b []byte, v any) error { return jsonv2.Unmarshal(b, v) }),
	host("sonic", sonic.Marshal, sonic.Unmarshal),
	host("go-json", gojson.Marshal, gojson.Unmarshal),

	// The rest are the baselines bench/plain measures on the README's three
	// payloads, with the same configuration and for the same reasons; see
	// its codec table. sonic-std is the like for like sonic (HTML escaped,
	// UTF-8 validated), json-iterator is ConfigCompatibleWithStandardLibrary,
	// jettison is an encoder only, simdjson-go a parser only, and sonnet is
	// measured but kept out of the chart.
	baseline("sonic-std", sonic.ConfigStd.Marshal, sonic.ConfigStd.Unmarshal),
	baseline("json-iterator", jsoniter.ConfigCompatibleWithStandardLibrary.Marshal, jsoniter.ConfigCompatibleWithStandardLibrary.Unmarshal),
	baseline("segmentio", segmentio.Marshal, segmentio.Unmarshal),
	baseline("jettison", jettison.Marshal, nil),
	baseline("sonnet", sonnet.Marshal, sonnet.Unmarshal),
	{
		// simdjson-go parses into a tape; bench/plain walks that tape into
		// TwitterStruct and Book by hand, and that walk is what the row
		// measures, on the shapes that decode into those two types. The
		// other shapes would each need a walk of their own.
		name:      "simdjson-go",
		unmarshal: bplain.SimdjsonUnmarshal,
		sides:     []string{"plain"},
		supports: func(v any) string {
			switch v.(type) {
			case *bplain.TwitterStruct, *bplain.Book:
				return ""
			}
			return fmt.Sprintf("no hand-written simdjson-go walk for %T: the reference rows only", v)
		},
		skip: func() string {
			if !simdjson.SupportedCPU() {
				return "simdjson-go needs AVX2 and CLMUL"
			}
			return ""
		},
	},
	{
		// easyjson is a code generator, reached through its own entry
		// points; the easyjson side carries its generated code. It
		// generates for struct types, so a top-level slice or map has no
		// row.
		name: "easyjson",
		marshal: func(v any) ([]byte, error) {
			return ej.Marshal(v.(ej.Marshaler))
		},
		unmarshal: func(b []byte, v any) error {
			return ej.Unmarshal(b, v.(ej.Unmarshaler))
		},
		sides: []string{"easyjson"},
		supports: func(v any) string {
			if _, ok := v.(ej.Marshaler); !ok {
				return fmt.Sprintf("easyjson generates methods for struct types only; %T has none", v)
			}
			return ""
		},
	},
	{
		// gojay's marshalers are written by hand in bench/gojay, for the
		// reference rows' types; the gojay side carries them. The other
		// shapes would each need a codec of their own.
		name: "gojay",
		marshal: func(v any) ([]byte, error) {
			return gojay.MarshalJSONObject(v.(gojay.MarshalerJSONObject))
		},
		unmarshal: func(b []byte, v any) error {
			return gojay.UnmarshalJSONObject(b, v.(gojay.UnmarshalerJSONObject))
		},
		sides: []string{"gojay"},
		supports: func(v any) string {
			if _, ok := v.(gojay.MarshalerJSONObject); !ok {
				return fmt.Sprintf("no hand-written gojay codec for %T: the reference rows only", v)
			}
			return ""
		},
	},
}

// skipUnsupported skips the test or benchmark when the codec cannot run here.
func (c codec) skipUnsupported(tb testing.TB) {
	tb.Helper()
	if c.skip != nil {
		if reason := c.skip(); reason != "" {
			tb.Skip(reason)
		}
	}
}

// pick returns the sides of s the codec runs on, in the codec's order, with
// the reason a side is skipped in place of the side when it is: no
// declaration of that name, or a type the codec has no code for.
func (c codec) pick(s shape) []pickedSide {
	var out []pickedSide
	for _, name := range c.sides {
		p := pickedSide{name: name}
		if i := s.side(name); i < 0 {
			p.skip = "no " + name + " declaration for this shape"
		} else {
			p.side = s.sides[i]
			if c.supports != nil {
				p.skip = c.supports(p.side.fresh())
			}
		}
		out = append(out, p)
	}
	return out
}

type pickedSide struct {
	name string
	side side
	skip string
}

// side is one type declaration of a shape: gen carries odjson's generated
// codec, plain is the reflection baseline, and a code generator that needs
// code attached to the types (easyjson, gojay) gets a declaration of its
// own, named after it.
type side struct {
	name string
	// value is the document decoded into the side's own type.
	value any
	// fresh returns a pointer to a zero value of the side's own type.
	fresh func() any
}

// shape is one document and the two types it decodes into.
type shape struct {
	name string
	// what says which mechanism the shape exercises; it is printed by
	// TestShapes so a benchmark log can be read without the source.
	what string
	data []byte
	// skip is set on a shape whose data is unavailable (a corpus that is not
	// on disk); its rows are skipped rather than failed.
	skip string
	// unmarshalOnly is set when the marshal direction would not measure
	// anything the document is about (a skip heavy document encodes to the
	// same bytes as one without the unknown members).
	unmarshalOnly bool
	sides         []side
}

// side returns the index of the declaration called name, or -1.
func (s shape) side(name string) int {
	for i, sd := range s.sides {
		if sd.name == name {
			return i
		}
	}
	return -1
}

// decodeInto decodes data into a new T with encoding/json and returns the
// side called name.
func decodeInto[T any](shapeName, name string, data []byte) side {
	v := new(T)
	if err := jsonv1.Unmarshal(data, v); err != nil {
		panic(fmt.Sprintf("%s: decode into %T: %v", shapeName, v, err))
	}
	return side{name: name, value: v, fresh: func() any { return new(T) }}
}

// build decodes data into G, P and E — the gen, plain and easyjson
// declarations of one type — with encoding/json, and returns the shape. A
// top-level slice or map is as good a G as a struct: new(G) is then a pointer
// to it (and the easyjson row skips itself, since easyjson generates for
// structs only).
func build[G, P, E any](name, what string, data []byte) shape {
	return shape{
		name: name,
		what: what,
		data: data,
		sides: []side{
			decodeInto[G](name, "gen", data),
			decodeInto[P](name, "plain", data),
			decodeInto[E](name, "easyjson", data),
		},
	}
}

// withSide adds a declaration called name, decoded into T, to a shape: the
// gojay side of the reference rows, whose hand-written codec bench/gojay
// carries.
func withSide[T any](s shape, name string) shape {
	if s.skip != "" {
		return s
	}
	s.sides = append(s.sides, decodeInto[T](s.name, name, s.data))
	return s
}

// corpus reads a nativejson-benchmark file from $ODJSON_BENCH_CORPUS, or
// returns a shape that is skipped with the reason.
func corpus[G, P, E any](name, what, file string) shape {
	dir := os.Getenv("ODJSON_BENCH_CORPUS")
	if dir == "" {
		return shape{name: name, what: what, skip: "ODJSON_BENCH_CORPUS is not set"}
	}
	b, err := os.ReadFile(filepath.Join(dir, file))
	if err != nil {
		return shape{name: name, what: what, skip: err.Error()}
	}
	return build[G, P, E](name, what, b)
}

func mustJSON(v any) []byte {
	b, err := jsonv1.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func compact(b []byte) []byte {
	var buf bytes.Buffer
	if err := jsonv1.Compact(&buf, b); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func indent(b []byte) []byte {
	var buf bytes.Buffer
	if err := jsonv1.Indent(&buf, b, "", "  "); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

// rng is the one source of randomness, so every document is the same on
// every run and machine.
func rng() *rand.Rand { return rand.New(rand.NewPCG(20260911, 1)) }

func uuid(r *rand.Rand) string {
	return fmt.Sprintf("%08x-%04x-4%03x-%04x-%012x",
		r.Uint32(), r.Uint32()&0xffff, r.Uint32()&0xfff, 0x8000|r.Uint32()&0x3fff, r.Uint64()&0xffffffffffff)
}

var words = strings.Fields(`the quick brown fox jumps over a lazy dog while
seven silent engineers measure every allocation before trusting a benchmark
result and nobody reads the appendix until the numbers stop agreeing`)

func sentence(r *rand.Rand, n int) string {
	var sb strings.Builder
	for i := range n {
		if i > 0 {
			sb.WriteByte(' ')
		}
		sb.WriteString(words[r.IntN(len(words))])
	}
	return sb.String()
}

// items builds n Item values with mostly unique strings and a handful of
// repeated ones (tags, roles), the way a real listing looks.
func items(r *rand.Rand, n int) []plain.Item {
	roles := []string{"owner", "editor", "viewer"}
	tags := []string{"alpha", "beta", "gamma", "delta", "prod", "staging", "eu", "us"}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	out := make([]plain.Item, n)
	for i := range out {
		name := sentence(r, 3)
		it := plain.Item{
			ID:        int64(r.Uint32())<<8 | int64(i),
			UUID:      uuid(r),
			Name:      name,
			Email:     fmt.Sprintf("%s.%d@example.com", words[r.IntN(len(words))], r.IntN(10000)),
			CreatedAt: base.Add(time.Duration(r.Int64N(int64(365 * 24 * time.Hour)))).Truncate(time.Second),
			Score:     math.Round(r.Float64()*10000) / 100,
			Count:     r.IntN(100000),
			Active:    r.IntN(3) != 0,
			Tags:      []string{tags[r.IntN(len(tags))], tags[r.IntN(len(tags))]},
			Attrs: map[string]string{
				"region": tags[6+r.IntN(2)],
				"tier":   fmt.Sprint(r.IntN(5)),
				"sku":    fmt.Sprintf("SKU-%06d", r.IntN(1000000)),
			},
		}
		if r.IntN(4) != 0 {
			it.Owner = &plain.Owner{ID: int64(r.Uint32()), Name: sentence(r, 2), Role: roles[r.IntN(len(roles))]}
		}
		out[i] = it
	}
	return out
}

func page(r *rand.Rand, n int) plain.Page {
	return plain.Page{Items: items(r, n), Total: n * 7, NextCursor: uuid(r)}
}

// lines builds n strings of roughly width bytes out of pick, which returns
// one token in the script under test.
func lines(r *rand.Rand, n, width int, pick func(*rand.Rand) string) []string {
	out := make([]string, n)
	for i := range out {
		var sb strings.Builder
		for sb.Len() < width {
			if sb.Len() > 0 {
				sb.WriteByte(' ')
			}
			sb.WriteString(pick(r))
		}
		out[i] = sb.String()
	}
	return out
}

func pickFrom(list []string) func(*rand.Rand) string {
	return func(r *rand.Rand) string { return list[r.IntN(len(list))] }
}

var (
	asciiWords    = words
	latinWords    = strings.Fields(`café naïve façade résumé Straße Übergröße piñata jalapeño smörgåsbord Ærø crème brûlée señor Zürich Köln Malmö Gdańsk Łódź`)
	cyrillicWords = strings.Fields(`быстрая коричневая лиса прыгает через ленивую собаку пока семь молчаливых инженеров измеряют каждое выделение памяти`)
	cjkWords      = strings.Fields(`素早い 茶色の 狐が 怠け者の 犬を 飛び越える 七人の 無口な 技術者が すべての 割り当てを 測定する 前に ベンチマークを 信じる`)
	hangulWords   = strings.Fields(`빠른 갈색 여우가 게으른 개를 뛰어넘는다 일곱 명의 조용한 엔지니어가 벤치마크를 믿기 전에 모든 할당을 측정한다`)
	emojiWords    = strings.Fields(`🦊 jumps 🐕 over 🎉 the 🚀 lazy 🧪 dog 📊 while 🔬 seven 🛠️ engineers 🧠 measure 💾 every 🧵 allocation ⏱️`)
	escapedWords  = strings.Fields("say\t\"hi\"\\n <b>bold</b>&amp; \"quoted\" back\\slash tab\tend ctrl line\nbreak <script>&</script>")
)

func text(r *rand.Rand, pick func(*rand.Rand) string) plain.Text {
	return plain.Text{Lines: lines(r, 96, 80, pick)}
}

func dense(r *rand.Rand, n int) plain.DenseDoc {
	rows := make([]plain.Dense, n)
	for i := range rows {
		d := &rows[i]
		v := reflect.ValueOf(d).Elem()
		for j := range v.NumField() {
			f := v.Field(j)
			switch f.Kind() {
			case reflect.Int32:
				f.SetInt(int64(r.IntN(1000)))
			case reflect.Bool:
				f.SetBool(r.IntN(2) == 0)
			case reflect.Float64:
				f.SetFloat(float64(r.IntN(10000)) / 100)
			}
		}
	}
	return plain.DenseDoc{Rows: rows}
}

// sparseDoc writes the Sparse rows by hand so that the document can carry
// explicit nulls, which omitempty pointers never encode.
func sparseDoc(r *rand.Rand, n int) []byte {
	rows := make([]map[string]any, n)
	for i := range rows {
		m := map[string]any{}
		for range 4 {
			m[fmt.Sprintf("f%02d", r.IntN(16))] = r.IntN(1000)
		}
		for range 3 {
			m[fmt.Sprintf("s%02d", r.IntN(16))] = sentence(r, 2)
		}
		for range 3 {
			m[fmt.Sprintf("s%02d", r.IntN(16))] = nil
		}
		if r.IntN(2) == 0 {
			m["n"] = map[string]any{"id": r.IntN(1000), "name": sentence(r, 2), "role": "viewer"}
		}
		rows[i] = m
	}
	return mustJSON(map[string]any{"rows": rows})
}

// skipDoc is a Page whose items each carry unknown members, nested objects
// and arrays included, that the decoder has to skip.
func skipDoc(r *rand.Rand, n int) []byte {
	its := items(r, n)
	rows := make([]map[string]any, n)
	for i, it := range its {
		var m map[string]any
		if err := jsonv1.Unmarshal(mustJSON(it), &m); err != nil {
			panic(err)
		}
		for j := range 12 {
			m[fmt.Sprintf("extra_%02d", j)] = sentence(r, 3)
		}
		m["extra_obj"] = map[string]any{"a": r.IntN(100), "b": sentence(r, 4), "c": []int{1, 2, 3}, "d": map[string]any{"e": true, "f": nil}}
		m["extra_arr"] = []any{r.IntN(100), sentence(r, 2), 1.5, false, nil, []any{"x", "y"}}
		rows[i] = m
	}
	return mustJSON(map[string]any{"items": rows, "total": n, "server": "edge-7", "took_ms": 12, "warnings": []any{}})
}

func numbers(r *rand.Rand) plain.Numbers {
	n := plain.Numbers{}
	for range 400 {
		n.I64 = append(n.I64, r.Int64()-math.MaxInt64/2)
		n.U64 = append(n.U64, r.Uint64())
		n.I16 = append(n.I16, int16(r.IntN(1<<16)-1<<15))
		n.F32 = append(n.F32, r.Float32()*1000)
		n.F64 = append(n.F64, r.Float64()*360-180)
		n.Sci = append(n.Sci, math.Pow(10, float64(r.IntN(600)-300))*r.Float64())
	}
	n.I64 = append(n.I64, math.MinInt64, math.MaxInt64, 0, -1)
	n.U64 = append(n.U64, math.MaxUint64, 0)
	n.F64 = append(n.F64, math.MaxFloat64, math.SmallestNonzeroFloat64, 1e21, 1e-7, 0.1, 100)
	return n
}

func floatsDoc(r *rand.Rand) plain.Canada {
	// The real canada.json has 480 polygons and 111k points; a fifth of that
	// is enough to be the same shape.
	var feats []plain.Feature
	for range 96 {
		ring := make([][]float64, 240)
		for i := range ring {
			ring[i] = []float64{r.Float64()*360 - 180, r.Float64()*180 - 90}
		}
		feats = append(feats, plain.Feature{
			Type:       "Feature",
			Properties: map[string]string{"name": "Nowhere"},
			Geometry:   plain.Geometry{Type: "Polygon", Coordinates: [][][]float64{ring}},
		})
	}
	return plain.Canada{Type: "FeatureCollection", Features: feats}
}

var loadShapes = sync.OnceValue(func() []shape {
	r := rng()
	twitter, err := os.ReadFile("../testdata/twitter.json")
	if err != nil {
		panic(err)
	}
	medium := mustJSON(page(r, 50))
	pages := []plain.Page{page(r, 20), page(r, 20), page(r, 20)}
	var generic any
	if err := jsonv1.Unmarshal(medium, &generic); err != nil {
		panic(err)
	}
	return []shape{
		// The reference rows: what bench/gen and bench/plain measure, so the
		// other rows can be read against them from the same process.
		withSide[bgojay.TwitterStruct](build[bgen.TwitterStruct, bplain.TwitterStruct, beasyjson.TwitterStruct]("twitter", "reference: the README's large payload", twitter), "gojay"),
		withSide[bgojay.Book](build[bgen.Book, bplain.Book, beasyjson.Book]("small", "reference: the README's small payload", bplain.SmallPayload()), "gojay"),

		withSide[bgojay.TwitterStruct](build[bgen.TwitterStruct, bplain.TwitterStruct, beasyjson.TwitterStruct]("twitter-compact", "twitter with the indentation removed", compact(twitter)), "gojay"),

		build[gen.Page, plain.Page, easyjson.Page]("page-3k", "one object, ~3 KiB, under odjsonrt.WholeValue's threshold", mustJSON(page(r, 12))),
		build[gen.Page, plain.Page, easyjson.Page]("page-12k", "one object, ~12 KiB", medium),
		build[gen.Page, plain.Page, easyjson.Page]("page-100k", "one object, ~100 KiB", mustJSON(page(r, 400))),
		build[gen.Page, plain.Page, easyjson.Page]("page-12k-indented", "page-12k pretty printed", indent(medium)),
		build[[]gen.Item, []plain.Item, []easyjson.Item]("array-items", "top-level []Item: 50 elements of ~240 B, no direct path, each read whole", mustJSON(items(r, 50))),
		build[[]gen.Page, []plain.Page, []easyjson.Page]("array-pages", "top-level []Page: 3 elements of ~5 KiB, no direct path, each above the whole-value threshold", mustJSON(pages)),
		build[map[string]gen.Item, map[string]plain.Item, map[string]easyjson.Item]("map-items", "top-level map[string]Item, 50 entries, no direct path", mustJSON(func() map[string]plain.Item {
			m := map[string]plain.Item{}
			for _, it := range items(r, 50) {
				m[it.UUID] = it
			}
			return m
		}())),
		build[gen.Generic, plain.Generic, easyjson.Generic]("generic", "page-12k decoded into any: the runtime's generic parser end to end", mustJSON(map[string]any{"payload": generic})),

		build[gen.Text, plain.Text, easyjson.Text]("text-ascii", "strings of ASCII words", mustJSON(text(r, pickFrom(asciiWords)))),
		build[gen.Text, plain.Text, easyjson.Text]("text-latin", "strings with Latin-1 accents: two byte sequences among ASCII", mustJSON(text(r, pickFrom(latinWords)))),
		build[gen.Text, plain.Text, easyjson.Text]("text-cyrillic", "strings of two byte sequences", mustJSON(text(r, pickFrom(cyrillicWords)))),
		build[gen.Text, plain.Text, easyjson.Text]("text-cjk", "strings of three byte sequences with E3 leads, as in twitter", mustJSON(text(r, pickFrom(cjkWords)))),
		build[gen.Text, plain.Text, easyjson.Text]("text-hangul", "strings of three byte sequences with EA-ED leads", mustJSON(text(r, pickFrom(hangulWords)))),
		build[gen.Text, plain.Text, easyjson.Text]("text-emoji", "strings with four byte sequences among ASCII", mustJSON(text(r, pickFrom(emojiWords)))),
		build[gen.Text, plain.Text, easyjson.Text]("text-escaped", "strings full of quotes, backslashes, control characters and HTML", mustJSON(text(r, pickFrom(escapedWords)))),
		build[gen.IDs, plain.IDs, easyjson.IDs]("unique-strings", "512 UUIDs: every string misses the decoder's cache", mustJSON(plain.IDs{Values: lines(r, 512, 1, uuid)})),

		build[gen.Numbers, plain.Numbers, easyjson.Numbers]("numbers", "full precision floats, exponents, float32, int64/uint64 extremes", mustJSON(numbers(r))),
		build[gen.Canada, plain.Canada, easyjson.Canada]("floats", "GeoJSON polygons: 23k coordinate pairs of full precision floats", mustJSON(floatsDoc(r))),
		build[gen.DenseDoc, plain.DenseDoc, easyjson.DenseDoc]("dense", "56 short scalar members per row, ~9 bytes per member", mustJSON(dense(r, 40))),
		build[gen.SparseDoc, plain.SparseDoc, easyjson.SparseDoc]("sparse", "48 optional members per row, 10 present and 3 of them null", sparseDoc(r, 60)),
		unmarshalOnly(build[gen.Page, plain.Page, easyjson.Page]("skip", "page whose items each carry 14 unknown members to skip", skipDoc(r, 50))),

		corpus[gen.Canada, plain.Canada, easyjson.Canada]("canada", "nativejson-benchmark canada.json (2.2 MB of floats)", "canada.json"),
		corpus[gen.CitmCatalog, plain.CitmCatalog, easyjson.CitmCatalog]("citm", "nativejson-benchmark citm_catalog.json (1.7 MB, indented, maps and ints)", "citm_catalog.json"),
	}
})

func unmarshalOnly(s shape) shape {
	s.unmarshalOnly = true
	return s
}

func shapes(tb testing.TB) []shape {
	tb.Helper()
	return loadShapes()
}

// TestDirectEnabled fails when the direct path is off, because then the gen
// rows of the top-level object shapes would be measuring the fallback.
func TestDirectEnabled(t *testing.T) {
	if !odjsonrt.DirectEnabled() {
		t.Fatal("odjsonrt's direct path is disabled on this toolchain; the gen rows would measure the public API fallback")
	}
}

// TestSameSource keeps the type declarations identical but for the package
// clause, so a difference between a gen row and a plain row is the generated
// code and nothing else, and easyjson's rows are measuring the same types.
func TestSameSource(t *testing.T) {
	p, err := os.ReadFile("plain/types.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range []string{"gen", "easyjson"} {
		g, err := os.ReadFile(pkg + "/types.go")
		if err != nil {
			t.Fatal(err)
		}
		want := bytes.Replace(p, []byte("package plain\n"), []byte("package "+pkg+"\n"), 1)
		if !bytes.Equal(g, want) {
			t.Fatalf("%s/types.go and plain/types.go differ beyond the package clause", pkg)
		}
	}
}

// TestShapes prints every shape with its size and checks every library on
// it: for a host, that the gen and plain sides decode the document to the
// same value and that the generated codec's own output round trips; for a
// baseline, that what it decodes and what it encodes agree with
// encoding/json's reading of the plain value, so that its rows are measuring
// the same job.
func TestShapes(t *testing.T) {
	for _, s := range shapes(t) {
		t.Run(s.name, func(t *testing.T) {
			if s.skip != "" {
				t.Skip(s.skip)
			}
			t.Logf("%7d bytes  %s", len(s.data), s.what)
			want := harness.Canonical(t, s.sides[s.side("plain")].value)
			for _, c := range codecs {
				t.Run(c.name, func(t *testing.T) {
					c.skipUnsupported(t)
					var got []any
					for _, p := range c.pick(s) {
						if p.skip != "" {
							t.Skip(p.skip)
						}
						got = append(got, roundTrip(t, c, s, p.side))
					}
					if len(got) == 2 {
						// A host: the two declarations, under the same
						// library, must read and write the same value.
						if !reflect.DeepEqual(got[0], got[1]) {
							t.Error("gen and plain disagree")
						}
						return
					}
					if !reflect.DeepEqual(got[0], want) {
						t.Error("differs from encoding/json's reading of the plain value")
					}
				})
			}
		})
	}
}

// roundTrip decodes the shape's document with c into a fresh value of the
// side's type, encodes it back with c, and returns the result as
// encoding/json reads it. The direction c lacks is done by encoding/json,
// so an encoder only or a parser only is still checked end to end.
func roundTrip(t *testing.T, c codec, s shape, sd side) any {
	t.Helper()
	v := sd.fresh()
	unmarshal := c.unmarshal
	if unmarshal == nil {
		unmarshal = jsonv1.Unmarshal
	}
	if err := unmarshal(s.data, v); err != nil {
		t.Fatalf("%s unmarshal: %v", sd.name, err)
	}
	marshal := c.marshal
	switch {
	case marshal == nil:
		marshal = jsonv1.Marshal
	case sd.name == "plain" && (c.name == "encoding-json" || c.name == "json-v2"):
		// Those two call the generated MarshalJSONTo, which follows
		// json/v2's semantics (a nil slice is [], not null), so the plain
		// side has to be encoded under the same semantics to be comparable.
		marshal = func(v any) ([]byte, error) { return jsonv2.Marshal(v) }
	}
	out, err := marshal(v)
	if err != nil {
		t.Fatalf("%s marshal: %v", sd.name, err)
	}
	var got any
	if err := jsonv1.Unmarshal(out, &got); err != nil {
		t.Fatalf("%s output: %v", sd.name, err)
	}
	return got
}

func BenchmarkMarshal(b *testing.B) {
	for _, s := range shapes(b) {
		b.Run("shape="+s.name, func(b *testing.B) {
			if s.skip != "" {
				b.Skip(s.skip)
			}
			if s.unmarshalOnly {
				b.Skip("unmarshal only")
			}
			for _, c := range codecs {
				if c.marshal == nil {
					continue
				}
				b.Run("lib="+c.name, func(b *testing.B) {
					c.skipUnsupported(b)
					for _, p := range c.pick(s) {
						b.Run("side="+p.name, func(b *testing.B) {
							if p.skip != "" {
								b.Skip(p.skip)
							}
							out, err := c.marshal(p.side.value)
							if err != nil {
								b.Fatal(err)
							}
							b.ReportAllocs()
							b.SetBytes(int64(len(out)))
							for b.Loop() {
								if _, err := c.marshal(p.side.value); err != nil {
									b.Fatal(err)
								}
							}
						})
					}
				})
			}
		})
	}
}

func BenchmarkUnmarshal(b *testing.B) {
	for _, s := range shapes(b) {
		b.Run("shape="+s.name, func(b *testing.B) {
			if s.skip != "" {
				b.Skip(s.skip)
			}
			for _, c := range codecs {
				if c.unmarshal == nil {
					continue
				}
				b.Run("lib="+c.name, func(b *testing.B) {
					c.skipUnsupported(b)
					for _, p := range c.pick(s) {
						b.Run("side="+p.name, func(b *testing.B) {
							if p.skip != "" {
								b.Skip(p.skip)
							}
							if err := c.unmarshal(s.data, p.side.fresh()); err != nil {
								b.Fatal(err)
							}
							b.ReportAllocs()
							b.SetBytes(int64(len(s.data)))
							for b.Loop() {
								if err := c.unmarshal(s.data, p.side.fresh()); err != nil {
									b.Fatal(err)
								}
							}
						})
					}
				})
			}
		})
	}
}

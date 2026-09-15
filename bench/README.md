# odjson benchmarks

A **separate Go module** (`github.com/mazrean/odjson/bench`) that benchmarks the
host JSON libraries odjson targets, on shared payloads.

It is deliberately not part of the root module: sonic drags in
`twitchyliquid64/golang-asm` and `golang.org/x/arch`, gojay a 2019-era
dependency graph of its own, and those must never leak
into the root `go.mod`. A `replace github.com/mazrean/odjson => ../` points at
the working tree, so the benchmarks always measure local code.

## Running

```sh
cd bench
go test -bench . -benchmem ./...
```

Useful variants:

```sh
go test -bench . -benchtime 10x -benchmem ./...   # quick smoke run
go test -bench BenchmarkUnmarshal -benchmem ./... # one direction only
go test -run TestPayloadsRoundTrip ./...          # fixtures + wiring sanity check
```

## What is measured

Ten libraries, driven through one uniform table:

| Label | Package | Sides |
| --- | --- | --- |
| `encoding-json` | `encoding/json` (v1) | both |
| `json-v2` | `encoding/json/v2` (stdlib, Go 1.27+, no GOEXPERIMENT needed) | both |
| `sonic` | `github.com/bytedance/sonic` | both |
| `go-json` | `github.com/goccy/go-json` | both |
| `json-iterator` | `github.com/json-iterator/go`, as `ConfigCompatibleWithStandardLibrary` | both |
| `segmentio` | `github.com/segmentio/encoding/json` | both |
| `jettison` | `github.com/wI2L/jettison` | Marshal only: it is an encoder |
| `simdjson-go` | `github.com/minio/simdjson-go`, plus a hand-written walk of its tape into the struct | Unmarshal only: it is a parser |
| `sonnet` | `github.com/sugawarayuuta/sonnet` (no tagged release; its latest commit, 2023-10) | both; measured, kept out of the chart |
| `easyjson` | `github.com/mailru/easyjson`, with its generated code | both, in `bench/easyjson` |
| `gojay` | `github.com/francoispqt/gojay`, with hand-written marshalers | both, in `bench/gojay` |

The first nine are measured as they ship, in `plain`. The last two are code
generators like odjson, so they need code attached to the types and get a
package each, the way odjson gets `gen`; see "The other code generators"
below. A library that does one direction only has no row on the other side,
in the tables and in the chart alike. `sonnet` is measured but deliberately
not drawn: it honours the v1 interfaces, so `gen` and `ab` carry it too, and
its numbers put it in the middle of the reflection libraries on every row
(see `docs/internals.md`), so a chart row for it would say nothing the
existing rows do not.

Three payloads, the three sizes sonic's own README benchmarks: `twitter`
(~616 KiB, decoded into `TwitterStruct` — deeply nested, lots of strings and
slices), `medium` (~13 KiB, the same shape at four statuses instead of a
hundred, decoded into the same `TwitterStruct` — the size where neither the
per-call floor nor throughput dominates on its own) and `small` (~340 B,
decoded into `Book` — the per-call overhead case, where fixed costs dominate).

Benchmark names are `BenchmarkMarshal/<lib>/<payload>` and
`BenchmarkUnmarshal/<lib>/<payload>`.

Reading the columns:

- **ns/op** — time per Marshal or Unmarshal call. Lower is better.
- **MB/s** — from `b.SetBytes(len(payload))`, so it is throughput relative to the
  *source document* size in both directions. It is a normalised restatement of
  ns/op, handy for comparing across the two payload sizes; it is not a separate
  measurement.
- **B/op**, **allocs/op** — from `b.ReportAllocs()`. Allocation count is usually
  the clearest signal of what a codegen approach is buying, since it is far less
  machine- and noise-dependent than ns/op.

Methodology notes, since they change what the numbers mean:

- **Unmarshal decodes into a fresh zero value every iteration.** Reusing one
  target would let a decoder skip re-allocating slices and maps it already
  filled, which flatters repeat runs and does not reflect real use.
- **Marshal encodes a pre-decoded value**, so parse cost is not folded into
  encode cost.
- **Every case is warmed up before timing.** sonic JIT-compiles and go-json
  builds encoders lazily on first use; `b.Loop()` resets the timer after the
  warm-up call, so that one-off cost stays out of the measurement. This matters
  a lot at low `-benchtime`.
- **simdjson-go reuses its tape.** `simdjson.Parse` takes its previous result
  back to reuse the buffers, which is how the library is meant to be called
  and the analogue of the buffer pools sonic and go-json keep internally; the
  struct it is walked into is still a fresh zero value every iteration. The
  row needs AVX2 and CLMUL and skips itself on a CPU without them.

Cross-library numbers only mean something on an otherwise idle machine. Compare
runs with [`benchstat`](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat)
rather than eyeballing single runs.

## Layout

```
bench/
  go.mod            module + replace directive
  testdata/
    twitter.json    payload, vendored from sonic
    medium.json     sonic's "Medium" payload, vendored from its decoder tests
    NOTICE          provenance + Apache-2.0 notice for the vendored files
  plain/
    twitter.go      TwitterStruct et al., vendored from sonic
    small.go        Book/Author + the small payload, vendored from sonic
    simdjson.go     the walk from simdjson-go's tape into those types
    bench_test.go   the benchmark and round-trip test
  gen/
    twitter.go      the same types, byte-identical but for the package clause
    small.go        likewise
    odjson_gen.go   generated by `go generate ./...`
    bench_test.go   the same table, over the generated types
  easyjson/
    twitter.go      the same types again (small.go minus sonic's easyjson:skip)
    *_easyjson.go   generated by `go generate ./...`
    bench_test.go   Marshal/Unmarshal through easyjson's own entry points
  gojay/
    twitter.go      the same types again
    codec.go        hand-written gojay marshalers and unmarshalers
    bench_test.go   Marshal/Unmarshal through gojay's own entry points
  internal/harness/ what easyjson/ and gojay/ share: fixtures, loops, parity
```

`plain` is the baseline: reflection-based encoding of ordinary Go structs, with
no odjson involvement. `gen` is the same types with odjson's generated codecs
attached. Because the two packages differ only in the generated file, the
difference between a `plain` row and the matching `gen` row is attributable to
odjson and nothing else. See `testdata/NOTICE` for the provenance of the
vendored fixtures.

The `simdjson-go` row lives in `plain` because it decodes into `plain`'s
types, but its decoder is not the library's alone: simdjson-go parses into a
tape and has nothing that fills a struct, so `plain/simdjson.go` walks that
tape into `TwitterStruct` and `Book` by hand. That is the code a simdjson-go
user has to write to get a typed value out, and it is what makes the row the
same job as the other Unmarshal rows rather than a parse. `TestSimdjsonParity`
holds it to encoding/json's reading of every fixture.

## The other code generators

easyjson and gojay do what odjson does — attach dedicated code to the types —
so they are the rows that compare like with like, and each gets a package of
its own, parallel to `gen`.

`easyjson/` is the vendored types plus what `easyjson -all -no_std_marshalers`
generates for them. `-no_std_marshalers` leaves `MarshalJSON` /
`UnmarshalJSON` off the types, so easyjson is reached through its own
`easyjson.Marshal` / `easyjson.Unmarshal` and nothing else changes. The only
edit to the vendored files is the removal of two `// easyjson:skip` comments
in `small.go`, which sonic carries to keep easyjson out of *its* benchmark.
`go generate ./...` regenerates the files through easyjson's own generator.

`gojay/` is the vendored types plus hand-written `MarshalJSONObject` /
`UnmarshalJSONObject` implementations in `codec.go`. gojay ships a generator,
but it stops at the first `interface{}` field, and `TwitterStruct` has
fourteen of them; hand-written code against gojay's API is what a gojay user
has to write for these types, so that is what the row measures. It follows
encoding/json where gojay leaves a choice — a nil `interface{}` is written as
`null` rather than dropped, a nil slice as `null` and an empty one as `[]` —
and `NKeys` returns 0 everywhere, because a non-zero count makes gojay stop
after that many members whether it knew them or not.

Both packages have a `TestParity` that holds their code to encoding/json's
reading of every fixture, in canonical form: what they decode must be the
same value, and what they encode must read back as the same value. Their
benchmarks are named `BenchmarkMarshal/easyjson/<payload>` and so on, the
same shape as `plain`'s and `gen`'s, so the chart reads all four packages
from one `go test -bench` output.

## odjson generated

`gen`'s rows measure unchanged call sites — the same `json.Marshal` /
`json.Unmarshal` as `plain`, now routed through the generated methods, which is
the only way odjson is used. There is no separate direct entry point to
measure: the generator emits the four interface methods and nothing else.

`TestGeneratedMatchesReflection` asserts that every host library really does
route through the generated codec. sonic, go-json, json-iterator, segmentio
and jettison call `MarshalJSON`, so their bytes must equal that method's
exactly; `encoding/json` and
`encoding/json/v2` are both json/v2 on this toolchain and call
`MarshalJSONTo`, which follows json/v2's semantics, so for those the check is
that the value survives a round trip through the generated pair. If that test
fails, the `gen` rows are not measuring what they claim to. easyjson, gojay
and simdjson-go have entry points of their own and never reach a
`MarshalJSON`, so they have no `gen` row.

To regenerate after changing the generator:

```sh
cd bench && go generate ./...
```

That covers `gen` and `shapes/gen`, which carry the same directive, and
`easyjson`, whose directive runs easyjson's generator.

## `ab`

`bench/ab` measures the generated codec against the reflection baseline **in one
process**, on the same three payloads. The other packages each measure one side, so comparing them is a
comparison across processes: different heaps, different GC state, and several
percent of drift between runs. Some of the differences that matter here are
smaller than that drift, and reading them off two separate runs produced results
whose sign changed with `GOMAXPROCS`. Use `ab` for any claim about the generated
codec versus reflection; use `gen` and `plain` for the absolute numbers.

```sh
cd bench && go test -bench . -count 5 ./ab/
```

`ab` covers every host library — the two standard libraries, sonic, go-json,
json-iterator, segmentio and (encode only) jettison; the sonic and go-json
rows are what settle
whether odjson wins their small unmarshal (sonic's by 1.15x, go-json's by
1.07x), is level on sonic's medium unmarshal (0.99x), and loses everything
else on them (it does, by the floor).

## `shapes`

`bench/shapes` asks whether the tuning generalises. Everything the generator
and `odjsonrt` were tuned on is `twitter` or `small`, and both hold a lot
fixed: one top-level object, indented, Japanese text, few floats, strings that
repeat. Each shape here changes one of those and measures gen against plain in
one process, the way `ab` does, so the ratio between the two rows says whether
the generated codec still pays for itself on that shape:

| shape | what it varies |
| --- | --- |
| `twitter`, `small` | the README's `twitter` and `small` payloads, as the reference rows |
| `twitter-compact`, `page-12k-indented` | whitespace |
| `page-3k`, `page-12k`, `page-100k` | one top-level object at the sizes between the two fixtures |
| `array-items`, `array-pages`, `map-items` | a top-level `[]T` / `map[string]T` of a generated type, so every generated value sits below the top level; the element size puts `array-items` under `odjsonrt.WholeValue`'s threshold and `array-pages` over it, which matters on the public path |
| `generic` | the same document decoded into `any` |
| `text-*` | strings of ASCII, Latin-1, Cyrillic, CJK, Hangul, emoji, and escape-heavy content, 96 lines of 80 bytes |
| `text-ascii-short`, `text-cjk-short` | the same two scripts in 1024 strings of ~8 bytes, so the per string cost is read apart from the per byte one |
| `unique-strings` | strings that never repeat, so the decoder's string cache never hits |
| `numbers`, `floats` | full precision floats, exponents, float32, integer extremes |
| `int-small`, `int-large`, `uint-large` | 1024 integers each, of one to three digits, of 19 digits with a sign, and of 20 digits without one |
| `float-short`, `float-full`, `float32` | 1024 floats each, of two decimal places, of all 17 significant digits, and as `float32` |
| `float-exp`, `float-exp-input` | exponent notation on both sides: values so large or small that every encoder spells them `1.2e+300`, and a hand written document that spells ordinary values `1.234567e+02`, which no encoder here writes (unmarshal only) |
| `bool-array`, `null-array` | the two keyword values: 1024 booleans, and 1024 optional integers half of which are `null` |
| `array-nested` | `int-small`'s 1024 integers again, in 128 rows of 8, so the difference between the rows is the nesting |
| `map-string-1k`, `map-int-1k` | an object of 1024 entries used as a dictionary, whose member names are data the generator cannot know |
| `obj-record`, `obj-map` | the same 42 KB document read as `[]Rec` and as `[]map[string]string`: struct against map, the same bytes both times |
| `obj-long-names` | `obj-record`'s values under member names of 24 bytes |
| `deep-nest` | 32 chains of 32 nested objects: a document that is deep where every other shape is wide |
| `empties` | 1024 empty arrays, 1024 empty objects and 1024 empty strings: structure with no content |
| `dense`, `sparse` | 56 short scalars per row; 48 optional members of which 10 are present |
| `skip` | documents whose members are mostly unknown to the struct (unmarshal only) |
| `canada`, `citm` | nativejson-benchmark's other two corpora, when `ODJSON_BENCH_CORPUS` points at a directory holding them; skipped otherwise |

The last three groups are the characteristic rows: one spelling per document,
1024 values in every one of them, where `numbers` is all six spellings at
once. A mixed document is the shape a real payload has, and it is also the
shape no measurement can be attributed to — a library that is quick on short
integers and slow on full precision floats reads as one middling row. Divide
such a row by 1024 and it is a per value cost that compares across the
group; the `text-*` rows divide by 96, and the two `-short` rows by 1024.

The synthetic documents are built from a fixed seed, so they are the same on
every run. `canada.json` and `citm_catalog.json` are not committed: their
provenance is less clear than `twitter.json`'s. Fetch them from
`miloyip/nativejson-benchmark`'s `data/` directory and point the variable at
it.

The other libraries run over the same shapes too, as the baselines they are
in the tables above, so the comparison against them can be read past the
three payloads. Each is measured on the declaration that carries its code,
which the `side=` key names:

| `lib=` | `side=` | shapes |
| --- | --- | --- |
| `encoding-json`, `json-v2`, `sonic`, `go-json` | `gen` and `plain` | all: the hosts, and the ratio between the two sides is what the package is about |
| `sonic-std`, `json-iterator`, `segmentio`, `jettison` (Marshal only), `sonnet` | `plain` | all: reflection, as they ship |
| `simdjson-go` (Unmarshal only) | `plain` | `twitter`, `small`, `twitter-compact`: the hand-written walk in `plain/simdjson.go` knows those two types |
| `easyjson` | `easyjson` | all but `array-items` and `map-items`: `shapes/easyjson` is `plain/types.go` with easyjson's generated code, and easyjson generates for struct types only |
| `gojay` | `gojay` | `twitter`, `small`, `twitter-compact`: the hand-written codec in `bench/gojay` |

A row a library cannot run skips with the reason, so a missing cell in a
`benchstat` table is never silent. `TestShapes` holds every baseline to
`encoding/json`'s reading of the plain value on every shape, the way
`TestParity` does in `easyjson/` and `gojay/`.

```sh
cd bench
go test -run TestShapes -v ./shapes/                 # every shape, its size, what it varies, and which rows skip
go test -run '^$' -bench . -benchmem -count 6 ./shapes/ > shapes.txt
benchstat -col /side -row .name,/shape,/lib shapes.txt                                       # gen against plain, per host
benchstat -filter '-/side:gen OR /lib:json-v2' -col /lib,/side -row .name,/shape shapes.txt  # every library on its own declaration
```

The benchmark names carry `shape=`, `lib=` and `side=` keys so `benchstat`
can pivot on them: `-col /side` puts gen and plain side by side with the
ratio between them, and the second form drops the hosts' `gen` rows but
json/v2's, so it reads one column per library with odjson's own column
(`json-v2` / `gen`) among them. What the shapes found is recorded in
[`docs/internals.md`](../docs/internals.md#what-the-other-shapes-say), and
the baselines' table under "The other libraries on the shapes" there. The
characteristic rows have a section of their own,
["What the characteristic shapes say"](../docs/internals.md#what-the-characteristic-shapes-say):
the nineteenth digit of an integer is a cliff in the decoder, the generated
map encoder sorts its keys where the runtime path does not and pays its
whole deficit to `json/v2` for it, and the float and depth rows are the
widest margins in the file.

## `floor`

`bench/floor` answers a different question: not "how fast is the generated
codec" but "what does the host library charge for using a codec at all". Its
marshaler returns an already encoded document and its unmarshaler discards its
input, so the numbers are a lower bound for any implementation of
`json.Marshaler` / `json.Unmarshaler`. For sonic and go-json that bound is
already at or above what those libraries cost to encode without the interface,
which is why odjson cannot win their marshal rows at any speed, and it is
more than half of what they cost to decode `twitter`, which is why the large
unmarshal rows are out of reach for a pure Go decoder. Keep this package: it is
the evidence for both claims.

## `proto`

`bench/proto` is a hand-written experiment, not generated code and not part of
odjson's build. It implements `encoding/json/v2`'s `MarshalerTo` and
`UnmarshalerFrom` for one struct four ways — reflection (no methods), the
value-driven form odjson generates, a token-driven form, and a value-driven form
tuned for the streaming contract — over three payload shapes (`small`,
`stringy`, `dense`).

It is the evidence behind the generator's current strategy: the token-driven
encoder measured at parity with reflection on string-heavy values but 18% worse
on field-dense ones, so the value-driven form was kept; the token-driven
*decoder* won on every shape, so that one was adopted for large values; a
later measurement in `ab` showed a single `ReadValue` plus a trusted byte
parser winning on small ones, which is what `odjsonrt.WholeValue` now selects
below 4 KiB, and the direct path in `odjsonrt/direct.go` now bypasses both
for any value under a plain `json.Marshal` / `json.Unmarshal`. Keep it
as the record
behind `docs/internals.md`'s "what the drop-in path costs" section; delete it
only together with that section.

See the root [README](../README.md) for the chart and what it means for how you
should call odjson, and
[`docs/internals.md`](../docs/internals.md#the-measured-tables) for the absolute
figures it is drawn from.

## The chart in the root README

`chart/` renders the root README's four tables as a pair of SVG small-multiple
bar charts (light and dark), written to `docs/assets/`:

```sh
go run ./chart
```

Its numbers are literals that mirror the README's tables — the tables stay the
source of truth, so re-measuring means editing both and regenerating.

The same renderer will also draw a chart of numbers measured somewhere else:

```sh
{
  go test -run '^$' -benchmem -count 5 \
    -bench '/^(json-v2|sonic|go-json|json-iterator|segmentio|jettison|simdjson-go)$/' ./plain
  go test -run '^$' -benchmem -count 5 -bench '/^json-v2$/' ./gen
  go test -run '^$' -benchmem -count 5 -bench '/^(easyjson|gojay)$/' ./easyjson ./gojay
} > bench.txt
mkdir -p out
go run ./chart -input bench.txt -out out -summary out/summary.md \
  -footer 'Median of 5 runs · <where these came from>'
```

`-input` takes the median per benchmark name and overwrites the literals;
`-summary` writes the same numbers as a Markdown table, for wherever the image
goes (a PNG has no alt text). `-out` keeps it away from `docs/assets/`, which
belongs to the README's figures alone. The chart refuses to draw with a row
missing, so every codec it names has to be in the input — which is also why
the `simdjson-go` row makes it need a CPU with AVX2. `.github/workflows/bench.yml` runs
exactly this on every PR and comments the result — read those numbers as a
shape, not as figures: a shared CI runner is far noisier than the machine the
README quotes.

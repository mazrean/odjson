# odjson internals

This document holds the measurement work and the implementation detail behind
the summary in the [README](../README.md). It is the evidence for the
positioning claims there: the ceilings that were found, the changes that were
made, and the ones that were tried and rejected. Do not re-open any of these
questions without re-running `bench/ab` and `bench/floor`.

The ratios below come from `bench/ab`, which measures the generated codec, the
reflection baseline and the interface floor in a single process — comparing
them across processes moves the small differences by more than their size. The
measured tables below are the absolute figures from `bench/gen` and
`bench/plain`, and the README's chart is drawn from them.

## The measured tables

These are the absolute figures the README's chart is drawn from — `bench/gen`
against `bench/plain`, **medians of ten runs** on an AMD Ryzen 9 7950X, Linux,
Go 1.27.1, all within ±3% per `benchstat` except `encoding/json`'s `small`
decode on its own (±11%) and sonic's `twitter` decode (±6%). `encoding/json`
v1's rows are here
rather than in the chart, which is about the `json/v2` story.

| Marshal `twitter` | on its own | with odjson | change |
| --- | --- | --- | --- |
| **encoding/json/v2** | 400 µs | **113 µs** | **3.54× faster** |
| **encoding/json** | 416 µs | **132 µs** | **3.16× faster** |
| sonic | 116 µs | — | |
| go-json | 245 µs | — | |

| Marshal `small` | on its own | with odjson | change |
| --- | --- | --- | --- |
| **encoding/json/v2** | 1.036 µs | **305 ns** | **3.40× faster** |
| **encoding/json** | 1.049 µs | **333 ns** | **3.15× faster** |
| sonic | 311 ns | — | |
| go-json | 380 ns | — | |

| Unmarshal `twitter` | on its own | with odjson | change |
| --- | --- | --- | --- |
| **encoding/json/v2** | 1.111 ms | **516 µs** | **2.15× faster** |
| **encoding/json** | 1.504 ms | **1.217 ms** | **1.24× faster** |
| sonic | 526 µs | — | |
| go-json | 687 µs | — | |

| Unmarshal `small` | on its own | with odjson | change |
| --- | --- | --- | --- |
| **encoding/json/v2** | 1.910 µs | **601 ns** | **3.18× faster** |
| **encoding/json** | 2.311 µs | **1.500 µs** | **1.54× faster** |
| sonic | 1.023 µs | — | |
| go-json | 799 ns | — | |

`encoding/json/v2` gains on all four, and the generated file is the only thing
that changed. `encoding/json` gains on all four too: its encodes on the direct
path, which learned that library's flag word (see "The direct path"), its
decodes through the public API path (see "What the decode side pays"), and
that is the whole of the difference between the two standard library columns.
The run is from 2026-09-12, after the changes described under "What the other
shapes say"; the run before them had `encoding/json`'s `twitter` encode at
1.04× *slower* (430 µs against 412 µs), which is what those changes removed.
The same comparison shows the `small` decode ratio at 3.18× against the
previous run's 3.32× (562 → 601 ns, while reflection moved 1864 → 1910 ns):
about 3.5% of that is the wider entry check, measured back-to-back on the same
day against the previous build (598 → 619 ns), and the rest is the day and the
binary; `bench/ab` in one process reads 575 ns for the same decode.

Against the libraries people leave the standard library for, that puts
`encoding/json/v2` + odjson:

| | vs go-json | vs sonic |
| --- | --- | --- |
| Marshal `twitter` | **2.17× faster** (113 vs 245 µs) | level (113 vs 116 µs) |
| Marshal `small` | **1.25× faster** (305 vs 380 ns) | level (305 vs 311 ns) |
| Unmarshal `twitter` | **1.33× faster** (516 vs 687 µs) | level (516 vs 526 µs) |
| Unmarshal `small` | **1.33× faster** (601 vs 799 ns) | **1.70× faster** (601 vs 1023 ns) |

Ahead of go-json on all four, the narrowest being 1.25×, the small encode.
Against sonic, three rows sit within 3% in this run, all three in sonic's
favour, and within 5% across runs, which is inside the drift between them —
`bench/ab`, in one process, puts the same three at 1.02× slower, level and
1.02× slower, and the small decode at 1.67× rather than 1.70×. Treat the three
as level and the small decode as a real win either way.

`sonic.Marshal`'s default configuration neither escapes HTML nor validates
UTF-8, so its encode rows are not doing equal work; `sonic.ConfigStd`, which
does both, measures 126 µs and 367 ns, and its `twitter` decode 588 µs.

**On sonic's small decode**, which looks slow next to go-json's: it is real,
and it is not an artefact of how this suite measures. sonic v1.15.3 on Go 1.27
amd64 uses its JIT decoder (`internal/decoder/jitdec`; the `compat` fallback
is gated on `go1.28` and `optdec` on an env var), its per-call floor is 45 ns
on `{}`, and reusing the decode target instead of allocating a fresh one moves
both libraries together (sonic 1.05 µs → 805 ns, go-json 755 → 513 ns) without
changing the order. sonic's advantage on this suite is concentrated in the
616 KiB payload, where it decodes 1.31× faster than go-json; on 340 B of fully
typed JSON, go-json's decoder is simply quicker.

## The public API ceiling

The `json/v2` rows in the tables above are what they are because of the direct
path described below: under a plain `json.Marshal` / `json.Unmarshal` the generated method
appends into, or parses out of, the coder's own buffer, the way `json/v2`'s
reflection codec does, and nothing in `jsontext`'s public API runs at all.
Everything that follows in this section is about the public API path, which
is what `encoding/json` call sites and every non-default situation still use.

For a value-driven `MarshalerTo` on that path, `gen = floor + odjson's own
encode`, so the room left for odjson is `plain - floor`:

| | floor | reflection | room | odjson's part | rate the room demands |
| --- | --- | --- | --- | --- | --- |
| `encoding/json` Marshal `twitter` | 320 µs | 401 µs | 81 µs | 103 µs | 3.2 GB/s |
| `encoding/json/v2` Marshal `twitter` | 352 µs | 396 µs | 45 µs | 107 µs | 5.8 GB/s |
| `encoding/json/v2` Marshal `small` | 784 ns | 996 ns | 212 ns | 241 ns | — |

(The `json/v2` rows here are measured with the direct path compiled out, by
`-tags odjson_safe`, since under a plain `json.Marshal` it never runs the
public API path at all.)

No `MarshalerTo` on the public API can turn those rows into 1.3× wins: a
marshaler that costs nothing still measures 352 µs on `twitter` and 784 ns on
`small` under `json/v2`, and reflection divided by 1.3 is 305 µs and 766 ns.
The two `twitter` ones are also settled by the last column: sonic's AVX2 and JIT compiled encoder writes this
document at 2.2–2.7 GB/s, so a budget of 3.2 or 5.8 GB/s is not a tuning
target, it is outside what any Go encoder does. A token-driven `MarshalerTo`
would not pay the reformat at all, but it pays per member instead, and at this
fixture's density that is no better — see the density note below.

`Marshal small` is arithmetic rather than throughput: of odjson's 241 ns,
about 35 ns is the formatting of the three non-integral floats in the fixture
(11 ns each on the short-decimal path described under "Float formatting";
`strconv`'s shortest formatting, which `json/v2`'s own encoder pays, is 26 ns
each). Fitting would mean halving everything else.

`Unmarshal twitter` was in that list until the generated decoder stopped
consuming unknown members with `Decoder.SkipValue` and started using
`Decoder.ReadValue` instead. The two validate identically — both push an object
namespace and reject duplicate names, including inside a value the target type
never looks at — but `SkipValue` walks the value token by token through the
state machine while `ReadValue` hands it to the raw consumer. On a document
where most members are unknown that was worth a fifth of the decode, and it is
what turned the row from a loss into a win.
`TestDuplicateNamesRejectedLikeJSONV2` in `internal/testfixture/v2parity` is the
guard on that equivalence.

**`encoding/json` gains too.** On Go 1.27 `encoding/json` is implemented on top
of `encoding/json/v2`, so it picks up the generated `MarshalJSONTo` /
`UnmarshalJSONFrom`. Its `Marshal` carries its own flag word (HTML escaping,
legacy error reporting, and so on), which the direct path used to decline, so
an unchanged `json.Marshal` call site was 4% behind reflection on `twitter`
while `json.Unmarshal` was 1.2×–1.6× ahead; the path now recognises that word
and writes `ModeV2HTML` (see "The direct path"), and the encode rows read
**3.2× on both payloads**. The decode rows still measure the public API path, because
`encoding/json`'s flags allow what the strict parsers refuse, and that is the
whole of the remaining difference between the two standard library columns
above.

**sonic and odjson are level on `twitter`, and how.** That payload is
dominated by `interface{}` fields, which no amount of code generation can turn
into static field access — odjson falls back to a hand-written `any` encoder
there, while sonic runs JIT-compiled SIMD; what keeps the encode level is that
the generated encoder never looks at a string twice. `json/v2` refuses invalid
UTF-8, and checking that with `utf8.Valid` after scanning a string for escapes
was a second pass over every byte, a fifth of the encode on a document that is
mostly CJK text; the escape scan now stops at the first non-ASCII byte and a
validator settles the run from there, three bytes at a time for the sequences
CJK is made of and by `utf8.DecodeRune` for anything else. Member names are
appended in pieces the compiler moves inline rather than through `memmove`,
and no float goes through `strconv`: a short decimal is proven by one
division and anything else by a shortest-digit search whose digits are
written as whole words (see "Float formatting").

On the decode side the same fused validation runs inside the string scanner,
and the rest of the profile is the byte oriented decoder's structure: a
decision tree over byte positions tells a struct's member names apart (a
`User` has a dozen that start with `profile_`, and comparing them in turn
was a `memequal` call each), whitespace after a newline is measured without
a data-dependent branch, and the 176 KiB `retweeted_status` member the struct
does not declare is validated in place — every string for UTF-8 and every
object at every depth for a repeated name, with a 256 bit filter per open
object settling most names without a scan. That skip is a quarter of the
decode and runs at 1.8 GB/s; the typed part runs at about 1 GB/s including
the allocations the target type asks for. On the fully typed `small` payload
odjson takes the decode outright, 1.7× ahead of sonic and 1.3× ahead of
go-json.

## What the drop-in path costs, and what was removed

A CPU profile of the drop-in marshal path put **57.6% of the time inside
`jsontext.reformatValue`** — `json/v2` re-parsing the bytes the generated
encoder had just produced — with `utf8.decodeRuneSlow` and `utf8.ValidString`
inside it, validating UTF-8 that odjson had already validated. Four changes
came out of that, all measured:

| change | why it helps |
| --- | --- |
| `MarshalJSONTo` writes in a **stream mode**: no HTML escaping, no UTF-8 validation | `jsontext` does both while reformatting; doing them twice was ~3% of the encode |
| the scratch buffer is a **pooled `*Buffer`**, not a `sync.Pool` of `[]byte` | `Put`ting a slice boxes its header — one allocation per call, 500 of them per encode of a 500-element slice |
| the escape scan is **word-at-a-time** (SWAR over eight bytes) | 14% of the encode was a byte-at-a-time table loop; this cut ~6% off the large payload |
| `UnmarshalJSONFrom` **drives the decoder token by token** on large documents instead of `ReadValue` + reparse | the document was being parsed twice; now it is parsed once, and strings skip odjson's UTF-8 pass because `jsontext` has already made it |

Together those took `encoding/json` from 1.17–1.20× *slower* to
1.08–1.17× *faster*, and `json/v2` from 1.38–1.48× slower to parity on small
documents.

**What is left is not removable through the public API.** `json/v2`'s own
struct encoder calls `xe.Tokens.Last.DisableNamespace()`, appends a precomputed
quoted member name straight into the encoder's buffer, and updates the state
machine inline. None of that is reachable from `jsontext`'s exported surface, so
every `MarshalerTo` pays, per object member, a duplicate-name namespace insert
(20% of the remaining profile) and a whitespace scan (8%) that `json/v2` itself
skips. On top of that, `WriteValue` must validate what it is given, so a
value-driven `MarshalerTo` inherently walks the output twice where reflection
walks it once. On a 616 KiB document that second pass costs about as much as
`json/v2`'s entire reflective encode — which is exactly the residual gap.

A token-driven encoder removes the second pass but pays a per-member cost
instead, so which one wins depends on how many bytes a document carries per
object member. `bench/proto` measures both shapes at two densities: at 16 bytes
per member the token form is 18% slower, at 1600 bytes per member it is 40%
faster. `bench/ab`'s `TestDensity` places the fixtures on that scale — `twitter`
is 255 KiB over 7463 members, or **34 bytes per member**, near the dense end.
Interpolating puts a token-driven encoder at about 1.13× reflection there,
which is where the value-driven form already is. Neither shape wins this
payload, so the simpler one was kept.

## The direct path

`json/v2`'s own codecs never call `WriteValue` or `ReadValue`. They append
into the encoder's buffer, read out of the decoder's buffer, and update the
state machine by hand, through an internal export that the linker refuses to
let any other module reach (`go:linkname` to
`encoding/json/internal.AllowInternalUse` is rejected at link time). The
sections below measure what that costs a `MarshalerTo` / `UnmarshalerFrom`
that stays on the public API: on `twitter`, a floor above reflection divided
by 1.3 for encoding, and a duplicate-name check on every member for decoding.

`odjsonrt/direct.go` reaches the same state without the export. At init it
looks the coder's fields up by name through `reflect`, checks their types,
and learns the option flags of a plain `json.Marshal` / `json.Unmarshal`, from
`encoding/json/v2` and from `encoding/json`, by observing a probe run through
the public API inside the real calls: at the top level, as an array element
and as an object member value, checking each time that the state machine's
entry reads as `jsontext`'s source says (object bit, namespace bits, a count
that the value raises by exactly one). The generated methods then ask
`BeginDirectEncodeMode` / `BeginDirectDecodeAt` whether the situation is one
of the learned ones:

- a **buffered** coder (`json.Marshal` to bytes, `json.Unmarshal` from
  bytes), so nothing has to be flushed or fetched;
- the coder's option flags **equal to one of the two plain calls'**, so no
  indentation or non-default semantics have to be reproduced — and, since
  `json/v2` applies a `,string` or `format` tag to a member's value through
  those same flags, such a member falls back on its own;
- a **value position**: not where an object member name is due, and not a
  namespace `jsontext` has marked invalid.

Depth is not a condition. Under those flags `jsontext`'s rule for one value is
the same everywhere: a `:` before it after a name, a `,` after an earlier
element or member, one more on the innermost entry's count afterwards, and
nothing else in the coder changes. So `BeginDirectEncodeMode` hands back the
buffer with that delimiter already appended, and `BeginDirectDecodeAt` hands
back the input with the offset of the value's first byte — past the
delimiter the decoder's own `PeekKind` consumed when the caller peeked (slice
elements), or past the one it consumes itself otherwise (map values, struct
fields), declining when what it finds is not the delimiter the state calls
for, so that the public path reports the error. A top-level `[]T`,
`map[string]T` or struct holding generated types therefore runs every element
on the direct path; before this the path covered the top-level value only,
and `bench/shapes` measured the loss (`array-items` 0.84×, see below).

When the conditions hold, `MarshalJSONTo` appends the value straight into the
encoder's buffer — under `ModeV2` (json/v2's own escaping, invalid UTF-8
rejected) for a `json/v2` call, under `ModeV2HTML` for an `encoding/json`
one, which is what that call's reformat made of the public path's `ModeStream`
bytes: `<`, `>`, `&`, U+2028 and U+2029 escaped, everything else, an invalid
byte included, as it was, with json/v2's container and omitempty semantics
throughout, since `MarshalJSONTo` has always followed those under either
library. `UnmarshalJSONFrom` parses the decoder's unread input with the byte
oriented `odjsonParseV2`, which validates everything `jsontext` would have:
grammar, UTF-8, unpaired surrogates and duplicate names at every depth. Then
the coder is advanced past the value. `encoding/json`'s decode stays on the
public path: its flags allow invalid UTF-8 and duplicate names, which the
strict parsers refuse, and `TestLenientOptionsReachTheFallback` holds it
there. In every other situation — an `io.Writer` / `io.Reader`, any option —
the methods take the public API path documented below, unchanged.

It is guarded three times over: the layout lookup and type checks at init, a
gate on the Go minor version it was verified against (1.27; a newer toolchain
gets the public API path until the layout is re-verified), and a self test at
init that runs the direct path through `json/v2` and `encoding/json`, at the
top level and nested in a slice, a map and a struct, and compares its answers
with the public API's, including the refusal of trailing data, truncated and
malformed nested input, and `encoding/json`'s escaping of HTML, U+2028 and
invalid UTF-8. Any failure disables it for the process and names the check
that failed; `odjsonrt.DirectEnabled` reports the outcome, and building with
`-tags odjson_safe` compiles it out. The generated code carries the public API
path in every case, so disabling costs speed and nothing else. The
`BeginDirectEncode` / `BeginDirectDecode` pair that generated code before the
nested path called is kept, top-level only as before, so an older generated
file keeps working against a newer `odjsonrt`.

What it is worth (`bench/ab`, medians of 5): `json/v2` goes from
0.86× / 0.97× / 1.02× / 1.45× to **3.52× / 3.61× / 2.05× / 3.24×** on
Marshal `twitter` / Marshal `small` / Unmarshal `twitter` / Unmarshal `small`,
with one allocation per encode; `encoding/json`, whose encode never had the
path before it learned that library's flag word, goes from
0.96× / 1.12× on the two marshals to **3.11× / 3.37×**, while its unmarshals
stay on the public path at 1.20× / 1.57×.

## What the decode side pays, and what was removed

The decoder has a tax of its own. Every object member name that goes through
`jsontext.Decoder`'s public API — `ReadToken`, `ReadValue`, `SkipValue`, or
the names inside a value read whole — is inserted into a per-object namespace
so that duplicates can be rejected, and that namespace is a linear scan over
the names already seen, so an object with *k* members costs *k²/2* string
comparisons. `json/v2`'s own struct decoder switches it off
(`Tokens.Last.DisableNamespace()`) and tracks the fields it knows in a bitset;
that call sits behind `export`, and the linker refuses a `go:linkname` to
`encoding/json/internal.AllowInternalUse`. On `twitter`, whose `User` objects
carry about forty members each, `objectNamespace.insert` is **19% of the
generated decoder's profile** and 6% of reflection's. Together with the
whitespace scan (10%) and the UTF-8 re-validation of non-ASCII strings (8%),
65% of the generated decode happens inside `ReadValue`, and none of that is
reachable from outside the package.

What was left to odjson was measured and cut, all through `bench/ab`:

| change | why it helps |
| --- | --- |
| **small values are read whole** (`odjsonrt.WholeValue`): when the unread buffer is at most 4 KiB and ends in a closing bracket, one `ReadValue` hands the bytes to a byte oriented decoder that trusts jsontext's validation and follows json/v2's semantics (`odjsonParseV2`) | a call per member costs more than a second scan of a few hundred bytes: `small` went from 1.72 µs to 1.40 µs. On `twitter` the same trade loses (1.19 ms against 1.07 ms), because the second scan walks 350 KiB of indentation and every unknown member again, so large values stay token driven |
| **scalar containers are read whole** even inside a token driven object | `[1,2,3]` is one `ReadValue` and a scan, not seven decoder calls |
| the next kind is read from **`UnreadBuffer`** (`odjsonrt.NextKind`) instead of `PeekKind` | `PeekKind` is a full state machine step whose checks the following `ReadValue` repeats; a peek at the buffer costs a few ns and falls back to `PeekKind` only when a streaming decoder has run dry |
| a **string cache** shared by the whole decode, as `json/v2`'s reflection decoder has | a name or value that recurs is allocated once: 13 → 6 allocations on `small`, 4551 → 2977 on `twitter` |
| scalars are parsed **straight from the `ReadValue` bytes**: integers in one pass over the digits, strings with a single `IndexByte` for the escape check | jsontext has already established the grammar, so re-scanning it was pure overhead |

Those took `json/v2` from parity to **1.36× faster** on the small
decode and to 1.03× on `twitter`, and `encoding/json` from 1.19×
to **1.45×** on the small decode. The ceiling on `twitter` is set by the 65%
above: with odjson's own share at zero, the row would read about 1.35×, and
with its share halved again, which is what remains realistic, about 1.1×.

The byte oriented decoder itself — the one behind `UnmarshalJSON`,
the direct path and the whole-value path, so every row above
— was then profiled on `small` and reworked. Its cost was spread thin: a call
per member name, a call per scalar, an allocation per string, and the same
whitespace test several times per value. What was cut, all measured on
`bench/ab`:

| change | why it helps |
| --- | --- |
| **member names are matched against the raw bytes**, quotes included: the generated decoder switches on the byte after the opening quote and compares the candidates as constants, which the compiler turns into word loads | a known name costs one byte switch and one or two compares instead of a scan, a length switch and a `memequal`; an escaped or folded spelling, or an unknown name, falls through to the general path unchanged |
| **bools, integers and floats are decoded inline**: `true`/`false` are word compares, an integer is `odjsonrt.ParseDecimal`, and a float without an exponent whose digits fit the mantissa is one exact division (Clinger's fast path, the same one `strconv` takes after its own scan) | the general parsers stay behind the `else`, so nothing changes for what they reject; a well-formed scalar simply costs no call |
| the **string cache** the json/v2 decoders already had is threaded through this decoder too, and remembers per entry whether it validated the string's UTF-8 | 13 → 6 allocations on `small`; a non-ASCII string that recurs skips its UTF-8 pass, while a decoder run with `AllowInvalidUTF8` can never turn that pass off for anyone else |
| the leading whitespace test is emitted once per member instead of once per fragment; runs of indentation are counted a word at a time (`TrailingZeros64` of the word XOR eight spaces) | one fewer branch per value, and 9% off the whitespace skip on the indented `twitter` document |
| `unquote` copies the runs between escapes whole, validating each run once | an escaped string used to be decoded and re-encoded a rune at a time |

Together those took that decoder from 625 ns to **402 ns** on `small`
and from 553 to 520 µs on `twitter`, and every unmarshal row with
it: `json/v2` from 1.36× to **3.1×** on `small` and to **1.5×** on `twitter`,
`encoding/json` to **1.6×** and **1.2×**, and sonic and go-json from losses
to **1.08×** and **1.02×** on `small` in the same process (re-measured after
the strict skip rework: 1.10× on sonic, and parity at 0.99× on go-json; after
the fused UTF-8 validation and the short-decimal floats, 1.13× and 1.11×; on
the current tree, **1.15×** and **1.12×**).

The whole-value path and the direct path are the reason a generated struct
carries two decoders besides the `encoding/json` one:
`odjsonParseFrom`, which drives the decoder, and `odjsonParseV2`, which parses
bytes under json/v2's rules and rejects what `jsontext` would have. Which one
runs is decided at runtime, per value, by the direct path's checks and then
the buffer test above. A
streaming decoder over an `io.Reader` starts with a 64 byte buffer that rarely
ends in a bracket, so it is driven token by token and keeps its memory bounded;
when a chunk does end in a bracket, `ReadValue` fetches the rest of that value,
which is correct, only buffered rather than streamed.

## What the other shapes say

Everything above was tuned on `twitter` and `small`, so `bench/shapes` asks
whether it generalises: the two fixtures, a compacted `twitter`, 21 synthetic
documents that each change one thing the fixtures hold fixed, and
nativejson-benchmark's `canada.json` and `citm_catalog.json`, gen against
plain in one process. The first run, on 2026-09-11 against the tree as it was
(569737b), found three places where it did not; the fixes that followed are
described after the table, and the table is the run after them, the same day
on the same Ryzen 9 7950X, Go 1.27.1, `-count 6` for the two standard
libraries and `-count 3` for sonic and go-json. The ratio is plain over gen,
so 2× means the generated codec halves the time and anything under 1× means
it is slower than reflection; the figure in brackets is the first run's,
where the two differ by more than the spread. Every row is within ±2% by
`benchstat` except `dense` and `citm` (±4%) and the `canada` plain decode
(±7%), so a ratio between 0.95× and 1.05× is read as level.

| shape | v2 Marshal | v2 Unmarshal | v1 Marshal | v1 Unmarshal |
| --- | --- | --- | --- | --- |
| `twitter` (reference) | 3.76× | 2.13× | 3.23× (0.96×) | 1.27× |
| `small` (reference) | 3.56× | 3.30× | 3.36× (1.12×) | 1.61× |
| `twitter-compact` | 3.76× | 2.30× | 3.22× (0.95×) | 1.30× |
| `page-3k` / `page-12k` / `page-100k` | 2.28× / 2.34× / 2.31× | 2.28× / 2.22× / 2.21× | 2.39× / 2.44× / 2.41× (1.39× / 1.38× / 1.34×) | 1.57× / 1.35× / 1.32× |
| `array-items` (`[]Item`) | 2.10× (0.84×) | 2.11× (1.22×) | 2.21× (1.12×) | 1.32× |
| `array-pages` (`[]Page`) | 2.31× (1.00×) | 2.16× (1.24×) | 2.42× (1.33×) | 1.34× |
| `map-items` (`map[string]Item`) | 1.87× (0.83×) | 1.84× (1.17×) | 1.90× (1.11×) | 1.26× |
| `generic` (`any`) | 1.67× | 1.44× | 2.14× (1.24×) | 2.24× |
| `text-ascii` | 2.23× | 1.79× | 1.41× (0.93×) | 0.94× |
| `text-cjk` (as `twitter`) | 1.71× | 1.93× | 1.25× (0.43×) | 1.05× |
| `text-hangul` | 1.48× | 1.76× | 1.09× (0.44×) | 1.02× |
| `text-latin` | 1.32× (0.77×) | 1.47× (0.96×) | 1.08× (0.46×) | 1.00× |
| `text-cyrillic` | 2.16× (0.81×) | 2.19× (0.90×) | 1.69× (0.45×) | 1.03× |
| `text-emoji` | 1.17× (0.94×) | 1.10× | 0.99× (0.46×) | 0.98× |
| `text-escaped` | 1.46× | 1.25× | 1.09× (0.55×) | **0.82×** |
| `unique-strings` | 2.08× | 1.53× | 1.51× (1.00×) | 1.04× |
| `numbers` | 1.32× (1.14×) | 1.09× | 1.31× (1.15×) | **0.87×** |
| `floats` (synthetic GeoJSON) | 1.53× (1.07×) | 1.96× | 1.55× (1.07×) | 1.32× |
| `canada` | 1.15× (1.01×) | 1.31× | 1.15× (1.02×) | 1.07× |
| `dense` | 4.33× (3.65×) | 3.28× | 4.31× (1.27×) | 1.39× |
| `sparse` | 8.51× | 2.39× | 7.21× (2.70×) | 1.47× |
| `skip` | — | 1.53× (1.68×) | — | 1.37× |
| `citm` | 3.81× | 3.01× | 3.76× (1.14×) | 1.22× |

(`text-emoji` and `text-latin` in the v2 encode column, and `small`, were
measured again after the last of the fixes at `-count 4`: 1.17×, 1.32× and
272 ns; the rest of the column is the `-count 6` run.)

What held from the start: the ratios do not depend on document size
(`page-3k` to `page-100k` are flat), on whitespace (`twitter-compact`,
`page-12k-indented`), on strings repeating (`unique-strings`), on member
density in either direction (`dense`, `sparse`), on unknown members (`skip`),
or on the real corpus whose bytes are structure and integers (`citm`). The
README's numbers were never a property of `twitter.json`.

What did not hold, and what was done about it:

- **A generated type below the top level lost the encode win on json/v2.**
  `[]Item` and `map[string]Item` encoded at 0.84× and 0.83× against
  reflection while the same items behind a top-level object were 2.18×,
  because the direct path covered one top-level value and every element of
  a slice or map went through `MarshalJSONTo` on the public API, whose
  `WriteValue` re-validates and reformats what `odjsonAppend` produced — the
  public API ceiling measured above, seen from the other side. The direct
  path now takes a value at any depth (see "The direct path"): `[]Item` reads
  2.10× / 2.11×, `map[string]Item` 1.87× / 1.84×, and `[]Page` 2.31× / 2.16×.
  The map rows sit below the slice rows because json/v2's reflection writes
  a map's members through its own fast path and the map keys are still its
  work either way.
- **`encoding/json` encode was a loss on any string heavy document.** Every
  `text-*` row was 0.43–0.55× and `twitter` 0.96×: `encoding/json`'s
  `Marshal` calls the same `MarshalJSONTo`, but with its own flag word, which
  the direct path declined, so every v1 encode paid the reformat. The path
  now recognises that word and writes `ModeV2HTML`, which is what the
  reformat made of the public path's bytes. `twitter` reads 3.23×, `small`
  3.36×, the `text-*` rows 0.99–1.69×, and every v1 encode row is now at or
  above 1×. v1 *decode* stays on the public path — its flags allow invalid
  UTF-8 and duplicate names, which the strict parsers refuse — so those rows
  are unchanged, `text-escaped` (0.82×) and `numbers` (0.87×) among them.
- **Non-ASCII text outside the CJK three byte range was slower than
  reflection.** The fused UTF-8 scan in `odjsonrt/utf8.go` settled only
  three byte sequences with leads E1–EC and EE–EF on its own and handed
  everything else to `utf8.DecodeRune` one rune per call: Latin-1 and
  Cyrillic encoded at 0.77× and 0.81×, emoji at 0.94×. It now settles every
  sequence length, four two byte or two four byte sequences per word, a
  word of accented Latin text is taken whole (`swarLatin`), and a lone two
  or four byte sequence among ASCII is settled in place. Cyrillic reads
  2.16× / 2.19×, Latin 1.32× / 1.47×, emoji 1.17× / 1.10×; Latin stays below
  ASCII (2.23×) because an accent every few bytes still ends each word scan
  early.
- **Full precision floats were level, and are not any more.** On 2026-09-11
  `canada` and the synthetic `floats` encoded at 1.01–1.07× on both
  libraries and `numbers` at 1.14×: the short decimal path declined them
  and both sides then ran `strconv`'s shortest formatting, which was the
  whole cost. The three encode rows in the table are from 2026-09-12, after
  `odjsonrt/ftoa.go` stopped calling `strconv` (see "Float formatting"):
  against the tree of the day before, the generated side reads `floats`
  −28%, `numbers` −15% (four `-randlayout` builds a side, five interleaved
  runs, n=20, p=0.000) and `canada` −12% (`-count 5`, one layout each
  side; the round-one binaries had read −17%, and the base side alone moved
  5% between the two builds, which is the layout noise the pooling is
  for), with
  `small` and `twitter` within the noise of their untouched decode rows
  (encode `small` +1.5% / +2.7% at p=0.42 / 0.06 under `json/v2` /
  `encoding/json`, against a v1 `twitter` decode that moved +1.9% at
  p=0.010 without being touched). The short path itself had moved
  the day before, when it started proving a candidate by one division
  instead of a two-word argument and accepting every short decimal rather
  than 93% of them, which is where `small`'s encode went from 290 to 272 ns.

sonic and go-json behave as the floor predicts on every shape: the generated
codec is slower on all 25 encode rows on sonic (0.13–0.63×) and on 24 of 25
on go-json (0.23–0.81×, with `generic` level at 1.07×), and on most decode
rows; the exceptions are the ones the README already names, `small`
(1.13× / 1.07×), and shapes of the same kind, `dense` (1.32× / 1.01×) and
`sparse` (1.16× on sonic), where the document is mostly member names and the
skip-and-validate pass they make before calling `UnmarshalJSON` is cheap
relative to their own decode.

A parallel audit the same day measured the same top-level-only scope from the
other direction (`Marshal([]T)` of twitter 1.23× slower, `MarshalWrite` 1.62×
slower, decode never regressing), so the two runs agreed on the diagnosis;
`MarshalWrite` and every other `io.Writer` / `io.Reader` entry point remain on
the public path by design.

## Which semantics a generated method follows

Each method follows the rules of the interface it implements, rather than
imposing one set on both:

| | `MarshalJSON` / `UnmarshalJSON` | `MarshalJSONTo` / `UnmarshalJSONFrom` |
| --- | --- | --- |
| nil slice / map / `[]byte` | `null` | `[]` / `{}` / `""` |
| `omitempty` on `0` or `false` | omitted | kept |
| HTML characters, U+2028/9 | escaped | not escaped |
| array length mismatch (decode) | padded or truncated | rejected |
| case-insensitive member match (decode) | opt-in (`-case-insensitive`) | no |
| `null` into a scalar, struct, `time.Time` or `,string` field (decode) | left untouched | zeroed |
| map member order | sorted by name | sorted by name |

**Which column a call site lands in is the host library's choice, not
odjson's.** `encoding/json/v2` prefers `MarshalJSONTo` when a type offers
both, and on Go 1.27 `encoding/json` is `encoding/json/v2` — so both standard
library packages take the right-hand column for generated types. The left-hand
column is what a `GOEXPERIMENT=nojsonv2` build sees, and what any library that
only knows `json.Marshaler` gets. If your `encoding/json` call sites depend on
v1's spellings — `null` for a nil slice, a zero dropped by `omitempty`, a
case-insensitive member match — that is the one thing generating a codec does
change, and `-case-insensitive` covers the last of the three.

The map row is the one place the generated code does *not* follow its
interface's rules. `encoding/json/v2` leaves map members in iteration order;
`odjsonAppend` sorts them (`slices.Sort` over the collected keys) in every
mode, so generated output stays byte-stable for a document containing maps
whichever method runs. That is a deliberate departure — byte stability is
worth more to a caller than matching `json/v2`'s non-determinism — and it is
the reason adopting odjson does not disturb golden files or a signed body.

This is verified, not asserted: `internal/testfixture/v2parity` decodes the same
documents into a generated type and an identical reflection-only type and
requires `json/v2` to produce the same bytes for both, and the JSON Test Suite
runs through the generated `UnmarshalJSONFrom` against `json/v2`'s own decoder
for all 318 cases.

## The two third-party libraries

sonic and go-json honour `json.Marshaler` and `json.Unmarshaler` too, so the
generated methods do reach them — the file compiles and behaves correctly
under both. They are in the README as the **yardstick**, not as targets:
odjson does not make them faster, and the reason is arithmetic rather than
tuning, which is why their "with odjson" columns are absent from the
benchmark tables rather than merely unflattering.

`bench/floor` measures what a library charges for routing through those
interfaces at all: its marshaler returns an already encoded document and its
unmarshaler discards its input, so the codec behind the interface costs
**nothing**. That is a lower bound for any possible implementation.

| `twitter` / `small` | interface floor | the library's own path | room left for a codec | odjson's codec through it |
| --- | --- | --- | --- | --- |
| sonic Marshal | 106 µs / 400 ns | 114 µs / 312 ns | 8 µs / **none** | 198 µs / 290 ns |
| go-json Marshal | 344 µs / 676 ns | 243 µs / 421 ns | **none** / **none** | 184 µs / 283 ns |
| sonic Unmarshal | 286 µs / 352 ns | 513 µs / 951 ns | 227 µs / **599 ns** | 474 µs / **534 ns** |
| go-json Unmarshal | 477 µs / 309 ns | 672 µs / 794 ns | 195 µs / **485 ns** | 497 µs / **445 ns** |

(The last column is that library's measurement over generated types minus its
own floor, so it is odjson's share as that host sees it; the two hosts do not
agree exactly, because each does a different amount of work around the call.)

On the marshal measurements the interface floor is at or above what sonic and
go-json cost without it: a `MarshalJSON` that costs literally zero still
loses, because the library has to make an interface call and then re-scan and
copy bytes it did not produce itself (sonic validates the bytes unless told
not to; go-json compacts them unconditionally). No amount of code generation
changes that, and the 8 µs sonic leaves on `twitter` is a fourteenth of what
its own JIT encoder spends.

Decoding is the one place they gain, and only on small documents: on `small`
odjson's decoder fits inside the room left, and `bench/ab` confirms it in one
process — sonic 956 → 831 ns, go-json 799 → 712 ns. On `twitter` the decoder
would have to be 2.1× and 2.5× faster than it is, which is 2.3× faster than
sonic's own JIT-compiled SIMD decoder over a 616 KiB document; not a tuning
target for a pure Go decoder that already runs within 1% of sonic's.

The model is not a guess: `library + odjson = odjson's own codec + floor`
holds on every row, to within the drift between the two processes each row is
built from. sonic's marshal of `twitter` measures 304 µs against a floor of
106 µs; go-json's 528 µs against 344 µs; sonic's unmarshal 760 µs against
286 µs. The floor is real, and it is additive.

So if you are on sonic or go-json, this is the honest summary: odjson has
nothing to offer you but a small-document decode. What the README's chart says
instead is that `encoding/json/v2` **with** odjson lands in the same
neighbourhood as those libraries without them — which is the case for not
taking on a third-party codec in the first place.

If you nonetheless run generated types through sonic, tell it that a
`json.Marshaler`'s output needs no re-validation:

```go
var api = sonic.Config{
	NoValidateJSONMarshaler: true, // odjson's output is already valid JSON
	NoValidateJSONSkip:      true, // and it validates the members it skips
}.Froze()
```

| sonic over generated types | default config | trusting config |
| --- | --- | --- |
| Marshal `twitter` | 304 µs | **209 µs** |
| Marshal `small` | 690 ns | **348 ns** |
| Unmarshal `twitter` | 760 µs | **554 µs** |
| Unmarshal `small` | 886 ns | **544 ns** |

Do **not** also set `CompactMarshaler`: it sounds right for odjson's
always-compact output, but it measures 1.7× *slower* than sonic's default and
2.3× slower than the trusting config above — 520 µs on `twitter`, against
303 µs and 230 µs measured beside it in a run of its own.

## Float formatting

`strconv.AppendFloat(v, 'f', -1, 64)` on a full precision coordinate
measured 33 ns on the 7950X, and a profile of it put the shortest-digit
search itself (Go 1.27's unrounded scaling, `internal/strconv/uscale.go`)
at 20% of that: the rest was `formatBase10` writing digits two at a time
into a scratch buffer, `setDigits` trimming them, and `fmtEFG` copying them
into the output one `append` per byte. The literature on the search
(Ryu, Schubfach, Dragonbox, Tejú Jaguá) would have shaved the 20%;
`odjsonrt/ftoa.go` keeps the same search, done in 6.7 ns with a 701 entry
table (the same range `strconv` covers; the fixed-notation range alone
would need 30), and replaced the 80%. In the
`odjsonrt` benchmarks (Ryzen 9 7950X, Go 1.27.1):

| values | before | after |
| --- | --- | --- |
| 1024 full precision coordinates (`AppendFloatFull`) | 44 ns each (12 of them the short path declining, 32 `strconv`) | 27 ns each (2 of them the one-place round a coordinate pays before the gate turns it away) |
| `40.8`, `-0.1`, `0.1`, `12.99`, `139.69171` (`AppendFloatShort`) | 12 ns each | 10.6 ns each |
| 1024 values in exponent notation, magnitudes 1e-300 to 1e300 (`AppendFloatExp`) | 27 ns each (`strconv`) | 24 ns each |

What it does, and what was measured on the way there:

- **The search is the paper's**, 2–3 128-bit multiplications by a rounded-up
  power of ten with two fraction bits and a sticky bit, and `pow10gen.go`
  writes the 701 entries (p from −350 to 350) the way `strconv`'s generator
  writes its 696; the fixed-notation range alone would need 30. The same
  search serves exponent notation (|v| below 1e-6 or at least 1e21,
  subnormals included, written by a byte loop since a document rarely
  holds one) and `float32`, with the 23 bit mantissa and the wider interval
  of its own. A `float32` skips the short decimal path below: that path
  proves a decimal at float64 precision, and a float32 can have a shorter
  one (1048576.25 is the float32 `1048576.2`). The exponent writer is the
  fixed writer's scratch scheme with the point at a fixed place and the
  exponent's sign and one to three digits assembled in a word without
  branches; a first version that aligned the digit words in registers
  with a funnel shift measured 24 ns for the writer alone, against 15.
- **The digits become ASCII eight at a time** with three multiplications
  (`digits8`: halves, quarters, digits, each split masked so that the shift
  does not mix the lanes), verified against `strconv` on all 10^8 inputs.
- **Nothing shifts a digit into place.** A first writer aligned the 17 digit
  string and spliced the point in with masks and funnel shifts, all
  branch-free, and measured 31 ns per value: about 190 instructions, most of
  them the compiler's guards around variable shifts. The writer that stayed
  takes the integer part as `⌊v⌋` (one conversion; the shortest decimal of
  a float below 2^53 crosses no integer the float does not, since any
  integer between them would be a float nearer the decimal) and the fraction
  as what remains of the digits, and stores each as a fixed-width word ending
  where it must end in a scratch buffer, the fraction first and the integer
  part and the point over its padding, then copies the scratch out in four
  whole words. A value's shape changes offsets, not code paths.
- **Trailing zeros are counted, not divided out.** The search leaves them
  when the interval held a multiple of a hundred, and a short decimal is
  nothing but: 40.8 arrives as 4080000000000000. Dividing by ten in a loop
  cost 14 rounds on that value and put a short decimal at 34 ns; they are
  now the leading zero bytes of the fraction's last word (`LeadingZeros64`
  of a SWAR nonzero-byte mask) and simply left outside the length.
- **Short decimals never reach the search.** Without a short path the
  general writer put `small`'s encode up 9–15%. The path that was there
  before (candidate `round(v·10^f)` for f = 1…7, proven by one division)
  came back with two changes: a gate, one multiplication by 10^7 whose
  product is an integer for every decimal of at most seven places, so a
  full precision value pays two rounds instead of seven before declining
  (it sits after the one-place round rather than in front of it: in front,
  it cost every one-place decimal a round and read `small` +2–3% above its
  floor in the pooled measurement); and a writer that shifts the digits
  into one word a digit at a time and stores it whole, for the numbers of
  at most eight bytes that nearly all of them are. The same digits through two `digits8` words and a splice read
  15.6 ns per value against the old path's 12 on `small`'s three floats, and
  `small` +4%; the word-at-a-time loop reads 10.6 and `small` level.
- **Parity is byte for byte.** `TestAppendFloatMatchesStrconv` runs 55
  million values against `strconv` (every decimal of up to six significant
  digits and seven places with its float neighbours, random bit patterns
  across the fixed range, binade edges, exact ties, the large integers),
  `TestFtoaPow10` recomputes the table with `math/big`, and
  `TestAppendFixedShapes` runs the writer on every digit count and point
  position against a byte loop.

## Measured and rejected, September 2026

A survey of what the fast JSON libraries do (sonic, simdjson, glaze,
yyjson, json-constantiater, the Go 1.27 `strconv`) produced eleven
candidates for the generated code and the runtime. Each was implemented
on its own branch and measured the same way: `bench/gen`, both
`encoding/json/v2` and `encoding/json` rows, every branch built four
times with `-ldflags=-randlayout=N` and the four layouts pooled over five
interleaved runs (n=20 per row), compared to `main` with `benchstat`. The
reason for the pooling is under "Measurement notes". What stayed:

| change | json/v2 rows that moved |
| --- | --- |
| the string stop test (`swarStringStop`, `swarUnsafe`) settles control bytes and quotes in one subtraction, by XORing each lane with 0x02 first | twitter encode -3.0% (and -2.5% under `encoding/json`); the decodes within 1.2% |
| `appendQuotedV2HTML` tests a word for all six escape bytes at once (`swarUnsafeHTML`) | `encoding/json` twitter encode -2.9%; the json/v2 rows, which never run that mode, within 0.6% |
| the generated encoder **opens the object with its first member's name** when no member is conditional, and **folds each string member's quotes into the literals around it** (`{"name":"` … `","next":`), the runtime writing the body alone (`AppendStringBodyChecked`) | small encode **-4.3%** (and -5.5% under `encoding/json`), twitter level; the decodes within 1.2%. Replicated on fresh builds (-4.0% / -5.2% in the first batch). A first cut put the ModeV2 body behind a separate switch function, which made every string two calls deep and read +1.9% on the twitter encode; the body now sits inside the switch function, and the quoted form is quotes written by the caller around the same call |
| a **small nested struct** (at most four members, none conditional, one level) is **spliced into its parent's encoder**, so its braces and quotes join the fusion above (`,"author":{"name":"` is one literal) | small encode **-2.5%** against the quote fusion alone (p=0.015 at n=48; -2.8% under `encoding/json`), twitter level. The call it removes is not the gain: three nested frames measured 0.7 ns, and splicing without the fusion read level |

What did not, with the numbers that decided it (json/v2 rows, p<0.05
unless marked ~):

| candidate | result |
| --- | --- |
| **a 256-bit filter in front of the strict unknown-name check**, so that a document full of unknown members does not pay the linear dedup scan | small decode **+8.1%**, `encoding/json` small **+3.6%**; twitter unmoved, because every `User` name is known and the unknown member's inner names already go through the skip filter. A first cut that kept the filter and the name slice in one struct escaped to the heap (allocations 6 → 11); the flat version still costs more on every object than it saves on the rare unknown-heavy one |
| **learned-order key speculation**: a per-type table remembers which member followed which, and the decoder tries that candidate before the byte switch | small decode **+10.3%**, twitter decode **+3.7%**, `encoding/json` small **+5.1%**. The atomic load and the compare cost more than the switch they were meant to skip, on documents already in declaration order; the shuffled-order case, where it could only gain, was not worth measuring after that |
| **an eight-digit SWAR fold in `ParseDecimal`** | every row ~ (small decode +2.6%, p=0.059). The numbers in both fixtures are short ids; the byte loop is already a handful of cycles |
| **decoding slice elements in place** (`append` a zero element, decode into `&s[len-1]`) instead of into a temporary that is then appended | every row ~. The element copy it removes is a few words per element |
| **opening the object with its first member's name** (`{"name":` as one literal, `}` unconditional) when no member is conditional, instead of writing a comma and patching it into a brace | `encoding/json` small encode -2.8% (p=0.015), json/v2 small -1.8% (p=0.068) in one batch, -4.0% / -3.2% in a second whose untouched v1 twitter decode moved -3.1% at the same time: not separable from layout on its own. Kept only as the base of the quote fusion above, whose literals it completes |
| **glaze-style front hash** for deep member-name trees (a hash of the first eight bytes selecting the candidate) | emitted nothing for the benchmark structs: the deep tree is exactly where the names share their first eight bytes (`profile_background_…`, `profile_sidebar_…`), so the hash cannot separate them. Structural, dropped before measuring |

Not attempted: length-specialised integer formatting for the narrow types
(`int8`, `int16`, `uint16`), which `strconv`'s two-digit tables already
handle in a couple of steps; json-constantiater's opt-in assumption tags;
anything that needs `GOEXPERIMENT=simd`.

### The decode round

A second pass, on the decoders alone, started from a profile of the
`json/v2` rows rather than from the literature: on `twitter` the whitespace
skip was 18% (12% in `skipSpaceSlow`, 6% in the inlined test), the strict
skip of the unknown `retweeted_status` members 30% for 42% of the bytes,
allocation 10%, and the string scan 19%; on `small`, 20% of the row is
outside the generated parser (the `json/v2` machinery, `sync.Pool` for the
string cache) and a further 11% is the six slice allocations the target
type needs. A check of the schema-known decoders in other languages
(System.Text.Json's source generator, DSL-JSON and fastjson2's name hashes,
Utf8Json's automata, glaze's compile time maps and its `minified`,
`null_terminated` and padding options, sonic-rs, Zig's `std.json`, the
Swift Foundation scanner, Jackson 2.18/2.19) and of the 2023–2026 papers
found nothing scalar that the generated decoder does not already do or that
the constraints allow: the remaining techniques either assume something
about the input (no whitespace, a terminator, padding the caller must
provide) or need SIMD. The same measurement procedure as above, every step
against the one before it.

What stayed:

| change | rows that moved |
| --- | --- |
| a **capacity hint per slice field** (`odjsonrt.CapHint`): the field's three decoders remember the largest length it has held and allocate the slice once at that size, coming down again when a decode finds the hint more than four times too large, bounded to 1 MiB per allocation; append growth and slices nested in elements are unchanged | `json/v2` twitter decode **-5.8%** (543 → 512 µs, p=0.000), B/op **-35%**: `Statuses` is 848 bytes, and growing a hundred of them from four copied 124 elements over five allocations. `encoding/json` twitter -1.6% (p=0.015); small and every marshal row within noise |
| the strict decoders' **unknown-name list as shared scratch** (`odjsonrt.UnknownNames`) instead of an `[8][]byte` zeroed on entry to every object, and the matched name no longer stored for an error path that can read it back. Every append is published through the cache (`AddUnknownName`), because a struct member nested after an unknown one takes its own start from the published length: a first cut that grew only the local slice let the nested object write over the outer one's names once the pooled cache was warm, and accepted a duplicate json/v2 rejects. `TestUnknownNamesSurviveNestedObjects` in `v2parity` decodes each case four times for that reason | `json/v2` small decode **-3.5%** and `encoding/json` small **-3.2%** (both p=0.000); twitter within noise. Frames: 416 → 192 bytes for the three-field `Author`, 464 → 224 for `User` |
| the **colon settled inline** after a raw name match (`odjsonrt.AfterName`, inline cost 47): the one space an indented document puts after it no longer reaches `skipSpaceSlow` through a call on every member | `json/v2` twitter -2.5% at p=0.14 on its own: not separable from layout. Kept with the change above, the pair reading twitter -3.3% against their base |

What did not:

| candidate | result |
| --- | --- |
| **indentation verified instead of scanned**: each object learns the newline-plus-spaces run of its first member and checks the rest against it with two masked word compares (`IndentAt`, inline cost 65), falling back to `SkipSpace` on a miss | twitter level (-0.2%), small **+3.0%** (p=0.003): the branch-free two-word path in `skipSpaceSlow` already costs about what the verification does, and the compact document pays for the extra code and three locals. Removed |
| **a wider duplicate-name filter** in `SkipValueStrict`, on the suspicion that 256 bits over forty-member objects scanned often | instrumented instead of built: on twitter the filter hits 346 times for 13,345 names (2.6%), 10,287 length compares in all, a few microseconds of a 510 µs decode. Nothing to widen |
| **choosing the string parser in the generated code** (`if strict { ParseStringStrict } else { ParseStringWith }`) instead of through `ParseStringV2`, a wrapper of two calls the inliner cannot take, so that every string costs one call less | `json/v2` twitter and small both -0.6%, while the marshal rows it cannot touch moved -2%: nothing above the floor. The wrapper's call is not where a string's time goes |

The round as a whole, against `main` in one interleaved run (four layouts
each, n=20): `json/v2` twitter decode **550 → 498 µs (-9.5%)**, small
**667 → 625 ns (-6.3%)**, `encoding/json` twitter -2.4% and small -2.7%,
all at p≤0.002, twitter's bytes per decode -35%. The floor of that run is
the `json/v2` small *encode*, which no decoder change can touch and which
read +3.7%: the generated file changed, and with it the alignment of every
literal after it, as described under "Measurement notes".

What is left on the decode side is what the profile said at the start:
allocation the target type dictates, the string scan, and a whitespace skip
that is already a handful of instructions per run. Each remaining candidate
is under the layout floor, which is why this round stopped here.

### The third decode round

The round after that started from the same profile and found the lever
the previous two had walked past: **bounds tests**. Every word loop in the
scanners read its word as `binary.LittleEndian.Uint64(data[i:])`, which
is two tests per word, the slice against the capacity and then, inside
`encoding/binary`, the sub-slice's length against eight, though the loop
bound had settled both; and every `p < len(data) && data[p]` kept a test
on the index, because the compiler cannot see that `p` is not negative.
The generated decoders had the same shape in every position test. Neither
is visible in a profile, which charges them to the line that carries
them. Each change was measured on its own against the one before it with
a micro-benchmark over the twitter document (`odjsonrt/scan_bench_test.go`:
the strict skip of the 73 `retweeted_status` values, the whitespace runs,
`scanStringStrict` over every string, `skipNonASCII` over every run), and
the stack as a whole with the pooled procedure below. What stayed, in
the order it was built:

| change | measured on its own |
| --- | --- |
| **long member names compared in sixteen byte pieces** (`internal/codegen`): the compiler expands a compare against a constant into word loads only up to sixteen bytes, and `User` has twenty names past that, each a call to `memequal` | the `User` decoder's `memequal` calls 34 → 16 |
| `true`, `false` and `null` as **one word compare** (`isTrue`, `isFalse`, `isNull`) instead of `hasLiteral`'s byte loop, which read 1.8% flat on twitter | — |
| the **colon after a parsed name settled inline** (`AfterName`) in `strictKey`, `ParseKey`, `ParseKeyStrict` and `scanKey`, where the one space after it reached `skipSpaceSlow` on every member | strict skip 124.4 → 122.9 µs |
| the any decoder's **duplicate check folded into the map insert**: whether the map grew says whether the name was new, so a member costs one map operation instead of a lookup at the name and an insert at the value; the frame keeps the name's offset for the error | — |
| **word loads through a sub-slice of exact extent** (`load64`, `load32`): one bounds test per word instead of two | strict skip 122.9 → 114.1 µs, whitespace runs 73.7 → 62.6 µs, `scanStringStrict` 121.3 → 110.3 µs |
| **indices tested against the length unsigned** (`uint(p) < uint(len(data))`) throughout the runtime, and the same emitted by the generator: one compare settles the test and the index | runtime bounds tests 120 → 80, `bench/gen`'s generated file 1106 → 514; whitespace runs 62.6 → 58.7 µs, `scanStringStrict` 110.3 → 108.3 |
| a **short plain string settled on its first word** (`shortString`): when the lowest lane the stop mask reports holds the closing quote, nothing the scan cares about stood before it, so a body of up to seven plain bytes costs no call. It takes the word rather than loading it, because with the load inside it costs 100 against the inliner's 80. A **member name of up to fifteen bytes settled on two words** (`shortName`) the same way: 26% of twitter's names end in the first word and 43% in the second | strict skip 113.2 → 108.0 µs, then 107.3 → 104.0 |
| `skipSpaceSlow` takes the **indent path first**, the space after a colon having lost every caller to `AfterName`, and tests the byte the run ended on with one compare before the table loop; the one-space test stays, after it, for the `", "` of a document written on one line, which a first cut dropped and which then cost +54% on that shape | whitespace runs that still reach it: indented twitter 44.8 → 38.5 µs, the same document rewritten with `", "` 21.6 → 23.1 |
| a **run of CJK text taken word by word** in `skipNonASCII`, without going back through the other scripts' patterns every six bytes | non-ASCII runs 19.1 → 16.5 µs |
| `EndUnknownNames` **skipped for an object that added no unknown name**, which is most of them | — |
| the **encoder's word loops and digit stores given the same treatment**: `load64` in `appendQuotedStream`, `appendStringBodyChecked` and `appendQuotedV2HTML`, sub-slices of exact extent under the float formatter's `PutUint64`, unsigned index tests in the byte loops; bounds tests in the encoder's files 106 → 89 | writing every twitter string once per mode: `ModeHTML` 383 → 326 µs (-14.9%, a byte loop with one test per byte gone), `ModeStream` and `ModeV2` -2%, `ModeV2HTML` level |
| **decoded strings carved out of shared chunks** (`StringCache.alloc`): a string the table does not hold is copied into the cache's current 4 KiB chunk instead of being allocated on its own, escaped strings are unescaped into a scratch buffer the cache keeps and carved from there, and a string longer than a quarter of a chunk keeps its own allocation. The trade, stated on `StringCache`, is retention: a string pins its chunk while reachable, and a pooled cache can pin up to 256 chunks through its table until the pool drops it. Approved as a product decision on 2026-09-12, having been declined before | two layouts, five interleaved runs, against the commit before: `json/v2` twitter decode **442 → 411 µs (-7.1%)**, allocations **2,468 → 1,037**, bytes -2.3%; `encoding/json` twitter -3.4%, small -1.4%; `json/v2` small level (p=0.075). A 16 KiB chunk read the same (-0.7%, p=0.075) and pins four times as much |

What did not:

| candidate | result |
| --- | --- |
| **selecting the byte a whitespace run ended on from the words already loaded**, branch-free, so the test that it starts a token does not wait on a load through the index and a table lookup | whitespace runs 73.7 → 82.4 µs (+11%): the variable shift and the select sit on the dependent chain, where the loads were overlapped by the core |
| **reading the scan words in place through `unsafe`**, which removes the one remaining test | the same as the sub-slice on every micro-benchmark; not kept, so the loads stay portable |
| `shortString` on the **string values the strict skip meets** | strict skip +1.4%: 45% of twitter's string values are longer than fifteen bytes, and those pay for the test |
| `skipStringStrict`'s body spelled into `SkipValueStrict`, one call less per skipped string | strict skip level |

The stack against `main`, pooled over four `-randlayout` builds a side and
five interleaved runs (n=20), in two separate batches: `json/v2` twitter
decode **490 → 447 µs (-8.8%)** after the first seven changes and
**491 → 432 µs (-12.0%)** with all of them, `json/v2` small decode
**634 → 608 ns (-4.2%)** and **631 → 602 ns (-4.7%)**, all at p=0.000.
The `encoding/json` decodes read -5.0% / -4.9% in the first batch and
level / -1.6% in the second, against a floor, from the marshal rows the
stack cannot touch, of +2.6% and -2.3%: the v1 rows sit inside it, the
json/v2 rows well above. The v1 rows are two thirds `encoding/json`'s own
validation pass, which hides what happens behind it: the generated
`UnmarshalJSON` called directly, base against stack over two layouts and
five interleaved runs, reads twitter **434 → 380 µs (-12.5%)** and small
**420 → 401 ns (-4.6%)**, both at p=0.000. On single builds, unpooled, the json/v2 twitter decode
went 474 → 418 µs and the small one 594 → 539 ns.

The whole branch against `main`, four layouts a side and five
interleaved runs (n=20), after the encoder change and the chunks:
`json/v2` twitter decode **499 → 411 µs (-17.6%)**, small **639 → 600 ns
(-6.1%)**, `encoding/json` twitter decode -6.1% and small -2.7% (p=0.057),
the twitter encodes -3.9% (`encoding/json`) and -4.3% (`json/v2`), all
others at p≤0.002; twitter's allocations per decode 2,468 → 1,037. The
small encodes read **+4.5%** in that run, which the encoder change does
not explain: measured on its own against the commit before it, three
layouts a side (n=15), it reads the small encodes +1.1% and +1.4% at
p=0.23 and p=0.15, and the twitter encodes -1.5% and -0.9%. What the
whole branch moves is the alignment of everything after the generated
decoders, which grew and shrank in every fixture; the small encode rows
are where that shows, as they did in the earlier rounds.

What is left is what was left before, minus the tests and the string
allocations: the strict skip is 0.39 ns per byte skipped, the whitespace
skip 2.5 ns per indent run, and of the 1,037 allocations a twitter decode
now makes, 900 are the string boxes, maps, slices and slice headers the
`any` fields dictate, which only an interface built by hand could remove.

## Measurement notes

All figures on this page were re-measured together on an AMD Ryzen 9 7950X,
Linux, Go 1.27.1: `bench/plain` and `bench/gen` at `-count=10` (every row
within ±3% by `benchstat`), `bench/floor` at `-count=6`, and `bench/ab` at
`-count=5`.

The `bench/plain` / `bench/gen` tables are one complete run, not a splice: two
such runs were taken and the second is quoted throughout, because the first
left the `json/v2` `small` encode at ±5% while every row of the second is
within ±3%; a filtered re-run of that one row at `-count=25` read 318.5 ns
±1%, confirming the first run's 334 ns as the outlier. The two full runs agree
within 2% on every row but go-json's `small` encode (378 ns against 421 ns),
which is the noisiest row across runs on this suite.

The `bench/plain` and `bench/gen` tables come from two separate processes,
which is fine for the absolute figures but not for the small differences
between a generated row and its baseline: those are of the same order as the
drift between two runs, and their sign moves with `GOMAXPROCS`. For that
comparison use `bench/ab`, which measures both sides in a single process. Its
verdict on this run (`-count 5`, after the changes described under "What the
other shapes say"): odjson wins every marshal and unmarshal row on
`encoding/json/v2` (3.52× / 3.61× / 2.05× / 3.24×) and on `encoding/json`
(3.11× / 3.37× / 1.20× / 1.57×), and the `small` unmarshal rows on sonic
(1.16×) and go-json (1.06×); it loses the `twitter` rows on both of those and
their `small` marshals. Against sonic's own path it is 1.02× slower on the
`twitter` marshal, level on the `small` marshal (286 vs 285 ns), 1.02× slower
on the `twitter` unmarshal and 1.67× faster on the `small` unmarshal.

With the direct path compiled out (`-tags odjson_safe`), the same process put
`encoding/json/v2` at **0.86× / 0.97× / 1.02× / 1.45×** in the run before the
path was widened — the decode side still wins, the encode side does not. That
is what a Go minor odjson has not verified yet costs, until a release widens
the gate.

**Function and data layout move the small rows by more than most changes
do.** Two builds of the same source that differ only in `-ldflags=-randlayout=N`
read up to 3% apart on the `small` rows, and a branch that changes a
generated file shifts every row it cannot causally touch: in the September
2026 batch two decoder-only branches moved the json/v2 `small` encode by
-3.3% and -3.9% (p≤0.016) and the twitter encode by -1.8%, and two
encoder-only branches moved the `encoding/json` twitter decode by -5.2% at
p=0.000. Function order is what `-randlayout` randomises; the data the
generated file adds (its literals, a table) moves everything after it in
rodata, which no linker flag randomises. So a sub-5% comparison is made
like this: build every side four times with different `-randlayout` seeds,
run the four layouts interleaved across the sides and pool them, and read
a branch's *untouched* rows first — the largest move among them is that
branch's floor, and a row it does touch counts only at p<0.05 and above
the floor. `bench/ab`'s one-process ratio does not escape this: the
generated side and the reflection side share a binary, but not the
alignment of the generated code's own data.

Compare your own with
[`benchstat`](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat) rather than
trusting a single run.

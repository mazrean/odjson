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
Go 1.27.1, every row within ±3% per `benchstat` but the three noted under
"Measurement notes". The tree is `main` at `7d57f14` (2026-09-13), after the
canada round described at the end of this page. `encoding/json` v1's
rows are here rather than in the chart, which is about the `json/v2` story.

| Marshal `twitter` | on its own | with odjson | change |
| --- | --- | --- | --- |
| **encoding/json/v2** | 399 µs | **93 µs** | **4.28× faster** |
| **encoding/json** | 412 µs | **113 µs** | **3.63× faster** |
| sonic | 125 µs | — | |
| go-json | 239 µs | — | |

| Marshal `small` | on its own | with odjson | change |
| --- | --- | --- | --- |
| **encoding/json/v2** | 1.040 µs | **255 ns** | **4.08× faster** |
| **encoding/json** | 1.034 µs | **270 ns** | **3.83× faster** |
| sonic | 313 ns | — | |
| go-json | 390 ns | — | |

| Unmarshal `twitter` | on its own | with odjson | change |
| --- | --- | --- | --- |
| **encoding/json/v2** | 1.073 ms | **370 µs** | **2.90× faster** |
| **encoding/json** | 1.485 ms | **1.144 ms** | **1.30× faster** |
| sonic | 500 µs | — | |
| go-json | 657 µs | — | |

| Unmarshal `small` | on its own | with odjson | change |
| --- | --- | --- | --- |
| **encoding/json/v2** | 1.854 µs | **536 ns** | **3.46× faster** |
| **encoding/json** | 2.256 µs | **1.384 µs** | **1.63× faster** |
| sonic | 1.024 µs | — | |
| go-json | 775 ns | — | |

`encoding/json/v2` gains on all four, and the generated file is the only thing
that changed. `encoding/json` gains on all four too: its encodes on the direct
path, which learned that library's flag word (see "The direct path"), its
decodes through the public API path (see "What the decode side pays"), and
that is the whole of the difference between the two standard library columns.

Against the run quoted here before — the same machine, the tree the fourth
decode round left — the `json/v2` column reads 93 → 93 µs, 253 → 255 ns,
371 → 370 µs and 541 → 536 ns, and reflection 402 → 399 µs,
1.068 → 1.040 µs, 1.089 → 1.073 ms and 1.864 → 1.854 µs. None of those
differences is itself a measurement. The only code between the two trees is the
canada round, whose own interleaved, pooled A/B put the `json/v2` decodes of
these two payloads level, the `encoding/json` decodes at +2.6–3.0% inside a
batch whose untouched `small` marshal rows moved −6 to −7%, which is the width of
that batch's layout floor; the rest is the drift between two separately built
runs, up to 3% on a `small` row here. Read the **ratios** rather than the
differences across runs everywhere on this page, because the baselines move
with them.

Against the libraries people leave the standard library for, that puts
`encoding/json/v2` + odjson:

| | vs go-json | vs sonic |
| --- | --- | --- |
| Marshal `twitter` | **2.57× faster** (93 vs 239 µs) | **1.34× faster** (93 vs 125 µs) |
| Marshal `small` | **1.53× faster** (255 vs 390 ns) | **1.23× faster** (255 vs 313 ns) |
| Unmarshal `twitter` | **1.78× faster** (370 vs 657 µs) | **1.35× faster** (370 vs 500 µs) |
| Unmarshal `small` | **1.45× faster** (536 vs 775 ns) | **1.91× faster** (536 vs 1024 ns) |

Ahead of go-json on all four, the narrowest being 1.45×, the small decode.
Ahead of sonic on all four as well, and — since the encode round — by margins
that survive changing the measurement. `bench/ab`, in one process, puts the
encodes at 1.19× and 1.19× and the decodes at 1.32× and 1.80×. The table's two
encode margins are wider than `ab`'s because both sides' rows land in
different places in the two binaries: sonic's 11–12% apart (125 vs
112 µs, 313 vs 282 ns), odjson's `small` row 7% apart the other way
(255 vs 238 ns). That is the drift between two builds rather than anything
odjson did. So the honest statement
of the encode side is **at least 1.19× on `twitter` and at least 1.19× on
`small`**, against a suite whose two runs can differ by ±5%. Before the encode
round the same rows read 1.09× in the table and 1.08× / 1.05× in `ab`, which
was the honest reading of "level".

`sonic.Marshal`'s default configuration neither escapes HTML nor validates
UTF-8, so its encode rows are not doing equal work; `sonic.ConfigStd`, which
does both, measures 126 µs and 367 ns, and its `twitter` decode 564 µs.

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
| `encoding/json` Marshal `twitter` | 320 µs | 410 µs | 89 µs | 92 µs | 2.9 GB/s |
| `encoding/json/v2` Marshal `twitter` | 360 µs | 396 µs | 36 µs | 83 µs | 7.3 GB/s |
| `encoding/json/v2` Marshal `small` | 773 ns | 1.043 µs | 270 ns | 159 ns | — |

(All three rows are measured with the direct path compiled out, by
`-tags odjson_safe`, since under a plain `json.Marshal` — from either standard
library — it never runs the public API path at all. Floor, reflection and
`gen` all come from one `bench/ab` process so the subtraction is meaningful.)

No `MarshalerTo` on the public API can turn the `twitter` rows into 1.3×
wins: a marshaler that costs nothing still measures 360 µs there under
`json/v2`, against reflection divided by 1.3, which is 305 µs. The last
column says the same thing from the other side — sonic's AVX2 and JIT
compiled encoder writes this document at 2.2–2.7 GB/s, so the 7.3 GB/s the
`json/v2` room demands is not a tuning target, it is outside what any Go
encoder does. The `encoding/json` row's 2.9 GB/s is inside sonic's band, so
the rate alone no longer excludes it; what excludes it is that odjson's own
share measures 92 µs against 89 µs of room — the encode round of September
2026 brought those within 3% of each other, and level is not a win — and the
row is moot in any case,
because that call site now takes the direct path (`ModeV2HTML`, see "The
direct path") rather than this one. A token-driven `MarshalerTo` would not pay
the reformat at all, but it pays per member instead, and at this fixture's
density that is no better — see the density note below.

`Marshal small` is the row that *does* fit, and with room to spare since the
encode round: odjson's 162 ns inside 248 ns of room, of which about 32 ns is
the formatting of the three non-integral floats in the fixture (11 ns each on
the short-decimal path described under "Float formatting"; `strconv`'s
shortest formatting, which `json/v2`'s own encoder pays, is 26 ns each). So
the public API path is worth 1.09× on `small`
(`bench/ab` under `-tags odjson_safe`) — a win, but nowhere near the 4.08×
the tables quote, which is the direct path's.

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
**3.45× on both payloads**. The decode rows still measure the public API path, because
`encoding/json`'s flags allow what the strict parsers refuse, and that is the
whole of the remaining difference between the two standard library columns
above.

**How the `twitter` encode caught sonic, and passed it.** That payload is
dominated by `interface{}` fields, which no amount of code generation can turn
into static field access — odjson falls back to a hand-written `any` encoder
there, while sonic runs JIT-compiled SIMD; what carries the row is that
the generated encoder never looks at a string twice. `json/v2` refuses invalid
UTF-8, and checking that with `utf8.Valid` after scanning a string for escapes
was a second pass over every byte, a fifth of the encode on a document that is
mostly CJK text; the escape scan now stops at the first non-ASCII byte and a
validator settles the run from there, three bytes at a time for the sequences
CJK is made of and by `utf8.DecodeRune` for anything else. Since the encode
round of September 2026 the copy is not a second look either: the scan stores
each word it judges, and the non-ASCII validator stores what it validates, so
a string is read once and written once (see "The encode round"). Member names are
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
odjson takes the decode outright, 1.86× ahead of sonic and 1.46× ahead of
go-json, and on `twitter` it is now 1.33× ahead of sonic as well.

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
0.90× / 1.09× / 1.10× / 1.59× to **4.21× / 4.36× / 2.77× / 3.41×** on
Marshal `twitter` / Marshal `small` / Unmarshal `twitter` / Unmarshal `small`,
with one allocation per encode; `encoding/json`, whose encode never had the
path before it learned that library's flag word, goes from
1.01× / 1.30× on the two marshals to **3.64× / 3.99×**, while its unmarshals
stay on the public path at 1.28× / 1.66×.

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
That ceiling is where the `encoding/json` `twitter` decode now sits — 1.31×
in the tables at the top, 1.29× in `bench/ab` — which is why the decode rounds
that took `json/v2` from 2.15× to 2.85× moved this row only from 1.24× to
1.31×: `json/v2` reads the bytes out of the coder's buffer on the direct path,
while `encoding/json`'s flags keep this one on the public path, against the
ceiling above.

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
the current tree, **1.16×** and **1.09×**).

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
described after the table.

The table below is a fresh run, on 2026-09-13 against `main` at `7d57f14`
— the tree after the canada round, the same code the tables at the top of
this page measure, in the same sitting. Same machine as ever (Ryzen 9 7950X,
Linux, Go 1.27.1), `-count 6` for the two standard libraries and `-count 3`
for sonic and go-json. The ratio is plain over gen, so 2× means the
generated codec halves the time and anything under 1× means it is slower
than reflection. Twenty-two rows of the `-count 6` run fell outside ±3% by
`benchstat` (ten encodes and twelve decodes, on both libraries and both
sides, against six in the previous run on the same idle machine); each was
re-run on its own at `-count 20`, where every one came back inside ±3%
(`text-ascii`'s v2 encode ±4%) and within a few percent of the suite's own
figure — the widest moves were the `text-ascii` v2 encode, 2.83× → 2.65×,
and the `citm` v2 decode, 4.02× → 4.19× — and the re-run figures are the
ones quoted for those rows. A ratio between 0.95× and 1.05× is read as
level.

| shape | v2 Marshal | v2 Unmarshal | v1 Marshal | v1 Unmarshal |
| --- | --- | --- | --- | --- |
| `twitter` (reference) | 4.67× | 2.97× | 3.57× | 1.35× |
| `small` (reference) | 4.37× | 3.59× | 3.90× | 1.66× |
| `twitter-compact` | 4.72× | 2.98× | 3.78× | 1.40× |
| `page-3k` / `page-12k` / `page-100k` | 2.67× / 2.77× / 2.85× | 2.60× / 2.75× / 2.77× | 2.61× / 2.67× / 2.70× | 1.68× / 1.52× / 1.47× |
| `page-12k-indented` | 2.78× | 2.56× | 2.66× | 1.39× |
| `array-items` (`[]Item`) | 2.47× | 2.41× | 2.40× | 1.40× |
| `array-pages` (`[]Page`) | 2.79× | 2.59× | 2.60× | 1.47× |
| `map-items` (`map[string]Item`) | 2.19× | 2.01× | 2.04× | 1.32× |
| `generic` (`any`) | 2.02× | 1.52× | 2.18× | 2.21× |
| `text-ascii` | 2.65× | 2.59× | 1.85× | 1.06× |
| `text-cjk` (as `twitter`) | 1.78× | 2.10× | 1.29× | 1.09× |
| `text-hangul` | 1.60× | 1.84× | 1.06× | 1.11× |
| `text-latin` | 2.13× | 1.91× | 1.34× | 1.07× |
| `text-cyrillic` | 3.36× | 2.68× | 1.78× | 1.06× |
| `text-emoji` | 1.49× | 1.35× | 1.22× | 1.08× |
| `text-escaped` | 2.16× | 1.52× | 1.35× | **0.87×** |
| `unique-strings` | 2.73× | 2.13× | 1.96× | 1.17× |
| `numbers` | 1.51× | 1.91× | 1.52× | 1.16× |
| `floats` (synthetic GeoJSON) | 1.55× | 4.23× | 1.55× | 1.90× |
| `canada` | 1.29× | 4.32× | 1.29× | 2.12× |
| `dense` | 5.36× | 4.05× | 5.30× | 1.45× |
| `sparse` | 11.19× | 2.79× | 8.35× | 1.58× |
| `skip` | — | 1.95× | — | 1.45× |
| `citm` | 4.26× | 4.19× | 4.25× | 1.30× |

**What the September 2026 work did to this table.** Against the 2026-09-11
run, every v2 decode row moved up, which is the decode rounds arriving
everywhere rather than on `twitter` alone: `twitter` 2.13× → 2.97×, `citm`
3.01× → 4.19×, `text-ascii` 1.79× → 2.59×, `dense` 3.28× → 4.05×, `skip`
1.53× → 1.95×, and the three `page-*` sizes 2.2× → 2.6–2.8× together; the
canada round then took the three float rows further than any of those,
`canada` 1.32× → 4.32×, `floats` 2.02× → 4.23× and `numbers` 1.21× → 1.91×
(and 1.09× → 2.12×, 1.35× → 1.90× and 0.91× → 1.16× on `encoding/json`,
whose decode shares the parser). The encode columns moved on the string
heavy rows, which is the word-at-a-time `ModeHTML` scan and then the encode
round's fused scan-and-copy: `text-ascii` 2.23× → 2.65×, `unique-strings`
2.08× → 2.73×, `text-emoji` 1.17× → 1.42× and, against the previous table
(`4777ed8`) rather than the 2026-09-11 run, `text-escaped` 1.74× → 2.16×,
`twitter` itself 3.95× → 4.67×, `dense` 4.37× → 5.36× and `sparse`
8.26× → 11.19× — and down on the rows whose strings are dense non-ASCII
from end to end, which the next paragraph is about. The v1 decode column is flat
to +0.14× outside the float rows,
as it should be — nothing on the September branches touched the public API
path it stays on except the parser. Of the two rows that were below 1×,
`text-escaped` (0.87×) and `numbers` (0.91×) on v1 decode, the canada
round's parser took `numbers` to 1.16×, so `text-escaped` is the one row
left.

**What the encode round cost, which this run is the first to show.** Against
the previous table, measured on `4777ed8` — the tree before the encode round
— the five `text-*` encode rows whose strings are non-ASCII went down on
both libraries while every other encode row went up: on `json/v2`,
`text-cyrillic` 2.37× → 1.74×, `text-cjk` 1.76× → 1.50×, `text-hangul`
1.47× → 1.23×, `text-latin` 1.59× → 1.48×, `text-emoji` 1.54× → 1.42×. That
is not drift. An A/B of that tree's `bench/shapes` binary against this one,
two rounds interleaved at `-count 6` (n=12), puts the generated side at
**+32%** on Cyrillic, **+19%** on Hangul, **+16%** on CJK, **+9%** on emoji
and **+6%** on Latin, with the reflection side level throughout and, in the
same A/B, `twitter` at **−16%**, `twitter-compact` −18%, `text-escaped`
−15%, `text-ascii` −4% and `unique-strings` level; the decodes of the same
shapes read level to +6%. Binaries built at the merges of PR #37 (the encode
round) and PR #39 (the codegen fold) read the same as this tree on those
five rows, to within 3%, so the whole of the move is the encode round's, and
since the ASCII-only rows did not lose, it is the non-ASCII copy rather than
the ASCII scan's stores: `copyNonASCII` stores each word it validates, which
is a win on `twitter`'s runs — 56 bytes on average, among ASCII — and a
loss against the scan-then-`memmove` it replaced on a string that is
nothing but Cyrillic or Hangul from its first byte to its last. The text
round (see "The text round" under "Measured and rejected") took that lever
the same day: the five `json/v2` encode cells above are that round's, from
its own three-way A/B (`4777ed8`, `main` at `088a337`, the round's tree,
two rounds of `-count 6` each) rather than from the suite run the rest of
the table comes from, and against `4777ed8` — the tree before the encode
round — they now read `text-latin` −25%, `text-cyrillic` −28%,
`text-hangul` −8%, `text-cjk` level (+0.5%, p = 0.03) and `text-emoji` +5%
(the rest p < 0.001; n = 12), with the reflection side within ±1% throughout, `twitter` on the
pooled `bench/gen` A/B level on both libraries, and the `encoding/json`
encode column untouched, since that path's appender was not changed. The
emoji row is the one residue of the encode round's trade: an emoji among
ASCII costs its word store in the fused scan, which is the design that won
`twitter` its 16%.

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
  2.47× / 2.41×, `map[string]Item` 2.19× / 2.01×, and `[]Page` 2.79× / 2.59×.
  The map rows sit below the slice rows because json/v2's reflection writes
  a map's members through its own fast path and the map keys are still its
  work either way.
- **`encoding/json` encode was a loss on any string heavy document.** Every
  `text-*` row was 0.43–0.55× and `twitter` 0.96×: `encoding/json`'s
  `Marshal` calls the same `MarshalJSONTo`, but with its own flag word, which
  the direct path declined, so every v1 encode paid the reformat. The path
  now recognises that word and writes `ModeV2HTML`, which is what the
  reformat made of the public path's bytes. `twitter` reads 3.57×, `small`
  3.90×, the `text-*` rows 1.06–1.85×, and every v1 encode row is now above
  1×. v1 *decode* stays on the public path — its flags allow invalid
  UTF-8 and duplicate names, which the strict parsers refuse — so those rows
  are unchanged, `text-escaped` (0.87×) and `numbers` (0.91×) among them
  (`numbers` has since moved to 1.16×, by the canada round's parser, which
  that path shares).
- **Non-ASCII text outside the CJK three byte range was slower than
  reflection.** The fused UTF-8 scan in `odjsonrt/utf8.go` settled only
  three byte sequences with leads E1–EC and EE–EF on its own and handed
  everything else to `utf8.DecodeRune` one rune per call: Latin-1 and
  Cyrillic encoded at 0.77× and 0.81×, emoji at 0.94×. It now settles every
  sequence length, four two byte or two four byte sequences per word, a
  word of accented Latin text is taken whole (`swarLatin`), and a lone two
  or four byte sequence among ASCII is settled in place. Cyrillic reads
  1.74× / 2.68×, Latin 1.48× / 1.91×, emoji 1.42× / 1.35×; Latin stays below
  ASCII (2.65×) because an accent every few bytes still ends each word scan
  early.
- **Full precision floats were level, and are not any more.** On 2026-09-11
  `canada` and the synthetic `floats` encoded at 1.01–1.07× on both
  libraries and `numbers` at 1.14×: the short decimal path declined them
  and both sides then ran `strconv`'s shortest formatting, which was the
  whole cost. The three encode rows in the table — `floats` 1.50×, `numbers`
  1.37×, `canada` 1.16× — postdate `odjsonrt/ftoa.go` dropping `strconv`
  (see "Float formatting"), which was measured on 2026-09-12 like this:
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

sonic and go-json behave as the floor predicts on every shape, in the
2026-09-13 run as in the first one: the generated codec is slower on all 25
encode rows on sonic (0.14–0.78×) and on 24 of 25 on go-json (0.27–0.90×,
`generic` the single exception at 1.10×), and on most decode rows. The decode
exceptions are the ones the README already names, `small` (1.06× on sonic /
1.03× on go-json in this run's `-count 3`; 1.16× / 1.09× in `bench/ab`), and
shapes of the same kind, `dense` (1.44× / 1.17×) and `sparse` (1.42× /
1.10×), where the document is mostly member names and the skip-and-validate
pass they make before calling `UnmarshalJSON` is cheap relative to their own
decode; go-json adds `generic` (1.15×) and, since the canada round, the
three float rows — `floats` 1.96×, `canada` 1.91×, `numbers` 1.14× — whose
one-pass parser its own decoder does not have, and sonic sits level on the
three `page-*` sizes, `array-pages`, `citm` and `skip` (0.97–1.01×). Both
remain far behind what the same decoder does under `json/v2`, which is the
point of the floor section: on `twitter` their decode rows read 0.81× and
0.77× against 2.97×.

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
| sonic Marshal | 111 µs / 394 ns | 125 µs / 313 ns | 14 µs / **none** | 147 µs / 253 ns |
| go-json Marshal | 348 µs / 687 ns | 239 µs / 390 ns | **none** / **none** | 142 µs / 244 ns |
| sonic Unmarshal | 276 µs / 362 ns | 500 µs / 1024 ns | 224 µs / **662 ns** | 345 µs / **516 ns** |
| go-json Unmarshal | 497 µs / 322 ns | 657 µs / 775 ns | 160 µs / **453 ns** | 325 µs / **406 ns** |

(The last column is that library's measurement over generated types minus its
own floor, so it is odjson's share as that host sees it; the two hosts do not
agree exactly, because each does a different amount of work around the call.)

On the marshal measurements the interface floor is at or above what sonic and
go-json cost without it: a `MarshalJSON` that costs literally zero still
loses, because the library has to make an interface call and then re-scan and
copy bytes it did not produce itself (sonic validates the bytes unless told
not to; go-json compacts them unconditionally). No amount of code generation
changes that, and the 14 µs sonic leaves on `twitter` is a ninth of what
its own JIT encoder spends.

Decoding is the one place they gain, and only on small documents: on `small`
odjson's decoder fits inside the room left, and `bench/ab` confirms it in one
process — sonic 977 → 842 ns, go-json 777 → 710 ns. On `twitter` the decoder
would have to be 1.5× and 2.0× faster than it is through those hosts; fitting
sonic's 224 µs of room means decoding the document 2.2× faster than sonic's
own JIT-compiled SIMD decoder does. That is not a tuning target for a pure Go
decoder, even one that is already 1.35× ahead of sonic under `json/v2`, where
it reads the bytes out of the coder's buffer instead of being handed them
twice.

The model is not a guess: `library + odjson = odjson's own codec + floor`
holds on every row, to within the drift between the two processes each row is
built from. sonic's marshal of `twitter` measures 258 µs against a floor of
111 µs; go-json's 490 µs against 348 µs; sonic's unmarshal 620 µs against
276 µs. The floor is real, and it is additive.

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
| Marshal `twitter` | 258 µs | **154 µs** |
| Marshal `small` | 648 ns | **308 ns** |
| Unmarshal `twitter` | 620 µs | **421 µs** |
| Unmarshal `small` | 878 ns | **522 ns** |

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
| 1024 full precision coordinates (`AppendFloatFull`) | 44 ns each (12 of them the short path declining, 32 `strconv`) | 27 ns each (2 of them the one-place round a coordinate pays before the gate turns it away); 25 ns since the canada round below, and 32 rather than 34.5 on the file itself |
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
- **A coordinate is written in three words, none of them loaded back.**
  Since the canada round (see "The canada round" below) a value whose
  integer part has at most seven digits and whose fraction has 8 to 16,
  which is every full precision coordinate, goes to `appendFixedWords`:
  the integer digits with the point behind them, the first f−8 fraction
  digits behind the point, and the last eight at the end, each word stored
  straight into the output over the zero padding of the one before, in
  that order, so that the scratch and its four-word copy-out are gone.
  The integer part below 100 comes from a digit-pair table rather than a
  `digits8` word, and the variable shifts are masked so that the compiler
  drops its guards for a count of 64 or more. The writer alone was level
  (27.0 → 26.8 µs on `AppendFloatFull`, so the store-forwarding stall the
  scratch was suspected of was not the cost); the pair table and the
  masks took it to 24.8 µs, −8%.
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
- **The rounds are not a loop any more.** The canada round (below) found
  that the loop over the places was the formatter's one data-dependent
  branch pattern, and that a real file of coordinates is exactly the
  input that defeats it: three quarters of `canada.json` are within a
  few ulps of a six place decimal, pass the gate, and fail the proof at
  the fifth, sixth or seventh round, while one in ten is a short decimal
  after all. Where the loop exits is data, so the predictors miss once or
  twice per value: the gate cost 8 ns per value on the 1024 values of
  `AppendFloatFull`, which the predictors learn, and 15 ns on the 111k of
  `canada.json`. One and two places are still tried first, a round each,
  since they are what most short decimals have. After that there is one
  more round: the seven place candidate's trailing decimal zeros, four
  divisibility tests done at once (a multiplication by the inverse of the
  odd part, a rotation, a comparison taken as a borrow), say how many
  places the decimal has, and one division proves or refuses that
  candidate; the branch on the proof is the only one left whose outcome
  depends on the value. `BenchmarkAppendFloatCanada`, which reads the
  file from `ODJSON_BENCH_CORPUS`, exists for this: 34.5 → 31.9 ns per
  value, the two place value of `AppendFloatShort` 2 ns cheaper, the
  learned loop of `AppendFloatFull` +0.4 ns.
- **Parity is byte for byte.** `TestAppendFloatMatchesStrconv` runs 55
  million values against `strconv` (every decimal of up to six significant
  digits and seven places with its float neighbours, random bit patterns
  across the fixed range, binade edges, exact ties, the large integers),
  `TestFtoaPow10` recomputes the table with `math/big`, and
  `TestAppendFixedShapes` runs the writer on every digit count and point
  position against a byte loop.

## Float parsing

`strconv.ParseFloat` on a full precision coordinate measured 31 ns on the
7950X, and the decoder around it paid more than that: `ParseSimpleFloat`
read all seventeen digits before finding that they did not fit 2^53 and
declining, `numberLiteral` scanned the literal again to find its end, and
`strconv` scanned it a third time (`readFloat`, 29% of the `canada` decode
on its own) before fitting it. `odjsonrt/atof.go` does the three in one
pass and fits the digits itself, in 11 ns per coordinate:

- **The digits are read eight at a time.** After the point, each word of
  the fraction is tested for digits with one SWAR mask and, when it is all
  digits, folded into the mantissa with three multiplications (pairs,
  quads, the whole); the word that ends the fraction has its digits moved
  to the top and the rest filled with zeros, which fold to the value of the
  digits alone. A fraction of one digit, which most short decimals have,
  takes the byte loop instead: the word's three multiplications cost more
  than its one step. The integer part and the exponent stay byte loops;
  they are one to three digits.
- **The fit is Clinger's when it can be and Eisel-Lemire's otherwise.** A
  mantissa below 2^53 and a decimal exponent within ±22 are both exact
  floats, and one division or multiplication is correctly rounded, as
  before. Anything else is the algorithm of Lemire's "Number Parsing at a
  Gigabyte per Second" (2021), the one `strconv` itself runs after its
  scan: the mantissa normalised to 64 bits times a truncated 128-bit
  power of ten, whose high 54 bits are the result unless the truncation
  could have moved them, in which case the low word of the power is folded
  in. `pow10gen.go` writes that table too, 651 entries (q from −342 to
  308, the whole float64 range) in floor form next to the rounded-up
  entries the formatter's search multiplies by; the two are not
  interchangeable, since each algorithm's proof assumes its own rounding,
  and `TestAtofPow10` recomputes every entry with `math/big`.
- **What it declines, it declines rather than guesses.** A literal that
  lands exactly halfway between two floats as far as the product can tell,
  a subnormal, an overflow, more than nineteen digits, and every malformed
  spelling go to the general path, where `strconv` runs as before and is
  the only thing that produces an error. Every accepted literal is bit for
  bit `strconv.ParseFloat`'s: `atof_test.go` sweeps 200k random literals
  of one to nineteen digits with the point at every position and exponents
  across the range, the decimal expansions of the halfway points of random
  floats to 15–18 digits, every byte that can follow a literal (the word
  loads read past its end), and a canada-shaped ring in place.
- **The product is written out rather than called.** A call, even one
  never taken, gives `ParseSimpleFloat` a frame and a stack check that
  every float would pay; the first version, with `eiselLemire` as a
  function, read 5.5 ns on `40.8` against the old path's 4.4, and 5.8 on
  `1234` against 4.0. Inlining it by hand recovered a little (5.3 and 5.4),
  the rest is the exponent test and the wider fit, and the `small`
  fixture's three short floats are what it costs: see the round.
- **float32 keeps the old path.** Rounding the float64 result to 32 bits
  would round twice; a float32 literal that Clinger's division cannot
  settle exactly goes to `strconv`, as before.

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

| the **any decoder's interface values assembled by hand** (`box.go`): the string and slice headers and the floats an `any` points to are carved from chunks of their own, and the interface is built from the type word of a real `any` and the slot's address, which is what the runtime's conversion does with a fresh allocation each. Checked at init against the conversions, and the `odjson_safe` tag selects those instead. Approved on 2026-09-12 with the chunks | two layouts, five interleaved runs, against the chunks alone: twitter's allocations per decode **1,037 → 475**, `json/v2` twitter decode level (-0.7%, p=0.25), bytes +0.5%; small, which has no `any`, +1.7% at p=0.04 and `encoding/json` twitter +2.6% at p=0.035, both the size of the layout floor. A tiny allocation costs about what the slot and the two words do; what the boxes remove is objects, not time, in a benchmark whose heap is otherwise empty |

| **`ModeHTML` and `ModePlain` scanned a word at a time** (`appendQuoted`): the v1 `MarshalJSON` modes were a byte loop over a table, with a remark that a word scan had once measured no faster; on `appendQuotedV2HTML`'s structure, with `swarUnsafe` or `swarUnsafeHTML` as the mask and encoding/json's U+FFFD for a byte that is not UTF-8, the same oracle tests against `json.Marshal` pass | writing every twitter string once: `ModeHTML` 352 → 229 µs (**-35%**), `ModePlain` 356 → 216 µs (-39%); `AppendStringKinds` ascii -44%, unicode -59%, mixed and escapes -16%, invalid level. In `bench/ab`, one process, n=15: the twitter encode **with odjson under sonic 309 → 273 µs (-11.6%)** and **under go-json 571 → 518 µs (-9.2%)**, both p=0.000; the small encodes level |

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

What is left is what was left before, minus the tests and the
allocations: the strict skip is 0.39 ns per byte skipped, the whitespace
skip 2.5 ns per indent run, and the 475 allocations a twitter decode now
makes are the maps, the `[]any` arrays and the struct slices the target
type dictates.

### The encode round

The rounds before this one were about the decode side. This one is the encode
side, and it is what moved the two marshal rows from level with sonic to
ahead of it. Against `main` at `4c33f05`, both trees built and run
interleaved, five pairs of `-count 2`: `json/v2` marshal `twitter` **−9.50%**
and `small` **−6.77%**, both at p=0.000, with every decode row of the same
pair inside the ±3% the layout floor moves them by. `encoding/json`'s marshal
rows moved −3.48% and −5.06% with them, since the number writer is shared.

**The string body writes as it scans.** Under `ModeV2`, the mode the direct
path writes, `appendStringBodyChecked` used to scan for the next byte needing
attention and then copy the run behind it with `append(dst, src[start:i]...)`,
which is a `runtime.memmove` call per string. The strings a document is made
of are short — twitter.json's 18,099 of them average 20.3 bytes — so that call
is a large part of what a string costs. It now takes room for the whole body
once, `len(src)-i+8`, a word of slack past the end, and stores each word it
loads before judging it: what lands above the first byte that needs attention
is either overwritten by the next store or left in the slack, because source
and destination advance together. Only an escape breaks that lockstep, and
only an escape takes the room again.

Three things were needed before the fusion paid. The first two were measured
on `BenchmarkAppendBodyTwitter`, added to `odjsonrt/scan_bench_test.go` for
this round: it writes every string of the twitter document through the entry
point generated code uses, and it is what any change to the string writer
should be measured with first.

- **The tail.** A string's last one to seven bytes were scanned a byte at a
  time, and a third of all strings are nothing but such a tail. Packing them
  into a masked word needs a shift by a value the compiler cannot bound, and
  Go guards those — the masked form measured *slower* than the byte loop it
  replaced, and the tail then cost as much as the word loop. What works is
  reading whole words that end where the string does: one word read backwards
  from the end for a string of eight bytes or more, whose lanes below the scan
  position can only be bytes already written out, and two overlapping halves
  for a string of four to seven. No shift at all. The micro-benchmark went
  178.6 → 158.7 µs.
- **One mask instead of two.** The scan stops at an escape or at a non-ASCII
  byte, which was `swarUnsafe(w) | w&swarHi`: two maskings, and an `&^ w`
  that clears exactly the high lanes the second term puts back.
  `(cq|e|w) & swarHi` is the same predicate in two operations rather than
  four: 158.7 → 152.0 µs.
- **One call per string.** With the `ModeV2` body behind a function of its
  own, a string cost two calls, the mode switch and the body. Writing the body
  out inside the switch is worth **1.92%** of the `twitter` encode and
  **2.33%** of the `small` one — the same mistake, and the same size, the
  September 2026 survey measured for a quotes-around-body wrapper.

**The non-ASCII run is copied where it is validated.** A quarter of
twitter.json's string bytes are not ASCII, in runs that average 56 bytes with
five of every six bytes in a run of thirty or more, and `skipNonASCII`
validated them without copying, so the encoder read them twice.
`copyNonASCII` is the same word cases with a store added to each; anything the
byte-at-a-time table settles it hands back to `skipNonASCII` and copies what
that validated, rather than carrying a second copy of the table. `twitter`
**−4.18%**, `small` **−1.77%**. What this round was not measured on, and
`bench/shapes` found on 2026-09-13 when the whole table was re-run against
the tree before it: a string that is dense non-ASCII from end to end pays
for the fused copy rather than gaining — `text-cyrillic` **+32%**,
`text-hangul` +19%, `text-cjk` +16%, `text-emoji` +9%, `text-latin` +6% on
the generated side (n=12, interleaved; the two merges after this round read
the same on those rows, so it is this round's), against `twitter` −16% and
`text-escaped` −15% in the same A/B. See "What the other shapes say", and
"The text round" below for what took it back.

**Integer digits go straight into the buffer.** `AppendInt` and `AppendUint`
were `strconv` wrappers, and `strconv` formats into a scratch array of its own
and copies the digits out of it — a call, a store-forwarding stall and a
`runtime.memmove` per number, which was a tenth of the `twitter` encode.
`digits8`, which the float writer already had, turns up to eight digits into
one word, and the digit count says how far to shift its leading zeros off, so
any integer is one to three stores into room taken once: `twitter` **−3.09%**.
Keeping `AppendInt` itself inside the inlining budget mattered as much as the
writer. With a call on each side of its sign branch it cost 133 units, so
every signed member paid two calls; taking the magnitude branch-free
(`(u^mask)-mask`) and passing the sign as a flag costs 78, and that alone is
`small` **−1.91%**.

**Rejected, with numbers:**

- **Unrolling the CJK loop.** Two words a turn (twelve bytes) read `twitter`
  +2.45% and `small` +1.19%. Three words a turn — twenty-four bytes, where
  three byte sequences and words line up again, so the stores do not overlap
  and eight characters are judged in one comparison — read +2.45% and +2.68%.
  The second result is the one that says why: `small`'s only non-ASCII string
  is 31 bytes and never reaches a twenty-four byte loop, so what it measures
  is the cost of the extra code in the function around it. `copyNonASCII` is
  the size it wants to be.
- **A sixteen byte scan in the fused body**, which the two-pass form had:
  level on the micro-benchmark. The same restructuring written with a `goto`
  back to the scan rather than a loop cost 2.3% on top of that — the loop form
  is what the register allocator wants.
- **Reaching `copyNonASCII` sooner**, by testing for a three byte lead ahead
  of the two byte and four byte cases: `small` +1.19% (p=0.06), `twitter`
  level.
- **A three byte case for the end of a non-ASCII run**, which would keep the
  last sequences of every run out of the table path: level on both rows.
- **A nil test inside `AppendAnyMode`**, to spare a call for each of the
  fourteen dynamic members twitter.json declares per status and per user and
  almost never fills: it puts the function at 82 to 104 inline units against a
  budget of 80, so it stops being inlined and the nil case costs a call again
  either way. It was redundant in any case — `internal/codegen` already emits
  `if v.Field == nil { … }` around the call, which is where that test belongs.
- **Writing integral floats and wide fixed integers with the new writer**
  rather than `strconv`: level on both rows (p=0.067 and p=0.245). Kept
  anyway, because it is what leaves the encode path with no `strconv` call in
  it at all.
- **The same fusion for `ModeV2HTML`**, the mode a direct-path `encoding/json`
  marshal writes. It was written — reservation, word stores, the backward-word
  tail, `copyNonASCII` for the runs, the line separators looked for after the
  copy and rewound to — and measured twice, four `-ldflags=-randlayout` seeds
  against four, five runs each (n=20). Both rounds agree on the `twitter` row:
  **−2.99%** (p=0.000) and **−2.86%** (p=0.004), and its untouched floor moved
  +2.0% and +0.6%, so the gain is that or a little more. The `small` row is
  where they disagree: **+2.83%** against an untouched floor of +3.47% in the
  first round, which is level or a shade better, and **+7.66%** (p=0.000)
  against a floor of +1.0% in the second. Level in one measurement and seven
  percent behind in the other is not a state a headline row may be left in. A
  profile puts about a fifth of the second figure inside the appender —
  `copyNonASCII` costs more than `skipNonASCII` plus the copy it replaces when
  a run's last bytes fall into the table path, which is exactly the shape of
  `small`'s one CJK member — and the rest outside it, spread. Two shapes for a
  string shorter than a word were tried inside it: two overlapping halves, as
  the `ModeV2` body uses (**+8.95%** on that row against a level floor,
  because this mode's word test carries five terms rather than two), and a
  byte loop storing as it goes (the +7.66% above). A large encode gaining 3 to
  5% is not worth a small one whose two measurements read level and −7.7%, so
  the mode keeps its two passes. What the attempt left behind is
  `odjsonrt/v2html_test.go`, which drives the appender against what
  `encoding/json` makes of the same literal through a `MarshalerTo` — the
  reformat this mode reproduces — over every prefix of a dozen shapes with one
  byte of each corrupted in turn. That oracle did not exist before and it
  covers the implementation that stayed.
- **Splicing `appendShortFloat` into `AppendFloat`**, the last place where a
  value cost two calls: level on every row pooled over four layouts (n=20),
  with the `encoding/json` `small` row trending −1.6% at p=0.057 and the
  `json/v2` one −0.7% at p=0.239. Not taken: it would cost
  `ftoa_short_test.go`'s pin on which values the short path accepts and
  declines, which is a performance contract nothing else records, and the
  measurement does not pay for it.

What is left, measured by replacing the `ModeV2` body with
`append(dst, src...)` — no scan, no validation, the wrong output and the right
timing — is **28.9 µs** of the `twitter` encode and **36.8 ns** of the `small`
one, against 35.7 µs and 40.8 ns measured the same way earlier in the round,
once the scan and the copy were already one pass. Roughly half of
the `twitter` figure is the non-ASCII validation. Of the rest of that row,
`bytes.Clone` — which `encoding/json/v2`'s `Marshal` makes of the buffer, and
which is not odjson's to remove — is 18%, and the dynamic members are 15%.

`ModeV2HTML`, the mode a direct-path `encoding/json` marshal writes, still
scans and copies in two passes — not for want of trying; see the rejected
list above. Its row moved only by the number writer.

### The second encode round

A round on top of the first one, on `main` at `8a12738`, asked to take the
encode side as far as it goes. What it found is mostly where the floor is.
Every figure is the pooled procedure of the Measurement notes: four
`-ldflags=-randlayout` seeds a side, five interleaved runs, n=20 per row,
the decode rows read as the untouched floor, and a touched row counted only
at p<0.05 and above that floor.

**Where the `twitter` encode goes**, from a profile of the `json/v2` row on
the tree the first round left: the generated encoders and everything they
call are about 70% of the row's wall time; `bytes.Clone`, which
`encoding/json/v2`'s `Marshal` makes of the buffer, and the large allocation
behind it are about 16%; the rest is the encoder pool and the collector's
share of a benchmark that allocates 262 KB per call. Of odjson's part, the
string writer is about 60% (17 µs of it the non-ASCII runs, whose six byte
CJK loop is ALU bound at a little over a cycle a byte), the dynamic members
about a quarter — half of that Go's map iterator over the `[]any` of
`map[string]any` the URL entities decode to, which has no faster public
form, the rest the strings inside those maps — and the integers about a
seventh. The 4,754 value strings hold 200 KB,
an average of 42 bytes, so the string writer's cost is its bytes rather than
its calls: 38% of the strings are under eight bytes and take no loop at all.

**Kept: two-shaped members fold into the literals around them.** A bool, a
nil-able pointer, slice, map, `[]byte` or interface member is written by a
branch, and the literal the encoder carries forward (`pending` in
`encodeStruct`) had to be flushed ahead of it: two appends where one would
do. Such a member is now held back with the text before it and flushed as an
if/else whose arms each write that text, their own shape, and the text that
came due after them, up to the next member's name — `true,"hots":` and
`false,"hots":` for a bool, `[]` or `null` with the mode test for a nil
slice, the opening bracket joined to the name for a non-nil one. The map
key's closing quote and colon join its value the same way, and a struct with
any unconditional member patches its opening brace statically.
`Marshal/encoding-json/small` **−3.85%** (p=0.000); every other marshal row
and every decode row level (`json/v2` `twitter` +0.8% at p=0.095). The
twitter document's 2,791 bools and 1,946 nulls each save an append, but the
arms carry the literals twice and the wide encoders grow by about a quarter
(`Statuses` 7.4 → 9.3 KB of machine code, `User` 9.1 → 11.3 KB; `Book`, with
few such members, by a tenth), and on that row the two cancel.

**Rejected, with numbers:**

- **Testing `ModeV2` first in `appendStringBodyChecked`**, one comparison
  instead of the three a `switch` over the other modes made ahead of it, and
  the `len(src) == 0` test dropped. `BenchmarkAppendBodyTwitter` `v2`
  **−4.55%** — and `v2html` **+5.67%**, though that mode's appender did not
  change (its assembly is byte for byte the same) and the hand-off to it got
  a comparison shorter. Pooled: `json/v2` `twitter` **−2.07%** (p=0.018),
  `encoding/json` `twitter` **+2.04%** (p=0.004), both `small` rows level.
  Four forms of the test (the mode the direct path writes first; `ModeV2HTML`
  first; a five-way `switch` with `ModeV2` explicit; the first form with the
  length test kept) all read the same way on the micro-benchmark, the
  `v2html` loss tracking the size of the change rather than its shape: the
  function shrank by exactly 32 bytes, which is the alignment the linker
  gives a function, and a row whose work is all in a callee moved four
  percent on that alone. Not something a source change controls, so not
  taken; a gain on the primary row bought with the same loss on the
  `encoding/json` one is not a gain.
- **`ModeV2HTML` as one pass**, written a second time: the `ModeV2` body's
  structure (room taken up front, every word stored before it is judged, the
  backward word for the tail), a six-term `swarUnsafeHTMLOrHigh` mask, and
  — to stay clear of what sank the first attempt — the byte loop for a
  string shorter than a word and `skipNonASCII` plus `copyRun` for the
  non-ASCII runs rather than `copyNonASCII`. `BenchmarkAppendBodyTwitter`
  `v2html` **−7.59%** (p=0.002), `v2` −3.18% from the shared prologue. On the
  rows, with the mode test above: every marshal row level (n=20, `encoding/json`
  `twitter` p=0.989); without it, stacked on the fusion alone: level again
  (`encoding/json` `twitter` **−0.2%**, p=0.529; `small` +1.3%, p=0.129). A
  profile of the `encoding/json` `twitter` row shows the appender's share
  fall by a tenth and the difference reappear inside `appendAny`, whose code
  did not change. Not taken: `v2html_test.go`'s oracle passes on it, but
  twice level on the row it exists for is not a state to ship more code in.

**What the floor is.** On the `json/v2` `twitter` row, 30% of the wall time
is `encoding/json/v2`'s own — the clone and the collector — and of odjson's
70%, half is the string writer at about a cycle a byte with a scalar word
scan and a branch-predicted loop exit per string, a quarter is the dynamic
members with the map iterator half of it, and the generated code's own
literals and tests are about 7%. The
`small` row is 277 ns of which 105 ns are odjson's, three strings, five
numbers and eleven member names. Two changes that each moved their
micro-benchmark by four to eight percent moved neither row past the layout
floor. Anything further here is either json/v2's, the runtime's, or SIMD.

### The fourth decode round

A round on top of the third, on `main` at `49531dd` (after the two encode
rounds), asked to take the decode side as far as it goes. Every figure is
the pooled procedure of the Measurement notes: four `-ldflags=-randlayout`
seeds a side, five interleaved runs, n=20 per row, the marshal rows read as
the untouched floor, and a touched row counted only at p<0.05 and above
that floor. The micro-benchmarks are `odjsonrt/scan_bench_test.go`'s,
pooled the same way; `BenchmarkParseStringTwitter`, every string of the
document through `ParseStringStrict` with a pooled cache, was added for it.

**Where the `twitter` decode goes**, from a profile of the `json/v2` row on
the tree the third round left (415 µs): the strict skip of the 73
`retweeted_status` values is 32% of the wall time, of which `strictKey`
(the member names with their duplicate filter) is 12% and the rest the
string scan and the whitespace; `User`'s decoder and what it calls is 29%;
`scanStringStrict` is 16% and `skipSpaceSlow` 8% of the whole; the string
table 6%; the zeroing of the chunks and the `Statuses` array 3%; the
collector about 8% of a benchmark that allocates 247 KB per call (92 KB of
chunks, 88 KB of `Statuses`, 40 KB of `any` maps and arrays, which are 312
of the 475 objects). The document's 18,099 strings hold 369 KB, 58% of its
bytes, at a mean of 20 bytes, 64% of them sixteen bytes or fewer; the
whitespace is 168 KB in 15,481 runs. The `small` row is 600 ns, of which
about 90 ns is the harness (`encoding/json/v2`'s decoder pool and
arshaler), about 100 ns the five slice allocations the type dictates, and
the rest the eleven members and four nested objects.

**Kept: the indent before a member settled without a call.** The
generated decoders emit, at the top of the member loop and of every array
element, SkipSpace's own inline test and behind it
`SkipSpace(data, SkipIndent(data, p))`: `SkipIndent` consumes the newline
and the run of up to sixteen spaces the way `skipSpaceSlow` does and
returns p for anything else, at exactly the inliner's budget, so the test
on the byte it stops at is SkipSpace's; `SkipValueStrict`'s separator loop
does the same. A document with no whitespace pays what it paid. `json/v2`
`twitter` decode **404.8 → 397.3 µs (−1.85%**, p=0.002), `encoding/json`
`twitter` decode **−2.2%** (p=0.021), both `small` rows and all four
marshal rows level; the strict skip micro-benchmark 103.6 → 101.5 µs
(−2.0%). A first form put SkipIndent ahead of SkipSpace's test at every
site: `json/v2` `twitter` −2.17%, but `json/v2` `small` **+4.0%** (p=0.000)
in a run whose untouched `encoding/json` `small` encode read +7.4%, so the
guard was added and the pair re-measured on four fresh seeds; that is the
figure above. This is not the rejected experiment of the first round,
which forced skipSpaceSlow's whole body into every one of the 249 sites.

**Rejected, with numbers:**

- **`strictKey` with `shortName` spelled into it and the filter hash
  taken from the two words the scan loaded** (`oneWordNameHash`,
  `twoWordNameHash`, held to `nameHash` by a test), which removes the
  `shortName` call and `nameHash`'s two loads from every name the strict
  skip meets. Strict skip **106.3 → 109.9 µs (+3.4%**, n=20 pooled); with
  the scan spelled in and the hash still taken from the bytes, +2.6% on a
  single layout. Fewer calls, and slower: the two inlined `shortString`
  bodies in a function that already holds the names list, the level and
  the document cost more in register pressure than the leaf call did.
- **`slot` under the inliner's budget** (cost 99 → 74: the two words read
  in place through `unsafe` on the architectures with an unaligned load,
  an `encoding/binary` spelling elsewhere and under `odjson_safe`, strings
  under four bytes left to the chunk allocator), so that `Make`,
  `MakeValid` and `MakeUTF8` pay no call for the hash. Every string of the
  document through `ParseStringStrict`: **308.6 → 306.0 µs (−0.85%)**,
  0.14 ns a string; the rows level. The call is not where a string's cost
  is — the table load and `memequal` are — and two arch-gated files are
  not worth 0.14 ns.
- **Slices of scalars carved from the string chunks** (`CarveFor`,
  `Carve`, `CarveEnd`: the room reserved off the chunk up front, the
  elements appended into it, the capacity clamped to the length at the end
  and the unused room given back when nothing carved behind it; `[]int`,
  `[]float64` and `[]bool` fields, the pointer-free kinds the decoder
  parses inline). `small`'s allocations 6 → 3 and 487 → 458 B — and
  `json/v2` `small` **+2.8% and +3.5%** on two independent sets of four
  layouts (p=0.000 and 0.001), `encoding/json` `small` +4.7% and +4.3%,
  `encoding/json` `twitter` +5.6% and +3.3%, `json/v2` `twitter` level. A
  noscan allocation of sixteen or thirty-two bytes is a bump of the tiny
  allocator or a size-class free list, about what the two generic calls
  and the reservation bookkeeping cost, so removing three of them buys
  nothing and the code in the loop costs the rest. The string chunks pay
  because a string was an allocation plus a copy plus a collector object
  for each of thousands; a slice field is one per field.

**Not levers, by these numbers.** Assembly or SIMD for the string scan: a
Go assembly call costs more than one SWAR word, and 64% of the strings end
inside two. Zeroing the chunks through a `runtime.mallocgc` linkname: about
1% for a runtime dependency the direct path's gate does not cover. The
harness share of `small`: json/v2's. What is left is what the third round
left, minus one call per member: the strict skip at about 0.38 ns a byte,
the string scan at about a cycle a byte with a call per string longer than
a word, the whitespace at 2.5 ns a run, the 475 objects the target type
dictates, and the collector's share of them.

### The canada round

A round on `main` at `e64880c` (after the re-measure of 2026-09-13), asked
to take `canada.json` as far as it goes. A census of the file first: 2.25 MB,
111,126 numbers, 90% of them seventeen significant digits with fifteen
fraction places, none with an exponent, 46 integers; 55,563 two-element
`[]float64` points in 480 rings. The gen rows read 4.45 ms to encode
(1.17× against reflection) and 8.16 ms to decode on `json/v2` (1.32×),
11.7 ms on `encoding/json` (1.09×). Three commits, in the order they were
measured, each `-count 6` on `bench/shapes` against the base binary, since
the moves are far above layout noise; the untouched rows were then read
with the pooled procedure of the Measurement notes.

**The decode profile said every literal was scanned three times** (see
"Float parsing"): `strconv.readFloat` 29%, `ParseSimpleFloat` declining
23%, `numberLiteral` 7%, the fit itself 12%. The one-pass parser with the
Eisel-Lemire step took the `json/v2` decode from 8.16 to 2.68 ms (−67%)
and `encoding/json`'s from 11.7 to 6.2 ms (−47%); `floats` −48% / −27%,
`numbers` −38% / −21%, `twitter` and `dense` level. sonic's and go-json's
gen rows moved the same way (−58% and −55%) and still trail those
libraries' own decoders (4.0 ms against sonic's 3.4), which is the
skip-and-validate floor of "The two third-party libraries", unchanged.

**The encode profile was the formatter**: `AppendFloat` 88% of the row,
of which `appendFixed` 33% (its `digits8` calls 17%), the short-decimal
gate 30% flat (`appendShortFloat`, the one-place round and the 10^7 gate a
coordinate pays before it is turned away), the search 12%. The
three-word writer of "Float formatting" is what was kept first: −8% on
the formatter, `canada` −3.3% and `floats` −4.2% on both libraries,
`numbers` −1.3%.

**Then the formatter was measured on the file rather than on a loop.**
`AppendFloatFull`'s 1024 values read 24 ns each; the same code on
`canada.json`'s 111k values read 34.5, and the gap closed as the loop
shrank (28.5 ns on 512 of them, 30.5 on 1024, 34 on 8192 and beyond),
which is the branch predictors running out of history. Timing the stages
apart on both streams put the whole of it in `appendShortFloat`, 8 → 15
ns, and a count of its outcomes said why: 81,816 of the 111,126 values
pass the 10^7 gate, because a coordinate produced by arithmetic is a few
ulps off a six place decimal, 73,818 of them then fail the proof at the
sixth round, 7,245 at the fifth, and 11,840 are short decimals accepted at
various rounds. Where the loop exits is data. The single round of "Float
formatting" replaces it: `canada` −11% against `main` on both libraries
(4.54 → 4.04 ms in that batch), `floats` −6.4%, `dense`, whose floats
have two places, −6.7% from the two place round, `numbers` −3 to −4%,
`twitter` level. The three-word writer and the single round together are
what the encode column of the shapes table shows. The marshal rows of the
README's two payloads, pooled again for the single round on their own
(four fresh seeds a side, five runs, n=20): `json/v2` `twitter` and
`small` level (p=0.95 and 0.13), `encoding/json` `twitter` +1.4% (p=0.02)
and `small` level (p=0.12, ±6%); the three one place floats of `small`
and the one of `twitter` take the same first round as before.

**Then the allocations were a third of what was left**: 57,634 per decode,
55k of them the points, and `mallocgc`, `makeslice`, `growslice` and the
collector's share read 15–20% of the profile after the parser. The scalar
slab (see `odjsonrt/slab.go`) took the `json/v2` decode from 2.68 to
2.44 ms (−9%) and `encoding/json`'s from 6.2 to 5.98 (−3.5%), the
allocations to 2,289 and the bytes from 6.35 to 5.51 MiB; `floats` reads
24,002 → 1,052 allocations. The fourth decode round turned this idea down
(see its list below) when it carved the scalar *fields* of a struct:
`small`'s three field slices are three tiny allocations, a bump of the
allocator each, and the carving cost more than it saved. This round
carves only a scalar slice that is not a struct field, the element of a
slice or map, where the count is in the thousands and a field's capacity
hint is not there to help; a field keeps its hint and its own allocation,
and `small`'s rows are not on the path.

**Against reflection, after the three:** `canada` reads 1.28× / 4.52× / 1.28× / 2.13× (v2 Marshal / v2 Unmarshal / v1 Marshal / v1 Unmarshal, against 1.16× / 1.32× / 1.16× / 1.09× in the table above; the encode ratios are the single round's, the decode ratios were measured before it), `floats` 1.57× / 4.15× / 1.57× / 1.91× (from 1.50× / 2.02× / 1.53× / 1.35×) and `numbers` 1.52× / 1.87× / 1.52× / 1.15× (from 1.37× / 1.21× / 1.34× / 0.91×); `numbers` on `encoding/json` decode, one of the two rows that was below 1×, is above it now, since that path's `odjsonParse` runs the same parser. The absolute `json/v2` `canada` rows are 4.04 ms to encode and 2.42 ms to decode against reflection's 5.19 and 10.94; the table rows above are updated to these.

**The untouched rows**, pooled (four `-randlayout` seeds a side, five
interleaved runs, n=20): the `json/v2` decodes are level (`twitter` 390.6 → 389.6 µs, p=0.31; `small` 597 → 603 ns, p=0.10), the `encoding/json` decodes read `twitter` +3.0% (p=0.006) and `small` +2.6% (p=0.000), and the four marshal rows, which nothing on the branch touches (their floats are short decimals, on the path that did not change), read `twitter` level and `small` −6.0% / −7.1% at p≈0.035 with ±3–4% spreads: the batch's layout floor is at least that wide, and the two `encoding/json` decode moves sit inside it, as the same row's −5.2% did under an encoder-only branch in the survey. What the branch demonstrably costs those rows is the wider parser on their three and one short floats: 0.9 ns each in the micro-benchmark, under 0.3% of either row.

**Not taken, with numbers:**

- `eiselLemire` as its own function, the natural shape, costs every float
  a frame and a stack check: 5.5 ns on `40.8` against the old 4.4, 5.8 ns
  on `1234` against 4.0; hand-inlined it reads 5.3 and 5.4, and the
  remaining half nanosecond is the exponent test and the wider fit.
- The three-word writer on its own was level (27.0 → 26.8 µs on the 1024
  coordinates): the scratch's store-forwarding stalls, named as the next
  lever after the formatter round, were not a cost the hardware charged.
  The digit-pair table for integer parts below 100 and the masked shifts
  are the −8%.
- Three branch-free rewrites tried on the same stream while hunting the
  mispredicts, before the stages were timed apart: `numDigits` deciding
  its correction by the sign of a difference and the integer part of up
  to three digits assembled without a branch on its size read +3%
  together on the stream and +6% on the learned loop, since those
  branches are well predicted and the arithmetic is not free; the two
  selects of the shortest search made branch-free, with the nearest
  integer always computed, read −2% on the stream and +9% on the learned
  loop, the cost of the multiplication it no longer skips. Neither kept.
- The single round without its nearness early-out, every value paying
  the divisibility tests and the division, read −4% on the stream and
  +30% on `AppendFloatFull`'s random full precision values, which never
  pass the gate; the early-out costs `canada` a mispredict on one value
  in four and is kept for the files that are not coordinates.
- The generated code still calls `ParseSimpleFloat` and, when it
  declines, `ParseFloat`, which runs the fast path a second time before
  `strconv`. After this round a decline is a halfway case, a subnormal or
  twenty digits, rare enough that splitting a slow entry point out was not
  worth a second exported function.

### The text round

A round on `main` at `088a337` (after the re-measure of 2026-09-13), asked
to take back what the encode round had cost the five `text-*` encode rows
whose strings are non-ASCII from end to end. Where those rows spent their
time first, with a micro-benchmark shaped like them
(`BenchmarkAppendBodyText` in `odjsonrt/scan_bench_test.go`: 96 lines of
at least 80 bytes of one script's words, the corpora `bench/shapes` builds,
through `AppendStringBodyChecked` under `ModeV2`): a line of Cyrillic ran
at 1.25 GB/s, four cycles a byte. Its letters are two bytes and its spaces
one, so `copyNonASCII`'s four-sequence word case took one word of each
word of text and then the two letters before the space failed all four
word tests, fell to `skipNonASCII` — which ran the same four tests again
before its table — and came back through `copyRun`: two calls and some
fourteen failed word tests per fifteen bytes. Hangul was worse for another
reason: `cjkWord` refuses the lead ED, whose second byte is restricted, so
every syllable above U+D000 sent the rest of its run to the table. Japanese
and Korean words of two to five characters between spaces paid a call and a
return to the ASCII scan per word. What the round did:

- **Two byte text is taken a word at a time, at a fixed stride.** The
  `ModeV2` body's two byte branch is now a loop over `swarTwoByte`, which
  judges a word by three exact lane masks — leads (`11xxxxxx`),
  continuations (`10xxxxxx`), and leads below C2 — and one comparison,
  `cont == lead<<8 | carry`: every continuation follows a lead, every lead
  but the top lane's is followed by one, and `carry` says whether the
  previous word ended in a lead that this word's first byte must
  continue. That is what `swarLatin` could not do — it refused a lead in
  the top lane, and Cyrillic puts a sequence across every other word
  boundary, which is why putting it in the scan loop had lost on Cyrillic
  before. The loop advances eight bytes whatever the word held; a first
  version advanced seven when the top lane was a lead, and read 4.3 µs on
  the Cyrillic micro against 3.1 µs at the fixed stride, because a stride
  that depends on the word's contents puts the next load behind the
  judgement of the previous one. When the loop stops with a lead owed it
  steps back onto it, its store standing, and the byte loop for the last
  seven bytes judges the pair. The helper returns the lead mask and a
  "bad" word rather than a verdict, at a cost of 59, so that it inlines;
  as one function with the escape test folded in it cost 100 and was not
  inlined, and cost 4.3 µs against 3.1 µs. Cyrillic **6.65 → 3.09 µs**
  (−54%), Latin **4.53 → 3.05 µs** (−33%) on the micro, against 4.68 and
  4.29 µs on `4777ed8`.
- **`copyNonASCII` takes the words between runs of three byte text.**
  Its `cjkWord` loop is as it was, six bytes a turn, and where it stops the
  word is judged by `swarMixed` and `swarMixedRange` — a lane classifier
  for two and three byte sequences among ASCII, the same carried-lead
  idea with a carry of one or two lanes, E0 and ED tested by bit 5 of the
  second byte as `cjkWord` tests them and refused in the top lane where
  that byte is out of reach, the escape test `swarUnsafe`'s — and the next
  word goes back to the `cjkWord` loop when it starts on a three byte
  lead, or on through the same test when it does not; the first word with
  no non-ASCII byte in it returns to the caller's scan. Split in two so
  that each half inlines (68 and 56 against the budget of 80): as one
  function of cost 160, called per word, the Japanese micro read 5.9 µs
  against 5.0. CJK **5.73 → 4.91 µs** (−14%), Hangul **7.37 → 5.42 µs**
  (−27%) on the micro, against 4.96 and 6.10 µs on `4777ed8`.
- **The last sequences of a string come from four byte loads.** Most of
  `twitter.json`'s runs end at the end of the string, in the seven bytes
  the word loops cannot reach, and those went to the table and `copyRun`
  one byte at a time; `cjkSeq`, the `cjkWord` test with the upper lanes
  filled in, settles up to two of them from a `load32` first.
- **The lone four byte sequence is judged from one load.** The hit path's
  test for an emoji among ASCII was three indexed continuation checks; it
  is one masked compare on a `load32` now, emoji **3.83 → 3.66 µs** on the
  micro.

**What the rows say.** Three-way `bench/shapes` A/B, `4777ed8` against
`main` against this tree, interleaved, two rounds of `-count 6` (n = 12),
`json/v2` encode, generated side: against `main`, `text-cyrillic`
**−45.8%**, `text-latin` **−30.3%**, `text-hangul` **−21.9%**, `text-cjk`
**−13.2%**, `text-emoji` −1.2%, `text-escaped` −0.9%, `unique-strings`
+0.5%, `text-ascii` +1.1%; against `4777ed8`, −27.7%, −24.8%, −7.8%,
+0.5% and +4.7%. The reflection side read within ±1% on every row, and the
`encoding/json` column, whose appender was not touched, moved −0.9% to
+3.5% — the layout of a binary whose `copyNonASCII` grew, since the
`skipNonASCII` those rows call is byte for byte what it was. The pooled
`bench/gen` A/B (four `-randlayout` seeds a side, five runs, n = 20):
`json/v2` `twitter` **+0.2%** (p = 0.55), `small` +0.9% (p = 0.43),
`encoding/json` −0.5% (p = 0.18) and −1.5% (p = 0.48) — level, all four.
The single-layout shapes binary read `twitter` +2.2% and `twitter-compact`
+2.8% on `json/v2` in the same A/B, which is what a single layout does
(see "The second encode round"); the pooled figure is the one to trust.

**Rejected, with numbers:**

- **Widening `cjkWord` to E0 and ED in the hot loop.** The second-byte
  test costs five operations in the six byte loop `twitter.json` spends
  its string time in: Japanese micro **+15%**, `twitter` micro +5%, and
  Hangul only −4.5%, because a word of Korean between two spaces is still
  a call per word. The test lives in `swarMixedRange` instead, where a
  word that has already left the loop pays it.
- **Staying in the mixed loop once entered.** The first form judged every
  word after the `cjkWord` loop by `swarMixed` until a word with no
  non-ASCII byte came: `twitter` micro **+9.7%** across four layouts.
  Tweets put a digit or a bracket between two stretches of Japanese every
  few dozen bytes, and from then on a dense run cost sixty operations a
  word instead of eight per six bytes. The loop returns to `cjkWord`
  whenever a word starts on a three byte lead, and the mixed test is
  gated on the word's first byte, so a word of pure Japanese never pays
  it.
- **Carrying the loaded word around the outer loop.** With `w` loaded at
  the bottom of the loop and judged at the top, the `cjkWord` loop grew
  four register moves a turn over `skipNonASCII`'s: `twitter` micro
  +2.5%. Loaded inside the loop, as `skipNonASCII` does, and again after
  it, the moves are gone.
- **The escape test after the structure tests.** A run of `twitter.json`
  that ends on a line break went through `swarMixed` and
  `swarMixedRange` before `swarUnsafe` reported the `\n`; with
  `swarUnsafe` first the `twitter` micro came from +2.5% to +1.5%, and
  the four byte loads for the string's last sequences to +1.4% (p =
  0.065), which is the residue the pooled rows cannot see.

What is left on these rows: the emoji row's remaining 5% against
`4777ed8` is the word store an emoji among ASCII pays in the fused scan,
the design that won `twitter` its 16%; `swarMixed` refuses four byte
sequences, so a document of dense emoji still goes through the table;
and the mixed loop costs some sixty operations a word where the two byte
loop costs twenty, because it classifies three sequence lengths at once.

## Measurement notes

The measured tables, the ratio tables, the floor figures and the shapes
table on this page were re-measured together on 2026-09-13 on an AMD Ryzen 9
7950X, Linux, Go 1.27.1, on `main` at `7d57f14`, the tree after the canada
round: `bench/plain` and `bench/gen` at `-count=10`, `bench/floor` at
`-count=6`, `bench/ab` at `-count=5`, `bench/ab` again under `-tags
odjson_safe` at `-count=5`, and `bench/shapes` at `-count=6` for the two
standard libraries and `-count=3` for sonic and go-json, each binary built
once and run alone, one after another, on an otherwise idle machine. The
five `json/v2` encode cells of the shapes table's `text-*` rows whose
strings are non-ASCII are the exception: they are the text round's, from
its own three-way A/B on the same machine (see "What the other shapes
say"). The percentages inside the round sections are each round's own
interleaved A/B, taken on that round's tree; a percentage between two
separately built binaries is not one this page trusts, and none is quoted.

The `bench/plain` / `bench/gen` tables are one run. Every row of it is within
±3% by `benchstat` except three: `bench/gen`'s `json/v2` `small` marshal
(±16%: two of its ten samples read 297 and 300 ns where the other eight lie
between 253 and 256), `bench/plain`'s sonic `twitter` marshal (±6%: its ten
samples fall in two clusters, 117–118 and 124–131 µs) and `bench/floor`'s
sonic `twitter` unmarshal floor (±7%). Re-run on their own at `-count 20`
they read 263 ns ±1%, 120 µs ±10% (the same two clusters) and 291 µs ±2%.
The tables quote the suite's medians — 255 ns, 125 µs and 276 µs — because
the re-runs disagree with them by about what two runs of this suite disagree
with each other (3–6% on those rows, 12% on `sonic.ConfigStd`'s `twitter`
marshal, which read 126 µs in the suite and 141 µs beside the re-run), so a
second process is a different measurement rather than a better one; the
sonic encode margin above is stated as the weaker of the two methods for
that reason. `bench/floor`'s `json/v2` marshal floor reads 359 µs and sonic's
111 µs against sonic's own 112 µs in `bench/ab`'s process, so its marshal
still leaves a few µs of room rather than none.

The `bench/plain` and `bench/gen` tables come from two separate processes,
which is fine for the absolute figures but not for the small differences
between a generated row and its baseline: those are of the same order as the
drift between two runs, and their sign moves with `GOMAXPROCS`. For that
comparison use `bench/ab`, which measures both sides in a single process. Its
verdict on this run (`-count 5`): odjson wins every marshal and unmarshal row
on `encoding/json/v2` (4.21× / 4.36× / 2.77× / 3.41×) and on `encoding/json`
(3.64× / 3.99× / 1.28× / 1.66×), and the `small` unmarshal rows on sonic
(1.16×) and go-json (1.09×); it loses the `twitter` rows on both of those and
their `small` marshals. Against sonic's own path it is 1.19× faster on the
`twitter` marshal, 1.19× faster on the `small` marshal, 1.32× faster on the
`twitter` unmarshal and 1.80× faster on the `small` unmarshal — the same four
signs the tables above report, with the two encode margins smaller, by the
distance between where the two binaries put sonic's rows and odjson's.

With the direct path compiled out (`-tags odjson_safe`), the same process puts
`encoding/json/v2` at **0.90× / 1.09× / 1.10× / 1.59×**: the decode side still
wins and so does the `small` encode, by the headroom the ceiling section
measures, while the `twitter` encode is a 1.11× loss. That is what a
Go minor odjson has not verified yet costs, until a release widens the gate.

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

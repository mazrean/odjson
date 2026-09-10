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
Go 1.27.1, all within ±3% per `benchstat`. `encoding/json` v1's rows are here
rather than in the chart, which is about the `json/v2` story.

| Marshal `twitter` | on its own | with odjson | change |
| --- | --- | --- | --- |
| **encoding/json/v2** | 399 µs | **112 µs** | **3.56× faster** |
| **encoding/json** | 412 µs | 430 µs | 1.04× slower |
| sonic | 114 µs | — | |
| go-json | 243 µs | — | |

| Marshal `small` | on its own | with odjson | change |
| --- | --- | --- | --- |
| **encoding/json/v2** | 1.060 µs | **319 ns** | **3.32× faster** |
| **encoding/json** | 1.027 µs | 884 ns | 1.16× faster |
| sonic | 312 ns | — | |
| go-json | 421 ns | — | |

| Unmarshal `twitter` | on its own | with odjson | change |
| --- | --- | --- | --- |
| **encoding/json/v2** | 1.080 ms | **508 µs** | **2.12× faster** |
| **encoding/json** | 1.464 ms | 1.213 ms | 1.21× faster |
| sonic | 513 µs | — | |
| go-json | 672 µs | — | |

| Unmarshal `small` | on its own | with odjson | change |
| --- | --- | --- | --- |
| **encoding/json/v2** | 1.864 µs | **562 ns** | **3.32× faster** |
| **encoding/json** | 2.271 µs | 1.463 µs | 1.55× faster |
| sonic | 951 ns | — | |
| go-json | 794 ns | — | |

`encoding/json/v2` gains on all four, and the generated file is the only thing
that changed. `encoding/json` gains on three of the four; its `twitter` encode
is 4% behind, and that loss is structural — v1 configures its coders with its
own flags, which the direct path declines.

Against the libraries people leave the standard library for, that puts
`encoding/json/v2` + odjson:

| | vs go-json | vs sonic |
| --- | --- | --- |
| Marshal `twitter` | **2.16× faster** (112 vs 243 µs) | level (112 vs 114 µs) |
| Marshal `small` | **1.32× faster** (319 vs 421 ns) | level (319 vs 312 ns) |
| Unmarshal `twitter` | **1.32× faster** (508 vs 672 µs) | level (508 vs 513 µs) |
| Unmarshal `small` | **1.41× faster** (562 vs 794 ns) | **1.69× faster** (562 vs 951 ns) |

Ahead of go-json on all four, the narrowest being 1.32×, shared by the small
encode and the large decode. Against sonic, three rows sit within 2% in one
direction or the other in this run, and within 5% across runs, which is inside
the drift between them — `bench/ab`, in one process, puts the same three at
1.03× slower, 1.04× slower and 1.02× faster, and the small decode at 1.62×
rather than 1.69×. Treat the three as level and the small decode as a real win
either way.

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
about 50 ns is the formatting of the three non-integral floats in the fixture
(16 ns each on the short-decimal path described below; `strconv`'s shortest
formatting, which `json/v2`'s own encoder pays, is 26 ns each). Fitting would
mean halving everything else.

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
`UnmarshalJSONFrom` — and with those tuned for the streaming contract (see
below) an unchanged `json.Marshal` / `json.Unmarshal` call site is
**1.2×–1.6× faster** on three of the four measurements, and 4% behind on the
fourth. `encoding/json` configures its coders with its own flags (HTML
escaping, legacy error reporting), which the direct path declines, so those
rows measure the public API path — which is the whole of the difference
between the two standard library columns above.

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
and a float that was a short decimal before it was parsed is printed as that
decimal, proven with exact arithmetic rather than found with Ryu.

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
odjson takes the decode outright, 1.7× ahead of sonic and 1.4× ahead of
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
and learns the state machine's values around a single top-level value by
observing a probe run through the public API inside real `json.Marshal` and
`json.Unmarshal` calls. The generated methods then ask `BeginDirectEncode` /
`BeginDirectDecode` whether the situation is the one that was learned:

- a **top-level** value (`StackDepth() == 0`, state machine at its initial
  entry), so no delimiter and no name stack is involved;
- a **buffered** coder (`json.Marshal` to bytes, `json.Unmarshal` from
  bytes), so nothing has to be flushed or fetched;
- the coder's option flags **equal to a plain call's**, so no indentation,
  escaping or legacy semantics have to be reproduced.

When all three hold, `MarshalJSONTo` appends the value straight into the
encoder's buffer under `ModeV2` (json/v2's own escaping, invalid UTF-8
rejected) and `UnmarshalJSONFrom` parses the decoder's unread input with the
byte oriented `odjsonParseV2`, which validates everything `jsontext` would
have: grammar, UTF-8, unpaired surrogates and duplicate names at every depth.
Then the coder is advanced past the value. In every other situation — a
nested value, an `io.Writer` / `io.Reader`, any option, `encoding/json` — the
methods take the public API path documented below, unchanged.

It is guarded three times over: the layout lookup and type checks at init, a
gate on the Go minor version it was verified against (1.27; a newer toolchain
gets the public API path until the layout is re-verified), and a self test at
init that runs the direct path through `json/v2` and compares its answers with
the public API's, including rejection of trailing data. Any failure disables
it for the process; `odjsonrt.DirectEnabled` reports the outcome, and building
with `-tags odjson_safe` compiles it out. The generated code carries the
public API path in every case, so disabling costs speed and nothing else.

What it is worth (`bench/ab`, medians of 5): `json/v2` goes from
0.86× / 0.97× / 1.02× / 1.45× to **3.42× / 3.42× / 2.02× / 3.09×** on
Marshal `twitter` / Marshal `small` / Unmarshal `twitter` / Unmarshal `small`,
with one allocation per encode.

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
verdict on this run: odjson wins every marshal and unmarshal row on
`encoding/json/v2` (3.42× / 3.42× / 2.02× / 3.09×), every `encoding/json` row
except the `twitter` marshal, and the `small` unmarshal rows on sonic (1.15×)
and go-json (1.12×); it loses the `twitter` rows on both of those and their
`small` marshals. Against sonic's own path it is 1.03× slower on the `twitter`
marshal, 1.04× slower on the `small` marshal, 1.02× faster on the `twitter`
unmarshal and 1.62× faster on the `small` unmarshal.

With the direct path compiled out (`-tags odjson_safe`), the same process puts
`encoding/json/v2` at **0.86× / 0.97× / 1.02× / 1.45×** — the decode side
still wins, the encode side does not. That is what a Go minor odjson has not
verified yet costs, until a release widens the gate.

Compare your own with
[`benchstat`](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat) rather than
trusting a single run.

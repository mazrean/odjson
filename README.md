# odjson

[![CI](https://github.com/mazrean/odjson/actions/workflows/ci.yml/badge.svg)](https://github.com/mazrean/odjson/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/mazrean/odjson.svg)](https://pkg.go.dev/github.com/mazrean/odjson)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](./LICENSE)

**odjson** is a CLI code generator that makes JSON encoding and decoding in Go
faster, alongside whichever JSON library you already use.

## Why

A Go struct already tells you everything about its JSON shape. Its field tags
say exactly which JSON member names are acceptable, in what order fields are
written, and what type each one has — all of it known at compile time.

Runtime JSON libraries throw that information away and rediscover it with
reflection on every call. odjson does the opposite: it reads your struct
definitions ahead of time and writes out dedicated, reflection-free code for
each one.

- **Marshal** — generated code appends struct fields directly to a byte slice.
  Field names, quoting, and separators are constants baked into the generated
  source. (Same core idea as
  [`sapphi-red/json-constantiater`](https://github.com/sapphi-red/json-constantiater).)
- **Unmarshal** — generated code is a specialised parser for that one struct.
  It streams the input straight into the struct's fields: no reflection, no
  intermediate `map[string]any`, no per-field name lookup at runtime.

## It sits alongside your JSON library

odjson does not replace your JSON library — it gives you a faster entry point
for the types that matter. For each struct `T` it emits `MarshalT`,
`AppendT` and `UnmarshalT`, ordinary functions you call at the boundary where
JSON enters and leaves your program. Everything else keeps going through the
library you already have.

With `-methods` it additionally emits the standard marshaling interfaces, which
every major library honours:

| Library                                                           | Interfaces implemented                |
| ----------------------------------------------------------------- | ------------------------------------- |
| [`encoding/json`](https://pkg.go.dev/encoding/json)                | `json.Marshaler` / `json.Unmarshaler` |
| [`encoding/json/v2`](https://pkg.go.dev/encoding/json/v2)          | `MarshalJSONTo` / `UnmarshalJSONFrom` |
| [`github.com/bytedance/sonic`](https://github.com/bytedance/sonic) | `json.Marshaler` / `json.Unmarshaler` |
| [`github.com/goccy/go-json`](https://github.com/goccy/go-json)     | `json.Marshaler` / `json.Unmarshaler` |

Then an unchanged `json.Marshal(v)` — whichever `json` that is — dispatches into
the generated code. That is convenient, but it is **not** the fast path: the
interface contract makes the host library re-scan and copy what the generated
codec produces. `-methods` is off by default for that reason; the
[benchmarks](#benchmarks) show the cost.

## Install

### As a project tool (recommended)

Pin odjson to your module so everyone — and CI — generates with the same
version:

```sh
go get -tool github.com/mazrean/odjson@latest
go tool odjson -h
```

### As a standalone binary

```sh
go install github.com/mazrean/odjson@latest
```

### Homebrew (macOS)

```sh
brew install --cask mazrean/tap/odjson
```

### WinGet (Windows)

```sh
winget install mazrean.odjson
```

(available once the manifest PR to `microsoft/winget-pkgs` is merged)

### Linux packages

Release assets include `.deb`, `.rpm` and `.apk` packages for `amd64` and
`arm64`. There is no hosted apt/yum/apk repository — download the asset for
your release and install it directly:

```sh
# Debian / Ubuntu
curl -LO https://github.com/mazrean/odjson/releases/latest/download/odjson_<version>_linux_amd64.deb
sudo dpkg -i odjson_<version>_linux_amd64.deb

# RHEL / Fedora / openSUSE
sudo rpm -i odjson_<version>_linux_amd64.rpm

# Alpine
apk add --allow-untrusted odjson_<version>_linux_amd64.apk
```

Prebuilt archives (`tar.gz`, and `zip` on Windows) for linux/darwin/windows on
amd64/arm64 are attached to every
[release](https://github.com/mazrean/odjson/releases), with a `checksums.txt`.

## Usage

Put a `go:generate` line next to the structs you want accelerated:

```go
package model

//go:generate go tool odjson -type User,Post

type User struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email,omitempty"`
}
```

Then:

```sh
go generate ./...
```

odjson writes `odjson_gen.go` beside the source, containing the codec for each
type. Commit it; it is ordinary Go code with no build tags required.

Then call the generated functions where the JSON crosses your program's
boundary:

```go
func handler(w http.ResponseWriter, r *http.Request) {
	var u model.User
	if err := model.UnmarshalUser(body, &u); err != nil {
		// ...
	}

	b, err := model.MarshalUser(&u)
	// ... or append into a buffer you already own:
	buf, err = model.AppendUser(buf, &u)
}
```

If you cannot change the call sites, `-methods` additionally emits
`MarshalJSON`/`UnmarshalJSON`, which every JSON library honours — but measure
first: see [Benchmarks](#benchmarks) for why that is not the default.

### Flags

```
odjson [flags] [packages]
```

With no package argument odjson generates for the package in the current
directory, which is what `//go:generate` needs.

| Flag                | Default          | Description                                                                                                   |
| ------------------- | ---------------- | ------------------------------------------------------------------------------------------------------------- |
| `-type`             | all              | Comma-separated struct type names. The default is every exported struct declared in the package.               |
| `-output`           | `odjson_gen.go`  | File name written into each matched package directory. A name, not a path.                                     |
| `-methods`          | `false`          | Also emit `MarshalJSON`/`UnmarshalJSON`, so unchanged call sites route through the generated codec. Off by default — see [Benchmarks](#benchmarks). |
| `-jsonv2`           | `true`           | With `-methods`, also emit the `encoding/json/v2` methods (`MarshalJSONTo` / `UnmarshalJSONFrom`).             |
| `-recursive`        | `true`           | Also generate codecs for struct types reachable from the selected ones, so nested values skip reflection too.  |
| `-escape-html`      | `true`           | Escape `<`, `>` and `&` in strings, matching `encoding/json`'s default.                                        |
| `-case-insensitive` | `true`           | Match member names case-insensitively when decoding, matching `encoding/json` v1.                              |
| `-version`          |                  | Print the version and exit.                                                                                    |

### What is generated

For a type `T`, odjson emits:

| Symbol                               | Purpose                                                                 |
| ------------------------------------ | ----------------------------------------------------------------------- |
| `MarshalT(v *T) ([]byte, error)`     | Direct encoder. The fastest path: nothing re-validates its output.       |
| `AppendT(dst []byte, v *T) ([]byte, error)` | Appends into a caller-owned buffer; allocation-free when reused.  |
| `UnmarshalT(data []byte, v *T) error`| Direct decoder.                                                          |
| `(T).MarshalJSON` / `(*T).UnmarshalJSON` | With `-methods`: routes unchanged call sites through the generated codec.  |
| `(T).MarshalJSONTo` / `(*T).UnmarshalJSONFrom` | With `-methods -jsonv2`: the streaming `encoding/json/v2` interfaces. |

Types that already implement `json.Marshaler`, `json.Unmarshaler`,
`encoding.TextMarshaler` or `encoding.TextUnmarshaler` are left alone — odjson
calls their existing methods instead of generating a conflicting one.

## Generated code and the runtime package

Generated files import `github.com/mazrean/odjson/odjsonrt`, the runtime
support package. It ships in the same module as the CLI, so `go get -tool` or
`go install` already gives you everything; there is no extra dependency to
add.

## Correctness

odjson's contract is that generated code behaves exactly like `encoding/json`
on the same type. That is enforced, not asserted:

- **Parity fixtures.** Fixture packages under `internal/testfixture/` cover
  every field shape the generator knows — all integer and float widths,
  `[]byte` vs `[N]byte`, pointers, slices, arrays, maps, `any`,
  `json.RawMessage`, `json.Number`, `time.Time`, embedded structs by value and
  by pointer, `omitempty`, `omitzero`, `,string`, `-`, unexported fields,
  self-referential types, cross-package types, generics and other fallbacks.
  Each is marshaled with both `encoding/json` and the generated code and
  compared **byte for byte**, and unmarshaled with both and compared for both
  acceptance and decoded value.
- **`encoding/json`'s field promotion rules**, reimplemented on `go/types`:
  depth wins, a tagged field beats an untagged one at the same depth, a tie is
  dropped entirely. Tested against `encoding/json` on a struct built to hit all
  three.
- **The [JSON Test Suite](https://seriot.ch/projects/parsing_json.html)**
  (sonic's copy, 318 cases) runs twice: once against the runtime's scanner in
  `odjsonrt`, and once *through generated decoders* — every case wrapped as an
  object member and decoded into `json.RawMessage`, `any` and a typed struct,
  each required to accept exactly what `encoding/json` accepts and to produce
  the same value.
- **Generated output is committed and diff-checked**, so a change to the
  generator that alters any fixture's output fails the build rather than
  slipping through.
- Types that already implement `json.Marshaler`, `json.Unmarshaler`,
  `encoding.TextMarshaler` or `encoding.TextUnmarshaler` are left alone.

Parity is measured against the toolchain in `go.mod`. Since Go 1.27
`encoding/json` is implemented on top of `encoding/json/v2` in v1 compatibility
mode, and a few escape forms differ from the classic v1 encoder — see the
package documentation of
[`odjsonrt`](https://pkg.go.dev/github.com/mazrean/odjson/odjsonrt) for the
exact list. Under `GOEXPERIMENT=nojsonv2` those three cases would differ.

## Benchmarks

The suite lives in the [`bench/`](./bench) module — a module of its own so that
`sonic`, `goccy/go-json` and the other comparison libraries never become
dependencies of `github.com/mazrean/odjson`. It runs two packages over the same
payloads: `plain` (the vendored types, untouched) and `gen` (the same types with
odjson's generated code). The only difference between them is the generated
file, so the difference between two rows is attributable to odjson.

```sh
cd bench
go test -bench . -benchmem ./...
```

Payloads are sonic's own fixtures: `twitter` (616 KiB, `TwitterStruct` — deeply
nested and full of `interface{}` fields) and `small` (340 B, `Book` — fully
typed, the per-call overhead case).

#### Marshal — `twitter`

| library | baseline | with generated methods | change |
| --- | --- | --- | --- |
| encoding/json | 389 µs | 431 µs | 1.11× slower |
| encoding/json/v2 | 382 µs | **140 µs** | **2.73× faster** |
| sonic | 98 µs | 285 µs | 2.92× slower |
| go-json | 235 µs | 548 µs | 2.33× slower |
| **odjson direct** | — | **179 µs** — 296 KiB, 105 allocs | **2.23× faster than encoding/json** |

#### Marshal — `small`

| library | baseline | with generated methods | change |
| --- | --- | --- | --- |
| encoding/json | 1.00 µs | 944 ns | 1.06× faster |
| encoding/json/v2 | 1.01 µs | **388 ns** | **2.61× faster** |
| sonic | 275 ns | 673 ns | 2.45× slower |
| go-json | 374 ns | 1.02 µs | 2.74× slower |
| **odjson direct** | — | **262 ns** — 416 B, 1 allocs | **3.86× faster than encoding/json** |

#### Unmarshal — `twitter`

| library | baseline | with generated methods | change |
| --- | --- | --- | --- |
| encoding/json | 1.48 ms | 1.24 ms | 1.20× faster |
| encoding/json/v2 | 1.10 ms | **769 µs** | **1.43× faster** |
| sonic | 484 µs | 868 µs | 1.79× slower |
| go-json | 671 µs | 1.10 ms | 1.64× slower |
| **odjson direct** | — | **585 µs** — 413 KiB, 4551 allocs | **2.49× faster than encoding/json** |

#### Unmarshal — `small`

| library | baseline | with generated methods | change |
| --- | --- | --- | --- |
| encoding/json | 2.29 µs | 1.65 µs | 1.39× faster |
| encoding/json/v2 | 1.91 µs | **826 ns** | **2.31× faster** |
| sonic | 961 ns | 1.21 µs | 1.26× slower |
| go-json | 781 ns | 1.08 µs | 1.39× slower |
| **odjson direct** | — | **737 ns** — 544 B, 13 allocs | **3.03× faster than encoding/json** |

#### The same comparison at matching semantics

`sonic.Marshal`'s default configuration neither escapes HTML nor validates
UTF-8, so its output is not `encoding/json`'s and the rows above are not
comparing equal work. `sonic.ConfigStd` does both, which is what odjson's
generated codec produces:

| | odjson direct | sonic (default) | sonic (`ConfigStd`) |
| --- | --- | --- | --- |
| Marshal `twitter` | 184 µs | 96 µs | 118 µs |
| Marshal `small` | **256 ns** | 270 ns | 348 ns |
| Unmarshal `twitter` | 589 µs | 481 µs | 575 µs |
| Unmarshal `small` | **718 ns** | 964 ns | 1.07 µs |

Against `goccy/go-json` the direct functions win all four outright (184 vs
236 µs, 256 vs 371 ns, 589 vs 667 µs, 718 vs 773 ns).

### How to read this

**Call the generated functions directly.** `MarshalT` / `UnmarshalT` /
`AppendT` are 2.1×–4.0× faster than `encoding/json`, faster than `goccy/go-json`
on all four measurements, and faster than `sonic` on the small payload in both
directions. That is odjson's core contribution and it needs no interfaces.

**`-methods` on `encoding/json/v2`: all four rows, 1.4×–2.7×.** On
`encoding/json`: three of four, and the loss is structural. The
`encoding/json` and `encoding/json/v2` rows above come from `bench/ab`, which
measures the generated codec, the reflection baseline and the interface floor
in a single process — comparing them across processes moves the small
differences by more than their size.

The `json/v2` rows are what they are because of the direct path described
below: under a plain `json.Marshal` / `json.Unmarshal` the generated method
appends into, or parses out of, the coder's own buffer, the way `json/v2`'s
reflection codec does, and nothing in `jsontext`'s public API runs at all.
Everything that follows in this section is about the public API path, which
is what `encoding/json` call sites and every non-default situation still use.

For a value-driven `MarshalerTo` on that path, `gen = floor + odjson's own
encode`, so the room left for odjson is `plain - floor`:

| | floor | reflection | room | odjson's part | rate the room demands |
| --- | --- | --- | --- | --- | --- |
| `encoding/json` Marshal `twitter` | 329 µs | 394 µs | 65 µs | 111 µs | 3.9 GB/s |
| `encoding/json/v2` Marshal `twitter` | 359 µs | 388 µs | 29 µs | 108 µs | 8.8 GB/s |
| `encoding/json/v2` Marshal `small` | 802 ns | 1.02 µs | 217 ns | 303 ns | — |

No `MarshalerTo` on the public API can turn those rows into 1.3× wins: a
marshaler that costs nothing still measures 365 µs on `twitter` and 808 ns on
`small` under `json/v2`, and reflection divided by 1.3 is 307 µs and 794 ns.
The two `twitter` ones are also settled by the last column: sonic's AVX2 and JIT compiled encoder writes this
document at 2.2–2.7 GB/s, so a budget of 3.9 or 8.8 GB/s is not a tuning
target, it is outside what any Go encoder does. A token-driven `MarshalerTo`
would not pay the reformat at all, but it pays per member instead, and at this
fixture's density that is no better — see the density note below.

`Marshal small` is arithmetic rather than throughput: of odjson's 288 ns, 79 ns
is `strconv`'s shortest-float formatting of the three non-integral floats in the
fixture — which `json/v2`'s own encoder pays too, inside its 225 ns. Fitting
would mean halving everything else.

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

**`-methods` pays off on `encoding/json` too.** On Go 1.27 `encoding/json` is
implemented on top of `encoding/json/v2`, so it picks up the generated
`MarshalJSONTo` / `UnmarshalJSONFrom` — and with those tuned for the streaming
contract (see below) an unchanged `json.Marshal` / `json.Unmarshal` call site is
**1.06×–1.39× faster** on three of the four measurements, and 11% behind on the
fourth. `encoding/json` configures its coders with its own flags (HTML
escaping, legacy error reporting), which the direct path declines, so those
rows measure the public API path.

**It still does not pay off on sonic or go-json.** Those call `MarshalJSON`,
get a `[]byte` back, and re-validate it; their own encoders are reflection-free
already, so the round trip costs more than it saves. Setting
`NoValidateJSONMarshaler` narrows the gap but does not close it. On sonic and
go-json, call the generated functions directly.

`-methods` stays off by default because that recommendation is not uniform: a
generator should not decide for you that your library is one of the two where
it pays.

**sonic is still ahead on `twitter`.** That payload is dominated by
`interface{}` fields, which no amount of code generation can turn into static
field access — odjson falls back to a hand-written `any` encoder there, while
sonic runs JIT-compiled SIMD. On the decode side the remaining gap is the
string scanner: it is 39% of odjson's unmarshal profile, and sonic's is SIMD.
On the fully typed `small` payload odjson takes the marshal outright.

### What the drop-in path costs, and what was removed

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

Together those took `encoding/json` + `-methods` from 1.17–1.20× *slower* to
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

### The direct path

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

What it is worth (`bench/ab`, medians of 5): `json/v2` + `-methods` goes from
0.87× / 0.93× / 1.03× / 1.36× to **2.73× / 2.61× / 1.43× / 2.31×** on
Marshal `twitter` / Marshal `small` / Unmarshal `twitter` / Unmarshal `small`,
with one allocation per encode.

### What the decode side pays, and what was removed

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

Those took `json/v2` + `-methods` from parity to **1.36× faster** on the small
decode and to 1.03× on `twitter`, and `encoding/json` + `-methods` from 1.19×
to **1.45×** on the small decode. The ceiling on `twitter` is set by the 65%
above: with odjson's own share at zero, the row would read about 1.35×, and
with its share halved again, which is what remains realistic, about 1.1×.

The whole-value path and the direct path are the reason a struct with
`-methods` carries two decoders besides the `encoding/json` one:
`odjsonParseFrom`, which drives the decoder, and `odjsonParseV2`, which parses
bytes under json/v2's rules and rejects what `jsontext` would have. Which one
runs is decided at runtime, per value, by the direct path's checks and then
the buffer test above. A
streaming decoder over an `io.Reader` starts with a 64 byte buffer that rarely
ends in a bracket, so it is driven token by token and keeps its memory bounded;
when a chunk does end in a bracket, `ReadValue` fetches the rest of that value,
which is correct, only buffered rather than streamed.

### `-methods` follows each library's semantics

Because the generated methods define what the host library produces, they follow
that library's rules rather than imposing one set:

| | `MarshalJSON` (encoding/json) | `MarshalJSONTo` (encoding/json/v2) |
| --- | --- | --- |
| nil slice / map / `[]byte` | `null` | `[]` / `{}` / `""` |
| `omitempty` on `0` or `false` | omitted | kept |
| HTML characters, U+2028/9 | escaped | not escaped |
| array length mismatch (decode) | padded or truncated | rejected |
| case-insensitive member match (decode) | yes | no |
| `null` into a scalar, struct, `time.Time` or `,string` field (decode) | left untouched | zeroed |
| map member order | sorted by name | map iteration order |

The map row is the one that costs something: `encoding/json/v2` does not sort
map members, so neither does the generated `MarshalJSONTo`. Its output for a
document containing maps is therefore no more byte-stable than `json/v2`'s own
— which is the point of following the host library's rules rather than
imposing `encoding/json`'s.

This is verified, not asserted: `internal/testfixture/v2parity` decodes the same
documents into a generated type and an identical reflection-only type and
requires `json/v2` to produce the same bytes for both, and the JSON Test Suite
runs through the generated `UnmarshalJSONFrom` against `json/v2`'s own decoder
for all 318 cases.

### Can `-methods` beat sonic or go-json?

No, and this is provable rather than a matter of tuning.

`bench/floor` measures what a host library charges for routing through
`json.Marshaler` / `json.Unmarshaler` at all: its marshaler returns an already
encoded document and its unmarshaler discards its input, so the generated codec
behind the interface costs **nothing**. That is a lower bound for any possible
implementation.

| `twitter` / `small` | interface floor | the library's own path | room left for a codec |
| --- | --- | --- | --- |
| sonic Marshal | 107 µs / 394 ns | 102 µs / 280 ns | **none — the floor is already higher** |
| go-json Marshal | 347 µs / 679 ns | 239 µs / 383 ns | **none** |
| sonic Unmarshal | 285 µs / 356 ns | 488 µs / 944 ns | 203 µs / 588 ns |
| go-json Unmarshal | 498 µs / 312 ns | 675 µs / 801 ns | 177 µs / 489 ns |

On the four marshal measurements the interface floor already exceeds what sonic
and go-json cost without it: a `MarshalJSON` that costs literally zero still
loses, because the library has to make an interface call and then re-scan and
copy bytes it did not produce itself. No amount of code generation changes that.

The four unmarshal measurements do leave room, but not enough: odjson's direct
decoder (598 µs / 744 ns) would have to become 2.9× and 3.4× faster on
`twitter`, and 1.3× and 1.5× faster on `small`, purely to break even — and
sonic's remaining decode budget of 203 µs is almost exactly what sonic itself
spends decoding, so matching it would mean matching a JIT-compiled SIMD decoder
with no margin at all.

The model is not a guess: `gen = direct + floor` holds to within a few percent
on every row. sonic's marshal of `twitter` measures 294 µs against a direct
encode of 190 µs and a floor of 107 µs; go-json's 559 µs against 190 µs and
347 µs. The floor is real, and it is additive.

So the recommendation for sonic and go-json users is not "tune `-methods`", it
is **do not use `-methods`; call `MarshalT` / `UnmarshalT` directly**, where
odjson is 1.3× faster than go-json's marshal, faster than both on the small
payload's marshal, and faster than sonic on the small payload's decode.

If you must keep `-methods` on for those libraries, sonic can at least be told
that a `json.Marshaler`'s output needs no re-validation:

```go
var api = sonic.Config{
	NoValidateJSONMarshaler: true, // odjson's output is already valid JSON
	NoValidateJSONSkip:      true, // and it validates the members it skips
}.Froze()
```

| sonic + `-methods` | default config | trusting config |
| --- | --- | --- |
| Marshal `twitter` | 303 µs | **224 µs** |
| Marshal `small` | 706 ns | **380 ns** |
| Unmarshal `twitter` | 1.16 ms | **975 µs** |
| Unmarshal `small` | 1.46 µs | **1.10 µs** |

Do **not** also set `CompactMarshaler`: it sounds right for odjson's
always-compact output, but it measures 1.6× *slower* (482 µs vs 303 µs on
`twitter`).

Numbers above are medians of five interleaved runs on an AMD Ryzen 9 7950X,
Linux, Go 1.27.1. They come from two separate processes — `bench/plain` and
`bench/gen` — which is fine for the absolute figures but not for the small
differences between a drop-in row and its baseline: those are of the same order
as the drift between two runs, and their sign moves with `GOMAXPROCS`. For that
comparison use `bench/ab`, which measures both sides in a single process; its
verdict is that `-methods` wins all four unmarshal rows and
`encoding/json`'s small marshal, and loses the three remaining marshal rows. Compare your own with
[`benchstat`](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat) rather than
trusting a single run.

## Contributing

See [AGENTS.md](./AGENTS.md) for the repository conventions (module layout,
linting via `go tool lint ./...`, testing with `go test -race ./...`, and
Conventional Commits).

## License

[MIT](./LICENSE)

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
| encoding/json | 401 µs | 482 µs | 1.20× slower |
| encoding/json/v2 | 388 µs | 574 µs | 1.48× slower |
| sonic | 100 µs | 281 µs | 2.81× slower |
| go-json | 239 µs | 542 µs | 2.27× slower |
| **odjson direct** | — | **176 µs** — 296 KiB, 105 allocs | **2.27× faster than encoding/json** |

#### Marshal — `small`

| library | baseline | with generated methods | change |
| --- | --- | --- | --- |
| encoding/json | 1.00 µs | 946 ns | 1.06× faster |
| encoding/json/v2 | 1.00 µs | 1.11 µs | 1.10× slower |
| sonic | 274 ns | 672 ns | 2.45× slower |
| go-json | 375 ns | 974 ns | 2.59× slower |
| **odjson direct** | — | **251 ns** — 416 B, 1 alloc | **3.98× faster than encoding/json** |

#### Unmarshal — `twitter`

| library | baseline | with generated methods | change |
| --- | --- | --- | --- |
| encoding/json | 1.46 ms | 1.71 ms | 1.17× slower |
| encoding/json/v2 | 1.08 ms | 1.49 ms | 1.38× slower |
| sonic | 500 µs | 1.14 ms | 2.29× slower |
| go-json | 679 µs | 1.40 ms | 2.07× slower |
| **odjson direct** | — | **844 µs** — 433 KiB, 4750 allocs | **1.74× faster than encoding/json** |

#### Unmarshal — `small`

| library | baseline | with generated methods | change |
| --- | --- | --- | --- |
| encoding/json | 2.27 µs | 1.95 µs | 1.16× faster |
| encoding/json/v2 | 1.88 µs | 1.76 µs | 1.07× faster |
| sonic | 998 ns | 1.46 µs | 1.46× slower |
| go-json | 782 ns | 1.35 µs | 1.73× slower |
| **odjson direct** | — | **945 ns** — 616 B, 18 allocs | **2.40× faster than encoding/json** |

### How to read this

**Call the generated functions directly.** `MarshalT` / `UnmarshalT` /
`AppendT` are 1.7×–4.0× faster than `encoding/json`, and on the fully typed
payload the marshal path beats every library tested. That is odjson's actual contribution.

**`-methods` is off by default, and the numbers are why.** Attaching
`MarshalJSON`/`UnmarshalJSON` does make an unchanged call site route through
the generated codec — but the contract obliges the host library to hand the
generated encoder an interface call and then re-scan and copy the bytes it gets
back, and to locate the value's extent before handing it to the decoder. That
overhead is larger than the reflection the generated codec removes, for every
library except `encoding/json` on small payloads. Generating a codec that makes
your program slower is not a trade-off worth defaulting to.

So: use `-methods` when you cannot change the call sites and you are on
`encoding/json` with small documents. Otherwise leave it off and call the
generated functions where the JSON actually crosses your program's boundary.

**sonic is still ahead on `twitter`.** That payload is dominated by
`interface{}` fields, which no amount of code generation can turn into static
field access — odjson falls back to a hand-written `any` encoder there, while
sonic runs JIT-compiled SIMD. On the decode side the remaining gap is the
string scanner: it is 39% of odjson's unmarshal profile, and sonic's is SIMD.

On the fully typed `small` payload odjson takes the marshal outright (251 ns vs
sonic's 274 ns) and lands within 5% of sonic on unmarshal — but go-json is still
fastest there at 782 ns. Generated code wins the encode side convincingly; on
the decode side it clears the standard library by a wide margin and trades
places with the hand-optimised libraries.

### Does `MarshalerTo` / `UnmarshalerFrom` help?

`encoding/json/v2` prefers the streaming `MarshalJSONTo` / `UnmarshalJSONFrom`
over the v1 pair when a type has both, and odjson emits them by default under
`-methods`. Generating with `-jsonv2=false` — leaving `json/v2` only the v1
interfaces to work with — isolates what they buy:

| under `encoding/json/v2` | v1 interfaces only | + `MarshalerTo`/`UnmarshalerFrom` | plain (reflection) |
| --- | --- | --- | --- |
| Marshal `twitter` | 599 µs, 574 KiB | 573 µs, 305 KiB | 388 µs |
| Marshal `small` | 1.15 µs, 800 B | 1.10 µs, 408 B | 1.00 µs |
| Unmarshal `twitter` | 1.48 ms | 1.49 ms | 1.08 ms |
| Unmarshal `small` | 1.79 µs | 1.76 µs | 1.88 µs |

So the streaming interfaces are real and measurably in use — encoding drops
~4% of its time and *half* its garbage, because `MarshalJSONTo` appends into a
pooled buffer and hands it to the encoder instead of allocating a fresh slice
to return. But that is a discount on the overhead, not a removal of it:
`jsontext.Encoder.WriteValue` still validates and copies everything written to
it, so `json/v2` remains 1.5× slower than its own reflection path on `twitter`.

Decoding gains nothing at all. `UnmarshalJSONFrom` receives a `jsontext.Decoder`,
and odjson's generated parser works over a byte slice, so it calls
`dec.ReadValue()` — which makes `json/v2` scan the value to find its extent
before odjson parses it a second time. Exactly the double parse the v1 interface
causes. Avoiding it would mean writing a second, token-based decoder against
`jsontext`, which trades one indirection for another.

The one place drop-in mode wins under `json/v2` is `Unmarshal small`, where the
generated parser is enough faster to cover the double parse of a 340-byte
document.

### Where the drop-in overhead is, and what would remove it

A CPU profile of the drop-in marshal path (`encoding/json` + `-methods`, the
`twitter` payload) puts **57.6% of the time inside
`jsontext.reformatValue`/`reformatObject`** — `json/v2` re-parsing the bytes the
generated encoder just produced. Within that: `ReformatString` 27%,
`utf8.decodeRuneSlow` 11.9%, `utf8.ValidString` 5.7%. Both sides validate the
same UTF-8, twice.

Three ways out were prototyped and measured (`bench/proto`, a hand-written
experiment on the `small` types, comparing `json/v2` reflection against a
value-driven and a token-driven `MarshalerTo`/`UnmarshalerFrom`):

| `json/v2`, 24 KiB string-heavy value | reflection | value-driven (today) | token-driven |
| --- | --- | --- | --- |
| Marshal | 28.6 µs | 47.8 µs | **28.8 µs** |
| Unmarshal | 31.9 µs | 56.6 µs | **31.6 µs** |

Driving `jsontext` token by token removes the whole reformat pass, and on the
decode side skipping odjson's unquote for escape-free strings — `jsontext` has
already validated them — removes the second UTF-8 pass. Together that is a
**1.7×–1.8× improvement on the drop-in path**.

But it tops out at *parity* with reflection, and on a small field-dense value
the token API is slightly **worse** (1.25 µs vs 1.21 µs for the value-driven
form), because `jsontext.Encoder` exposes no way to append a precomputed quoted
member name — which is exactly what `json/v2`'s own encoder does internally.
The public `MarshalerTo` contract is the floor: a generated codec can reach
`json/v2`'s reflection encoder, not pass it.

**sonic users have a cheaper lever, and it needs no change to odjson.** sonic
can be told that a `json.Marshaler`'s output needs no re-validation:

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
`twitter`). Even so, sonic with `-methods` still does not beat sonic without
it — its own encoder is simply faster than any code that has to hand bytes
across an interface. The advice stands: call the generated functions directly.

Numbers above are medians of five interleaved runs on an AMD Ryzen 9 7950X,
Linux, Go 1.27.1. Compare your own with
[`benchstat`](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat) rather than
trusting a single run.

## Contributing

See [AGENTS.md](./AGENTS.md) for the repository conventions (module layout,
linting via `go tool lint ./...`, testing with `go test -race ./...`, and
Conventional Commits).

## License

[MIT](./LICENSE)

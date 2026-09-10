# odjson

[![CI](https://github.com/mazrean/odjson/actions/workflows/ci.yml/badge.svg)](https://github.com/mazrean/odjson/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/mazrean/odjson.svg)](https://pkg.go.dev/github.com/mazrean/odjson)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](./LICENSE)

**odjson** — *overdrive JSON* — is a CLI code generator that makes
`encoding/json/v2` as fast as a JIT-compiled third-party codec **from the
outside**. It is bolted on, not swapped in: you keep the standard library,
your call sites do not change, and deleting one generated file undoes all of
it.

A Go struct already tells you everything about its JSON shape at compile time.
Runtime libraries throw that away and rediscover it with reflection on every
call; odjson reads your struct definitions ahead of time and writes out
dedicated, reflection-free code for each one, behind the two methods
`encoding/json/v2` was already looking for.

```go
//go:generate go tool odjson -type User
```

```sh
$ go generate ./...     # writes odjson_gen.go
$ rm odjson_gen.go      # and this is the entire uninstall
```

What that buys: **2.0×–3.6× on `encoding/json/v2`** across four measurements —
ahead of [`goccy/go-json`](https://github.com/goccy/go-json) on all four, and
level with [`bytedance/sonic`](https://github.com/bytedance/sonic)'s
JIT-compiled SIMD codec on three of them, 1.7× ahead on the fourth — while
still being the standard library, with no dependency added.

## It bolts onto the standard library

odjson does not replace `encoding/json/v2`. For each struct it implements the
four standard marshaling interfaces:

| Interface                             | Package                                                   |
| ------------------------------------- | --------------------------------------------------------- |
| `MarshalJSONTo` / `UnmarshalJSONFrom` | [`encoding/json/v2`](https://pkg.go.dev/encoding/json/v2)  |
| `MarshalJSON` / `UnmarshalJSON`       | [`encoding/json`](https://pkg.go.dev/encoding/json)        |

That is the **entire** generated API surface. There is no odjson symbol to
call, no wrapper type, no configuration object — nothing in your program
refers to odjson at all. Every `json.Marshal(v)` and `json.Unmarshal(b, &v)`
you already wrote dispatches into the generated code.

Two consequences follow, and they are the whole pitch:

- **You keep the standard library.** Its options, its error types, its
  streaming decoders and everything else in your program that touches those
  types keeps working, because odjson only supplies the two methods the
  library was already looking for.
- **It comes off whenever you want.** `rm odjson_gen.go`, rebuild, and the
  package is back to plain reflection. There is no migration to undo and no
  API to unpick — which is what makes it cheap to try, and cheap to abandon if
  a future standard library closes the gap.

### It depends on `jsontext`'s internal layout

The headline `json/v2` numbers rest on a **direct path**
(`odjsonrt/direct.go`), and you should know what it is before adopting odjson.

`jsontext`'s public API charges a floor that no `MarshalerTo` can get under —
346 µs on the `twitter` payload for a marshaler that costs *nothing* — plus a
per-object-member duplicate-name check that `json/v2`'s own codecs switch off
through an internal export the linker refuses to let any other module reach.
So for a **top-level** value under a plain, buffered, option-free
`json.Marshal` / `json.Unmarshal`, the generated methods write into and read
out of the coder's own buffer directly, through `reflect`-computed field
offsets and `unsafe`.

That is a dependency on unexported layout, so it is fenced in:

- **gated to the Go minor version it was verified against** (1.27) — a newer
  toolchain gets the public API path until the layout is re-verified;
- **checked by type at init**, and **self-tested at init** by running the
  direct path through `json/v2` and comparing its answers with the public
  API's;
- **any failure disables it for the whole process**, and
  `odjsonrt.DirectEnabled` reports the outcome;
- **`-tags odjson_safe` compiles it out** entirely;
- **every generated method keeps the public API path as its fallback**, and
  takes it for any nested value, any `io.Writer` / `io.Reader`, any option and
  every `encoding/json` call.

Disabling it costs speed and nothing else. Details, and what the public API
path costs without it, are in [docs/internals.md](./docs/internals.md#the-direct-path).

## Benchmarks

The suite lives in the [`bench/`](./bench) module — a module of its own so
that `sonic`, `goccy/go-json` and the other comparison libraries never become
dependencies of `github.com/mazrean/odjson`. It runs two packages over the
same payloads: `plain` (the vendored types, untouched) and `gen` (the same
types with odjson's generated code). The only difference between them is the
generated file, so the difference between two rows is attributable to odjson.

```sh
cd bench
go test -bench . -benchmem ./...
```

Payloads are sonic's own fixtures: `twitter` (616 KiB, `TwitterStruct` —
deeply nested and full of `interface{}` fields) and `small` (340 B, `Book` —
fully typed, the per-call overhead case). The two standard libraries are the
subject; `sonic` and `go-json` are the yardstick — odjson does not make *them*
faster ([why](./docs/internals.md#the-two-third-party-libraries)), so they
appear at their own speed only.

| Marshal `twitter` | on its own | with odjson | change |
| --- | --- | --- | --- |
| **encoding/json/v2** | 390 µs | **110 µs** | **3.55× faster** |
| **encoding/json** | 403 µs | 421 µs | 1.04× slower |
| sonic | 113 µs | — | |
| go-json | 238 µs | — | |

| Marshal `small` | on its own | with odjson | change |
| --- | --- | --- | --- |
| **encoding/json/v2** | 1.02 µs | **321 ns** | **3.19× faster** |
| **encoding/json** | 1.02 µs | 867 ns | 1.18× faster |
| sonic | 307 ns | — | |
| go-json | 400 ns | — | |

| Unmarshal `twitter` | on its own | with odjson | change |
| --- | --- | --- | --- |
| **encoding/json/v2** | 1.09 ms | **535 µs** | **2.04× faster** |
| **encoding/json** | 1.48 ms | 1.22 ms | 1.21× faster |
| sonic | 550 µs | — | |
| go-json | 646 µs | — | |

| Unmarshal `small` | on its own | with odjson | change |
| --- | --- | --- | --- |
| **encoding/json/v2** | 1.87 µs | **620 ns** | **3.01× faster** |
| **encoding/json** | 2.26 µs | 1.48 µs | 1.53× faster |
| sonic | 1.03 µs | — | |
| go-json | 760 ns | — | |

`encoding/json/v2` gains on all four, and the generated file is the only thing
that changed. `encoding/json` gains on three of the four; its `twitter` encode
is 4% behind, and that loss is structural — v1 configures its coders with its
own flags, which the direct path declines.

Against the libraries people leave the standard library for, that puts
`encoding/json/v2` + odjson:

| | vs go-json | vs sonic |
| --- | --- | --- |
| Marshal `twitter` | **2.16× faster** (110 vs 238 µs) | level (110 vs 113 µs) |
| Marshal `small` | **1.24× faster** (321 vs 400 ns) | 1.05× slower (321 vs 307 ns) |
| Unmarshal `twitter` | **1.21× faster** (535 vs 646 µs) | level (535 vs 550 µs) |
| Unmarshal `small` | **1.23× faster** (620 vs 760 ns) | **1.67× faster** (620 vs 1.03 µs) |

Three caveats, so you can weigh the numbers yourself:

- Medians of three runs on an AMD Ryzen 9 7950X, Linux, Go 1.27.1.
- `sonic.Marshal`'s default configuration neither escapes HTML nor validates
  UTF-8, so its encode rows are not doing equal work; `sonic.ConfigStd`, which
  does both, measures 123 µs and 359 ns.
- The tables come from two separate processes, which is fine for absolute
  figures but not for small differences between a generated row and its
  baseline. For that use `bench/ab`, which measures both sides in one process;
  it puts the two `twitter` rows within 3% of sonic in either direction, and
  sonic's own `twitter` decode drifts between 480 and 550 µs from run to run.

The point is not that odjson wins every row. It is that this is the standard
library, with no dependency added, no call site changed and one file to delete
to get back. The full accounting — where every microsecond goes, what was
tried and rejected, and why sonic and go-json cannot be sped up — is in
[docs/internals.md](./docs/internals.md).

## Quick start

Install the generator as a project tool, so everyone — and CI — generates with
the same version:

```sh
go get -tool github.com/mazrean/odjson@latest
```

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

That is the whole integration. The call sites stay as they were:

```go
func handler(w http.ResponseWriter, r *http.Request) {
	var u model.User
	if err := json.Unmarshal(body, &u); err != nil {
		// ...
	}

	b, err := json.Marshal(&u)
	// ...
}
```

To take it back off, delete `odjson_gen.go` and rebuild.

## Install

`go get -tool` is the recommended way in — it pins the generator to your
module, and the runtime package generated files import
(`github.com/mazrean/odjson/odjsonrt`) ships in the same module, so there is
nothing else to add:

```sh
go get -tool github.com/mazrean/odjson@latest
go tool odjson -h
```

<details>
<summary>Standalone binary, Homebrew, WinGet, Linux packages</summary>

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

</details>

## How it works

For a type `T`, odjson emits exactly four methods, plus the unexported codecs
behind them:

| Symbol                                         | Purpose                                                   |
| ---------------------------------------------- | --------------------------------------------------------- |
| `(T).MarshalJSONTo` / `(*T).UnmarshalJSONFrom` | `encoding/json/v2`'s streaming interfaces. The fast path. |
| `(T).MarshalJSON` / `(*T).UnmarshalJSON`       | `encoding/json` v1's interfaces, following v1's rules.    |

- **Marshal** — the generated code appends struct fields directly to a byte
  slice. Field names, quoting and separators are constants baked into the
  generated source. (Same core idea as
  [`sapphi-red/json-constantiater`](https://github.com/sapphi-red/json-constantiater).)
- **Unmarshal** — the generated code is a specialised parser for that one
  struct. It streams the input straight into the struct's fields: no
  reflection, no intermediate `map[string]any`, no per-field name lookup at
  runtime. Member names are matched against the document's raw bytes, quotes
  included, and bools, integers and simple floats are decoded inline; anything
  the fast path cannot see falls through to a general parser rather than
  guessing.
- **Which decoder runs** is decided per value at runtime: the direct path when
  it applies, a whole-value byte parser for small values, and a token-driven
  pass over `jsontext.Decoder` for large ones, which keeps a streaming decode's
  memory bounded.

Types that already implement `json.Marshaler`, `json.Unmarshaler`,
`encoding.TextMarshaler` or `encoding.TextUnmarshaler` are left alone — odjson
calls their existing methods instead of generating a conflicting one.

### The one behaviour change to expect

Each generated method follows the rules of the interface it implements. On Go
1.27 `encoding/json` is implemented on top of `encoding/json/v2` and prefers
`MarshalJSONTo` when a type offers both, so **an unchanged `encoding/json`
call site moves to v2's spellings** for generated types: a nil slice encodes
as `[]` rather than `null`, `omitempty` keeps a zero number, array lengths are
strict on decode, and member names are matched case-sensitively, which
`-case-insensitive` covers. The
[full table is in docs/internals.md](./docs/internals.md#which-semantics-a-generated-method-follows).

> **One thing to know about embedding.** A struct that embeds a generated type
> and does not get a codec of its own inherits the embedded type's
> `MarshalJSON`, and would then encode as only that embedded part. The default
> — every exported struct in the package, plus everything `-recursive` reaches
> — covers this within a package. Take care when you narrow it with `-type`, or
> when another module embeds one of your generated types without running odjson
> over its own.

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
| `-recursive`        | `true`           | Also generate codecs for struct types reachable from the selected ones, so nested values skip reflection too.  |
| `-escape-html`      | `true`           | Escape `<`, `>` and `&` in strings, matching `encoding/json`'s default.                                        |
| `-case-insensitive` | `false`          | In `UnmarshalJSON`, fall back to a case-insensitive member match the way `encoding/json` v1 does. Off by default, matching `encoding/json/v2`. |
| `-version`          |                  | Print the version and exit.                                                                                    |

## Correctness

odjson's contract has two halves, one per interface: `MarshalJSON` /
`UnmarshalJSON` behave exactly like `encoding/json` v1 on the same type, and
`MarshalJSONTo` / `UnmarshalJSONFrom` behave exactly like `encoding/json/v2`.
Each half is enforced by fixtures, not asserted:

- **Parity fixtures** under `internal/testfixture/` cover every field shape the
  generator knows — every numeric width, `[]byte` vs `[N]byte`, pointers,
  slices, arrays, maps, `any`, `json.RawMessage`, `json.Number`, `time.Time`,
  embedding by value and by pointer, `omitempty`, `omitzero`, `,string`, `-`,
  unexported fields, self-referential and cross-package types, generics and
  other fallbacks — and compare **byte for byte** against the standard
  library. Because the generated methods are what `encoding/json` now calls,
  the reference side reads the same value as an identically declared twin type
  odjson was never run on, so reflection still produces the expected answer.
  `internal/testfixture/v2parity` does the same for the v2 half.
- **The [JSON Test Suite](https://seriot.ch/projects/parsing_json.html)** (318
  cases) runs against the runtime's scanner and *through generated decoders*,
  each case required to accept exactly what the standard library accepts and
  produce the same value.
- **`encoding/json`'s field promotion rules**, reimplemented on `go/types` and
  tested against `encoding/json` on a struct built to hit all three.
- **Generated output is committed and diff-checked**, so a generator change
  that alters any fixture's output fails the build.

Parity is measured against the toolchain in `go.mod`. Since Go 1.27
`encoding/json` is implemented on top of `encoding/json/v2` in v1
compatibility mode, and a few escape forms differ from the classic v1 encoder
— see the package documentation of
[`odjsonrt`](https://pkg.go.dev/github.com/mazrean/odjson/odjsonrt) for the
exact list.

## Contributing

See [AGENTS.md](./AGENTS.md) for the repository conventions (module layout,
linting via `go tool lint ./...`, testing with `go test -race ./...`, and
Conventional Commits).

## License

[MIT](./LICENSE)

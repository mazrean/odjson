# odjson

[![CI](https://github.com/mazrean/odjson/actions/workflows/ci.yml/badge.svg)](https://github.com/mazrean/odjson/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/mazrean/odjson.svg)](https://pkg.go.dev/github.com/mazrean/odjson)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](./LICENSE)

**odjson** — *overdrive JSON* — is a CLI code generator that makes
`encoding/json/v2` as fast as a JIT-compiled third-party codec **from the
outside**. It is bolted on, not swapped in: you keep the standard library,
your call sites do not change, and deleting one generated file undoes all of
it.

```go
//go:generate go tool odjson -type User
```

```sh
$ go generate ./...     # writes odjson_gen.go
$ rm odjson_gen.go      # and this is the entire uninstall
```

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="./docs/assets/bench-dark.svg">
  <img alt="Time per operation, lower is better. Marshal twitter: encoding/json/v2 390 µs, with odjson 110 µs, sonic 113 µs, go-json 238 µs. Marshal small: 1020 ns, with odjson 321 ns, sonic 307 ns, go-json 400 ns. Unmarshal twitter: 1090 µs, with odjson 535 µs, sonic 550 µs, go-json 646 µs. Unmarshal small: 1870 ns, with odjson 620 ns, sonic 1030 ns, go-json 760 ns." src="./docs/assets/bench-light.svg" width="912">
</picture>

**2.0×–3.6× on `encoding/json/v2`** across all four measurements — ahead of
[`goccy/go-json`](https://github.com/goccy/go-json) on every one, level with
[`bytedance/sonic`](https://github.com/bytedance/sonic)'s JIT-compiled SIMD
codec on three and 1.7× ahead on the small decode — while still being the
standard library, with no dependency added.

The payloads are sonic's own fixtures: `twitter` (616 KiB, deeply nested and
full of `interface{}` fields) and `small` (340 B, fully typed, the per-call
overhead case). The only difference between a baseline bar and an odjson bar
is the generated file.

<details>
<summary>How to read the numbers, and how to reproduce them</summary>

The suite lives in the [`bench/`](./bench) module — a module of its own so
that `sonic`, `goccy/go-json` and the other comparison libraries never become
dependencies of `github.com/mazrean/odjson`:

```sh
cd bench
go test -bench . -benchmem ./...
```

- The two standard libraries are the subject; `sonic` and `go-json` are the
  yardstick. odjson does **not** make those two faster
  ([why, with measurements](./docs/internals.md#the-two-third-party-libraries)),
  so they appear at their own speed only.
- `sonic.Marshal`'s default configuration neither escapes HTML nor validates
  UTF-8, so its encode bars are not doing equal work; `sonic.ConfigStd`, which
  does both, measures 123 µs and 359 ns.
- The chart's figures come from two separate processes, which is fine in
  absolute terms but not for small differences between a generated row and its
  baseline. For those use `bench/ab`, which measures both sides in one
  process: it puts the two `twitter` rows within 3% of sonic in either
  direction, and sonic's own `twitter` decode drifts between 480 and 550 µs
  from run to run.
- `encoding/json` v1 gains too — 1.2×–1.5× on three of the four, and 4% behind
  on the `twitter` encode. Its rows are in
  [the measured tables](./docs/internals.md#the-measured-tables), left out of
  the chart to keep the `json/v2` story legible.

The point is not that odjson wins every row. It is that this is the standard
library, with no dependency added, no call site changed and one file to delete
to get back. The full accounting — where every microsecond goes, what was
tried and rejected, and why sonic and go-json cannot be sped up — is in
[docs/internals.md](./docs/internals.md).

</details>

One thing to know before adopting it: those `json/v2` numbers rest on a
**direct path** that depends on `jsontext`'s internal layout. It is gated to
the Go minor version it was verified against, self-tested at init, disabled
for the process on any failure, and compiled out by a build tag — and every
generated method keeps the ordinary public API path as its fallback. The
[guards are spelled out below](#it-depends-on-jsontexts-internal-layout).

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

## How it works

A Go struct already tells you everything about its JSON shape at compile time.
Runtime libraries throw that away and rediscover it with reflection on every
call; odjson reads your struct definitions ahead of time and writes out
dedicated, reflection-free code for each one.

### It bolts onto the standard library

odjson does not replace `encoding/json/v2`. For each type `T` it emits exactly
four methods — the standard marshaling interfaces — plus the unexported codecs
behind them:

| Symbol                                         | Interface                                                  |
| ---------------------------------------------- | ---------------------------------------------------------- |
| `(T).MarshalJSONTo` / `(*T).UnmarshalJSONFrom` | [`encoding/json/v2`](https://pkg.go.dev/encoding/json/v2)'s streaming interfaces. The fast path. |
| `(T).MarshalJSON` / `(*T).UnmarshalJSON`       | [`encoding/json`](https://pkg.go.dev/encoding/json) v1's interfaces, following v1's rules. |

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

### What the generated code does

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

### It depends on `jsontext`'s internal layout

`jsontext`'s public API charges a floor that no `MarshalerTo` can get under —
346 µs on the `twitter` payload for a marshaler that costs *nothing* — plus a
per-object-member duplicate-name check that `json/v2`'s own codecs switch off
through an internal export the linker refuses to let any other module reach.
So for a **top-level** value under a plain, buffered, option-free
`json.Marshal` / `json.Unmarshal`, the generated methods write into and read
out of the coder's own buffer directly, through `reflect`-computed field
offsets and `unsafe` (`odjsonrt/direct.go`). That is where the chart's
`json/v2` numbers come from.

Depending on unexported layout is a real risk, so it is fenced in:

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
path costs without it, are in
[docs/internals.md](./docs/internals.md#the-direct-path).

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

# odjson

[![CI](https://github.com/mazrean/odjson/actions/workflows/ci.yml/badge.svg)](https://github.com/mazrean/odjson/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/mazrean/odjson.svg)](https://pkg.go.dev/github.com/mazrean/odjson)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](./LICENSE)

**odjson** — *overdrive JSON* — is a CLI code generator that makes
`encoding/json/v2` as fast as a JIT-compiled third-party codec **from the
outside**. It is bolted on, not swapped in: you keep the standard library, you
do not edit a call site, and deleting one generated file undoes all of it.

```go
//go:generate go tool odjson -type User
```

```sh
$ go generate ./...     # writes odjson_gen.go
$ rm odjson_gen.go      # and this is the entire uninstall
```

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="./docs/assets/bench-dark.svg">
  <img alt="Time per operation, lower is better. Marshal large: encoding/json/v2 392 µs, with odjson 111 µs, sonic 117 µs, go-json 237 µs. Marshal small: 1025 ns, with odjson 317 ns, sonic 308 ns, go-json 374 ns. Unmarshal large: 1072 µs, with odjson 506 µs, sonic 492 µs, go-json 655 µs. Unmarshal small: 1842 ns, with odjson 573 ns, sonic 977 ns, go-json 770 ns." src="./docs/assets/bench-light.svg" width="912">
</picture>

**2.1×–3.5× on `encoding/json/v2`** across all four measurements — ahead of
[`goccy/go-json`](https://github.com/goccy/go-json) on every one, level with
[`bytedance/sonic`](https://github.com/bytedance/sonic)'s JIT-compiled SIMD
codec on three and 1.7× ahead on the small decode — while still being the
standard library, with no third-party codec in your build. (One module does
come along: generated files import `github.com/mazrean/odjson/odjsonrt`, the
runtime support package — see [Requirements](#requirements).)

The payloads are sonic's own fixtures, under the names sonic gives them:
`large` (616 KiB, deeply nested and full of `interface{}` fields — sonic's
`twitter.json`, which [`bench/`](./bench) and
[docs/internals.md](./docs/internals.md) still call `twitter`) and `small`
(340 B, fully typed, the per-call overhead case). The only difference between
a baseline bar and an odjson bar is the generated file.

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
  does both, measures 126 µs and 374 ns.
- The chart's figures come from two separate processes, which is fine in
  absolute terms but not for small differences between a generated row and its
  baseline. For those use `bench/ab`, which measures both sides in one
  process: it puts every sonic row within 3% of the chart's, and sonic's own
  `large` decode has drifted between 480 and 550 µs across runs.
- `encoding/json` v1 gains too — 1.2×–1.5× on three of the four, and 5% behind
  on the `large` encode. Its rows are in
  [the measured tables](./docs/internals.md#the-measured-tables), left out of
  the chart to keep the `json/v2` story legible.

The point is not that odjson wins every row. It is that this is the standard
library, with no third-party codec, no call site changed and one file to
delete to get back. The full accounting — where every microsecond goes, what was
tried and rejected, and why sonic and go-json cannot be sped up — is in
[docs/internals.md](./docs/internals.md).

</details>

> [!IMPORTANT]
> Two things to check before adopting it, both of them below: generating a codec
> [changes the JSON your existing call sites produce](#the-json-on-the-wire),
> and the speed above
> [depends on standard library internals](#it-depends-on-standard-library-internals).

## Requirements

- **Go 1.27 or newer.** odjson generates for `encoding/json/v2`, which landed
  in the standard library in Go 1.27 and is on by default there. Generated
  files import `encoding/json/jsontext`, so on Go 1.26 and older they do not
  compile. Since 1.27 `encoding/json` is itself implemented on top of
  `encoding/json/v2` — which is why an unchanged v1 call site reaches the
  generated v2 methods, and why
  [the JSON on the wire changes](#the-json-on-the-wire).
- **One module joins your build**: generated files import
  `github.com/mazrean/odjson/odjsonrt`, the runtime support package. It ships
  in the same module as the CLI, so `go get -tool` adds it for you. No
  third-party JSON codec is involved.
- **Nothing extra to build.** `odjson_gen.go` is ordinary Go with no build
  tags, carries a `// Code generated by odjson. DO NOT EDIT.` header, and is
  committed — a developer without the tool installed builds the repo normally
  and only needs odjson to regenerate.

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
<summary>Standalone binary, Homebrew, Linux packages</summary>

### As a standalone binary

```sh
go install github.com/mazrean/odjson@latest
```

### Homebrew (macOS)

```sh
brew install --cask mazrean/tap/odjson
```

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

## What adopting it changes

Two things change when you commit a generated file. Neither is hidden by
odjson, and both are worth reading before you do it.

### The JSON on the wire

You do not edit a call site, but the bytes it produces move. Since Go 1.27
`encoding/json` is implemented on top of `encoding/json/v2`, and `json/v2`
prefers `MarshalJSONTo` when a type offers both — so for a generated type, an
unchanged `json.Marshal` / `json.Unmarshal` gets `encoding/json/v2`'s
semantics, except where `encoding/json`'s own options put v1's back.

This is what an ordinary `encoding/json` call site does before and after
generating a codec, measured against the same type declared twice — once with
a generated file, once without:

| at an `encoding/json` call site | without odjson | with odjson |
| --- | --- | --- |
| nil slice / map | `null` | `[]` / `{}` |
| nil `[]byte` | `null` | `""` |
| `omitempty` on `0` or `false` | omitted | **kept** |
| `null` into a scalar, struct, `time.Time` or `,string` field (decode) | left untouched | **zeroed** |
| array length mismatch (decode) | padded or truncated | **rejected** |
| member name in the wrong case (decode) | matched | **not matched** |
| map member order | sorted by name | sorted by name — *unchanged* |
| `<`, `>`, `&`, U+2028/9 | escaped | escaped — *unchanged* |

> [!WARNING]
> The three decode rows are the ones to check first: input your service accepts
> today can start being **rejected**, or can start **zeroing** a field it used
> to leave alone. `-case-insensitive` does not restore the last of them here —
> it, and `-escape-html`, only reach the v1 `UnmarshalJSON` / `MarshalJSON`
> methods, which a `json/v2` call site never runs.

The two rows people worry about most do **not** change. The generated encoder
sorts map members the way `encoding/json` does, so a struct containing a map
stays byte-stable: golden files, string-compared test output and a signature
over a serialized body all keep working. And `encoding/json` applies its own
HTML escaping over whatever a marshaler returns, so `<`, `>`, `&` and
U+2028/9 stay escaped for v1 call sites. (A direct `json/v2.Marshal` does not
escape them — that is `json/v2`'s rule, not something odjson introduces.)

The per-method rules behind this, including what a `GOEXPERIMENT=nojsonv2`
build sees, are in
[docs/internals.md](./docs/internals.md#which-semantics-a-generated-method-follows).
It matters most if you publish a library: the generated methods are exported
API on your types, your users inherit them, and removing the file later is a
breaking change for them rather than a clean uninstall.

> [!WARNING]
> **Embedding.** A struct that embeds a generated type and does not get a
> codec of its own inherits the embedded type's methods, and would then encode
> as **only that embedded part** — silently dropping its own fields. Within a
> package the default covers this: every exported struct, plus everything
> `-recursive` reaches. The cases to watch are narrowing with `-type`, and
> **another module embedding one of your generated types** without running
> odjson over its own — there is no local signal when that happens, so a type
> you export is a type whose embedders need a codec too.

### It depends on standard library internals

The `json/v2` speed above does not come from generated code alone. It comes
from a **direct path** (`odjsonrt/direct.go`) that steps outside the standard
library's exported surface: for a top-level value under a plain, buffered,
option-free `json.Marshal` / `json.Unmarshal`, odjson writes into and reads
out of the `encoding/json/v2` coder's own buffer through `reflect`-computed
field offsets and `unsafe`, rather than through `jsontext`'s public API. It
does that because `json/v2`'s own codecs reach the same state through an
internal export the linker refuses to let any other module use, and the public
API charges a floor and a per-member duplicate-name check that put those
numbers out of reach.

**That is a dependency on unexported standard library layout, which no
compatibility promise covers.** It is fenced in so that it can only ever cost
speed:

- **verified against one Go minor at a time** (currently 1.27), and gated on
  it;
- **checked by type at init**, and **self-tested at init** by running the
  direct path through `json/v2` and comparing its answers, including the
  rejection of trailing data, with the public API's;
- **any failure disables it for the whole process**, and
  `odjsonrt.DirectEnabled` reports the outcome — assert it in a test if you
  want to be told;
- **`-tags odjson_safe` compiles it out** entirely;
- **every generated method keeps the public API path as its fallback**, and
  takes it for any nested value, any `io.Writer` / `io.Reader`, any option and
  every `encoding/json` call.

> [!IMPORTANT]
> **What this costs you on a new Go release.** When Go 1.28 ships, odjson does
> not trust a layout it has not verified: the direct path stays off until a
> release adds 1.28 support, and until then generated types run entirely on the
> public API path. Your code keeps compiling and keeps producing the same JSON —
> but the `json/v2` figures above are the direct path's, and without it the
> margin over the standard library narrows sharply, to the point where the
> encode side is no longer a win at all. Plan a version bump around a Go
> upgrade, or build with `-tags odjson_safe` if you would rather never depend on
> it. The measurements for both paths are in
> [docs/internals.md](./docs/internals.md#the-direct-path).

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
| `(T).MarshalJSONTo` / `(*T).UnmarshalJSONFrom` | [`encoding/json/v2`](https://pkg.go.dev/encoding/json/v2)'s streaming interfaces. What a `json.Marshal` reaches on Go 1.27, and where the speed is. |
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
  a future standard library closes the gap. (With one caveat, in
  [what adopting it changes](#the-json-on-the-wire): if you *publish* the
  types, the generated methods are exported API your users inherit, so
  removing the file is a breaking change for them.)

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
linting via `go tool lint ./... ./tools/...`, testing with `go test -race ./...`, and
Conventional Commits).

## License

[MIT](./LICENSE)

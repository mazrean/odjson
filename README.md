# odjson

English | [日本語](./README.ja.md)

[![CI](https://github.com/mazrean/odjson/actions/workflows/ci.yml/badge.svg)](https://github.com/mazrean/odjson/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/mazrean/odjson.svg)](https://pkg.go.dev/github.com/mazrean/odjson)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](./LICENSE)

**odjson** (*overdrive JSON*) is a CLI code generator that brings `encoding/json/v2` up to [`bytedance/sonic`](https://github.com/bytedance/sonic) speed without editing a line of your code.
JSON encoding and decoding that already goes through the standard `encoding/json/v2` gets 2.2×–3.5× faster by adding the comment below and running `go generate`.
```go
//go:generate go tool odjson -type User
```

Deleting the generated file (`odjson_gen.go`) puts everything back to plain `encoding/json/v2`.

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="./docs/assets/bench-dark.svg">
  <img alt="Time per operation, lower is better. Marshal large: encoding/json/v2 400 µs, with odjson 113 µs, sonic 116 µs, go-json 245 µs. Marshal small: 1036 ns, with odjson 305 ns, sonic 311 ns, go-json 380 ns. Unmarshal large: 1111 µs, with odjson 516 µs, sonic 526 µs, go-json 687 µs. Unmarshal small: 1910 ns, with odjson 601 ns, sonic 1023 ns, go-json 799 ns." src="./docs/assets/bench-light.svg" width="912">
</picture>

The benchmarks measure **2.2×–3.5× over `encoding/json/v2`** on encode and decode alike. They also put odjson ahead of [`goccy/go-json`](https://github.com/goccy/go-json) on every measurement, and level with [`bytedance/sonic`](https://github.com/bytedance/sonic) — which JIT-compiles hand-written assembly and uses SIMD — on three of the four, with a 1.7× lead on the small decode.

<details>
<summary>Benchmark environment and how to reproduce it</summary>

The benchmarks live in [`bench/`](./bench), a Go module of its own. It is kept separate so that `sonic`, `goccy/go-json` and the other comparison libraries never reach `github.com/mazrean/odjson`'s `go.mod`.

### Commands

The chart's figures come from the command below, with `benchstat` taking the median.

```sh
cd bench
go test -run xxx -bench 'Benchmark(Marshal|Unmarshal)/(json-v2|go-json|sonic)/' -benchmem -count 10 ./gen/ ./plain/
```

### Environment

| Item | Value |
| --- | --- |
| CPU | AMD Ryzen 9 7950X |
| OS | Linux (WSL2) |
| Go | 1.27.1 |
| Method | `bench/gen` and `bench/plain` each run 10 times; the median is quoted |
| Variance | measured with [`benchstat`](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat) |

### What is measured

Four subjects: `encoding/json` v1, `encoding/json/v2`, `sonic` and `goccy/go-json`. The benchmark names are `BenchmarkMarshal/<library>/<input>` and `BenchmarkUnmarshal/<library>/<input>`.
Both inputs are the same ones [`bytedance/sonic`](https://github.com/bytedance/sonic)'s own benchmarks use.

| Input | Size | Go type | Content |
| --- | ---: | --- | --- |
| `large` | ~616 KiB | `TwitterStruct` | `twitter.json` |
| `small` | ~340 B | `Book` | a small JSON document |

### Conditions

- `Unmarshal` reads into a fresh zero value on every iteration.
- `Marshal` uses a value decoded beforehand.
- Every case is warmed up once before the timer starts.
- The chart's figures come from separate processes. To compare generated code against reflection inside one process, use [`bench/ab`](./bench/ab).

The method and the assumptions behind it are written up in [bench/README.md](./bench/README.md) and [docs/internals.md](./docs/internals.md).

</details>

> [!IMPORTANT]
> odjson depends heavily on the internals of `encoding/json/v2` and `encoding/jsontext`. On any Go minor version other than the one it has been verified against (Go 1.27), the acceleration disables itself and the speed drops. Deleting the generated file also stops all of it immediately.

## Requirements

Go 1.27 or newer. Generated files import `encoding/json/jsontext`, so they do not compile on 1.26 and older.

`encoding/json` benefits too, since 1.27 implements it on top of `encoding/json/v2` internally, but odjson is tuned to get the most out of `encoding/json/v2`, which is the recommended way to use it. Through `encoding/json` the two encodes come out at 3.2× and the two decodes at 1.2×–1.5×: the encodes take the direct write into the internal buffer described below, while the decodes go through the public API, because the coder flags `encoding/json` sets allow input that the direct path's strict parsers refuse.

## Quick Start

Install odjson:

```sh
go get -tool github.com/mazrean/odjson@latest
```

Add a `//go:generate` comment:

```go
package model

//go:generate go tool odjson -type User,Post

type User struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email,omitempty"`
}
```

Running `go generate` then writes an `odjson_gen.go` file:

```sh
go generate ./...
```

After that, `encoding/json/v2` encodes and decodes far faster on its own:

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

## Install

`go get -tool` is the recommended way in. It puts `github.com/mazrean/odjson/odjsonrt`, which the generated code uses, under the same `go.mod` version management.
```sh
go get -tool github.com/mazrean/odjson@latest
go tool odjson -h
```

Other package managers work as well.
<details>
<summary>Standalone binary, Homebrew, Linux packages</summary>

### Binary

```sh
go install github.com/mazrean/odjson@latest
```

### Homebrew (macOS)

```sh
brew install --cask mazrean/tap/odjson
```

### Linux packages (Debian / Ubuntu / RHEL / Fedora / openSUSE / Alpine)

```sh
# Debian / Ubuntu
curl -LO https://github.com/mazrean/odjson/releases/latest/download/odjson_<version>_linux_amd64.deb
sudo dpkg -i odjson_<version>_linux_amd64.deb

# RHEL / Fedora / openSUSE
sudo rpm -i odjson_<version>_linux_amd64.rpm

# Alpine
apk add --allow-untrusted odjson_<version>_linux_amd64.apk
```

</details>

### Flags

```
odjson [flags] [packages]
```

With no package argument odjson generates for the package in the current directory, which is what `//go:generate` needs.

| Flag                | Default          | Description                                                                                                   |
| ------------------- | ---------------- | ------------------------------------------------------------------------------------------------------------- |
| `-type`             | all              | Comma-separated struct type names to generate for. The default is every struct declared in the package, unexported ones included.         |
| `-output`           | `odjson_gen.go`  | File name written into each matched package directory. A name, not a path.                                     |
| `-recursive`        | `true`           | Also generate codecs for struct types reachable from the selected ones, so nested values skip reflection too.  |
| `-escape-html`      | `true`           | Escape `<`, `>` and `&` in strings, matching `encoding/json`'s default.                                        |
| `-case-insensitive` | `false`          | In `UnmarshalJSON`, fall back to a case-insensitive member match the way `encoding/json` v1 does. Off by default, matching `encoding/json/v2`. |
| `-version`          |                  | Print the version and exit.                                                                                    |

## How it works

### Generating dedicated Marshal/Unmarshal code per type
A Go struct already carries everything about its JSON shape at compile time. `encoding/json/v2` and every other JSON library extract that at run time and encode and decode from it.
odjson reads the struct definitions ahead of time instead and writes out code dedicated to each type, which is where the speed comes from.
Concretely, the generated code encodes and decodes without reflection and without an intermediate `map[string]any`:
this performs JIT-grade optimisation at compile time and cuts the run-time overhead sharply.

- **Marshal**: appends struct fields straight to a byte slice
    - field names, quotes and separators are baked into the generated source as constants
    - the approach is based on [`sapphi-red/json-constantiater`](https://github.com/sapphi-red/json-constantiater)
- **Unmarshal**: streams the input straight into the struct's fields
    - no reflection, no intermediate `map[string]any`, no run-time field name lookup
    - member names are matched against the document's raw bytes, quotes included
    - bools, integers and simple floats are decoded inline

### Bolting onto the standard library

On top of that, making the dedicated code satisfy the standard library's `(T).MarshalJSONTo` / `(*T).UnmarshalJSONFrom` interfaces is what lets code that uses the standard library benefit from odjson without being changed.
Only `encoding/json/v2`, by offering streaming interfaces with that little overhead, made such an implementation possible in the first place.
The result keeps **the standard library's reliability** while reaching **github.com/bytedance/sonic's speed**.

The methods added to each type are these:

| Symbol                                         | Interface                                                  |
| ---------------------------------------------- | ---------------------------------------------------------- |
| `(T).MarshalJSONTo` / `(*T).UnmarshalJSONFrom` | [`encoding/json/v2`](https://pkg.go.dev/encoding/json/v2)'s streaming interfaces. What a `json.Marshal` reaches on Go 1.27, and where the speed is. |
| `(T).MarshalJSON` / `(*T).UnmarshalJSON`       | [`encoding/json`](https://pkg.go.dev/encoding/json) v1's interfaces, following v1's rules. |

Types that already implement `json.Marshaler`, `json.Unmarshaler`, `encoding.TextMarshaler` or `encoding.TextUnmarshaler` are left alone. Nothing about their behaviour changes, but they get nothing from odjson either.

One more thing on the encode side: writing to `(encoding/jsontext).Encoder` through the ordinary route costs too much in validation of what was written to reach the speed above. odjson writes straight into the encoder's internal buffer, working from `(*encoding/jsontext).Encoder`'s memory layout.
That is where a large part of the speed comes from, and it depends on the standard library's internal layout. So the path is only enabled on Go minor versions it has been verified against; on any other it disables itself at start-up and falls back to the implementation that goes through the public API. That still compiles and still produces the same JSON — only the speed drops. See [docs/internals.md](./docs/internals.md) for the details.


### Other small optimisations

A number of smaller optimisations are what close the remaining gap to `github.com/bytedance/sonic`:
- buffer reuse through `sync.Pool`
- a dedicated `float` formatter: a short decimal is proven by one division, anything else by a shortest-digit search that writes whole words instead of going through `strconv`
- word-at-a-time scanning
- fusing UTF-8 validation into the string scan, removing the separate validation pass

## What adopting it changes

Adopting odjson does not normally change behaviour, but a few cases can shift as described below.

### Changes to `encoding/json`'s behaviour

Since Go 1.27 `encoding/json` is implemented on top of `encoding/json/v2`, and `json/v2` prefers `MarshalJSONTo` when a type offers both. So an unchanged v1 call site reaches the encoder and decoder odjson generated for `encoding/json/v2`, and some behaviour shifts with it.

The concrete changes are these:
| Case | Without odjson | With odjson |
| --- | --- | --- |
| nil slice / map | `null` | `[]` / `{}` |
| nil `[]byte` | `null` | `""` |
| `omitempty` on `0` or `false` | omitted | **kept** |
| `null` into a scalar, struct, `time.Time` or `,string` field (decode) | left untouched | **zeroed** |
| array length mismatch (decode) | padded or truncated | **rejected** |
| member name in the wrong case (decode) | matched | **not matched** |
| map member order | sorted by name | sorted by name (*unchanged*) |
| `<`, `>`, `&`, U+2028/9 | escaped | escaped (*unchanged*) |

### Changes to embedding behaviour

When a struct is embedded as below, the `(T).MarshalJSONTo` / `(*T).UnmarshalJSONFrom` methods added to `A`, the type odjson accelerated, are inherited by `B` as well.
`B` then encodes and decodes through `A`'s JSON handling, and `FieldB` stops being recognised as a JSON member.
```go
// A is accelerated by odjson
type A struct {
	FieldA string `json:"field_a"`
}

// B embeds A
type B struct {
	A
	FieldB string `json:"field_b"`
}
```

For now, `B` has to be accelerated by odjson too.

## How reliable the encoding and decoding is

odjson's `MarshalJSON` / `UnmarshalJSON` behave exactly like `encoding/json`, and its `MarshalJSONTo` / `UnmarshalJSONFrom` exactly like `encoding/json/v2`. The test suites and fixtures below are what confirm it.

- the parity fixtures in `internal/testfixture/`
    - covering every field shape the generator can handle
    - every numeric width, `[]byte` and `[N]byte`, pointers, slices, arrays, maps, `any`, `json.RawMessage`, `json.Number`, `time.Time`, embedding by value and by pointer, `omitempty`, `omitzero`, `,string`, `-`, unexported fields, self-referential and cross-package types, generics and other fallbacks
    - compared against the standard library's encode and decode results, which must match exactly
- **the [JSON Test Suite](https://seriot.ch/security/parsing_json.html)** (318 cases)
    - run against the runtime scanner and the generated decoders
    - each case must produce the same value as the standard library
- **`encoding/json`'s field promotion rules**
    - reimplemented on `go/types`
    - checked against `encoding/json` on a struct built to hit all three cases

Parity is measured against the toolchain named in `go.mod`. Since Go 1.27 `encoding/json` is implemented on top of `encoding/json/v2` in v1 compatibility mode, and a few escape forms differ from the classic v1 encoder. For the exact list, see the package documentation of [`odjsonrt`](https://pkg.go.dev/github.com/mazrean/odjson/odjsonrt).

## License

[MIT](./LICENSE)

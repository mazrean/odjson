# AGENTS.md

Single source of truth for coding-agent instructions in this repository.
`CLAUDE.md` contains only `@AGENTS.md`; any other client-specific instruction
file (`GEMINI.md`, `.cursor/rules`, …) must point here rather than duplicate
this content.

## What odjson is

odjson is a CLI code generator that accelerates JSON encoding/decoding in Go.

A struct's field tags tell odjson, at compile time, exactly which JSON fields
are acceptable. From that it generates dedicated, reflection-free code for each
struct:

- `MarshalJSON` / `UnmarshalJSON` (`encoding/json` v1 interfaces)
- `MarshalJSONTo` / `UnmarshalJSONFrom` (`encoding/json/v2` interfaces)
- direct `MarshalT` / `AppendT` / `UnmarshalT` functions, for callers that want
  to skip the interface indirection entirely

Because every major Go JSON library honours those interfaces —
`encoding/json`, `encoding/json/v2`, `github.com/bytedance/sonic`,
`github.com/goccy/go-json` — odjson layers *on top of* whichever library the
user already has, and makes it faster. It does not replace them.

**Where the win actually is** (measured, see `bench/`): the direct functions,
always. `-methods` is a second win on the two standard libraries — it makes
`encoding/json/v2` 1.4-2.7x *faster* on all four measurements and
`encoding/json` 1.06-1.39x faster on three of four — but it cannot win on
sonic or go-json, and `bench/floor` proves why rather than asserting it: with
a `MarshalJSON` that costs nothing, sonic still spends 107us on the twitter
payload against 102us for its own reflection path, and go-json 347us against
239us. The interface floor is above the target, so no generated code can
close it. Do not re-open that question without re-running `bench/floor`. It
stays **off by default** for that reason: the recommendation is not uniform
across libraries, so the user has to make it. Keep the benchmark section of
`README.md` honest about this, and re-measure before changing the default.

The json/v2 numbers rest on `odjsonrt/direct.go`, **the direct path**: for a
top-level value under a plain `json.Marshal` / `json.Unmarshal` the generated
methods write into and read from the coder's own buffer through
reflect-computed offsets and `unsafe`, because `jsontext`'s public API charges
a floor (365us / 808ns for a marshaler that costs nothing) and a per-name
duplicate check that put 1.3x out of reach (see "The direct path" and "What
the decode side pays" in `README.md`). It is gated to the Go minor version it
was verified against (1.27), checked by type at init, self-tested through
json/v2 before use, and compiled out by `-tags odjson_safe`; every generated
method keeps the public API path as its fallback. **On a new Go minor,
re-verify the layout against `jsontext`'s source and widen the gate in
`calibrateDirect`; never widen it blind.** Anything parsed on the direct path
goes through the `*Strict` runtime parsers, which must reject exactly what
`jsontext` rejects (invalid UTF-8, unpaired surrogates, duplicate names at any
depth); `internal/testfixture/v2parity` is the guard.

The generated `MarshalJSONTo`/`UnmarshalJSONFrom` deliberately follow
`encoding/json/v2`'s semantics rather than `encoding/json`'s (nil slices encode
as `[]`, `omitempty` keeps zero numbers, array lengths are strict, member names
are matched case-sensitively). `odjsonrt.StringMode` is what selects between the
two; it is threaded through every generated `odjsonAppend`. Changing that
threading changes observable output, so `internal/testfixture/v2parity` and
`internal/testfixture/suitev2` must stay green.

Generation strategy:

- **Marshal**: direct struct-field-to-bytes appends (same idea as
  `sapphi-red/json-constantiater`).
- **Unmarshal**: a specialised parser per struct that streams straight into
  the struct's fields, with no reflection and no intermediate map. With
  `-methods` each struct gets two more decoders for `UnmarshalJSONFrom`:
  `odjsonParseFrom` drives the `jsontext.Decoder` member by member, and
  `odjsonParseV2` parses a byte slice under json/v2's semantics (null
  zeroes, arrays are strict, names are case sensitive). Its `strict`
  argument says whether those bytes still need json/v2's checks: `true` on
  the direct path, where nothing has looked at them, `false` when they came
  out of the decoder, which has already applied whatever options the caller
  passed. Never check again in the second case; that refuses what
  `AllowDuplicateNames` or `AllowInvalidUTF8` (and so every `encoding/json`
  call) explicitly allowed, and `TestLenientOptionsReachTheFallback` in
  `internal/testfixture/v2parity` guards it. `odjsonrt.WholeValue` picks
  between the two decoders per value at runtime: small values are read
  whole, large ones are driven token by token.

## Repository layout

Two Go modules:

| Path       | Module                             | Purpose                                        |
| ---------- | ---------------------------------- | ---------------------------------------------- |
| `.` (root) | `github.com/mazrean/odjson`        | `package main` — the `odjson` CLI + `odjsonrt` |
| `bench/`   | `github.com/mazrean/odjson/bench`  | Benchmarks against the four JSON libraries     |

Within the root module:

- `main.go` at the **repository root** is the CLI entry point. There is no
  `cmd/odjson` directory. The built binary is named `odjson`.
- `odjsonrt/` — `github.com/mazrean/odjson/odjsonrt`, the runtime support
  package that generated code imports. It is part of the root module and is a
  public API surface: treat breaking changes to it as breaking changes to the
  project.
- `internal/` — generator implementation, not part of the public API:
  `analyzer` (go/packages loading and encoding/json's field promotion rules),
  `codegen` (source rendering), `generate` (the driver), and `testfixture`,
  whose sub-packages each pin one area against `encoding/json`: the root
  package covers every scalar and composite field shape, `embed` covers field
  promotion and conflict resolution, `fallback` covers what the generator hands
  back to reflection, `crosspkg` covers types imported from another package,
  `suite` runs the JSON Test Suite through generated decoders, and
  `withmethods` covers `-methods`.

Every fixture's generated file is committed, and `internal/generate`'s tests
regenerate each one and fail on any difference. Regenerate with
`go generate ./...` from the repo root (and again from `bench/gen` — a separate
module) whenever the generator changes.
- `tools/lint/` — the repo's linter binary, wired in through the `tool`
  directive (see Linting).

`bench/` is a separate module on purpose: benchmarking pulls in `sonic`,
`goccy/go-json` and friends, and those must never become dependencies of the
root module. Never add a benchmark-only dependency to the root `go.mod`.

The CLI version is read from `runtime/debug.ReadBuildInfo()`. Do **not**
introduce `-X`/`ldflags` version injection, and do not disable `-buildvcs`.

## Tooling

- Build/generate/lint tools are managed with the Go 1.24+ `tool` directive in
  `go.mod`. Do **not** create or reinstate a `tools.go` file.
- CLI tool versions are pinned in `mise.toml` at the repository root
  (Go toolchain, `gopls`, `goreleaser`, `gh`, …). The coding-agent CLI itself
  is not pinned there.
- `gopls` backs the gopls MCP server; keep its pin in `mise.toml`.

## Linting

- `tools/lint/` is a package **inside the root module** producing a single
  linter binary that combines the `go vet` analyzer suite
  (`golang.org/x/tools/go/analysis/suite/vet`) with `staticcheck` and
  `stylecheck`, via `multichecker`. Analyzers that staticcheck marks
  non-default are skipped, matching upstream staticcheck defaults.
- Run it from the repository root:

  ```sh
  go tool lint ./...
  ```

- Do **not** pin or introduce `golangci-lint`. `tools/lint` is the canonical
  linter.
- Lint failures are blockers before commit.
- To add or bump a linter dependency, edit the root `go.mod`.
- **Why it is not its own module** (a deliberate deviation from the org-wide
  convention): a nested module would have to be wired in with `replace ./tools/lint`,
  and `go install github.com/mazrean/odjson@latest` refuses to install a module
  whose `go.mod` carries replace directives (`go help install`). odjson is a
  CLI whose primary install path is `go install`, so the replace cannot exist.
  The cost is that `staticcheck` appears in the root module graph.

## Testing

```sh
go test -v ./...      # local
go test -race ./...   # CI, and before anything that will be released
```

- Tests live next to the code in `*_test.go`; prefer table-driven cases.
- Generated code should be checked against a reference implementation
  (`encoding/json` at minimum).
- `odjsonrt/testdata/JSONTestSuite/` holds a JSON conformance corpus available
  for parser tests.

## Dependency injection

If compile-time DI is needed, use [`mazrean/kessoku`](https://github.com/mazrean/kessoku)
(added to the `tool` directive, invoked via `//go:generate go tool kessoku $GOFILE`).
Do not introduce `google/wire`.

## Spec-driven development

- New specs live under `specs/`. The legacy `.kiro/specs/` layout is
  deprecated, as are `cc-sdd` and `github/spec-kit`.
- Use the `writing-feature-spec`, `writing-technical-design`,
  `writing-implementation-tasks` and `writing-project-constitution` skills.

## Commits

- Conventional Commits, via the `committing-code` skill.
- Keep commits atomic.
- Never bypass hooks (`--no-verify`) without explicit authorization.
- Release tags are `vX.Y.Z`; pushing one triggers `.github/workflows/release.yml`.
  The tagged tree must be clean, otherwise the toolchain stamps the version as
  `+dirty`.

## Shared skills

Reusable, cross-repo skills belong in `mazrean/agent-skills`; per-stack
standards belong in `mazrean/apm-plackage/<stack>`. Do not add them to this
repo.

## Browser automation

Use the Playwright CLI plus the Playwright Agent Skill. Do not register a
Playwright MCP server.

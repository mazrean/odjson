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

**Where the win actually is** (measured, see `bench/`): the direct functions.
The `MarshalJSON` contract obliges the host library to make an interface call
and then re-scan and copy the bytes it gets back, and to locate a value's
extent before handing it to the decoder. Measured against the same code without
the methods, that overhead exceeds the reflection the generated codec removes
for every library except `encoding/json` on small payloads. That is why
`-methods` defaults to **off**: a generator must not make a program slower by
default. Keep this statement in `README.md`'s benchmark section; do not soften
it, and re-measure before changing the default.

Generation strategy:

- **Marshal**: direct struct-field-to-bytes appends (same idea as
  `sapphi-red/json-constantiater`).
- **Unmarshal**: a specialised parser per struct that streams straight into
  the struct's fields, with no reflection and no intermediate map.

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

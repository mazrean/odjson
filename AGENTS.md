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

- `MarshalJSONTo` / `UnmarshalJSONFrom` (`encoding/json/v2` interfaces)
- `MarshalJSON` / `UnmarshalJSON` (`encoding/json` v1 interfaces)

Those four methods are the **entire** generated API surface. There are no
exported direct functions: everything a caller reaches goes through the
standard interfaces, so an unchanged `json.Marshal` / `json.Unmarshal` picks
the generated codec up, and deleting the generated file undoes all of it. Do
not re-introduce package level `MarshalT` / `AppendT` / `UnmarshalT`; "keep
using the standard library, and take it off whenever you like" is the product,
and a second entry point contradicts it.

**Positioning** (measured, see `bench/`): the target is `encoding/json/v2`
(2.1-3.5x faster on all four measurements) and `encoding/json` (1.2-1.5x on
three of four; the twitter encode is 5% behind because v1's coder flags make
the direct path decline). `github.com/bytedance/sonic` and
`github.com/goccy/go-json` honour the v1 interfaces too and the generated code
is correct under them, but odjson does **not** make them faster: it wins only
their small unmarshal rows (sonic's by 1.10x and go-json's by 1.09x in
`bench/ab`), and `bench/floor` proves why the rest cannot be won rather than
asserting it: with a
`MarshalJSON` that costs nothing, sonic still spends 107us on the twitter
payload against 117us for its own path, and go-json 345us against 237us,
because sonic validates and go-json compacts whatever a marshaler returns.
On the decode side the floor is the skip-and-validate pass they make before
calling `UnmarshalJSON`: 260us and 470us on twitter, against their own 492us
and 655us, so the generated decoder would have to run 2.2x faster than sonic's
JIT to break even there. Do not re-open either question without re-running
`bench/floor` and `bench/ab`. In `README.md` those two libraries are
**comparison baselines only** — their "with odjson" columns stay out of the
tables, and the claim to keep honest is that json/v2 + odjson beats go-json on
all four (the small encode by 1.18x, the narrowest) and is level with sonic
on three of the four (within 5%, in either direction across runs; `bench/ab`
in one process reads 1.03x / 1.03x / 1.01x) and 1.7x ahead on the small decode
(1.58x in `bench/ab`). Re-measure before restating any of it.

`-case-insensitive` defaults to **false**, matching json/v2; it only affects
the v1 `UnmarshalJSON` path. The root and `embed` fixtures pass it explicitly,
because their parity oracle is `encoding/json` v1, which folds case.

The byte oriented decoder (`odjsonParse`, behind `UnmarshalJSON`,
the direct path and the whole-value path) matches known member names against
the document's raw bytes, quotes included, before scanning anything, and
decodes bools, integers and simple floats inline; the general runtime parsers
are the fallback and the only thing that produces an error. Anything added to
that decoder has to keep the parity fixtures green, and any new fast path must
decline rather than guess: `internal/testfixture`'s scalar cases cover the
spellings the raw match cannot see.

The json/v2 numbers rest on `odjsonrt/direct.go`, **the direct path**: for a
top-level value under a plain `json.Marshal` / `json.Unmarshal` the generated
methods write into and read from the coder's own buffer through
reflect-computed offsets and `unsafe`, because `jsontext`'s public API charges
a floor (365us / 808ns for a marshaler that costs nothing) and a per-name
duplicate check that put 1.3x out of reach (see "The direct path" and "What
the decode side pays" in `docs/internals.md`). It is gated to the Go minor version it
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
  the struct's fields, with no reflection and no intermediate map. Each
  struct gets two more decoders for `UnmarshalJSONFrom`:
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

Three Go modules:

| Path       | Module                            | Purpose                                        |
| ---------- | --------------------------------- | ---------------------------------------------- |
| `.` (root) | `github.com/mazrean/odjson`       | `package main` — the `odjson` CLI + `odjsonrt` |
| `bench/`   | `github.com/mazrean/odjson/bench` | Benchmarks against the four JSON libraries     |
| `tools/`   | `github.com/mazrean/odjson/tools` | `lint` and `apicompat` (see Tooling)           |

Both nested modules exist to keep dependencies **out of the root module's
graph**, because everything in it is downloaded by anyone who depends on
odjson. Neither imports the root module, so neither needs a `replace`
directive — which is what keeps `go install github.com/mazrean/odjson@latest`
working (`go help install`: it refuses a module whose `go.mod` carries
replace directives). Never add a benchmark-only or tool-only dependency to
the root `go.mod`.

`go.work` at the repository root joins `.` and `./tools`, which is what makes
`go tool lint` and `go tool apicompat` resolve from the root even though the
`tool` directives live in `tools/go.mod`. It is committed deliberately,
against the usual advice not to: a workspace is never seen by `go install
github.com/mazrean/odjson@latest`, nor by anyone importing `odjsonrt`, so it
costs consumers nothing and saves every contributor from writing it by hand.

`bench/go.work` (`use .`) exists because a root `go.work` makes every nested
module directory it does not `use` fail with "directory prefix . does not
contain modules listed in go.work"; its own one-line workspace is what keeps
`cd bench && go test` working. Both `go.work.sum` files are committed
alongside their `go.work`, for the same reason `go.sum` is. `bench/` is kept out of the root workspace on
purpose: in a workspace MVS runs over the union of every `use`d module, so
`sonic`'s requirements would start choosing versions for root builds and
tests.

Within the root module:

- `main.go` at the **repository root** is the CLI entry point. There is no
  `cmd/odjson` directory. The built binary is named `odjson`.
- `odjsonrt/` — `github.com/mazrean/odjson/odjsonrt`, the runtime support
  package that generated code imports. It is part of the root module and is a
  public API surface: treat breaking changes to it as breaking changes to the
  project.
- `internal/` — generator implementation, not part of the public API:
  `analyzer` (go/packages loading and encoding/json's field promotion rules),
  `codegen` (builds the file as a `go/ast` tree and prints it; `emit.go`
  explains how statements get the positions that drive the printer's
  layout), `generate` (the driver), and `testfixture`,
  whose sub-packages each pin one area against `encoding/json`: the root
  package covers every scalar and composite field shape, `embed` covers field
  promotion and conflict resolution, `fallback` covers what the generator hands
  back to reflection, `crosspkg` covers types imported from another package,
  `suite` runs the JSON Test Suite through generated decoders, and
  `withmethods` covers a minimal type reached through every library.

  Because the generated methods are what `encoding/json` now calls, the parity
  oracle cannot be `json.Marshal` on the fixture type itself. Each affected
  fixture declares its types a second time in a sibling `plain` package that
  odjson never runs on, and `plainref.Of` reads a value as that twin;
  `plainref.SameLayout`, asserted by a `TestSameLayout` in each fixture, is
  what keeps the two declarations from drifting. Where no field type carries a
  generated codec (`fallback`, `crosspkg`) a locally defined type is enough
  and no `plain` package exists. Parity tests call `MarshalJSON` /
  `UnmarshalJSON` directly, because `json.Marshal` would reach the v2 methods
  and their v2 semantics instead.

Every fixture's generated file is committed, and `internal/generate`'s tests
regenerate each one and fail on any difference. Regenerate with
`go generate ./...` from the repo root (and again from `bench/gen` — a separate
module) whenever the generator changes.
- `docs/internals.md` — the measurement record and the implementation detail
  behind `README.md`'s summary: the public API ceiling, the direct path, what
  the decode side pays, the v1/v2 semantics table, and why sonic and go-json
  cannot be sped up. `README.md` stays short and links here; anything measured
  and rejected belongs in this file rather than being deleted.

`bench/` is a separate module on purpose: benchmarking pulls in `sonic`,
`goccy/go-json` and friends, and those must never become dependencies of the
root module. `tools/` is separate for the same reason: it pulls in
`staticcheck` and `golang.org/x/exp`, which nobody who merely uses odjson
should have to download.

The CLI version is read from `runtime/debug.ReadBuildInfo()`. Do **not**
introduce `-X`/`ldflags` version injection, and do not disable `-buildvcs`.

## Tooling

- Build/generate tools are managed with the Go 1.24+ `tool` directive. Do
  **not** create or reinstate a `tools.go` file.
- Those `tool` directives live in **`tools/go.mod`**, not in the root one, and
  the root `go.work` is what lets `go tool lint` / `go tool apicompat` still
  be typed from the repository root. The root `go.mod` must **never** regain a
  `tool` line: that is the exact change that puts `staticcheck` and
  `golang.org/x/exp` back into the graph of everyone who imports `odjsonrt`.
- To add a tool, add its package under `tools/` and a `tool` line to
  `tools/go.mod`. To add or bump a tool dependency, edit `tools/go.mod` —
  never the root one.
- CLI tool versions are pinned in `mise.toml` at the repository root
  (Go toolchain, `gopls`, `goreleaser`, `gh`, …). The coding-agent CLI itself
  is not pinned there.
- `gopls` backs the gopls MCP server; keep its pin in `mise.toml`.

## Linting

- `tools/lint` is a single linter binary combining the `go vet` analyzer suite
  (`golang.org/x/tools/go/analysis/suite/vet`) with `staticcheck` and
  `stylecheck`, via `multichecker`. Analyzers that staticcheck marks
  non-default are skipped, matching upstream staticcheck defaults.
- Run it from the repository root:

  ```sh
  go tool lint ./... ./tools/...
  ```

  `./...` resolves to the root module only, even in workspace mode, so
  `./tools/...` is what also lints the tools themselves. Both patterns are
  needed; neither implies the other.
- Do **not** pin or introduce `golangci-lint`. `tools/lint` is the canonical
  linter.
- Lint failures are blockers before commit.

## API compatibility

- `tools/apicompat/` compares one package between two checkouts with
  `golang.org/x/exp/apidiff` and exits non-zero on an **incompatible** change.
  Compatible additions are never printed: a gate that reports them is a gate
  people learn to ignore.

  ```sh
  go tool apicompat -base /path/to/base-checkout github.com/mazrean/odjson/odjsonrt
  ```

- **`odjsonrt` is the surface that matters.** Generated files import it, so an
  incompatible change there breaks every tree that has already run the
  generator — including trees whose owners will not regenerate before
  upgrading. The root package is `package main` and has nothing to break.
- It lives in the `tools` module, so `golang.org/x/exp` stays out of the root
  module's graph; `go tool apicompat` reaches it through the root `go.work`.
- CI (`.github/workflows/ci.yml`, the `apicompat` job) compares against the
  PR's base commit — **not** against `<module>@latest`, which is what
  `mazrean/kessoku` does and which odjson cannot do until it has a release
  tag. Once tags exist, a push to `main` compares against the newest one; until
  then that path emits a `::notice` and skips.
- On a breaking change the job fails and writes the diff to its job summary.
  It deliberately does **not** comment: `bench.yml` already comments as
  `github-actions[bot]` and edits its own last comment, and a second bot
  comment would become the one it edits. kessoku comments here because it is
  the only bot on its PRs.

## CI

Four workflows, all under `.github/workflows/`:

- `ci.yml` — build, test, lint, the `apicompat` gate, and a compile-only pass
  over the `bench/` module.
- `bench.yml` — runs `bench/plain` and `bench/gen` on the PR head, renders the
  README's chart from those numbers (`go run ./chart -input …`), and comments
  the image on the PR. **Those numbers are not the README's**: a shared
  two-core runner is much noisier than the machine `README.md` and
  `docs/internals.md` quote, so the chart's footer names the runner and the
  comment carries the raw `go test` output. Never restate a CI figure as a
  measured claim — re-run locally first.

  The PNGs go on an orphan `bench-images` branch, one directory per PR head
  commit, and the comment links them as
  `github.com/<repo>/blob/bench-images/…?raw=true`. That branch is generated
  output — never merge from it, and never add to it by hand.

  Every other way in is closed, so do not "simplify" this back into one of
  them:

  - `raw.githubusercontent.com` is a cookie-less host. While odjson is
    private it answers 404 without a signed token, and a token cannot go in a
    comment. `github.com` carries the reader's own session instead, and GitHub
    does not route its own domains through the camo image proxy — checked with
    `POST /markdown`, which returns those `src` attributes unrewritten. The
    `blob` form keeps working if odjson opens up.
  - `gh pr comment --attach` uploads to GitHub's own attachment host, which
    would need no branch at all, but only under a **user** token: the Actions
    token is server-to-server and gh rejects it with `unsupported
    authentication type`. It would also need a `gh` newer than the runner
    image ships, and it rewrites `![alt](path)` and only that — an HTML `src=`
    is left untouched, and so is a path carrying a `#gh-dark-mode-only`
    fragment, so `<picture>` would cost a second pass. All three were tried.
  - GitHub strips `data:` URIs, and a workflow artifact has no URL of its own.

  PNG rather than the README's SVG, because GitHub serves SVG as `text/plain`,
  which `<img>` will not render.

  The comment itself is one `gh pr comment --edit-last --create-if-none`, so
  there is no script to maintain — which is also why `apicompat` reports
  through its job summary instead of commenting.
- `release.yml` — GoReleaser, on a `vX.Y.Z` tag.
- `renovate.yml` — **self-hosted** Renovate, weekly plus `workflow_dispatch`.
  There is no Mend-hosted app on this repository: the bot is this workflow, and
  it runs under the same GitHub App as `release.yml` (which is why `APP_ID` /
  `APP_PRIVATE_KEY` must also exist at the repository level, outside the
  `Release` environment — a cron job must never wait on an environment
  reviewer). See "Dependency updates".

`bench.yml` is skipped for PRs from forks, whose token can neither push nor
comment.

## Dependency updates

Repository configuration is `.github/renovate.json5`; the options Renovate only
accepts globally live in `renovate.yml` as `RENOVATE_*` variables. The `json5`
form is deliberate — the reasoning below has to sit next to the rules it
explains.

**Renovate has no `go.work` support**: it neither reads the file nor writes
`go.work.sum` (renovatebot/renovate#23359, #37653). Three modules and two
workspaces make that worth spelling out, and each claim here was measured
against this tree on go1.27.1:

- `postUpdateOptions: ["gomodTidy"]` is safe. `go mod tidy` is idempotent in
  all three modules with the workspace active, and `cd bench && go mod tidy` is
  unmoved by a change to the root module — so `gomodTidyAll` is not needed.
- A stale `go.work.sum` does **not** break CI: the go command writes the
  entries it needs on the next build, even under the default `-mod=readonly`.
  What it does is let the committed file drift, so a `postUpgradeTasks` chain
  (`go work sync`, `go mod tidy`, `go -C tools mod tidy`, `go -C bench work
  sync`) puts it back. The chain is a no-op on a clean tree.
- `go work sync` writes the workspace-selected version into each `use`d
  module's `go.mod`, and that is the point: in a workspace MVS runs over the
  union of `.` and `./tools`, so a bump landing in `tools/go.mod` alone already
  changes what the root module builds against. It only ever *raises* a
  requirement, so it cannot revert the bump Renovate just made.
- Do **not** switch to `postUpdateOptions: ["gomodMassage"]` for `bench/`'s
  `replace github.com/mazrean/odjson => ../`. It comments the replace out
  before running `go`, which leaves `go get` fetching the placeholder
  `v0.0.0-00010101000000-000000000000` from the proxy, where it does not exist.
- A `mise.toml` `go` minor/major bump needs a tick on the dependency dashboard,
  because odjson's direct path is gated to the Go minor it was verified against
  and an unattended bump would switch it off silently. Match it with
  `matchDepNames: ["go"]`: the mise manager's packageName for it is
  `golang/go`, so a `matchPackageNames: ["go"]` rule would never fire.

Validate a change to the config before pushing it:

```sh
npx --yes --package renovate -- renovate-config-validator --strict --no-global \
  .github/renovate.json5
```

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
(added to the `tool` directive in **`tools/go.mod`**, never the root one,
invoked via `//go:generate go tool kessoku $GOFILE`). Do not introduce
`google/wire`.

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

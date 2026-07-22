# port-scan-mk3 — Agent & Developer Guide

`CLAUDE.md` is a symlink to this file. Keep this file short; it is loaded into
every session. Detailed rules live in the files linked below — read them when
their trigger applies.

## Read before you act
- **Governance for AI agents:** `.claude/rules/00-diagnostic.md` (start here),
  then `.claude/rules/10-model-dispatch.md` before delegating,
  `.claude/rules/20-judgment-rubric.md` before declaring work done, and
  `.claude/rules/40-maintenance-protocol.md` before editing any rule file.
- **Project law:** `.claude/rules/constitution.md`. Its MUST-level rules
  (Test-First, Quality Gates, SOLID boundaries, Go 1.24.x + stdlib `net`)
  override convenience. When in doubt, the constitution wins.

## Build & Test Commands
- Build all binaries (Linux + Windows): `make build`
- Build one command: `go build ./cmd/port-scan` (commands live in `cmd/*/`;
  there is **no** `cmd/app`)
- Run tests: `make test` (`go test -race -shuffle=on ./...`)
- **Full quality gate (run before claiming done): `make verify`** — runs gofmt,
  `go vet`, build, race tests, and the >=85% coverage gate. It mirrors CI
  exactly. Add the isolated Docker e2e with `make verify-e2e`.
- Format: `make fmt` · Lint: `make lint` (uses golangci-lint if installed) ·
  Tidy: `go mod tidy` · List targets: `make help`

## Definition of Done (do not skip)
1. `make verify` exits 0. Paste the final result line as evidence.
2. If you changed the scan pipeline, writers, or pressure control, also run
   `make verify-e2e` (needs Docker) and confirm it exits 0.
3. New production behavior started with a failing test (constitution III).
Never claim "done", "fixed", or "passing" without the command output.

## Coding Standards
- **Idiomatic Go**: accept interfaces, return concrete types.
- **Errors**: always check; wrap with `fmt.Errorf("...: %w", err)`.
- **Naming**: camelCase internal, PascalCase exported.
- **Concurrency**: prefer channels; guard shared state. Anything shared across
  scan worker goroutines (a logger, a buffer) needs a mutex — see the mutex in
  `pkg/scanapp/scan_logger.go`. Always test concurrency with `-race`.
- **Library-first**: new scanner behavior goes in `pkg/` with unit tests before
  any `cmd/` wiring (constitution I).
- **Cross-platform**: code must build and run on Linux and Windows. Use
  `filepath`, `t.TempDir()`, and `runtime.GOOS` rather than hardcoded paths.
- **Modern Go**: Go 1.24.x runtime; use the 1.21+ stdlib features it includes
  (`slices`, `maps`, etc.).

## Where things live
- `cmd/` CLI entrypoints · `pkg/` reusable domain logic · `internal/testkit`
  test helpers · `e2e/` isolated Docker e2e · `scripts/` gate scripts ·
  `docs/` docs and release notes · `.claude/rules/` governance + constitution.

## Agent skills

### Issue tracker

Issues live in this repo's GitHub Issues (`superplayground3000/port-scan-mk3`),
managed via the `gh` CLI. See `docs/agents/issue-tracker.md`.

### Triage labels

Default label vocabulary: `needs-triage`, `needs-info`, `ready-for-agent`,
`ready-for-human`, `wontfix`. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: `CONTEXT.md` + `docs/adr/` at the repo root (created lazily by
`/domain-modeling`). See `docs/agents/domain.md`.

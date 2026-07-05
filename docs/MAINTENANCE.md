# Maintainability Baseline

This document is the operator's contract for keeping port-scan-mk3 healthy over
time. It ties together the constitution's Quality Gates, the one-command
verification flow, cross-platform support, and a complete runnable example.
Every command below was executed and verified when this file was written.

- **Project law:** [`.claude/rules/constitution.md`](../.claude/rules/constitution.md)
- **Agent/dev guide (always loaded):** [`../AGENTS.md`](../AGENTS.md)
- **Governance for AI agents:** [`.claude/rules/00-diagnostic.md`](../.claude/rules/00-diagnostic.md)

## 1. The quality gate (single source of truth)

Everything runs through `scripts/verify.sh`, exposed as make targets. CI
(`.github/workflows/ci.yml`) calls the same scripts, so **green locally means
green in CI**.

| Command | What it runs | When |
|---|---|---|
| `make verify` | gofmt · `go vet` · `go build` · `go test -race -shuffle=on` · coverage ≥85% | Before every "done" |
| `make verify-e2e` | all of the above **plus** the isolated Docker e2e suite | When you touch the scan pipeline, writers, or pressure control |
| `make test` | `go test -race -shuffle=on ./...` | Quick inner loop |
| `make cover` | coverage gate only (`scripts/coverage_gate.sh`) | Checking the 85% floor |
| `make fmt` / `make fmt-check` | format in place / fail if unformatted | Before committing |
| `make e2e` | `e2e/run_e2e.sh` only | Isolated e2e alone |
| `make help` | list all targets | Discovery |

Rule: never declare work complete without `make verify` exiting 0. See
[`20-judgment-rubric.md`](../.claude/rules/20-judgment-rubric.md).

### Why `-race` matters
`go test ./...` (no `-race`) and the coverage gate cannot detect concurrency
bugs. The `-race` path in `make verify` caught a real data race in the shared
scan logger. Do not remove `-race` from the test path.

## 2. Cross-platform (Windows + Linux)

The product cross-compiles for both from the Makefile:

```bash
make build          # builds dist/linux/* and dist/windows/*.exe for every cmd/
make build-linux    # Linux x64 only
make build-windows  # Windows x64 only
```

- **Product code** must build and run on Linux and Windows: use `filepath`,
  `t.TempDir()`, and `runtime.GOOS` instead of hardcoded paths.
- **Dev scripts** (`scripts/verify.sh`, `e2e/run_e2e.sh`) are bash. On Windows,
  run them from **Git Bash** or **WSL**. `go build` / `go test` / `make` work
  natively on Windows with a POSIX-shell make.
- CI runs the full gate on Linux and additionally builds + tests on
  `windows-latest`.

## 3. Complete runnable example (self-contained, loopback only)

No Docker or external network needed — the scan targets `127.0.0.1` only
(constitution V: never scan real external hosts). From the repo root:

```bash
# Validate inputs (human-readable)
go run ./cmd/port-scan validate \
  -cidr-file example/port-scan/cidr.csv \
  -port-file example/port-scan/ports.csv \
  -format human

# Run the scan against loopback, API + pre-scan-ping disabled
go run ./cmd/port-scan scan \
  -cidr-file example/port-scan/cidr.csv \
  -port-file example/port-scan/ports.csv \
  -disable-api=true \
  -disable-pre-scan-ping=true \
  -timeout 500ms \
  -output example/out/scan.csv
```

Expected: a structured completion summary on stderr and timestamped result CSVs
in `example/out/` (git-ignored). Verified output line:

```
[INFO] scan_completion fields=map[close_count:4 ... open_count:2 ... total_tasks:6 ... success:true]
```

More examples for every CLI tool: [`example/README.md`](../example/README.md).

## 4. Isolated end-to-end test

`e2e/run_e2e.sh` (via `make verify-e2e` or `make e2e`) stands up mock targets
and mock pressure APIs in Docker Compose on an isolated network, runs the
scanner as a container, asserts open/closed/timeout detection and
pressure-control failure handling, and writes report artifacts to `e2e/out/`.
It requires Docker + `docker compose`. It touches no real hosts.

## 5. Coverage floor (mind the margin)

Total coverage sits just above the 85% floor and is carried by aggregate; six
packages are individually below 85% (see
[`00-diagnostic.md`](../.claude/rules/00-diagnostic.md) Problem 3). When you add
production code, add tests **in the same package, same change**, then confirm:

```bash
go tool cover -func=coverage.out | tail -1   # total must be >= 85%
```

Never lower the threshold, delete tests, or extend `EXCLUDE_PATTERN` in
`scripts/coverage_gate.sh` to pass — that requires user approval
([`40-maintenance-protocol.md`](../.claude/rules/40-maintenance-protocol.md)).

## 6. Known cross-platform & e2e follow-ups (tracked debt)

The first CI run surfaced pre-existing issues that predate the CI itself. The
Linux quality gate is green and **blocking**; the Windows and e2e jobs run as
**non-blocking** (`continue-on-error: true` in `ci.yml`) until these are fixed.
Do not delete the jobs — fix the causes and flip them back to blocking.

**Windows test portability** (`go build` on Windows passes; these tests fail):
- `cmd/cidr-compare` tests exec a helper via `./cidr-compare-test` — needs a
  `.exe` suffix / proper path on Windows.
- `pkg/preprocess` `TestOutputPath*` assert forward-slash paths but production
  uses `filepath` (backslashes on Windows) — fix the test expectations to build
  paths with `filepath.Join`.
- `pkg/scanapp` default resume path is hardcoded `/tmp/resume_state.json` — make
  it `os.TempDir()`-based (a real production portability fix), and update the
  test.
- `TestEnsureFDLimit_WhenWorkersExceedLimit_ReturnsError` relies on the Unix
  `RLIMIT_NOFILE` check — skip it on Windows (`runtime.GOOS == "windows"`).

**e2e determinism:**
- The `api_timeout` failure-injection scenario in `e2e/run_e2e.sh` is
  timing-sensitive: the scan can complete before the pressure-timeout turns
  fatal on fast runners. Make the scenario deterministic (e.g. more work, or a
  hard fail signal) so the assertion is stable.

Each fix must follow test-first (constitution III). When a job is green on
Windows/CI, remove its `continue-on-error` in `.github/workflows/ci.yml`.

## 7. Release evidence

Product releases use semver and ship `docs/release-notes/<version>.md` with
features, fixes, breaking changes, and migration guidance (constitution VII).
Keep documentation in sync with code on every change
([`.claude/rules/documents.md`](../.claude/rules/documents.md)).

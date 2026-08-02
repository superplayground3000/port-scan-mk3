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
- CI runs the full gate on Linux and additionally runs the **native Windows
  gate** (below) on `windows-latest`.

### 2.1 Native Windows gate (`scripts/windows_gate.ps1`)

`make verify` needs a POSIX shell and `make verify-e2e` builds and runs **Linux**
binaries inside Docker containers — see section 4. Neither of them ever executes
a Windows `.exe`. `scripts/windows_gate.ps1` is that missing half, and it is the
only place the logic lives; `.github/workflows/ci.yml` just calls it, so the CI
job and a developer's machine run exactly the same checks.

```powershell
# from the repo root, in PowerShell (pwsh 7 or Windows PowerShell 5.1)
.\scripts\windows_gate.ps1
# add -KeepWorkspace to inspect the scratch files after a failure
```

What it asserts (issue #63, automating the high-value parts of the manual plan
in issue #60 without Docker):

| # | Check | Why it can only be done natively |
|---|---|---|
| 1 | environment report (`go version`, `go env`) and `go env GOOS` is `windows` | cross-compiling is not proof of native execution |
| 2 | `TEMP`/`TMP`/`GOTMPDIR` are ASCII, redirected if not | MSYS2 GCC fails on non-ASCII temp paths |
| 3 | 64-bit MinGW-w64 gcc present, `CGO_ENABLED=1`, plus a probe module with a known data race that the detector **must** report | proves `-race` is armed instead of silently degraded |
| 4 | `go vet ./...`, `go build ./...` | — |
| 5 | `go test -race -shuffle=on -count=1 ./...` | same test line as `scripts/verify.sh` |
| 6 | build **every** command under `cmd/` as an `.exe` and launch each one | catches missing-DLL / bad-image failures |
| 7 | loopback `generate-buckets -> scan`: the listening port is `open`, every unused port is not | — |
| 8 | loopback `generate-buckets -> scan (aborted) -> scan -resume`: one header, every target exactly once | Windows file-sharing semantics on append-reopen |
| 9 | every output directory name contains a **space**; every produced file is renamed and finally deleted right after the process exits | an unreleased handle makes a rename/delete fail on Windows, but never on Linux |

Everything it scans is `127.0.0.0/8` created by the script itself (constitution
V). The interruption in check 8 is produced by pointing `-pressure-api` at a
refused loopback port rather than by a signal: Windows has no real SIGINT and
`os.Process.Signal(os.Interrupt)` is unsupported there, so signal-driven
cancellation stays honestly unverified (section 6).

**Prerequisites — runtime vs race-test. These are not the same list.**

- *RUNTIME prerequisites* (needed to build and run the product on Windows):
  Go 1.24.x and nothing else. No cgo, no C compiler. `make build-windows`
  cross-compiles the same binaries from Linux.
- *RACE-TEST prerequisites* (needed for `go test -race`, i.e. for this gate):
  additionally a **64-bit** MinGW-w64 C compiler (`x86_64-w64-mingw32`) on
  `PATH` and `CGO_ENABLED=1`. Install with `choco install mingw`, or MSYS2
  `pacman -S mingw-w64-x86_64-gcc`, or use the gcc shipped with Strawberry Perl.
  A 32-bit (`i686`) compiler cannot build the race runtime.
  On a machine whose user profile or `TEMP` contains non-ASCII characters, also
  point `TEMP`, `TMP` and `GOTMPDIR` at an ASCII path — MSYS2 GCC cannot create
  its own temp files otherwise. The gate does this automatically; do it by hand
  if you run `go test -race` directly:

  ```powershell
  $env:TEMP = 'C:\gotmp'; $env:TMP = 'C:\gotmp'; $env:GOTMPDIR = 'C:\gotmp'
  ```

If the compiler is missing the gate **fails**. It never falls back to a non-race
run: a green "tests passed" line that silently dropped `-race` is worse than a
red job, because it looks like coverage that does not exist. The contract tests
in `internal/ciguard/windows_gate_test.go` run inside `make verify` on every
platform and keep the script and the workflow honest. `internal/ciguard` is a
**test-only** package — it holds no production code and ships in no binary; its
sole job is to make CI-config drift fail a normal `go test`. Those tests fail if `cmd/` grows
a command the gate does not launch, if `-race`/`-shuffle=on` or the compiler
`throw` disappears, if a non-loopback address appears in the script, or if the
Windows job stops calling the script or becomes non-blocking.

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

**The Docker e2e suite is LINUX coverage only.** `e2e/scanner/Dockerfile` builds
the scanner inside a Linux image and every scenario runs Linux binaries in Linux
containers, even when you launch `make verify-e2e` from a Windows host. It
proves nothing about Windows file handles, path shapes, or `.exe` startup. The
Windows-native equivalent is `scripts/windows_gate.ps1` (section 2.1); the two
are complements, not substitutes, and neither one covers the other's platform.

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

The Linux quality gate and the **Windows** job are both green and **blocking**.
The e2e job still runs **non-blocking** (`continue-on-error: true` in `ci.yml`).
Do not delete a job, and never re-add `continue-on-error` to turn a red run
green — fix the cause.

**Windows test portability — DONE** (PRs #48/#49/#50, issue #47). Master is green
on Windows with zero skips. Recorded here because the fixes corrected two pieces
of guidance this section previously gave, and a future agent should not repeat
them:

- `cmd/cidr-compare` tests exec'd `./cidr-compare-test`. Root cause was the
  missing `.exe` suffix (Windows decides executability by `PATHEXT`), not the
  relative path. Fixed with a shared `buildTestBinary(t)` helper that builds
  into `t.TempDir()` and returns an absolute path. Until then these tests never
  launched the binary on Windows at all — they had zero coverage there.
- `pkg/preprocess` `TestOutputPath*` asserted forward-slash paths. **Do not fix
  this by building the expectation with `filepath.Join`** (the earlier advice
  here): that calls the same function production calls, making the assertion
  tautological — it would pass by construction and could never disagree with the
  code. Fixed by comparing `filepath.ToSlash(got)` against the literal
  expectation, keeping an independent source of truth.
- The `/tmp/resume_state.json` expectation was **only in the test**. Production
  was already portable — `defaultResumeStateFile` is a bare filename
  (`pkg/scanapp/scan.go:19`) joined via `filepath.Join`
  (`pkg/scanapp/resume_path.go:20`). The earlier advice to make production
  `os.TempDir()`-based would have changed working code for no reason.
- `TestEnsureFDLimit_WhenWorkersExceedLimit_ReturnsError` relies on Unix
  `RLIMIT_NOFILE`. **Do not fix this with `t.Skip` on Windows** (the earlier
  advice here): skipping deletes the contract instead of verifying it. Fixed by
  splitting the test by build tag so each platform asserts its own documented
  contract — unix keeps the error assertion, windows asserts the no-op contract
  in `fdlimit_windows.go`.
- Three timing tests (dashboard refresh, pressure poll, cancel-drain) were
  load-dependent flakes: Windows' default timer granularity is ~15.6ms, far
  coarser than Linux. Fixed by waiting for events with a generous timeout
  (`internal/testkit.WaitFor`) rather than sleeping a fixed budget. **Lengthening
  a sleep is not a fix** — it raises the stake on the same gamble.

**e2e determinism — still open:**
- The `api_timeout` failure-injection scenario in `e2e/run_e2e.sh` is
  timing-sensitive: the scan can complete before the pressure-timeout turns
  fatal on fast runners. Make the scenario deterministic (e.g. more work, or a
  hard fail signal) so the assertion is stable, then drop that job's
  `continue-on-error`.

**Windows native validation — now automated** (issue #63). The Part 2 gaps from
`docs/windows-ci-fix/design.md` — file-handle release after a run, the full
generate-buckets→scan→resume flow, append-reopen under Windows sharing
semantics, `.exe` startup, and path shapes containing spaces — are covered by
`scripts/windows_gate.ps1` (section 2.1), which the blocking `windows-latest`
job runs on every push and PR. The race detector is proven armed there rather
than assumed.

**Still uncovered on Windows:**
- **Interrupt-signal delivery.** Windows has no real SIGINT and
  `os.Process.Signal(os.Interrupt)` is unsupported there, so Ctrl+C-driven
  cancellation is left honestly unverified rather than faked. The gate reaches
  the same *resume* state through a pressure-API failure instead, which
  exercises the append-reopen path but not the signal path.
- **Windows ARM64.** The gate runs on `windows-latest` x64 only (explicitly out
  of scope for issue #63).

Each fix must follow test-first (constitution III).

## 7. Release evidence

Product releases use semver and ship `docs/release-notes/<version>.md` with
features, fixes, breaking changes, and migration guidance (constitution VII).
Keep documentation in sync with code on every change
([`.claude/rules/documents.md`](../.claude/rules/documents.md)).

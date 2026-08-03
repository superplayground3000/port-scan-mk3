# Maintainability Baseline

This document is the operator's contract for keeping port-scan-mk3 healthy over
time. It ties together the constitution's Quality Gates, the one-command
verification flow, cross-platform support, and a complete runnable example.
Every command below was executed and verified when this file was written.

- **Project law:** [`.claude/rules/constitution.md`](../.claude/rules/constitution.md)
- **Agent/dev guide (always loaded):** [`../AGENTS.md`](../AGENTS.md)
- **Governance for AI agents:** [`.claude/rules/00-diagnostic.md`](../.claude/rules/00-diagnostic.md)

## 1. The quality gate (single source of truth)

Everything runs through `scripts/verify.sh`, exposed as make targets, so
**green locally means green in CI**.

One honest caveat: CI does not literally call `verify.sh`. It calls
`scripts/coverage_gate.sh` and `e2e/run_e2e.sh`, but its Linux `gate` job
inlines gofmt/vet/build/test instead of invoking the script — so a check added
only to `verify.sh` would never run in CI. That is why the line-ending rules
below are additionally guarded by a Go test, which both CI jobs do run.
Switching the `gate` job to `bash scripts/verify.sh` would remove the caveat;
`.claude/rules/90-letter-to-future-sessions.md` already names that as the
intended design, and `.github/workflows/ci.yml` is owned by issues #63/#71.

| Command | What it runs | When |
|---|---|---|
| `make verify` | line endings · gofmt · `go vet` · `go build` · `go test -race -shuffle=on` · coverage ≥85% | Before every "done" |
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
make build          # builds dist/linux/* and dist/windows/*.exe for every cmd/,
                    # then runs the artifact gate below
make build-linux    # Linux x64 only
make build-windows  # Windows x64 only
make verify-dist    # artifact gate only (checks whatever is in dist/ right now)
                    # honours DIST_DIR: `make verify-dist DIST_DIR=x` gates x/
```

### Release artifact rules (issue #65)

- **Cross-builds are explicit.** Both recipes set `GOOS`, `GOARCH` and
  `CGO_ENABLED` themselves and never inherit them from the build host, so
  `dist/linux/` always holds `linux/amd64` and `dist/windows/` always holds
  `windows/amd64` — whether you build on Linux, on Windows, or in CI.
  Windows ARM64 is deliberately not produced.
- **The build loops are fail-fast** (`set -e`). A shell `for` loop's exit status
  is the status of its *last* iteration, so before #65 a command that failed to
  compile in the middle of the loop was masked by a later success: `make build`
  exited 0 with an artifact missing from `dist/`. Any single command build
  failure now aborts the target immediately. Do not remove `set -e` from those
  recipes.
- **`CGO_ENABLED=0` for release artifacts.** Decided in #65. With cgo enabled
  the output depends on whether the *build host* has a C toolchain, and the
  Linux binary links dynamically against that host's glibc — the same source
  then yields materially different artifacts on different machines, which is
  exactly the non-determinism #65 is about. With `CGO_ENABLED=0` the Linux
  binary is statically linked and portable, and Go uses the pure-Go (`netgo`)
  resolver. That is sufficient here because the scanner uses only stdlib `net`
  (constitution "Technology Stack"); the accepted trade-off is that host lookups
  no longer go through glibc NSS plugins (LDAP, mDNS, …) — `/etc/hosts` and DNS
  still work normally. On Windows Go has no cgo resolver at all, so the setting
  only makes the existing behavior explicit. This also aligns the Makefile with
  the production-like path that already existed: `e2e/scanner/Dockerfile:6`
  builds the scanner with `CGO_ENABLED=0 GOOS=linux GOARCH=amd64`.
  To build a cgo-linked binary
  deliberately, override it: `make build CGO_ENABLED_RELEASE=1` (the artifact
  gate will then correctly reject it).
- **Release artifacts are byte-for-byte reproducible.** `buildTime` is derived
  from the commit (`TZ=UTC0 git log -1 --date=format-local:...`), not the wall
  clock, and every build passes `-trimpath` so absolute source paths are not
  baked in. Two builds of the same commit produce identical bytes. Before #65
  the recipes stamped `date -u`, so every rebuild differed and a published
  binary could not be checked against the commit it claimed.
  `test_build_recipes.sh` TEST 5 pins this along both axes that used to leak:
  5a builds twice across a wall-clock second boundary and a timezone change (the
  `buildTime` axis), and 5b builds the same commit from two different absolute
  paths (the `-trimpath` axis), each asserting byte-identical output.
  The guarantee is scoped: it holds for the same commit built from the **same
  repo tag metadata**, in a *clean* checkout. `VERSION` comes from `git describe
  --always --dirty`, which is not a pure function of the commit alone — a dirty
  tree stamps differently on purpose, and two clones of the same commit that can
  see different tags resolve a different `VERSION`, which changes the `-ldflags`
  string recorded in build info and therefore the bytes. Pinning a fully
  commit-derived version belongs with the tag-driven release contract in issue
  #70; until then, read the guarantee as "same commit **and** same tag
  metadata", not "same commit" unconditionally.
- **`dist/` is no longer tracked** and is git-ignored. This deliberately
  reverses commit `97b4e8f` ("track linux and windows binaries in dist/"),
  which had committed the 10 prebuilt binaries and dropped `dist/` from
  `.gitignore` with a `!dist/windows/*.exe` negation. Committed binaries went
  stale on every source change and bloated the repo, and — now that builds are
  reproducible — they add nothing a rebuild cannot reproduce exactly.
  **The replacement tag-driven release flow that would publish these artifacts
  is issue #70 and does not exist yet**, so as of this change the binaries are
  **not obtainable by cloning**: build them yourself with `make build` (they
  land in the ignored `dist/`). This is a known, temporary gap until #70 ships.
- **The artifact gate is `scripts/verify_dist.sh`.** It is not a comment or a
  convention: for every `cmd/*/main.go` it asserts the artifact exists and that
  both the toolchain build info (`go version -m`) *and* the on-disk executable
  header (ELF vs. PE `MZ`) agree with the directory it sits in. It discovers the
  command list itself rather than trusting the Makefile, so a command the
  Makefile forgot to build is caught. It also refuses to report success if it
  inspected zero artifacts, so an emptied target list cannot make it pass
  vacuously. It always gates the directory the Makefile actually built into
  (`make build DIST_DIR=x` verifies `x`, not `dist/`).
  Running it on a fresh clone without building first is expected to fail — it
  verifies what you built, not what is committed under `dist/`.
- **The recipes themselves are pinned by `scripts/test_build_recipes.sh`.**
  The #65 fixes live in Makefile recipes, which `go test` cannot reach, so this
  is the retained regression suite that keeps them fixed: it drives the real
  `build-linux`/`build-windows`/`build` targets and asserts (1) a mid-loop
  compile failure aborts the target *and stops it* rather than being masked by
  a later success, (2) a hostile `GOOS`/`GOARCH` in the environment cannot leak
  into either cross-build — checked against the produced binary's ELF/PE
  header, (3) `make build` gates the `DIST_DIR` it wrote, (4) the artifact
  gate cannot pass vacuously, and (5) two builds of the same commit are
  byte-for-byte identical — both across a wall-clock/timezone change (5a) and
  across two different absolute build paths (5b), the two inputs a commit-derived
  `buildTime` and `-trimpath` exist to neutralize (see the reproducibility
  bullet above). Every test
  builds into a temporary `DIST_DIR`, so the suite never touches the working
  `dist/` tree — TEST 3 asserts this by fingerprinting `dist/` on disk before
  and after (a `git status -- dist` check would go vacuous now that `dist/` is
  git-ignored). It runs in `bash scripts/verify.sh` (hence in `make verify`)
  and CI calls the same script, so local and CI cannot drift apart.

- **Product code** must build and run on Linux and Windows: use `filepath`,
  `t.TempDir()`, and `runtime.GOOS` instead of hardcoded paths.
- **Dev scripts** (`scripts/verify.sh`, `e2e/run_e2e.sh`) are bash. On Windows,
  run them from **Git Bash** or **WSL**. `go build` / `go test` / `make` work
  natively on Windows with a POSIX-shell make.
- CI runs the full gate on Linux — after `go build ./...` it runs
  `bash scripts/test_build_recipes.sh` (the same recipe suite `make verify`
  runs), so the fail-fast cross-build recipes and the `verify_dist.sh` artifact
  gate are exercised on every PR, not just `go build ./...`. It does **not** run
  `make clean build`: the suite builds into a temporary `DIST_DIR` instead of
  cleaning and rewriting the ignored `dist/` tree. CI additionally builds +
  tests on `windows-latest`.

### Line endings are owned by the repository, not by your git config

`.gitattributes` at the repository root pins what lands on disk, so a checkout
is the same on every machine. Without it, git falls back to each developer's
`core.autocrlf`; the Git for Windows installer offers `core.autocrlf=true` as
its default, which rewrites LF to CRLF at checkout time — and then `gofmt -l`
rejects a pristine clone before anyone has edited a line (issue #64), while
bash refuses the CRLF shell scripts.

The policy, and why:

| Pattern | Rule | Reason |
|---|---|---|
| `*` | `text=auto eol=lf` | Default: git detects text vs binary, normalises text to LF in the index, checks out LF everywhere. |
| `*.go`, `*.sh`, `Makefile`, `Dockerfile`, `*.yml`, `*.mod`, `*.sum` | `text eol=lf` | Redundant with the catch-all **on purpose** — these are the ones whose breakage is a build failure, so they survive a careless edit of the catch-all. |
| `*.ps1` | `text eol=lf` | PowerShell 5.1 and 7 both run LF-only scripts, and the repository's only `.ps1` is LF and linted in a Linux container. Switch to `eol=crlf` if this project ever ships Authenticode-signed scripts — `Set-AuthenticodeSignature` appends a CRLF signature block. |
| `*.bat`, `*.cmd` | `text eol=crlf` | The one deliberate exception. `cmd.exe` parses a batch file line by line as it executes it, and label/multi-line handling depends on the CR. There is no batch file in the repository yet; the rule is here so the first one is correct by default. |
| `dist/**`, `*.exe`, `*.pptx`, image/archive types | `binary` | `binary` is git's macro for `-text -diff`: never EOL-converted (that would corrupt them), never diffed as text. `text=auto` would auto-detect most of these, but detection is a heuristic and a corrupted committed binary is silent. |

Two checks defend this, because they catch different regressions:

- **`scripts/verify.sh`** (`make verify`, first step) compares *your* working
  tree against the declared attributes via `git ls-files --eol`, and fails if
  `.gitattributes` is missing. Fast, and names the real cause instead of
  letting gofmt blame your source.
- **`tests/repohygiene/line_endings_test.go`** re-materialises the tracked
  files through `git checkout-index` with `core.autocrlf=true` forced on and
  asserts the result is LF and gofmt-clean. It reproduces the Windows failure
  deterministically *from any platform*, and it runs in CI on both the Linux
  (`go test -race -shuffle=on ./...`) and Windows (`go test ./...`) jobs.

If you ever do end up with a mismatched tree:

```bash
git add --renormalize .
git checkout-index -f -a
```

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

The Linux quality gate, the **Windows** job, and the Docker **e2e** job are all
green and **blocking** (issue #71 removed the e2e job's `continue-on-error`; see
the determinism note below). Do not delete a job, and never re-add
`continue-on-error` to turn a red run green — fix the cause.

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

**e2e determinism — resolved (issue #71):**
- The pressure-failure scenarios in `e2e/run_e2e.sh` (`api_5xx`, `api_timeout`,
  `api_conn_fail`) used to be timing-sensitive: `api_timeout` could finish its
  tiny scan before the pressure-timeout path turned fatal, so it passed without
  ever exercising the fatal abort. The **mock-backed** scenarios (`api_5xx`,
  `api_timeout`) are now **event-driven** and no longer race a clock. The mock
  pressure API counts every failure it serves (`GET /admin/stats`); each
  mock-backed scenario baselines that counter, runs the scan in the background,
  and **waits** for the mock to serve the scanner's fatal threshold (3
  consecutive failures) before judging the run — watching the scan PID so an
  early exit fails loudly instead of hanging.
- `api_conn_fail` is deliberately **not** mock-watched: it points the scanner at
  a closed port (`127.0.0.1:9`), so there is no mock to instrument and it passes
  an empty `watch_service`. It is not event-driven, and it does not need to be —
  it cannot pass vacuously, because the shared reason-specific assertions below
  still apply: exit 0 is rejected, exit 124 is rejected, and the fatal pressure
  log line plus a resumable snapshot are both required.
- The correctness of the assertion, not just its timing, was hardened. A
  non-zero exit alone is not accepted: the scenario **rejects exit 0** (should
  have failed), **rejects exit 124** (the outer `timeout` hard-kill — a hang, not
  a pressure abort), requires the fatal log line `pressure api failed 3 times`
  (so an unrelated error cannot masquerade as the pressure path), and validates
  the snapshot with `assert-resume-snapshot -require-progress -require-remaining`
  (the snapshot path IS the scan's `-resume` input, so it must show both an
  advanced cursor and work still pending). A scan that hangs after the pressure
  path goes fatal is still caught: the outer `timeout` hard-kills it at the
  `SCAN_HARD_LIMIT` ceiling with exit 124, which the scenario rejects as a hang
  rather than accepting as a pressure abort.
- The `/24` fail workload is a **liveness margin, not the correctness
  mechanism**: its ~50s scan floor guarantees the scan is still running when the
  threshold is served. Shrinking it degrades to a *loud red* (the PID-watch
  detects an early exit and fails with a diagnosis), never a silent vacuous
  pass. Do not "optimize" it down and then read a red as flakiness.
- Known trade-off: `pressure_failures_served` swallows `docker compose exec`
  and parse errors (returns empty), so a broken mock surfaces via the wait
  deadline rather than immediately. This is deliberate — the deadline path fails
  loudly with a diagnosis — but noted so it is a documented choice, not an
  accident.
- Because the correctness signal is the served-failure event plus the
  reason-specific assertions above (not the scan being slow enough), the Docker
  e2e job is now **blocking** (`continue-on-error` removed).

**Still uncovered on Windows** (Part 1 paid down test-quality debt; it did not
add Windows-specific coverage). See `docs/windows-ci-fix/design.md` Part 2:
file-handle release after a run, the full prep→scan→resume flow (e2e is
Docker/Linux-only, so it has never run on Windows), append-reopen under Windows
sharing semantics, and Windows path shapes. Interrupt-signal delivery is a
documented gap — Windows has no real SIGINT and `os.Process.Signal(os.Interrupt)`
is unsupported there, so it is left honestly unverified rather than faked.

Each fix must follow test-first (constitution III).

## 7. Release evidence

Product releases use semver and ship `docs/release-notes/<version>.md` with
features, fixes, breaking changes, and migration guidance (constitution VII).
Keep documentation in sync with code on every change
([`.claude/rules/documents.md`](../.claude/rules/documents.md)).

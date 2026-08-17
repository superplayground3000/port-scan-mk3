# Maintainability Baseline

This document is the operator's contract that keeps port-scan-mk3 healthy over
time. It ties together the Quality Gates of the constitution, the one-command
verification flow, cross-platform support, and a complete runnable example.
When this file was written, the author ran every command below and made sure
that it works.

- **Project law:** [`.claude/rules/constitution.md`](../.claude/rules/constitution.md)
- **Agent/dev guide (always loaded):** [`../AGENTS.md`](../AGENTS.md)
- **Governance for AI agents:** [`.claude/rules/00-diagnostic.md`](../.claude/rules/00-diagnostic.md)

## 1. The quality gate (single source of truth)

Everything runs through `scripts/verify.sh`, exposed as make targets, so
**green locally means green in CI**.

One honest caveat: CI does not literally call `verify.sh`. It calls
`scripts/coverage_gate.sh` and `e2e/run_e2e.sh`, but its Linux `gate` job
inlines gofmt/vet/build/test instead of a call to the script. Thus a check that
exists only in `verify.sh` never runs in CI. For that reason, a Go test also
guards the line-ending rules below, and both CI jobs do run that test. A change
of the `gate` job to `bash scripts/verify.sh` removes the caveat.
`.claude/rules/90-letter-to-future-sessions.md` already names that as the
intended design, and issues #63/#71 own `.github/workflows/ci.yml`.

| Command | What it runs | When |
|---|---|---|
| `make verify` | line endings · gofmt · `go vet` · `go build` · `go test -race -shuffle=on` · coverage ≥85% | Before every "done" |
| `make verify-e2e` | all of the above **plus** the isolated Docker e2e suite | When you touch the scan pipeline, writers, or pressure control |
| `make test` | `go test -race -shuffle=on ./...` | Quick inner loop |
| `make cover` | coverage gate only (`scripts/coverage_gate.sh`) | Checking the 85% floor |
| `make fmt` / `make fmt-check` | format in place / fail if unformatted | Before committing |
| `make e2e` | `e2e/run_e2e.sh` only | Isolated e2e alone |
| `make verify-performance` | complete native Linux large-data matrix | Large-input evidence and hot-path changes |
| `make help` | list all targets | Discovery |

Rule: never declare work complete until `make verify` exits 0. See
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
  `CGO_ENABLED` themselves, and they never inherit them from the build host.
  Thus `dist/linux/` always holds `linux/amd64` and `dist/windows/` always holds
  `windows/amd64`, whether you build on Linux, on Windows, or in CI.
  The build deliberately does not produce Windows ARM64.
- **The build loops are fail-fast** (`set -e`). The exit status of a shell `for`
  loop is the status of its *last* iteration. So before #65, a later success hid
  a command that failed to compile in the middle of the loop: `make build`
  exited 0 with an artifact missing from `dist/`. Now any single command build
  failure aborts the target immediately. Do not remove `set -e` from those
  recipes.
- **`CGO_ENABLED=0` for release artifacts.** Decided in #65. With cgo enabled,
  the output depends on whether the *build host* has a C toolchain. The Linux
  binary then links dynamically against the glibc of that host. The same source
  then gives materially different artifacts on different machines, which is
  exactly the non-determinism that #65 is about. With `CGO_ENABLED=0` the Linux
  binary is statically linked and portable, and Go uses the pure-Go (`netgo`)
  resolver. That is sufficient here because the scanner uses only stdlib `net`
  (constitution "Technology Stack"). The accepted trade-off is that host lookups
  no longer go through glibc NSS plugins (LDAP, mDNS, …), but `/etc/hosts` and
  DNS still work normally. On Windows Go has no cgo resolver, so the setting
  only makes the existing behavior explicit. This also aligns the Makefile with
  the production-like path that already existed: `e2e/scanner/Dockerfile:6`
  builds the scanner with `CGO_ENABLED=0 GOOS=linux GOARCH=amd64`.
  To build a cgo-linked binary
  deliberately, override the setting: `make build CGO_ENABLED_RELEASE=1` (the
  artifact gate then correctly rejects it).
- **Release artifacts are byte-for-byte reproducible.** The Makefile derives
  `buildTime` from the commit (`TZ=UTC0 git log -1 --date=format-local:...`),
  not from the wall clock. Every build also passes `-trimpath`, so no absolute
  source path goes into the binary. Two builds of the same commit produce
  identical bytes. Before #65 the recipes stamped `date -u`, so every rebuild
  differed. A check of a published binary against the commit it claimed was not
  possible. `test_build_recipes.sh` TEST 5 pins this along both axes that used to
  leak. 5a builds twice across a wall-clock second boundary and a timezone
  change (the `buildTime` axis). 5b builds the same commit from two different
  absolute paths (the `-trimpath` axis). Each one asserts byte-identical output.
  The guarantee is scoped: it holds for the same commit built from the **same
  repo tag metadata**, in a *clean* checkout. `VERSION` comes from `git describe
  --always --dirty`, which is not a pure function of the commit alone. A dirty
  tree stamps differently on purpose. Two clones of the same commit that see
  different tags resolve a different `VERSION`. That changes the `-ldflags`
  string in the build info, and therefore the bytes. A fully commit-derived
  version belongs with the tag-driven release contract in issue #70. Until then,
  read the guarantee as "same commit **and** same tag metadata", not "same
  commit" unconditionally.
- **`dist/` is no longer tracked** and is git-ignored. This deliberately
  reverses commit `97b4e8f` ("track linux and windows binaries in dist/"),
  which committed the 10 prebuilt binaries and dropped `dist/` from
  `.gitignore` with a `!dist/windows/*.exe` negation. Committed binaries went
  stale on every source change and made the repository large. Builds are
  reproducible now, so they add nothing that a rebuild cannot reproduce exactly.
  **The replacement tag-driven release flow for these artifacts is issue #70,
  and it does not exist yet.** So as of this change, a clone gives you
  **no binaries**. Build them yourself with `make build` (they
  land in the ignored `dist/`). This is a known, temporary gap until #70 ships.
- **The artifact gate is `scripts/verify_dist.sh`.** It is not a comment or a
  convention. For every `cmd/*/main.go` it asserts that the artifact exists. It
  also asserts that both the toolchain build info (`go version -m`) *and* the
  on-disk executable header (ELF vs. PE `MZ`) agree with the directory that
  holds the artifact. It discovers the command list itself and does not trust
  the Makefile, so it catches a command that the Makefile forgot to build.
  If it inspected zero artifacts, it also refuses to report success, so an
  emptied target list cannot make it pass vacuously. It always gates the
  directory that the Makefile built into (`make build DIST_DIR=x` gates `x`,
  not `dist/`).
  On a fresh clone with no build first, it fails, and that is expected: it
  gates what you built, not what is committed under `dist/`.
- **`scripts/test_build_recipes.sh` pins the recipes themselves.**
  The #65 fixes live in Makefile recipes, which `go test` cannot reach. So this
  is the retained regression suite that keeps them fixed. It drives the real
  `build-linux`/`build-windows`/`build` targets. It asserts (1) that a mid-loop
  compile failure aborts the target *and stops it*, and that no later success
  hides it. It asserts (2) that a hostile `GOOS`/`GOARCH` in the environment
  cannot leak into either cross-build — the test reads the ELF/PE header of the
  produced binary. It asserts (3) that `make build` gates the `DIST_DIR` it
  wrote, and (4) that the artifact gate cannot pass vacuously. It asserts (5)
  that two builds of the same commit are byte-for-byte identical.
  TEST 5 covers both a wall-clock/timezone change (5a)
  and two different absolute build paths (5b). Those are the two inputs that a
  commit-derived `buildTime` and `-trimpath` exist to neutralize (see the
  reproducibility bullet above). Every test
  builds into a temporary `DIST_DIR`, so the suite never touches the working
  `dist/` tree. TEST 3 asserts this: it fingerprints `dist/` on disk before
  and after (a `git status -- dist` check goes vacuous now that `dist/` is
  git-ignored). It runs in `bash scripts/verify.sh` (and thus in `make verify`),
  and CI calls the same script, so local and CI cannot drift apart.

- **Product code** must build and run on Linux and Windows: use `filepath`,
  `t.TempDir()`, and `runtime.GOOS` instead of hardcoded paths.
- **Dev scripts** (`scripts/verify.sh`, `e2e/run_e2e.sh`) are bash. On Windows,
  run them from **Git Bash** or **WSL**. `go build` / `go test` / `make` work
  natively on Windows with a POSIX-shell make.
- CI runs the full gate on Linux. After `go build ./...` it runs
  `bash scripts/test_build_recipes.sh`, the same recipe suite that `make verify`
  runs. So every PR exercises the fail-fast cross-build recipes and the
  `verify_dist.sh` artifact gate, not only `go build ./...`. CI does **not** run
  `make clean build`: the suite builds into a temporary `DIST_DIR`, and it does
  not clean and rewrite the ignored `dist/` tree. CI also runs the
  **native Windows gate** (below) on `windows-latest`.

### 2.1 Native Windows gate (`scripts/windows_gate.ps1`)

`make verify` needs a POSIX shell, and `make verify-e2e` builds and runs
**Linux** binaries inside Docker containers — see section 4. Neither of them ever
runs a Windows `.exe`. `scripts/windows_gate.ps1` is that missing half, and it is
the only place where the gate logic lives. `.github/workflows/ci.yml` only calls
it, so the CI job and a developer's machine run exactly the same checks.

The workflow runs one script **before** the gate.
`scripts/windows_setup_mingw.ps1` provisions the race-build prerequisites: a
64-bit MinGW-w64 gcc (installed with Chocolatey when the machine has none) and
ASCII `TEMP`/`TMP`/`GOTMPDIR`. It exports them to the gate step through
`$GITHUB_ENV`/`$GITHUB_PATH`. This is deliberate: **the job must not depend on
what the `windows-latest` image happens to preinstall** (issue #63). That
dependence breaks with no message on the day GitHub changes the image. If the
setup script cannot provision a genuine 64-bit gcc, it **throws**, so the gate
never degrades to a non-race run. It is idempotent: a developer with MinGW-w64
already installed can run it (or skip it) and then run the gate in the same
shell.

```powershell
# from the repo root, in PowerShell (pwsh 7 or Windows PowerShell 5.1)
.\scripts\windows_setup_mingw.ps1   # provision 64-bit MinGW-w64 + ASCII temp
.\scripts\windows_gate.ps1
# add -KeepWorkspace to inspect the scratch files after a failure
```

What it asserts (issue #63, which automates the high-value parts of the manual
plan in issue #60 without Docker):

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
| 9 | every output directory name contains a **space**, and the test renames and then deletes every produced file directly after the process exits | an unreleased handle makes a rename or a delete fail on Windows, but never on Linux |

Everything it scans is `127.0.0.0/8` that the script itself created
(constitution V). Check 8 does not use a signal for the interruption. It points
`-pressure-api` at a refused loopback port, because Windows has no real SIGINT
and does not support `os.Process.Signal(os.Interrupt)`. So signal-driven
cancellation stays honestly unverified (section 6).

**Prerequisites — runtime vs race-test. These are not the same list.**

- *RUNTIME prerequisites* (needed to build and run the product on Windows):
  Go 1.24.x and nothing else. No cgo, no C compiler. `make build-windows`
  cross-compiles the same binaries from Linux.
- *RACE-TEST prerequisites* (needed for `go test -race`, that is, for this
  gate): additionally a **64-bit** MinGW-w64 C compiler (`x86_64-w64-mingw32`)
  on `PATH` and `CGO_ENABLED=1`. Install with `choco install mingw`, or MSYS2
  `pacman -S mingw-w64-x86_64-gcc`, or use the gcc that Strawberry Perl ships.
  A 32-bit (`i686`) compiler cannot build the race runtime.
  If the user profile or `TEMP` of the machine contains non-ASCII characters,
  also point `TEMP`, `TMP` and `GOTMPDIR` at an ASCII path. Otherwise MSYS2 GCC
  cannot create its own temp files. The gate does this automatically. If you run
  `go test -race` directly, do it by hand:

  ```powershell
  $env:TEMP = 'C:\gotmp'; $env:TMP = 'C:\gotmp'; $env:GOTMPDIR = 'C:\gotmp'
  ```

If the compiler is missing, the gate **fails**. It never degrades to a non-race
run. A green "tests passed" line that dropped `-race` gives no message. That is
worse than a red job, because it looks like coverage that does not exist. The contract
tests in `internal/ciguard/windows_gate_test.go` and
`internal/ciguard/windows_setup_test.go` run inside `make verify` on every
platform, and they keep the scripts and the workflow honest. `internal/ciguard`
is a **test-only** package. It holds no production code and ships in no binary.
Its sole job is to make CI-config drift fail a normal `go test`. Those tests fail
if `cmd/` grows a command the gate does not run. They fail if `-race`/`-shuffle=on`
or the compiler `throw` disappears. They fail if a non-loopback address appears in
the script. They fail if the Windows job no longer calls the script or becomes
non-blocking. They also fail if the job no longer provisions the 64-bit MinGW-w64
compiler and ASCII temp before the gate (that is, it depends on the runner image
again).

### 2.2 Native Windows validation (`scripts/windows_pressure_validation.ps1`)

The gate answers "is this change safe to merge". It cannot answer "does this
test flake on Windows", because a gate runs each test one time and a flake needs
repetition. `scripts/windows_pressure_validation.ps1` is that second question,
and `.github/workflows/windows-validation.yml` dispatches it by hand.

This is **not** a gate, and it must never become one. It repeats the pressure API
family 100 times by default (issue #79) and then runs the full Windows gate. A
run that fails is the point: the failure log is the evidence that issue #99
tracks. That is why the workflow uploads its artifact with `if: always()` and
why the script writes its log and its environment record before it returns a
status.

Dispatch it from the Actions tab, or with
`gh workflow run windows-validation.yml -f count=100`. Use `-f skip_gate=true`
to run only the family loop. On your own Windows machine, run the script
directly; it falls back to the system temp directory when `RUNNER_TEMP` is
absent.

`internal/ciguard/windows_pressure_validation_test.go` pins the contract, so
`make verify` catches drift on Linux before a dispatch spends a Windows runner.
It fails if the run loses `-race`, `-shuffle=on`, the repetition count, or the
`if: always()` upload, and it fails if the workflow starts reacting to `push` or
`pull_request`.

### Line endings are owned by the repository, not by your git config

`.gitattributes` at the repository root pins what lands on disk, so a checkout
is the same on every machine. Without it, git uses each developer's
`core.autocrlf` instead. The Git for Windows installer offers `core.autocrlf=true`
as its default, which rewrites LF to CRLF at checkout time. Then `gofmt -l`
rejects a pristine clone before anyone edited a line (issue #64), and
bash refuses the CRLF shell scripts.

The policy, and why:

| Pattern | Rule | Reason |
|---|---|---|
| `*` | `text=auto eol=lf` | Default: git detects text vs binary, normalizes text to LF in the index, and checks out LF everywhere. |
| `*.go`, `*.sh`, `Makefile`, `Dockerfile`, `*.yml`, `*.mod`, `*.sum` | `text eol=lf` | Redundant with the catch-all **on purpose** — when these files break, the build fails, so they survive a careless edit of the catch-all. |
| `*.ps1` | `text eol=lf` | PowerShell 5.1 and 7 both run LF-only scripts, and the only `.ps1` in the repository is LF and linted in a Linux container. If this project ever ships Authenticode-signed scripts, switch to `eol=crlf` — `Set-AuthenticodeSignature` appends a CRLF signature block. |
| `*.bat`, `*.cmd` | `text eol=crlf` | The one deliberate exception. `cmd.exe` parses a batch file line by line as it executes it, and label/multi-line handling depends on the CR. There is no batch file in the repository yet. The rule is here so that the first one is correct by default. |
| `dist/**`, `*.exe`, `*.pptx`, image/archive types | `binary` | `binary` is the git macro for `-text -diff`. Git never converts the EOLs of these files, because that conversion corrupts them, and it never diffs them as text. `text=auto` detects most of these files automatically, but the detection is a heuristic, and a corrupted committed binary gives no signal. |

Two checks defend this, because they catch different regressions:

- **`scripts/verify.sh`** (`make verify`, first step) compares *your* working
  tree against the declared attributes via `git ls-files --eol`, and fails if
  `.gitattributes` is missing. It is fast, and it names the real cause instead
  of a gofmt failure that blames your source.
- **`tests/repohygiene/line_endings_test.go`** re-materializes the tracked
  files through `git checkout-index` with `core.autocrlf=true` forced on. It
  then asserts that the result is LF and gofmt-clean. It reproduces the Windows
  failure deterministically *from any platform*, and it runs in CI on both the
  Linux (`go test -race -shuffle=on ./...`) and Windows (`go test ./...`) jobs.

If your tree ever has mismatched line endings:

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
in `example/out/` (git-ignored). Output line from a real run:

```
[INFO] scan_completion fields=map[close_count:4 ... open_count:2 ... total_tasks:6 ... success:true]
```

More examples for every CLI tool: [`example/README.md`](../example/README.md).

## 4. Isolated end-to-end test

`e2e/run_e2e.sh` (via `make verify-e2e` or `make e2e`) starts mock targets
and mock pressure APIs in Docker Compose on an isolated network. It runs the
scanner as a container. It asserts open/closed/timeout detection and
pressure-control failure handling, and it writes report artifacts to `e2e/out/`.
It requires Docker + `docker compose`. It touches no real hosts.

**The Docker e2e suite is LINUX coverage only.** `e2e/scanner/Dockerfile` builds
the scanner inside a Linux image, and every scenario runs Linux binaries in Linux
containers. This is true even when you run `make verify-e2e` from a Windows host.
It proves nothing about Windows file handles, path shapes, or `.exe` startup. The
Windows-native equivalent is `scripts/windows_gate.ps1` (section 2.1). The two
are complements, not substitutes, and neither one covers the other's platform.

### 4.1 Large-data performance evidence

`make verify-performance` runs the complete Linux matrix.
The command uses `scripts/performance_gate.sh` and writes JSON and Markdown reports.

Linux and Windows CI run bounded performance smoke cases.
These cases use 100,000 records and a 100 MB snapshot.
GitHub-hosted runners supply correctness and instrumentation evidence only.

Read [`performance-harness.md`](performance-harness.md) for the complete matrix contract.
That document also defines report fields, evidence labels, and Windows release checks.

## 5. Coverage floor (mind the margin)

Total coverage is a little more than the 85% floor, and the aggregate carries it.
Six packages are individually less than 85% (see
[`00-diagnostic.md`](../.claude/rules/00-diagnostic.md) Problem 3). When you add
production code, add tests **in the same package, same change**. Then make sure
that the total is at least 85%:

```bash
go tool cover -func=coverage.out | tail -1   # total must be >= 85%
```

Never lower the threshold, delete tests, or extend `EXCLUDE_PATTERN` in
`scripts/coverage_gate.sh` to pass — that requires user approval
([`40-maintenance-protocol.md`](../.claude/rules/40-maintenance-protocol.md)).

## 6. Known cross-platform & e2e follow-ups (tracked debt)

The Linux quality gate, the **Windows** job, and the Docker **e2e** job are all
green and **blocking** (issue #71 removed the `continue-on-error` of the e2e job
— see the determinism note below). Do not delete a job. Never re-add
`continue-on-error` to turn a red run green. Fix the cause.

**Windows test portability — DONE** (PRs #48/#49/#50, issue #47). Master is green
on Windows with zero skips. Recorded here because the fixes corrected two pieces
of guidance this section previously gave, and a future agent must not repeat
them:

- `cmd/cidr-compare` tests ran `./cidr-compare-test`. The root cause was the
  missing `.exe` suffix (Windows decides executability by `PATHEXT`), not the
  relative path. The fix is a shared `buildTestBinary(t)` helper that builds
  into `t.TempDir()` and returns an absolute path. Until then these tests never
  ran the binary on Windows, so they had zero coverage there.
- `pkg/preprocess` `TestOutputPath*` asserted forward-slash paths. **Do not fix
  this with `filepath.Join` in the expectation** (the earlier advice
  here): that calls the same function that production calls, which makes the
  assertion tautological. Such a test passes by construction and can never
  disagree with the code. The fix compares `filepath.ToSlash(got)` against the
  literal expectation, which keeps an independent source of truth.
- The `/tmp/resume_state.json` expectation was **only in the test**. Production
  was already portable. The current scan configuration supplies the required
  snapshot path, and the runtime uses that exact path. The earlier advice to
  use `os.TempDir()` changed working code for no reason.
- `TestEnsureFDLimit_WhenWorkersExceedLimit_ReturnsError` relies on Unix
  `RLIMIT_NOFILE`. **Do not fix this with `t.Skip` on Windows** (the earlier
  advice here): a skip deletes the contract instead of a test of it. The fix
  splits the test by build tag, so each platform asserts its own documented
  contract. Unix keeps the error assertion, and windows asserts the no-op
  contract in `fdlimit_windows.go`.
- Three timing tests (dashboard refresh, pressure poll, cancel-drain) were
  load-dependent flakes: the default timer granularity of Windows is ~15.6ms,
  far coarser than Linux. The fix waits for events with a generous timeout
  (`internal/testkit.WaitFor`) instead of a sleep with a fixed budget. **A longer
  sleep is not a fix** — it raises the stake on the same gamble.

**e2e determinism — resolved (issue #71):**
- The pressure-failure scenarios in `e2e/run_e2e.sh` (`api_5xx`, `api_timeout`,
  `api_conn_fail`) used to be timing-sensitive. `api_timeout` sometimes finished
  its tiny scan before the pressure-timeout path became fatal, so it passed
  without a test of the fatal abort. The **mock-backed** scenarios (`api_5xx`,
  `api_timeout`) are now **event-driven** and no longer race a clock. The mock
  pressure API counts every failure it serves (`GET /admin/stats`). Each
  mock-backed scenario baselines that counter and runs the scan in the
  background. It then **waits** for the mock to serve the fatal threshold of the
  scanner (3 consecutive failures) before it judges the run. It also watches the
  scan PID, so an early exit fails loudly instead of a hang.
- `api_conn_fail` is deliberately **not** mock-watched: it points the scanner at
  a closed port (`127.0.0.1:9`), so there is no mock to instrument and it passes
  an empty `watch_service`. It is not event-driven, and it does not need to be.
  It cannot pass vacuously, because the shared reason-specific assertions below
  still apply: the scenario rejects exit 0, rejects exit 124, and requires both
  the fatal pressure log line and a resumable snapshot.
- The change hardened the correctness of the assertion, not only its timing. A
  non-zero exit alone is not enough: the scenario **rejects exit 0** (the scan
  must have failed), **rejects exit 124** (the outer `timeout` hard-kill — a
  hang, not a pressure abort), requires the fatal log line `pressure api failed
  3 times` (so an unrelated error cannot masquerade as the pressure path). It
  also makes sure that the snapshot is correct with `assert-resume-snapshot
  -require-progress -require-remaining`. The snapshot path IS the `-resume`
  input of the scan, so it must show both an advanced cursor and work still
  pending. The scenario also catches a scan that hangs after the pressure path
  becomes fatal: the outer `timeout` hard-kills it at the `SCAN_HARD_LIMIT`
  ceiling with exit 124, and the scenario rejects that as a hang instead of a
  pressure abort.
- The `/24` fail workload is a **liveness margin, not the correctness
  mechanism**: its ~50s scan floor guarantees that the scan still runs when the
  mock serves the threshold. If you shrink it, the result degrades to a *loud
  red* (the PID-watch detects an early exit and fails with a diagnosis), never
  to a silent vacuous pass. Do not "optimize" it down and then read a red as
  flakiness.
- Known trade-off: `pressure_failures_served` discards `docker compose exec`
  and parse errors (it returns empty), so a broken mock appears at the wait
  deadline instead of immediately. This is deliberate — the deadline path fails
  loudly with a diagnosis — but this note records it as a documented choice, not
  an accident.
- The correctness signal is the served-failure event and the reason-specific
  assertions above, not a scan that is slow enough. So the Docker
  e2e job is now **blocking** (`continue-on-error` removed).

**Windows native validation — now automated** (issue #63). `scripts/windows_gate.ps1`
(section 2.1) covers the Part 2 gaps from `docs/windows-ci-fix/design.md`:
file-handle release after a run, the full generate-buckets→scan→resume flow,
append-reopen under Windows sharing semantics, `.exe` startup, and path shapes
that contain spaces. The blocking `windows-latest` job runs that script on every
push and PR. There the gate proves that the race detector is armed. It does not
assume it.

**Still uncovered on Windows:**
- **Interrupt-signal delivery.** Windows has no real SIGINT and does not support
  `os.Process.Signal(os.Interrupt)`, so Ctrl+C-driven
  cancellation stays honestly unverified, and the gate does not fake it. The
  gate reaches the same *resume* state through a pressure-API failure instead,
  which exercises the append-reopen path but not the signal path.
- **Windows ARM64.** The gate runs on `windows-latest` x64 only (explicitly out
  of scope for issue #63).

Each fix must follow test-first (constitution III).

## 7. Release evidence

Product releases use semver and ship `docs/release-notes/<version>.md` with
features, fixes, breaking changes, and migration guidance (constitution VII).
Keep documentation in sync with code on every change
([`.claude/rules/documents.md`](../.claude/rules/documents.md)).

### How a release is produced (issue #70)

When you push an **annotated** `v*` tag, `.github/workflows/release.yml` runs.
`package` (ubuntu) builds from the tagged source and calls
`scripts/package_release.sh`. `smoke-windows` (windows-latest) verifies the
checksums, extracts the archive and runs
`scripts/smoke_release.sh`, which runs every `.exe`. `publish` then creates the
GitHub Release from `docs/release-notes/<version>.md`. `workflow_dispatch` runs
everything except `publish`, so you can exercise the path without a publish.

Rules that hold this together — read the issue before you weaken any of them:

- **The tag must be annotated and must equal `git describe`.** The workflow
  fails otherwise. A lightweight tag is invisible to `git describe`, so the
  binaries report a version that does not match the release that ships them.
  The checkout uses `fetch-depth: 0` for the same reason.
- **`-X main.<var>` only writes to a variable in the `main` package of the
  linked binary, and it gives no message when the target does not exist.** That
  is why every command declares `version`, `buildTime` and `commit` in package
  main. It is also why `tests/release` reads the `LDFLAGS` of the Makefile,
  rebuilds with sentinels, and asserts that the sentinels appear in the binary. An
  in-process test that asserts the `dev` fallback proves nothing here, because
  `go test` links no stamps. A rename of one of those variables must fail that
  test, not pass without a message.
- **The Windows job is not optional.** The Linux packaging runner cannot run a
  Windows EXE, and the Docker e2e suite runs Linux containers
  (constitution V), so it covers Linux only. Without the `smoke-windows` job,
  nobody runs the published `.exe` assets anywhere.
- **`publish` is gated on `github.event_name == 'push'` and on the ref**,
  because a user can start a `workflow_dispatch` run on a tag ref.
- **Archives are reproducible too, and that costs three separate things.**
  `package_release.sh` normalizes the entry timestamps to the timestamp of the
  commit, sorts the entries, **exports `TZ=UTC0`**, and **chmods every staged
  file explicitly**. The last two are not optional polish. Info-ZIP stores the
  timestamp of each entry as a DOS date in *local* time (`zip -X` does not
  strip it). It also records the unix mode of each entry in the central
  directory. Without those two steps, the published checksum depends on the
  timezone and umask of the packaging runner instead of on the commit.
  `TestPackageRelease_ArchiveIsReproducible` runs the two packaging commands
  under different timezones AND different umasks for exactly this reason. Do not
  "simplify" it back to a single environment, or it passes against both defects.

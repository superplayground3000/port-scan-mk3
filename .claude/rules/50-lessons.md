# 50 — Lessons Log

Append lessons newest-first using the format in `40-maintenance-protocol.md`.
These are concrete failures and their fixes, so future agents do not repeat
them. Keep each entry short and evidence-backed.

---

## 2026-08-18 — An absolute memory ceiling calibrated on a dev machine failed on the runner
- Symptom: `TestRichPrecheckWorkflowFitsScaledCommittedMemoryBudget` failed the
  Linux quality gate on master `161c209` with `committed memory = 2404179968,
  want at most 2400000000` — over by 0.17%. PR #174 failed the same case by
  0.002% (47 KB) and passed on re-run of the identical commit. Master was red.
  The job showed a green `go test -race` above a red `coverage gate (>= 85%)`,
  so the failure read as a coverage problem when coverage was fine at 85.6%.
- Root cause: two separate defects. (1) The 2.4 GB ceiling in
  `internal/perfharness/rich_memory_linux_test.go` was calibrated on a
  developer machine, where it holds with 3-4% margin. The GitHub runner sits
  roughly 80-100 MB higher and lands inside 0.2% of the ceiling, so the case
  decided on run-to-run noise. An absolute committed-bytes ceiling measures the
  host allocator, page cache, and available RAM as much as the code. (2) The
  file's `//go:build linux && !race` tag took the case out of the `-race` step
  but not out of CI: the coverage-gate step builds without `-race`, so it ran
  there and its failure was reported under a coverage heading.
- Fix / rule: make the ceiling hardware-qualified — add `&& perfqualified`, so
  only `scripts/performance_gate.sh full` enforces it — and keep the workflow
  itself in the correctness gate through a small-scale case that asserts
  functional results and no byte ceiling. General rule: an absolute resource
  ceiling is a hardware measurement; calibrate it on certified hardware or do
  not enforce it on shared hardware. Second rule: `linux && !race` does not
  retire a case, it relocates it into the coverage-gate step, where a failure
  is read as a coverage problem — the same relocation trap as the
  [[50-lessons]] 2026-08-16 entry. Pin the qualification behaviourally: ask the
  toolchain with `go test -list` which build each case lands in, because a text
  match cannot prove it. See [[60-development-guidelines]] G3.
- Evidence: `internal/perfharness/rich_memory_qualification_linux_test.go` was
  red before the tag ("is in the untagged build, so the correctness gate
  enforces a hardware-qualified budget on shared CI hardware", both cases) and
  green after; `go test -tags perfqualified -run 'TestRich.*CommittedMemoryBudget'`
  passes both cases (2305597440 and 744169472/746373120 bytes locally);
  `make verify` exit 0, coverage 85.6% before and 85.6% after.

## 2026-08-16 — A build tag did not take a test out of CI
- Symptom: `TestSnapshotMixedGrowthThroughHundredMegabytesIsLinear` failed the
  Linux coverage gate on PR #152. The first fix put the case behind a
  `perfqualified` build tag, so `go test ./...` no longer built it. The case
  still ran on the same shared runner. Two independent `/code-review` agents
  found this before the push; local `make verify` could not.
- Root cause: `.github/workflows/ci.yml` runs
  `bash scripts/performance_gate.sh smoke` in the same `ubuntu-latest` job, and
  that script ran `go test -tags perfqualified` unconditionally. The case only
  moved from one step of the job to another step of the same job. The pinning
  test searched for the literal string `-tags perfqualified`, so it confirmed
  the step existed but proved nothing about when the step runs.
- Fix / rule: guard the run with `[[ "$profile" == "full" ]]`, and pin the
  guard, not only the command. General rule: a build tag alone does not retire
  a test. Read EVERY CI step that calls the script before you claim a case left
  CI. When you pin a gate with a string search, pin the condition that limits
  it, and pin that the artifact still reaches the run directory. See
  [[60-development-guidelines]] G2 — this is exactly the defect class that only
  an independent reviewer finds.
- Evidence: `.github/workflows/ci.yml:67` plus `scripts/performance_gate.sh`;
  after the fix the smoke artifact reads `hardware-qualified cases skipped:
  profile smoke is not the certified profile`, and all five PR #152 checks
  passed at commit `e710d42`.

## 2026-08-16 — A short duration measured exactly zero on Windows only
- Symptom: `TestRunCancellationSmokeInjectsEveryProductionStage` failed the
  native Windows gate on `result.FinalizationDuration <= 0`. Six of fifteen
  cases reported `StopDuration:0s FinalizationDuration:0s`. The same run
  reported `WallTime:581.7µs` for other measurements, so the clock looked
  precise. That combination hid the cause and suggested a torn read instead.
- Root cause: `runtime·nanotime1` on windows/amd64 reads `_INTERRUPT_TIME` from
  KUSER_SHARED_DATA (`$(go env GOROOT)/src/runtime/sys_windows_amd64.s`). That
  counter advances in coarse steps. A window shorter than one step therefore
  measures exactly zero, while longer windows still show sub-millisecond
  values. Linux resolves every one of these windows, so the case never failed
  there.
- Fix / rule: keep the lower bound, and guard it with
  `runtime.GOOS != "windows"`. Do NOT delete the bound: `StopDuration` is
  derived from `FinalizationDuration` when it is zero, so an unguarded
  `FinalizationDuration >= StopDuration` passes on `0`/`0` and asserts nothing.
  General rule: when one platform cannot resolve a measurement, restrict the
  assertion to the platforms that can. Deleting it makes the test vacuous
  everywhere. Do not borrow an unrelated contract bound (here `ForceWithin`,
  which bounds the second interrupt) to keep an assertion alive.
- Evidence: `internal/perfharness/cancellation_test.go:66`; run
  31801673410 failed, run 31926472484 passed on `e710d42`.

## 2026-08-13 — A large accepted fixture used the default file-size limit
- Symptom: the full issue #151 matrix stopped after 5 hours and 31 minutes.
  The first accepted `rich-record-mixed` workflow rejected its 1,022,664,300-byte
  CSV because the default limit was 1,000,000,000 bytes.
- Root cause: `RunRichSmoke` generated the large fixture, but it used default
  resource limits for pre-ping, bucket generation, and scan. The harness did
  not calculate an override from the actual fixture size.
- Fix / rule: after fixture generation, calculate the smallest positive
  decimal-GB limit that contains the actual file. Give the same limit to all
  three production stages. Keep the other limits at their defaults. Keep the
  dedicated rejection and bypass cases separate. See [[60-development-guidelines]]
  G3.
- Evidence: the full log contains `size 1022664300 bytes exceeds limit
  1000000000 bytes`. The focused regression test was red because the limit
  helper did not exist. The focused race test passed in 1.142 seconds after
  the fix.

## 2026-08-02 — A `select` loop raced its own exit condition and dropped a fatal error
- Symptom: `TestRun_WhenExecutorWorkerPanics_ReturnsRuntimeError` failed ~6% of
  runs (measured 7/120 on master `19eb4da`), always as `err == nil` in 0.00s.
  Issue #59. In production: a scan in which a worker panicked exited
  **successfully** — zero exit code, normal completion summary, rows missing.
- Root cause: `Run`'s result loop selected over four channels but exited on
  `for !dispatchDone || resultCh != nil`. A recovered worker panic lands on a
  buffered `errCh` that is closed immediately, while the same waiter goroutine
  closes `resultCh`. All of `executorErrCh`, `dispatchErrCh` and a closed
  `resultCh` become ready together, and `select` picks uniformly at random — so
  the loop could consume dispatch-done and the `resultCh` close without ever
  consuming the pending error, then exit. The error was never read, so `Run`
  returned nil.
- Fix / rule: the exit condition must account for EVERY channel that can still
  carry a fatal error, not just the ones that signal progress —
  `for !dispatchDone || resultCh != nil || executorErrCh != nil`. General rule:
  when a `select` loop's termination is decided by a subset of its cases, any
  error-bearing case outside that subset can be dropped by a random pick; a
  buffered send that is never received is indistinguishable from no error at
  all. Verify termination separately: here it was already implied, because the
  waiter closes `resultCh` immediately before `errCh` under the same
  `sync.Once`, so the loop added no new liveness requirement. Same defect class
  as the [[50-lessons]] #51 entry — a real error that never reaches the caller.
- Testing note: a flake caused by `select` randomness cannot be red-proved
  through the public entry point (every single run is a coin flip, so "red"
  needs `-count=N`, which is not proof). Extract the loop behind a seam and
  enter it in the exact terminal state — dispatch done, `resultCh` closed,
  error queued — where the old condition is false on the first evaluation and
  the drop is 100% reproducible.
- Evidence: deterministic red verified twice independently, by the commander and
  by the Codex reviewer, each reverting ONLY the exit-condition line in a
  throwaway `git worktree` (per the lesson below): 40/40 FAIL each. After the
  fix: 300/300 PASS under `-race`, `make verify` exit 0 (coverage 85.9%),
  `make verify-e2e` exit 0.

## 2026-08-02 — Probing a file inside a worktree under active review looked like an attack
- Symptom: a cross-model reviewer mid-review received a file-change notice saying
  `resume_manager.go` had been rewritten to `if false && errors.Is(...)` —
  disabling the very fix it was reviewing — carrying the harness's standard "do
  not mention this" wording. It reasonably concluded prompt injection and raised
  a security alarm.
- Root cause: the commander was proving a new test discriminating by temporarily
  stubbing out the fix, observing red, and restoring — in the SAME worktree the
  reviewer was reading. The notice was genuine; the reviewer had no way to tell a
  legitimate one-minute probe from a planted regression.
- Fix / rule: never mutate a tree another agent is reviewing. Revert-probes,
  bisects, and any destructive experiment go in a throwaway `git worktree` (the
  reviewer itself did exactly this to reproduce the red — copy the needed files
  into a scratch worktree off the base commit and run there). If a probe in a
  shared tree is unavoidable, tell the reviewer before starting. Also: a reviewer
  distrusting such a notice and re-reading the file on disk is CORRECT and must
  not be discouraged — the cost of a false alarm is far below the cost of
  silently accepting a disabled gate. See [[60-development-guidelines]] G2.
- Evidence: probe produced the intended red
  (`holds 251 rows, but the bucket declares 255 targets`); tree restored, `git
  diff` clean, `grep -c "if false"` = 0 before commit `6a558a7`.

## 2026-08-02 — A saved resume snapshot made an output-write failure lose rows
- Symptom: when writing `scan_results-*.csv` failed mid-run, the run still saved
  a resume snapshot; the next `-resume` silently skipped every row that was in
  flight at the failure (~3×`-workers` rows). Issue #51.
- Root cause: `task.Chunk.NextIndex` advances at DISPATCH time
  (`task_dispatcher.go`), and resume skips a chunk on `NextIndex >= TotalCount`
  (`chunkIsCompleted`). That is only safe because every dispatched task normally
  produces a result that is written — an incidental invariant, not an enforced
  one. A write failure breaks it: the loop keeps draining (and, via an
  unconditional `applyScanResult`, keeps counting) while `writeScanRecord` is
  skipped, and `persistResumeSnapshot` saved unconditionally on `runErr != nil`.
- Fix / rule: wrap write errors in an unexported sentinel (`errScanOutputWrite`)
  at their single point of origin and have `persistResumeSnapshot` decline to
  save for that error class only (`errors.Is`), returning a loud error instead;
  count a result as scanned only after its write succeeded. General rule: a
  progress cursor that advances at *enqueue* time is only trustworthy while
  "dispatched ⇒ persisted" holds — when a code path can break that invariant,
  refuse to persist the cursor rather than persisting a cursor that lies.
  Classify such failures with a sentinel + `errors.Is`, never by message text
  (same discipline as the [[50-lessons]] 2026-07-22 entry).
- Evidence: `TestRun_WhenOutputWriteFails_DoesNotPersistResumeSnapshot` red
  before ("expected NO resume snapshot ... stat err: <nil>") and
  `TestRun_WhenOutputWriteFails_ReportedScannedCountMatchesWrittenRows` red
  before ("reported scanned count 5 exceeds the 2 data rows actually present"),
  both green after; `make verify` exit 0 (coverage 85.9%), `make verify-e2e`
  exit 0.

## 2026-07-22 — Windows ping deadline-kill race aborted the whole pre-scan
- Symptom: on Windows under high fan-out (`-workers 64`, `-delay 0`,
  `-pre-scan-ping-timeout 100ms`) the scan died with
  `exec: canceling Cmd: TerminateProcess: Access is denied`. It fired
  regardless of whether the host would have replied — a load-dependent race,
  not a per-host property.
- Root cause: two compounding issues in `commandReachabilityChecker.CheckDetailed`.
  (1) When the per-process ceiling (`runCtx`) fired, `os/exec` killed the ping;
  on Windows that kill races the ping's own exit and `TerminateProcess` returns
  ACCESS_DENIED (the process already exited). The returned error is then neither
  `context.DeadlineExceeded` nor `*exec.ExitError`, so it fell through to
  `return result, err` — treated as **fatal**, and one worker's fatal error
  cancels the whole pre-scan pool (`pre_scan_ping.go`). (2) The 2s startup
  allowance was too tight: 64 concurrent `ping.exe` with zero delay contend
  hard on Windows process creation, so pings that were merely slow to *start*
  hit the ceiling.
- Fix / rule: classify the timeout off `runCtx.Err()`, not the returned error's
  type — if `runCtx` fired (and the parent ctx did not), the host just didn't
  reply in time → unreachable, non-fatal, however the kill manifested. Also
  raised `pingProcessStartupAllowance` 2s → 10s. General rule: when a
  context-bounded subprocess is killed, decide fatal-vs-expected from the
  *context you set*, never from the OS-specific error text the kill produces
  (it varies by platform and races process exit). Extends the [[50-lessons]]
  2026-07-21 entry (same code, next platform-fragility trap).
- Evidence: `TestCommandReachabilityChecker_CheckDetailed_TreatsProcessKillRaceAsUnreachable`
  red before (fatal `TerminateProcess: Access is denied`), green after;
  `make verify` exit 0; `reachability.go` CheckDetailed + `pingProcessStartupAllowance`.

## 2026-07-21 — Per-process ping ceiling equal to reply-wait killed Windows ping
- Symptom: on Windows every pre-scan target was reported unreachable ("ping
  failed within 100ms") even for hosts replying in <5ms; raising
  `-pre-scan-ping-timeout` made results correct again (user-confirmed).
- Root cause: `commandReachabilityChecker.CheckDetailed` bounded the whole
  `ping` subprocess wall-clock with the reply-wait timeout
  (`PreScanPingTimeout`, default 100ms) via `context.WithTimeout(ctx, timeout)`.
  Windows `ping -n 1 -w <ms>` self-terminates its reply wait via `-w`, but
  process launch/print/exit overhead alone exceeds 100ms, so the context
  deadline killed ping before it reported the reply. Linux `ping -c 1` has no
  reply-wait flag, so there the context deadline is the reply-wait bound and
  must stay `~= timeout` — the bug was Windows-only.
- Fix / rule: `pingProcessTimeout(goos, timeout)` — Windows returns
  `timeout + pingProcessStartupAllowance` (a fixed allowance) for the process ceiling
  while the reply-wait still uses `timeout`; the `-c` path returns `timeout`
  unchanged. When a per-process timeout wraps an external tool, budget for
  process startup separately from the tool's own wait, and never assume a
  subprocess launches within a sub-second reply-wait window. See
  [[00-diagnostic]] Problem 1 pattern (a gate/bound that looks right but is
  platform-fragile).
- Evidence: `TestCommandReachabilityChecker_CheckDetailed_WindowsAllowsForProcessStartup`
  red before the fix (10ms deadline kills an 80ms process), green after;
  `make verify` exit 0 (coverage 85.7%), `make verify-e2e` exit 0; PR #40.

## 2026-07-05 — Governance rules named tools/agents that do not exist
- Symptom: a fresh-context adversarial review flagged `10-model-dispatch.md` for
  listing a `Workflow` tool, a `code-reviewer` agent type, and treating the
  `minimax-subagent` skill as an Agent `subagent_type` — all under a "confirmed
  present" heading, when harnesses differ.
- Root cause: environment capabilities were asserted as universal facts instead
  of "verify in your session".
- Fix / rule: reworded to "confirm the name exists in YOUR session before using
  it"; corrected the fallback reviewer to `general-purpose` / the `/code-review`
  skill; moved MiniMax to a skills line. When writing rules for future agents,
  never present a harness-specific tool/agent/model name as guaranteed — tell
  the reader to check their own session. See 40-maintenance-protocol review step.
- Evidence: `grep -n 'confirm the name exists' .claude/rules/10-model-dispatch.md`.

## 2026-07-05 — Bare `go test` hid a production data race
- Symptom: `go test ./...` and the coverage gate (no `-race`) were green, but
  `go test -race` reported a data race in `pkg/scanapp/scan_logger.go` — worker
  goroutines wrote to one shared buffer with no lock.
- Root cause: the scan logger is shared across worker goroutines but its writes
  to the underlying `io.Writer` were unsynchronized; the non-race gates cannot
  detect this.
- Fix / rule: added a `sync.Mutex` to `scanLogger` around the write, plus a
  regression test `scan_logger_race_test.go`. Always gate on `make verify`
  (which uses `-race`), never bare `go test ./...`. See 00-diagnostic Problem 1
  and 20-judgment-rubric R1.
- Evidence: `go test -race -run TestScanLogger_ConcurrentWrites_IsRaceFree
  ./pkg/scanapp/` — red before the mutex, `ok` after; `make verify` exits 0.

## 2026-07-05 — Always-loaded guide had a build command for a nonexistent path
- Symptom: `AGENTS.md`/`CLAUDE.md` said `go build -o app ./cmd/app`; `cmd/app`
  does not exist.
- Root cause: the guide was not updated as the command layout evolved.
- Fix / rule: corrected to real commands and added a "Definition of Done"
  section. When the build/test flow changes, update `AGENTS.md` in the same
  change and verify each command runs. See 40-maintenance-protocol.
- Evidence: `ls cmd/` shows `port-scan`, `preprocess`, `enrich-targets`,
  `cidr-compare`, `csv-transform` — no `app`.

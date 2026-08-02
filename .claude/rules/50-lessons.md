# 50 — Lessons Log

Append lessons newest-first using the format in `40-maintenance-protocol.md`.
These are concrete failures and their fixes, so future agents do not repeat
them. Keep each entry short and evidence-backed.

---

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

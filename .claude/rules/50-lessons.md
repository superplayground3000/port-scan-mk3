# 50 — Lessons Log

Append lessons newest-first using the format in `40-maintenance-protocol.md`.
These are concrete failures and their fixes, so future agents do not repeat
them. Keep each entry short and evidence-backed.

---

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
  `timeout + pingProcessStartupAllowance` (fixed 2s) for the process ceiling
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

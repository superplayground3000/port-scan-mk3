# Issue #157 — `-quiet` hid error-level logs

Branch `fix/157-quiet-hides-errors`, cut from master `161c209`.
All commands below were run with `GOTOOLCHAIN=go1.24.4`.

## 1. Mechanism

`scanLogger.enabled` combined two independent decisions with `&&`:

```go
// pkg/scanapp/scan_logger.go:121 at 161c209
func (l *scanLogger) enabled(level int, msg string) bool {
	return l != nil && level >= l.level && (!l.quiet || isPressureLog(msg))
}
```

The quiet clause sat beside the level test, not under it. So with `-quiet` the
logger dropped a message at **any** level unless its text happened to contain
`pressure` or `[API]` (`isPressureLog`, `scan_logger.go:124` at `161c209`).

Two error-level, operator-facing messages disappeared:

- `pkg/scanapp/executor.go:110` — `logger.errorf("%v", err)` in `reportFatal`,
  which carries the recovered worker panic raised at `executor.go:151`.
- `pkg/scanapp/executor.go:133` — the once-per-run local resource failure line,
  which states the affected rows are `NOT confirmed closed`.

A `-quiet` scan that exhausted the host's local resources therefore wrote
`error(local)` rows to the CSV and printed nothing to standard error, so a
reader could take those rows for closed ports.

There are **two** separate `quiet` concepts in `pkg/scanapp`. Only the first was
the defect and only the first was removed:

1. **Logger quiet** — `scanLogger.quiet`, set by `newLoggerWithQuiet`, read only
   inside `enabled`. Removed.
2. **Progress quiet** — `outputCommitterConfig.quiet` /
   `scanResultLoopDeps.quiet`, fed from `cfg.Quiet` at `scan_runtime.go:307` and
   consumed at `result_aggregator.go:100` and `:123`. **Unchanged.** It is the
   behavior the fix must preserve, and case 3 below pins it.

## 2. The change

| File | Change |
|---|---|
| `pkg/scanapp/scan_logger.go:118` | `enabled` now tests the level only. A doc comment records that `-log-level` is the sole owner of log verbosity. |
| `pkg/scanapp/scan_logger.go` | Deleted `isPressureLog` (no caller left), the `quiet` field (no reader left), and `newLoggerWithQuiet` (it only forwarded to `newLogger`). |
| `pkg/scanapp/scan_logger.go:109` | `enabledEvent()` and `enabled(level int)` dropped the now-unused `msg` parameter. |
| `pkg/scanapp/scan.go:151` | Calls `newLogger(values.LogLevel, values.Format == "json", stderr)`. |
| `pkg/scanapp/executor.go:193`, `pkg/scanapp/output_committer.go:176` | Adapted to the `enabledEvent()` signature. No behavior change. |
| `pkg/config/parser_helpers.go:29` | Flag help is now `suppress progress and per-result console output; use -log-level for log verbosity`. |

## 3. Red proofs

The new cases in `pkg/scanapp/scan_quiet_logging_test.go` drive the real
production path: `Run` with `config.ScanValues.Quiet = true`, capturing the
process stderr and stdout buffers. That seam survives the removal of
`newLoggerWithQuiet`, so the same test text is red before and green after.

RED was observed in a throwaway worktree at the base commit
(`git worktree add /tmp/ps157-base 161c209`), so the branch tree was never
mutated for a probe.

Command:

```
cd /tmp/ps157-base && GOTOOLCHAIN=go1.24.4 go test -race -run 'TestRun_WhenQuiet' ./pkg/scanapp/
```

Verbatim output:

```
--- FAIL: TestRun_WhenQuietAndWorkerPanics_StillLogsTheFatalErrorToStderr (0.00s)
    scan_quiet_logging_test.go:68: quiet run hid the fatal worker panic from stderr; stderr = ""
--- FAIL: TestRun_WhenQuietAndLocalResourceFailure_StillWarnsRowsAreNotConfirmedClosed (0.00s)
    scan_quiet_logging_test.go:91: quiet run hid the local resource failure warning; stderr = ""
--- FAIL: TestRun_WhenQuiet_LogLevelAloneDecidesWhetherInfoLinesAppear (0.00s)
    scan_quiet_logging_test.go:147: -quiet at -log-level info must keep the info-level scan_result lines; stderr = ""
FAIL
FAIL	github.com/xuxiping/port-scan-mk3/pkg/scanapp	0.017s
FAIL
```

The very first slice was proved red on its own before any production edit, on
the branch tree, before the fix existed:

```
GOTOOLCHAIN=go1.24.4 go test -race -run 'TestRun_WhenQuietAndWorkerPanics_StillLogsTheFatalErrorToStderr' ./pkg/scanapp/
--- FAIL: TestRun_WhenQuietAndWorkerPanics_StillLogsTheFatalErrorToStderr (0.00s)
    scan_quiet_logging_test.go:67: quiet run hid the fatal worker panic from stderr; stderr = ""
FAIL
FAIL	github.com/xuxiping/port-scan-mk3/pkg/scanapp	0.009s
FAIL
```

### `TestRun_WhenQuiet_StillSuppressesThePeriodicProgressLine` was NEVER red

Stated plainly: this case is a regression pin, not a red-first proof. It passes
at `161c209` and it passes after the change. It exists to catch a careless
grep-and-delete of concept 2 above. Measured on the base commit:

```
cd /tmp/ps157-base && GOTOOLCHAIN=go1.24.4 go test -race -v -run 'TestRun_WhenQuiet_StillSuppressesThePeriodicProgressLine' ./pkg/scanapp/
=== RUN   TestRun_WhenQuiet_StillSuppressesThePeriodicProgressLine
--- PASS: TestRun_WhenQuiet_StillSuppressesThePeriodicProgressLine (0.00s)
PASS
ok  	github.com/xuxiping/port-scan-mk3/pkg/scanapp	1.014s
```

It is not vacuous: it runs the same scan twice and asserts `progress cidr=`
appears on stdout **without** `-quiet` and is absent **with** `-quiet`.

### `pkg/config` flag help

```
GOTOOLCHAIN=go1.24.4 go test -race -run 'TestRegisterCommonFlags_QuietUsage' ./pkg/config/
--- FAIL: TestRegisterCommonFlags_QuietUsageDescribesProgressOnly (0.00s)
    quiet_flag_usage_test.go:27: -quiet usage still promises pressure API logs, which the logger no longer special-cases; usage = "suppress console logs, keep pressure API logs"
FAIL
FAIL	github.com/xuxiping/port-scan-mk3/pkg/config	0.007s
FAIL
```

Green after the help-string change: `ok github.com/xuxiping/port-scan-mk3/pkg/config 1.008s`.

## 4. Green

```
GOTOOLCHAIN=go1.24.4 go test -race -v -run 'TestRun_WhenQuiet' ./pkg/scanapp/
=== RUN   TestRun_WhenQuietAndWorkerPanics_StillLogsTheFatalErrorToStderr
--- PASS: TestRun_WhenQuietAndWorkerPanics_StillLogsTheFatalErrorToStderr (0.00s)
=== RUN   TestRun_WhenQuietAndLocalResourceFailure_StillWarnsRowsAreNotConfirmedClosed
--- PASS: TestRun_WhenQuietAndLocalResourceFailure_StillWarnsRowsAreNotConfirmedClosed (0.00s)
=== RUN   TestRun_WhenQuiet_StillSuppressesThePeriodicProgressLine
--- PASS: TestRun_WhenQuiet_StillSuppressesThePeriodicProgressLine (0.00s)
=== RUN   TestRun_WhenQuiet_LogLevelAloneDecidesWhetherInfoLinesAppear
--- PASS: TestRun_WhenQuiet_LogLevelAloneDecidesWhetherInfoLinesAppear (0.00s)
PASS
ok  	github.com/xuxiping/port-scan-mk3/pkg/scanapp	1.021s
```

## 5. Discrimination probe

A throwaway worktree (`git worktree add /tmp/ps157-probe 161c209`) received the
final test files while keeping the four production files at `161c209`. The
branch tree was not touched.

```
cd /tmp/ps157-probe && GOTOOLCHAIN=go1.24.4 go test -race -run 'TestRun_WhenQuiet|TestScanResultEventBelowLogLevel' ./pkg/scanapp/
--- FAIL: TestRun_WhenQuietAndWorkerPanics_StillLogsTheFatalErrorToStderr (0.00s)
    scan_quiet_logging_test.go:68: quiet run hid the fatal worker panic from stderr; stderr = ""
--- FAIL: TestRun_WhenQuietAndLocalResourceFailure_StillWarnsRowsAreNotConfirmedClosed (0.00s)
    scan_quiet_logging_test.go:91: quiet run hid the local resource failure warning; stderr = ""
--- FAIL: TestRun_WhenQuiet_LogLevelAloneDecidesWhetherInfoLinesAppear (0.00s)
    scan_quiet_logging_test.go:147: -quiet at -log-level info must keep the info-level scan_result lines; stderr = ""
FAIL
FAIL	github.com/xuxiping/port-scan-mk3/pkg/scanapp	0.030s
FAIL
```

Honest scope note: the probe reverts the whole production change for
`scan_logger.go`, `scan.go`, `executor.go`, and `output_committer.go`, not the
`enabled` line alone. Reverting only that line is not possible in isolation,
because removing the quiet clause also removed the field, the constructor, and
the `msg` parameter the other three files pass. The three call-site edits carry
no behavior, so the reverted `enabled` filter is the only behavioral difference
in the probe.

`TestScanResultEventBelowLogLevelDoesNotAllocate` passed in the probe too, which
is correct: it never depended on quiet.

## 6. The replaced allocation test

`pkg/scanapp/scan_logger_quiet_alloc_test.go` was removed and replaced by
`pkg/scanapp/scan_logger_alloc_test.go`.

- `TestQuietLoggerStillWritesPressureEvents` asserted that a quiet logger still
  writes a `pressure [API]` line. That is exactly the behavior the binding
  decision removes, so the case was deleted, not repaired.
- `TestQuietScanResultEventDoesNotAllocate` built the logger at level `error`
  and emitted an info-level event, so `1 >= 2` was already false and the level
  test alone short-circuited `enabled` before the quiet clause could matter. The
  name was wrong about the cause. It is now
  `TestScanResultEventBelowLogLevelDoesNotAllocate`, and it gained a control
  case that runs the same call at `info` level and asserts the event **is**
  written, so the zero-allocation claim describes a suppression and not a dead
  code path.
- `BenchmarkQuietScanResultEvent` became `BenchmarkScanResultEventBelowLogLevel`
  with an unchanged body, for the same reason.

## 7. Gates

`GOTOOLCHAIN=go1.24.4 make verify`:

```
coverage gate passed: 85.6%

=== RESULT ===
All selected quality gates passed.
```

`GOTOOLCHAIN=go1.24.4 go tool cover -func=coverage.out | tail -1`:

```
total:							(statements)		85.6%
```

`pkg/scanapp` itself is at 87.7% and `pkg/config` at 95.0%, so deleting
`isPressureLog` and one test did not cost the thin margin.

### e2e

`make verify-e2e` was **not** strictly required — the brief judged this change
to leave the scan pipeline, the writers, and pressure control alone. Checking
that claim against the actual diff, `pkg/scanapp/executor.go` and
`pkg/scanapp/output_committer.go` both changed, so it was run anyway. Docker was
available.

```
GOTOOLCHAIN=go1.24.4 make verify-e2e
...
--- PASS: TestScanPipeline_PausesOnPressureAndResumes (0.00s)
--- PASS: TestScanPipeline_WithoutPressureAPICompletesWithoutPause (0.00s)
--- PASS: TestScanPipeline_DefaultScenarioCompletesWithoutLoss (0.00s)
PASS
e2e report generated at /media/hp/secondary/projects/port-scan-mk3/e2e/out

=== RESULT ===
All selected quality gates passed.
EXIT=0
```

Both call sites only dropped the argument of `enabledEvent`, which the fix had
already made unused, so no pipeline or writer behavior changed.

## 8. Benchmark

Same benchmark body, renamed by this change. "Before" is
`BenchmarkQuietScanResultEvent` at `161c209` in `/tmp/ps157-base`; "after" is
`BenchmarkScanResultEventBelowLogLevel` on the branch. Both:
`go test -bench=... -benchmem -count=6 -run='^$' ./pkg/scanapp/`.

| Run | Before (ns/op) | After (ns/op) |
|---|---|---|
| 1 | 3.107 | 3.106 |
| 2 | 3.120 | 3.067 |
| 3 | 3.270 | 3.079 |
| 4 | 3.081 | 3.044 |
| 5 | 3.287 | 3.045 |
| 6 | 3.097 | 3.042 |

Median 3.114 -> 3.056 ns/op, about 1.9% faster. `0 B/op` and `0 allocs/op` in
every one of the twelve runs. Removing a boolean AND from the per-log-line path
is not a regression. `benchstat` is not installed on this machine, so the
comparison is by eye across six runs, as `60-development-guidelines.md` G3
allows.

## 9. Documents

Changed, because each stated the old meaning:

- `cmd/port-scan/README.md:111` — flag table row.
- `cmd/port-scan/README.md:377` — the "Suppress console logs (keep pressure API
  logs)" section. It is now "Suppress the progress output", it says the logs are
  not filtered, and it adds the `-quiet -log-level error` command that restores
  a silent run.
- `docs/apps/port-scan/SPEC.md:159` — flag table row.
- `docs/cli/flags.md` — a new bullet in "Interaction Rules and Behavior Notes"
  states the `-quiet` / `-log-level` relationship and the fully silent pair.
- `example/README.md:121` — "to stop the per-probe logs, add `-quiet=true`" was
  wrong after the change. It now says `-log-level error`, and notes that
  `-quiet=true` stops the progress output only.

Left alone on purpose:

- `README.md:421`, `docs/cli/flags.md:57,85,125` — "Shared observability flags".
  They name the flag without describing its effect.
- `docs/cli/flags.md:36` — lists the shared flag names only.
- `docs/cli/flags.md:149` — lists the values `validate` accepts and discards. It
  makes no claim about what `-quiet` does.
- `cmd/port-scan/command_handlers.go:132,135,138` — usage lines print `[-quiet]`
  only, with no description.
- `docs/superpowers/`, `docs/plans/`, `labs/`, `.agent-evidence/issue-156.md` —
  dated historical records, out of scope by instruction.
- `docs/release-notes/` — **deliberately not touched.** The maintainer deferred
  the versioned release note to issue #173. Constitution II still requires a
  compatibility note for this CLI contract change; it is owed on #173, not here.

## 10. Not verified

- **Windows.** Only Linux gates ran on this machine. The new tests use
  `t.TempDir()`, `filepath.Join`, and `syscall.EADDRNOTAVAIL` /
  `syscall.ECONNREFUSED`, which `pkg/scanner/dial_error.go` classifies through
  its portable errno table on every platform, so they should hold. CI is the
  real check.
- **`benchstat`.** Not installed; see section 8.
- **The release note.** Owed on issue #173, see section 9.
- **golangci-lint.** Not installed here, so `make lint` falls back to `go vet`,
  which `make verify` already ran.

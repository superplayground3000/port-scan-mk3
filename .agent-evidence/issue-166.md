# Issue 166 evidence

Stops `internal/perfharness` tests from asserting a positive duration that the
Windows clock cannot guarantee.

## The mechanism

The Go monotonic clock on Windows reads `_INTERRUPT_TIME` from
`KUSER_SHARED_DATA`. That counter advances in coarse irregular steps, so an
operation that finishes inside one step measures exactly `0s`. The CI evidence in
the issue shows run 1 at `0s` while five runs in the same execution measured
357µs to 1.5492ms. That pattern is the signature of the coarse counter, not of a
stopped clock.

## Scope: ten sites, not one

The issue named `matrix_test.go:25`. A search of the package found **ten**
assertion sites. One was already guarded by an inline copy of the rule. The
tenth, `internal/perfharness/cmd/perf-harness/main_test.go`, is in a different
package and was not in the original triage list. Fixing it was a scope extension,
approved by the maintainer, because it is the same defect class inside the same
file boundary.

Production makes the derived rates part of the same defect:

```go
// internal/perfharness/measure.go:107, and the same shape at workflow.go:488
if elapsed > 0 {
	observation.ThroughputPerSecond = float64(units) / elapsed.Seconds()
	observation.MegabytesPerSecond = float64(outputBytes) / 1_000_000 / elapsed.Seconds()
}
```

When the clock reads zero, the rates stay zero, so every `ThroughputPerSecond <= 0`
assertion fails for the identical reason.

## Two treatments, chosen deliberately

**Wall time uses the platform rule.** A zero really is unobservable on Windows,
so a platform rule is the only honest option.

**Rates do not use the platform rule.** They are tied to the wall time instead,
mirroring the production guard above:

```go
if result.SteadyMedian.WallTime > 0 && result.SteadyMedian.MegabytesPerSecond <= 0 {
```

This catches a defect a platform check would hide: a run whose wall time is
measurable but whose rate wrongly came out zero. Probe 2 below measures that
payoff rather than asserting it.

| Site (post-change) | Treatment |
| --- | --- |
| `cancellation_test.go:61` | platform rule, replacing the inline copy |
| `cancellation_test.go:70` | platform rule |
| `cancellation_test.go:143` | platform rule |
| `matrix_test.go:28` | platform rule |
| `workflow_test.go:31` | platform rule |
| `workflow_test.go:196` | platform rule |
| `measure_test.go:28` | platform rule, wall-time half |
| `measure_test.go:34` | tied to wall time |
| `workflow_test.go:117` | tied to wall time |
| `output_workflow_test.go:33` | tied to wall time |
| `cmd/perf-harness/main_test.go:496` | tied to wall time |

Where a composite condition mixed a duration term with a non-duration term, the
non-duration term was split into its own unconditional check rather than
inheriting a guard. Run counts, byte counts, correctness flags, verdict, and
manifest checks therefore keep exactly their previous strength. That is
acceptance criterion 3, and an independent reviewer diffed every split line by
line to confirm nothing was dropped.

### Sites deliberately left unchanged

- `report_test.go`, `workflow_observation_test.go`,
  `stage_workflow_internal_test.go` — exact equality against fixed synthetic
  values, not a measured clock. Deterministic on every platform.
- `snapshot_growth_linux_test.go:35` — a `t.Logf` plus a verdict check, and
  Linux-only.
- `cancellation_test.go:57` — an upper bound, which a coarse clock cannot break.
- `cancellation_test.go:60`, `FinalizationDuration < StopDuration` — a **relative**
  comparison. Both values are `time.Since(trigger.at)` from the same start, and
  finalization ends later (`cancellation_workflow.go:176,296`). The clock is
  monotonic, so the ordering holds even with coarse steps. This keeps full
  strength on Windows and must not be relaxed.

## The seam

The rule is a pure function that takes `goos`, following the established
repository pattern at `pkg/scanapp/reachability.go:121`
(`func pingProcessTimeout(goos string, timeout time.Duration)`), including the
convention that an empty string means `runtime.GOOS`.

The parameter is the point. It makes a Windows-only rule provable from Linux,
deterministically, instead of waiting for CI to catch an intermittent flake.

The helper lives in `internal/perfharness/clock_resolution_test.go`, in
`package perfharness_test`, together with its test.

Why test-only rather than a production file:

- Every wall-time consumer is in `perfharness_test`, so one test-only file reaches
  all of them without exporting anything.
- A production file would force an exported `MeasuresShortDurations`, which would
  invite production code to branch on clock coarseness. Production must not do
  that. It already handles the coarse clock correctly with `if elapsed > 0`.
- The tenth site, in `package main`, needs no access, because its assertion is a
  rate and takes the wall-time treatment.

Coverage gate: `scripts/coverage_gate.sh` uses
`EXCLUDE_PATTERN="e2e|internal/testkit"`, so `internal/perfharness` **is**
measured. A `_test.go` file carries no coverage statements, so this helper cannot
move the gate either way. A production helper would have added statements to the
denominator; a marginal coverage gain is not a reason to grow the public API.

Verified that no second copy of the rule exists:

```text
grep -rn 'runtime.GOOS' internal/perfharness/
```

Only `clock_resolution_test.go` carries it. The `runtime.GOOS` checks at
`measure_test.go:40,43` are the pre-existing per-platform memory-metric
availability checks, `LinuxPeakRSSBytes` against `WindowsWorkingSetBytes`. That
is a different concern and is unchanged.

## Red proof for the helper

Captured live, before the real implementation, against a deliberately wrong
`return true` stub:

```text
--- FAIL: TestMeasuresShortDurationsLosesTheLowerBoundOnlyOnWindows (0.00s)
    clock_resolution_test.go:26: measuresShortDurations("windows") = true, want false
FAIL
FAIL	github.com/xuxiping/port-scan-mk3/internal/perfharness	0.002s
```

Green after the real body:

```text
--- PASS: TestMeasuresShortDurationsLosesTheLowerBoundOnlyOnWindows (0.00s)
ok  	github.com/xuxiping/port-scan-mk3/internal/perfharness	0.002s
```

An earlier run produced only `undefined: measuresShortDurations`. A missing
symbol proves the test calls something absent, not that the test discriminates,
so the stub run above is the proof that counts.

Cases covered: `"windows"` false, `"linux"` true, `"darwin"` true, `"freebsd"`
true, and `""` equal to `runtime.GOOS != "windows"`.

## Vacuousness probes

Relaxing an assertion is the change that can silently become a no-op, so the
relaxed state was attacked directly. Every probe ran in a throwaway
`git worktree` under `/tmp`, never in the branch tree, and each was reverted
after its run.

**Probe 1 — a zero wall time on the NON-Windows path.** `measure.go` was made to
report `WallTime: 0`. Three tests failed on Linux:

```text
--- FAIL: TestMeasureRecordsPortableGoMetrics
    measure_test.go:29: time metrics = {... WallTime:0s ThroughputPerSecond:0 ...}
--- FAIL: TestRunProductionSmokeUsesTheProductionWorkflowWithFakeProbes
    workflow_test.go:32: workflow timings are not separate: {...}
--- FAIL: TestRunFixtureCaseKeepsOneFixtureAndSummarizesSixRuns
    matrix_test.go:29: run summary = {...}
```

The platform rule does not blind Linux.

**Probe 2 — a positive wall time with a zero rate.** The rate computation at
`measure.go:107` was disabled while the wall time stayed real. All four
wall-time-tied sites fired:

```text
--- FAIL: TestRunOutputCaseMeasuresBothWritersAndKeepsOnlyFirstOutputs
    output_workflow_test.go:34: ... WallTime:210.996µs ThroughputPerSecond:0 MegabytesPerSecond:0 ...
--- FAIL: TestMeasureRecordsPortableGoMetrics
    measure_test.go:35: ... WallTime:724.079µs ThroughputPerSecond:0 ...
--- FAIL: TestProductionCandidateAndBucketCasesUseExactDeclaredCounts
    workflow_test.go:118
--- FAIL: TestRunCommandWritesSmokeReports
    main_test.go:497
```

The wall times are non-zero. A platform check would have hidden this defect on
Windows. This is the measured justification for the second treatment.

**Probe 3 — a wrong run count.** `result.Runs = result.Runs[:5]` in `matrix.go`
failed `matrix_test.go:25`.

**Probe 4 — a missing manifest.** `result.Manifest = nil` in `matrix.go` failed
`matrix_test.go:32`, `retained manifest is nil`.

**Probe A — simulate Windows everywhere.** The helper was forced to
`return false`, so every platform takes the Windows path. The only failure in
either package was the helper's own test, which the probe had deliberately
broken. Everything else passed.

**Probe B — simulate Windows and inject a real defect.** With the helper still
forced to `return false`, the run loops at `matrix.go:24` and `workflow.go:240`
were cut from 6 to 5. Six tests failed, including
`TestRunFixtureCaseKeepsOneFixtureAndSummarizesSixRuns` and
`TestRunCommandWritesSmokeReports`.

Probes A and B say the relaxed assertions still catch a wrong run count while the
Windows path is active. They are a simulation of the rule, not a measurement of a
real coarse clock.

## Quality gate

```text
GOTOOLCHAIN=go1.24.4 make verify

coverage gate passed: 85.6%

=== RESULT ===
All selected quality gates passed.
```

Exit status 0.

## Validation triggers

- **Unit:** covered above.
- **e2e:** not triggered. The committed diff changes seven `_test.go` files and
  this evidence file. No production file changed.
- **Performance:** not triggered, for the same reason. The harness measures
  performance, but this change does not alter how it measures. Issue #166 puts
  that out of scope.

## On red-first

G1 requires a red test before a **production behavior change**. This change
alters no production file, so there is no production behavior to red-prove. The
new helper does have a real red, recorded above. For the relaxed assertions the
meaningful question is vacuousness, and the six probes answer it. An independent
reviewer agreed that this is not an evasion.

## Known limits, stated rather than glossed

**The rate form is not literally equivalent to the previous code on Linux.**
Previously `rate <= 0` would also fire if the wall time were wrongly zero. The
new `WallTime > 0 && rate <= 0` form is vacuous in that one corner. An
independent reviewer traced the consequence: all three case paths go through
`SummarizeCase` (`matrix.go:56`, `workflow.go:272`, `output_workflow.go:60`), and
both `Measure` (`measure_test.go:28`) and the `SummarizeCase` output
(`matrix_test.go:28`) still pin the wall time above zero on Linux, so a
wall-time-zeroing defect is caught upstream. The residual gap is a defect that
zeroes the wall time only between `Measure` and `SummarizeCase` on one specific
case path. Narrow, but real, and recorded here rather than left to be found
later.

## What is NOT proven, and cannot be from this machine

**Acceptance criterion 5 is not met.** It requires the native Windows gate to
pass, and says not to claim a fix from a single green run, because the failure is
intermittent.

Every probe above ran on Linux. The Windows branch was exercised only through an
explicit `"windows"` argument or a forced helper, never against a real coarse
clock. Probes A and B are a strong argument. They are not a measurement.

Until a maintainer runs the native Windows gate several times, treat this issue as
fixed on the Linux evidence and unproven on Windows. That step belongs with
[#99](https://github.com/superplayground3000/port-scan-mk3/issues/99).

## Independent review

Cross-provider review was not available: Codex hit a provider usage limit, with
credits returning 2026-08-20. Rule G2 ranks reviewers as different provider, then
different Claude model, then any fresh-context agent, so this used the second
rank: a different Claude model with no knowledge of the implementing
conversation.

**State this plainly when citing this file: the change has one same-provider,
different-model review round. It has no cross-provider round.**

Verdict: **APPROVE**, no issues requiring changes. The reviewer confirmed all six
claims by reproducing them, including its own re-runs of Probes A and B, and
added three defect injections of its own on the simulated Windows path: disabling
the rate computation (caught by four tests), nulling the manifest (caught), and
blanking `ScanDigest` at `workflow.go:718` (caught at `workflow_test.go:28`). No
defect class it tried slipped through.

It also confirmed independently that no forbidden shortcut was used: no
`time.Sleep`, no retry, and no enlarged fixture anywhere in the diff.

It found two sentences in an earlier version of this file that claimed more than
the commands proved. Both are corrected above: the claim that "the suite is green
on Windows" now says that Probes A and B are a simulation of the rule, and the
claim of "full assertion strength on Windows as well as Linux" is replaced by the
specific statement of what the rate form catches, with its non-equivalence
recorded under Known limits.

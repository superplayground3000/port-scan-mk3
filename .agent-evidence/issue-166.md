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
assertion sites. One was already guarded. The tenth,
`internal/perfharness/cmd/perf-harness/main_test.go`, sits in a different package
and was not in the original triage list.

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

This is stronger than a platform check. It keeps full assertion strength on
Windows as well as Linux, and it still catches the real defect where the wall time
is measurable but the rate wrongly came out zero.

| Site | Treatment |
| --- | --- |
| `cancellation_test.go:61` | platform rule (replaced the inline copy) |
| `cancellation_test.go:70` | platform rule |
| `cancellation_test.go:143` | platform rule |
| `matrix_test.go:28` | platform rule |
| `workflow_test.go:31` | platform rule |
| `workflow_test.go:196` | platform rule |
| `measure_test.go:28` | platform rule |
| `measure_test.go:34` | tied to wall time |
| `workflow_test.go:117` | tied to wall time |
| `output_workflow_test.go:33` | tied to wall time |
| `cmd/perf-harness/main_test.go:496` | tied to wall time |

The composite conditions were split, so the run count, byte counts, correctness
flags, verdict, and manifest checks stay unconditional and exactly as strict as
before. That is acceptance criterion 3.

`cmd/perf-harness/main_test.go` is `package main` and cannot see the helper. It
needed no access, because its only affected assertion is a derived rate. So the
platform rule still exists in exactly one place, which is acceptance criterion 2.

## The seam

The rule is a pure function that takes `goos`, following the established
repository pattern at `pkg/scanapp/reachability.go:121`
(`func pingProcessTimeout(goos string, timeout time.Duration)`), including the
convention that an empty string means `runtime.GOOS`.

The parameter is the point. It makes a Windows-only rule provable from Linux,
deterministically, instead of waiting for CI to catch an intermittent flake.

The helper lives in `internal/perfharness/clock_resolution_test.go`, in
`package perfharness_test`. It is test-only, so it stays out of the production
coverage denominator.

Verified that no second copy of the rule exists:

```text
grep -rn 'GOOS != "windows"\|GOOS == "windows"' internal/perfharness/
(none)
```

## Discrimination proof for the helper

Forcing the helper to `return false` makes its own test fail on the exact
distinction it exists to draw:

```text
--- FAIL: TestMeasuresShortDurationsLosesTheLowerBoundOnlyOnWindows (0.00s)
    clock_resolution_test.go:38: measuresShortDurations("linux") = false, want true
    clock_resolution_test.go:38: measuresShortDurations("darwin") = false, want true
    clock_resolution_test.go:38: measuresShortDurations("freebsd") = false, want true
    clock_resolution_test.go:38: measuresShortDurations("") = false, want true
```

## Vacuousness probes: the tests still bite on Windows

Relaxing an assertion is exactly the change that can silently become a no-op, so
the relaxed state was tested directly. Both probes ran in a throwaway
`git worktree` under `/tmp`, never in the branch tree.

**Probe A — simulate Windows.** The helper was forced to `return false`, so every
platform takes the Windows path. The only failure in the whole package was the
helper's own test, which the probe had deliberately broken. Every other test
passed.

Conclusion: the suite is green on Windows.

**Probe B — simulate Windows and inject a real defect.** With the helper still
forced to `return false`, the run loops at `matrix.go:24` and `workflow.go:240`
were changed from 6 runs to 5.

```text
--- FAIL: TestRichFixtureLoadFitsScaledCommittedMemoryBudget
--- FAIL: TestRunFixtureCaseKeepsOneFixtureAndSummarizesSixRuns
--- FAIL: TestRunFixtureCaseUsesProductionCIDRLoader
--- FAIL: TestRunRichOversizeCaseRejectsDefaultAndCompletesWithPositiveOverride
--- FAIL: TestRunFixtureCaseUsesProductionRichAndPortLoaders
--- FAIL: TestRunCommandWritesSmokeReports
```

Conclusion: on Windows the tests still catch a wrong run count, including the test
this issue reports and the one in `cmd/perf-harness`. The relaxation did not make
them vacuous.

The probe worktree was removed. The branch tree is clean and contains no probe
text.

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
- **e2e:** not triggered. This changes test assertions in a test-support package.
  No production file changed.
- **Performance:** not triggered. No production file changed, so no hot path
  moved. The harness measures performance but this change does not alter how it
  measures. Issue #166 puts that explicitly out of scope.

## On red-first

`.claude/rules/60-development-guidelines.md` G1 requires a red test before a
**production behavior change**. This change alters no production file. Every
edit is a test assertion or the new test-only helper.

The evidence that stands in for it is stated plainly rather than dressed up as a
red-green cycle:

- the helper has a real discrimination proof, recorded above
- the relaxed assertions have Probe A and Probe B, which is the harder question

## What is NOT proven here, and cannot be

**Acceptance criterion 5 is not met by this branch.** It requires the native
Windows gate to pass, and says not to claim a fix from a single green run because
the failure is intermittent.

No work on this machine can produce that evidence. The failure needs a native
Windows host, and it appears at random, so it needs repeated runs. Probe A shows
the suite passes when the Windows rule is active, which is a strong argument, but
it is a simulation of the rule and not a measurement of the real clock.

Until a maintainer runs the native Windows gate several times, treat this issue as
fixed on the Linux evidence and unproven on Windows. That step belongs with
[#99](https://github.com/superplayground3000/port-scan-mk3/issues/99).

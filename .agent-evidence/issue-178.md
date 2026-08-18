# Issue #178 — the regression benchmark's ns/op rule could not fire

## 1. The defect

`RunRegressionBenchmark` timed ONE snapshot write per run. On windows/amd64 the
Go monotonic clock reads `_INTERRUPT_TIME`, which advances in about 15.6 ms
steps, so a shorter operation measures exactly `0s`.

That zero reached `AfterNSPerOp`. `ratioFloat` (`evaluate.go`) guards a zero
denominator but not a zero numerator, so `ratioFloat(0, positive)` is `0`, and
`0 > 1.1` is false. An unmeasurable benchmark reported "no regression".

The production contract is exposed too, not only the 4 KB test fixture:
`contract.go` uses `TargetBytes: 1_000_000` against a 7.76 ms baseline, which is
also below one clock step.

## 2. Where it fired

| Run | Commit | Result |
|---|---|---|
| PR #177 | branch | FAIL |
| [32118376564](https://github.com/superplayground3000/port-scan-mk3/actions/runs/32118376564) | master `5e2503c` | FAIL |

In the master run, four of six benchmark runs read `WallTime:0s`, and both
`ColdStart` and `SteadyMedian` collapsed to `0s`. The verdict carried only
`regression-bytes-per-op`. The absent `regression-ns-per-op` is what the
assertion at `evaluate_test.go:67` failed on.

## 3. The fix

Three commits.

- `db394dd` — batch the writes and divide by the iteration count. Add a
  `regression-unmeasured` verdict rule so a zero after-value fails instead of
  passing.
- `e0cd702` — four fixes from review round 1 (below).
- `10961cc` — comment corrections from review round 2 (below).

The batch window comes from the clock granularity the platform actually
reports, divided by a quantization budget, with a 20 ms floor. The iteration
count grows adaptively, so a fine clock keeps fast tests.

## 4. Red-first evidence (constitution III)

The bug CANNOT reproduce on this Linux machine, whose clock resolves
nanoseconds. A test that passes here proves nothing about Windows. So the
coarse-clock model in `regression_internal_test.go` asserts `read(1) == 0`
FIRST, proving the model hides one operation, before testing that batching
recovers it.

Reviewer 1 proved both production changes discriminate, by reverting one at a
time in a throwaway worktree:

- reverted the `/ iterations` division ->
  `regression_internal_test.go:98: AfterNSPerOp = 2e+09, want the batch wall time divided by the iteration count`
- removed the unmeasured guard ->
  `evaluate_test.go:85: verdict passed on an unmeasured regression: {Passed:true Failures:[]}`

The second reproduces the original #178 bug exactly.

## 5. Proof the rule now fires on a coarse clock

Forcing the Windows-equivalent window through the unexported seam
`runRegressionBenchmark(ctx, spec, measurementWindow(15_625*time.Microsecond))`
with the real test's `TargetBytes: 4_096`:

```
baseline=1e+12 -> 6.168278341s, iterations=31447, passed=true, failures=[]
baseline=1     -> 4.602487043s, iterations=26912, passed=false,
  failures=[{Rule:regression-ns-per-op ...} {Rule:regression-bytes-per-op ...}]
```

`regression-ns-per-op` fires on a clock that reads zero for one operation. That
is the rule #178 says could never fire.

## 6. Review round 1 — BLOCK, four fixes applied

1. `regressionQuantizationBudget` 0.01 -> 0.03. At 0.01 the Windows window is
   1.5625 s per run and the CI test cost 32-34 s. At 0.03 it is ~521 ms and the
   test costs 10.77 s. 3 percent stays well under the `MaxRegression` of 10
   percent that it guards.
2. `observeClockGranularity` had an unbounded busy loop. A frozen or
   virtualized clock would hang CI. Now bounded, with an injectable clock
   reader. Verified: a clock that never advances returns `0s`, which
   `measurementWindow` turns into the 20 ms floor.
3. One `regression-unmeasured` rule became two, so the verdict says which
   dimension went unmeasured, and no rule appears twice.
4. The amortization bias is documented in code instead of shipped silently.

## 7. The amortization bias — deliberately NOT fixed here

`measure` calls `runtime.GC` and `debug.FreeOSMemory` before it starts the
clock. The reset is outside the window, but it releases pages, so the
operations just after it pay to fault them back in. A batch divides that
warm-up across its iterations. A single-operation measurement charges the whole
cost to the one operation.

So the batched figure reads LOW, and the bias GROWS with batch size — worst on
Windows, the platform this fix serves.

| Machine | small batches | large batches |
|---|---|---|
| commander | -16.8% | -28.85% |
| reviewer | -23.3% | -28.4% |

`contract.go`'s `BeforeNSPerOp: 7_762_347` was recorded under single-operation
semantics, so the ratio against it is optimistic.

Reviewer 1 rated this a blocker. The commander overrode the severity and both
reviewer 2 and reviewer 1's own re-check agreed. Reasons:

- `scripts/performance_gate.sh` never passes `-regression-before-ns`, so the
  hardcoded baseline is always used. The real figure on this machine is ~1.15M
  ns against 7,762,347 — a ratio of **0.148**. The code must get **7.4x
  slower** before the rule fires. The gate is already far looser than the ~29%
  bias, so the bias is second-order.
- Re-recording a gate baseline is a user-approval action
  (`40-maintenance-protocol.md`).
- A hardware-calibrated constant must be recorded on certified hardware
  (`50-lessons.md` 2026-08-18, issue #175). `PERF_MINIMUM_PROFILE_CERTIFIED` is
  not set here, so recording it on this machine would repeat that mistake.

**Open for the user: re-record the baseline on certified hardware, and decide
whether the 7.4x looseness is tracked as its own issue.**

## 8. Review round 2 — APPROVE, three comment corrections

Reviewer 2 approved and raised three comment-accuracy points, all applied in
`10961cc`:

1. The mechanism was misstated. The reset runs before the clock starts, so it
   is never inside the window. The cost is post-reset warm-up.
2. "approximately 2000 iterations" did not reproduce (~700-750 on the second
   machine). Absolute hardware figures do not transfer. Now a range, marked as
   belonging to the hardware that produced it.
3. `would` violates the writing standard.

Reviewer 2 reported item 3 as pre-existing in master. It is not:
`git show 5e2503c:internal/perfharness/evaluate.go` does not contain the line,
so it came from `db394dd` on this branch and was in scope.

## 9. Gates

```
=== RESULT ===
All selected quality gates passed.
coverage gate passed: 85.6%
```

Coverage rose from 85.5% to 85.6%. No gate, threshold, or test was weakened.
`MaxRegression` is still 0.10. `scripts/coverage_gate.sh` and
`scripts/performance_gate.sh` are untouched. No `t.Skip`, no `//nolint`, no
deleted assertions. The original `regression-ns-per-op` assertion is intact.

Test cost: 0.14 s before the fix, 0.34 s now on Linux, 10.77 s at the
Windows-equivalent window.

`make verify-e2e` is NOT required. The change touches only four files under
`internal/perfharness/` — no scan pipeline, writer, or pressure-control code.
Both reviewers confirmed this independently.

## 10. Not verified

- **Real Windows hardware.** Every Windows figure here is simulated on Linux by
  forcing the Windows-equivalent window through the unexported seam. Real CI
  hardware is slower, so the 10.77 s is a lower bound. The first CI run on the
  PR is the real verification.
- **One unreproduced `make verify` failure.** Reviewer 2 saw a single
  `FAIL internal/perfharness` on its first run and lost the failing test name
  before it self-resolved. It did not reproduce across its 5 isolated package
  runs, 2 full `make verify` reruns, or 1 run under artificial CPU load. The
  commander then ran the package 6 times under `-race -shuffle=on` and 4 times
  without `-race`, all clean, plus 3 clean full `make verify` runs. About 26
  clean runs total, no reproduction. Consistent with this package's documented
  timing flakiness rather than something new, but NOT proven so.
- **Cross-provider review.** Codex is rate-limited until 2026-08-20, so both
  reviews were rank 2 under `60-development-guidelines.md` G2: a different
  Claude model with fresh context.

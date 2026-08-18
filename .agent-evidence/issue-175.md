# Issue #175 — rich memory budgets are now hardware-qualified

Branch `fix/175-qualify-memory-budgets`, cut from master `41be484`.

## Mechanism, in my own words

`internal/perfharness/rich_memory_linux_test.go` held two absolute
committed-memory ceilings on a one-million-item workload:

- `TestRichPrecheckWorkflowFitsScaledCommittedMemoryBudget` — 2,400,000,000 bytes
- `TestRichFixtureLoadFitsScaledCommittedMemoryBudget` — 900,000,000 bytes

An absolute committed-bytes ceiling measures the host allocator, page cache,
and available RAM as much as it measures the code. The ceiling was calibrated
on a developer machine, where it holds with 3-4% margin. The GitHub runner sits
roughly 80-100 MB higher and lands inside 0.2% of the ceiling, so the case
decided on run-to-run noise: master `161c209` failed by 0.17%, PR #174 failed
by 0.002% (47 KB) and passed on re-run of the identical commit.

The old build tag was `//go:build linux && !race`. That tag takes the case out
of the CI `go test -race` step but not out of CI. The coverage-gate step builds
without `-race`, so the case ran there and its failure was reported under the
`coverage gate (>= 85%)` heading while coverage itself was fine at 85.6%.

The fix follows what the repo already did for the identical failure in
`internal/perfharness/snapshot_growth_linux_test.go`: add `&& perfqualified`,
so the case builds only for the certified performance profile.

Verified file:line references for the machinery, which I did NOT modify:

- `scripts/performance_gate.sh:89` — `go test -tags perfqualified ./internal/perfharness -count=1 -timeout=30m`,
  inside `if [[ "$profile" == "full" ]]` at `scripts/performance_gate.sh:88`.
  The `else` branch at `scripts/performance_gate.sh:96` writes
  `hardware-qualified cases skipped: profile $profile is not the certified profile`.
- `.github/workflows/ci.yml:67` — `bash scripts/performance_gate.sh smoke`, so
  CI never takes the `full` branch and never runs the qualified cases.
- `internal/perfharness/gate_contract_test.go:36-45` (comment from line 32) — the existing pin that the
  step exists, that `"$profile" == "full"` precedes it, and that the log is
  preserved.

## What changed

| File | Note |
| --- | --- |
| `internal/perfharness/rich_memory_linux_test.go:1` | tag is now `//go:build linux && !race && perfqualified` |
| `internal/perfharness/rich_memory_linux_test.go:3-15` | header comment: why these ceilings are hardware-qualified |
| `internal/perfharness/rich_memory_qualification_linux_test.go` | NEW behavioural pin (untagged, `//go:build linux`) |
| `internal/perfharness/gate_contract_test.go:84-96` | NEW cross-platform pin on the build tag itself |
| `internal/perfharness/rich_memory_scale_linux_test.go` | NEW small-scale untagged cases, functional assertions, no byte ceiling |
| `docs/performance-harness.md:201-217` | the qualified-case paragraph now covers absolute memory ceilings too |
| `.claude/rules/50-lessons.md:9-41` | dated lesson, newest first |

## RED proof

Both pins were written and run BEFORE the build tag changed.

`TestRichCommittedMemoryBudgetsStayHardwareQualified`, verbatim:

```
=== RUN   TestRichCommittedMemoryBudgetsStayHardwareQualified
=== PAUSE TestRichCommittedMemoryBudgetsStayHardwareQualified
=== CONT  TestRichCommittedMemoryBudgetsStayHardwareQualified
    rich_memory_qualification_linux_test.go:29: TestRichFixtureLoadFitsScaledCommittedMemoryBudget is in the untagged build, so the correctness gate enforces a hardware-qualified budget on shared CI hardware
    rich_memory_qualification_linux_test.go:29: TestRichPrecheckWorkflowFitsScaledCommittedMemoryBudget is in the untagged build, so the correctness gate enforces a hardware-qualified budget on shared CI hardware
--- FAIL: TestRichCommittedMemoryBudgetsStayHardwareQualified (0.80s)
FAIL
FAIL	github.com/xuxiping/port-scan-mk3/internal/perfharness	0.803s
FAIL
```

`TestRichMemoryBudgetsCarryTheHardwareQualifiedBuildTag`, verbatim:

```
=== RUN   TestRichMemoryBudgetsCarryTheHardwareQualifiedBuildTag
=== PAUSE TestRichMemoryBudgetsCarryTheHardwareQualifiedBuildTag
=== CONT  TestRichMemoryBudgetsCarryTheHardwareQualifiedBuildTag
    gate_contract_test.go:94: rich memory budgets are not hardware-qualified: "//go:build linux && !race"
--- FAIL: TestRichMemoryBudgetsCarryTheHardwareQualifiedBuildTag (0.00s)
FAIL
FAIL	github.com/xuxiping/port-scan-mk3/internal/perfharness	0.002s
FAIL
```

The behavioural pin is the important one. It asks the toolchain
(`go test -list '.*'`, once untagged and once with `-tags perfqualified`) which
build each case lands in. A text match cannot prove that. It asserts both
halves: the cases are absent from the untagged build, and present in the
perfqualified build. Neither pin is itself `perfqualified`-tagged, so both run
in CI. The behavioural pin is `//go:build linux` with no `!race`, so it runs in
BOTH the `go test -race` step and the coverage-gate step.

## GREEN proof

```
=== RUN   TestRichMemoryBudgetsCarryTheHardwareQualifiedBuildTag
=== PAUSE TestRichMemoryBudgetsCarryTheHardwareQualifiedBuildTag
=== RUN   TestRichCommittedMemoryBudgetsStayHardwareQualified
=== PAUSE TestRichCommittedMemoryBudgetsStayHardwareQualified
=== CONT  TestRichMemoryBudgetsCarryTheHardwareQualifiedBuildTag
=== CONT  TestRichCommittedMemoryBudgetsStayHardwareQualified
--- PASS: TestRichMemoryBudgetsCarryTheHardwareQualifiedBuildTag (0.00s)
--- PASS: TestRichCommittedMemoryBudgetsStayHardwareQualified (0.57s)
PASS
ok  	github.com/xuxiping/port-scan-mk3/internal/perfharness	0.573s
```

## The qualified cases still run on the certified path

`GOTOOLCHAIN=go1.24.4 go test -tags perfqualified -run 'TestRich.*CommittedMemoryBudget' -v ./internal/perfharness/`

```
=== RUN   TestRichFixtureLoadFitsScaledCommittedMemoryBudget
    rich_memory_linux_test.go:40: committed memory: cold=744169472 steady=746373120
--- PASS: TestRichFixtureLoadFitsScaledCommittedMemoryBudget (9.09s)
=== RUN   TestRichPrecheckWorkflowFitsScaledCommittedMemoryBudget
    rich_memory_linux_test.go:63: committed memory: 2305597440
--- PASS: TestRichPrecheckWorkflowFitsScaledCommittedMemoryBudget (21.72s)
=== RUN   TestRichCommittedMemoryBudgetsStayHardwareQualified
=== PAUSE TestRichCommittedMemoryBudgetsStayHardwareQualified
=== CONT  TestRichCommittedMemoryBudgetsStayHardwareQualified
--- PASS: TestRichCommittedMemoryBudgetsStayHardwareQualified (0.49s)
PASS
ok  	github.com/xuxiping/port-scan-mk3/internal/perfharness	31.307s
```

Both budgets still hold on this machine. The cases are not retired; they moved
to `scripts/performance_gate.sh full`.

## Coverage

Measured with `GOTOOLCHAIN=go1.24.4 bash scripts/coverage_gate.sh` and
`GOTOOLCHAIN=go1.24.4 go tool cover -func=coverage.out | tail -1`.

| Stage | `internal/perfharness` | repo total |
| --- | --- | --- |
| Before any change (master `41be484`) | 82.6% | 85.6% |
| After the build tag, before the remedy | 82.5% | 85.5% |
| After the small-scale remedy (final) | 82.5% | 85.6% |

The tag change cost coverage in exactly one function: `internal/perfharness/measure.go:27`
`measure`, 96.1% to 90.2%. The lost lines are the 100 ms `heapTicker.C` sampling
branch inside the sampler goroutine, which only fires when an action runs longer
than 100 ms. That is a timing artifact of the one-million-item scale, not a
workflow path.

Remedy: `internal/perfharness/rich_memory_scale_linux_test.go` runs the same two
workflows — `RunFixtureCase` with `FamilyRichRecordMixed` and `RunRichSmoke` with
`FamilyRichPrecheck` — at 1,000 items instead of 1,000,000. It asserts functional
results (row counts, six runs, retained manifest artifact on disk, complete
pre-ping and snapshot) and that the committed-memory metric is populated. It
asserts NO byte ceiling. Both cases run in 0.03 s.

Repo total is back to 85.6%, the same number as master. No threshold was
lowered, no package was added to `EXCLUDE_PATTERN`, no test was deleted, no
`t.Skip` and no `//nolint` was added.

`internal/perfharness` is 82.5% and remains below 85% individually, exactly as
it was before this change. It is not in `EXCLUDE_PATTERN` and I did not put it
there. The gate is on the repo total, which passes.

## make verify

`GOTOOLCHAIN=go1.24.4 make verify`

```
coverage gate passed: 85.6%

=== RESULT ===
All selected quality gates passed.
```

## Triggers I checked against the actual diff

`git status --short` for this change:

```
 M .claude/rules/50-lessons.md
 M docs/performance-harness.md
 M internal/perfharness/gate_contract_test.go
 M internal/perfharness/rich_memory_linux_test.go
?? internal/perfharness/rich_memory_qualification_linux_test.go
?? internal/perfharness/rich_memory_scale_linux_test.go
```

- **e2e**: NOT triggered. The diff touches no scan pipeline, writer, or
  pressure-control code. Every code file changed is a `_test.go` file inside
  `internal/perfharness`. `make verify-e2e` was not run.
- **Performance benchmark**: NOT triggered. No hot path changed. No production
  file changed at all.

## Docs

Changed:

- `docs/performance-harness.md:201-217` — the paragraph that said only growth-ratio
  cases build under `perfqualified` was false after this change. It now also
  covers absolute committed-memory ceilings, states why, and records that the
  untagged build still runs both workflows at a small scale with no byte
  ceiling.

Deliberately not changed:

- `docs/MAINTENANCE.md` — no statement about these budgets or the qualified
  profile (`grep` for `rich_memory`, `memory budget`, `committed memory`,
  `perfqualified` returns nothing in that file).
- `docs/performance-harness.md:169,181,182` — these describe the evaluate
  module's own budget vocabulary, not these test ceilings. Still true.
- `docs/plans/`, `docs/superpowers/`, `docs/requests/`, `docs/specs/`,
  `docs/release-notes/`, `labs/`, `.agent-evidence/` — dated historical records
  and shipped release notes, out of scope by instruction.
- No release-notes entry was added. The next release is 5.0.0 and its notes are
  tracked on issue #173.
- `scripts/performance_gate.sh`, `.github/workflows/ci.yml`, and the existing
  guard in `gate_contract_test.go` were NOT modified. They are already correct.

## What I could NOT verify

1. **CI behaviour on the GitHub runner.** Everything here ran on this machine.
   I did not push and no CI run exists for this branch. The claim that the
   qualified cases no longer run in CI rests on reading
   `.github/workflows/ci.yml:66` (`performance_gate.sh smoke`) and
   `scripts/performance_gate.sh:87-96` (the `full` guard), plus the behavioural
   pin proving the cases leave the untagged build. It does not rest on an
   observed green CI run.
2. **The Windows job.** `gate_contract_test.go` is cross-platform, so the string
   pin runs there. `rich_memory_qualification_linux_test.go` is `//go:build linux`
   and does not. I did not run anything on Windows.
3. **`make verify-e2e`.** Not run, because the trigger does not apply. Not an
   e2e-relevant change.
4. **The 900 MB fixture-load budget's margin on the runner.** Issue #175 asks to
   check it. I have only this machine's numbers: cold 744,169,472 and steady
   746,373,120 bytes, about 17% under the 900 MB ceiling. I have no runner
   measurement for it. It is now hardware-qualified, so the question no longer
   gates CI, but the runner margin remains unmeasured.
5. **Nested `go test -list` cost in CI.** The behavioural pin shells out to the
   toolchain twice. Locally that is 0.5-0.8 s against a warm build cache. On a
   cold CI cache it compiles the package's test binary twice more, which will
   cost more. I did not measure it on CI hardware.
6. **Independent review.** Not obtained by me. This branch has not been reviewed
   by a second provider or a fresh-context agent.

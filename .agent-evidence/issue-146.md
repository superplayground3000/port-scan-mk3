# Issue 146 TDD evidence

## Confirmed seams

- `pkg/input` CIDR and port parser interfaces, including their file adapters.
- `pkg/state` snapshot load and save interfaces.
- `pkg/pressure` simple and OAuth response decoder interfaces.
- `pkg/config` validated command options for all four workflows.
- `pkg/scanapp` workflow side effects and the existing three-source-failure policy.
- `internal/perfharness` matrix and report interface.

All tests observe return values, errors, files, HTTP requests, or scan side effects through these seams.

## Red-first runs

### CIDR byte and record limits

Command: `go test ./pkg/input -run '^TestLoadCIDRsWithLimitsEnforcesBytesAndDataRecords$' -count=1`

Result: failed to compile because `input.CIDRLimits` and `input.LoadCIDRsWithColumnsContextAndLimits` did not exist.

### Port byte and record limits

Command: `go test ./pkg/input -run '^TestLoadPortsWithLimitsCountsNonblankDuplicateRecords$' -count=1`

Result: failed to compile because `input.LoadPortsContextWithLimits` did not exist.

### Snapshot load and atomic-save limits

Command: `go test ./pkg/state -run '^TestSnapshot(LimitsApplyToLoadAndAtomicSave|ByteLimitRejectsInputAndOutput)$' -count=1`

Result: failed to compile because `state.SnapshotLimits`, `state.LoadSnapshotWithLimits`, and `state.SaveSnapshotWithLimits` did not exist.

### Pressure response byte and entry limits

Command: `go test ./pkg/pressure -run '^(TestSimpleHTTPEnforcesContentLengthAndStreamLimit|TestOAuthHTTPEnforcesTokenBytesAndIncrementalDataEntries)$' -count=1`

Result: failed to compile because the response-limit type and constructors did not exist.

### Command limit defaults, overrides, bypass, and invalid values

Command: `go test ./pkg/config -run '^Test(CommandResourceLimitFlagsHaveDefaultsOverridesAndIndependentBypass|ResourceLimitFlagsRejectNegativeAndOverflowValues)$' -count=1`

Result: failed to compile because `config.ResourceLimitValues` and `ResolveResourceLimits` did not exist.

### Workflow side-effect ordering

Command: `go test ./pkg/scanapp -run '^(TestRunPrePingRejectsCIDRRecordLimitBeforePingOrOutput|TestGenerateBucketsRejectsPortAndSnapshotLimitsWithoutSnapshotArtifact)$' -count=1`

Result: all three cases failed because the workflows returned success and produced side effects instead of applying the parsed limits.

### Validate workflow limits

Command: `go test ./pkg/validate -run '^TestInputs_AppliesCIDRAndPortRecordLimits$' -count=1`

Result: failed because `Inputs(-cidr-input-record-limit)` returned `{Valid:true Detail:ok}`.

### Scan snapshot side-effect ordering

Command: `go test ./pkg/scanapp -run '^TestRunRejectsSnapshotLimitsBeforeOutputOrTCP$' -count=1`

Result: failed because `Run()` returned success instead of rejecting the two-chunk snapshot at limit one.

### Pressure limit wiring and three-failure policy

Command: `go test ./pkg/scanapp -run '^TestPressureResponseLimitFailureUsesThreeFailurePolicy$' -count=1 -timeout=5s`

Result: failed after one second because the source ignored the configured byte limit and the poller did not reach three failures.

### Explicit limits for Go callers

Command: `go test ./pkg/config -run '^TestProgrammaticConstructorsAcceptExplicitDisabledLimits$' -count=1`

Result: failed to compile because `config.NewScanWithResourceLimits` did not exist.

The expanded constructor test then failed because the validate, pre-ping, and generate-buckets explicit constructors did not exist.

### Performance matrix for ten resource-limit flags

Command: `go test ./internal/perfharness -run '^TestRunResourceLimitCaseExecutesEveryNonTargetFlagAndBypassKind$' -count=1`

Result: failed to compile because `ResourceLimitSpec` and `RunResourceLimitCase` did not exist.

### Rich oversized input contract

Command: `go test ./internal/perfharness -run '^TestRunRichOversizeCaseRejectsDefaultAndCompletesWithPositiveOverride$' -count=1`

Result: failed to compile because `RichOversizeSpec` and `RunRichOversizeCase` did not exist.

Runner command: `go test ./internal/perfharness/cmd/perf-harness -run '^TestRunCommandWritesSmokeReports$' -count=1 -timeout=120s`

Result: failed with 51 cases instead of 111 because the command still skipped the ten non-target limit groups.

### CIDR load scale matrix

Command: `go test ./internal/perfharness -run '^TestContractListsEveryRequiredScaleCase$' -count=1`

Result: failed because the contract lacked the 1 MB, 10 MB, and 100 MB CIDR load fixtures.

Command: `go test ./internal/perfharness -run '^TestRunFixtureCaseUsesProductionCIDRLoader$' -count=1`

Result: failed because the fixture case reported zero production CIDR rows instead of ten. It only checked the fixture digest.

### CIDR and snapshot growth thresholds

Command: `go test ./internal/perfharness/cmd/perf-harness -run '^TestApplyInputAndSnapshotGrowthThresholdsChecksEveryTenfoldStep$' -count=1`

Result: failed to compile because `applyInputAndSnapshotGrowthThresholds` did not exist.

### CLI help

Command: `go test ./cmd/port-scan -run '^TestCLIHelp_IncludesRequiredFlags$' -count=1`

Result: failed because the top-level help did not list `-cidr-input-size-limit-gb` or the other resource-limit flags.

### Isolated oversized pressure service

Command: `go test ./e2e/mock-pressure-api -run '^TestNewPressureHandler_OversizeRecordsClientFailure$' -count=1`

Result: failed with `status = 500, want 200` because the isolated mock had no oversized-success response mode.

## Green evidence

### Focused race suite

Command: `go test -race ./pkg/input ./pkg/state ./pkg/pressure ./pkg/config ./pkg/validate ./pkg/scanapp -count=1 -timeout=180s`

Result:

```text
ok  github.com/xuxiping/port-scan-mk3/pkg/input 1.011s
ok  github.com/xuxiping/port-scan-mk3/pkg/state 1.015s
ok  github.com/xuxiping/port-scan-mk3/pkg/pressure 1.023s
ok  github.com/xuxiping/port-scan-mk3/pkg/config 1.015s
ok  github.com/xuxiping/port-scan-mk3/pkg/validate 1.009s
ok  github.com/xuxiping/port-scan-mk3/pkg/scanapp 2.768s
```

The first broad race attempt exposed an early-return compatibility defect.
Test configurations without the optional resource-limit resolver returned before their cleanup paths ran.
The scan workflow now gives these configurations the documented default limits.

### Six-run before and after benchmarks

Command: `go test ./pkg/input ./pkg/pressure -run '^$' -bench 'Benchmark(LoadPortsOneMB|SimpleResponseOneMB)$' -benchmem -count=6`

The before run used an archive of base commit `8f247acdde26e7139a08a954f3d655d7ea4fd69d` in `/tmp/issue146-baseline.Knpl0f`.
The archive contained only the two compatible benchmark files from this branch.
It did not modify another repository or worktree.

| Benchmark | Base median | First branch median | Change | Base bytes/op | First branch bytes/op |
| --- | ---: | ---: | ---: | ---: | ---: |
| Port input, 0.98 MB | `6562729 ns/op` | `6914739 ns/op` | `+5.36%` | `17059527` | `19222030` |
| Simple pressure, 1 MB | `3772545 ns/op` | `2826907 ns/op` | `-25.07%` | `4153399` | `3130531` |

This first port result was a blocker.
The bounded port path added one complete input buffer before parsing.
Its `B/op` increase was `12.68%`, which exceeded the `10%` allocation threshold.

#### Superseding result after the allocation fix

The input modules now enforce the byte limit through a streaming reader.
They no longer retain a second complete input buffer.
The same branch command ran six more times after this fix.

| Benchmark | Base median | Fixed branch median | Change |
| --- | ---: | ---: | ---: |
| Port `ns/op` | `6562729` | `6763651` | `+3.06%` |
| Port `B/op` | `17059527` | `17059607` | `+0.0005%` |
| Port `allocs/op` | `131099` | `131100` | `+0.0008%` |
| Simple pressure `ns/op` | `3772545` | `2904829` | `-23.00%` |
| Simple pressure `B/op` | `4153399` | `3129756` | `-24.64%` |
| Simple pressure `allocs/op` | `146` | `110` | `-24.66%` |

The fixed port result is below the `10%` time, byte, and allocation thresholds.
The pressure path is faster and allocates less memory because it decodes one named field.

### Full verification correction

The first `make verify` run passed formatting, vet, builds, release reproducibility, and all race tests.
The coverage gate then rejected total statement coverage of `84.6%`.

Focused file-adapter tests now cover exact metadata limits, early oversized-file rejection, and missing-file errors.
The next standalone coverage gate passed at `85.0%` without a threshold change.

The final `make verify` run completed with:

```text
coverage gate passed: 85.0%

=== RESULT ===
All selected quality gates passed.
```

The isolated Docker gate used `COMPOSE_PROJECT_NAME=issue146_limits_c3d4071`.
It completed with the same final result line.
The new cases rejected an oversized port input before snapshot creation.
They also stopped a scan after three oversized pressure responses.

### Full performance fixture correction

The first full performance run passed all cases through the rich oversized group.
Its first default-reject observation then failed with:

```text
default rich input limit did not reject the oversized fixture: <nil>
```

The manifest showed `963750116` actual bytes for a `1000000001` byte target.
The production limit correctly accepted this file because it was less than 1 GB.
The rich fixture now uses a conservative fixed-width estimate.
A fixture test verifies that `TargetBytes` is a lower bound.

The failed run took `14:05.20` and reached `18996320` KB maximum RSS.
Its raw `/usr/bin/time -v` process metric reported `Swaps: 0`.
This value describes only that measured process; it does not assign global swap activity.

After the fixture correction, `make verify` again completed with:

```text
coverage gate passed: 85.0%

=== RESULT ===
All selected quality gates passed.
```

### Snapshot growth evaluator correction

The complete matrix exposed a growth comparison between `chunk-heavy` at 1 MB and `port-heavy` at 10 MB.
These are different object shapes and cannot form one growth chain.

Command: `go test ./internal/perfharness/cmd/perf-harness -run '^TestApplyInputAndSnapshotGrowthThresholdsChecksEveryTenfoldStep$' -count=1`

Result: failed because the evaluator marked `snapshot-heavy/port-heavy` instead of the same-shape `snapshot-heavy/mixed/ten-megabytes` case.

The corrected evaluator uses one explicit `snapshot-heavy/mixed` chain.
It compares 1 MB, 10 MB, 100 MB, and 1 GB in ascending order.
The object-heavy snapshot shapes remain independent correctness cases.

After this correction, the focused harness packages passed in `2.816s` and `17.744s`.
The complete `make verify` gate also passed at `85.0%` coverage.

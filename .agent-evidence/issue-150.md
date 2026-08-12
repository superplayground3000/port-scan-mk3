# Issue 150 TDD evidence

## Confirmed seams

- `internal/perfharness` owns fixture generation, manifests, metrics, normalization, evaluation, and reports.
- Linux shell and Windows PowerShell scripts own process launch, OS metrics, filesystem setup, and cleanup.
- `pkg/scanapp.RunOptions.Dial` supplies the fake probe to the production scan workflow.
- Make targets, CI smoke entrypoints, fixtures, and reports are observable artifacts.

## Red and green log

### Deterministic record-heavy fixture

Red command:

```text
go test ./internal/perfharness -run TestGenerateRecordHeavyIsDeterministic -count=1
```

Expected red reason: the `internal/perfharness` package does not exist.

Observed red result:

```text
github.com/xuxiping/port-scan-mk3/internal/perfharness: no non-test Go files in /tmp/port-scan-mk3-issue-150/internal/perfharness
FAIL github.com/xuxiping/port-scan-mk3/internal/perfharness [build failed]
```

Green command:

```text
go test ./internal/perfharness -run TestContractListsEveryRequiredScaleCase -count=1
```

Observed green result:

```text
ok github.com/xuxiping/port-scan-mk3/internal/perfharness 0.003s
```

### One-gigabyte record fixture definition

Red command:

```text
go test ./internal/perfharness -run TestContractListsEveryRequiredScaleCase -count=1
```

Observed red reason:

```text
the record-heavy family lacks a 1 GB fixture
FAIL github.com/xuxiping/port-scan-mk3/internal/perfharness 0.003s
```

Green command:

```text
go test ./internal/perfharness -run 'TestContractListsEveryRequiredScaleCase|TestGenerateEveryFixtureFamilyProducesAValidManifest' -count=1
```

Observed green result:

```text
ok github.com/xuxiping/port-scan-mk3/internal/perfharness 0.005s
```

## Quality gates

### Final Go 1.24.4 repository gate

Command:

```text
GOTOOLCHAIN=go1.24.4 make verify
```

Result:

```text
coverage gate passed: 85.9%
=== RESULT ===
All selected quality gates passed.
```

### Final Go 1.24.4 Docker e2e gate

Command:

```text
GOTOOLCHAIN=go1.24.4 make verify-e2e
```

Result:

```text
PASS
ok github.com/xuxiping/port-scan-mk3/tests/integration 2.636s
=== RESULT ===
All selected quality gates passed.
```

### Complete Go 1.24.4 Linux performance matrix

Command:

```text
GOTOOLCHAIN=go1.24.4 make verify-performance
```

Every matrix result:

```text
case passed: record-heavy
case passed: record-heavy/one-gigabyte
case passed: candidate-heavy
case passed: port-heavy
case passed: task-heavy
case passed: output-heavy
case passed: snapshot-heavy/chunk-heavy
case passed: snapshot-heavy/port-heavy
case passed: snapshot-heavy/unreachable-heavy
case passed: snapshot-heavy/mixed
case passed: resume-heavy
case passed: resume-heavy
case passed: resume-heavy
case passed: rich-record-mixed
case passed: rich-unique-key
case passed: rich-hot-key
case passed: rich-precheck
case passed: rich-deny/deny-only
case passed: rich-deny/accept-deny-conflict
case passed: production-workflow/workers-1
case passed: production-workflow/workers-16
case passed: production-workflow/workers-256
case passed: native-loopback/workers-1
case passed: native-loopback/workers-32
performance matrix passed: JSON=performance-out/run-20260812T080206Z-1907639/report/performance-report.json Markdown=performance-out/run-20260812T080206Z-1907639/report/performance-report.md
Performance matrix artifacts: performance-out/run-20260812T080206Z-1907639
```

The final complete matrix used 9.9 GB and 4:00.73 of wall time.
The maximum RSS was 218,808 KB. It used zero swaps and three major page faults.
All 24 case observations contain nonzero Linux peak RSS and committed memory.
The runner applied the cold and steady worker-memory threshold.

Hardware evidence:

```text
label: hardware-qualified
CPU: AMD RYZEN AI MAX+ 395 w/ Radeon 8060S
physical cores: 16
logical cores: 32
RAM: 131891437568 bytes
power mode: powersave
filesystem: tmpfs
disk: WD_BLACK SN7100 2TB
Go: go1.24.4
commit under test: 4febd75330d1ba979ba90dc38d2bf7f0b1776c95 plus the worktree changes
constraints: none recorded
```

### Final six-run Go 1.24.4 benchmark

Command:

```text
for run in 1 2 3 4 5 6; do GOTOOLCHAIN=go1.24.4 go test ./internal/perfharness -run '^$' -bench '^BenchmarkGenerateSnapshotOneMB$' -benchmem -count=1; done
```

Results:

```text
1788252 ns/op 262529 B/op 5 allocs/op
1704099 ns/op 262516 B/op 5 allocs/op
1754792 ns/op 262508 B/op 5 allocs/op
1697856 ns/op 262509 B/op 5 allocs/op
1709732 ns/op 262500 B/op 5 allocs/op
1722594 ns/op 262507 B/op 5 allocs/op
```

The median is approximately 1,717,000 ns/op and 262,508 B/op.
The result is less than the recorded before median for both metrics.

### Windows cross-build and adapter syntax

Commands:

```text
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 GOTOOLCHAIN=go1.24.4 go test -c ./internal/perfharness
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 GOTOOLCHAIN=go1.24.4 go test -c ./internal/perfharness/cmd/perf-harness
bash -n scripts/performance_gate.sh
git diff --check
```

Result:

```text
perfharness.test.exe: PE32+ executable for MS Windows, x86-64
perf-command.test.exe: PE32+ executable for MS Windows, x86-64
all commands exited 0
```

PowerShell was not installed on the Linux host. Native Windows CI must parse and run the PowerShell adapter.

### Simplified Technical English self-check

Mode: pragmatic. The text is descriptive, except for the command procedures.
The selected verb for the check concept is `make sure that`.

The three longest prose lines in `docs/performance-harness.md` contain 14 words each.
No changed prose contains a contraction, a semicolon, a banned modal, or a complex perfect tense.
All procedural conditions come before their commands. No unwanted term from the check concept remains.

### Status writer failure

Red command:

```text
GOTOOLCHAIN=go1.24.4 go test ./internal/perfharness/cmd/perf-harness -run TestRunCommandFailsWhenItCannotWriteStatus -count=1
```

Observed red reason:

```text
exit code = 0, want 1 for status output failure
FAIL github.com/xuxiping/port-scan-mk3/internal/perfharness/cmd/perf-harness 0.432s
```

Green command:

```text
GOTOOLCHAIN=go1.24.4 go test ./internal/perfharness/cmd/perf-harness -run TestRunCommandFailsWhenItCannotWriteStatus -count=1
```

Observed green result:

```text
ok github.com/xuxiping/port-scan-mk3/internal/perfharness/cmd/perf-harness 0.003s
```

### Independent Standards review round one

The reviewer blocked the first commit for these items:

- CLI and workflow failure tests were missing.
- CLI status writer errors were ignored.
- Worker profiles repeated the matrix contract.

The change now includes a red-first status writer failure test and a zero-item workflow failure test.
The command checks each status write and reads both worker profiles from the matrix contract.

The reviewer also called the required single `Harness` interface a prohibited god interface.
Issue 150 explicitly requires one private interface for these evidence operations.
The interface remains one deep evidence-run boundary, and production packages do not use it.

### Per-case native process metrics

Red command:

```text
GOTOOLCHAIN=go1.24.4 go test ./internal/perfharness -run TestMeasureRecordsPortableGoMetrics -count=1
```

Observed red reason:

```text
LinuxPeakRSSBytes:0 PeakCommittedBytes:0
FAIL github.com/xuxiping/port-scan-mk3/internal/perfharness 0.003s
```

Green commands:

```text
GOTOOLCHAIN=go1.24.4 go test ./internal/perfharness -run TestMeasureRecordsPortableGoMetrics -count=1
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 GOTOOLCHAIN=go1.24.4 go test -c ./internal/perfharness
```

Observed green result:

```text
ok github.com/xuxiping/port-scan-mk3/internal/perfharness 0.003s
Windows output: PE32+ executable for MS Windows, x86-64
```

The Linux adapter reads `VmHWM` and `VmSwap` from `/proc/self/status` for each action.
The Windows adapter uses the peak fields from `GetProcessMemoryInfo` for each action.
The scripts keep the raw whole-process evidence in addition to these case metrics.

Neither adapter maps generic file I/O to paging I/O.
The paging byte fields are zero when the OS does not expose per-process paging byte counters.

The first final-matrix attempt stopped at the free-space preflight:

```text
insufficient free space: have 49586282496 bytes, require 50000000000 bytes
```

The previous generated matrix directory was the exact cause of the space shortage.
After its evidence was recorded, safe cleanup removed only that exact ignored directory.
The second attempt passed and produced the final artifact path above.

The bounded native Linux smoke passed with nonzero RSS and committed-memory fields for all seven cases.
The worker-memory evaluator compared the cold and steady 16-worker and 256-worker observations.

### Worker-memory threshold in the matrix runner

Red command:

```text
GOTOOLCHAIN=go1.24.4 go test ./internal/perfharness/cmd/perf-harness -run TestApplyWorkerMemoryThresholdBlocksMoreThanTwentyFivePercentGrowth -count=1
```

Observed red reason:

```text
undefined: applyWorkerMemoryThreshold
FAIL github.com/xuxiping/port-scan-mk3/internal/perfharness/cmd/perf-harness [build failed]
```

Green commands:

```text
GOTOOLCHAIN=go1.24.4 go test ./internal/perfharness/cmd/perf-harness -run TestApplyWorkerMemoryThresholdBlocksMoreThanTwentyFivePercentGrowth -count=1
GOTOOLCHAIN=go1.24.4 bash scripts/performance_gate.sh smoke /tmp/port-scan-mk3-issue-150-smoke-per-case
```

Observed green result:

```text
ok github.com/xuxiping/port-scan-mk3/internal/perfharness/cmd/perf-harness 0.002s
performance matrix passed: JSON=/tmp/port-scan-mk3-issue-150-smoke-per-case/report/performance-report.json Markdown=/tmp/port-scan-mk3-issue-150-smoke-per-case/report/performance-report.md
Performance matrix artifacts: /tmp/port-scan-mk3-issue-150-smoke-per-case
```

### Full repository gate

Command:

```text
make verify
```

Result:

```text
coverage gate passed: 86.1%
=== RESULT ===
All selected quality gates passed.
```

### Native Linux smoke

Command:

```text
bash scripts/performance_gate.sh smoke /tmp/port-scan-mk3-issue-150-smoke
```

Result:

```text
performance matrix passed: JSON=/tmp/port-scan-mk3-issue-150-smoke/report/performance-report.json Markdown=/tmp/port-scan-mk3-issue-150-smoke/report/performance-report.md
Performance matrix artifacts: /tmp/port-scan-mk3-issue-150-smoke
```

The matrix used 380 MB. Its wall time was 52.84 seconds.
The maximum RSS was 238,860 KB. It used zero swaps and zero major page faults.

### Snapshot generator benchmark before allocation correction

Command:

```text
go test -run '^$' -bench '^BenchmarkGenerateSnapshotOneMB$' -benchmem -count=6 ./internal/perfharness
```

Results:

```text
9747009 ns/op 1134501 B/op 109023 allocs/op
5584630 ns/op 1134358 B/op 109023 allocs/op
5422407 ns/op 1134357 B/op 109023 allocs/op
7318697 ns/op 1134355 B/op 109023 allocs/op
8228261 ns/op 1134425 B/op 109023 allocs/op
7762347 ns/op 1134359 B/op 109023 allocs/op
```

### Snapshot generator benchmark after allocation correction

Command:

```text
go test -run '^$' -bench '^BenchmarkGenerateSnapshotOneMB$' -benchmem -count=6 ./internal/perfharness
```

Results:

```text
3257380 ns/op 262216 B/op 2 allocs/op
3087927 ns/op 262188 B/op 2 allocs/op
3311116 ns/op 262174 B/op 2 allocs/op
3472829 ns/op 262172 B/op 2 allocs/op
3193960 ns/op 262180 B/op 2 allocs/op
2737847 ns/op 262174 B/op 2 allocs/op
```

The change decreased both `ns/op` and `B/op`. It removed 109,021 allocations per operation.

### Distinct snapshot shapes

Red command:

```text
go test ./internal/perfharness -run TestSnapshotHeavyShapesProduceDistinctValidSnapshots -count=1
```

Observed red reason:

```text
chunk-heavy shape had zero chunks and an unreachable-only payload
port-heavy shape had zero chunks and an unreachable-only payload
mixed shape had zero chunks and an unreachable-only payload
FAIL github.com/xuxiping/port-scan-mk3/internal/perfharness 0.004s
```

Green command:

```text
go test ./internal/perfharness -run TestSnapshotHeavyShapesProduceDistinctValidSnapshots -count=1
```

Observed green result:

```text
ok github.com/xuxiping/port-scan-mk3/internal/perfharness 0.003s
```

### Matrix contract in the report schema

Red command:

```text
go test ./internal/perfharness/cmd/perf-harness -run TestRunCommandWritesSmokeReports -count=1
```

Observed red reason:

```text
internal/perfharness/cmd/perf-harness/main_test.go:43:16: report.Contract undefined
FAIL github.com/xuxiping/port-scan-mk3/internal/perfharness/cmd/perf-harness [build failed]
```

Green command:

```text
go test ./internal/perfharness -run TestRunNativeLoopbackSmokeSupportsRequiredWorkerProfiles -count=1
```

Observed green result:

```text
ok github.com/xuxiping/port-scan-mk3/internal/perfharness 0.028s
```

### Loopback cases in the command matrix

Red command:

```text
go test ./internal/perfharness/cmd/perf-harness -run TestRunCommandWritesSmokeReports -count=1
```

Observed red reason:

```text
case count = 5, want fixture, fake-worker, and loopback-worker cases
FAIL github.com/xuxiping/port-scan-mk3/internal/perfharness/cmd/perf-harness 0.262s
```

Green command:

```text
go test ./internal/perfharness/cmd/perf-harness -run TestRunCommandWritesSmokeReports -count=1
```

Observed green result:

```text
ok github.com/xuxiping/port-scan-mk3/internal/perfharness/cmd/perf-harness 0.456s
```

### Failure, rich override, and flush matrix definitions

Red command:

```text
go test ./internal/perfharness -run TestContractListsEveryRequiredScaleCase -count=1
```

Observed red reason:

```text
internal/perfharness/harness_test.go:164:27: contract.FailureScenarios undefined
internal/perfharness/harness_test.go:167:27: contract.RichOversizeCases undefined
internal/perfharness/harness_test.go:170:27: contract.OutputFlushIntervals undefined
FAIL github.com/xuxiping/port-scan-mk3/internal/perfharness [build failed]
```

Green command:

```text
go test ./internal/perfharness -run 'TestCancellationInjectorCoversEveryStageAndProgressPoint|TestContractListsEveryRequiredScaleCase' -count=1
```

Observed green result:

```text
ok github.com/xuxiping/port-scan-mk3/internal/perfharness 0.002s
```

### Native loopback worker profiles

Red command:

```text
go test ./internal/perfharness -run TestRunNativeLoopbackSmokeSupportsRequiredWorkerProfiles -count=1
```

Observed red reason:

```text
internal/perfharness/workflow_test.go:36:26: harness.RunNativeLoopbackSmoke undefined
FAIL github.com/xuxiping/port-scan-mk3/internal/perfharness [build failed]
```

### CRLF fixture output

Red command:

```text
go test ./internal/perfharness -run TestGenerateRecordHeavySupportsCRLF -count=1
```

Observed red reason:

```text
fixture does not use CRLF only: "ip,ip_cidr,fab_name,cidr_name\n..."
FAIL github.com/xuxiping/port-scan-mk3/internal/perfharness 0.003s
```

Green command:

```text
go test ./internal/perfharness -run TestGenerateRecordHeavySupportsCRLF -count=1
```

Observed green result:

```text
ok github.com/xuxiping/port-scan-mk3/internal/perfharness 0.002s
```

Green command:

```text
go test ./internal/perfharness -run TestRunFixtureCaseKeepsOneFixtureAndSummarizesSixRuns -count=1
```

Observed green result:

```text
ok github.com/xuxiping/port-scan-mk3/internal/perfharness 0.003s
```

### Performance command and smoke report

Red command:

```text
go test ./internal/perfharness/cmd/perf-harness -run TestRunCommandWritesSmokeReports -count=1
```

Observed red reason:

```text
internal/perfharness/cmd/perf-harness/main_test.go:19:14: undefined: runCommand
FAIL github.com/xuxiping/port-scan-mk3/internal/perfharness/cmd/perf-harness [build failed]
```

Green command:

```text
go test ./internal/perfharness/cmd/perf-harness -run TestRunCommandWritesSmokeReports -count=1
```

Observed green result:

```text
ok github.com/xuxiping/port-scan-mk3/internal/perfharness/cmd/perf-harness 0.260s
```

### Native adapters and gate entrypoints

Red command:

```text
go test ./internal/perfharness -run TestPerformanceGateEntrypointsKeepOSAdaptersThin -count=1
```

Observed red reason:

```text
read scripts/performance_gate.sh: open /tmp/port-scan-mk3-issue-150/scripts/performance_gate.sh: no such file or directory
FAIL github.com/xuxiping/port-scan-mk3/internal/perfharness 0.002s
```

Green command:

```text
go test ./internal/perfharness -run TestPerformanceGateEntrypointsKeepOSAdaptersThin -count=1
```

Observed green result:

```text
ok github.com/xuxiping/port-scan-mk3/internal/perfharness 0.002s
```

### Cancellation and line-ending contract

Red command:

```text
go test ./internal/perfharness -run 'TestCancellationInjectorCoversEveryStageAndProgressPoint|TestContractListsEveryRequiredScaleCase' -count=1
```

Observed red reason:

```text
internal/perfharness/cancellation_test.go:15:14: contract.StopWithin undefined
internal/perfharness/cancellation_test.go:21:33: undefined: perfharness.NewCancellationInjector
internal/perfharness/harness_test.go:140:27: contract.InputLineEndings undefined
FAIL github.com/xuxiping/port-scan-mk3/internal/perfharness [build failed]
```

Green command:

```text
go test ./internal/perfharness -run TestRunProductionSmokeUsesTheProductionWorkflowWithFakeProbes -count=1
```

Observed green result:

```text
ok github.com/xuxiping/port-scan-mk3/internal/perfharness 0.017s
```

### Six-run fixture case

Red command:

```text
go test ./internal/perfharness -run TestRunFixtureCaseKeepsOneFixtureAndSummarizesSixRuns -count=1
```

Observed red reason:

```text
internal/perfharness/matrix_test.go:16:25: harness.RunFixtureCase undefined
FAIL github.com/xuxiping/port-scan-mk3/internal/perfharness [build failed]
```

Green command:

```text
go test ./internal/perfharness -run TestMeasureRecordsPortableGoMetrics -count=1
```

Observed green result:

```text
ok github.com/xuxiping/port-scan-mk3/internal/perfharness 0.003s
```

### Production workflow with a fake probe

Red command:

```text
go test ./internal/perfharness -run TestRunProductionSmokeUsesTheProductionWorkflowWithFakeProbes -count=1
```

Observed red reason:

```text
internal/perfharness/workflow_test.go:15:25: harness.RunProductionSmoke undefined
internal/perfharness/workflow_test.go:15:78: undefined: perfharness.WorkflowSpec
FAIL github.com/xuxiping/port-scan-mk3/internal/perfharness [build failed]
```

Green command:

```text
go test ./internal/perfharness -run TestCompareSemanticNormalizesOnlyDeclaredVolatileFields -count=1
```

Observed green result:

```text
ok github.com/xuxiping/port-scan-mk3/internal/perfharness 0.002s
```

### Portable Go metrics

Red command:

```text
go test ./internal/perfharness -run TestMeasureRecordsPortableGoMetrics -count=1
```

Observed red reason:

```text
internal/perfharness/measure_test.go:14:30: harness.Measure undefined
FAIL github.com/xuxiping/port-scan-mk3/internal/perfharness [build failed]
```

Green command:

```text
go test ./internal/perfharness -run TestWriteReportsRecordsColdRunAndFiveRunMedian -count=1
```

Observed green result:

```text
ok github.com/xuxiping/port-scan-mk3/internal/perfharness 0.002s
```

### Semantic parity normalization

Red command:

```text
go test ./internal/perfharness -run TestCompareSemanticNormalizesOnlyDeclaredVolatileFields -count=1
```

Observed red reason:

```text
internal/perfharness/normalize_test.go:14:23: undefined: perfharness.SemanticArtifact
internal/perfharness/normalize_test.go:33:28: harness.CompareSemantic undefined
FAIL github.com/xuxiping/port-scan-mk3/internal/perfharness [build failed]
```

Green command:

```text
go test ./internal/perfharness -run TestEvaluateAppliesAbsoluteGrowthRegressionAndWorkerBudgets -count=1
```

Observed green result:

```text
ok github.com/xuxiping/port-scan-mk3/internal/perfharness 0.002s
```

### JSON and Markdown reports

Red command:

```text
go test ./internal/perfharness -run TestWriteReportsRecordsColdRunAndFiveRunMedian -count=1
```

Observed red reason:

```text
internal/perfharness/report_test.go:27:29: undefined: perfharness.SummarizeCase
internal/perfharness/report_test.go:44:24: harness.WriteReports undefined
FAIL github.com/xuxiping/port-scan-mk3/internal/perfharness [build failed]
```

Green command:

```text
go test ./internal/perfharness -run TestGenerateRecordHeavyIsDeterministic -count=1
```

Observed green result:

```text
ok github.com/xuxiping/port-scan-mk3/internal/perfharness 0.003s
```

### Complete matrix structure

Red command:

```text
go test ./internal/perfharness -run TestContractListsEveryRequiredScaleCase -count=1
```

Observed red reason:

```text
internal/perfharness/harness_test.go:56:26: undefined: perfharness.DefaultContract
internal/perfharness/harness_test.go:59:15: undefined: perfharness.FamilyCandidateHeavy
FAIL github.com/xuxiping/port-scan-mk3/internal/perfharness [build failed]
```

Green command:

```text
go test ./internal/perfharness -run TestContractListsEveryRequiredScaleCase -count=1
```

Observed green result:

```text
ok github.com/xuxiping/port-scan-mk3/internal/perfharness 0.004s
```

### Threshold evaluation

Red command:

```text
go test ./internal/perfharness -run TestEvaluateAppliesAbsoluteGrowthRegressionAndWorkerBudgets -count=1
```

Observed red reason:

```text
internal/perfharness/evaluate_test.go:14:21: harness.Evaluate undefined
internal/perfharness/evaluate_test.go:14:42: undefined: perfharness.EvaluationInput
FAIL github.com/xuxiping/port-scan-mk3/internal/perfharness [build failed]
```

### Fixture families and manifest verification

Red command:

```text
go test ./internal/perfharness -run TestGenerateEveryFixtureFamilyProducesAValidManifest -count=1
```

Observed red reason:

```text
internal/perfharness/harness_test.go:67:22: harness.Validate undefined
internal/perfharness/harness_test.go:70:41: manifest.ManifestPath undefined
FAIL github.com/xuxiping/port-scan-mk3/internal/perfharness [build failed]
```

Green command:

```text
go test ./internal/perfharness -run TestGenerateEveryFixtureFamilyProducesAValidManifest -count=1
```

Observed green result:

```text
ok github.com/xuxiping/port-scan-mk3/internal/perfharness 0.004s
```

### Separate fixture-generation and production-stage metrics

Red command:

```text
go test ./internal/perfharness -run TestRunProductionSmokeUsesTheProductionWorkflowWithFakeProbes -count=1
```

Observed red reason:

```text
result.FixtureGeneration undefined
result.Stage undefined
FAIL github.com/xuxiping/port-scan-mk3/internal/perfharness [build failed]
```

Green command:

```text
go test ./internal/perfharness -run TestRunProductionSmokeUsesTheProductionWorkflowWithFakeProbes -count=1
```

Observed green result:

```text
ok github.com/xuxiping/port-scan-mk3/internal/perfharness 0.022s
```

### Separate phase metrics in the report

Red command:

```text
go test ./internal/perfharness/cmd/perf-harness -run TestRunCommandWritesSmokeReports -count=1
```

Observed red reason:

```text
result.FixtureGeneration undefined
FAIL github.com/xuxiping/port-scan-mk3/internal/perfharness/cmd/perf-harness [build failed]
```

Green command:

```text
go test ./internal/perfharness/cmd/perf-harness -run TestRunCommandWritesSmokeReports -count=1
```

Observed green result:

```text
ok github.com/xuxiping/port-scan-mk3/internal/perfharness/cmd/perf-harness 0.434s
```

### Port-heavy snapshot total count

Red command:

```text
go test ./internal/perfharness -run 'TestSnapshotHeavyShapesProduceDistinctValidSnapshots/port-heavy' -count=1
```

Observed red reason:

```text
port-heavy total count = 0, want 401
FAIL github.com/xuxiping/port-scan-mk3/internal/perfharness 0.002s
```

Green command:

```text
go test ./internal/perfharness -run 'TestSnapshotHeavyShapesProduceDistinctValidSnapshots/port-heavy' -count=1
```

Observed green result:

```text
ok github.com/xuxiping/port-scan-mk3/internal/perfharness 0.003s
```

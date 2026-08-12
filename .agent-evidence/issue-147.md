# Issue 147 evidence

## Baseline benchmark

Command:

```text
go test -run '^$' -bench '^BenchmarkWriteScanRecord$' -benchmem -count=6 ./pkg/scanapp
```

Environment: `go version go1.26.1 linux/amd64`

```text
BenchmarkWriteScanRecord-32  2256619  510.4 ns/op  454 B/op  4 allocs/op
BenchmarkWriteScanRecord-32  2252838  515.2 ns/op  454 B/op  4 allocs/op
BenchmarkWriteScanRecord-32  2339308  522.9 ns/op  454 B/op  4 allocs/op
BenchmarkWriteScanRecord-32  2414358  509.7 ns/op  454 B/op  4 allocs/op
BenchmarkWriteScanRecord-32  2213468  538.4 ns/op  454 B/op  4 allocs/op
BenchmarkWriteScanRecord-32  2401861  525.4 ns/op  454 B/op  4 allocs/op
PASS
```

## Contemporaneous before and after benchmark

The first after run had unstable CPU timing. The benchmarked function did not
change. A same-period comparison used `GOMAXPROCS=1` for both worktrees.

Command for base and branch:

```text
GOMAXPROCS=1 go test -run '^$' -bench '^BenchmarkWriteScanRecord$' -benchmem -count=6 ./pkg/scanapp
```

Base at `55840c3a4eebe70b0fcf04e66f73a95276120de3`:

```text
807.3 ns/op  454 B/op  4 allocs/op
808.3 ns/op  454 B/op  4 allocs/op
559.0 ns/op  454 B/op  4 allocs/op
608.5 ns/op  454 B/op  4 allocs/op
780.8 ns/op  454 B/op  4 allocs/op
783.0 ns/op  454 B/op  4 allocs/op
PASS
```

Branch:

```text
720.1 ns/op  454 B/op  4 allocs/op
556.2 ns/op  454 B/op  4 allocs/op
709.3 ns/op  454 B/op  4 allocs/op
793.3 ns/op  454 B/op  4 allocs/op
792.1 ns/op  454 B/op  4 allocs/op
797.7 ns/op  454 B/op  4 allocs/op
PASS
```

The base median is 783.0 ns/op. The branch median is 792.1 ns/op. The change
is 1.2%. Allocated bytes and allocation counts are unchanged.

## Red tests

### Buffered writer construction

```text
go test ./pkg/writer -run 'TestBufferedCSVWriterPublishesCompleteRecordsOnlyAfterFlush|TestExistingCSVWriterStillFlushesEachRecord'
# github.com/xuxiping/port-scan-mk3/pkg/writer [github.com/xuxiping/port-scan-mk3/pkg/writer.test]
pkg/writer/csv_writer_buffered_test.go:11:7: undefined: NewBufferedCSVWriter
FAIL github.com/xuxiping/port-scan-mk3/pkg/writer [build failed]
FAIL
```

### Performance report output metrics

```text
go test ./internal/perfharness -run '^TestWriteReportsRecordsColdRunAndFiveRunMedian$'
--- FAIL: TestWriteReportsRecordsColdRunAndFiveRunMedian (0.00s)
    report_test.go:68: Markdown report lacks "Output bytes"
FAIL
FAIL github.com/xuxiping/port-scan-mk3/internal/perfharness 0.003s
FAIL
```

### Top-level CLI help

```text
go test ./cmd/port-scan -run '^TestCLIHelp_IncludesRequiredFlags$'
--- FAIL: TestCLIHelp_IncludesRequiredFlags (0.00s)
    main_extra_test.go:61: missing help flag -output-flush-results
FAIL
FAIL github.com/xuxiping/port-scan-mk3/cmd/port-scan 0.003s
FAIL
```

### Performance output-failure scenario

```text
go test ./internal/perfharness -run '^TestRunFailureSmokeExecutesProductionSnapshotAndPressureFailures$'
--- FAIL: TestRunFailureSmokeExecutesProductionSnapshotAndPressureFailures (0.00s)
    workflow_test.go:99: scenario=output-failure: unsupported failure scenario "output-failure"
FAIL
FAIL github.com/xuxiping/port-scan-mk3/internal/perfharness 0.003s
FAIL
```

### Output matrix contract

```text
go test ./internal/perfharness/cmd/perf-harness -run 'TestOutputSpecs|TestApplyOutputThresholds'
internal/perfharness/cmd/perf-harness/main_test.go:92:10: undefined: outputSpecs
internal/perfharness/cmd/perf-harness/main_test.go:103:11: undefined: outputSpecs
internal/perfharness/cmd/perf-harness/main_test.go:119:5: undefined: applyOutputThresholds
FAIL github.com/xuxiping/port-scan-mk3/internal/perfharness/cmd/perf-harness [build failed]
FAIL
```

### Output performance case

```text
go test ./internal/perfharness -run '^TestRunOutputCase'
internal/perfharness/output_workflow_test.go:15:36: perfharness.New().RunOutputCase undefined (type perfharness.Suite has no field or method RunOutputCase)
internal/perfharness/output_workflow_test.go:15:84: undefined: perfharness.OutputSpec
internal/perfharness/output_workflow_test.go:41:30: perfharness.New().RunOutputCase undefined (type perfharness.Suite has no field or method RunOutputCase)
internal/perfharness/output_workflow_test.go:41:78: undefined: perfharness.OutputSpec
FAIL github.com/xuxiping/port-scan-mk3/internal/perfharness [build failed]
FAIL
```

### Batch commit boundary

```text
go test ./pkg/scanapp -run '^TestOutputCommitterCommitsAtTheBoundaryAndFlushesTheFinalBatch$'
pkg/scanapp/output_committer_test.go:37:15: undefined: newOutputCommitter
pkg/scanapp/output_committer_test.go:37:34: undefined: outputCommitterConfig
pkg/scanapp/output_committer_test.go:39:4: unknown field scanPath in struct literal of type batchOutputs
pkg/scanapp/output_committer_test.go:40:4: unknown field openOnlyPath in struct literal of type batchOutputs
FAIL github.com/xuxiping/port-scan-mk3/pkg/scanapp [build failed]
FAIL
```

### Scan flag contract

```text
go test ./pkg/config -run 'TestParseScan(ReturnsDefaults|AcceptsEveryOutputFlushMode|RejectsNegativeOutputFlushBeforeIO)'
pkg/config/scan_config_test.go:38:3: unknown field OutputFlushResults in struct literal of type config.ScanValues
pkg/config/scan_config_test.go:75:20: values.OutputFlushResults undefined (type config.ScanValues has no field or method OutputFlushResults)
FAIL github.com/xuxiping/port-scan-mk3/pkg/config [build failed]
FAIL
```

### Coverage gate after the first complete implementation

```text
GOTOOLCHAIN=go1.24.4 go tool cover -func=coverage.out | tail -n 1
total: (statements) 84.7%
```

The gate requires 85%. I added public writer, scan output, and performance
harness behavior tests. The unchanged gate then reported:

```text
coverage gate passed: 85.0%
```

### Buffered output syscall coalescing

The first full matrix found that interval `1000` was only 1.90 to 1.92 times
as fast as interval `1`. The gate requires at least 2 times. This test exposed
the remaining small writes from the CSV encoder:

```text
GOTOOLCHAIN=go1.24.4 go test ./pkg/writer -run '^TestBufferedCSVWriterCoalescesOneThousandSmallResults$' -count=1
--- FAIL: TestBufferedCSVWriterCoalescesOneThousandSmallResults (0.00s)
    csv_writer_buffered_test.go:59: underlying writes before Flush() = 10, want 0
FAIL
FAIL github.com/xuxiping/port-scan-mk3/pkg/writer 0.002s
FAIL
```

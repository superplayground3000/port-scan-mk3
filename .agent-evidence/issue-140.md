# Issue 140 TDD evidence

## AC2: A basic row port overrides the port file

RED command:

```text
GOTOOLCHAIN=go1.24.4 go test -race ./pkg/scanapp -run '^TestGenerateBuckets_BasicRowPortOverridesPortFile$' -count=1
```

RED result:

```text
--- FAIL: TestGenerateBuckets_BasicRowPortOverridesPortFile (0.00s)
    bucketgen_test.go:193: snapshot ports = [80/tcp], want [443/tcp]
FAIL
FAIL github.com/xuxiping/port-scan-mk3/pkg/scanapp 0.010s
```

The failure proves that bucket generation ignored the basic row port.

GREEN command:

```text
GOTOOLCHAIN=go1.24.4 go test -race ./pkg/scanapp -run '^TestGenerateBuckets_BasicRowPortOverridesPortFile$' -count=1
```

GREEN result:

```text
ok github.com/xuxiping/port-scan-mk3/pkg/scanapp 1.011s
```

## AC4: Basic row ports make the port file optional

RED command:

```text
GOTOOLCHAIN=go1.24.4 go test -race ./pkg/scanapp -run '^TestGenerateBuckets_BasicRowsWithPortsDoNotNeedPortFile$' -count=1
```

RED result:

```text
--- FAIL: TestGenerateBuckets_BasicRowsWithPortsDoNotNeedPortFile (0.00s)
    bucketgen_test.go:289: GenerateBuckets() error = generate-buckets: load inputs: -port-file is required when cidr input is not rich mode
FAIL
FAIL github.com/xuxiping/port-scan-mk3/pkg/scanapp 0.007s
```

GREEN command:

```text
GOTOOLCHAIN=go1.24.4 go test -race ./pkg/scanapp -run '^TestGenerateBuckets_Basic(RowPortOverridesPortFile|MixedRowPortsResumeWithoutCrossProduct|RowsWithPortsDoNotNeedPortFile)$' -count=1
```

GREEN result:

```text
ok github.com/xuxiping/port-scan-mk3/pkg/scanapp 1.013s
```

## AC5: A row without a port source returns a row error

RED command:

```text
GOTOOLCHAIN=go1.24.4 go test -race ./pkg/scanapp -run '^TestGenerateBuckets_BasicRowWithoutAnyPortSourceReturnsRowError$' -count=1
```

RED result:

```text
--- FAIL: TestGenerateBuckets_BasicRowWithoutAnyPortSourceReturnsRowError (0.00s)
    bucketgen_test.go:319: GenerateBuckets() error = generate-buckets: load inputs: -port-file is required when cidr input is not rich mode, want row port-source error
FAIL
FAIL github.com/xuxiping/port-scan-mk3/pkg/scanapp 0.008s
```

GREEN command:

```text
GOTOOLCHAIN=go1.24.4 go test -race ./pkg/scanapp -run '^TestGenerateBuckets_Basic(RowPortOverridesPortFile|MixedRowPortsResumeWithoutCrossProduct|RowsWithPortsDoNotNeedPortFile|RowWithoutAnyPortSourceReturnsRowError)$' -count=1
```

GREEN result:

```text
ok github.com/xuxiping/port-scan-mk3/pkg/scanapp 1.014s
```

## AC8: Snapshot target semantics round-trip

RED command:

```text
GOTOOLCHAIN=go1.24.4 go test -race ./pkg/state -run '^TestSnapshot_TargetSemanticsRoundTripAndLegacyAbsence$' -count=1
```

RED result:

```text
pkg/state/target_semantics_test.go:13:3: unknown field TargetSemanticsVersion in struct literal of type Snapshot
pkg/state/target_semantics_test.go:13:27: undefined: CurrentTargetSemanticsVersion
pkg/state/target_semantics_test.go:14:3: unknown field BasicPortFallback in struct literal of type Snapshot
FAIL github.com/xuxiping/port-scan-mk3/pkg/state [build failed]
```

The compile failure proves that the snapshot had no explicit semantics version or basic fallback metadata.

GREEN command:

```text
GOTOOLCHAIN=go1.24.4 go test -race ./pkg/state -run '^TestSnapshot_TargetSemanticsRoundTripAndLegacyAbsence$' -count=1
```

GREEN result:

```text
ok github.com/xuxiping/port-scan-mk3/pkg/state 1.010s
```

## AC9 and AC10: Legacy row-port snapshot stops before dial

RED command:

```text
GOTOOLCHAIN=go1.24.4 go test -race ./pkg/scanapp -run '^TestRun_WhenLegacyBasicSnapshotCountHidesDifferentRowPortSet_RejectsBeforeDial$' -count=1
```

RED result:

```text
--- FAIL: TestRun_WhenLegacyBasicSnapshotCountHidesDifferentRowPortSet_RejectsBeforeDial (0.00s)
    scan_test.go:871: Run() error = execute scan runtime: resume state references 192.0.2.0/24 ports [80/tcp], which have no scannable targets in the current input; start a fresh scan or run generate-buckets to create a new snapshot, want legacy target-semantics regeneration guidance
FAIL
FAIL github.com/xuxiping/port-scan-mk3/pkg/scanapp 0.008s
```

The failure proves that rejection did not use the required semantics version.

GREEN command:

```text
GOTOOLCHAIN=go1.24.4 go test -race ./pkg/scanapp -run '^TestRun_WhenLegacyBasicSnapshotCountHidesDifferentRowPortSet_RejectsBeforeDial$' -count=1
```

GREEN result:

```text
ok github.com/xuxiping/port-scan-mk3/pkg/scanapp 1.010s
```

## Validate uses the basic row-port contract

RED command:

```text
GOTOOLCHAIN=go1.24.4 go test -race ./pkg/validate -run '^TestInputs_When(AllBasicRowsHavePortsAndPortFileMissing_ReturnsValidResult|BasicRowHasNoPortSource_ReturnsRowError)$' -count=1
```

RED result:

```text
--- FAIL: TestInputs_WhenAllBasicRowsHavePortsAndPortFileMissing_ReturnsValidResult (0.00s)
    service_test.go:156: Inputs() = {Valid:false Detail:-port-file is required when cidr input is not rich mode}, want valid result
--- FAIL: TestInputs_WhenBasicRowHasNoPortSource_ReturnsRowError (0.00s)
    service_test.go:173: Inputs() = {Valid:false Detail:-port-file is required when cidr input is not rich mode}, want row port-source error
FAIL
FAIL github.com/xuxiping/port-scan-mk3/pkg/validate 0.005s
```

GREEN command:

```text
GOTOOLCHAIN=go1.24.4 go test -race ./pkg/validate -run '^TestInputs_When(AllBasicRowsHavePortsAndPortFileMissing_ReturnsValidResult|BasicRowHasNoPortSource_ReturnsRowError)$' -count=1
```

GREEN result:

```text
ok github.com/xuxiping/port-scan-mk3/pkg/validate 1.009s
```

## Snapshot compatibility and recorded fallback

Focused command:

```text
GOTOOLCHAIN=go1.24.4 go test -race ./pkg/scanapp -run '^(TestRun_BasicResumeWithoutPortFile_Succeeds|TestRun_ProducesEnrichedRowsFromRichCSVAndSnapshot|TestChunkBuild_TotalCountMatchesRuntimeExpected_Rich)$' -count=1
```

Result:

```text
ok github.com/xuxiping/port-scan-mk3/pkg/scanapp 1.013s
```

This result covers legacy row-port-free basic resume and unchanged rich behavior.

Recorded-fallback command:

```text
GOTOOLCHAIN=go1.24.4 go test -race ./pkg/scanapp -run '^TestRun_CurrentBasicSnapshotUsesRecordedFallbackWithoutPortFile$' -count=1
```

Result:

```text
ok github.com/xuxiping/port-scan-mk3/pkg/scanapp 1.012s
```

## Performance

Benchmark command for each state:

```text
GOTOOLCHAIN=go1.24.4 go test -run '^$' -bench '^BenchmarkResumeRebuild/resume_runtime_130_of_4000$' -benchmem -count=6 ./pkg/scanapp
```

Base samples at `c108e08c04efa00aef400a0eedae3950582b29f9`:

```text
13451154 ns/op 25062023 B/op 47198 allocs/op
14988314 ns/op 25055195 B/op 47177 allocs/op
15087423 ns/op 25054989 B/op 47174 allocs/op
15323595 ns/op 25054486 B/op 47173 allocs/op
10557163 ns/op 25054646 B/op 47175 allocs/op
11381541 ns/op 25054258 B/op 47171 allocs/op
```

Base medians: `14219734 ns/op`, `25054817.5 B/op`, and `47174.5 allocs/op`.

Initial concrete-task-path samples:

```text
39983943 ns/op 56639056 B/op 396084 allocs/op
38626029 ns/op 56653553 B/op 396010 allocs/op
40763458 ns/op 56622896 B/op 396021 allocs/op
41578225 ns/op 56624058 B/op 396013 allocs/op
36090424 ns/op 56666091 B/op 396012 allocs/op
41004875 ns/op 56622273 B/op 396009 allocs/op
```

Initial medians: `40373700.5 ns/op`, `56631557 B/op`, and `396012.5 allocs/op`.
The initial changes were `+183.93%`, `+126.03%`, and `+739.47%`. This result blocked the implementation.

Final fast-path samples:

```text
13468250 ns/op 26144416 B/op 49227 allocs/op
12831964 ns/op 26142274 B/op 49207 allocs/op
12703290 ns/op 26143920 B/op 49203 allocs/op
12599654 ns/op 26138357 B/op 49204 allocs/op
11942398 ns/op 26142012 B/op 49203 allocs/op
12752965 ns/op 26141292 B/op 49204 allocs/op
```

Final medians: `12728127.5 ns/op`, `26142143 B/op`, and `49204 allocs/op`.
The final changes were `-10.49%`, `+4.34%`, and `+4.30%`. No metric regressed by more than 10%.

## Broad race regression and writing check

```text
GOTOOLCHAIN=go1.24.4 go test -race -shuffle=on ./... -count=1
```

The command exited with code 0. All packages reported `ok`.

The pragmatic Simple English check found no prohibited pattern in added English documentation. The longest added descriptive sentences contain no more than 25 words.

## Final quality gates

`GOTOOLCHAIN=go1.24.4 make verify` exited with code 0:

```text
coverage gate passed: 85.1%

=== RESULT ===
All selected quality gates passed.
```

`COMPOSE_PROJECT_NAME=issue140_rowports_34067ae GOTOOLCHAIN=go1.24.4 make verify-e2e` exited with code 0:

```text
coverage gate passed: 85.2%
PASS
ok github.com/xuxiping/port-scan-mk3/tests/integration 2.725s
e2e report generated at /tmp/port-scan-mk3-issue-140/e2e/out

=== RESULT ===
All selected quality gates passed.
```

## Mixed row ports: resume does not create a Cartesian product

RED command:

```text
GOTOOLCHAIN=go1.24.4 go test -race ./pkg/scanapp -run '^TestGenerateBuckets_BasicMixedRowPortsResumeWithoutCrossProduct$' -count=1
```

RED result:

```text
--- FAIL: TestGenerateBuckets_BasicMixedRowPortsResumeWithoutCrossProduct (0.00s)
    bucketgen_test.go:243: buildRuntimeWithPredicate() error = resume state for 192.0.2.0/24 is incompatible with the current target set (saved total_count=1, now expected=2)
FAIL
FAIL github.com/xuxiping/port-scan-mk3/pkg/scanapp 0.009s
```

The failure proves that resume still rebuilt the old CIDR-by-port-file Cartesian product.

GREEN command:

```text
GOTOOLCHAIN=go1.24.4 go test -race ./pkg/scanapp -run '^TestGenerateBuckets_Basic(RowPortOverridesPortFile|MixedRowPortsResumeWithoutCrossProduct)$' -count=1
```

GREEN result:

```text
ok github.com/xuxiping/port-scan-mk3/pkg/scanapp 1.011s
```

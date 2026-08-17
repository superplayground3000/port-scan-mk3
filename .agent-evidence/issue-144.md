# Issue 144 TDD evidence

## Slice 1: expansion limit module

Command:

```text
go test ./pkg/task -run 'TestEstimate|TestNewExpansion' -count=1
```

Red result before production code:

```text
# github.com/xuxiping/port-scan-mk3/pkg/task [github.com/xuxiping/port-scan-mk3/pkg/task.test]
pkg/task/expansion_limits_test.go:10:19: undefined: EstimateIPSelectors
pkg/task/expansion_limits_test.go:10:41: undefined: SelectorInput
pkg/task/expansion_limits_test.go:10:90: undefined: DefaultExpansionLimits
pkg/task/expansion_limits_test.go:18:11: undefined: EstimateIPSelectors
pkg/task/expansion_limits_test.go:18:33: undefined: SelectorInput
pkg/task/expansion_limits_test.go:18:82: undefined: DefaultExpansionLimits
pkg/task/expansion_limits_test.go:39:19: undefined: EstimateCandidateCounts
pkg/task/expansion_limits_test.go:39:45: undefined: CandidateInput
pkg/task/expansion_limits_test.go:39:109: undefined: DefaultExpansionLimits
pkg/task/expansion_limits_test.go:49:17: undefined: NewExpansionLimits
pkg/task/expansion_limits_test.go:49:17: too many errors
FAIL github.com/xuxiping/port-scan-mk3/pkg/task [build failed]
FAIL
```

## Slice 13: command help

Command:

```text
go test ./cmd/port-scan -run 'TestCLIHelp_IncludesRequiredFlags' -count=1
```

Red result before production code:

```text
--- FAIL: TestCLIHelp_IncludesRequiredFlags (0.00s)
    main_extra_test.go:60: missing help flag -target-count-limit
FAIL
FAIL github.com/xuxiping/port-scan-mk3/cmd/port-scan 0.003s
FAIL
```

## Slice 12: rich precheck authorization count

Command:

```text
go test ./pkg/scanapp -run 'TestEstimateAuthorizedExpansion_Precheck' -count=1
```

Red result before production code:

```text
--- FAIL: TestEstimateAuthorizedExpansion_PrecheckCountsOnlyAuthorizedAddresses (0.00s)
    expansion_limits_test.go:480: authorized precheck candidate count = 0, want 3
FAIL
FAIL github.com/xuxiping/port-scan-mk3/pkg/scanapp 0.002s
FAIL
```

## Slice 11: per-CIDR failure order

Command:

```text
go test ./pkg/task -run 'TestEstimateIPSelectors_StopsAtTheFirstCIDR' -count=1
```

Red result before production code:

```text
--- FAIL: TestEstimateIPSelectors_StopsAtTheFirstCIDRThatCrossesTheLimit (0.00s)
    expansion_limits_test.go:110: EstimateIPSelectors() error = row 3 selector "not-a-selector": invalid selector "not-a-selector": invalid CIDR address: not-a-selector, want first CIDR limit at row 2
FAIL
FAIL github.com/xuxiping/port-scan-mk3/pkg/task 0.002s
FAIL
```

## Performance evidence

Toolchain:

```text
go version go1.24.4 linux/amd64
```

Base command on `integration/125-p1` at `e0b53b2`:

```text
GOTOOLCHAIN=go1.24.4 go test -run '^$' -bench '^BenchmarkExpandIPSelectorsSlash16$' -benchmem -count=6 ./pkg/task
```

Base results:

```text
11840265 ns/op  4794963 B/op  66081 allocs/op
10029508 ns/op  4794790 B/op  66080 allocs/op
10183405 ns/op  4794793 B/op  66080 allocs/op
10827380 ns/op  4794470 B/op  66080 allocs/op
10043238 ns/op  4794788 B/op  66080 allocs/op
10158134 ns/op  4794796 B/op  66080 allocs/op
```

Change command:

```text
GOTOOLCHAIN=go1.24.4 go test -run '^$' -bench '^(BenchmarkExpandIPSelectorsSlash16|BenchmarkEstimateIPSelectorsDefaultRejection)$' -benchmem -count=6 ./pkg/task
```

Changed `/16` results:

```text
11324821 ns/op  4795171 B/op  66087 allocs/op
10491767 ns/op  4794947 B/op  66086 allocs/op
11170112 ns/op  4794757 B/op  66086 allocs/op
11032936 ns/op  4794941 B/op  66086 allocs/op
10336729 ns/op  4794934 B/op  66086 allocs/op
10248594 ns/op  4794939 B/op  66086 allocs/op
```

The median changed from approximately `10.171 ms/op` to `10.762 ms/op`.
This result is approximately `+5.8%`, which is less than the `10%` blocker threshold.
Allocation bytes stayed near `4.795 MB/op`, and allocations increased by six per operation.

Bounded default rejection results:

```text
510.1 ns/op  400 B/op  11 allocs/op
483.1 ns/op  400 B/op  11 allocs/op
749.3 ns/op  400 B/op  11 allocs/op
479.9 ns/op  400 B/op  11 allocs/op
483.3 ns/op  400 B/op  11 allocs/op
475.3 ns/op  400 B/op  11 allocs/op
```

## Slice 10: resume snapshot metadata preservation

Command:

```text
go test ./pkg/scanapp -run 'TestPersistResumeSnapshot_PreservesTargetExpansionState' -count=1
```

Red result before production code:

```text
# github.com/xuxiping/port-scan-mk3/pkg/scanapp [github.com/xuxiping/port-scan-mk3/pkg/scanapp.test]
pkg/scanapp/expansion_limits_test.go:156:3: too many arguments in call to persistResumeSnapshot
    have (string, *scanLogger, []*chunkRuntime, state.PreScanPingState, nil, bool, *state.TargetExpansionState, error, nil)
    want (string, *scanLogger, []*chunkRuntime, state.PreScanPingState, *state.OutputState, bool, error, error)
FAIL github.com/xuxiping/port-scan-mk3/pkg/scanapp [build failed]
FAIL
```

## Slice 9: scan stored limits and incomplete planning

Command:

```text
go test ./pkg/scanapp -run 'TestRunScan_StoredLimit|TestRunScan_CountsOnlyIncomplete' -count=1
```

Red result before production code:

```text
--- FAIL: TestRunScan_StoredLimitStopsBeforeOutputAndExplicitFlagReplacesIt (0.00s)
    expansion_limits_test.go:103: Run(stored limit) error = execute scan runtime: output opener reached, want stored count limit
FAIL
FAIL github.com/xuxiping/port-scan-mk3/pkg/scanapp 0.004s
FAIL
```

## Slice 8: scan limit selection

Command:

```text
go test ./pkg/scanapp -run 'TestEffectiveScanExpansionLimits' -count=1
```

Red result before production code:

```text
# github.com/xuxiping/port-scan-mk3/pkg/scanapp [github.com/xuxiping/port-scan-mk3/pkg/scanapp.test]
pkg/scanapp/expansion_limits_test.go:195:17: undefined: effectiveScanExpansionLimits
pkg/scanapp/expansion_limits_test.go:207:16: undefined: effectiveScanExpansionLimits
pkg/scanapp/expansion_limits_test.go:219:16: undefined: effectiveScanExpansionLimits
pkg/scanapp/expansion_limits_test.go:227:15: undefined: effectiveScanExpansionLimits
FAIL github.com/xuxiping/port-scan-mk3/pkg/scanapp [build failed]
FAIL
```

## Slice 7: generated snapshot metadata

Command:

```text
go test ./pkg/scanapp -run 'TestGenerateBuckets_StoresEffectiveLimits' -count=1
```

Red result before production code:

```text
--- FAIL: TestGenerateBuckets_StoresEffectiveLimitsAndPreFilterCandidateCount (0.00s)
    expansion_limits_test.go:184: snapshot target expansion = nil
FAIL
FAIL github.com/xuxiping/port-scan-mk3/pkg/scanapp 0.009s
FAIL
```

## Slice 6: snapshot target expansion fields

Command:

```text
go test ./pkg/state -run 'TestSnapshot_TargetExpansion' -count=1
```

Red result before production code:

```text
# github.com/xuxiping/port-scan-mk3/pkg/state [github.com/xuxiping/port-scan-mk3/pkg/state.test]
pkg/state/expansion_limits_test.go:11:11: undefined: TargetExpansionState
pkg/state/expansion_limits_test.go:16:40: unknown field TargetExpansion in struct literal of type Snapshot
pkg/state/expansion_limits_test.go:23:9: got.TargetExpansion undefined (type Snapshot has no field or method TargetExpansion)
pkg/state/expansion_limits_test.go:24:52: got.TargetExpansion undefined (type Snapshot has no field or method TargetExpansion)
pkg/state/expansion_limits_test.go:35:12: legacy.TargetExpansion undefined (type Snapshot has no field or method TargetExpansion)
pkg/state/expansion_limits_test.go:36:62: legacy.TargetExpansion undefined (type Snapshot has no field or method TargetExpansion)
FAIL github.com/xuxiping/port-scan-mk3/pkg/state [build failed]
FAIL
```

## Slice 5: pre-ping and bucket workflow limits

Command:

```text
go test ./pkg/scanapp -run 'TestPrePingAndGenerateBuckets_UseExplicitCountLimit' -count=1
```

Red result before workflow production code:

```text
--- FAIL: TestPrePingAndGenerateBuckets_UseExplicitCountLimit (0.00s)
    expansion_limits_test.go:123: RunPrePing() error = <nil>, want explicit count limit
FAIL
FAIL github.com/xuxiping/port-scan-mk3/pkg/scanapp 0.004s
FAIL
```

## Slice 4: authorized workflow counting

Command:

```text
go test ./pkg/scanapp -run 'TestEstimateAuthorizedExpansion' -count=1
```

Red result before production code:

```text
# github.com/xuxiping/port-scan-mk3/pkg/scanapp [github.com/xuxiping/port-scan-mk3/pkg/scanapp.test]
pkg/scanapp/expansion_limits_test.go:31:11: undefined: estimateAuthorizedExpansion
pkg/scanapp/expansion_limits_test.go:75:19: undefined: estimateAuthorizedExpansion
FAIL github.com/xuxiping/port-scan-mk3/pkg/scanapp [build failed]
FAIL
```

## Slice 3: command flag interfaces

Command:

```text
go test ./pkg/config -run 'TestExpansionLimit|TestScanExpansion' -count=1
```

Red result before production code:

```text
# github.com/xuxiping/port-scan-mk3/pkg/config_test [github.com/xuxiping/port-scan-mk3/pkg/config.test]
pkg/config/expansion_limits_test.go:13:35: undefined: config.TargetExpansionValues
pkg/config/expansion_limits_test.go:21:10: cannot use config.ParseValidate(args) (value of struct type config.ValidateConfig) as expansionLimitConfig value in return statement: config.ValidateConfig does not implement expansionLimitConfig (missing method ResolveTargetExpansion)
pkg/config/expansion_limits_test.go:24:10: cannot use config.ParsePrePing(args) (value of struct type config.PrePingConfig) as expansionLimitConfig value in return statement: config.PrePingConfig does not implement expansionLimitConfig (missing method ResolveTargetExpansion)
pkg/config/expansion_limits_test.go:28:10: cannot use config.ParseGenerateBuckets(args) (value of struct type config.GenerateBucketsConfig) as expansionLimitConfig value in return statement: config.GenerateBucketsConfig does not implement expansionLimitConfig (missing method ResolveTargetExpansion)
pkg/config/expansion_limits_test.go:32:10: cannot use config.ParseScan(args) (value of struct type config.ScanConfig) as expansionLimitConfig value in return statement: config.ScanConfig does not implement expansionLimitConfig (missing method ResolveTargetExpansion)
FAIL github.com/xuxiping/port-scan-mk3/pkg/config [build failed]
FAIL
```

## Slice 2: public expansion interface

Command:

```text
go test ./pkg/task -run 'TestExpandIPSelectors' -count=1
```

Red result before production code:

```text
# github.com/xuxiping/port-scan-mk3/pkg/task [github.com/xuxiping/port-scan-mk3/pkg/task.test]
pkg/task/selector_expand_test.go:39:15: undefined: ExpandIPSelectorsWithLimits
pkg/task/selector_expand_test.go:47:14: undefined: ExpandIPSelectorsWithLimits
pkg/task/selector_expand_test.go:61:11: undefined: ExpandIPSelectorsWithLimits
FAIL github.com/xuxiping/port-scan-mk3/pkg/task [build failed]
FAIL
```

## Quality gates

Go 1.24.4 `make verify` final result:

```text
coverage gate passed: 87.5%

=== RESULT ===
All selected quality gates passed.
```

Go 1.24.4 `make verify-e2e` final result:

```text
PASS
ok github.com/xuxiping/port-scan-mk3/tests/integration 2.736s
e2e report generated at /tmp/port-scan-mk3-issue-144/e2e/out

=== RESULT ===
All selected quality gates passed.
```

Windows cross-build result:

```text
/tmp/port-scan-issue-144.exe: PE32+ executable for MS Windows 6.01 (console), x86-64, 15 sections
```

## Independent review

The first fresh-context review approved the Spec axis and blocked the Standards axis.
It found missing same-package estimator tests, incomplete public comments, two help-text semicolons, and a forwarding-only scan adapter.

The review fixes added direct `pkg/task` tests and removed the forwarding adapter.
They also completed the public comments and corrected the help text.
The direct estimator coverage result is:

```text
EstimateAuthorizedCIDRRecords 93.8%
pkg/task total 85.1%
```

The final fresh-context re-review verdicts are:

```text
Standards: APPROVE
Spec: APPROVE
Remaining findings: None
Independent make verify: All selected quality gates passed.
```

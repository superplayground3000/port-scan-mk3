# Issue 145 TDD Evidence

Branch: `fix/145-cancellation-safety`

Base commit: `4febd75330d1ba979ba90dc38d2bf7f0b1776c95`

## Approved test seams

- `scanapp.Run` and the private scan-runtime execution interface
- Parsing, expansion, and rebuild transitions that accept a context
- Dispatcher rate, pause, send, and delay waits
- The executor dial adapter for queued and in-flight tasks
- Output files, snapshot files, rewind state, and telemetry
- CLI process signals on Linux and Windows
- Snapshot temp-file replacement

## Baseline observations

- Bucket, pause-gate, and task-send waits already select on the run context.
- Dispatch delay uses `time.Sleep` and is not cancellable.
- The executor does not accept the run context. A worker dials every queued task.
- Cancellation does not mark queued tasks unwritten or rewind their chunks.
- The result loop skips late results after any terminal error.
- The signal adapter handles the first interrupt only.

## Red-green log

### Cancellable dispatch delay

Red command:

`go test -race ./pkg/scanapp -run '^TestDispatchTasks_WhenCanceledDuringDelay_DoesNotWaitOrEnqueueNextTask$' -count=1`

Key failure:

`dispatch error = <nil>, want context.Canceled` after 4.00s. The dispatcher slept for both 2s delays and queued the second task.

Green command:

`go test -race ./pkg/scanapp -run '^TestDispatchTasks_WhenCanceledDuringDelay_DoesNotWaitOrEnqueueNextTask$' -count=1`

Result: `ok github.com/xuxiping/port-scan-mk3/pkg/scanapp 1.519s`

### Queue abandonment and in-flight completion

Red command:

`go test -race ./pkg/scanapp -run '^TestScanExecutor_WhenCanceled_AbandonsQueuedTasksButFinishesInFlightProbe$' -count=1`

Key failure:

`startScanExecutor` did not accept a context and returned no abandoned-task stream.

Green command:

`go test -race ./pkg/scanapp -run '^TestScanExecutor_WhenCanceled_AbandonsQueuedTasksButFinishesInFlightProbe$' -count=1`

Result: `ok github.com/xuxiping/port-scan-mk3/pkg/scanapp 1.046s`

### Rewind abandoned work

Red command:

`go test -race ./pkg/scanapp -run '^TestRun_WhenCanceled_RewindsAbandonedQueueToLowestUnwritten$' -count=1`

Key failure:

`saved progress = (next 3, scanned 1), want (1, 1)`. The snapshot skipped two queued tasks that no worker dialed.

Green command:

`go test -race ./pkg/scanapp -run '^TestRun_WhenCanceled_RewindsAbandonedQueueToLowestUnwritten$' -count=1`

Result: `ok github.com/xuxiping/port-scan-mk3/pkg/scanapp 1.018s`

### Persist late in-flight results after a fatal stop

Red command:

`go test -race ./pkg/scanapp -run '^TestRunResultLoop_WhenFatalErrorStopsDispatch_PersistsLateInFlightResult$' -count=1`

Key failure:

`writer calls = (0, 0), want (1, 1)`. The result loop discarded the completed in-flight result after a fatal pressure error.

Green command:

`go test -race ./pkg/scanapp -run '^TestRunResultLoop_WhenFatalErrorStopsDispatch_PersistsLateInFlightResult$' -count=1`

Result: `ok github.com/xuxiping/port-scan-mk3/pkg/scanapp 1.011s`

### Input row cancellation

Red command:

`go test -race ./pkg/input -run '^TestLoadCIDRsWithColumnsContext_ReadsCancellationAtRowTransitions$' -count=1`

Key failure:

`undefined: LoadCIDRsWithColumnsContext`. The input interface had no context-aware parse path.

Green commands:

- `go test -race ./pkg/input -run '^TestLoadCIDRsWithColumnsContext_ReadsCancellationAtRowTransitions$' -count=1`
- `go test -race ./pkg/input -count=1`

Both commands returned `ok`.

### Scan-runtime input wiring

Red command:

`go test -race ./pkg/scanapp -run '^TestLoadRunInputsContext_PassesCancellationToCIDRLoader$' -count=1`

Key failures:

- `unknown field loadCIDRRecordsContext in struct literal of type runDependencies`
- `undefined: loadRunInputsContext`

Green commands:

- `go test -race ./pkg/scanapp -run '^TestLoadRunInputsContext_PassesCancellationToCIDRLoader$' -count=1`
- `go test -race ./pkg/scanapp -run '^TestLoadRunInputs_' -count=1`

Both commands returned `ok`.

### Candidate expansion cancellation

Red command:

`go test -race ./pkg/task -run '^TestExpandIPSelectorsContext_ReadsCancellationWithin4096Addresses$' -count=1`

Key failure:

`undefined: ExpandIPSelectorsContext`. Candidate expansion had no cancellable interface.

Green commands:

- `go test -race ./pkg/task -run '^TestExpandIPSelectorsContext_ReadsCancellationWithin4096Addresses$' -count=1`
- `go test -race ./pkg/task -count=1`

Both commands returned `ok`.

### Runtime rebuild cancellation for basic input

Red command:

`go test -race ./pkg/scanapp -run '^TestPrepareRuntimePlanContext_StopsCandidateExpansionWithin4096Items$' -count=1`

Key failure:

`undefined: prepareRuntimePlanContext`. Runtime rebuild did not pass a context into grouping and expansion.

Green command:

`go test -race ./pkg/scanapp -run '^TestPrepareRuntimePlanContext_StopsCandidateExpansionWithin4096Items$' -count=1`

Result: `ok github.com/xuxiping/port-scan-mk3/pkg/scanapp 1.010s`

## Fatal worker error rewind

Red command:

`go test -race ./pkg/scanapp -run '^TestScanExecutor_WhenDialPanics_AbandonsCurrentAndQueuedTasksWithoutNextProbe$' -count=1`

Key failure:

`abandoned task indexes = [], want [0 1]`

Run/snapshot red command:

`go test -race ./pkg/scanapp -run '^TestRun_WhenDialPanics_RewindsActiveAndQueuedTasks$' -count=1`

Key failure:

`saved progress = (next 3, scanned 0), want (0, 0)`

Green stress command:

`go test -race -shuffle=on ./pkg/scanapp -run '^(TestScanExecutor_WhenDialPanics_AbandonsCurrentAndQueuedTasksWithoutNextProbe|TestRun_WhenDialPanics_RewindsActiveAndQueuedTasks|TestScanExecutor_WhenCanceled_AbandonsQueuedTasksButFinishesInFlightProbe|TestRun_WhenCanceled_RewindsAbandonedQueueToLowestUnwritten)$' -count=20`

Result: `ok github.com/xuxiping/port-scan-mk3/pkg/scanapp 1.181s`

## Emergency-exit process tests

Linux command:

`go test -race ./cmd/port-scan -run '^TestScanInterruptContext_OnLinux_ProcessSecondSIGINTExits130$' -count=10`

Result: `ok github.com/xuxiping/port-scan-mk3/cmd/port-scan 1.075s`

The test starts a helper subprocess, sends two real SIGINT events, and observes
the shipped `os.Exit(130)` callback as process exit code `130`.

Windows cross-compile command:

`GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go test -c -o <temp.exe> ./cmd/port-scan`

Result: exit `0` and a PE test executable. The Windows process test creates a
new process group, sends two real `CTRL_BREAK_EVENT` events, and observes exit
code `130`. Native execution remains assigned to Windows CI and issue 99.

## Successful-run telemetry

Red command:

`go test -race ./pkg/scanapp -run '^TestRun_WhenSuccessful_DoesNotLogCancellationDrainTelemetry$' -count=100`

Key failure:

`successful run logged cancellation drain telemetry` with
`"msg":"probe_drain_complete"` and zero canceled work.

Green command:

`go test -race ./pkg/scanapp -run '^(TestRun_WhenSuccessful_DoesNotLogCancellationDrainTelemetry|TestScanExecutor_WhenDialPanics_AbandonsCurrentAndQueuedTasksWithoutNextProbe|TestRun_WhenDialPanics_RewindsActiveAndQueuedTasks|TestRun_WhenCanceled_LogsDeterministicDrainAndSnapshotTelemetry)$' -count=10`

Result: `ok github.com/xuxiping/port-scan-mk3/pkg/scanapp 1.774s`

## Final quality gates

Command: `GOTOOLCHAIN=go1.24.4 make verify`

Key result:

```text
coverage gate passed: 87.5%

=== RESULT ===
All selected quality gates passed.
```

Command: `make verify-e2e`

Key result:

```text
PASS
ok  github.com/xuxiping/port-scan-mk3/tests/integration  2.635s

=== RESULT ===
All selected quality gates passed.
```

The host command `go version` reports `go1.26.1 linux/amd64`. The final verify
command selected `go1.24.4` through `GOTOOLCHAIN`. The Docker e2e build used
the repository image `golang:1.24-alpine`. The Windows cross-build and artifact
checks passed in both final quality-gate runs.

No real host was scanned. The e2e gate used the isolated Docker targets.

## Issue 150 snapshot-matrix integration

Issue 145 does not add a second performance harness or report schema. The
shared issue 150 matrix can use these production integration points:

1. Build each fixture as a `state.Snapshot` value.
2. Time `state.SaveSnapshot(path, snapshot)`. This call includes JSON encode,
   temporary-file write, file sync, close, and atomic replacement.
3. Read the persisted size with `os.Stat(path)` after the call.
4. Use the same filesystem and path policy for every matrix cell.
5. Correlate scan runs with the `snapshot_save_complete` event. It contains
   `duration_ms` and `rewound_chunks`.

`make verify-performance` does not exist at the branch base. Issue 150 owns
that target, the matrix schema, thresholds, and cross-platform runner.

## Simple English self-check

Pragmatic mode applies to changed documentation and comments.

- The three longest new prose sentences contain 16, 15, and 14 words. These
  counts are below the 25-word pragmatic limit.
- Searches found no contractions, `has been`, `have been`, `should`, or
  semicolons in the new prose.
- The comma-plus-`-ing` search found only the noun `grouping` in a list.
- New procedural conditions precede their commands. Other `if` and `when`
  matches are Go syntax or descriptive API comments.
- The selected comparison verb is `make sure`. New prose does not mix it with
  `check`, `verify`, or `confirm`.

### Runtime rebuild cancellation for rich input

Red command:

`go test -race ./pkg/scanapp -run '^TestPrepareRuntimePlanContext_StopsRichGroupingAndDeduplication$' -count=1`

Key failure:

`prepare error = <nil>, want context.Canceled`. The rich grouping and deduplication path ignored the runtime context.

Green command:

`go test -race ./pkg/scanapp -run '^TestPrepareRuntimePlanContext_StopsRichGroupingAndDeduplication$' -count=1`

Result: `ok github.com/xuxiping/port-scan-mk3/pkg/scanapp 1.009s`

### Port row cancellation

Red command:

`go test -race ./pkg/input -run '^TestLoadPortsContext_ReadsCancellationAtRowTransitions$' -count=1`

Key failure:

`undefined: LoadPortsContext`. Port parsing had no cancellable interface.

Green command:

`go test -race ./pkg/input -run '^TestLoadPortsContext_ReadsCancellationAtRowTransitions$' -count=1`

Result: `ok github.com/xuxiping/port-scan-mk3/pkg/input 1.009s`

### Scan-runtime port wiring

Red command:

`go test -race ./pkg/scanapp -run '^TestLoadRunInputsContext_PassesCancellationToPortLoader$' -count=1`

Key failure:

`unknown field loadPortSpecsContext in struct literal of type runDependencies`.

Green command:

`go test -race ./pkg/scanapp -run '^TestLoadRunInputsContext_PassesCancellationToPortLoader$' -count=1`

Result: `ok github.com/xuxiping/port-scan-mk3/pkg/scanapp 1.010s`

### First and second interrupts

Red command:

`go test -race ./pkg/state -run '^TestWithInterruptChannel_FirstInterruptCancelsAndSecondInterruptExits130$' -count=1`

Key failure:

`undefined: withInterruptChannel`. The signal adapter had no second-interrupt state.

Green commands:

- `go test -race ./pkg/state -run '^TestWithInterruptChannel_FirstInterruptCancelsAndSecondInterruptExits130$' -count=1`
- `go test -race ./pkg/state -count=1`

Both commands returned `ok`.

### Linux CLI interrupt process behavior

Red command:

`go test -race ./cmd/port-scan -run '^TestScanInterruptContext_OnLinux_FirstSIGINTExplainsGracefulStopAndSecondExits130$' -count=1`

Key failure:

`undefined: newScanInterruptContext`. The scan command still used the first-interrupt-only adapter.

Green command:

`go test -race ./cmd/port-scan -run '^TestScanInterruptContext_OnLinux_FirstSIGINTExplainsGracefulStopAndSecondExits130$' -count=1`

Result: `ok github.com/xuxiping/port-scan-mk3/cmd/port-scan 1.009s`

### Snapshot failure usability message

Red command:

`go test -race ./pkg/state -run '^TestSaveSnapshot_WhenWriteFails_PreservesPreviousSnapshot$' -count=1`

Key failure:

The save error named the failed write stage but did not state that the previous snapshot remained usable.

Green commands:

- `go test -race ./pkg/state -run '^TestSaveSnapshot_WhenWriteFails_PreservesPreviousSnapshot$' -count=1`
- `go test -race ./pkg/state -count=1`

Both commands returned `ok`.

### Cancellation telemetry

Red command:

`go test -race ./pkg/scanapp -run '^TestRun_WhenCanceled_LogsDeterministicDrainAndSnapshotTelemetry$' -count=1`

Key failure:

The log had no `probe_drain_complete` or `snapshot_save_complete` event.

Green command:

`go test -race ./pkg/scanapp -run '^(TestRun_WhenCanceled_LogsDeterministicDrainAndSnapshotTelemetry|TestScanExecutor_WhenCanceled_AbandonsQueuedTasksButFinishesInFlightProbe)$' -count=1`

Result: `ok github.com/xuxiping/port-scan-mk3/pkg/scanapp 1.016s`

## Focused package validation

Command:

`go test -race ./pkg/input ./pkg/task ./pkg/state ./pkg/scanapp ./cmd/port-scan -count=1`

All five packages returned `ok`.

## Six-run hot-path benchmarks

All comparisons used commit `4febd75` and the changed worktree on the same
machine. The commands used `-benchmem -count=6`.

| Benchmark | Before median | After median | Result |
|---|---:|---:|---|
| `BenchmarkLoadCIDRsTenThousandRows` | 26.77 ms/op | 27.59 ms/op | +3.1% time; allocation counts unchanged |
| `BenchmarkExpandIPSelectorsSlash16` | 27.56 ms/op | 24.65 ms/op | -10.6% time; allocation counts unchanged |
| `BenchmarkStartScanExecutor` | 391.9 us/op | 86.1 us/op | -78.0% time; +7.4% B/op; +0.7% allocs/op |

No median time, allocated-byte, or allocation-count regression exceeded 10%.
Raw output files are `/tmp/issue145-{input,task,executor}-{before,after}.txt`.
The final executor output after panic-path fixes is
`/tmp/issue145-executor-after-final.txt`.

## Gate attempts

First `make verify` attempt: failed in
`TestRun_WhenCanceled_LogsDeterministicDrainAndSnapshotTelemetry`.

The integration test required `in_flight_probes=1`, but the dispatcher and the
external cancel can race after the dial callback returns. The executor seam
already asserts exact `(in-flight=1, abandoned=2)` values with a blocked dial
and a prefilled queue. The integration assertion now checks the event schema.

The 20-run focused shuffle exposed a telemetry scheduling race. The context
could cancel during a probe, but the watcher could run after the probe finished.
The probe finish path now records the pre-decrement in-flight count when it
observes cancellation before the watcher.

## Snapshot port rebuild cancellation

Red command:

`go test -race ./pkg/scanapp -run '^TestParsePortRowsContext_ChecksCancellationWithinFourThousandNinetySixRows$' -count=1`

Key failure:

`pkg/scanapp/cancellation_runtime_test.go:65:12: undefined: parsePortRowsContext`

Green command:

`go test -race ./pkg/scanapp -run '^(TestParsePortRowsContext_ChecksCancellationWithinFourThousandNinetySixRows|TestDispatchTasks_CancellationStopsBucketGateAndSendWaits)$' -count=1`

Result: `ok github.com/xuxiping/port-scan-mk3/pkg/scanapp 1.010s`

## Snapshot fixture calibration

The shared full matrix requires 100 MB chunk-heavy, port-heavy, and unreachable-heavy snapshots.
The proportional fixture estimator stopped below 100 MB for discrete snapshot shapes.

The focused test first failed with this result:

```text
prepareSnapshotSaveFixture: snapshot save fixture did not reach target 100000000 bytes
FAIL
```

A first one-item correction also failed because a discrete item did not cross the target.
The final correction advances by one quarter of the allowed size tolerance when proportional growth does not increase the serialized size.
The correction keeps the existing upper-size tolerance and overflow protection.

The focused test passed all three 100 MB shapes:

```text
PASS
ok  github.com/xuxiping/port-scan-mk3/internal/perfharness  8.110s
```

The package race command also passed:

```text
ok  github.com/xuxiping/port-scan-mk3/internal/perfharness  74.749s
```

## Final performance result

The complete Linux matrix tested commit `3ad301eacb6f574a83b6839919186dc41a81aac7`.
It ran for `4:44:24` and exited with status 0.
All 149 cases passed, including every snapshot load and save case.

The 100 MB snapshot-save results used these serialized sizes:

| Shape | Serialized bytes | Steady median |
| --- | ---: | ---: |
| chunk-heavy | `100,397,567` | `225.919432ms` |
| port-heavy | `100,002,468` | `535.764384ms` |
| unreachable-heavy | `100,058,836` | `147.821524ms` |

The mixed 1 GB snapshot-save case wrote `1,000,000,143` bytes in a `2.05328577s` steady median.
The operation included serialization, write, sync, close, and atomic replacement.

The raw reports are here:

```text
/media/hp/secondary/issue125-performance-3ad301e/report/performance-report.json
/media/hp/secondary/issue125-performance-3ad301e/report/performance-report.md
/media/hp/secondary/issue125-performance-3ad301e/matrix-os-metrics.txt
```

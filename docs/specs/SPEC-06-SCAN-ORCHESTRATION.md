# SPEC-06: Scan Orchestration Specification

## Overview

`pkg/scanapp` owns the scan workflow. The public `Run` function is a small
facade. One private `scanRuntime` owns the lifecycle and error order.

```text
config.ScanConfiguration
        |
        v
scanapp.Run
        | resolve values and create adapters
        v
scanRuntime.execute
        | load -> rebuild -> open -> start -> drain -> save -> report -> close
        v
CSV output and optional resume snapshot
```

The scan command only consumes a bucket snapshot. It does not ping targets or
build fresh chunks.

## 1. Public boundary

```go
type ScanConfiguration interface {
    Resolve() (config.ScanValues, error)
}

func Run(
    ctx context.Context,
    configuration ScanConfiguration,
    stdout io.Writer,
    stderr io.Writer,
    opts RunOptions,
) error
```

`config.ScanConfig` implements `ScanConfiguration`. `Run` resolves it before
file or network work. The command handler does not inspect pressure variants.

`RunOptions` contains optional runtime seams:

```go
type RunOptions struct {
    Dial                DialFunc
    PressureLimit       int
    DisableKeyboard     bool
    PressureSource      PressureSource
    ReachabilityChecker ReachabilityChecker
}
```

`ReachabilityChecker` applies to pre-ping. Scan does not use it.

## 2. Private runtime

`scanRuntime.execute` owns this order:

1. Load input files and the required snapshot.
2. Resolve prior or new output paths.
3. Rebuild incomplete chunk runtimes.
4. Open both output files.
5. Start dashboard, controller, keyboard, and pressure tasks.
6. Start the executor and dispatcher.
7. Drain result, executor, dispatcher, pressure, and abandoned-task channels.
8. Close all per-chunk rate limiters.
9. Rewind each chunk to its lowest unwritten task.
10. If work is incomplete, save the snapshot.
11. Select the final error and emit one completion summary.
12. Cancel background work and close output files.

There is one concrete runtime implementation. The package does not expose an
interface for the runtime, dispatcher, executor, result loop, or filesystem.

## 3. Input and snapshot preparation

The runtime uses a private `inputConfiguration`. It contains only the target
path, optional port path, column names, and missing-port rule.

```go
type inputConfiguration struct {
    cidrFile         string
    cidrIPCol        string
    cidrIPCidrCol    string
    portFile         string
    allowMissingPort bool
}
```

The runtime loads `config.ScanValues.ResumeInput` with `state.LoadSnapshot`.
The snapshot blocklist defines reachability. An empty blocklist makes every
target reachable.

`prepareRuntimePlanContext` rebuilds incomplete chunks with a narrow
`runtimePolicy`.
Completed chunks do not create runtime work.

Parsing and rebuild read cancellation at row and chunk transitions. Expansion,
grouping, and deduplication read cancellation within 4,096 items.

## 4. Output setup

The runtime resolves output paths after it loads the snapshot. A snapshot with
recorded paths reuses them in append mode. A new snapshot gets timestamped
paths.

`openBatchOutputs` opens both final CSV files before worker startup. A new file
gets one header. A resumed file must have the expected header.

The runtime writes each `writer.Record` to the all-results writer. It also
writes open records to the open-only writer. A result is committed only after
all required writes succeed.

## 5. Executor and dispatcher

`startCancellableScanExecutor` creates a worker pool. Cancellation abandons
queued tasks. It does not change the timeout of a started probe.

Each started probe sends one `scanResult`. The worker starts no next probe after
cancellation. The executor reports every abandoned task.

`dispatchTasks` processes chunks in order. For each task it:

1. Gets a rate-limit token.
2. Waits for the pause gate.
3. Sends one task to the executor.
4. Advances the dispatch cursor.
5. Waits for the cancellable configured delay.

The dispatcher goroutine closes the task channel when dispatch ends.

## 6. Result drain and error order

The result loop continues required channel drain after cancellation. It writes
results from started probes unless an output error prevents the write.

The loop keeps the first pressure, executor, or output error. Simultaneous
runtime errors can arrive in either select order.

The final error order is:

1. A snapshot-save error replaces every earlier run error.
2. A runtime error replaces a dispatcher error.
3. A dispatcher error is returned when no higher-priority error exists.

Output close errors are best effort. They do not replace the selected error.

## 7. Resume safety

The dispatcher can advance before a worker starts a queued probe. It can also
advance before an output write completes.

Both cases record the lowest unwritten index for each affected chunk. The
snapshot save rewinds each affected cursor to that index.

This rule can repeat a persisted row. It cannot skip an unwritten task.
Completion counts include only committed rows.

The configured resume path is both the load path and the save path. A clean,
complete run does not save a snapshot.

The first user interrupt starts this flow. A second interrupt forces exit code
`130` without a current-snapshot guarantee.

## 8. Pressure and pause control

`scanapp` owns the one-method `PressureSource` interface. `pkg/pressure` owns
the concrete HTTP and OAuth adapters.

The monitor samples on the configured interval. Pressure at or above the limit
pauses dispatch. Lower pressure resumes dispatch. Three consecutive failures
stop the run. A successful sample resets the failure count.

Manual pause and pressure pause are independent. Any active pause closes the
dispatch gate.

## 9. Observability

Human output on a TTY can show the dashboard. JSON and non-TTY output do not
emit ANSI dashboard control codes.

The runtime emits progress during snapshot rebuild and result processing. It
emits one completion summary for success, cancellation, or failure.

`probe_drain_complete` reports drain time, in-flight probes, and abandoned
tasks. `snapshot_save_complete` reports save time and rewound chunks.

## 10. Extension rules

- Add transport implementations behind `DialFunc` or `PressureSource`.
- Keep orchestration order inside `scanRuntime`.
- Pass narrow policy values to dispatcher and runtime helpers.
- Do not add a shared configuration object.
- Do not add public writer or runtime adapters.

## 11. Main files

| File | Responsibility |
| --- | --- |
| `scan.go` | Public facade and adapter creation |
| `scan_runtime.go` | Lifecycle and error order |
| `input_loader.go` | Input files |
| `runtime_builder.go` | Runtime plan rebuild |
| `executor.go` | TCP worker pool |
| `task_dispatcher.go` | Ordered dispatch and pause gate |
| `result_aggregator.go` | Writes, counters, and event drain |
| `resume_manager.go` | Rewind and snapshot save |
| `pressure_monitor.go` | Pressure polling |
| `output_files.go` | CSV output session |

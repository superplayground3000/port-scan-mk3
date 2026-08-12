# port-scan Design Document

**Tool**: `cmd/port-scan` | **Revised**: 2026-08-11

## Architecture Overview

As of **4.0.0**, `port-scan` is a three-step pipeline. Each subcommand calls a
dedicated `pkg/scanapp` entry point. The pre-ping command uses
`config.ParsePrePing`, which returns an opaque `config.PrePingConfig` value.
The bucket command uses `config.ParseGenerateBuckets`, which returns an opaque
`config.GenerateBucketsConfig` value. The scan command uses `config.ParseScan`,
which returns an opaque `config.ScanConfig` value. The validate command uses
`config.ParseValidate`, which returns an opaque `config.ValidateConfig` value.
Durable files connect the three stages. The `scan` command only consumes a
bucket snapshot.

```
CLI entry point (main.go)
    │
    ├── handleValidateCommand
    │       config.ParseValidate() → config.ValidateConfig
    │           └── validate.Inputs(Configuration) → cli.WriteValidation()
    │
    ├── handlePrePingCommand
    │       config.ParsePrePing() → config.PrePingConfig
    │           └── scanapp.RunPrePing(PrePingConfiguration)
    │           ├── collect unique IPs → reachability checker (platform ping)
    │           ├── progress (stderr) via pkg/progress
    │           └── writer.UnreachableWriter → unreachable_results-<ts>.csv (path → stdout)
    │
    ├── handleGenerateBucketsCommand
    │       config.ParseGenerateBuckets() → config.GenerateBucketsConfig
    │           └── scanapp.GenerateBuckets(GenerateBucketsConfiguration)
    │           ├── load records + ports; parse -unreachable-file blocklist
    │           ├── resolve basic row ports or rich targets → build chunks (parallel over -workers)
    │           ├── stamp pre_scan_ping.enabled=true and timeout_ms=0
    │           └── state.SaveSnapshot(-buckets-out)   (== resume Snapshot JSON)
    │
    └── handleScanCommand → runScan
            config.ParseScan() → config.ScanConfig
                   │
                   ▼
            scanapp.Run(ScanConfiguration)
                   │
                   ├── resolve configuration before file and network work
                   ├── create pressure, dial, and output adapters
                   └── scanRuntime.execute(context.Context)
                           ├── load the snapshot and build chunk runtimes
                           ├── open output files before worker startup
                           ├── start pressure, control, worker, and dispatch tasks
                           ├── drain result and error channels after cancellation
                           ├── write results and rewind unwritten tasks
                           ├── save resume state before final error selection
                           ├── emit one completion summary
                           └── stop background tasks and close output files
```

## Pipeline Stages

### Pre-ping configuration

`scanapp.RunPrePing` accepts the consumer-owned `PrePingConfiguration`
interface. The workflow resolves this interface before file, process, or
network work. `config.PrePingConfig` implements the interface.

`config.NewPrePing` gives tests and non-CLI callers the same input rules as the
parser. An uninitialized `config.PrePingConfig` returns
`config.ErrUninitializedConfiguration`.

### Bucket generation configuration

`scanapp.GenerateBuckets` accepts the consumer-owned
`GenerateBucketsConfiguration` interface. The workflow resolves this
interface before it reads or writes files. `config.GenerateBucketsConfig`
implements the interface.

`config.NewGenerateBuckets` gives tests and non-CLI callers the same input
rules as the parser. An uninitialized `config.GenerateBucketsConfig` returns
`config.ErrUninitializedConfiguration`.

The bucket configuration does not contain a pre-ping timeout. Bucket
generation writes `timeout_ms=0` as explicit snapshot metadata.

### Validate configuration

`validate.Inputs` accepts the consumer-owned `Configuration` interface. The
workflow resolves this interface before it reads an input file.
`config.ValidateConfig` implements the interface.

`config.NewValidate` gives tests and non-CLI callers the same input rules as
the parser. An uninitialized `config.ValidateConfig` returns
`config.ErrUninitializedConfiguration`.

`config.ParseValidate` accepts the complete legacy validate flag surface. It
verifies all legacy values and discards values that the workflow does not use.
This behavior keeps the current CLI contract.

### Scan configuration

`scanapp.Run` accepts the consumer-owned `ScanConfiguration` interface. The
workflow resolves this interface before file or network work.
`config.ScanConfig` implements the interface.

`config.NewScan` gives tests and non-CLI callers the same input rules as the
parser. An uninitialized `config.ScanConfig` returns
`config.ErrUninitializedConfiguration`.

The scan configuration contains one opaque `PressurePolicy`. This policy has a
disabled, simple HTTP, or authenticated OAuth variant. Partial OAuth values
cannot leave `pkg/config`.

The scan parser accepts `-progress-interval` for compatibility. The parsed
value does not cross the scan configuration seam and does not change behavior.

### Scan runtime

`scanapp.Run` is the public facade. It resolves `ScanConfiguration` and creates
the approved runtime adapters. It then creates one private `scanRuntime` and
calls `execute`.

`scanRuntime` is a concrete module inside `pkg/scanapp`. It has no public
interface and no separate package. The module owns preparation, startup,
channel drain, persistence, completion, and shutdown order.

The runtime accepts the existing dial, pressure, terminal, renderer, and
output-fault adapters. It does not add interfaces for the dispatcher,
executor, filesystem, result loop, or output session.

`RunOptions` contains only optional runtime seams. `Dial` replaces TCP dialing.
`PressureSource` replaces pressure sampling. `ReachabilityChecker` applies only
to pre-ping. `PressureLimit`, `DisableKeyboard`, and `ProgressInterval` adjust
runtime control without adding CLI flags.

### Stage 1: Input Loading (`input_loader.go`)

`loadRunInputsContext(ctx, inputConfiguration, deps)` returns
`runInputs{cidrRecords, portSpecs}`. `inputConfiguration` is private and
contains only paths, column names, and the missing-port rule.

1. `readCIDRFileContext` calls `input.LoadCIDRsWithColumnsContext`.
2. `readPortFileContext` calls `input.LoadPortsContext` when a port file is present.

### Stage 2: Plan Building (`runtime_builder.go`, `group_builder.go`, `chunk_lifecycle.go`)

`scanRuntime.execute` loads the required bucket snapshot before it builds the
runtime plan. The scan workflow does not build fresh chunks.

`prepareRuntimePlanContext` rebuilds each incomplete snapshot chunk. Each
`chunkRuntime` contains targets, a state tracker, and a leaky-bucket rate
limiter. A completed chunk does not produce runtime work.

The rebuild reads the context during record filters, expansion, grouping,
deduplication, and target filters. Inner loops read it within 4,096 items.

The snapshot can contain prior output paths. The runtime uses these paths in
append mode. Otherwise, it creates new timestamped paths.

**Target resolution:**

- **Basic mode** (`basic_target_resolution.go`): expands selectors, applies row-port overrides, applies fallback ports, and removes duplicate tasks.
- The basic module groups only IPs with the same port set into one Cartesian chunk. This rule prevents ports from leaking between rows.
- Bucket generation and resume rebuild use the same basic resolution result and exact counts.
- **Rich mode** (`richGroupStrategy`): groups by `dst_network_segment` and keeps global execution-key deduplication.

New snapshots contain `target_semantics_version=1`. Basic snapshots also
contain `basic_port_fallback` for blank row ports. If basic input contains a
row port, the runtime rejects a legacy unversioned snapshot before output or
network work. Run `generate-buckets` to create a new snapshot.

### Stage 3: Output Setup (`batch_output.go`, `output_files.go`)

`openBatchOutputs(scanPath, openPath, appendMode)` writes directly to the final
CSV paths. A new run creates each file and writes its header. A resumed run
validates each existing header and appends rows.

The runtime opens both output files before it starts workers or the dispatcher.
`Finalize` only closes the file handles. A close error does not replace the
selected run error.

### Stage 4: Executor (`executor.go`)

`startCancellableScanExecutor(ctx, workers, timeout, dial, taskCh)` creates a
goroutine pool. The context controls queued work but not a started dial.

```go
for w := 0; w < workers; w++ {
    go func() {
        for task := range taskCh {
            if canceled {
                abandon(task)
                continue
            }
            r := scanner.ScanTCP(dial, task.ip, task.port, timeout)
            result := scanResult{chunkIdx: task.chunkIdx, record: ...}
            resultCh <- result
        }
    }()
}
```

Workers share a bounded task channel. A worker does not start a probe after
cancellation. It reports each abandoned task to the result loop.

An in-flight probe finishes with its original timeout. The executor closes its
result, error, and abandoned-task channels after all workers stop.

### Stage 5: Dispatch (`task_dispatcher.go`)

`dispatchTasks(ctx, policy, ctrl, logger, runtimes, taskCh)`:

- Iterates over runtimes (chunk-serial, index-sequential within each)
- Acquires rate limit token from leaky bucket
- Blocks on pause gate (`<-ctrl.Gate()`)
- Creates `scanTask{chunkIdx, ipCidr, ip, port, meta}` and sends to task channel
- Updates the tracker and waits for the cancellable configured delay

The runtime starts dispatch in one goroutine. This goroutine closes the task
channel after `dispatchTasks` returns.

### Stage 6: Result Aggregation (`result_aggregator.go`)

`runResultLoop` receives from the result, executor-error, dispatcher-error, and
pressure-error channels. It continues required channel drain after
cancellation.

- `writeScanRecord()` — writes to both all-results and open-only CSV writers
- `applyScanResult()` — updates runtime tracker and summary counters
- `emitScanResultEvents()` — logs to stdout/logger at progress intervals

A result is committed only after all required writes succeed. The loop writes
late in-flight results after cancellation or a non-output runtime error.

After an output error, the loop marks each later result as unwritten. It also
marks every abandoned queued task as unwritten.

### Stage 7: Resume (`resume_manager.go`)

`persistResumeSnapshot` rewinds every chunk with unwritten work. It then saves
incomplete, canceled, or fatal work. A clean completed run saves no snapshot.

The configured `-resume` path is the only snapshot path. The runtime loads the
snapshot from this path. It saves updated state to the same path when the run
does not finish cleanly.

The runtime saves the snapshot before it selects the final error. A snapshot
save error replaces a runtime error. A runtime error replaces a dispatcher
error.

The runtime logs `probe_drain_complete` and `snapshot_save_complete` separately.
These events include in-flight, abandoned, rewind, and duration values.

### Stage 8: Finalize (`output_files.go`)

The runtime emits one completion summary after snapshot persistence. It then
stops background tasks and closes the output files through deferred cleanup.
Output close errors remain best-effort.

## Key Data Structures

### scanTarget

```go
type scanTarget struct {
    ip     string
    ipCidr string
    ipU32  uint32
    port   int
    meta   targetMeta
}
```

### scanTask

```go
type scanTask struct {
    chunkIdx int
    taskIdx  int
    ipCidr   string
    ip       string
    port     int
    meta     targetMeta
}
```

### scanResult

```go
type scanResult struct {
    chunkIdx int
    taskIdx  int
    record   writer.Record
}
```

### chunkRuntime

```go
type chunkRuntime struct {
    ipCidr  string
    ports   []int
    targets []scanTarget
    state   *task.Chunk
    tracker *chunkStateTracker
    bkt     *ratelimit.LeakyBucket
}
```

### chunkStateTracker

```go
type chunkStateTracker struct {
    mu             sync.Mutex
    chunk          *task.Chunk
    firstUnwritten int
}
```

The tracker serializes cursor and count updates. `firstUnwritten` is `-1` when
the runtime has no output-failure rewind to apply.

## Rate Control

### Leaky Bucket (`ratelimit/leaky_bucket.go`)

Per-chunk leaky bucket token refill:
- `BucketRate` tokens/second refill
- `BucketCapacity` maximum burst
- `Acquire(ctx)` blocks until a token is available

### Speed Controller (`speedctrl/controller.go`)

Manages pause gate:
- Keyboard `p`/`r` toggles manual pause
- Pressure API toggles API-based pause
- Gate blocks dispatch when either condition is active

## Pressure API Integration

### Adapter module (`pkg/pressure`)

`pkg/pressure` owns the new HTTP and OAuth transport adapters.
`SimpleHTTP` returns one normalized aggregate value. `OAuthMulti` polls all
configured endpoints concurrently and keeps source results in configuration
order. A failed source makes the aggregate value zero and returns an error.

Both constructors require an explicit `http.Client` and valid HTTP endpoints.
Each OAuth endpoint owns a separate token cache and mutex. Each sample returns
a new source-result slice.

`scanapp` owns the one-method `PressureSource` interface. `RunOptions` accepts
an optional source for tests. Otherwise, a private factory creates the adapter
from the validated policy. The command handler does not inspect pressure
variants or create HTTP clients.

### Polling Loop (`pressure_monitor.go`)

```
every PressureInterval:
    sample, err := PressureSource.Sample(ctx)
    record every source result
    if err != nil:
        record failure
        stop after the third consecutive failure
        continue
    update controller with sample.Maximum
    if sample.Maximum >= limit:
        controller.PauseFromAPI()
    else:
        controller.ResumeFromAPI()
```

### Dashboard Telemetry

The pressure monitor sends one `pressurePoll` to
`pressureTelemetryObserver.OnPressurePoll`. The poll contains the sample,
aggregate error, failure count, and sample time. The dashboard records source
results before aggregate status.

The controller observer receives manual and API pause changes separately.

## Dashboard Architecture

### Conditions for Activation

All must be true:
- Command is `scan`
- stderr is a TTY (`term.IsTerminal(stderr)`)
- `-format` is not `json`

### Components

| File | Responsibility |
|------|----------------|
| `dashboard_state.go` | Thread-safe state management with mutex |
| `dashboard_renderer.go` | ANSI escape sequence rendering |
| `dashboard_runtime.go` | Lifecycle (Start/Stop, ticker management) |
| `dashboard_telemetry.go` | Telemetry helpers |

### Rendering

Uses ANSI sequences:
- `\x1b[2J\x1b[H` — clear screen and home cursor
- 500ms refresh interval via `time.Ticker`

## Output Schema

Fixed 14-column schema in `pkg/writer/csv_writer.go`:

```
ip,ip_cidr,port,status,response_time_ms,fab_name,cidr_name,service_label,decision,matched_policy_id,reason,execution_key,src_ip,src_network_segment
```

This is the single source of truth. Changing the schema is a MAJOR version change per the constitution.

## Resume Mechanism

`scan` requires a snapshot through `-resume`. The snapshot contains chunk
progress, pre-ping metadata, and optional output paths. On cancellation or a
fatal error, the runtime writes updated progress to the same file. On the next
run, the runtime rebuilds only incomplete chunks. It reopens recorded output
files in append mode when output paths are present.

## Testing Strategy

- **Unit tests**: Each package has `*_test.go` files
- **Integration tests**: `cmd/*/integration_test.go`
- **e2e tests**: Docker Compose with isolated networks and mock services (`e2e/`)

Quality gates:
- `go test ./...` must pass
- `bash scripts/coverage_gate.sh` must pass with >= 85% coverage
- `bash e2e/run_e2e.sh` must pass for scan pipeline changes

## Resource-limit ownership

The configuration passes `input.CIDRLimits` and `input.PortLimits` to the file adapters.
The input module owns byte and record counting.
This seam keeps the workflow free of parser policy.

The CIDR file adapter counts records with a streaming pass.
It then rewinds the file and allocates the exact result capacity.
The parser converts each CSV row directly to one domain record.
The adapter returns an error if the record count changes between the two passes.
The reader interface cannot rewind and uses incremental result growth.

`state.SnapshotLimits` owns snapshot byte and object counts.
The state module checks a replacement before it creates the temporary file.

`pressure.ResponseLimits` owns HTTP byte limits and OAuth entry counts.
The pressure module decodes OAuth data arrays one entry at a time.

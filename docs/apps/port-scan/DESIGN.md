# port-scan Design Document

**Tool**: `cmd/port-scan` | **Revised**: 2026-08-11

## Architecture Overview

As of **2.0.0**, `port-scan` is a three-step pipeline. Each subcommand calls a
dedicated `pkg/scanapp` entry point. The pre-ping command uses
`config.ParsePrePing`, which returns an opaque `config.PrePingConfig` value.
The bucket command uses `config.ParseGenerateBuckets`, which returns an opaque
`config.GenerateBucketsConfig` value. The scan command uses `config.ParseScan`,
which returns an opaque `config.ScanConfig` value. The validate command still
uses a legacy configuration function. Durable files connect the three stages.
The `scan` command only consumes a bucket snapshot.

```
CLI entry point (main.go)
    │
    ├── handleValidateCommand
    │       config.Parse() → validate.Inputs() → cli.WriteValidation()
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
    │           ├── subtract blocklist; group per CIDR; build chunks (parallel over -workers)
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

### Stage 1: Input Loading (`input_loader.go`)

`loadRunInputs(cfg, deps)` returns `runInputs{cidrRecords, portSpecs}`.

1. `readCIDRFile(cfg.CIDRFile)` → `input.LoadCIDRsWithColumns()` — auto-detects basic vs rich mode
2. `readPortFile(cfg.PortFile)` → `input.LoadPorts()` (if port file provided)

### Stage 2: Plan Building (`runtime_builder.go`, `group_builder.go`, `chunk_lifecycle.go`)

`scanRuntime.execute` loads the required bucket snapshot before it builds the
runtime plan. The scan workflow does not build fresh chunks.

`prepareRuntimePlan` rebuilds each incomplete snapshot chunk. Each
`chunkRuntime` contains targets, a state tracker, and a leaky-bucket rate
limiter. A completed chunk does not produce runtime work.

The snapshot can contain prior output paths. The runtime uses these paths in
append mode. Otherwise, it creates new timestamped paths.

**CIDR Grouping Strategies:**

- **Basic mode** (`basicGroupStrategy`): groups records by `ip_cidr` boundary, expands IP selectors within each CIDR
- **Rich mode** (`richGroupStrategy`): groups by `dst_network_segment`, implements CIDR-scoped rate control with global execution-key deduplication

### Stage 3: Output Setup (`batch_output.go`, `output_files.go`)

`openBatchOutputs(scanPath, openPath, appendMode)` writes directly to the final
CSV paths. A new run creates each file and writes its header. A resumed run
validates each existing header and appends rows.

The runtime opens both output files before it starts workers or the dispatcher.
`Finalize` only closes the file handles. A close error does not replace the
selected run error.

### Stage 4: Executor (`executor.go`)

`startScanExecutor(workers, timeout, dial, taskCh)` creates a goroutine pool:

```go
for w := 0; w < workers; w++ {
    go func() {
        for task := range taskCh {
            r := scanner.ScanTCP(dial, task.ip, task.port, timeout)
            result := scanResult{chunkIdx: task.chunkIdx, record: ...}
            resultCh <- result
        }
    }()
}
```

Workers share a bounded task channel. The executor closes its result and error
channels after all workers stop. The runtime result loop serializes output.

### Stage 5: Dispatch (`task_dispatcher.go`)

`dispatchTasks(ctx, policy, ctrl, logger, runtimes, taskCh)`:

- Iterates over runtimes (chunk-serial, index-sequential within each)
- Acquires rate limit token from leaky bucket
- Blocks on pause gate (`<-ctrl.Gate()`)
- Creates `scanTask{chunkIdx, ipCidr, ip, port, meta}` and sends to task channel
- Updates tracker and applies configured delay

The runtime starts dispatch in one goroutine. This goroutine closes the task
channel after `dispatchTasks` returns.

### Stage 6: Result Aggregation (`result_aggregator.go`)

`runResultLoop` receives from the result, executor-error, dispatcher-error, and
pressure-error channels. It continues required channel drain after
cancellation.

- `writeScanRecord()` — writes to both all-results and open-only CSV writers
- `applyScanResult()` — updates runtime tracker and summary counters
- `emitScanResultEvents()` — logs to stdout/logger at progress intervals

A result is committed only after all required writes succeed. After an output
error, the loop marks each later result as unwritten.

### Stage 7: Resume (`resume_manager.go`)

`persistResumeSnapshot` rewinds each affected chunk after an output error. It
then saves incomplete work, canceled work, or fatal work. A clean completed run
does not save a snapshot.

The runtime saves the snapshot before it selects the final error. A snapshot
save error replaces a runtime error. A runtime error replaces a dispatcher
error.

### Stage 8: Finalize (`output_files.go`)

The runtime emits one completion summary after snapshot persistence. It then
stops background tasks and closes the output files through deferred cleanup.
Output close errors remain best-effort.

## Key Data Structures

### scanTarget

```go
type scanTarget struct {
    ip       string
    ipCidr   *net.IPNet
    port     int
    meta     writer.Record  // Rich metadata
}
```

### scanTask

```go
type scanTask struct {
    chunkIdx int
    ipCidr   string
    ip       string
    port     int
    meta     writer.Record
}
```

### scanResult

```go
type scanResult struct {
    chunkIdx int
    record   writer.Record
}
```

### chunkRuntime

```go
type chunkRuntime struct {
    targets []scanTarget
    ports   []int
    tracker *chunkStateTracker
    bkt     *ratelimit.LeakyBucket
}
```

### chunkStateTracker

```go
type chunkStateTracker struct {
    NextIndex    int
    ScannedCount int
    TotalCount   int
    Status       string  // "pending" | "in_progress" | "completed"
}
```

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

On SIGINT or error, `resume_state.json` is written containing:
- Chunk CIDR
- NextIndex (next task to execute)
- ScannedCount
- Status

On resume: chunks are loaded, tracker `NextIndex` is restored, dispatch picks up where it left off.

## Testing Strategy

- **Unit tests**: Each package has `*_test.go` files
- **Integration tests**: `cmd/*/integration_test.go`
- **e2e tests**: Docker Compose with isolated networks and mock services (`e2e/`)

Quality gates:
- `go test ./...` must pass
- `bash scripts/coverage_gate.sh` must pass with >= 85% coverage
- `bash e2e/run_e2e.sh` must pass for scan pipeline changes

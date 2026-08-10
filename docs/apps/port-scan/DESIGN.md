# port-scan Design Document

**Tool**: `cmd/port-scan` | **Revised**: 2026-08-10

## Architecture Overview

As of **2.0.0**, `port-scan` is a three-step pipeline. Each subcommand calls a
dedicated `pkg/scanapp` entry point. The pre-ping command uses
`config.ParsePrePing`, which returns an opaque `config.PrePingConfig` value.
The bucket command uses `config.ParseGenerateBuckets`, which returns an opaque
`config.GenerateBucketsConfig` value. The other commands still use legacy
configuration functions during the active migration. Durable files connect
the three stages. The `scan` command only consumes a bucket snapshot.

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
            ParseFor("scan")   (-resume REQUIRED; no ping flags)
                   │
                   ▼
            scanapp.Run()   (loads the bucket snapshot; NO reachability checker)
                   │
                   ├── reachable predicate from snapshot blocklist (never pings)
                   ├── build group runtimes with leaky-bucket rate control
                   ├── start pressure API poller (unless -disable-api)
                   ├── start scan executor (N workers, net.Dialer)
                   │       └── net.DialTimeout → scan result
                   ├── task dispatcher (flow control via speedctrl.Controller)
                   ├── result writer goroutine
                   │       ├── writer.CSVWriter → scan_results-<ts>.csv (all results)
                   │       └── writer.OpenOnlyWriter → opened_results-<ts>.csv (open only)
                   ├── dashboard runtime (TTY only)
                   └── resume state persister (on SIGINT or error)
                           └── updates the -resume bucket snapshot in place
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

### Stage 1: Input Loading (`input_loader.go`)

`loadRunInputs(cfg config.Config)` returns `runInputs{cidrRecords, portSpecs}`.

1. `readCIDRFile(cfg.CIDRFile)` → `input.LoadCIDRsWithColumns()` — auto-detects basic vs rich mode
2. `readPortFile(cfg.PortFile)` → `input.LoadPorts()` (if port file provided)

### Stage 2: Plan Building (`runtime_builder.go`, `group_builder.go`, `chunk_lifecycle.go`)

`prepareRunPlan(cfg, inputs, deps, now)`:

1. **Load or build chunks** — if resume path provided, load from JSON; otherwise build from input records
2. **Build runtimes** — create `chunkRuntime` per CIDR with state tracker and leaky-bucket rate limiter
3. **Resolve output paths** — generate timestamped file names

**CIDR Grouping Strategies:**

- **Basic mode** (`basicGroupStrategy`): groups records by `ip_cidr` boundary, expands IP selectors within each CIDR
- **Rich mode** (`richGroupStrategy`): groups by `dst_network_segment`, implements CIDR-scoped rate control with global execution-key deduplication

### Stage 3: Output Setup (`batch_output.go`, `output_files.go`)

`openBatchOutputs(scanPath, openedPath)` creates `.tmp` files:
- `scan_results-...tmp` (all results)
- `opened_results-...tmp` (open ports only)

On `Finalize(success)`:
- Success: rename `.tmp` → final names
- Failure: keep `.tmp` files for debugging

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

Workers share a bounded task channel; results are serialized by a single writer goroutine.

### Stage 5: Dispatch (`task_dispatcher.go`)

`dispatchTasks(ctx, policy, ctrl, logger, runtimes, taskCh)`:

- Iterates over runtimes (chunk-serial, index-sequential within each)
- Acquires rate limit token from leaky bucket
- Blocks on pause gate (`<-ctrl.Gate()`)
- Creates `scanTask{chunkIdx, ipCidr, ip, port, meta}` and sends to task channel
- Updates tracker and applies configured delay

### Stage 6: Result Aggregation (`result_aggregator.go`)

Main event loop receives from `resultCh`:
- `writeScanRecord()` — writes to both all-results and open-only CSV writers
- `applyScanResult()` — updates runtime tracker and summary counters
- `emitScanResultEvents()` — logs to stdout/logger at progress intervals

### Stage 7: Resume (`resume_manager.go`)

`persistResumeState(cfg, opts, logger, runtimes, dispatchErr, runErr)`:

Saves chunk states (NextIndex, ScannedCount, Status) when:
- Incomplete (some tasks not done) AND
- (Error occurred OR shouldSaveOnDispatchErr)

### Stage 8: Finalize (`output_files.go`)

`outputs.Finalize(success)` renames or preserves `.tmp` files.

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

The scan monitor still uses the legacy `PressureFetcher` seam during the active
configuration migration. The new adapters do not change current CLI behavior.

### Polling Loop (`pressure_monitor.go`)

```
every PressureInterval:
    pressure, err := PressureFetcher.Fetch(ctx)
    if err != nil:
        record failure
        stop after the third consecutive failure
        continue
    update controller with pressure value
    if pressure >= limit:
        controller.PauseFromAPI()
    else:
        controller.ResumeFromAPI()
```

### Dashboard Telemetry

Pressure monitor observes:
- `pressureTelemetryObserver.OnPressureSample(percent, time)`
- `pressureTelemetryObserver.OnPressureFailure(streak, time)`
- Per-source samples and failures when the fetcher supplies source results
- `controllerTelemetryObserver.OnControllerStatusChange(status)`

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

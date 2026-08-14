# port-scan Specification

**Tool**: `cmd/port-scan` | **Revised**: 2026-08-12

## Overview

`port-scan` is a TCP port scanner for IPv4 that supports two input modes (basic
and rich), pressure-aware pacing via a configurable API, resumable scans, and
real-time dashboard display.

As of **2.0.0** it is a **three-step pipeline**. Durable file hand-offs connect
`pre-ping`, `generate-buckets`, and `scan`. Each pipeline command uses a
dedicated configuration parser. The validate command uses
`config.ParseValidate` and an opaque configuration value. `scan` requires a
bucket snapshot through `-resume`. It constructs no
reachability checker and never pings. See the
[2.0.0 release notes](../../release-notes/2.0.0.md) for the flag relocation
table and migration.

## Commands

### pre-ping

Pings unique target IPs and writes the `unreachable_results-<ts>.csv` blocklist,
printing its resolved path to stdout for chaining. Reports percentage progress
to stderr.

```bash
port-scan pre-ping -cidr-file <path> [-pre-scan-ping-timeout 100ms] [-workers N] \
  [-output <path>] [-progress-interval N] [-cidr-ip-col ip] [-cidr-ip-cidr-col ip_cidr] \
  [-log-level info] [-format human|json] [-quiet]
```

No `-port-file` and no ping-toggle flag (skip pinging by skipping this step).

### generate-buckets

Builds the resume bucket snapshot over targets minus an optional blocklist and
writes it to `-buckets-out`. Performs no network I/O. Parallelized over
`-workers` with deterministic, CIDR-sorted output; always stamps
`pre_scan_ping.enabled=true`.

```bash
port-scan generate-buckets -cidr-file <path> -buckets-out <path> [-port-file <path>] \
  [-unreachable-file <path>] [-workers N] [-progress-interval N] \
  [-cidr-ip-col ip] [-cidr-ip-cidr-col ip_cidr] [-log-level info] [-format human|json] [-quiet]
```

### scan

Runs the pure TCP scan of a bucket snapshot. **`-resume` is required**; it reads
the snapshot at start and, on cancel/error, writes progress back in place at the
same path. No ping flags are registered.

```bash
port-scan scan -cidr-file <path> -resume <bucket-file> [-output <path>] [flags...]
```

### validate

Parses and validates input files without performing any network scan.

```bash
port-scan validate -cidr-file <path> [-port-file <path>] [-format human|json]
```

**Exit codes (all commands):**
- `0` — success (inputs valid / snapshot written / scan completed)
- `1` — runtime error (file write failure, config error during run, validation failure)
- `2` — CLI or config error (missing required flags, unknown flag, invalid value)
- `130` — canceled by SIGINT after successful snapshot persistence

### Scan cancellation

The first Ctrl+C or Windows Ctrl+Break starts graceful cancellation. Parent
context cancellation and fatal runtime errors use the same stop flow.

Parsing and runtime rebuild read the context at row and chunk transitions.
Inner expansion and deduplication loops read it within 4,096 items.

Rate, pause, send, and `-delay` waits stop after cancellation. Queued probes do
not start. Each started probe finishes with its original `-timeout`.

The result loop persists every completed in-flight result. It abandons queued
tasks and rewinds each chunk to its lowest unwritten index.

A resumed run can repeat a persisted row after a rewind. It cannot skip an
unwritten task.

The second Ctrl+C or Ctrl+Break forces exit code `130`. This exit does not
promise a current snapshot or finalized output handles.

A snapshot-save error replaces cancellation and returns exit code `1`. The
error states whether the previous snapshot remains usable.

The scan commits a result batch only after both result writers flush. Progress
and summaries include only committed results.

A controlled stop flushes the final batch. A write or flush failure rewinds
the current batch to its earliest pending task in each chunk.

Each `scan_result` event includes `output_state=pending` and `batch_id`. The
runtime emits `output_batch_committed` or `output_batch_failed` for each batch.

The `output_batch_summary` event reports the interval, batch counts, maximum
batch size, and total flush time.

## CLI Flags

The three pipeline commands register only their workflow flags. A foreign flag
is an unknown-flag error. `validate` is a compatibility exception. It accepts
and verifies the complete 30-flag surface of the removed shared parser.

The table shows the pipeline ownership of each flag. `validate` accepts all
listed flags except `-progress-interval`, `-unreachable-file`, and
`-buckets-out`. Required flags are `-cidr-file` (all), `-buckets-out`
(`generate-buckets`), and `-resume` (`scan`). See
[All flags](../../cli/flags.md) for per-command tables.

| Flag | Default | Description |
|------|---------|-------------|
| `-cidr-file` | (required) | All commands. Path to the CIDR/rich input CSV. |
| `-cidr-ip-col` / `-cidr-ip-cidr-col` | `ip` / `ip_cidr` | All commands. Case-sensitive column mapping. |
| `-target-count-limit` | `10000000` | All commands. Maximum candidate addresses. `0` disables this limit. |
| `-target-memory-limit-gb` | `16` | All commands. Target expansion budget in decimal GB. `0` disables this limit. |
| `-cidr-input-size-limit-gb` | `1` | All commands. Maximum CIDR input size in decimal GB. |
| `-cidr-input-record-limit` | `10000000` | All commands. Maximum CIDR data records. |
| `-port-input-size-limit-mb` | `1` | `validate`, `generate-buckets`, and `scan`. Maximum port input size in decimal MB. |
| `-port-input-record-limit` | `65535` | `validate`, `generate-buckets`, and `scan`. Maximum nonblank port records. |
| `-snapshot-size-limit-gb` | `2` | `generate-buckets` and `scan`. Maximum snapshot load and save size in decimal GB. |
| `-snapshot-chunk-limit` | `10000000` | `generate-buckets` and `scan`. Maximum snapshot chunks. |
| `-snapshot-port-entry-limit` | `10000000` | `generate-buckets` and `scan`. Maximum port entries across all chunks. |
| `-snapshot-unreachable-ip-limit` | `10000000` | `generate-buckets` and `scan`. Maximum unreachable IPs. |
| `-pressure-response-size-limit-mb` | `1` | `scan` only. Maximum size of each pressure response in decimal MB. |
| `-pressure-response-entry-limit` | `10000` | `scan` only. Maximum entries in each OAuth data array. |
| `-pre-scan-ping-timeout` | `100ms` | `pre-ping` only. Ping reply-wait timeout (must be > 0). Removed from `scan`. |
| `-unreachable-file` | (empty) | `generate-buckets` only, optional. Blocklist CSV (a `pre-ping` output) whose `ip` column is subtracted. |
| `-buckets-out` | (required) | `generate-buckets` only. Output path for the bucket snapshot. |
| `-resume` | (required) | `scan` only. Bucket snapshot to scan; updated in place on cancel/error. |
| `-progress-interval` | `100` | `pre-ping`, `generate-buckets`, `scan`. The scan parser accepts this compatibility flag but does not use its value. |
| `-port-file` | (basic mode) | `generate-buckets` (primary; required in basic mode, ignored in rich mode) and `scan` (fallback, normally ignored — chunks carry ports). |
| `-output` | `scan_results.csv` | `pre-ping` (unreachable CSV dir/anchor) and `scan` (`scan_results-<ts>.csv` / `opened_results-<ts>.csv` dir/anchor). `generate-buckets` uses `-buckets-out`. |
| `-output-flush-results` | `1000` | `scan` only. Probe results per output batch. `1` flushes each result. `0` disables periodic flushes. Positive values have no fixed maximum. |
| `-timeout` | `100ms` | `scan` only. Per-scan TCP connection timeout (Go duration string). |
| `-delay` | `10ms` | `scan` only. Pause between dispatching consecutive tasks. |
| `-bucket-rate` | `100` | `scan` only. Leaky-bucket token refill rate (tokens/second) |
| `-bucket-capacity` | `100` | `scan` only. Leaky-bucket maximum burst size |
| `-workers` | `10` | `pre-ping`, `generate-buckets`, `scan`. Concurrent workers (also parallelizes bucket generation) |
| `-pressure-api` | `http://localhost:8080/api/pressure` | `scan` only. URL of the pressure API endpoint. |
| `-disable-api` | `false` | `scan` only. Disable pressure API polling; use only local rate control |
| `-pressure-interval` | `5s` | `scan` only. Pressure poll interval (Go duration string or integer seconds) |
| `-pressure-auth-url` | (empty) | `scan` only. OAuth token endpoint URL (required with `-pressure-use-auth`) |
| `-pressure-data-url` | (empty) | `scan` only. Comma-separated pressure data endpoint URLs (required with `-pressure-use-auth`) |
| `-pressure-client-id` | (empty) | `scan` only. OAuth client ID (required with `-pressure-use-auth`) |
| `-pressure-client-secret` | (empty) | `scan` only. OAuth client secret (required with `-pressure-use-auth`) |
| `-pressure-use-auth` | `false` | `scan` only. Use OAuth-authenticated pressure fetcher |
| `-log-level` | `info` | All commands. Log verbosity: `debug`, `info`, or `error` |
| `-format` | `human` | All commands. `human` or `json`. |
| `-quiet` | `false` | All commands. Suppress console logs; keep pressure API logs |

## Input Formats

### Basic Mode: CIDR CSV + Port File

**CIDR CSV** — one row per IP selector:

| Column | Required | Description |
|--------|----------|-------------|
| `ip` | Yes | IP selector — single IPv4 address or CIDR range |
| `ip_cidr` | Yes | Boundary CIDR containing the selector |
| `fab_name` | No | Fabric/mesh name carried through to output |
| `cidr_name` | No | Human-readable CIDR label carried through to output |
| `port` | No | Pre-specified port number on this row (otherwise uses port-file) |

**Port file** — one `port/tcp` per line:

```
80/tcp
443/tcp
8080/tcp
```

### Rich Mode: Firewall-Policy CSV

Auto-detected when all required columns are present. No port file needed.

| Column | Required | Description |
|--------|----------|-------------|
| `src_ip` | Yes | Source IPv4 address |
| `src_network_segment` | Yes | Source network boundary CIDR containing src_ip |
| `dst_ip` | Yes | Destination IPv4 address (scan target) |
| `dst_network_segment` | Yes | Destination network boundary CIDR containing dst_ip |
| `service_label` | Yes | Service identifier |
| `protocol` | Yes | Protocol (only `tcp` accepted) |
| `port` | Yes | TCP port number (1–65535) |
| `decision` | Yes | `accept` (scan target) or `deny` (skip) |
| `matched_policy_id` | Yes | Policy rule identifier |
| `reason` | Yes | Policy match reason |

Rows with `decision=accept` become scan targets. Rows with `decision=deny` never become network targets.

A deny row overrides an accept row with the same normalized `execution_key`. This rule applies before ICMP, bucket generation, resume rebuild, and TCP dispatch.

The `validate` command rejects malformed deny rows. A deny-only input succeeds with no probes, an empty snapshot, header-only results, and a zero-task summary.

Generated snapshots record that rich deny rows were excluded. If an unmarked snapshot has rich deny input, `scan` stops before TCP dispatch.

### CIDR Expansion in Rich Mode

Rich mode performs reason-aware IP expansion:
- If `reason` is `PRECHECK_ALLOW_ALL`: entire `dst_network_segment` is expanded
- If `reason` is `MATCH_POLICY_ACCEPT`: only `dst_ip` is scanned
- Otherwise: only `dst_ip` is scanned

### Target Expansion Limits

All commands verify the full expansion before ping, dial, output creation, or snapshot write.
`validate` calculates the values without target enumeration.

The candidate count includes every authorized input row before de-duplication, broadcast removal, and blocklist filtering.
Repeated and overlapping selectors count again. Rich deny rows contribute zero candidates.

The default candidate limit is `10000000`. The default memory limit is `16` decimal GB.
The memory estimate is `1000000000 + candidate count * 1500` bytes.
Thus, `10000000` candidates equal `16` GB in this estimate.

An IPv4 `/9` passes the default limits. An IPv4 `/8` does not pass them.
A positive flag value replaces its default. A value of `0` disables that limit.

CAUTION: If both flags are `0`, expansion continues without a hidden limit.
Available memory, address space, and operating-system policy then limit the command.

The `pkg/task` expansion-limits module owns counting, estimation, overflow checks, and limit errors.
Configuration parsers adapt CLI values to this module.
Workflows supply authorized rows and do not copy the limit rules.

### Data Resource Limits

Each parser or decoder owns its data policy.
Workflow configuration supplies verified values to that module.

File adapters use metadata for an early size rejection.
Bounded readers also reject the first byte more than the limit.

The CIDR record count excludes the header and blank records.
The port record count includes duplicate nonblank records.

Snapshot loading and saving use the same effective limits.
A failed replacement keeps the previous snapshot unchanged.
A failed new save leaves no snapshot file.

Pressure adapters apply the byte limit to each HTTP response.
They use `Content-Length` for an early rejection and enforce the stream limit.
The OAuth data decoder processes entries incrementally.

A positive flag value replaces its default.
A value of `0` disables only that limit.

CAUTION: A disabled limit can exhaust memory or terminate the process.

## Output Formats

### scan_results-*.csv

All scan results (open, close, timeout). Fixed 14-column schema:

```
ip,ip_cidr,port,status,response_time_ms,fab_name,cidr_name,service_label,decision,matched_policy_id,reason,execution_key,src_ip,src_network_segment
```

| Column | Description |
|--------|-------------|
| `ip` | Scanned IP address |
| `ip_cidr` | Boundary CIDR |
| `port` | TCP port number |
| `status` | `open`, `close`, `close(timeout)`, `error(local)` (the scanning host failed — port state unknown) or `unknown` (indeterminate transport error) |
| `response_time_ms` | TCP connection latency in milliseconds |
| `fab_name` | Fabric name from CIDR input |
| `cidr_name` | CIDR label from CIDR input |
| `service_label` | Service label (rich mode only) |
| `decision` | Policy decision (rich mode only) |
| `matched_policy_id` | Policy ID (rich mode only) |
| `reason` | Policy reason (rich mode only) |
| `execution_key` | Dedup key in `ip:port/tcp` form (rich mode only) |
| `src_ip` | Source IP from rich input (rich mode only) |
| `src_network_segment` | Source network boundary (rich mode only) |

Basic mode rows carry `ip`, `ip_cidr`, `port`, `status`, `response_time_ms`, `fab_name`, `cidr_name`; all other columns are empty.

### opened_results-*.csv

Same schema as `scan_results-*.csv`, filtered to `status=open` only.

### Bucket snapshot (resume state)

The bucket snapshot produced by `generate-buckets` (`-buckets-out`) **is** the
resume state JSON. `scan` reads it via `-resume`; on SIGINT or scan failure it
writes progress back **in place at the same `-resume` path**. It contains the
per-CIDR chunk states plus the `pre_scan_ping` envelope (which carries the
unreachable blocklist so `scan` never needs to ping).

```json
{
  "chunks": [
    {"cidr": "10.0.0.0/24", "ports": ["8080/tcp"], "next_index": 50, "scanned_count": 50, "total_count": 254, "status": "in_progress"}
  ],
  "pre_scan_ping": {"enabled": true, "timeout_ms": 0, "unreachable_ipv4_u32": [168430081]},
  "target_expansion": {"candidate_count": 256, "candidate_limit": 10000000, "memory_limit_gb": 16}
}
```

`generate-buckets` stores the count before broadcast and blocklist filtering.
On resume, `scan` counts only incomplete chunks.

If scan limit flags are absent, `scan` uses the stored limits.
Each explicit scan flag replaces its related stored limit.
A legacy snapshot has no `target_expansion` object and uses the defaults.

## Pressure API Contract

### Plain HTTP pressure

`pkg/pressure.SimpleHTTP` makes an unauthenticated GET request. It expects this
JSON response:

```json
{"pressure": 45.0}
```

### OAuth pressure

`pkg/pressure.OAuthMulti` obtains separate bearer tokens for the configured
data endpoints. Each data response is an array:

```json
[{"data": {"Percent": 45.0}}, {"data": {"Percent": 30.0}}]
```

Each source returns the maximum `Percent` value across its entries.
Every present `Percent` value must be finite. One non-finite value makes its source and the complete poll fail.

### Multi-source result

`OAuthMulti` polls all data endpoints concurrently. It returns source results
in configuration order. A successful sample returns the maximum pressure
across all sources. If one source fails, the aggregate value is zero and the
sample returns an error with all source results.

The scan monitor uses the consumer-owned `PressureSource` seam. A private
factory maps the opaque pressure policy to a `pkg/pressure` adapter.

### Pressure Response Parsing

The `pressure` field accepts a number or a numeric string. Finite values are normalized to one decimal place when this calculation cannot overflow.

`NaN`, positive infinity, and negative infinity are errors. This rule applies to all letter cases and to values from custom `PressureSource` implementations.

Finite zero, negative values, and values more than 100 remain valid.

### Pressure Failure Streak

The monitor uses one failure streak for all pressure errors. The first two failures do not change the pressure-control state.

After the third consecutive failure, the scan uses the existing fatal pressure-error path. One complete successful poll resets the overall streak.

Each OAuth source has an independent health streak. A source failure retains its last finite dashboard value and does not display a non-finite percentage.

### Pause Behavior

When pressure meets or exceeds `PressureLimit` (default 60%), the scanner
pauses dispatch until pressure drops below the threshold.

## Dashboard

The dashboard provides real-time terminal UI when all conditions are met:
- Command is `scan`
- stderr is a TTY
- `-format` is not `json`

Disabled when any condition fails.

**Display sections:**
- Progress: `Progress: X/Y (Z%)`
- Current CIDR: `Current CIDR: X.X.X.X/XX`
- Bucket status: `Bucket: waiting_bucket | waiting_gate | enqueued`
- Dispatch rate: `Dispatch/s: X.XX`
- Results rate: `Results/s: X.XX`
- Controller status: `Controller: RUNNING | PAUSED(API) | PAUSED(MANUAL) | PAUSED(API+MANUAL)`
- API pressure: `API Pressure: XX%`
- Last update: `Last Update: ISO8601`
- API health: `Health: ok | fail streak N`
- Per-source health (multi-source): `API Sources: src1=XX% ok | src2=- fail streak 1`

Refresh interval: 500ms.

## File Structure

```
cmd/port-scan/
├── main.go              # Entry point, command routing
├── command_handlers.go  # validate/scan command handlers
├── main_test.go
├── main_scan_test.go
├── main_extra_test.go
└── test_helpers_test.go

pkg/pressure/
└── pressure.go                 # HTTP and OAuth pressure adapters

pkg/scanapp/
├── scan.go                    # Main orchestration (Run)
├── executor.go                # Worker pool
├── task_dispatcher.go         # Task dispatch with rate limiting
├── resume_manager.go          # Resume state persistence
├── input_loader.go            # Input loading
├── runtime_types.go           # Core data structures
├── runtime_builder.go         # Run plan composition
├── group_builder.go           # CIDR grouping (basic/rich strategies)
├── chunk_lifecycle.go         # Chunk management
├── result_aggregator.go       # Result processing
├── batch_output.go            # Batch output paths
├── output_files.go            # Output file management
├── scan_helpers.go            # Helper functions
├── scan_logger.go             # Logging
├── dispatch_observer.go       # Dispatch events
├── pressure_monitor.go        # Pressure API polling
├── pressure_source.go         # Validated policy to pressure adapter
├── dashboard_state.go         # Dashboard state management
├── dashboard_renderer.go      # ANSI rendering
├── dashboard_runtime.go       # Lifecycle management
├── dashboard_telemetry.go     # Telemetry helpers
├── record_mapper.go           # Record mapping
├── record_writer.go           # Record writing
└── fdlimit_*.go               # File descriptor limit handling
```

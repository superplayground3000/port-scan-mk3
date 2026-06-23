# port-scan Specification

**Tool**: `cmd/port-scan` | **Revised**: 2026-05-03

## Overview

`port-scan` is a TCP port scanner for IPv4 that supports two input modes (basic and rich), pressure-aware pacing via a configurable API, resumable scans, and real-time dashboard display.

## Commands

### validate

Parses and validates input files without performing any network scan.

```bash
port-scan validate -cidr-file <path> [-port-file <path>] [-format human|json]
```

**Exit codes:**
- `0` — all inputs valid
- `1` — inputs invalid (details in stdout if `-format json`)
- `2` — config/flag error (details to stderr)

### scan

Runs the full scan pipeline.

```bash
port-scan scan -cidr-file <path> [-port-file <path>] [flags...]
```

**Exit codes:**
- `0` — scan completed, result CSVs written
- `1` — runtime error (file write failure, config error during run, or validation failure)
- `2` — CLI or config error (missing required flags, invalid flag values, parse failure)
- `130` — scan canceled by SIGINT; `resume_state.json` written

## CLI Flags

All flags apply to both `validate` and `scan` commands unless noted.

| Flag | Default | Description |
|------|---------|-------------|
| `-cidr-file` | (required) | Path to the CIDR input CSV |
| `-port-file` | (required in basic mode) | Path to the port input file (one `port/tcp` per line). Not required in rich mode. |
| `-output` | `scan_results.csv` | Base path for result CSV files. Actual files are `scan_results-<ts>.csv` and `opened_results-<ts>.csv` written in the same directory as the output path. |
| `-timeout` | `100ms` | Per-scan TCP connection timeout (Go duration string) |
| `-delay` | `10ms` | Pause between dispatching consecutive tasks |
| `-bucket-rate` | `100` | Leaky-bucket token refill rate (tokens/second) |
| `-bucket-capacity` | `100` | Leaky-bucket maximum burst size |
| `-workers` | `10` | Number of concurrent scan goroutines |
| `-pressure-api` | `http://localhost:8080/api/pressure` | URL of the pressure API endpoint (plain HTTP) |
| `-pressure-interval` | `5s` | How often to poll the pressure API (Go duration string or integer seconds) |
| `-disable-api` | `false` | Disable pressure API polling; use only local rate control |
| `-pressure-auth-url` | (empty) | OAuth token endpoint URL (required with `-pressure-use-auth`) |
| `-pressure-data-url` | (empty) | Comma-separated list of pressure data endpoint URLs (required with `-pressure-use-auth`) |
| `-pressure-client-id` | (empty) | OAuth client ID (required with `-pressure-use-auth`) |
| `-pressure-client-secret` | (empty) | OAuth client secret (required with `-pressure-use-auth`) |
| `-pressure-use-auth` | `false` | Use OAuth-authenticated pressure fetcher |
| `-resume` | (empty) | Path to a resume state JSON file to continue an interrupted scan |
| `-log-level` | `info` | Log verbosity: `debug`, `info`, or `error` |
| `-format` | `human` | Output format: `human` or `json`. Applies to `validate` output only. |
| `-quiet` | `false` | Suppress console logs; keep pressure API logs |
| `-cidr-ip-col` | `ip` | Column name for the IP selector in the CIDR CSV |
| `-cidr-ip-cidr-col` | `ip_cidr` | Column name for the boundary CIDR in the CIDR CSV |

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

Rows with `decision=accept` become scan targets; `decision=deny` rows are skipped.

### CIDR Expansion in Rich Mode

Rich mode performs reason-aware IP expansion:
- If `reason` is `PRECHECK_ALLOW_ALL`: entire `dst_network_segment` is expanded
- If `reason` is `MATCH_POLICY_ACCEPT`: only `dst_ip` is scanned
- Otherwise: only `dst_ip` is scanned

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
| `status` | `open`, `close`, or `close(timeout)` |
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

### resume_state.json

Written on SIGINT or scan failure. Contains chunk states for resuming.

```json
{
  "Chunks": [
    {"CIDR": "10.0.0.0/24", "NextIndex": 50, "ScannedCount": 50, "Status": "in_progress"}
  ]
}
```

## Pressure API Contract

### Plain HTTP Fetcher

`SimplePressureFetcher` makes unauthenticated GET to the configured URL. Expected JSON response:

```json
{"pressure": 45.0}
```

### OAuth-Authenticated Fetcher

`AuthenticatedPressureFetcher` obtains a bearer token from `authURL` and uses it to fetch from `dataURL`. The data response is an array:

```json
[{"data": {"Percent": 45.0}}, {"data": {"Percent": 30.0}}]
```

Returns the **maximum** `Percent` value across all entries.

### Multi-Source Fetcher

`MultiSourcePressureFetcher` fans out to multiple `dataURLs` concurrently with shared OAuth credentials. Returns the maximum pressure across all sources. If **any** source fails, the entire fetch fails.

### Pressure Response Parsing

`pressure` field accepts: number, numeric string, or JSON number types. Values are normalized to one decimal place.

### Pause Behavior

When pressure exceeds `PressureLimit` (default 60%), the scanner pauses dispatch until pressure drops below the threshold.

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
├── pressure.go                # PressureFetcher interface and implementations
├── dashboard_state.go         # Dashboard state management
├── dashboard_renderer.go      # ANSI rendering
├── dashboard_runtime.go       # Lifecycle management
├── dashboard_telemetry.go     # Telemetry helpers
├── record_mapper.go           # Record mapping
├── record_writer.go           # Record writing
└── fdlimit_*.go               # File descriptor limit handling
```
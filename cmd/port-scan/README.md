# port-scan

TCP port scanner for IPv4 with pressure-aware pacing, rate control, and resume support. Scans a set of targets and ports, writing results to CSV output files.

## Overview

`port-scan` is the main scanner binary for the port-scan-mk3 project. It supports two input modes:

- **Basic mode** — A CIDR CSV file with IP selectors and boundary CIDRs, paired with a port file listing TCP ports to test.
- **Rich mode** — A single CSV file with full firewall-policy rows (src_ip, dst_ip, port, decision, etc.). In rich mode, no separate port file is required.

The scanner dispatches tasks to a configurable worker pool, writes results in real time, and can be paused/resumed via SIGINT or a pressure API.

## Commands

### validate

Parse and validate input files without running a network scan. Runs in O(n) time over the input rows.

```bash
port-scan validate -cidr-file targets.csv -port-file ports.csv
port-scan validate -cidr-file targets.csv -port-file ports.csv -format json
```

### scan

Run the full scan pipeline. Outputs are written to `scan_results-<timestamp>.csv` (all results) and `opened_results-<timestamp>.csv` (open ports only).

```bash
port-scan scan -cidr-file targets.csv -port-file ports.csv
port-scan scan -cidr-file targets.csv -port-file ports.csv -output ./results
port-scan scan -cidr-file targets.csv -port-file ports.csv -format json -log-level debug
```

## Architecture

```
CLI entry point (main.go)
    │
    ├── handleValidateCommand
    │       config.Parse() → validate.Inputs() → cli.WriteValidation()
    │
    └── handleScanCommand
            config.Parse()
                   │
                   ▼
            scanapp.Run()
                   │
                   ├── Load CIDR records (basic or rich mode) via input.LoadCIDRsWithColumns
                   ├── Load port specs via input.LoadPorts (basic mode only)
                   ├── build group runtimes with leaky-bucket rate control
                   ├── start pressure API poller (unless -disable-api)
                   ├── start keyboard-controlled pause loop
                   ├── start scan executor (N workers, net.Dialer)
                   │       │
                   │       └── net.DialTimeout → scan result
                   │
                   ├── task dispatcher (orchestrates flow control via speedctrl.Controller)
                   │       │
                   │       └── dispatches (IP, port) scanTask to worker queue
                   │
                   ├── result writer goroutine
                   │       │
                   │       ├── writer.CSVWriter → scan_results-<ts>.csv (all results)
                   │       └── writer.OpenOnlyWriter → opened_results-<ts>.csv (open only)
                   │
                   └── resume state persister (on SIGINT or error)
                           └── resume_state.json
```

### Key Packages

| Package | Responsibility |
|---------|----------------|
| `pkg/config` | Flag parsing and Config struct |
| `pkg/validate` | Input file validation (exists, parseable, correct schema) |
| `pkg/input` | CIDR CSV and port file loading; auto-detects basic vs rich mode |
| `pkg/scanapp` | Full scan orchestration: task dispatch, executor, pressure monitor, dashboard, result writer |
| `pkg/speedctrl` | Rate control via leaky-bucket controller; keyboard pause support |
| `pkg/scanner` | Low-level TCP scanning via `net.DialTimeout` |
| `pkg/writer` | CSV output with fixed 14-column schema; OpenOnlyWriter filter |
| `pkg/cli` | CLI formatting (human vs JSON output for validate command) |
| `pkg/state` | SIGINT cancel context for graceful interruption |

## CLI Flags

All flags apply to both `validate` and `scan` commands unless noted.

| Flag | Default | Description |
|------|---------|-------------|
| `-cidr-file` | (required) | Path to the CIDR input CSV |
| `-port-file` | (required in basic mode) | Path to the port input file (one `port/tcp` per line). Not required in rich mode. |
| `-output` | `scan_results.csv` | Base path for result CSV files. Actual files are `scan_results-<ts>.csv` and `opened_results-<ts>.csv` written in the same directory as the output path. |
| `-timeout` | `100ms` | Per-scan TCP connection timeout (duration string) |
| `-pre-scan-ping-timeout` | `100ms` | Pre-scan ping reachability timeout (duration string, must be > 0) |
| `-delay` | `10ms` | Pause between dispatching consecutive tasks |
| `-bucket-rate` | `100` | Leaky-bucket token refill rate (tokens/second) |
| `-bucket-capacity` | `100` | Leaky-bucket maximum burst size |
| `-workers` | `10` | Number of concurrent scan goroutines |
| `-pressure-api` | `http://localhost:8080/api/pressure` | URL of the pressure API endpoint |
| `-pressure-interval` | `5s` | How often to poll the pressure API (duration or integer seconds) |
| `-disable-api` | `false` | Disable pressure API polling; use only local rate control |
| `-pressure-auth-url` | (empty) | OAuth token endpoint URL (required with `-pressure-use-auth`) |
| `-pressure-data-url` | (empty) | Comma-separated list of pressure data endpoint URLs (required with `-pressure-use-auth`; all sources must succeed; maximum value is used) |
| `-pressure-client-id` | (empty) | OAuth client ID (required with `-pressure-use-auth`) |
| `-pressure-client-secret` | (empty) | OAuth client secret (required with `-pressure-use-auth`) |
| `-pressure-use-auth` | `false` | Use OAuth-authenticated pressure fetcher |
| `-resume` | (empty) | Path to a resume state JSON file to continue an interrupted scan |
| `-log-level` | `info` | Log verbosity: `debug`, `info`, or `error` |
| `-format` | `human` | Output format: `human` (line-oriented) or `json` (structured). Applies to `validate` output only. |
| `-quiet` | `false` | Suppress console logs; keep pressure API logs |
| `-cidr-ip-col` | `ip` | Column name for the IP selector in the CIDR CSV |
| `-cidr-ip-cidr-col` | `ip_cidr` | Column name for the boundary CIDR in the CIDR CSV |

## Input Formats

### Basic Mode: CIDR CSV

One row per IP selector. Columns are matched by header name with fallback to position.

| Column | Required | Description |
|--------|----------|-------------|
| `ip` | Yes | IP selector — single IPv4 address or CIDR range |
| `ip_cidr` | Yes | Boundary CIDR containing the selector |
| `fab_name` | No | Fabric/mesh name carried through to output |
| `cidr_name` | No | Human-readable CIDR label carried through to output |
| `port` | No | Pre-specified port number on this row (otherwise uses port-file) |

**Example:**
```csv
fab_name,ip,ip_cidr,cidr_name
fab1,10.0.0.1,10.0.0.0/24,internal
fab1,10.0.0.2,10.0.0.0/24,internal
fab2,192.168.1.0,192.168.1.0/28,dmz
```

### Basic Mode: Port File

One specification per line in `port/tcp` format.

```
80/tcp
443/tcp
8080/tcp
```

### Rich Mode: Firewall-Policy CSV

Auto-detected by the presence of all required columns. No port file is needed — each row specifies its own dst_ip, dst_network_segment, port, and decision. Rows with `decision=accept` become scan targets; `decision=deny` rows are skipped.

| Column | Required | Description |
|--------|----------|-------------|
| `src_ip` | Yes | Source IPv4 address |
| `src_network_segment` | Yes | Source network boundary CIDR containing src_ip |
| `dst_ip` | Yes | Destination IPv4 address (scan target) |
| `dst_network_segment` | Yes | Destination network boundary CIDR containing dst_ip |
| `service_label` | Yes | Service identifier |
| `protocol` | Yes | Protocol (only `tcp` is accepted) |
| `port` | Yes | TCP port number (1–65535) |
| `decision` | Yes | `accept` (scan) or `deny` (skip) |
| `matched_policy_id` | Yes | Policy rule identifier |
| `reason` | Yes | Policy match reason |

**Example:**
```csv
src_ip,src_network_segment,dst_ip,dst_network_segment,service_label,protocol,port,decision,matched_policy_id,reason
10.0.0.10,10.0.0.0/24,192.168.1.100,192.168.1.0/24,web,tcp,80,accept,P-1,allow
10.0.0.10,10.0.0.0/24,192.168.1.100,192.168.1.0/24,web,tcp,443,accept,P-1,allow
10.0.0.10,10.0.0.0/24,10.0.0.0/8,10.0.0.0/8,internal,tcp,22,deny,P-2,block
```

## Output Formats

### scan_results-*.csv

All scan results (open, close, timeout). Fixed 14-column schema:

```
ip,ip_cidr,port,status,response_time_ms,fab_name,cidr_name,service_label,decision,matched_policy_id,reason,execution_key,src_ip,src_network_segment
```

| Column | Description |
|--------|-------------|
| `ip` | Scanned IP address |
| `ip_cidr` | Boundary CIDR (or `cidr` field from input) |
| `port` | TCP port number |
| `status` | `open`, `close`, or `close(timeout)` |
| `response_time_ms` | TCP connection latency in milliseconds |
| `fab_name` | Fabric name from CIDR input |
| `cidr_name` | CIDR label from CIDR input |
| `service_label` | Service label (rich mode only) |
| `decision` | Policy decision (rich mode only) |
| `matched_policy_id` | Policy ID (rich mode only) |
| `reason` | Policy reason (rich mode only) |
| `execution_key` | Dedup key in `ip:port/protocol` form (rich mode only) |
| `src_ip` | Source IP from rich input (rich mode only) |
| `src_network_segment` | Source network boundary (rich mode only) |

Basic mode rows carry `ip`, `ip_cidr`, `port`, `status`, `response_time_ms`, `fab_name`, `cidr_name`; all other columns are empty.

**Example row (basic mode):**
```
192.168.1.1,192.168.1.0/24,80,open,12,fab1,internal,,,,,,,,
```

### opened_results-*.csv

Same schema as `scan_results-*.csv`, but contains only rows where `status=open`.

### resume_state.json

Written on SIGINT or scan failure. Contains completed task keys so the scan can be resumed from where it left off.

## Usage Examples

### Validate basic inputs

```bash
port-scan validate -cidr-file targets.csv -port-file ports.csv
```

### Validate with JSON output

```bash
port-scan validate -cidr-file targets.csv -port-file ports.csv -format json
```

### Validate rich-mode input (no port file needed)

```bash
port-scan validate -cidr-file policy.csv
```

### Full scan with defaults

```bash
port-scan scan -cidr-file targets.csv -port-file ports.csv
```

### Scan with output to a specific directory

```bash
port-scan scan -cidr-file targets.csv -port-file ports.csv -output ./results
```

### Scan with JSON-formatted stderr logs

```bash
port-scan scan -cidr-file targets.csv -port-file ports.csv -format json -log-level debug
```

### Scan with custom workers and timeout

```bash
port-scan scan -cidr-file targets.csv -port-file ports.csv -workers 20 -timeout 500ms
```

### Scan with aggressive rate limiting

```bash
port-scan scan -cidr-file targets.csv -port-file ports.csv -bucket-rate 500 -bucket-capacity 500
```

### Scan with pressure API control

```bash
port-scan scan -cidr-file targets.csv -port-file ports.csv -pressure-api http://localhost:8080/api/pressure -pressure-interval 5s
```

### Scan with OAuth-authenticated pressure API

```bash
port-scan scan -cidr-file targets.csv -port-file ports.csv \
  -pressure-use-auth \
  -pressure-auth-url https://auth.example.com/oauth/token \
  -pressure-data-url https://api.example.com/pressure \
  -pressure-client-id my-client \
  -pressure-client-secret my-secret
```

### Scan with multiple authenticated pressure API sources

All sources share the same OAuth credentials. The scanner polls each URL concurrently and pauses when any source reports pressure at or above the threshold.

```bash
port-scan scan -cidr-file targets.csv -port-file ports.csv \
  -pressure-use-auth \
  -pressure-auth-url https://auth.example.com/oauth/token \
  -pressure-data-url "https://api1.example.com/pressure,https://api2.example.com/pressure" \
  -pressure-client-id my-client \
  -pressure-client-secret my-secret
```

### Scan with rich-mode policy CSV

```bash
port-scan scan -cidr-file policy.csv
```

### Resume an interrupted scan

```bash
port-scan scan -cidr-file targets.csv -port-file ports.csv -resume ./results/resume_state.json
```

### Custom column names in CIDR CSV

```bash
port-scan scan -cidr-file targets.csv -port-file ports.csv -cidr-ip-col source_ip -cidr-ip-cidr-col source_cidr
```

### Suppress console logs (keep pressure API logs)

```bash
port-scan scan -cidr-file targets.csv -port-file ports.csv -quiet
```

## Exit Codes

| Code | Meaning | Details |
|------|---------|---------|
| `0` | Success | Scan completed; result CSVs written |
| `1` | Runtime error | File write failure, config error during run, or validation failure |
| `2` | CLI or config error | Missing required flags, invalid flag values, parse failure |
| `130` | Scan canceled | SIGINT received during scan; `resume_state.json` written |

**Validation exit codes:**
- `validate` with `0` — all inputs valid
- `validate` with `1` — inputs invalid (detail in JSON stdout)
- `validate` with `2` — config/flag error (detail to stderr)

## Building

```bash
# Build the binary
go build -o port-scan ./cmd/port-scan

# Build all commands
go build -o port-scan ./cmd/port-scan
go build -o cidr-compare ./cmd/cidr-compare
```

## Testing

```bash
# Test this command
go test ./cmd/port-scan/...

# Test all packages
go test ./...
```

## Implementation Notes

The scanner uses Go standard library `net.DialTimeout` for TCP connections. Workers are goroutines sharing a bounded task channel; results are serialized at write time by a single writer goroutine.

Pressure control works by having the task dispatcher consult a `speedctrl.Controller` before releasing each task. The controller accumulates tokens from the leaky-bucket scheduler and the pressure API (when enabled). Keyboard input (`p` to pause, `r` to resume) updates the controller directly.

The output schema (14 columns) is defined as a single source of truth in `pkg/writer/csv_writer.go`. Changing the schema is a MAJOR version change per the product constitution.
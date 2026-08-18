# port-scan

TCP port scanner for IPv4 with pressure-aware pacing, rate control, and resume support. It scans a set of targets and ports. It writes the results to CSV output files.

## Overview

`port-scan` is the main scanner binary for the port-scan-mk3 project. It supports two input modes:

- **Basic mode** — A CIDR CSV file with IP selectors and boundary CIDRs. Each row can override the port file.
- **Rich mode** — A single CSV file with full firewall-policy rows (for example, src_ip, dst_ip, port, and decision). In rich mode, you do not need a separate port file.

The scanner dispatches probe tasks to a worker pool that you configure. It writes results during the scan.

An OS interrupt cancels the scan. The pressure API or space bar can pause and resume dispatch in the same process.

## Commands

### validate

This command parses and validates the input files. It does not run a network scan.

The validation cost increases with the input byte count and row count.

```bash
port-scan validate -cidr-file targets.csv -port-file ports.csv
port-scan validate -cidr-file targets.csv -port-file ports.csv -format json
```

### pre-ping

Ping the authorized candidate addresses. The command writes an unreachable-address CSV and prints its path.

### generate-buckets

Build a resume snapshot from the authorized input. In basic mode, this command also reads the port file.

### scan

Scan the probe tasks in a required resume snapshot. The command never pings targets or builds new buckets.

```bash
port-scan pre-ping -cidr-file targets.csv
port-scan generate-buckets -cidr-file targets.csv -port-file ports.csv -buckets-out buckets.json
port-scan scan -cidr-file targets.csv -resume buckets.json -output ./results
```

## Architecture

```
CLI entry point (main.go)
    │
    ├── handleValidateCommand
    │       config.ParseValidate() → config.ValidateConfig
    │           └── validate.Inputs(Configuration) → cli.WriteValidation()
    │
    ├── handlePrePingCommand → scanapp.RunPrePing
    ├── handleGenerateBucketsCommand → scanapp.GenerateBuckets
    └── handleScanCommand → scanapp.Run
                               │
                               └── private scanRuntime.execute(context.Context)
                                      ├── load the snapshot and rebuild incomplete chunks
                                      ├── start pressure, control, worker, and dispatch tasks
                                      ├── drain results and errors after cancellation
                                      ├── write committed results and save resume state
                                      └── stop tasks and close output files
```

### Key Packages

| Package | Responsibility |
|---------|----------------|
| `pkg/config` | Command-specific flag parsing and opaque configuration values |
| `pkg/validate` | Input file validation (exists, parseable, correct schema) |
| `pkg/input` | CIDR CSV and port file loading. It detects basic or rich mode automatically |
| `pkg/scanapp` | Workflow entry points and the private scan runtime |
| `pkg/speedctrl` | Rate control and a space-bar pause gate |
| `pkg/scanner` | Low-level TCP scanning via `net.DialTimeout` |
| `pkg/writer` | CSV output with a fixed 14-column schema, plus the OpenOnlyWriter filter |
| `pkg/cli` | CLI formatting (human vs JSON output for validate command) |
| `pkg/state` | Interrupt contexts for graceful cancellation and emergency exit |

## CLI Flags

The pipeline commands accept only the flags in their command usage. The
`validate` command also accepts the complete legacy flag surface for
compatibility.

| Flag | Default | Description |
|------|---------|-------------|
| `-cidr-file` | (required) | Path to the CIDR input CSV |
| `-port-file` | (conditional) | Path to the port input file. A basic row with a blank `port` value uses this file. Rich mode ignores it. |
| `-output` | `scan_results.csv` | Base path for result CSV files. Actual files are `scan_results-<ts>.csv` and `opened_results-<ts>.csv` written in the same directory as the output path. |
| `-output-flush-results` | `1000` | Number of probe results in one output batch. `1` flushes each result. `0` disables periodic flushes. |
| `-timeout` | `100ms` | Per-scan TCP connection timeout (duration string) |
| `-pre-scan-ping-timeout` | `100ms` | Pre-scan ping reachability timeout (duration string, must be > 0) |
| `-delay` | `10ms` | Wait between consecutive task dispatches |
| `-bucket-rate` | `100` | Leaky-bucket token refill rate (tokens/second) |
| `-bucket-capacity` | `100` | Leaky-bucket maximum burst size |
| `-workers` | `10` | Number of concurrent scan goroutines |
| `-pressure-api` | `http://localhost:8080/api/pressure` | URL of the pressure API endpoint |
| `-pressure-interval` | `5s` | How often to poll the pressure API (duration or integer seconds) |
| `-disable-api` | `false` | Disable pressure API polling. Use local rate control only |
| `-pressure-auth-url` | (empty) | OAuth token endpoint URL (required with `-pressure-use-auth`) |
| `-pressure-data-url` | (empty) | Comma-separated list of pressure data endpoint URLs. Required with `-pressure-use-auth`. All sources must succeed, and the command uses the maximum value |
| `-pressure-client-id` | (empty) | OAuth client ID (required with `-pressure-use-auth`) |
| `-pressure-client-secret` | (empty) | OAuth client secret (required with `-pressure-use-auth`) |
| `-pressure-use-auth` | `false` | Use OAuth-authenticated pressure fetcher |
| `-resume` | (empty) | Path to a resume state JSON file to continue an interrupted scan |
| `-log-level` | `info` | Log verbosity: `debug`, `info`, or `error` |
| `-format` | `human` | Output format: `human` (line-oriented) or `json` (structured). Applies to `validate` output only. |
| `-quiet` | `false` | Suppress the periodic progress output. It does not filter the per-result or error logs; use `-log-level` for log verbosity |
| `-cidr-ip-col` | `ip` | Column name for the IP selector in the CIDR CSV |
| `-cidr-ip-cidr-col` | `ip_cidr` | Column name for the boundary CIDR in the CIDR CSV |
| `-target-count-limit` | `10000000` | Maximum candidate addresses. Set `0` to disable the count limit. |
| `-target-memory-limit-gb` | `16` | Target expansion budget in decimal GB. Set `0` to disable the memory limit. |

The commands verify target expansion before ping, dial, snapshot write, or result-file creation.
The estimate is `1000000000 + candidate count * 1500` bytes.

The count includes each authorized input row before de-duplication, broadcast removal, and blocklist filtering.
A rich deny row contributes zero candidates. An IPv4 `/9` passes the defaults, but an IPv4 `/8` does not.

`generate-buckets` stores the effective limits and candidate count in the snapshot.
`scan` uses the stored limits unless an explicit scan flag replaces one limit.
A legacy snapshot uses the defaults.

CAUTION: Set both flags to `0` only when the host has sufficient memory.
With this setting, no hidden target limit protects the process from resource exhaustion.

### Input, snapshot, and pressure limits

The CIDR input defaults are `1` decimal GB and `10000000` data records.
Use `-cidr-input-size-limit-gb` and `-cidr-input-record-limit` to change them.

The port input defaults are `1` decimal MB and `65535` nonblank records.
Use `-port-input-size-limit-mb` and `-port-input-record-limit` to change them.

The snapshot defaults are `2` decimal GB and `10000000` items for each object type.
The object types are chunks, port entries, and unreachable IPs.
Use the four `-snapshot-*-limit` flags to change these values.

Each pressure response defaults to `1` decimal MB.
Each OAuth data array defaults to `10000` entries.
Use `-pressure-response-size-limit-mb` and `-pressure-response-entry-limit` to change them.

A positive value replaces its default. A negative value is an error.
Set one flag to `0` to disable only that limit.

CAUTION: A disabled limit can exhaust memory or terminate the process.

## Input Formats

### Basic Mode: CIDR CSV

One row per IP selector. `port-scan` matches the columns by header name. If the header name is absent, `port-scan` uses the column position.

| Column | Required | Description |
|--------|----------|-------------|
| `ip` | Yes | IP selector — single IPv4 address or CIDR range |
| `ip_cidr` | Yes | Boundary CIDR containing the selector |
| `fab_name` | No | Fabric/mesh name carried through to output |
| `cidr_name` | No | Human-readable CIDR label carried through to output |
| `port` | No | TCP port for this row. A value overrides the port file for all IPs from this selector. |

**Example:**
```csv
fab_name,ip,ip_cidr,cidr_name
fab1,10.0.0.1,10.0.0.0/24,internal
fab1,10.0.0.2,10.0.0.0/24,internal
fab2,192.168.1.0,192.168.1.0/28,dmz
```

### Basic Mode: Port File

One specification per line in `port/tcp` format.

A basic row with a blank `port` value uses every port in this file. If all
basic rows contain a port, `-port-file` is optional. The scanner combines all
rows and scans each unique `ip:port/tcp` target one time.

```
80/tcp
443/tcp
8080/tcp
```

### Rich Mode: Firewall-Policy CSV

`port-scan` detects this mode when all required columns are present. You do not need a port file because each row contains its port.

Rows with `decision=accept` become scan targets. Rows with `decision=deny` never become network targets.

A deny row overrides an accept row with the same normalized `execution_key`. This rule applies before ICMP, bucket generation, resume rebuild, and TCP dispatch.

The `validate` command still rejects malformed deny rows. A deny-only input succeeds and produces empty scan artifacts.

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

If an unmarked snapshot has rich deny input, `scan` rejects it before TCP dispatch. Run `generate-buckets` to create a new snapshot.

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

Basic mode rows carry `ip`, `ip_cidr`, `port`, `status`, `response_time_ms`, `fab_name`, and `cidr_name`. All the other columns are empty.

**Example row (basic mode):**
```
192.168.1.1,192.168.1.0/24,80,open,12,fab1,internal,,,,,,,,
```

### opened_results-*.csv

This file has the same schema as `scan_results-*.csv`. It contains only the rows where `status=open`.

The scan treats both result files as one commit batch. It updates progress only
after both writers flush. A controlled stop flushes the final batch.

If a write or flush fails, the scan rewinds the complete current batch. A
resumed scan can duplicate rows from that batch, but it does not omit them.

### Bucket snapshot and resume state

The path from `-resume` is the bucket snapshot and the resume state. `port-scan` updates it after cancellation or a scan failure.
The file records completed work and the lowest unwritten task in each chunk.

Resume reads and parses the current input. It rebuilds only incomplete chunks and does not revalidate completed chunks.

Completed chunks can use an earlier input revision. If all results must use one input revision, start a fresh run.

Queued probes do not start after cancellation. Started probes finish with their
original timeout, and the command writes their results before snapshot persistence.

Press Ctrl+C or Ctrl+Break one time for graceful cancellation. Press it again
to force exit code `130` without a current-snapshot guarantee.

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

All the sources share the same OAuth credentials. The scanner polls each URL concurrently. When the pressure from any source is at the threshold or higher, the scanner pauses.

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

### Suppress the progress output

`-quiet` stops the periodic `progress cidr=...` line on standard output, and
the matching `scan_progress` event, because both come from one emitter. It
changes nothing else. Per-result `scan_result` events and error-level lines,
such as a worker panic or a local resource failure, still go to standard error.

```bash
port-scan scan -cidr-file targets.csv -port-file ports.csv -quiet
```

To make the run fully silent, also set the log level to `error`:

```bash
port-scan scan -cidr-file targets.csv -port-file ports.csv -quiet -log-level error
```

## Exit Codes

| Code | Meaning | Details |
|------|---------|---------|
| `0` | Success | The scan completed, and the command wrote the result CSVs |
| `1` | Runtime error | File write failure, config error during run, or validation failure |
| `2` | CLI or config error | Missing required flags, invalid flag values, parse failure |
| `130` | Scan canceled | The first interrupt saved the snapshot, or the second interrupt forced an emergency exit |

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

The scanner uses the Go standard library `net.Dialer.DialContext` for TCP connections.
Workers share a bounded task channel. One result loop serializes output writes.

Cancellation stops queue consumption before the next dial. It does not change
the context or timeout of a dial that already started.

For pressure control, the task dispatcher reads a `speedctrl.Controller` before it releases each task.

The leaky bucket controls rate. The pressure API and the space bar control the pause gate.

`pkg/writer/csv_writer.go` defines the output schema (14 columns) as a single source of truth. A change to the schema is a MAJOR version change, per the product constitution.

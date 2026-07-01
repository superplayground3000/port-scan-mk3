# csv-transform Design Document

**Tool**: `cmd/csv-transform` | **Core**: `pkg/csvtransform` | **Revised**: 2026-07-01

## Architecture

`cmd/csv-transform` is thin CLI wiring (flag/env parsing, exit codes) over the
reusable `pkg/csvtransform` pipeline:

```
Input CSV
    │
    ▼
spreadsheet.Reader (pkg/spreadsheet)
    │
    ▼
Column Indexing (header row)
    │
    ▼
Row Processing Loop  (pkg/csvtransform.Run)
    │
    ├── Filter: pass column == "FALSE" (case-insensitive)
    │
    ├── ResolveHost: DNS lookup for hostname → IPv4
    │
    ├── SplitPorts: "/"-separated → individual ports
    │
    └── Expand: one output row per (host, port) pair
    │
    ▼
Output CSV (Rich format)
```

## Components

### TransformConfig (`cmd/csv-transform`)

```go
type TransformConfig struct {
    Input     string // Path to input CSV
    Output    string // Path to output CSV
    SheetName string // Worksheet name (default: all-runs)
    HostCol   string // Host column name (default: Host)
    PortCol   string // Port column name (default: Port)
    PassCol   string // Pass/fail column name (default: Pass the test)
}
```

### ParseConfigFromArgs (`cmd/csv-transform`)

Parses CLI flags and environment variables. Supports:
- CLI flags: `--input`, `--output`, `--host-col`, `--port-col`, `--pass-col`, `--sheet`
- Environment variables: `TRANSFORM_INPUT`, `TRANSFORM_OUTPUT`, `TRANSFORM_HOST_COL`, `TRANSFORM_PORT_COL`, `TRANSFORM_PASS_COL`, `TRANSFORM_SHEET_NAME`

`runMain` maps the parsed `TransformConfig` 1:1 onto `csvtransform.Config` and
calls `csvtransform.Run(cfg, stderrOut)`, wiring `stderrOut` (the process stderr)
as the warning sink. A non-nil `Run` error is reported and mapped to exit code 1;
this is distinct from the config-parse error path, which exits with code 2.

### csvtransform.Config / csvtransform.Run (`pkg/csvtransform`)

```go
type Config struct {
    Input, Output, SheetName, HostCol, PortCol, PassCol string
}

func Run(cfg Config, warn io.Writer) error
```

Wires together CSV reading, column indexing, filtering, host resolution, port
expansion, and CSV output. Problematic rows are logged to the injected `warn`
writer and skipped (never `os.Stderr` directly, and `warn` is never nil/buffered
by the caller).

**Row processing logic:**
1. Check row has sufficient columns
2. Filter out rows where pass column is not "FALSE"
3. Resolve host via DNS or passthrough for IP addresses
4. Split "/"-separated port string into individual ports
5. Expand each (host, port) pair into output row

Note: an invalid port token produces **two** warnings in sequence — `SplitPorts`
logs `invalid port %q: %v` first, then `Run` logs the row-level
`skipping row %d: invalid port value %q` when it observes `ports == nil`. This
double-warning ordering is intentional and covered by tests.

### SplitPorts (`pkg/csvtransform`)

```go
func SplitPorts(portStr string, warn io.Writer) ([]int, error)
```

Splits a "/"`-separated port string into individual port integers:
- Empty string returns `nil` (caller skips row)
- Invalid ports are skipped silently (logged to `warn`)
- Returns `nil` if all ports invalid

### ResolveHost (`pkg/csvtransform`)

```go
func ResolveHost(host string) (string, error)
```

Resolves a host (IP or hostname) to an IPv4 string:
- IPv4 addresses returned as-is
- Hostnames resolved via `net.LookupIP`
- On resolution failure, original hostname string is returned **silently, with
  no warning emitted** (downstream validation catches it)

### ShouldIncludeRow (`pkg/csvtransform`)

```go
func ShouldIncludeRow(passVal string) bool
```

Returns `true` only if passVal is `"FALSE"` (case-insensitive, trimmed).

### spreadsheet.Reader (`pkg/spreadsheet/reader.go`)

Provides a unified interface for reading CSV and Excel files:
- CSV: reads file directly
- Excel: opens specified worksheet

## File Structure

```
cmd/csv-transform/
├── main.go          # CLI entry, TransformConfig, flag/env parsing, runMain wiring to csvtransform.Run
├── main_test.go      # config/flag tests + black-box runMain test
└── README.md

pkg/csvtransform/
├── transform.go      # SplitPorts(warn), ResolveHost, ShouldIncludeRow, csvHeader, default* consts
├── transform_test.go
├── pipeline.go       # Config, Run(cfg, warn) — the transform pipeline core
└── pipeline_test.go

pkg/spreadsheet/
├── reader.go        # Unified CSV/Excel reader
└── reader_test.go
```

## Pipeline Integration

```
csv-transform --input=scan.csv --output=data.csv
       │
       ▼
port-scan validate --cidr-file=data.csv
       │
       ▼
port-scan scan --cidr-file=data.csv --format=json --output=results
```

## Default Constants

```go
const (
    defaultSrcIP           = "10.0.0.1"
    defaultSrcNetwork      = "10.0.0.0/24"
    defaultDstNetwork      = "10.0.0.0/24"
    defaultServiceLabel    = "unknown"
    defaultProtocol        = "tcp"
    defaultDecision        = "accept"
    defaultMatchedPolicyID = "transformed"
    defaultReason          = "MATCH_POLICY_ACCEPT"
)
```

## Output CSV Header

```go
const csvHeader = "src_ip,src_network_segment,dst_ip,dst_network_segment,service_label,protocol,port,decision,matched_policy_id,reason"
```
# csv-transform Specification

**Tool**: `cmd/csv-transform` | **Core**: `pkg/csvtransform` | **Revised**: 2026-07-01

## Overview

`csv-transform` transforms a CSV file containing port scan results into a Rich CSV input file consumable by the `port-scan` pipeline.

## Usage

```bash
csv-transform --input=<path> --output=<path>
```

## CLI Flags

| Flag | Env Var | Default | Description |
|------|---------|---------|-------------|
| `--input` | `TRANSFORM_INPUT` | _(required)_ | Path to input CSV file |
| `--output` | `TRANSFORM_OUTPUT` | _(required)_ | Path to output CSV file |
| `--host-col` | `TRANSFORM_HOST_COL` | `Host` | Column containing IP or hostname |
| `--port-col` | `TRANSFORM_PORT_COL` | `Port` | Column containing port(s), "/"-separated |
| `--pass-col` | `TRANSFORM_PASS_COL` | `Pass the test` | Column indicating pass/fail |
| `--sheet` | `TRANSFORM_SHEET_NAME` | `all-runs` | Worksheet name (ignored for CSV files) |

## Input Format

CSV with columns for Host, Port, and Pass the test:

| Host | Port | Pass the test |
|------|------|---------------|
| 192.168.1.1 | 80/443 | FALSE |
| 10.0.0.5 | 22 | TRUE |

**Row filtering:**
- Rows where `Pass the test` is `"TRUE"` (case-insensitive, trimmed) are **skipped**
- Ports separated by `/` are **expanded** into one row per port
- Hostnames are resolved via DNS; on failure the hostname string is passed through as-is

## Output Format

Rich CSV with 10 columns:

```
src_ip,src_network_segment,dst_ip,dst_network_segment,service_label,protocol,port,decision,matched_policy_id,reason
```

**Default field values:**

| Column | Value |
|--------|-------|
| `src_ip` | `10.0.0.1` |
| `src_network_segment` | `10.0.0.0/24` |
| `dst_network_segment` | `10.0.0.0/24` |
| `service_label` | `unknown` |
| `protocol` | `tcp` |
| `decision` | `accept` |
| `matched_policy_id` | `transformed` |
| `reason` | `MATCH_POLICY_ACCEPT` |

`dst_ip` is the resolved IP address (or hostname if resolution fails).

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Transform succeeded, output file written |
| `1` | Runtime error (file not found, CSV parse error) |
| `2` | CLI flag validation error (missing required `--input` or `--output`) |

## Building and Testing

```bash
# Build
go build -o csv-transform ./cmd/csv-transform

# Test (CLI flag/wiring tests)
go test ./cmd/csv-transform/...

# Test (transform pipeline core)
go test ./pkg/csvtransform/...
```
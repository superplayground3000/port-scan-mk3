# xlsx-transform

Converts an xlsx workbook containing port scan results into a Rich CSV input file consumable by the `port-scan` pipeline.

## Usage

```bash
xlsx-transform --input=scan.xlsx --output=data.csv
```

## Flags

| Flag | Env Var | Default | Description |
|------|---------|---------|-------------|
| `--input` | `TRANSFORM_INPUT` | _(required)_ | Path to input xlsx file |
| `--output` | `TRANSFORM_OUTPUT` | _(required)_ | Path to output CSV file |
| `--sheet` | `TRANSFORM_SHEET_NAME` | `all-runs` | Worksheet name |
| `--host-col` | `TRANSFORM_HOST_COL` | `Host` | Column containing IP or hostname |
| `--port-col` | `TRANSFORM_PORT_COL` | `Port` | Column containing port(s), "/"-separated |
| `--pass-col` | `TRANSFORM_PASS_COL` | `Pass the test` | Column indicating pass/fail |

## Input Format

The xlsx should have columns for Host, Port, and Pass the test. Example:

| Host | Port | Pass the test |
|------|------|---------------|
| 192.168.1.1 | 80/443 | FALSE |
| 10.0.0.5 | 22 | TRUE |

- Rows where `Pass the test` is `"TRUE"` (case-insensitive) are **skipped**.
- Ports separated by `/` are **expanded** into one row per port.
- Hostnames are resolved via DNS; on failure the hostname string is passed through as-is.

## Output Format

Rich CSV with 10 columns:

```
src_ip,src_network_segment,dst_ip,dst_network_segment,service_label,protocol,port,decision,matched_policy_id,reason
```

Default field values:

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

## Examples

### Basic usage

```bash
xlsx-transform --input=scan.xlsx --output=data.csv
```

### With environment variables

```bash
export TRANSFORM_INPUT=/path/to/scan.xlsx
export TRANSFORM_OUTPUT=/path/to/output.csv
xlsx-transform
```

### Custom worksheet name

```bash
xlsx-transform --input=results.xlsx --output=data.csv --sheet=MySheet
```

```bash
TRANSFORM_SHEET_NAME=MySheet xlsx-transform --input=results.xlsx --output=data.csv
```

### Custom column names

```bash
xlsx-transform \
  --input=scan.xlsx \
  --output=data.csv \
  --host-col=IPAddress \
  --port-col=Ports \
  --pass-col=Status
```

```bash
export TRANSFORM_HOST_COL=IPAddress
export TRANSFORM_PORT_COL=Ports
export TRANSFORM_PASS_COL=Status
xlsx-transform --input=scan.xlsx --output=data.csv
```

### Full environment variable configuration

```bash
export TRANSFORM_INPUT=/data/scan_results.xlsx
export TRANSFORM_OUTPUT=/data/ready_for_scan.csv
export TRANSFORM_SHEET_NAME=all-runs
export TRANSFORM_HOST_COL=Host
export TRANSFORM_PORT_COL=Port
export TRANSFORM_PASS_COL=Pass\ the\ test
xlsx-transform
```

## Pipeline Integration

After transforming, use the output as input to the `port-scan` pipeline:

```bash
# Transform xlsx → CSV
xlsx-transform --input=scan.xlsx --output=data.csv

# Validate the CSV
port-scan validate --input=data.csv

# Run the scan
port-scan scan --input=data.csv --format=json --output=results.json
```

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Transform succeeded, output file written |
| `1` | Runtime error (file not found, xlsx parse error, sheet not found) |
| `2` | CLI flag validation error (missing required `--input` or `--output`) |

## Error Examples

```bash
# Missing --input
$ xlsx-transform --output=data.csv
error: missing required --input flag
# exit 2

# File not found
$ xlsx-transform --input=nonexistent.xlsx --output=data.csv
error: failed to open xlsx: file not found
# exit 1

# Sheet not found
$ xlsx-transform --input=scan.xlsx --output=data.csv --sheet=NonExistent
error: sheet not found: NonExistent
# exit 1
```

# csv-transform

Transforms a CSV file containing port scan results into a Rich CSV input file consumable by the `port-scan` pipeline.

## Usage

```bash
csv-transform --input=scan.csv --output=data.csv
```

## Flags

| Flag | Env Var | Default | Description |
|------|---------|---------|-------------|
| `--input` | `TRANSFORM_INPUT` | _(required)_ | Path to input CSV file |
| `--output` | `TRANSFORM_OUTPUT` | _(required)_ | Path to output CSV file |
| `--host-col` | `TRANSFORM_HOST_COL` | `Host` | Column containing IP or hostname |
| `--port-col` | `TRANSFORM_PORT_COL` | `Port` | Column containing port(s), "/"-separated |
| `--pass-col` | `TRANSFORM_PASS_COL` | `Pass the test` | Column indicating pass/fail |

## Input Format

The CSV should have columns for Host, Port, and Pass the test. Example:

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
csv-transform --input=scan.csv --output=data.csv
```

### With environment variables

```bash
export TRANSFORM_INPUT=/path/to/scan.csv
export TRANSFORM_OUTPUT=/path/to/output.csv
csv-transform
```

### Custom column names

```bash
csv-transform \
  --input=scan.csv \
  --output=data.csv \
  --host-col=IPAddress \
  --port-col=Ports \
  --pass-col=Status
```

```bash
export TRANSFORM_HOST_COL=IPAddress
export TRANSFORM_PORT_COL=Ports
export TRANSFORM_PASS_COL=Status
csv-transform --input=scan.csv --output=data.csv
```

### Full environment variable configuration

```bash
export TRANSFORM_INPUT=/data/scan_results.csv
export TRANSFORM_OUTPUT=/data/ready_for_scan.csv
export TRANSFORM_HOST_COL=Host
export TRANSFORM_PORT_COL=Port
export TRANSFORM_PASS_COL=Pass\ the\ test
csv-transform
```

## Pipeline Integration

After transforming, use the output as input to the `port-scan` pipeline:

```bash
# Transform CSV → Rich CSV
csv-transform --input=scan.csv --output=data.csv

# Validate the CSV
port-scan validate --cidr-file=data.csv

# Run the scan
port-scan scan --cidr-file=data.csv --format=json --output=results.json
```

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Transform succeeded, output file written |
| `1` | Runtime error (file not found, CSV parse error) |
| `2` | CLI flag validation error (missing required `--input` or `--output`) |

## Error Examples

```bash
# Missing --input
$ csv-transform --output=data.csv
error: missing required --input flag
# exit 2

# File not found
$ csv-transform --input=nonexistent.csv --output=data.csv
error: failed to open CSV: open nonexistent.csv: no such file or directory
# exit 1

# Invalid CSV
$ csv-transform --input=malformed.csv --output=data.csv
error: failed to open CSV: <underlying csv error>
# exit 1
```

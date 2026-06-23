# preprocess Specification

**Tool**: `cmd/preprocess` | **Revised**: 2026-05-03

## Overview

`preprocess` filters a rich CSV by removing targets whose `dst_network_segment` falls within any closed CIDR. It is used in the port-scan-mk3 pipeline between enrichment and scanning.

## Usage

```bash
preprocess --input=<path> --cleaned-cidrs=<path> --fab-name=<name> --output-dir=<path>
```

## CLI Flags

All four flags are required.

| Flag | Description |
|------|-------------|
| `--input` | Path to rich CSV input |
| `--cleaned-cidrs` | Path to cleaned CIDRs CSV (fab,segment,status) |
| `--fab-name` | Data center / fabric name (used to filter closed CIDRs) |
| `--output-dir` | Base output directory |

## Input Formats

### Rich CSV Input

Expects a header row with `dst_network_segment` column. Column name matching is case-insensitive.

### Cleaned CIDRs CSV

| Column | Description |
|--------|-------------|
| `fab` | Fabric/data center name |
| `segment` | CIDR notation |
| `status` | `open` or `close` |

Only rows with `status=close` (case-insensitive) and matching `fab-name` are loaded into the filter tree.

**Example:**
```csv
fab,segment,status
dc-east,10.0.0.0/8,close
dc-east,192.168.0.0/16,close
dc-west,10.0.0.0/8,open
```

## Output

Written to:
```
<output-dir>/<fab-name>/<YYYYMMDDTHHMMSSZ>/input.csv
```

The header row is passed through from the input. Only rows whose `dst_network_segment` is **not** contained within any closed CIDR are written.

## Filter Logic

For each input row:
1. Parse `dst_network_segment` as CIDR
2. Query closed CIDR tree for containment
3. If contained in any closed CIDR → **drop**
4. If not contained → **keep**

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Filter succeeded, output file written |
| `1` | Runtime error (file open, parse, write failure) |

## Building and Testing

```bash
# Build
go build -o preprocess ./cmd/preprocess

# Test
go test ./pkg/preprocess/...
```
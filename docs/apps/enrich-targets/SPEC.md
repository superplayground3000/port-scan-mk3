# enrich-targets Specification

**Tool**: `cmd/enrich-targets` | **Revised**: 2026-05-03

## Overview

`enrich-targets` transforms a minimal `host,port` CSV into a full rich CSV (10-column format) consumable by `port-scan`. It uses a CIDR reference tree to determine `dst_network_segment` and a service map to label ports.

## Usage

```bash
enrich-targets --input=<path> --cidr-list=<path> --service-map=<path> --output=<path>
```

## CLI Flags

All four flags are required.

| Flag | Description |
|------|-------------|
| `--input` | Path to opened targets CSV (host,port columns) |
| `--cidr-list` | Path to CIDR reference CSV |
| `--service-map` | Path to port-to-service-label CSV |
| `--output` | Path to write enriched rich CSV |

## Input Formats

### Input CSV (host,port)

Minimal CSV with host and port columns:

| host | port |
|------|------|
| 192.168.1.1 | 80 |
| 192.168.1.2 | 443 |
| 10.0.0.5 | 22 |

Column names are case-insensitive.

### CIDR Reference CSV

First column contains CIDR values. Header detection via first-row parse attempt.

**Example:**
```csv
10.0.0.0/24
192.168.0.0/16
```

### Service Map CSV

| port | service_label |
|------|---------------|
| 80 | http |
| 443 | https |
| 22 | ssh |

Column names: `port`, `service_label` (case-insensitive).

## Output Format

Rich CSV with 10 columns:

```
src_ip,src_network_segment,dst_ip,dst_network_segment,service_label,protocol,port,decision,matched_policy_id,reason
```

**Default values:**

| Column | Value |
|--------|-------|
| `src_ip` | `10.59.42.39` |
| `src_network_segment` | `10.59.42.39/32` |
| `protocol` | `tcp` |
| `decision` | `accept` |
| `matched_policy_id` | `enriched` |
| `reason` | `MATCH_POLICY_ACCEPT` |

`dst_ip`: the host from input (validated as IPv4).
`dst_network_segment`: CIDR containing host (most specific match from tree, falls back to host/32).
`service_label`: from service map (falls back to configured fallback).
`port`: from input.

## CIDR Resolution

For each host, the tool queries the CIDR tree for all entries containing the host IP. If multiple matches exist, the **most specific** (smallest range = largest prefix length) is selected. If no match, falls back to `host/32`.

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Enrichment succeeded, output file written |
| `1` | Runtime error (file open, parse, write failure) |

## Building and Testing

```bash
# Build
go build -o enrich-targets ./cmd/enrich-targets

# Test
go test ./pkg/enrich/...
```
# cidr-compare Specification

**Tool**: `cmd/cidr-compare` | **Revised**: 2026-08-14

## Overview

`cidr-compare` compares open CIDR ranges against deny CIDR ranges to detect network policy violations. It finds all open CIDRs that fall within any deny CIDR block.

## Usage

```bash
cidr-compare -deny-file <path> -open-file <path>
```

## CLI Flags

| Flag | Type | Description | Env Variable |
|------|------|-------------|--------------|
| `-deny-file` | string | Path to deny CSV file | `CIDR_COMPARE_DENY_FILE` |
| `-open-file` | string | Path to open CSV file | `CIDR_COMPARE_OPEN_FILE` |

Both flags are required. Either pass as CLI flags or set via environment variables.

## Input Formats

### Deny CSV

The input requires a CSV header record. Both official headers select name-based columns.

If neither official header exists, the tool uses columns 0 and 1. One official header is an error.

| Column | Required | Description |
|--------|----------|-------------|
| `dst_network_segment` | Yes (or col 0) | CIDR notation, e.g. `10.0.0.0/8` |
| `decision` | Yes (or col 1) | Policy decision; only rows with `decision=deny` are processed |

**Example:**
```csv
dst_network_segment,decision
10.0.0.0/8,deny
192.168.0.0/16,deny
```

Rows where `decision` is not `deny` (case-insensitive) are skipped silently.

### Open CSV

The input requires a CSV header record. Both official headers select name-based columns.

If neither official header exists, the tool uses columns 0 and 1. One official header is an error.

| Column | Required | Description |
|--------|----------|-------------|
| `segment` | Yes (or col 0) | CIDR notation, e.g. `10.1.2.3/32` |
| `status` | Yes (or col 1) | Status; only rows with `status=open` are processed |

**Example:**
```csv
segment,status
10.1.2.3/32,open
192.168.1.1/32,open
```

Rows where `status` is not `open` (case-insensitive) are skipped silently.

## Output Format

CSV written to stdout with header `deny_cidr,open_cidr`. Each row represents a containment relationship where the deny CIDR fully contains the open CIDR.

```
deny_cidr,open_cidr
10.0.0.0/8,10.1.2.3/32
192.168.0.0/16,192.168.1.1/32
```

An open CIDR within multiple deny CIDRs produces multiple output rows (one per matching deny). If no containment relationships exist, only the header is printed.

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Success — output written to stdout |
| `1` | Error — missing required flags, file not found, or parse failure |

**Error conditions:**
- Missing `-deny-file` or `-open-file` (flags or env vars not set): prints usage to stderr, exits 1
- Deny file does not exist or cannot be opened: exits 1 with message
- Open file does not exist or cannot be opened: exits 1 with message
- An empty file, a missing header, or a partial official header causes exit 1.
- A malformed CSV record, a short record, or an invalid CIDR causes exit 1.

The parser ignores blank lines and whitespace-only lines. It accepts quoted fields that contain newlines.

The parser stops at the first other invalid record. The command writes no stdout data for an input error.

The diagnostic gives the input role and the input path. It prints the path
without quotation marks. A Windows path thus keeps its single backslashes.

## Building and Testing

```bash
# Build
go build -o cidr-compare ./cmd/cidr-compare

# Test
go test ./cmd/cidr-compare/...
go test ./pkg/cidrutil/...
```

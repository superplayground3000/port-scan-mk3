# cidr-compare

Compare open CIDR ranges against deny CIDR ranges to detect network policy violations. The tool finds all open CIDRs that fall within any deny CIDR block.

## Overview

`cidr-compare` loads deny and open CIDR sets from CSV files and reports pairs where an open CIDR is contained within a deny CIDR range. It is used in the port-scan-mk3 pipeline to cross-reference scan results against network policy rules.

**Use cases:**
- Identify open scan results that landed in policy-blocked address space.
- Audit firewall or network policy rule coverage.
- Validate that scan output doesn't include addresses that should have been filtered.

## Architecture

```
                    ┌──────────────────────────────────────┐
                    │          cidr-compare                │
                    └──────────────────────────────────────┘

  Deny CSV ──► DenyCSVReader ──► ParseCIDR ──► IntervalTree.Insert()
                                                           │
  Open CSV ──► OpenCSVReader ──► ParseCIDR ──► IntervalTree.Query()
                                                           │
                                                           ▼
                                                   Matching pairs
                                                           │
                                                           ▼
                                                     stdout
                                        (deny_cidr,open_cidr CSV)

  pkg/cidrutil
  ├── types.go         CIDREntry, MatchResult types
  ├── tree.go          IntervalTree with O(n) linear scan containment query
  ├── parser.go        DenyCSVReader, OpenCSVReader streaming parsers
```

**Components:**
- `DenyCSVReader` — streams deny CSV rows, filters `decision=deny` rows, parses CIDR to `CIDREntry`
- `OpenCSVReader` — streams open CSV rows, filters `status=open` rows, parses CIDR to `CIDREntry`
- `IntervalTree` — holds deny CIDRs as interval bounds (uint32 StartIP/EndIP); `Query` performs linear containment scan

The interval tree uses a **linear O(n) scan over all entries** on each query. For large deny sets, a balanced tree structure would improve performance. The current implementation is suitable for small-to-medium policy rule sets.

## CLI Flags

| Flag | Type | Description | Env Variable |
|------|------|-------------|--------------|
| `-deny-file` | string | Path to deny CSV file | `CIDR_COMPARE_DENY_FILE` |
| `-open-file` | string | Path to open CSV file | `CIDR_COMPARE_OPEN_FILE` |

Both flags are required. Either pass them as CLI flags or set the corresponding environment variables.

## Input/Output Formats

### Deny CSV

Expects a CSV with a header row. Columns are matched by name with fallback to column index:

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

Expects a CSV with a header row. Columns are matched by name with fallback to column index:

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

### Output

CSV written to stdout with the header `deny_cidr,open_cidr`. Each row represents a containment relationship where the deny CIDR fully contains the open CIDR.

```
deny_cidr,open_cidr
10.0.0.0/8,10.1.2.3/32
192.168.0.0/16,192.168.1.1/32
```

An open CIDR that falls within multiple deny CIDRs will produce multiple output rows (one per matching deny). If no containment relationships exist, only the header is printed.

## Usage Examples

### Basic comparison

```bash
cidr-compare -deny-file deny.csv -open-file open.csv
```

### With environment variables

```bash
export CIDR_COMPARE_DENY_FILE=deny.csv
export CIDR_COMPARE_OPEN_FILE=open.csv
cidr-compare
```

### Save output to file

```bash
cidr-compare -deny-file deny.csv -open-file open.csv > matches.csv
```

### Piping to another tool

```bash
cidr-compare -deny-file deny.csv -open-file open.csv | tail -n +2 | wc -l
```

### Using with stdin (via file redirection)

```bash
cidr-compare -deny-file deny.csv -open-file open.csv | awk -F, '{print $1}' | sort -u
```

### Real pipeline example

If `port-scan` produced `open_results.csv` and you have `policy_deny.csv`:

```bash
cidr-compare -deny-file policy_deny.csv -open-file open_results.csv
```

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Success — output written to stdout |
| `1` | Error — missing required flags, file not found, or parse failure |

**Error conditions:**
- Missing `-deny-file` or `-open-file` (flags or env vars not set): prints usage to stderr, exits 1
- Deny file does not exist or cannot be opened: exits 1 with message
- Open file does not exist or cannot be opened: exits 1 with message
- Malformed CSV rows are skipped with a warning; they do not cause exit 1
- Invalid CIDR values are skipped with a warning; they do not cause exit 1

## Building

```bash
go build -o cidr-compare ./cmd/cidr-compare
```

## Testing

```bash
go test ./cmd/cidr-compare/...
go test ./pkg/cidrutil/...
```

## Implementation Details

The implementation lives in two packages:

- `cmd/cidr-compare/` — CLI entry point, flag handling, file I/O orchestration
- `pkg/cidrutil/` — reusable domain logic:
  - `types.go` — `CIDREntry` (with StartIP/EndIP bounds), `MatchResult`
  - `tree.go` — `IntervalTree` (linear scan containment), `ParseCIDR`
  - `parser.go` — `DenyCSVReader`, `OpenCSVReader` with header-aware column mapping

`cidr-compare` respects the library-first design principle: domain logic lives in `pkg/cidrutil/` and is composed in `cmd/cidr-compare/`.
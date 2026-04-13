# cidr-compare

Compare open CIDR ranges against deny CIDR ranges to find overlaps. Uses an interval tree for efficient O(log n) lookup.

## Usage

```bash
cidr-compare -deny-file deny.csv -open-file open.csv
```

### Environment Variables

| Variable | Description |
|----------|-------------|
| `CIDR_COMPARE_DENY_FILE` | Path to deny CSV (alternative to `-deny-file`) |
| `CIDR_COMPARE_OPEN_FILE` | Path to open CSV (alternative to `-open-file`) |

## Input Formats

### Deny CSV

| Column | Description |
|--------|-------------|
| `dst_network_segment` | CIDR notation (e.g., `10.0.0.0/24`) |
| `decision` | Policy decision (informational) |

Falls back to columns 0 and 1 if headers are missing.

### Open CSV

| Column | Description |
|--------|-------------|
| `segment` | CIDR notation (e.g., `10.0.0.0/28`) |
| `status` | Status (informational) |

Falls back to columns 0 and 1 if headers are missing.

## Output

CSV to stdout with header `deny_cidr,open_cidr`. Each row represents a pair where the deny CIDR contains the open CIDR.

```
deny_cidr,open_cidr
10.0.0.0/24,10.0.0.0/28
```

## Algorithm

Uses an interval tree built from deny CIDR entries. For each open CIDR, the tree is queried to find all deny CIDRs that contain it. This provides efficient lookup compared to brute-force comparison.

```
Deny CSV ──> IntervalTree.Insert() ──┐
                                     ├──> IntervalTree.Query() ──> Matching pairs
Open CSV ──> OpenCSVReader ──────────┘         │
                                              v
                                     stdout: deny_cidr,open_cidr
```

## Examples

```bash
# Basic comparison
cidr-compare -deny-file deny.csv -open-file open.csv

# With environment variables
export CIDR_COMPARE_DENY_FILE=deny.csv
export CIDR_COMPARE_OPEN_FILE=open.csv
cidr-compare

# Save output to file
cidr-compare -deny-file deny.csv -open-file open.csv > matches.csv
```

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Success (output written to stdout) |
| `1` | Runtime error (file not found, parse error) |

## Implementation

See `pkg/cidrutil/` for the interval tree and CSV reader implementations.

---
**Revised**: 2026-04-13 | **Author**: docs-team
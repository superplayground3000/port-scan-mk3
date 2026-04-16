# Preprocess Workflow Design

**Date**: 2026-04-16
**Status**: Approved

## Problem

Port scanning requires prepared input CSVs, but the raw data comes in different
forms depending on the scan mode:

- **From scratch**: A rich CSV (per data center) with all 10 fields needs closed
  CIDRs filtered out before scanning.
- **Re-scan**: A minimal `host,port` CSV from a previous scan needs enrichment
  into rich CSV format, then the same closed-CIDR filtering.

There is no tooling today to handle either transformation.

## Solution Overview

Two separate CLI tools, each doing one thing:

1. **`cmd/enrich-targets`** — Enriches minimal `host,port` opened targets into
   rich CSV format using reference data.
2. **`cmd/preprocess`** — Filters a rich CSV by removing targets whose
   `dst_network_segment` falls within any closed CIDR, then writes a port-scan-
   ready input file.

Both tools share domain logic in `pkg/` and a centralized configuration package
for all column names and constants.

## Data Flow

```
From-scratch mode:

  filtered_targets.csv ──→ [preprocess] ──→ <out>/<fab>/<ts>/input.csv
       (rich CSV)                ↑
                        cleaned_cidrs.csv


Re-scan mode:

  opened_targets.csv ──→ [enrich-targets] ──→ enriched.csv ──→ [preprocess] ──→ <out>/<fab>/<ts>/input.csv
     (host,port)               ↑                                     ↑
                        cidrs.csv                           cleaned_cidrs.csv
                        services.csv
```

## Centralized Configuration: `pkg/preprocesscfg`

All CSV column names, placeholder values, and constant strings are defined in a
single package. Both CLI tools and their domain packages import from here. When
field names change, only this file needs updating.

```go
package preprocesscfg

// Rich CSV output columns.
var (
    ColSrcIP             = "src_ip"
    ColSrcNetworkSegment = "src_network_segment"
    ColDstIP             = "dst_ip"
    ColDstNetworkSegment = "dst_network_segment"
    ColServiceLabel      = "service_label"
    ColProtocol          = "protocol"
    ColPort              = "port"
    ColDecision          = "decision"
    ColMatchedPolicyID   = "matched_policy_id"
    ColReason            = "reason"
)

// Opened targets input columns.
var (
    ColHost      = "host"
    ColPortInput = "port"
)

// Cleaned CIDRs columns.
var (
    ColCIDR   = "cidr"
    ColStatus = "status"
)

// Service map columns.
var (
    ColServicePort = "port"
    ColServiceName = "service_label"
)

// CIDR status values.
var (
    StatusOpen  = "open"
    StatusClose = "close"
)

// Placeholder and default values for enrichment.
var (
    DefaultSrcIP             = "10.59.42.39"
    DefaultSrcNetworkSegment = "10.59.42.39/32"
    DefaultProtocol          = "tcp"
    DefaultDecision          = "accept"
    DefaultPolicyID          = "enriched"
    DefaultReason            = "MATCH_POLICY_ACCEPT"
    FallbackServiceLabel     = "unknown"
)
```

## Tool 1: `cmd/enrich-targets`

### Purpose

Transform a minimal `host,port` CSV from a previous scan into a full rich CSV
suitable for the preprocess filter.

### CLI Interface

```
Usage: enrich-targets [flags]

Flags:
  --input        Path to opened targets CSV (host,port)    [required]
  --cidr-list    Path to CIDR reference CSV                [required]
  --service-map  Path to port-to-service-label CSV         [required]
  --output       Path to write enriched rich CSV           [required]
```

Exit codes: 0 on success, 1 on fatal error.

### Input Files

**Opened targets** (`--input`):
```csv
host,port
10.0.1.5,22
10.0.1.5,80
```

**CIDR reference list** (`--cidr-list`):
A CSV listing CIDRs. For each host IP, the tool finds the smallest (most
specific) containing CIDR to use as `dst_network_segment`. If no CIDR contains
the host, falls back to `<host>/32`.

**Service map** (`--service-map`):
```csv
port,service_label
22,SSH
80,HTTP
443,HTTPS
```

If a port is not in the map, `service_label` defaults to `unknown`.

### Enrichment Field Mapping

| Rich CSV Field         | Source                                        |
|------------------------|-----------------------------------------------|
| `src_ip`               | Placeholder: `10.59.42.39`                    |
| `src_network_segment`  | Placeholder: `10.59.42.39/32`                 |
| `dst_ip`               | `host` column from input                      |
| `dst_network_segment`  | Smallest containing CIDR from reference list  |
| `service_label`        | Lookup from service map (fallback: `unknown`) |
| `protocol`             | Always `tcp`                                  |
| `port`                 | `port` column from input                      |
| `decision`             | Always `accept`                               |
| `matched_policy_id`    | Always `enriched`                             |
| `reason`               | Always `MATCH_POLICY_ACCEPT`                  |

### Domain Package: `pkg/enrich`

```go
type Enricher struct {
    cidrTree   *cidrutil.IntervalTree
    serviceMap map[int]string
}

func NewEnricher(cidrTree *cidrutil.IntervalTree, serviceMap map[int]string) *Enricher

// Enrich produces a RichRow from a host and port.
// Never skips rows — always returns a result with fallback values.
func (e *Enricher) Enrich(host string, port int) (RichRow, error)
```

CIDR lookup uses `cidrutil.IntervalTree.Query()` then selects the match with the
largest prefix length (smallest IP range) for the most specific result.

## Tool 2: `cmd/preprocess`

### Purpose

Filter a rich CSV by removing targets whose `dst_network_segment` is contained
within any closed CIDR, then write the surviving rows to a structured output
path.

### CLI Interface

```
Usage: preprocess [flags]

Flags:
  --input          Path to rich CSV                          [required]
  --cleaned-cidrs  Path to cleaned CIDRs CSV (cidr,status)  [required]
  --fab-name       Data center / fabric name                 [required]
  --output-dir     Base output directory                     [required]
```

Exit codes: 0 on success, 1 on fatal error.

### Input Files

**Rich CSV** (`--input`): Full 10-column rich CSV format (either the original
filtered targets or the output of `enrich-targets`).

**Cleaned CIDRs** (`--cleaned-cidrs`):
```csv
cidr,status
10.0.0.0/16,close
10.1.0.0/16,open
192.168.0.0/24,close
```

### Filtering Logic

1. Load cleaned CIDRs, extract those with `status = close`.
2. Build an `IntervalTree` from the closed CIDRs.
3. For each input row, parse `dst_network_segment` and query the closed tree.
4. If the closed tree contains a CIDR that encompasses the target's
   `dst_network_segment`, drop the row. Otherwise, keep it.

### Domain Package: `pkg/preprocess`

```go
type Filter struct {
    closedTree *cidrutil.IntervalTree
}

func NewFilter(closedTree *cidrutil.IntervalTree) *Filter

// Keep returns true if the target's dst_network_segment is not contained
// within any closed CIDR.
func (f *Filter) Keep(dstNetworkSegment string) (bool, error)
```

### Output

**Path**: `<output-dir>/<fab-name>/<timestamp>/input.csv`

Timestamp format: `20060102T150405Z` (UTC).

**Summary** (printed to stderr after completion):
- Total input rows
- Rows kept
- Rows dropped (with count per closed CIDR)

## Package Layout

```
pkg/
├── preprocesscfg/          # Column names, defaults, constants
│   └── config.go
├── enrich/                 # Enrichment logic
│   ├── enricher.go         # Enricher struct, Enrich() method
│   ├── loader.go           # LoadCIDRList(), LoadServiceMap()
│   └── enricher_test.go
├── preprocess/             # Filtering logic
│   ├── filter.go           # Filter struct, Keep() method
│   ├── loader.go           # LoadCleanedCIDRs()
│   ├── output.go           # OutputPath(), WriteOutput()
│   └── filter_test.go
cmd/
├── enrich-targets/
│   └── main.go             # CLI wiring only
├── preprocess/
│   └── main.go             # CLI wiring only
```

Dependencies: both `pkg/enrich` and `pkg/preprocess` depend on `pkg/cidrutil`
and `pkg/preprocesscfg`. No dependency on `pkg/input` or `pkg/scanapp`.

## Testing Strategy

### Unit Tests

**`pkg/enrich`**:
- Enrichment with full CIDR match and service label match
- Missing CIDR match (fallback to `/32`)
- Missing service label (fallback to `unknown`)
- Malformed host IP
- Duplicate input rows (pass through, no dedup)

**`pkg/preprocess`**:
- Filter keep: target CIDR not in any closed CIDR
- Filter drop: target CIDR contained in a closed CIDR
- Containment edge cases: exact match, subnet-of-subnet
- Empty closed list (keep all rows)
- All CIDRs closed (drop all rows)
- Summary output accuracy

### Integration Tests

- End-to-end CSV in → CSV out for both tools
- Verify output path structure `<dir>/<fab>/<timestamp>/input.csv`
- Round-trip: enrich then preprocess, verify final output is valid port-scan
  input

## Reuse of `cidrutil.IntervalTree`

The existing `IntervalTree.Query()` method returns all entries containing a given
CIDR. Both tools use it directly:

- **Preprocess filter**: Query with `dst_network_segment`. Any result means the
  target is in a closed CIDR — drop it.
- **Enrichment**: Query with host IP (as `/32`). Select the result with the
  largest prefix length (most specific CIDR) as `dst_network_segment`.

No modifications to `pkg/cidrutil` are required.

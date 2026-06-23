# enrich-targets Design Document

**Tool**: `cmd/enrich-targets` | **Revised**: 2026-05-03

## Architecture

```
Input CSV (host,port)
    │
    ▼
CSV Reader (host_idx, port_idx)
    │
    ▼
Row Processing Loop
    │
    ├── Validate host is IPv4
    │
    ├── Query CIDR tree for dst_network_segment (most specific match)
    │
    ├── Lookup service label from service map
    │
    └── Build RichRow
    │
    ▼
Output CSV (Rich format)
```

## Components

### Enricher (`pkg/enrich/enricher.go`)

```go
type Enricher struct {
    cidrTree   *cidrutil.IntervalTree
    serviceMap map[int]string
}
```

```go
func NewEnricher(cidrTree *cidrutil.IntervalTree, serviceMap map[int]string) *Enricher
func (e *Enricher) Enrich(host string, port int) (RichRow, error)
```

**Enrich logic:**
1. Parse and validate host as IPv4
2. Validate port range [1, 65535]
3. Query CIDR tree for containing CIDRs; select most specific (smallest range)
4. Lookup service label from map; fallback if not found
5. Return RichRow with all fields populated

### RichRow

```go
type RichRow struct {
    SrcIP             string
    SrcNetworkSegment string
    DstIP             string
    DstNetworkSegment string
    ServiceLabel      string
    Protocol          string
    Port              string
    Decision          string
    MatchedPolicyID   string
    Reason            string
}

func (r RichRow) ToSlice() []string
```

### Loader Functions (`pkg/enrich/loader.go`)

**LoadServiceMap:**
- Reads CSV with `port` and `service_label` columns (case-insensitive)
- Returns `map[int]string`
- Skips rows with invalid port numbers

**LoadCIDRList:**
- Reads CSV, first column is CIDR value
- Auto-detects header by attempting to parse first row as CIDR
- Returns `*cidrutil.IntervalTree` with all CIDRs inserted

### CIDR Tree Query

```go
func (e *Enricher) findSmallestCIDR(host string) string
```

1. Create host/32 entry
2. Query tree: `e.cidrTree.Query(entry)`
3. If no matches, return `host/32`
4. Find match with smallest range (most specific)
5. Return that CIDR's network string

## File Structure

```
cmd/enrich-targets/
├── main.go          # CLI entry, flag handling, orchestration
├── main_test.go

pkg/enrich/
├── enricher.go      # Enricher type, Enrich method, findSmallestCIDR
├── enricher_test.go
├── loader.go        # LoadServiceMap, LoadCIDRList
└── loader_test.go

pkg/preprocesscfg/
└── config.go        # Column name constants, default values
```

## Constants (pkg/preprocesscfg)

```go
var (
    ColHost      = "host"
    ColPortInput = "port"
)

var (
    DefaultSrcIP             = "10.59.42.39"
    DefaultSrcNetworkSegment = "10.59.42.39/32"
    DefaultProtocol          = "tcp"
    DefaultDecision          = "accept"
    DefaultPolicyID          = "enriched"
    DefaultReason            = "MATCH_POLICY_ACCEPT"
)
```

## Pipeline Integration

```
enrich-targets --input=opened.csv --cidr-list=cidrs.csv --service-map=services.csv --output=enriched.csv
        │
        ▼
preprocess --input=enriched.csv --cleaned-cidrs=cleaned.csv --fab-name=dc-east --output-dir=./scan-input
        │
        ▼
port-scan scan --cidr-file=scan-input/dc-east/<timestamp>/input.csv --disable-api=true
```
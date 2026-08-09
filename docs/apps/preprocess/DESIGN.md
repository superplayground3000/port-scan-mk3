# preprocess Design Document

**Tool**: `cmd/preprocess` | **Revised**: 2026-08-09

## Architecture

```
Rich CSV Input
    │
    ▼
CSV Reader (find dst_network_segment column)
    │
    ▼
Row Processing Loop
    │
    ├── Parse dst_network_segment as CIDR
    │
    ├── Query closed CIDR tree
    │
    └── If not contained → write to output
    │
    ▼
Filtered Output CSV
```

## Components

### Filter (`pkg/preprocess/filter.go`)

```go
type Filter struct {
    closedTree *cidrutil.IntervalTree
}

func NewFilter(closedTree *cidrutil.IntervalTree) *Filter
func (f *Filter) Keep(dstNetworkSegment string) (bool, error)
```

**Keep logic:**
1. Parse `dstNetworkSegment` as CIDR entry
2. Query `closedTree.Query(entry)`
3. Return `true` if no matches (not in closed tree), `false` if any match (contained in closed CIDR)

### Loader Functions (`pkg/preprocess/loader.go`)

**LoadCleanedCIDRs:**
- Reads cleaned CIDRs CSV
- Filters by `fab-name` and `status=close` (case-insensitive)
- Returns `*cidrutil.IntervalTree` with closed CIDRs inserted

### Fab Name (`pkg/preprocess/fabname.go`)

```go
type FabName struct { /* private fields */ }

func ParseFabName(name string) (FabName, error)
func (n FabName) String() string
```

`ParseFabName` validates a raw name and returns an opaque value. Only this
constructor can store a nonempty name in `FabName`.

`String` returns the original validated name. It does not change the name.

### Output Functions (`pkg/preprocess/output.go`)

**OutputPathForFabName:**

```go
func OutputPathForFabName(baseDir string, fabName FabName, ts time.Time) (string, error)
```

The function accepts the opaque value and returns this path:

```text
<baseDir>/<fabName>/<YYYYMMDDTHHMMSSZ>/input.csv
```

The function rejects the zero `FabName` value with `ErrInvalidFabName`. Each
returned path stays lexically below `baseDir`.

The function does not resolve symlinks or Windows junctions. A hostile link
inside `baseDir` can redirect file operations outside that directory.

**OutputPath:**
```go
func OutputPath(baseDir, fabName string, ts time.Time) string
// Returns: <baseDir>/<fabName>/<YYYYMMDDTHHMMSSZ>/input.csv
```

`OutputPath` remains exported for source compatibility. Its raw `fabName`
parameter has no containment guarantee. New callers use `ParseFabName` and
`OutputPathForFabName`.

`ValidateFabName` also remains exported for source compatibility. The CLI uses
`ParseFabName` before it opens an input file. The CLI flags and output layout do
not change.

**CreateOutputWriter:**
- Creates all parent directories with `os.MkdirAll`
- Returns `*csv.Writer` and `*os.File`
- Caller must Flush and Close

**PrintSummary:**
- Writes human-readable filter summary to stderr

## File Structure

```
cmd/preprocess/
├── main.go          # CLI entry, flag handling, orchestration
├── main_test.go

pkg/preprocess/
├── filter.go        # Filter type, Keep method
├── filter_test.go
├── fabname.go       # FabName type, parser, and compatibility validator
├── fabname_test.go
├── loader.go        # LoadCleanedCIDRs
├── loader_test.go
├── output.go        # Typed and compatibility output paths, CSV output
└── output_test.go

pkg/preprocesscfg/
└── config.go        # Column name constants, status values
```

## Pipeline Integration

### From-Scratch Flow

```
preprocess --input=<filtered-targets>/<fab>/<ts>/opened_targets.csv \
           --cleaned-cidrs=cleaned_cidrs.csv \
           --fab-name=<fab> \
           --output-dir=./scan-input
        │
        ▼
port-scan scan --cidr-file=scan-input/<fab>/<ts>/input.csv --disable-api=true
```

### Re-scan Flow

```
enrich-targets --input=opened.csv --cidr-list=cidrs.csv --service-map=services.csv --output=enriched.csv
        │
        ▼
preprocess --input=enriched.csv --cleaned-cidrs=cleaned.csv --fab-name=<fab> --output-dir=./scan-input
        │
        ▼
port-scan scan --cidr-file=scan-input/<fab>/<ts>/input.csv --disable-api=true
```

## CIDR Status Values (pkg/preprocesscfg)

```go
var (
    StatusOpen  = "open"
    StatusClose = "close"
)
```

## Column Names (pkg/preprocesscfg)

```go
var (
    ColDstNetworkSegment = "dst_network_segment"
    ColFab               = "fab"
    ColCIDR              = "segment"
    ColStatus            = "status"
)
```

# cidr-compare Design Document

**Tool**: `cmd/cidr-compare` | **Revised**: 2026-05-03

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
```

## Components

### DenyCSVReader (`pkg/cidrutil/parser.go`)

Streams deny CSV rows, filters `decision=deny` rows, parses CIDR to `CIDREntry`.

```go
type DenyCSVReader struct {
    // ...
}
func (r *DenyCSVReader) Read() (CIDREntry, error)
```

### OpenCSVReader (`pkg/cidrutil/parser.go`)

Streams open CSV rows, filters `status=open` rows, parses CIDR to `CIDREntry`.

```go
type OpenCSVReader struct {
    // ...
}
func (r *OpenCSVReader) Read() (CIDREntry, error)
```

### IntervalTree (`pkg/cidrutil/tree.go`)

Holds deny CIDRs as interval bounds (uint32 StartIP/EndIP). `Insert()` adds a CIDR entry. `Query()` performs linear O(n) containment scan.

```go
type IntervalTree struct {
    entries []CIDREntry
}

func (t *IntervalTree) Insert(entry CIDREntry)
func (t *IntervalTree) Query(entry CIDREntry) []CIDREntry
```

**Algorithm**: Linear scan over all entries. For each deny entry, checks if query CIDR's range falls within the deny entry's range. Returns all matching deny CIDRs.

**Complexity**: O(n) for both insert and query where n = number of deny entries.

### CIDREntry (`pkg/cidrutil/types.go`)

```go
type CIDREntry struct {
    Network    string  // CIDR notation string, e.g. "10.0.0.0/8"
    StartIP    uint32  // Numeric start IP (uint32 representation)
    EndIP      uint32  // Numeric end IP (uint32 representation)
}
```

### ParseCIDR (`pkg/cidrutil/tree.go`)

Converts CIDR string to `CIDREntry` with numeric bounds.

```go
func ParseCIDR(cidr string) (CIDREntry, error)
```

### MatchResult (`pkg/cidrutil/types.go`)

```go
type MatchResult struct {
    DenyCIDR  string
    OpenCIDR  string
}
```

## Processing Flow

1. Load all deny CIDRs into `IntervalTree` via `Insert()`
2. Stream open CIDRs via `OpenCSVReader`
3. For each open CIDR entry, call `tree.Query(entry)`
4. For each match, output a row: `deny_cidr,open_cidr`

## Implementation Notes

- Streaming parsers avoid loading entire file into memory
- Column matching by name with positional fallback for robustness
- Malformed/invalid rows are skipped with warnings, not errors
- The interval tree uses a **linear O(n) scan** over all entries on each query

## File Structure

```
cmd/cidr-compare/
├── main.go              # CLI entry point, flag handling, file I/O
├── main_test.go
└── integration_test.go

pkg/cidrutil/
├── types.go             # CIDREntry, MatchResult types
├── tree.go              # IntervalTree with ParseCIDR
├── parser.go            # DenyCSVReader, OpenCSVReader
├── parser_test.go
├── tree_test.go
└── types_test.go
```
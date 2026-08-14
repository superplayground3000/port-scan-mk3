# cidr-compare Design Document

**Tool**: `cmd/cidr-compare` | **Revised**: 2026-08-14

## Architecture

```
                    ┌──────────────────────────────────────┐
                    │          cidr-compare                │
                    └──────────────────────────────────────┘

  Deny CSV ──► DenyCSVReader ──► parseCSV ──► ParseCIDR ──► IntervalTree.Insert()
                                                           │
  Open CSV ──► OpenCSVReader ──► parseCSV ──► ParseCIDR ──► IntervalTree.Query()
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

This compatibility adapter passes its `io.Reader` and the deny policy to `parseCSV`.

```go
type DenyCSVReader struct {
    // ...
}
func (r *DenyCSVReader) ReadAll() ([]CIDREntry, error)
```

### OpenCSVReader (`pkg/cidrutil/parser.go`)

This compatibility adapter passes its `io.Reader` and the open policy to `parseCSV`.

```go
type OpenCSVReader struct {
    // ...
}
func (r *OpenCSVReader) ReadAll() ([]CIDREntry, error)
```

### parseCSV (`pkg/cidrutil/parser.go`)

The unexported parser uses `encoding/csv.Reader`. It accepts an `io.Reader` and a small role-specific policy.

The string adapters use `strings.NewReader`. Thus, all four public entry points use the same parser and error contract.

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

1. Parse both CSV inputs before stdout receives data.
2. Load all deny CIDRs into `IntervalTree` with `Insert()`.
3. For each open CIDR entry, call `tree.Query(entry)`.
4. For each match, write a `deny_cidr,open_cidr` row.

## Implementation Notes

- The canonical parser reads complete CSV records without another full-file copy.
- Both official headers select name-based columns. No official headers select legacy columns 0 and 1.
- One official header is an error. Empty input and a missing header are errors.
- Blank lines are valid. The parser stops at the first other invalid record.
- The parser returns errors to its caller. It does not write to the global logger.
- The command writes no stdout data until both inputs are valid.
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

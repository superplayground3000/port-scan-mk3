# CSV Support for xlsx-transform — Design

**Status:** Historical

**Current architecture:** [port-scan design](../apps/port-scan/DESIGN.md)

## Overview

Add transparent CSV file support to `xlsx-transform` via a new `pkg/spreadsheet` package that auto-detects `.xlsx` vs `.csv` by content and returns the same `[][]string` output for both.

## Goals

- Users can pass either `.xlsx` or `.csv` files to `xlsx-transform --input` without format flags
- Format is determined by content detection (PK magic for xlsx, plain text for CSV)
- Strict extension matching: `.csv` content in a `.xlsx` file (or vice versa) is rejected
- Library-first: domain logic lives in `pkg/spreadsheet`, CLI wiring unchanged in concept

## Non-Goals

- No streaming API (files are small per spec)
- No support for tab/semicolon/pipe delimiters — only comma
- No CSV dialect negotiation
- `pkg/xlsx` remains xlsx-only; no changes to its API or consumers

## Package: `pkg/spreadsheet`

### Types

```go
// Reader reads both .xlsx and .csv files, returning worksheet rows as [][]string.
// Format is auto-detected by content; extension must match detected format.
type Reader struct{ path string }

// NewReader returns a Reader for the given file path.
// The file must have a .xlsx or .csv extension.
func NewReader(path string) *Reader

// OpenSheet opens the file and returns all rows from the worksheet.
// For .xlsx: sheetName selects the worksheet.
// For .csv: sheetName is ignored (treated as single-sheet).
// Returns ErrNotXLSX, ErrNotCSV, or ErrExtensionMismatch on detection failure.
func (r *Reader) OpenSheet(sheetName string) ([][]string, error)
```

### Sentinel Errors

```go
var ErrNotXLSX           = errors.New("file is not a valid xlsx workbook")
var ErrNotCSV             = errors.New("file is not a valid csv file")
var ErrExtensionMismatch  = errors.New("file extension does not match detected format")
```

### Detection Logic

1. Read first 2 bytes of the file
2. If `0x50 0x4B` (PK ZIP magic) → treat as xlsx; extension must be `.xlsx`
3. Otherwise → treat as CSV; extension must be `.csv`
4. Extension mismatch at any point → `ErrExtensionMismatch`

### CSV Dialect

- Comma-delimited
- First row is a header (returned as data rows, same as xlsx GetRows)
- RFC-4180-like quoting: double-quote wrapping for fields containing commas/newlines
- LF line endings (\n); CRLF accepted
- Empty lines are skipped

## File Layout

```
pkg/spreadsheet/
  reader.go       — Reader, NewReader, OpenSheet, content detection
  reader_test.go — format detection, extension mismatch, E2E
  csv.go         — readCSV helper, row parsing
  csv_test.go    — CSV unit tests
```

## Integration with `cmd/xlsx-transform`

- In `main.go`: replace `xlsx.NewReader(cfg.Input)` with `spreadsheet.NewReader(cfg.Input)`
- `pkg/xlsx` stays unchanged; `xlsx-transform` is the only consumer that switches
- Drop-in replacement: same `*Reader` interface (both have `OpenSheet(name string) ([][]string, error)`)

## Testing

- `TestReadCSV_ValidFile` — well-formed CSV with headers
- `TestReadCSV_QuotedFields` — fields containing commas, double-quotes
- `TestReadCSV_MissingFile` — file not found
- `TestReader_DetectXLSX` — PK magic detected as xlsx, extension validated
- `TestReader_DetectCSV` — plain text detected as CSV, extension validated
- `TestReader_ExtensionMismatch` — `.csv` file with PK magic (or vice versa) → `ErrExtensionMismatch`
- E2E test in `cmd/xlsx-transform/main_test.go` with CSV input (same schema: Host, Port, Pass the test)

## Constitution Alignment

| Principle | Compliance |
|-----------|------------|
| Library-First Design | Domain logic in `pkg/spreadsheet`, zero pipeline dependencies |
| SOLID Boundaries | `pkg/spreadsheet` owns detection; xlsx/csv reading in separate files |
| Test-First Delivery | Failing tests written before implementation |
| Dependency Minimalism | No new third-party deps; stdlib `encoding/csv` for CSV |
| CLI Contract-First | No CLI flag changes; format auto-detected |

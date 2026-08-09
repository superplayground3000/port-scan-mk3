# SPEC-09: Writer System Specification

## Overview

```
pkg/writer/
├── csv_writer.go           # Core CSV writing
├── csv_writer_test.go      # Unit tests
├── csv_writer_contract_test.go  # Contract tests
├── open_writer.go          # Filter for "open" only
└── open_writer_test.go     # Unit tests
```

## 1. Core Data Structure

### Record

```go
type Record struct {
    IP                string  // Target IP
    IPCidr            string  // CIDR boundary
    Port              int     // Target port
    Status            string  // "open", "close", "close(timeout)", "error(local)", "unknown"
    ResponseMS        int64   // Response time in ms (only for "open")
    FabName           string  // From input (optional)
    CIDR              string  // Legacy: alias for IPCidr
    CIDRName          string  // From input (optional)
    ServiceLabel      string  // Rich: service label
    Decision          string  // Rich: accept/deny
    PolicyID          string  // Rich: policy ID
    Reason            string  // Rich: decision reason
    ExecutionKey      string  // Rich: dst_ip:port/protocol
    SrcIP             string  // Rich: source IP
    SrcNetworkSegment string  // Rich: source network
}
```

## 2. CSV Writer

### Constructor

```go
func NewCSVWriter(out io.Writer) *CSVWriter
```

### Write Method

```go
func (w *CSVWriter) Write(r Record) error
```

**Behavior:**
1. If header not written: call `WriteHeader()` first
2. Write record as CSV row
3. Flush underlying writer

### WriteHeader Method

```go
func (w *CSVWriter) WriteHeader() error
```

**CSV Columns (fixed order):**
```
ip, ip_cidr, port, status, response_time_ms, fab_name, cidr_name,
service_label, decision, policy_id, reason, execution_key, src_ip, src_network_segment
```

**Idempotent:** Tracks `wroteHeader` flag - subsequent calls are no-ops.

### Column Definitions

```go
type ColumnDef struct {
    Header string
    Extract func(Record) string
}
```

**Extraction functions:**
```go
var columns = []ColumnDef{
    {Header: "ip", Extract: func(r Record) string { return r.IP }},
    {Header: "ip_cidr", Extract: func(r Record) string { 
        if r.IPCidr != "" { return r.IPCidr }
        return r.CIDR  // Fallback to legacy field
    }},
    // ... etc
}
```

## 3. Open-Only Writer (Filter)

### Constructor

```go
func NewOpenOnlyWriter(inner *CSVWriter) *OpenOnlyWriter
```

### Write Method

```go
func (w *OpenOnlyWriter) Write(r Record) error
```

**Behavior:**
- If `r.Status == "open"`: forward to inner writer
- Otherwise: silently drop (no write)

### Nil Safety

```go
func (w *OpenOnlyWriter) Write(r Record) error {
    if w == nil || w.inner == nil {
        return nil  // No-op
    }
    // ... normal logic
}
```

## 4. Output Files

### Two Output Writers

| Writer | File | Content |
|--------|------|---------|
| `CSVWriter` | `scan_results-*.csv` | All scan results |
| `OpenOnlyWriter` | `opened_results-*.csv` | Only "open" ports |

### Usage in Orchestration

```go
// Create main writer (all results)
allWriter := writer.NewCSVWriter(scanFile)

// Create filtered writer (open only)
openWriter := writer.NewOpenOnlyWriter(allWriter)

// Write to both
allWriter.Write(record)    // Always writes
openWriter.Write(record)   // Writes only if open
```

## 5. Batch Output

### File Naming

```
scan_results-YYYYMMDDTHHMMSSZ.csv
opened_results-YYYYMMDDTHHMMSSZ.csv
```

### Collision Handling

If file exists in same second:
```
scan_results-20240318T123456Z.csv
scan_results-20240318T123456Z-1.csv
scan_results-20240318T123456Z-2.csv
```

### Direct-Write Pattern (no `.tmp`)

Scan and open-only results are written DIRECTLY to their final paths — there is
no `.tmp` intermediate and no rename-on-success step. This makes results durable
on a graceful Ctrl+C (every already-scanned row survives) and lets `-resume`
reopen the same file in append mode:

- Fresh run: `os.Create` + `NewCSVWriter` (header written on first `Write`).
- Resume (append): `O_APPEND|O_CREATE` + `NewCSVWriterAppending` (header assumed
  present); if the file is missing/empty, fall back to `NewCSVWriter` so the
  header is recreated.

(The `unreachable_results` writer used by `pre-ping` still uses the `.tmp` +
rename pattern; only the scan/open result files changed.)

### NewCSVWriterAppending

```go
func NewCSVWriterAppending(out io.Writer) *CSVWriter
```

Returns a `CSVWriter` pre-marked as having written its header, so the first
`Write` (and any `WriteHeader`) emits no header row. Use it when appending to a
file that already contains the canonical header.

## 6. Writer Contract

| Rule | Description |
|------|-------------|
| Header first | Automatically written on first `Write()` |
| One-time header | Subsequent `WriteHeader()` calls are no-ops |
| Auto-flush | Each `Write()` flushes underlying writer |
| IPCidr fallback | If `IPCidr` empty, falls back to `CIDR` field |
| Nil-safe | OpenOnlyWriter handles nil gracefully |

## 7. Adding New Output Fields

### Step 1: Add field to Record

```go
type Record struct {
    // ... existing fields
    NewField string  // Add new field
}
```

### Step 2: Add column definition

```go
var columns = []ColumnDef{
    // ... existing
    {Header: "new_field", Extract: func(r Record) string {
        return r.NewField
    }},
}
```

### Step 3: Update orchestration

Pass new field data through:
- `scanResult` → `result_aggregator.go`
- `writeScanRecord()` → `batch_output.go`

## 8. Implementation Files Reference

| File | Responsibility |
|------|----------------|
| `pkg/writer/csv_writer.go` | Core CSV writing |
| `pkg/writer/open_writer.go` | Filter for "open" only |
| `pkg/writer/csv_writer_contract_test.go` | Contract verification |
| `pkg/scanapp/batch_output.go` | Batch path resolution |
| `pkg/scanapp/result_aggregator.go` | Record population |

## 9. Integration Points

- **Orchestration**: `result_aggregator.writeScanRecord()` uses both writers
- **Record**: Populated from `scanResult` with rich metadata
- **Batch**: File paths from `batch_output.go`
- **Finalize**: Renamed on success in `output_files.go`

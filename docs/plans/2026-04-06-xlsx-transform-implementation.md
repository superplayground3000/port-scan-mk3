# xlsx-transform Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement `xlsx-transform`, a standalone binary that reads an xlsx file and outputs a Rich CSV for the port-scan pipeline.

**Architecture:** Library-first: `pkg/xlsx` handles xlsx reading with zero scan-pipeline dependencies. `cmd/xlsx-transform` wires CLI flags, env var config, host resolution, port expansion, and filtering into a Rich CSV writer.

**Tech Stack:** Go 1.24, `github.com/xuri/excelize/v2` for xlsx reading, stdlib `net`, `encoding/csv`, `flag`, `fmt`.

---

## Task 1: Add xlsx dependency

**Files:**
- Modify: `go.mod`

**Step 1: Add excelize dependency**

Run: `go get github.com/xuri/excelize/v2@latest`
Expected: Adds `github.com/xuri/excelize/v2` to go.mod and go.sum

**Step 2: Tidy modules**

Run: `go mod tidy`
Expected: Clean go.mod, go.sum updated

**Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "deps: add github.com/xuri/excelize/v2 for xlsx reading

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

## Task 2: Create pkg/xlsx package scaffold

**Files:**
- Create: `pkg/xlsx/reader.go`
- Create: `pkg/xlsx/reader_test.go`

**Step 1: Write the failing test**

```go
// pkg/xlsx/reader_test.go
package xlsx

import (
    "os"
    "testing"
)

func TestReader_OpenSheet_NotFound(t *testing.T) {
    r := NewReader("nonexistent.xlsx")
    _, err := r.OpenSheet("Sheet1")
    if err == nil {
        t.Fatal("expected error opening nonexistent file, got nil")
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/xlsx/... -v`
Expected: FAIL — "undefined: NewReader"

**Step 3: Write minimal implementation**

```go
// pkg/xlsx/reader.go
package xlsx

import "github.com/xuri/excelize/v2"

// Reader opens xlsx files and reads worksheet data.
type Reader struct {
    path string
}

// NewReader returns a Reader for the given xlsx file path.
func NewReader(path string) *Reader {
    return &Reader{path: path}
}

// OpenSheet opens the named worksheet and returns its rows as [][]string.
// Each row is a slice of cell values in column order.
func (r *Reader) OpenSheet(name string) ([][]string, error) {
    f, err := excelize.OpenFile(r.path)
    if err != nil {
        return nil, err
    }
    defer f.Close()

    rows, err := f.GetRows(name)
    if err != nil {
        return nil, err
    }
    // Convert [][]string rows to [][]string (already correct type)
    return rows, nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./pkg/xlsx/... -v`
Expected: PASS

**Step 5: Commit**

```bash
git add pkg/xlsx/reader.go pkg/xlsx/reader_test.go go.mod go.sum
git commit -m "feat: add pkg/xlsx with xlsx reader

NewReader opens an xlsx file. OpenSheet reads a worksheet
into [][]string rows.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

## Task 3: Add xlsx reader tests for GetRows and sheet-not-found

**Files:**
- Modify: `pkg/xlsx/reader_test.go`

**Step 1: Write tests for GetRows behavior**

```go
func TestReader_GetRows_SheetNotFound(t *testing.T) {
    // Use a real xlsx file (will create test fixture in testdata/)
    r := NewReader("testdata/sample.xlsx")
    _, err := r.OpenSheet("NonExistentSheet")
    if err == nil {
        t.Fatal("expected error for nonexistent sheet, got nil")
    }
}

func TestReader_GetRows_OpensValidSheet(t *testing.T) {
    r := NewReader("testdata/sample.xlsx")
    rows, err := r.OpenSheet("all-runs")
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if len(rows) == 0 {
        t.Fatal("expected at least header row")
    }
}
```

**Step 2: Create test fixture**

Create directory `pkg/xlsx/testdata/` and add a minimal xlsx file there. Use excelize to generate it programmatically in a setup step, or create a simple CSV-to-xlsx conversion. For simplicity, generate a minimal xlsx in a test helper.

**Step 3: Run tests**

Run: `go test ./pkg/xlsx/... -v`
Expected: Tests pass (requires creating a valid xlsx fixture — can use excelize in a setup script)

**Step 4: Commit**

```bash
git add pkg/xlsx/reader_test.go pkg/xlsx/testdata/
git commit -m "test: add pkg/xlsx reader tests for sheet and file errors

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

## Task 4: Implement transform.go — port splitting, host resolution, filtering

**Files:**
- Create: `cmd/xlsx-transform/transform.go`
- Create: `cmd/xlsx-transform/transform_test.go`

**Step 1: Write failing tests for SplitPorts**

```go
// cmd/xlsx-transform/transform_test.go
package main

import (
    "reflect"
    "testing"
)

func TestSplitPorts(t *testing.T) {
    tests := []struct {
        input    string
        expected []int
        wantErr  bool
    }{
        {"80", []int{80}, false},
        {"80/443", []int{80, 443}, false},
        {"22/80/443/8080", []int{22, 80, 443, 8080}, false},
        {"", nil, false}, // empty → skip
        {"abc", nil, false}, // invalid → skip (logged, not error)
        {"80/abc", nil, false}, // partial invalid → skip
    }

    for _, tt := range tests {
        got, err := SplitPorts(tt.input)
        if (err != nil) != tt.wantErr {
            t.Errorf("SplitPorts(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
            continue
        }
        if !reflect.DeepEqual(got, tt.expected) {
            t.Errorf("SplitPorts(%q) = %v, want %v", tt.input, got, tt.expected)
        }
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/xlsx-transform/... -v -run TestSplitPorts`
Expected: FAIL — "undefined: SplitPorts"

**Step 3: Write minimal SplitPorts implementation**

```go
// cmd/xlsx-transform/transform.go

// SplitPorts splits a "/"-separated port string into individual port integers.
// Empty or invalid port strings return nil (caller skips the row).
func SplitPorts(portStr string) ([]int, error) {
    if portStr == "" {
        return nil, nil
    }
    parts := strings.Split(portStr, "/")
    ports := make([]int, 0, len(parts))
    for _, p := range parts {
        p = strings.TrimSpace(p)
        port, err := strconv.Atoi(p)
        if err != nil {
            // Invalid port — skip silently (log to stderr per spec)
            fmt.Fprintf(os.Stderr, "skipping invalid port value: %q\n", p)
            return nil, nil
        }
        ports = append(ports, port)
    }
    return ports, nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./cmd/xlsx-transform/... -v -run TestSplitPorts`
Expected: PASS

**Step 5: Write failing test for ResolveHost**

```go
func TestResolveHost(t *testing.T) {
    tests := []struct {
        input    string
        expected string
    }{
        {"192.168.1.1", "192.168.1.1"},           // IP passthrough
        {"8.8.8.8", "8.8.8.8"},                    // IP passthrough
        {"localhost", "127.0.0.1"},                 // hostname resolves (or hostname string on failure)
    }

    for _, tt := range tests {
        got, err := ResolveHost(tt.input)
        if err != nil {
            t.Errorf("ResolveHost(%q) error = %v", tt.input, err)
            continue
        }
        if got == "" {
            t.Errorf("ResolveHost(%q) returned empty string", tt.input)
        }
        // For IPs, result should equal input
        if net.ParseIP(tt.input) != nil && got != tt.input {
            t.Errorf("ResolveHost(%q) = %q, want %q (IP should passthrough)", tt.input, got, tt.input)
        }
    }
}
```

**Step 6: Run test to verify it fails**

Run: `go test ./cmd/xlsx-transform/... -v -run TestResolveHost`
Expected: FAIL — "undefined: ResolveHost"

**Step 7: Write ResolveHost implementation**

```go
// ResolveHost resolves a host (IP or hostname) to an IPv4 string.
// If host is already a valid IPv4, it is returned as-is.
// If host is a hostname, net.LookupIP is called; on failure the
// original hostname string is returned (downstream validation will catch it).
func ResolveHost(host string) (string, error) {
    host = strings.TrimSpace(host)
    if host == "" {
        return "", nil // empty → skip silently
    }
    ip := net.ParseIP(host)
    if ip != nil && ip.To4() != nil {
        return ip.String(), nil
    }
    // Hostname — try DNS resolution
    addrs, err := net.LookupIP(host)
    if err != nil || len(addrs) == 0 {
        // Resolution failed — use hostname as-is per spec
        return host, nil
    }
    for _, addr := range addrs {
        if v4 := addr.To4(); v4 != nil {
            return v4.String(), nil
        }
    }
    // No IPv4 found — use hostname as-is
    return host, nil
}
```

**Step 8: Run test to verify it passes**

Run: `go test ./cmd/xlsx-transform/... -v -run TestResolveHost`
Expected: PASS

**Step 9: Write failing test for ShouldIncludeRow**

```go
func TestShouldIncludeRow(t *testing.T) {
    tests := []struct {
        passVal  string
        expected bool
    }{
        {"TRUE", false},
        {"true", false},
        {"TRUE ", false},
        {"FALSE", true},
        {"false", true},
        {"FALSE ", true},
        {"PASS", false},    // not FALSE
        {"", false},        // empty → skip
        {"UNKNOWN", false}, // not FALSE → skip
    }

    for _, tt := range tests {
        got := ShouldIncludeRow(tt.passVal)
        if got != tt.expected {
            t.Errorf("ShouldIncludeRow(%q) = %v, want %v", tt.passVal, got, tt.expected)
        }
    }
}
```

**Step 10: Run test to verify it fails**

Run: `go test ./cmd/xlsx-transform/... -v -run TestShouldIncludeRow`
Expected: FAIL — "undefined: ShouldIncludeRow"

**Step 11: Write ShouldIncludeRow implementation**

```go
// ShouldIncludeRow returns true if the row should be included in output.
// Only rows where passVal is "FALSE" (case-insensitive, trimmed) are included.
func ShouldIncludeRow(passVal string) bool {
    return strings.EqualFold(strings.TrimSpace(passVal), "FALSE")
}
```

**Step 12: Run test to verify it passes**

Run: `go test ./cmd/xlsx-transform/... -v -run TestShouldIncludeRow`
Expected: PASS

**Step 13: Commit**

```bash
git add cmd/xlsx-transform/transform.go cmd/xlsx-transform/transform_test.go
git commit -m "feat: implement transform logic — port splitting, host resolution, filtering

SplitPorts: splits /-separated port strings, skips invalid ports (logged to stderr)
ResolveHost: IPv4 passthrough, hostname → net.LookupIP, fallback to hostname on failure
ShouldIncludeRow: include only rows where pass column is FALSE (case-insensitive)

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

## Task 5: Implement main.go — CLI entry point

**Files:**
- Create: `cmd/xlsx-transform/main.go`
- Create: `cmd/xlsx-transform/main_test.go`

**Step 1: Write failing test for flag parsing and config**

```go
// cmd/xlsx-transform/main_test.go
package main

import (
    "os"
    "testing"
)

func TestTransformConfig_FromFlags(t *testing.T) {
    // Test that required flags are enforced
    os.Args = []string{"xlsx-transform"} // no flags set
    cfg, err := ParseConfig()
    if err == nil {
        t.Fatal("expected error for missing required --input and --output, got nil")
    }
    if cfg != nil {
        t.Errorf("expected nil config on error, got %+v", cfg)
    }
}

func TestTransformConfig_EnvVarOverride(t *testing.T) {
    os.Setenv("TRANSFORM_INPUT", "/path/to/input.xlsx")
    os.Setenv("TRANSFORM_OUTPUT", "/path/to/output.csv")
    defer os.Unsetenv("TRANSFORM_INPUT")
    defer os.Unsetenv("TRANSFORM_OUTPUT")

    os.Args = []string{"xlsx-transform"}
    cfg, err := ParseConfig()
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if cfg.Input != "/path/to/input.xlsx" {
        t.Errorf("Input = %q, want %q", cfg.Input, "/path/to/input.xlsx")
    }
    if cfg.Output != "/path/to/output.csv" {
        t.Errorf("Output = %q, want %q", cfg.Output, "/path/to/output.csv")
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/xlsx-transform/... -v -run TestTransformConfig`
Expected: FAIL — "undefined: ParseConfig, TransformConfig"

**Step 3: Write TransformConfig and ParseConfig**

```go
// cmd/xlsx-transform/main.go
package main

import (
    "flag"
    "fmt"
    "os"
    "strings"
)

// TransformConfig holds all configuration for the transform.
type TransformConfig struct {
    Input     string // Path to input xlsx file (required)
    Output    string // Path to output CSV file (required)
    SheetName string // Worksheet name (default: all-runs)
    HostCol   string // Host column name (default: Host)
    PortCol   string // Port column name (default: Port)
    PassCol   string // Pass/fail column name (default: Pass the test)
}

// envOrDefault returns the environment variable value or the default.
func envOrDefault(envKey, defaultVal string) string {
    if val := os.Getenv(envKey); val != "" {
        return val
    }
    return defaultVal
}

// ParseConfig parses CLI flags and environment variables into TransformConfig.
// Exits with code 2 if required flags are missing.
func ParseConfig() (*TransformConfig, error) {
    cfg := &TransformConfig{
        SheetName: envOrDefault("TRANSFORM_SHEET_NAME", "all-runs"),
        HostCol:   envOrDefault("TRANSFORM_HOST_COL", "Host"),
        PortCol:   envOrDefault("TRANSFORM_PORT_COL", "Port"),
        PassCol:   envOrDefault("TRANSFORM_PASS_COL", "Pass the test"),
    }

    flag.StringVar(&cfg.Input, "input", "", "Path to input xlsx file (required)")
    flag.StringVar(&cfg.Output, "output", "", "Path to output CSV file (required)")
    flag.StringVar(&cfg.SheetName, "sheet", cfg.SheetName, "Worksheet name")
    flag.StringVar(&cfg.HostCol, "host-col", cfg.HostCol, "Host column name")
    flag.StringVar(&cfg.PortCol, "port-col", cfg.PortCol, "Port column name")
    flag.StringVar(&cfg.PassCol, "pass-col", cfg.PassCol, "Pass/fail column name")

    flag.Usage = func() {
        fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s [options]\n", os.Args[0])
        flag.PrintDefaults()
    }

    flag.Parse()

    if cfg.Input == "" {
        return nil, fmt.Errorf("missing required --input flag")
    }
    if cfg.Output == "" {
        return nil, fmt.Errorf("missing required --output flag")
    }

    return cfg, nil
}

func main() {
    os.Exit(runMain(os.Args, os.Stdout, os.Stderr))
}

func runMain(args []string, stdout, stderr io.Writer) int {
    cfg, err := ParseConfig()
    if err != nil {
        fmt.Fprintln(stderr, "error:", err)
        return 2
    }

    if err := runTransform(cfg); err != nil {
        fmt.Fprintln(stderr, "error:", err)
        return 1
    }

    fmt.Fprintln(stdout, "transform complete:", cfg.Output)
    return 0
}
```

**Step 4: Add runTransform stub**

```go
func runTransform(cfg *TransformConfig) error {
    // TODO: implement
    return nil
}
```

**Step 5: Run test to verify it fails on unimplemented runTransform (or passes if stub returns nil)**

Run: `go test ./cmd/xlsx-transform/... -v -run TestTransformConfig`
Expected: Tests pass (stub returns nil)

**Step 6: Write failing test for real runTransform**

```go
func TestRunTransform_InputNotFound(t *testing.T) {
    cfg := &TransformConfig{
        Input:  "/nonexistent/file.xlsx",
        Output: t.TempDir() + "/out.csv",
    }
    err := runTransform(cfg)
    if err == nil {
        t.Fatal("expected error for nonexistent input file, got nil")
    }
}
```

**Step 7: Run test — should fail with unimplemented or unimpressive stub**

Run: `go test ./cmd/xlsx-transform/... -v -run TestRunTransform`
Expected: FAIL (depends on stub)

**Step 8: Write full runTransform**

```go
func runTransform(cfg *TransformConfig) error {
    // 1. Open xlsx
    r := xlsx.NewReader(cfg.Input)
    rows, err := r.OpenSheet(cfg.SheetName)
    if err != nil {
        return fmt.Errorf("failed to open xlsx: %w", err)
    }
    if len(rows) < 2 {
        return nil // no data rows, empty output
    }

    // 2. Build column index from header
    header := rows[0]
    colIdx := map[string]int{}
    for i, col := range header {
        colIdx[strings.TrimSpace(col)] = i
    }

    hostIdx, ok := colIdx[cfg.HostCol]
    if !ok {
        return fmt.Errorf("required column not found: %s", cfg.HostCol)
    }
    portIdx, ok := colIdx[cfg.PortCol]
    if !ok {
        return fmt.Errorf("required column not found: %s", cfg.PortCol)
    }
    passIdx, ok := colIdx[cfg.PassCol]
    if !ok {
        return fmt.Errorf("required column not found: %s", cfg.PassCol)
    }

    // 3. Build Rich CSV records
    const (
        outSrcIP        = "10.0.0.1"
        outSrcSeg       = "10.0.0.0/24"
        outDstSeg       = "10.0.0.0/24"
        outServiceLabel = "unknown"
        outProtocol     = "tcp"
        outDecision     = "accept"
        outPolicyID     = "transformed"
        outReason       = "MATCH_POLICY_ACCEPT"
    )

    var outRecords [][]string
    for rowIdx, row := range rows[1:] {
        realRowIdx := rowIdx + 2 // 1-indexed, skip header

        if len(row) <= passIdx || len(row) <= hostIdx || len(row) <= portIdx {
            continue // skip malformed rows
        }

        passVal := strings.TrimSpace(row[passIdx])
        if !ShouldIncludeRow(passVal) {
            continue
        }

        host := strings.TrimSpace(row[hostIdx])
        if host == "" {
            continue
        }

        portStr := strings.TrimSpace(row[portIdx])
        if portStr == "" {
            continue
        }

        ports, err := SplitPorts(portStr)
        if err != nil || len(ports) == 0 {
            continue
        }

        dstIP, _ := ResolveHost(host) // never returns error, hostname on failure

        for _, port := range ports {
            outRecords = append(outRecords, []string{
                outSrcIP, outSrcSeg, dstIP, outDstSeg,
                outServiceLabel, outProtocol,
                strconv.Itoa(port), outDecision,
                outPolicyID, outReason,
            })
        }
    }

    // 4. Write CSV
    f, err := os.Create(cfg.Output)
    if err != nil {
        return fmt.Errorf("failed to create output file: %w", err)
    }
    defer f.Close()

    w := csv.NewWriter(f)
    defer w.Flush()

    header := []string{
        "src_ip", "src_network_segment", "dst_ip", "dst_network_segment",
        "service_label", "protocol", "port", "decision",
        "matched_policy_id", "reason",
    }
    if err := w.Write(header); err != nil {
        return fmt.Errorf("failed to write CSV header: %w", err)
    }
    for _, rec := range outRecords {
        if err := w.Write(rec); err != nil {
            return fmt.Errorf("failed to write CSV row: %w", err)
        }
    }

    return nil
}
```

**Step 9: Run all tests**

Run: `go test ./cmd/xlsx-transform/... -v`
Expected: PASS

**Step 10: Commit**

```bash
git add cmd/xlsx-transform/main.go cmd/xlsx-transform/main_test.go
git commit -m "feat: add cmd/xlsx-transform CLI entry point

CLI with --input, --output, --sheet, --host-col, --port-col, --pass-col flags.
All flags support TRANSFORM_* env var overrides per spec.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

## Task 6: End-to-end integration test

**Files:**
- Create: `cmd/xlsx-transform/testdata/sample.xlsx` (or generate in test)

**Step 1: Write integration test**

```go
func TestRunTransform_E2E(t *testing.T) {
    // Generate a minimal xlsx file in-memory using excelize
    f := excelize.NewFile()
    defer f.Close()
    sheetName := "all-runs"
    f.NewSheet(sheetName)
    f.SetCellValue(sheetName, "A1", "Host")
    f.SetCellValue(sheetName, "B1", "Port")
    f.SetCellValue(sheetName, "C1", "Pass the test")
    f.SetCellValue(sheetName, "A2", "192.168.1.1")
    f.SetCellValue(sheetName, "B2", "80/443")
    f.SetCellValue(sheetName, "C2", "FALSE")
    f.SetCellValue(sheetName, "A3", "8.8.8.8")
    f.SetCellValue(sheetName, "B3", "53")
    f.SetCellValue(sheetName, "C3", "TRUE") // should be skipped

    tmpFile, err := os.CreateTemp("", "*.xlsx")
    if err != nil {
        t.Fatal(err)
    }
    tmpPath := tmpFile.Name()
    defer os.Remove(tmpPath)
    tmpFile.Close()

    if err := f.SaveAs(tmpPath); err != nil {
        t.Fatal(err)
    }

    cfg := &TransformConfig{
        Input:     tmpPath,
        Output:    t.TempDir() + "/out.csv",
        SheetName: "all-runs",
        HostCol:   "Host",
        PortCol:   "Port",
        PassCol:   "Pass the test",
    }

    if err := runTransform(cfg); err != nil {
        t.Fatalf("runTransform error: %v", err)
    }

    // Verify output
    data, err := os.ReadFile(cfg.Output)
    if err != nil {
        t.Fatalf("failed to read output: %v", err)
    }

    lines := strings.Split(strings.TrimSpace(string(data)), "\n")
    if len(lines) != 3 { // header + 2 rows (80, 443 expanded)
        t.Errorf("expected 3 lines, got %d: %q", len(lines), data)
    }

    // Check header
    expectedHeader := "src_ip,src_network_segment,dst_ip,dst_network_segment,service_label,protocol,port,decision,matched_policy_id,reason"
    if lines[0] != expectedHeader {
        t.Errorf("header = %q, want %q", lines[0], expectedHeader)
    }
}
```

**Step 2: Run test**

Run: `go test ./cmd/xlsx-transform/... -v -run TestRunTransform_E2E`
Expected: PASS

**Step 3: Commit**

```bash
git add cmd/xlsx-transform/main_test.go
git commit -m "test: add e2e integration test for transform

Verifies: header output, row expansion (80/443 → 2 rows),
pass/fail filtering (TRUE row skipped).

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

## Task 7: Verify full build

**Step 1: Build binary**

Run: `go build -o xlsx-transform ./cmd/xlsx-transform/`
Expected: Binary builds without errors

**Step 2: Run all tests including new packages**

Run: `go test ./...`
Expected: All packages pass

**Step 3: Run coverage**

Run: `bash scripts/coverage_gate.sh`
Expected: Total coverage >= 85% (new packages add coverage)

**Step 4: Commit**

```bash
git add -A
git commit -m "feat: complete xlsx-transform binary

xlsx-transform reads an xlsx workbook and outputs a Rich CSV
for the port-scan pipeline.

Usage:
  xlsx-transform --input=scan.xlsx --output=data.csv
  TRANSFORM_INPUT=/path/to/file.xlsx TRANSFORM_OUTPUT=/path/to/out.csv xlsx-transform

Files:
  pkg/xlsx/reader.go        — xlsx worksheet reading
  cmd/xlsx-transform/main.go — CLI entry point
  cmd/xlsx-transform/transform.go — port splitting, host resolution, filtering

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

## Task 8: Update SPEC-03 or note integration

**Step 1: Check if any existing spec needs updating**

Read `SPEC-03-INPUT-SYSTEM.md` — if this tool is purely pre-pipeline and doesn't modify any existing spec's contracts, no changes needed.

**Step 2: Commit (if needed)**

If any spec was updated, commit it separately.

---

## Summary: Files Created/Modified

| File | Action |
|------|--------|
| `go.mod` | Modify: add `github.com/xuri/excelize/v2` |
| `go.sum` | Modify: add dependency |
| `pkg/xlsx/reader.go` | Create |
| `pkg/xlsx/reader_test.go` | Create |
| `pkg/xlsx/testdata/sample.xlsx` | Create (test fixture) |
| `cmd/xlsx-transform/main.go` | Create |
| `cmd/xlsx-transform/main_test.go` | Create |
| `cmd/xlsx-transform/transform.go` | Create |
| `cmd/xlsx-transform/transform_test.go` | Create |
| `docs/plans/2026-04-06-xlsx-transform-implementation.md` | Create (this plan) |

## Constitution Alignment

| Principle | Compliance |
|-----------|------------|
| Library-First Design | Transform logic in `pkg/xlsx`, CLI wiring in `cmd/xlsx-transform` |
| Test-First Delivery | All tasks start with failing tests |
| SOLID Boundaries | `pkg/xlsx` has zero pipeline dependencies |
| Dependency Minimalism | Single xlsx library |
| Exit Codes | 0/1/2 documented and enforced |

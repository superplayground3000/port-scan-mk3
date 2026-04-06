# CSV Support Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add transparent .xlsx/.csv file support to xlsx-transform via a new `pkg/spreadsheet` package with content-auto-detection and strict extension matching.

**Architecture:** `pkg/spreadsheet` provides a unified `Reader` that auto-detects xlsx (PK magic) vs CSV (plain text) and returns `[][]string` for both. It validates extension matches detected format. `cmd/xlsx-transform/main.go` swaps `xlsx.NewReader` → `spreadsheet.NewReader` (drop-in replacement).

**Tech Stack:** Go stdlib only (`encoding/csv`, `os`). No new third-party deps.

---

## Task 1: Create `pkg/spreadsheet` package scaffold and CSV reader

**Files:**
- Create: `pkg/spreadsheet/csv.go`
- Create: `pkg/spreadsheet/csv_test.go`

### Step 1: Write the failing CSV tests

```go
// pkg/spreadsheet/csv_test.go
package spreadsheet

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadCSV_ValidFile(t *testing.T) {
	tmpDir := t.TempDir()
	csvPath := filepath.Join(tmpDir, "data.csv")
	csvContent := "Host,Port,Pass the test\n192.168.1.1,80,FALSE\n8.8.8.8,443,FALSE\n"
	if err := os.WriteFile(csvPath, []byte(csvContent), 0644); err != nil {
		t.Fatalf("failed to write CSV: %v", err)
	}

	rows, err := readCSV(csvPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 3 { // header + 2 data rows
		t.Fatalf("expected 3 rows, got %d", len(rows))
	}
	if rows[0][0] != "Host" || rows[1][1] != "80" {
		t.Fatalf("unexpected rows: %v", rows)
	}
}

func TestReadCSV_QuotedFields(t *testing.T) {
	tmpDir := t.TempDir()
	csvPath := filepath.Join(tmpDir, "quoted.csv")
	csvContent := "Host,Port,Pass the test\n\"192,168,1,1\",80,FALSE\n"
	if err := os.WriteFile(csvPath, []byte(csvContent), 0644); err != nil {
		t.Fatalf("failed to write CSV: %v", err)
	}

	rows, err := readCSV(csvPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0][0] != "Host" {
		t.Fatalf("unexpected row 0: %v", rows[0])
	}
	if rows[1][0] != "192,168,1,1" {
		t.Fatalf("expected quoted field unquoted, got %q", rows[1][0])
	}
}

func TestReadCSV_MissingFile(t *testing.T) {
	_, err := readCSV("/nonexistent/path.csv")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}
```

### Step 2: Run tests to verify they fail

Run: `go test ./pkg/spreadsheet/... -v -run TestReadCSV`
Expected: FAIL — "undefined: readCSV"

### Step 3: Write minimal `readCSV` implementation

```go
// pkg/spreadsheet/csv.go
package spreadsheet

import (
	"encoding/csv"
	"os"
)

// readCSV reads and returns all rows from a CSV file.
func readCSV(path string) ([][]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	reader := csv.NewReader(f)
	// Relax strict mode: allow bare quotes and variable field counts.
	reader.UnsafeQuotes = true
	rows, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}
	return rows, nil
}
```

### Step 4: Run tests to verify they pass

Run: `go test ./pkg/spreadsheet/... -v -run TestReadCSV`
Expected: PASS

### Step 5: Commit

```bash
git add pkg/spreadsheet/csv.go pkg/spreadsheet/csv_test.go
git commit -m "feat: add pkg/spreadsheet with CSV reader

readCSV reads comma-delimited CSV with header row.
RFC-4180-like quoting handled via csv.Reader UnsafeQuotes.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

## Task 2: Create `pkg/spreadsheet` unified Reader with format detection

**Files:**
- Create: `pkg/spreadsheet/reader.go`
- Create: `pkg/spreadsheet/reader_test.go`

### Step 1: Write the failing reader tests

```go
// pkg/spreadsheet/reader_test.go
package spreadsheet

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestReader_DetectXLSX(t *testing.T) {
	// Create a real xlsx file.
	tmpDir := t.TempDir()
	xlsxPath := filepath.Join(tmpDir, "test.xlsx")
	f := excelize.NewFile()
	f.NewSheet("all-runs")
	f.SetCellValue("all-runs", "A1", "Host")
	f.SetCellValue("all-runs", "B1", "Port")
	f.SetCellValue("all-runs", "A2", "192.168.1.1")
	f.SetCellValue("all-runs", "B2", "80")
	if err := f.SaveAs(xlsxPath); err != nil {
		t.Fatalf("failed to save xlsx: %v", err)
	}

	r := NewReader(xlsxPath)
	rows, err := r.OpenSheet("all-runs")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
}

func TestReader_DetectCSV(t *testing.T) {
	tmpDir := t.TempDir()
	csvPath := filepath.Join(tmpDir, "test.csv")
	csvContent := "Host,Port,Pass the test\n192.168.1.1,80,FALSE\n"
	if err := os.WriteFile(csvPath, []byte(csvContent), 0644); err != nil {
		t.Fatalf("failed to write CSV: %v", err)
	}

	r := NewReader(csvPath)
	rows, err := r.OpenSheet("any-sheet-name")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
}

func TestReader_ExtensionMismatch_XLSXContent_CSVExtension(t *testing.T) {
	// Create a real xlsx but save with .csv extension.
	tmpDir := t.TempDir()
	wrongPath := filepath.Join(tmpDir, "data.csv")
	f := excelize.NewFile()
	f.NewSheet("all-runs")
	f.SetCellValue("all-runs", "A1", "Host")
	f.SetCellValue("all-runs", "A2", "192.168.1.1")
	if err := f.SaveAs(wrongPath); err != nil {
		t.Fatalf("failed to save xlsx: %v", err)
	}

	r := NewReader(wrongPath)
	_, err := r.OpenSheet("all-runs")
	if err == nil {
		t.Fatal("expected error for extension mismatch, got nil")
	}
	if !IsExtensionMismatch(err) {
		t.Fatalf("expected ErrExtensionMismatch, got %v", err)
	}
}

func TestReader_ExtensionMismatch_CSVContent_XLSXExtension(t *testing.T) {
	tmpDir := t.TempDir()
	wrongPath := filepath.Join(tmpDir, "data.xlsx")
	csvContent := "Host,Port,Pass the test\n192.168.1.1,80,FALSE\n"
	if err := os.WriteFile(wrongPath, []byte(csvContent), 0644); err != nil {
		t.Fatalf("failed to write CSV: %v", err)
	}

	r := NewReader(wrongPath)
	_, err := r.OpenSheet("all-runs")
	if err == nil {
		t.Fatal("expected error for extension mismatch, got nil")
	}
	if !IsExtensionMismatch(err) {
		t.Fatalf("expected ErrExtensionMismatch, got %v", err)
	}
}

func TestReader_MissingFile(t *testing.T) {
	r := NewReader("/nonexistent/file.csv")
	_, err := r.OpenSheet("Sheet1")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}
```

Also add `IsExtensionMismatch` helper to the implementation.

### Step 2: Run tests to verify they fail

Run: `go test ./pkg/spreadsheet/... -v -run TestReader`
Expected: FAIL — "undefined: NewReader, Reader, IsExtensionMismatch"

### Step 3: Write minimal Reader implementation

```go
// pkg/spreadsheet/reader.go
package spreadsheet

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/xuri/excelize/v2"
)

var (
	// ErrNotXLSX is returned when the file is not a valid xlsx workbook.
	ErrNotXLSX = errors.New("file is not a valid xlsx workbook")
	// ErrNotCSV is returned when the file is not a valid csv file.
	ErrNotCSV = errors.New("file is not a valid csv file")
	// ErrExtensionMismatch is returned when the file extension does not match the detected format.
	ErrExtensionMismatch = errors.New("file extension does not match detected format")
)

// Reader reads both .xlsx and .csv files, returning worksheet rows as [][]string.
// Format is auto-detected by content; extension must match detected format.
type Reader struct {
	path string
}

// NewReader returns a Reader for the given file path.
// The file must have a .xlsx or .csv extension.
func NewReader(path string) *Reader {
	return &Reader{path: path}
}

// IsExtensionMismatch reports whether err is an extension mismatch error.
func IsExtensionMismatch(err error) bool {
	return errors.Is(err, ErrExtensionMismatch)
}

// detectFormat reads the first 2 bytes of the file to determine its format.
// Returns "xlsx" or "csv", or an error.
func detectFormat(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	header := make([]byte, 2)
	if _, err := f.Read(header); err != nil {
		return "", err
	}

	if header[0] == 0x50 && header[1] == 0x4B {
		return "xlsx", nil
	}
	return "csv", nil
}

// OpenSheet opens the file and returns all rows.
// For .xlsx: sheetName selects the worksheet.
// For .csv: sheetName is ignored.
func (r *Reader) OpenSheet(sheetName string) ([][]string, error) {
	detected, err := detectFormat(r.path)
	if err != nil {
		return nil, err
	}

	ext := strings.ToLower(filepath.Ext(r.path))
	isXLSX := ext == ".xlsx"
	isCSV := ext == ".csv"

	if detected == "xlsx" && !isXLSX {
		return nil, ErrExtensionMismatch
	}
	if detected == "csv" && !isCSV {
		return nil, ErrExtensionMismatch
	}

	if detected == "csv" {
		return readCSV(r.path)
	}

	// xlsx
	f, err := excelize.OpenFile(r.path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return f.GetRows(sheetName)
}
```

### Step 4: Run tests to verify they pass

Run: `go test ./pkg/spreadsheet/... -v`
Expected: PASS

### Step 5: Commit

```bash
git add pkg/spreadsheet/reader.go pkg/spreadsheet/reader_test.go
git commit -m "feat: add pkg/spreadsheet unified reader with auto-detection

Reader auto-detects xlsx (PK magic) vs CSV (plain text).
Strict extension matching: .csv containing xlsx data (or vice versa)
returns ErrExtensionMismatch.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

## Task 3: Wire `pkg/spreadsheet` into `cmd/xlsx-transform`

**Files:**
- Modify: `cmd/xlsx-transform/main.go`

### Step 1: Verify current tests pass before changes

Run: `go test ./cmd/xlsx-transform/... -v`
Expected: PASS

### Step 2: Update `main.go` import and reader instantiation

Change the import from:
```go
"github.com/xuxiping/port-scan-mk3/pkg/xlsx"
```

To:
```go
"github.com/xuxiping/port-scan-mk3/pkg/spreadsheet"
```

And change:
```go
reader := xlsx.NewReader(cfg.Input)
```
To:
```go
reader := spreadsheet.NewReader(cfg.Input)
```

The `OpenSheet` method signature is identical: `OpenSheet(name string) ([][]string, error)`.

### Step 3: Run tests to verify they still pass

Run: `go test ./cmd/xlsx-transform/... -v`
Expected: PASS (xlsx E2E still works since detectFormat handles xlsx)

### Step 4: Commit

```bash
git add cmd/xlsx-transform/main.go
git commit -m "refactor: xlsx-transform uses pkg/spreadsheet for auto-detect

pkg/xlsx.NewReader replaced with spreadsheet.NewReader.
Format detection (xlsx vs CSV) now happens in spreadsheet package.
Extension must match detected format (strict mode).

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

## Task 4: Add CSV E2E test to `cmd/xlsx-transform`

**Files:**
- Modify: `cmd/xlsx-transform/main_test.go`

### Step 1: Write failing CSV E2E test

Add to `main_test.go`:

```go
func TestRunTransform_E2E_CSVInput(t *testing.T) {
	// Create a CSV file with the same schema as xlsx input.
	tmpDir := t.TempDir()
	csvPath := filepath.Join(tmpDir, "input.csv")
	csvContent := "Host,Port,Pass the test\n192.168.1.1,80/443,FALSE\n8.8.8.8,53,TRUE\n"
	if err := os.WriteFile(csvPath, []byte(csvContent), 0644); err != nil {
		t.Fatalf("failed to write CSV: %v", err)
	}

	outputDir := t.TempDir()
	outputPath := filepath.Join(outputDir, "out.csv")

	cfg := &TransformConfig{
		Input:     csvPath,
		Output:    outputPath,
		SheetName: "ignored-for-csv",
		HostCol:   "Host",
		PortCol:   "Port",
		PassCol:   "Pass the test",
	}

	if err := runTransform(cfg); err != nil {
		t.Fatalf("runTransform failed: %v", err)
	}

	// Read and verify output.
	fd, err := os.Open(outputPath)
	if err != nil {
		t.Fatalf("failed to open output CSV: %v", err)
	}
	defer fd.Close()

	records, err := csv.NewReader(fd).ReadAll()
	if err != nil {
		t.Fatalf("failed to read CSV: %v", err)
	}

	if len(records) < 2 {
		t.Fatalf("expected at least 2 data rows, got %d: %v", len(records)-1, records[1:])
	}

	// Find port 80 and 443 rows.
	port80 := false
	port443 := false
	for i := 1; i < len(records); i++ {
		if len(records[i]) > 6 && records[i][6] == "80" {
			port80 = true
		}
		if len(records[i]) > 6 && records[i][6] == "443" {
			port443 = true
		}
	}
	if !port80 {
		t.Error("port 80 row not found")
	}
	if !port443 {
		t.Error("port 443 row not found")
	}

	// TRUE row (8.8.8.8:53) must be absent.
	for _, rec := range records {
		if len(rec) > 6 && rec[0] == "10.0.0.1" && rec[2] == "8.8.8.8" && rec[6] == "53" {
			t.Error("TRUE-marked row (8.8.8.8:53) should be absent from output")
		}
	}
}
```

### Step 2: Run test to verify it fails

Run: `go test ./cmd/xlsx-transform/... -v -run TestRunTransform_E2E_CSVInput`
Expected: PASS (implementation already handles CSV via spreadsheet.NewReader)

### Step 3: Commit

```bash
git add cmd/xlsx-transform/main_test.go
git commit -m "test: add CSV E2E test for xlsx-transform

Verifies: CSV input with same schema as xlsx, row expansion (80/443),
pass/fail filtering (TRUE row skipped).

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

## Task 5: Run full test suite and coverage gate

### Step 1: Run all tests

Run: `go test ./...`
Expected: ALL PASS

### Step 2: Run coverage gate

Run: `bash scripts/coverage_gate.sh`
Expected: Total coverage >= 85%

### Step 3: Full build

Run: `go build -o xlsx-transform ./cmd/xlsx-transform/`
Expected: Binary builds without errors

### Step 4: Commit

```bash
git add -A
git commit -m "feat: xlsx-transform supports both .xlsx and .csv inputs

Auto-detects file format by content (PK magic for xlsx, plain text for CSV).
Strict extension matching: mismatched extension returns error.
No CLI flag changes — transparent to users.

Files:
  pkg/spreadsheet/reader.go — unified reader with format detection
  pkg/spreadsheet/csv.go     — CSV reading helper
  cmd/xlsx-transform/main.go — swapped xlsx.Reader → spreadsheet.Reader

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

## Summary of Files

| File | Action |
|------|--------|
| `pkg/spreadsheet/csv.go` | Create |
| `pkg/spreadsheet/csv_test.go` | Create |
| `pkg/spreadsheet/reader.go` | Create |
| `pkg/spreadsheet/reader_test.go` | Create |
| `cmd/xlsx-transform/main.go` | Modify |
| `cmd/xlsx-transform/main_test.go` | Modify |

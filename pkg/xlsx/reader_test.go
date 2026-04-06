package xlsx

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestReader_OpenSheet_FileNotFound(t *testing.T) {
	r := NewReader("nonexistent.xlsx")
	_, err := r.OpenSheet("Sheet1")
	if err == nil {
		t.Fatal("expected error opening nonexistent file, got nil")
	}
}

func TestReader_OpenSheet_NotXLSX(t *testing.T) {
	// Create a file that is NOT a valid xlsx (plain CSV content).
	tmpDir := t.TempDir()
	csvPath := filepath.Join(tmpDir, "data.csv")
	if err := os.WriteFile(csvPath, []byte("Host,Port,Pass the test\n192.168.1.1,80,FALSE\n"), 0644); err != nil {
		t.Fatalf("failed to write CSV file: %v", err)
	}

	r := NewReader(csvPath)
	_, err := r.OpenSheet("Sheet1")
	if err == nil {
		t.Fatal("expected error opening non-xlsx file, got nil")
	}
	if !errors.Is(err, ErrNotXLSX) {
		t.Fatalf("expected ErrNotXLSX, got %v", err)
	}
}

func TestReader_OpenSheet_SheetNotFound(t *testing.T) {
	// Create a real xlsx file first as test fixture
	// Use excelize to create a minimal test xlsx
	tmpDir := t.TempDir()
	testFile := tmpDir + "/test.xlsx"

	f := excelize.NewFile()
	// Add a sheet named "TestSheet"
	f.NewSheet("TestSheet")
	// Set a cell value so the sheet has content
	f.SetCellValue("TestSheet", "A1", "hello")
	if err := f.SaveAs(testFile); err != nil {
		t.Fatalf("failed to save test xlsx: %v", err)
	}

	r := NewReader(testFile)
	_, err := r.OpenSheet("NonexistentSheet")
	if err == nil {
		t.Fatal("expected error opening nonexistent sheet, got nil")
	}
}

func TestReader_OpenSheet_EmptySheet(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := tmpDir + "/test.xlsx"

	f := excelize.NewFile()
	f.NewSheet("empty-sheet")
	if err := f.SaveAs(testFile); err != nil {
		t.Fatalf("failed to save test xlsx: %v", err)
	}

	r := NewReader(testFile)
	rows, err := r.OpenSheet("empty-sheet")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected 0 rows for empty sheet, got %d", len(rows))
	}
}

func TestReader_OpenSheet_Success(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := tmpDir + "/test.xlsx"

	f := excelize.NewFile()
	f.NewSheet("TestSheet")
	f.SetCellValue("TestSheet", "A1", "hello")
	f.SetCellValue("TestSheet", "B1", "world")
	f.SetCellValue("TestSheet", "A2", "foo")
	if err := f.SaveAs(testFile); err != nil {
		t.Fatalf("failed to save test xlsx: %v", err)
	}

	r := NewReader(testFile)
	rows, err := r.OpenSheet("TestSheet")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if len(rows[0]) != 2 {
		t.Fatalf("expected 2 columns in row 0, got %d", len(rows[0]))
	}
	if rows[0][0] != "hello" || rows[0][1] != "world" {
		t.Fatalf("unexpected row 0: %v", rows[0])
	}
	if rows[1][0] != "foo" {
		t.Fatalf("unexpected row 1: %v", rows[1])
	}
}
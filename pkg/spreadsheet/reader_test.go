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
	// Create a real xlsx file, then write its bytes to a .csv extension.
	// excelize refuses to SaveAs with .csv extension, so we create
	// a proper xlsx and then manually write it with wrong extension.
	tmpDir := t.TempDir()
	properPath := filepath.Join(tmpDir, "proper.xlsx")
	wrongPath := filepath.Join(tmpDir, "data.csv")
	f := excelize.NewFile()
	f.NewSheet("all-runs")
	f.SetCellValue("all-runs", "A1", "Host")
	f.SetCellValue("all-runs", "A2", "192.168.1.1")
	if err := f.SaveAs(properPath); err != nil {
		t.Fatalf("failed to save xlsx: %v", err)
	}
	data, err := os.ReadFile(properPath)
	if err != nil {
		t.Fatalf("failed to read xlsx bytes: %v", err)
	}
	if err := os.WriteFile(wrongPath, data, 0644); err != nil {
		t.Fatalf("failed to write xlsx bytes to .csv file: %v", err)
	}

	r := NewReader(wrongPath)
	_, err = r.OpenSheet("all-runs")
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

func TestIsExtensionMismatch(t *testing.T) {
	if !IsExtensionMismatch(ErrExtensionMismatch) {
		t.Error("expected true for ErrExtensionMismatch")
	}
	if IsExtensionMismatch(os.ErrNotExist) {
		t.Error("expected false for os.ErrNotExist")
	}
	// nil error
	if IsExtensionMismatch(nil) {
		t.Error("expected false for nil")
	}
}
package spreadsheet

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReader_CSV(t *testing.T) {
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

func TestReader_NonCSVExtension(t *testing.T) {
	tmpDir := t.TempDir()
	wrongPath := filepath.Join(tmpDir, "data.xlsx")
	csvContent := "Host,Port,Pass the test\n192.168.1.1,80,FALSE\n"
	if err := os.WriteFile(wrongPath, []byte(csvContent), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	r := NewReader(wrongPath)
	_, err := r.OpenSheet("all-runs")
	if err == nil {
		t.Fatal("expected error for non-CSV extension, got nil")
	}
	if err != ErrNotCSV {
		t.Fatalf("expected ErrNotCSV, got %v", err)
	}
}

func TestReader_MissingFile(t *testing.T) {
	r := NewReader("/nonexistent/file.csv")
	_, err := r.OpenSheet("Sheet1")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

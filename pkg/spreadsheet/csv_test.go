package spreadsheet

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadCSV_ValidFile(t *testing.T) {
	content := "Host,Port,Pass the test\n192.168.1.1,22,true\n192.168.1.2,80,false\n"
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "scan.csv")
	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	rows, err := readCSV(tmpFile)
	if err != nil {
		t.Fatalf("readCSV returned error: %v", err)
	}

	if len(rows) != 3 {
		t.Errorf("expected 3 rows, got %d", len(rows))
	}

	if rows[0][0] != "Host" {
		t.Errorf("expected header[0] 'Host', got %q", rows[0][0])
	}

	if rows[2][1] != "80" {
		t.Errorf("expected second data row port '80', got %q", rows[2][1])
	}
}

func TestReadCSV_QuotedFields(t *testing.T) {
	content := "Host,Port,Pass the test\n\"192,168,1,1\",22,true\n"
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "scan.csv")
	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	rows, err := readCSV(tmpFile)
	if err != nil {
		t.Fatalf("readCSV returned error: %v", err)
	}

	if len(rows) != 2 {
		t.Errorf("expected 2 rows, got %d", len(rows))
	}

	if rows[1][0] != "192,168,1,1" {
		t.Errorf("expected unquoted '192,168,1,1', got %q", rows[1][0])
	}
}

func TestReadCSV_MissingFile(t *testing.T) {
	_, err := readCSV("/nonexistent/path.csv")
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
}

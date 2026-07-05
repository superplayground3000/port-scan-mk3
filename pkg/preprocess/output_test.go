package preprocess

import (
	"bytes"
	"encoding/csv"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/preprocesscfg"
)

func TestOutputPath(t *testing.T) {
	ts := time.Date(2026, 4, 16, 15, 30, 0, 0, time.UTC)
	got := OutputPath("/data/out", "dc-east", ts)
	// OutputPath uses filepath.Join, so the separator is OS-native (backslash on
	// Windows); build the expectation the same way to stay cross-platform.
	expected := filepath.Join("/data/out", "dc-east", "20260416T153000Z", "input.csv")
	if got != expected {
		t.Errorf("expected %s, got %s", expected, got)
	}
}

func TestOutputPath_SpecialCharsInFab(t *testing.T) {
	ts := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	got := OutputPath("/out", "fab-1", ts)
	expected := filepath.Join("/out", "fab-1", "20260102T030405Z", "input.csv")
	if got != expected {
		t.Errorf("expected %s, got %s", expected, got)
	}
}

func TestCreateOutputWriter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dc-east", "20260416T153000Z", "input.csv")

	cw, f, err := CreateOutputWriter(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer f.Close()

	cw.Write([]string{"a", "b"})
	cw.Flush()

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("expected output file to exist")
	}
}

func TestWriteRichCSV(t *testing.T) {
	var buf bytes.Buffer
	cw := csv.NewWriter(&buf)

	rows := [][]string{
		{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10"},
	}
	if err := WriteRichCSV(cw, rows); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, strings.Join(preprocesscfg.RichHeader(), ",")) {
		t.Error("expected rich CSV header in output")
	}
	if !strings.Contains(output, "1,2,3,4,5,6,7,8,9,10") {
		t.Error("expected data row in output")
	}
}

func TestPrintSummary(t *testing.T) {
	var buf bytes.Buffer
	if err := PrintSummary(&buf, 100, 80, 20); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "100") {
		t.Error("expected total count in summary")
	}
	if !strings.Contains(output, "80") {
		t.Error("expected kept count in summary")
	}
	if !strings.Contains(output, "20") {
		t.Error("expected dropped count in summary")
	}
}

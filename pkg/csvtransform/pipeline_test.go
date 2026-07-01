package csvtransform

import (
	"bytes"
	"encoding/csv"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunTransform_FileNotFound(t *testing.T) {
	cfg := Config{
		Input:     "/nonexistent/path/to/file.csv",
		Output:    "/tmp/out.csv",
		SheetName: "all-runs",
		HostCol:   "Host",
		PortCol:   "Port",
		PassCol:   "Pass the test",
	}
	if err := Run(cfg, &bytes.Buffer{}); err == nil {
		t.Fatalf("Run with nonexistent file = nil, want error")
	}
}

func TestRunTransform_NonCSVExtension(t *testing.T) {
	tmpDir := t.TempDir()
	// Create a file with wrong extension
	inputPath := filepath.Join(tmpDir, "data.xlsx")
	csvContent := "Host,Port,Pass the test\n192.168.1.1,80,FALSE\n"
	if err := os.WriteFile(inputPath, []byte(csvContent), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	outputPath := filepath.Join(tmpDir, "out.csv")
	cfg := Config{
		Input:     inputPath,
		Output:    outputPath,
		SheetName: "all-runs",
		HostCol:   "Host",
		PortCol:   "Port",
		PassCol:   "Pass the test",
	}
	if err := Run(cfg, &bytes.Buffer{}); err == nil {
		t.Fatalf("Run with wrong extension = nil, want error")
	}
}

func TestRunTransform_MissingHostColumn(t *testing.T) {
	tmpDir := t.TempDir()
	csvPath := filepath.Join(tmpDir, "test.csv")
	// Omit "Host" column
	csvContent := "Port,Pass the test\n80,FALSE\n"
	if err := os.WriteFile(csvPath, []byte(csvContent), 0644); err != nil {
		t.Fatalf("failed to write CSV: %v", err)
	}

	outputPath := filepath.Join(tmpDir, "out.csv")
	cfg := Config{
		Input:     csvPath,
		Output:    outputPath,
		SheetName: "all-runs",
		HostCol:   "Host",
		PortCol:   "Port",
		PassCol:   "Pass the test",
	}
	if err := Run(cfg, &bytes.Buffer{}); err == nil {
		t.Fatalf("Run with missing Host column = nil, want error")
	}
}

func TestRunTransform_MissingPortColumn(t *testing.T) {
	tmpDir := t.TempDir()
	csvPath := filepath.Join(tmpDir, "test.csv")
	// Omit "Port" column
	csvContent := "Host,Pass the test\n192.168.1.1,FALSE\n"
	if err := os.WriteFile(csvPath, []byte(csvContent), 0644); err != nil {
		t.Fatalf("failed to write CSV: %v", err)
	}

	outputPath := filepath.Join(tmpDir, "out.csv")
	cfg := Config{
		Input:     csvPath,
		Output:    outputPath,
		SheetName: "all-runs",
		HostCol:   "Host",
		PortCol:   "Port",
		PassCol:   "Pass the test",
	}
	if err := Run(cfg, &bytes.Buffer{}); err == nil {
		t.Fatalf("Run with missing Port column = nil, want error")
	}
}

func TestRunTransform_MissingPassColumn(t *testing.T) {
	tmpDir := t.TempDir()
	csvPath := filepath.Join(tmpDir, "test.csv")
	// Omit "Pass the test" column
	csvContent := "Host,Port\n192.168.1.1,80\n"
	if err := os.WriteFile(csvPath, []byte(csvContent), 0644); err != nil {
		t.Fatalf("failed to write CSV: %v", err)
	}

	outputPath := filepath.Join(tmpDir, "out.csv")
	cfg := Config{
		Input:     csvPath,
		Output:    outputPath,
		SheetName: "all-runs",
		HostCol:   "Host",
		PortCol:   "Port",
		PassCol:   "Pass the test",
	}
	if err := Run(cfg, &bytes.Buffer{}); err == nil {
		t.Fatalf("Run with missing Pass column = nil, want error")
	}
}

func TestRunTransform_OutputFileCreationFails(t *testing.T) {
	tmpDir := t.TempDir()
	csvPath := filepath.Join(tmpDir, "test.csv")
	csvContent := "Host,Port,Pass the test\n192.168.1.1,80,FALSE\n"
	if err := os.WriteFile(csvPath, []byte(csvContent), 0644); err != nil {
		t.Fatalf("failed to write CSV: %v", err)
	}

	cfg := Config{
		Input:     csvPath,
		Output:    "/proc/nonexistent/out.csv",
		SheetName: "all-runs",
		HostCol:   "Host",
		PortCol:   "Port",
		PassCol:   "Pass the test",
	}
	if err := Run(cfg, &bytes.Buffer{}); err == nil {
		t.Fatalf("Run with unreachable output path = nil, want error")
	}
}

func TestRunTransform_E2E(t *testing.T) {
	// Create a CSV file
	tmpDir := t.TempDir()
	csvPath := filepath.Join(tmpDir, "input.csv")
	csvContent := `Host,Port,Pass the test
192.168.1.1,80/443,FALSE
8.8.8.8,53,TRUE
example.com,22,FALSE
`
	if err := os.WriteFile(csvPath, []byte(csvContent), 0644); err != nil {
		t.Fatalf("failed to write CSV: %v", err)
	}

	outputPath := filepath.Join(tmpDir, "out.csv")

	cfg := Config{
		Input:     csvPath,
		Output:    outputPath,
		SheetName: "all-runs",
		HostCol:   "Host",
		PortCol:   "Port",
		PassCol:   "Pass the test",
	}

	if err := Run(cfg, &bytes.Buffer{}); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// Read and verify output CSV
	fd, err := os.Open(outputPath)
	if err != nil {
		t.Fatalf("failed to open output CSV: %v", err)
	}
	defer fd.Close()

	records, err := csv.NewReader(fd).ReadAll()
	if err != nil {
		t.Fatalf("failed to read CSV: %v", err)
	}

	// Verify header
	expectedHeader := "src_ip,src_network_segment,dst_ip,dst_network_segment,service_label,protocol,port,decision,matched_policy_id,reason"
	if len(records) < 1 {
		t.Fatalf("CSV has no rows, expected header + data rows")
	}
	headerStr := ""
	for i, h := range records[0] {
		if i > 0 {
			headerStr += ","
		}
		headerStr += h
	}
	if headerStr != expectedHeader {
		t.Errorf("header mismatch:\ngot:  %q\nwant: %q", headerStr, expectedHeader)
	}

	// We expect exactly 2 rows (80 and 443 expanded from row 1; row 2 skipped because TRUE).
	// Row 3 (example.com) may or may not resolve; if it doesn't the behavior is acceptable.
	if len(records) < 2 {
		t.Fatalf("expected at least 2 data rows, got %d: %v", len(records)-1, records[1:])
	}

	// Find the rows for ports 80 and 443
	port80Row := -1
	port443Row := -1
	for i := 1; i < len(records); i++ {
		row := records[i]
		if len(row) < 10 {
			continue
		}
		if row[6] == "80" {
			port80Row = i
		}
		if row[6] == "443" {
			port443Row = i
		}
	}

	if port80Row == -1 {
		t.Errorf("did not find row with port=80; rows: %v", records[1:])
	}
	if port443Row == -1 {
		t.Errorf("did not find row with port=443; rows: %v", records[1:])
	}

	// Verify dst_ip for port 80 row is 192.168.1.1 (IP passthrough)
	if port80Row != -1 {
		row := records[port80Row]
		if row[2] != "192.168.1.1" {
			t.Errorf("dst_ip for port 80 row = %q, want %q", row[2], "192.168.1.1")
		}
		if row[9] != "MATCH_POLICY_ACCEPT" {
			t.Errorf("reason for port 80 row = %q, want %q", row[9], "MATCH_POLICY_ACCEPT")
		}
	}
	if port443Row != -1 {
		row := records[port443Row]
		if row[9] != "MATCH_POLICY_ACCEPT" {
			t.Errorf("reason for port 443 row = %q, want %q", row[9], "MATCH_POLICY_ACCEPT")
		}
	}

	// Verify TRUE row (8.8.8.8:53) is NOT in output
	for _, rec := range records {
		if len(rec) > 6 && rec[0] == "10.0.0.1" && rec[2] == "8.8.8.8" && rec[6] == "53" {
			t.Errorf("TRUE-marked row (8.8.8.8:53) should be absent from output, found: %v", rec)
		}
	}
}

func TestRunTransform_ProblematicRowsLoggedAndSkipped(t *testing.T) {
	tmpDir := t.TempDir()
	csvPath := filepath.Join(tmpDir, "input.csv")
	// CSV with problematic rows mixed in
	csvContent := `Host,Port,Pass the test
192.168.1.1,80,FALSE
,22,FALSE
192.168.1.2,,FALSE
192.168.1.3,abc,FALSE
192.168.1.4,443,TRUE
192.168.1.5,22,FALSE
`
	if err := os.WriteFile(csvPath, []byte(csvContent), 0644); err != nil {
		t.Fatalf("failed to write CSV: %v", err)
	}

	outputPath := filepath.Join(tmpDir, "out.csv")

	// Capture warnings into a buffer instead of stderr.
	var warnBuf bytes.Buffer

	cfg := Config{
		Input:     csvPath,
		Output:    outputPath,
		SheetName: "all-runs",
		HostCol:   "Host",
		PortCol:   "Port",
		PassCol:   "Pass the test",
	}

	if err := Run(cfg, &warnBuf); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	logOutput := warnBuf.String()

	// Verify problematic rows were logged
	// CSV row numbers: 1=header, 2=192.168.1.1 (valid), 3=empty host, 4=empty port, 5=invalid port, 6=TRUE, 7=valid
	t.Logf("log output: %q", logOutput)
	if !strings.Contains(logOutput, "skipping row 2") {
		t.Error("expected log for empty host row")
	}
	if !strings.Contains(logOutput, "skipping row 3") {
		t.Error("expected log for empty port row")
	}
	if !strings.Contains(logOutput, "skipping row 4") {
		t.Error("expected log for invalid port row")
	}
	if !strings.Contains(logOutput, "skipping row 5") {
		t.Error("expected log for TRUE pass column row")
	}

	// Verify valid rows still processed
	fd, err := os.Open(outputPath)
	if err != nil {
		t.Fatalf("failed to open output CSV: %v", err)
	}
	defer fd.Close()

	records, err := csv.NewReader(fd).ReadAll()
	if err != nil {
		t.Fatalf("failed to read CSV: %v", err)
	}

	// Should have 2 valid rows (80 and 22)
	if len(records) != 3 { // header + 2 data rows
		t.Errorf("expected 2 data rows, got %d: %v", len(records)-1, records[1:])
	}
}

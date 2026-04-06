package main

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestTransformConfig_FromFlags_MissingRequired(t *testing.T) {
	// Simulate calling ParseConfig with no flags set.
	// We save original env vars and restore after.
	origInput := os.Getenv("TRANSFORM_INPUT")
	origOutput := os.Getenv("TRANSFORM_OUTPUT")
	defer func() {
		if origInput != "" {
			os.Setenv("TRANSFORM_INPUT", origInput)
		} else {
			os.Unsetenv("TRANSFORM_INPUT")
		}
		if origOutput != "" {
			os.Setenv("TRANSFORM_OUTPUT", origOutput)
		} else {
			os.Unsetenv("TRANSFORM_OUTPUT")
		}
	}()
	os.Unsetenv("TRANSFORM_INPUT")
	os.Unsetenv("TRANSFORM_OUTPUT")

	cfg, err := ParseConfigFromArgs(nil)
	if err == nil {
		t.Fatalf("ParseConfigFromArgs(nil) = %v, want non-nil error", cfg)
	}
}

func TestParseConfig_EnvVarOverride(t *testing.T) {
	// Set env vars and ensure they override defaults.
	origInput := os.Getenv("TRANSFORM_INPUT")
	origOutput := os.Getenv("TRANSFORM_OUTPUT")
	defer func() {
		if origInput != "" {
			os.Setenv("TRANSFORM_INPUT", origInput)
		} else {
			os.Unsetenv("TRANSFORM_INPUT")
		}
		if origOutput != "" {
			os.Setenv("TRANSFORM_OUTPUT", origOutput)
		} else {
			os.Unsetenv("TRANSFORM_OUTPUT")
		}
	}()

	os.Setenv("TRANSFORM_INPUT", "/env/input.xlsx")
	os.Setenv("TRANSFORM_OUTPUT", "/env/output.csv")

	cfg, err := ParseConfigFromArgs(nil)
	if err != nil {
		t.Fatalf("ParseConfigFromArgs(nil) unexpected error: %v", err)
	}
	if cfg.Input != "/env/input.xlsx" {
		t.Errorf("cfg.Input = %q, want %q", cfg.Input, "/env/input.xlsx")
	}
	if cfg.Output != "/env/output.csv" {
		t.Errorf("cfg.Output = %q, want %q", cfg.Output, "/env/output.csv")
	}
}

func TestRunTransform_FileNotFound(t *testing.T) {
	cfg := &TransformConfig{
		Input:     "/nonexistent/path/to/file.xlsx",
		Output:    "/tmp/out.csv",
		SheetName: "all-runs",
		HostCol:   "Host",
		PortCol:   "Port",
		PassCol:   "Pass the test",
	}
	if err := runTransform(cfg); err == nil {
		t.Fatalf("runTransform with nonexistent file = nil, want error")
	}
}

func TestRunTransform_SheetNotFound(t *testing.T) {
	f := excelize.NewFile()
	defer f.Close()

	sheetName := "actual-sheet"
	f.NewSheet(sheetName)
	// Set a header so the file is valid but we ask for the wrong sheet.
	headers := []string{"Host", "Port", "Pass the test"}
	for colIdx, header := range headers {
		cell, _ := excelize.CoordinatesToCellName(colIdx+1, 1)
		f.SetCellValue(sheetName, cell, header)
	}

	inputDir, err := os.MkdirTemp("", "xlsx-transform-input-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(inputDir)

	inputPath := filepath.Join(inputDir, "test.xlsx")
	if err := f.SaveAs(inputPath); err != nil {
		t.Fatalf("failed to save xlsx: %v", err)
	}

	outputDir, err := os.MkdirTemp("", "xlsx-transform-output-*")
	if err != nil {
		t.Fatalf("failed to create temp output dir: %v", err)
	}
	defer os.RemoveAll(outputDir)

	cfg := &TransformConfig{
		Input:     inputPath,
		Output:    filepath.Join(outputDir, "out.csv"),
		SheetName: "nonexistent-sheet",
		HostCol:   "Host",
		PortCol:   "Port",
		PassCol:   "Pass the test",
	}
	if err := runTransform(cfg); err == nil {
		t.Fatalf("runTransform with wrong sheet = nil, want error")
	}
}

func TestRunTransform_MissingHostColumn(t *testing.T) {
	f := excelize.NewFile()
	defer f.Close()

	sheetName := "all-runs"
	f.NewSheet(sheetName)
	// Omit "Host" column.
	headers := []string{"Port", "Pass the test"}
	for colIdx, header := range headers {
		cell, _ := excelize.CoordinatesToCellName(colIdx+1, 1)
		f.SetCellValue(sheetName, cell, header)
	}
	f.SetCellValue(sheetName, "A2", "80")
	f.SetCellValue(sheetName, "B2", "FALSE")

	inputDir, err := os.MkdirTemp("", "xlsx-transform-input-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(inputDir)

	inputPath := filepath.Join(inputDir, "test.xlsx")
	if err := f.SaveAs(inputPath); err != nil {
		t.Fatalf("failed to save xlsx: %v", err)
	}

	outputDir, err := os.MkdirTemp("", "xlsx-transform-output-*")
	if err != nil {
		t.Fatalf("failed to create temp output dir: %v", err)
	}
	defer os.RemoveAll(outputDir)

	cfg := &TransformConfig{
		Input:     inputPath,
		Output:    filepath.Join(outputDir, "out.csv"),
		SheetName: sheetName,
		HostCol:   "Host",
		PortCol:   "Port",
		PassCol:   "Pass the test",
	}
	if err := runTransform(cfg); err == nil {
		t.Fatalf("runTransform with missing Host column = nil, want error")
	}
}

func TestRunTransform_MissingPortColumn(t *testing.T) {
	f := excelize.NewFile()
	defer f.Close()

	sheetName := "all-runs"
	f.NewSheet(sheetName)
	// Omit "Port" column.
	headers := []string{"Host", "Pass the test"}
	for colIdx, header := range headers {
		cell, _ := excelize.CoordinatesToCellName(colIdx+1, 1)
		f.SetCellValue(sheetName, cell, header)
	}
	f.SetCellValue(sheetName, "A2", "192.168.1.1")
	f.SetCellValue(sheetName, "B2", "FALSE")

	inputDir, err := os.MkdirTemp("", "xlsx-transform-input-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(inputDir)

	inputPath := filepath.Join(inputDir, "test.xlsx")
	if err := f.SaveAs(inputPath); err != nil {
		t.Fatalf("failed to save xlsx: %v", err)
	}

	outputDir, err := os.MkdirTemp("", "xlsx-transform-output-*")
	if err != nil {
		t.Fatalf("failed to create temp output dir: %v", err)
	}
	defer os.RemoveAll(outputDir)

	cfg := &TransformConfig{
		Input:     inputPath,
		Output:    filepath.Join(outputDir, "out.csv"),
		SheetName: sheetName,
		HostCol:   "Host",
		PortCol:   "Port",
		PassCol:   "Pass the test",
	}
	if err := runTransform(cfg); err == nil {
		t.Fatalf("runTransform with missing Port column = nil, want error")
	}
}

func TestRunTransform_MissingPassColumn(t *testing.T) {
	f := excelize.NewFile()
	defer f.Close()

	sheetName := "all-runs"
	f.NewSheet(sheetName)
	// Omit "Pass the test" column.
	headers := []string{"Host", "Port"}
	for colIdx, header := range headers {
		cell, _ := excelize.CoordinatesToCellName(colIdx+1, 1)
		f.SetCellValue(sheetName, cell, header)
	}
	f.SetCellValue(sheetName, "A2", "192.168.1.1")
	f.SetCellValue(sheetName, "B2", "80")

	inputDir, err := os.MkdirTemp("", "xlsx-transform-input-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(inputDir)

	inputPath := filepath.Join(inputDir, "test.xlsx")
	if err := f.SaveAs(inputPath); err != nil {
		t.Fatalf("failed to save xlsx: %v", err)
	}

	outputDir, err := os.MkdirTemp("", "xlsx-transform-output-*")
	if err != nil {
		t.Fatalf("failed to create temp output dir: %v", err)
	}
	defer os.RemoveAll(outputDir)

	cfg := &TransformConfig{
		Input:     inputPath,
		Output:    filepath.Join(outputDir, "out.csv"),
		SheetName: sheetName,
		HostCol:   "Host",
		PortCol:   "Port",
		PassCol:   "Pass the test",
	}
	if err := runTransform(cfg); err == nil {
		t.Fatalf("runTransform with missing Pass column = nil, want error")
	}
}

func TestRunTransform_OutputFileCreationFails(t *testing.T) {
	f := excelize.NewFile()
	defer f.Close()

	sheetName := "all-runs"
	f.NewSheet(sheetName)
	headers := []string{"Host", "Port", "Pass the test"}
	for colIdx, header := range headers {
		cell, _ := excelize.CoordinatesToCellName(colIdx+1, 1)
		f.SetCellValue(sheetName, cell, header)
	}
	f.SetCellValue(sheetName, "A2", "192.168.1.1")
	f.SetCellValue(sheetName, "B2", "80")
	f.SetCellValue(sheetName, "C2", "FALSE")

	inputDir, err := os.MkdirTemp("", "xlsx-transform-input-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(inputDir)

	inputPath := filepath.Join(inputDir, "test.xlsx")
	if err := f.SaveAs(inputPath); err != nil {
		t.Fatalf("failed to save xlsx: %v", err)
	}

	// Use a path that cannot be created (read-only filesystem-like path).
	cfg := &TransformConfig{
		Input:     inputPath,
		Output:    "/proc/nonexistent/out.csv",
		SheetName: sheetName,
		HostCol:   "Host",
		PortCol:   "Port",
		PassCol:   "Pass the test",
	}
	if err := runTransform(cfg); err == nil {
		t.Fatalf("runTransform with unreachable output path = nil, want error")
	}
}

func TestParseConfig_MissingOutput(t *testing.T) {
	origInput := os.Getenv("TRANSFORM_INPUT")
	origOutput := os.Getenv("TRANSFORM_OUTPUT")
	defer func() {
		if origInput != "" {
			os.Setenv("TRANSFORM_INPUT", origInput)
		} else {
			os.Unsetenv("TRANSFORM_INPUT")
		}
		if origOutput != "" {
			os.Setenv("TRANSFORM_OUTPUT", origOutput)
		} else {
			os.Unsetenv("TRANSFORM_OUTPUT")
		}
	}()
	os.Setenv("TRANSFORM_INPUT", "/some/input.xlsx")
	os.Unsetenv("TRANSFORM_OUTPUT")

	cfg, err := ParseConfigFromArgs(nil)
	if err == nil {
		t.Fatalf("ParseConfigFromArgs with only --input = %v, want error", cfg)
	}
	if cfgErr, ok := err.(*ConfigError); ok {
		if cfgErr.Code != 2 {
			t.Errorf("ConfigError.Code = %d, want 2", cfgErr.Code)
		}
	} else {
		t.Fatalf("expected *ConfigError, got %T", err)
	}
}

func TestParseConfig_MissingInput(t *testing.T) {
	origInput := os.Getenv("TRANSFORM_INPUT")
	origOutput := os.Getenv("TRANSFORM_OUTPUT")
	defer func() {
		if origInput != "" {
			os.Setenv("TRANSFORM_INPUT", origInput)
		} else {
			os.Unsetenv("TRANSFORM_INPUT")
		}
		if origOutput != "" {
			os.Setenv("TRANSFORM_OUTPUT", origOutput)
		} else {
			os.Unsetenv("TRANSFORM_OUTPUT")
		}
	}()
	os.Setenv("TRANSFORM_OUTPUT", "/some/output.csv")
	os.Unsetenv("TRANSFORM_INPUT")

	cfg, err := ParseConfigFromArgs(nil)
	if err == nil {
		t.Fatalf("ParseConfigFromArgs with only --output = %v, want error", cfg)
	}
	if cfgErr, ok := err.(*ConfigError); ok {
		if cfgErr.Code != 2 {
			t.Errorf("ConfigError.Code = %d, want 2", cfgErr.Code)
		}
	} else {
		t.Fatalf("expected *ConfigError, got %T", err)
	}
}

func TestRunTransform_E2E(t *testing.T) {
	// Create a real xlsx file in-memory using excelize.
	f := excelize.NewFile()
	defer f.Close()

	sheetName := "all-runs"
	headers := []string{"Host", "Port", "Pass the test"}

	// Set up header row (row 1 in excelize is index 0).
	headerRow, _ := f.NewSheet(sheetName)
	_ = headerRow
	for colIdx, header := range headers {
		cell, _ := excelize.CoordinatesToCellName(colIdx+1, 1)
		f.SetCellValue(sheetName, cell, header)
	}

	// Row 2: 192.168.1.1, 80/443, FALSE → expands to 2 rows.
	f.SetCellValue(sheetName, "A2", "192.168.1.1")
	f.SetCellValue(sheetName, "B2", "80/443")
	f.SetCellValue(sheetName, "C2", "FALSE")

	// Row 3: 8.8.8.8, 53, TRUE → should be SKIPPED.
	f.SetCellValue(sheetName, "A3", "8.8.8.8")
	f.SetCellValue(sheetName, "B3", "53")
	f.SetCellValue(sheetName, "C3", "TRUE")

	// Row 4: example.com, 22, FALSE → hostname resolution may fail but row included.
	f.SetCellValue(sheetName, "A4", "example.com")
	f.SetCellValue(sheetName, "B4", "22")
	f.SetCellValue(sheetName, "C4", "FALSE")

	// Save xlsx to a temp file.
	inputDir, err := os.MkdirTemp("", "xlsx-transform-input-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(inputDir)

	inputPath := filepath.Join(inputDir, "test.xlsx")
	if err := f.SaveAs(inputPath); err != nil {
		t.Fatalf("failed to save xlsx: %v", err)
	}

	// Create temp output file.
	outputDir, err := os.MkdirTemp("", "xlsx-transform-output-*")
	if err != nil {
		t.Fatalf("failed to create temp output dir: %v", err)
	}
	defer os.RemoveAll(outputDir)

	outputPath := filepath.Join(outputDir, "out.csv")

	// Build config and call runTransform.
	cfg := &TransformConfig{
		Input:     inputPath,
		Output:    outputPath,
		SheetName: sheetName,
		HostCol:   "Host",
		PortCol:   "Port",
		PassCol:   "Pass the test",
	}

	if err := runTransform(cfg); err != nil {
		t.Fatalf("runTransform failed: %v", err)
	}

	// Read and verify output CSV.
	fd, err := os.Open(outputPath)
	if err != nil {
		t.Fatalf("failed to open output CSV: %v", err)
	}
	defer fd.Close()

	reader := csv.NewReader(fd)
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("failed to read CSV: %v", err)
	}

	// Verify header.
	expectedHeader := "src_ip,src_network_segment,dst_ip,dst_network_segment,service_label,protocol,port,decision,matched_policy_id,reason"
	if len(records) < 1 {
		t.Fatalf("CSV has no rows, expected header + data rows")
	}
	if records[0][0] != "src_ip" {
		t.Fatalf("header mismatch: got %q, want %q", records[0][0], "src_ip")
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

	// We expect exactly 2 rows (80 and 443 expanded from row 2; row 3 skipped because TRUE).
	// Row 4 (example.com) may or may not resolve; if it doesn't the behavior is acceptable.
	if len(records) < 2 {
		t.Fatalf("expected at least 2 data rows, got %d: %v", len(records)-1, records[1:])
	}

	// Find the rows for ports 80 and 443.
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

	// Verify dst_ip for port 80 row is 192.168.1.1 (IP passthrough).
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

	// Verify TRUE row (8.8.8.8:53) is NOT in output.
	for _, rec := range records {
		if len(rec) > 6 && rec[0] == "10.0.0.1" && rec[2] == "8.8.8.8" && rec[6] == "53" {
			t.Errorf("TRUE-marked row (8.8.8.8:53) should be absent from output, found: %v", rec)
		}
	}
}
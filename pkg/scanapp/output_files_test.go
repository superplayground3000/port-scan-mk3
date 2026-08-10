package scanapp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/writer"
)

// TestOpenBatchOutputs_WhenAppendParentDirRemoved_RecreatesDir is the A1 fix:
// a -resume whose recorded output directory was removed must recreate it via
// MkdirAll rather than fail with ENOENT on the append-reopen path.
func TestOpenBatchOutputs_WhenAppendParentDirRemoved_RecreatesDir(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "gone")
	scanPath := filepath.Join(sub, "scan_results.csv")
	openPath := filepath.Join(sub, "opened_results.csv")

	// The recorded directory does not exist at resume time (never created).
	if _, err := os.Stat(sub); !os.IsNotExist(err) {
		t.Fatalf("precondition: expected %s absent, got %v", sub, err)
	}

	outputs, err := openBatchOutputs(scanPath, openPath, true)
	if err != nil {
		t.Fatalf("expected append-reopen to recreate the missing dir, got: %v", err)
	}
	if err := outputs.scanWriter.Write(writer.Record{IP: "1.2.3.4", IPCidr: "1.2.3.0/24", Port: 80, Status: "open"}); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if err := outputs.Finalize(); err != nil {
		t.Fatalf("finalize failed: %v", err)
	}
	header, rows := readCSVRows(t, scanPath)
	if len(header) == 0 || header[0] != "ip" {
		t.Fatalf("expected recreated file with header, got %v", header)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 data row in recreated file, got %d", len(rows))
	}
}

// TestOpenBatchOutputs_WhenAppendHeaderMismatch_FailsLoudly is the A2 fix:
// appending onto a non-empty file whose first line is not the canonical schema
// header must fail loudly instead of blindly appending to a mismatched file.
func TestOpenBatchOutputs_WhenAppendHeaderMismatch_FailsLoudly(t *testing.T) {
	dir := t.TempDir()
	scanPath := filepath.Join(dir, "scan_results.csv")
	openPath := filepath.Join(dir, "opened_results.csv")

	// A file with a stale/edited header that differs from writer.Columns.
	stale := "ip,port,status\n1.2.3.4,80,open\n"
	if err := os.WriteFile(scanPath, []byte(stale), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	_, err := openBatchOutputs(scanPath, openPath, true)
	if err == nil {
		t.Fatal("expected a loud error appending to a mismatched-header file, got nil")
	}
	if !strings.Contains(err.Error(), "does not match the current output schema") {
		t.Fatalf("expected schema-mismatch error, got: %v", err)
	}
}

func TestBatchOutputs_WhenFreshMode_WritesDirectlyToFinalPathNoTmp(t *testing.T) {
	dir := t.TempDir()
	scanPath := filepath.Join(dir, "scan.csv")
	openPath := filepath.Join(dir, "open.csv")

	outputs, err := openBatchOutputs(scanPath, openPath, false)
	if err != nil {
		t.Fatalf("open failed: %v", err)
	}

	if err := outputs.scanWriter.Write(writer.Record{
		IP: "1.2.3.4", IPCidr: "1.2.3.0/24", Port: 80, Status: "open",
	}); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// Rows are durable at the final path BEFORE Finalize — no .tmp promotion.
	if _, err := os.Stat(scanPath); err != nil {
		t.Fatalf("expected final scan file to exist before finalize, got: %v", err)
	}
	if _, err := os.Stat(scanPath + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("expected no tmp scan file, got: %v", err)
	}
	if _, err := os.Stat(openPath + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("expected no tmp open file, got: %v", err)
	}

	if err := outputs.Finalize(); err != nil {
		t.Fatalf("finalize failed: %v", err)
	}

	if _, err := os.Stat(openPath); err != nil {
		t.Fatalf("expected final open file, got: %v", err)
	}
}

func TestBatchOutputs_WhenWritten_ContainsWrittenData(t *testing.T) {
	dir := t.TempDir()
	scanPath := filepath.Join(dir, "scan.csv")
	openPath := filepath.Join(dir, "open.csv")

	outputs, err := openBatchOutputs(scanPath, openPath, false)
	if err != nil {
		t.Fatalf("open failed: %v", err)
	}

	if err := outputs.scanWriter.Write(writer.Record{
		IP: "1.2.3.4", IPCidr: "1.2.3.0/24", Port: 80, Status: "open",
	}); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	if err := outputs.Finalize(); err != nil {
		t.Fatalf("finalize failed: %v", err)
	}

	data, err := os.ReadFile(scanPath)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if !strings.Contains(string(data), "1.2.3.4") {
		t.Fatalf("expected data in final file, got: %s", string(data))
	}
}

// TestBatchOutputs_WhenAppendMode_AppendsWithoutDuplicateHeader proves a second
// open in append mode continues the same file without re-emitting the header
// (design §3.7).
func TestBatchOutputs_WhenAppendMode_AppendsWithoutDuplicateHeader(t *testing.T) {
	dir := t.TempDir()
	scanPath := filepath.Join(dir, "scan.csv")
	openPath := filepath.Join(dir, "open.csv")

	first, err := openBatchOutputs(scanPath, openPath, false)
	if err != nil {
		t.Fatalf("open (fresh) failed: %v", err)
	}
	if err := first.scanWriter.Write(writer.Record{IP: "1.1.1.1", IPCidr: "1.1.1.0/24", Port: 80, Status: "open"}); err != nil {
		t.Fatalf("write 1 failed: %v", err)
	}
	if err := first.Finalize(); err != nil {
		t.Fatalf("finalize 1 failed: %v", err)
	}

	second, err := openBatchOutputs(scanPath, openPath, true)
	if err != nil {
		t.Fatalf("open (append) failed: %v", err)
	}
	if err := second.scanWriter.Write(writer.Record{IP: "2.2.2.2", IPCidr: "2.2.2.0/24", Port: 80, Status: "open"}); err != nil {
		t.Fatalf("write 2 failed: %v", err)
	}
	if err := second.Finalize(); err != nil {
		t.Fatalf("finalize 2 failed: %v", err)
	}

	data, err := os.ReadFile(scanPath)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	got := string(data)
	if n := strings.Count(got, "ip,ip_cidr,port,status"); n != 1 {
		t.Fatalf("expected exactly one header line, got %d in:\n%s", n, got)
	}
	if !strings.Contains(got, "1.1.1.1") || !strings.Contains(got, "2.2.2.2") {
		t.Fatalf("expected both rows in appended file, got:\n%s", got)
	}
}

// TestBatchOutputs_WhenAppendModeAndFileMissing_RecreatesWithHeader covers the
// prior-output-deleted edge: append mode on a missing/empty file recreates it
// with a header.
func TestBatchOutputs_WhenAppendModeAndFileMissing_RecreatesWithHeader(t *testing.T) {
	dir := t.TempDir()
	scanPath := filepath.Join(dir, "scan.csv")
	openPath := filepath.Join(dir, "open.csv")

	outputs, err := openBatchOutputs(scanPath, openPath, true)
	if err != nil {
		t.Fatalf("open (append, missing) failed: %v", err)
	}
	if err := outputs.scanWriter.Write(writer.Record{IP: "3.3.3.3", IPCidr: "3.3.3.0/24", Port: 80, Status: "open"}); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if err := outputs.Finalize(); err != nil {
		t.Fatalf("finalize failed: %v", err)
	}

	data, err := os.ReadFile(scanPath)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "ip,ip_cidr,port,status") {
		t.Fatalf("expected recreated file to have a header, got:\n%s", got)
	}
	if !strings.Contains(got, "3.3.3.3") {
		t.Fatalf("expected data row in recreated file, got:\n%s", got)
	}
}

func TestResolveBatchOutputPaths_WhenAllocated_ReturnsScanOpenUnreachableWithSharedSuffix(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 3, 2, 1, 30, 45, 0, time.UTC)

	paths, err := resolveBatchOutputPaths(filepath.Join(dir, "scan_results.csv"), now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantScan := filepath.Join(dir, "scan_results-20260302T013045Z.csv")
	wantOpen := filepath.Join(dir, "opened_results-20260302T013045Z.csv")
	wantUnreachable := filepath.Join(dir, "unreachable_results-20260302T013045Z.csv")
	if paths.scanPath != wantScan {
		t.Fatalf("scan path mismatch: got=%s want=%s", paths.scanPath, wantScan)
	}
	if paths.openPath != wantOpen {
		t.Fatalf("open path mismatch: got=%s want=%s", paths.openPath, wantOpen)
	}
	if paths.unreachablePath != wantUnreachable {
		t.Fatalf("unreachable path mismatch: got=%s want=%s", paths.unreachablePath, wantUnreachable)
	}
}

func TestUnreachableOutput_WhenFinalizeCalledOnSuccess_RenamesTmpToFinal(t *testing.T) {
	dir := t.TempDir()
	finalPath := filepath.Join(dir, "unreachable.csv")

	output, err := openUnreachableOutput(finalPath)
	if err != nil {
		t.Fatalf("open failed: %v", err)
	}

	if err := output.writer.Write(writer.UnreachableRecord{
		IP:     "1.2.3.4",
		IPCidr: "1.2.3.0/24",
		Status: "unreachable",
		Reason: "pre-scan",
	}); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	if err := output.Finalize(true); err != nil {
		t.Fatalf("finalize failed: %v", err)
	}

	if _, err := os.Stat(finalPath); err != nil {
		t.Fatalf("expected final unreachable file, got: %v", err)
	}
	if _, err := os.Stat(finalPath + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("expected no tmp unreachable file, got: %v", err)
	}
}

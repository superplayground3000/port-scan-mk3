package scanapp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/writer"
)

func TestBatchOutputs_WhenCommitCalledOnSuccess_RenamesTmpToFinal(t *testing.T) {
	dir := t.TempDir()
	scanPath := filepath.Join(dir, "scan.csv")
	openPath := filepath.Join(dir, "open.csv")

	outputs, err := openBatchOutputs(scanPath, openPath)
	if err != nil {
		t.Fatalf("open failed: %v", err)
	}

	if err := outputs.scanWriter.Write(writer.Record{
		IP: "1.2.3.4", IPCidr: "1.2.3.0/24", Port: 80, Status: "open",
	}); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	if err := outputs.Finalize(true); err != nil {
		t.Fatalf("finalize failed: %v", err)
	}

	if _, err := os.Stat(scanPath); err != nil {
		t.Fatalf("expected final scan file, got: %v", err)
	}
	if _, err := os.Stat(openPath); err != nil {
		t.Fatalf("expected final open file, got: %v", err)
	}
	if _, err := os.Stat(scanPath + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("expected no tmp scan file, got: %v", err)
	}
}

func TestBatchOutputs_WhenFinalizeCalledOnFailure_KeepsTmpFiles(t *testing.T) {
	dir := t.TempDir()
	scanPath := filepath.Join(dir, "scan.csv")
	openPath := filepath.Join(dir, "open.csv")

	outputs, err := openBatchOutputs(scanPath, openPath)
	if err != nil {
		t.Fatalf("open failed: %v", err)
	}

	if err := outputs.scanWriter.Write(writer.Record{
		IP: "1.2.3.4", IPCidr: "1.2.3.0/24", Port: 80, Status: "open",
	}); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	if err := outputs.Finalize(false); err != nil {
		t.Fatalf("finalize failed: %v", err)
	}

	// final files should NOT exist
	if _, err := os.Stat(scanPath); !os.IsNotExist(err) {
		t.Fatalf("expected no final scan file on failure, got: %v", err)
	}
	// tmp files should exist
	if _, err := os.Stat(scanPath + ".tmp"); err != nil {
		t.Fatalf("expected tmp scan file on failure, got: %v", err)
	}
}

func TestBatchOutputs_WhenFinalizedSuccessfully_ContainsWrittenData(t *testing.T) {
	dir := t.TempDir()
	scanPath := filepath.Join(dir, "scan.csv")
	openPath := filepath.Join(dir, "open.csv")

	outputs, err := openBatchOutputs(scanPath, openPath)
	if err != nil {
		t.Fatalf("open failed: %v", err)
	}

	if err := outputs.scanWriter.Write(writer.Record{
		IP: "1.2.3.4", IPCidr: "1.2.3.0/24", Port: 80, Status: "open",
	}); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	if err := outputs.Finalize(true); err != nil {
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

func TestUnreachableOutput_WhenFinalizeFails_DoesNotOpenScanOrOpenOutputs(t *testing.T) {
	dir := t.TempDir()
	finalPath := filepath.Join(dir, "unreachable.csv")
	if err := os.Mkdir(finalPath, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	paths := batchOutputPaths{
		scanPath:        filepath.Join(dir, "scan.csv"),
		openPath:        filepath.Join(dir, "open.csv"),
		unreachablePath: finalPath,
	}
	if _, err := openBatchOutputsAfterUnreachable(paths); err == nil {
		t.Fatal("expected unreachable finalize to fail")
	}

	if _, err := os.Stat(paths.scanPath + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("expected scan tmp not to be created, got: %v", err)
	}
	if _, err := os.Stat(paths.openPath + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("expected open tmp not to be created, got: %v", err)
	}
}

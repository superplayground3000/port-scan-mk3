package scanapp

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestResolveBatchOutputPaths_WhenNoExistingFiles_UsesBaseTimestampNames(t *testing.T) {
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

func TestResolveBatchOutputPaths_WhenExistingFilesCollide_AppendsIncrementingSuffix(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 3, 2, 1, 30, 45, 0, time.UTC)

	// existing first attempt
	firstScan := filepath.Join(dir, "scan_results-20260302T013045Z.csv")
	firstOpen := filepath.Join(dir, "opened_results-20260302T013045Z.csv")
	if err := os.WriteFile(firstScan, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(firstOpen, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// existing second attempt
	secondScan := filepath.Join(dir, "scan_results-20260302T013045Z-1.csv")
	secondOpen := filepath.Join(dir, "opened_results-20260302T013045Z-1.csv")
	if err := os.WriteFile(secondScan, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secondOpen, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	paths, err := resolveBatchOutputPaths(filepath.Join(dir, "scan_results.csv"), now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantScan := filepath.Join(dir, "scan_results-20260302T013045Z-2.csv")
	wantOpen := filepath.Join(dir, "opened_results-20260302T013045Z-2.csv")
	wantUnreachable := filepath.Join(dir, "unreachable_results-20260302T013045Z-2.csv")
	if paths.scanPath != wantScan || paths.openPath != wantOpen || paths.unreachablePath != wantUnreachable {
		t.Fatalf("unexpected paths: %+v", paths)
	}
}

func TestResolveBatchOutputPaths_WhenOnlyUnreachableCollides_AdvancesSuffix(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 3, 2, 1, 30, 45, 0, time.UTC)

	firstUnreachable := filepath.Join(dir, "unreachable_results-20260302T013045Z.csv")
	if err := os.WriteFile(firstUnreachable, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	paths, err := resolveBatchOutputPaths(filepath.Join(dir, "scan_results.csv"), now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantScan := filepath.Join(dir, "scan_results-20260302T013045Z-1.csv")
	wantOpen := filepath.Join(dir, "opened_results-20260302T013045Z-1.csv")
	wantUnreachable := filepath.Join(dir, "unreachable_results-20260302T013045Z-1.csv")
	if paths.scanPath != wantScan || paths.openPath != wantOpen || paths.unreachablePath != wantUnreachable {
		t.Fatalf("unexpected paths: %+v", paths)
	}
}

func TestResolveBatchOutputPaths_WhenOver100Collisions_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 3, 2, 1, 30, 45, 0, time.UTC)
	ts := now.UTC().Format("20060102T150405Z")

	// Create 100 collision files (seq 0 through 99)
	if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("scan_results-%s.csv", ts)), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for i := 1; i < 100; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("scan_results-%s-%d.csv", ts, i)), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	_, err := resolveBatchOutputPaths(filepath.Join(dir, "scan_results.csv"), now)
	if err == nil {
		t.Fatal("expected error when 100 collisions exist")
	}
	if !strings.Contains(err.Error(), "failed to allocate") {
		t.Fatalf("unexpected error: %v", err)
	}
}

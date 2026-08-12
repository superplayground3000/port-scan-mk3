package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/xuxiping/port-scan-mk3/internal/perfharness"
)

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, os.ErrClosed
}

func TestRunCommandWritesSmokeReports(t *testing.T) {
	t.Parallel()

	outputDir := filepath.Join(t.TempDir(), "report path")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runCommand([]string{
		"-profile", "smoke",
		"-output", outputDir,
		"-smoke-items", "5",
		"-smoke-snapshot-bytes", "4096",
		"-evidence-label", "hardware-qualified",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exit code = %d, stderr=%s", exitCode, stderr.String())
	}
	data, err := os.ReadFile(filepath.Join(outputDir, "report", "performance-report.json"))
	if err != nil {
		t.Fatalf("ReadFile(report): %v", err)
	}
	var report perfharness.Report
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("Unmarshal(report): %v", err)
	}
	if len(report.Cases) != 7 {
		t.Fatalf("case count = %d, want fixture, fake-worker, and loopback-worker cases", len(report.Cases))
	}
	if report.Hardware.EvidenceLabel != perfharness.EvidenceHardwareQualified {
		t.Fatalf("evidence label = %q", report.Hardware.EvidenceLabel)
	}
	if len(report.Contract.Limits) != 12 || len(report.Contract.CancelStages) != 5 {
		t.Fatalf("report lacks the matrix contract: %+v", report.Contract)
	}
	for _, result := range report.Cases {
		if result.Name == "production-workflow/workers-16" {
			if result.FixtureGeneration == nil || len(result.FixtureGeneration.Runs) != 6 {
				t.Fatalf("workflow fixture-generation metrics = %+v", result.FixtureGeneration)
			}
			if result.ColdStart.InputBytes == 0 || result.ColdStart.OutputBytes == 0 {
				t.Fatalf("workflow stage metrics = %+v", result.ColdStart)
			}
			return
		}
	}
	t.Fatal("workers-16 workflow case is missing")
}

func TestRunCommandFailsWhenItCannotWriteStatus(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	exitCode := runCommand([]string{
		"-profile", "smoke",
		"-output", filepath.Join(t.TempDir(), "report"),
		"-smoke-items", "1",
		"-smoke-snapshot-bytes", "1024",
	}, failingWriter{}, &stderr)
	if exitCode != 1 {
		t.Fatalf("exit code = %d, want 1 for status output failure", exitCode)
	}
}

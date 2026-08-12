package scanapp

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/state"
)

// TestRun_AfterWriteFailure_ResumeCoversEveryTarget verifies the saved recovery
// path with ordered results.
//
// The first run saves a corrected cursor. The second run uses that snapshot
// and appends the remaining results to the recorded output files.
func TestRun_AfterWriteFailure_ResumeCoversEveryTarget(t *testing.T) {
	cfg, tmp, bucketsFile := newInterruptibleScanConfig(t)

	// Independent source of truth for how many rows a complete scan owes: the
	// bucket file's own declared totals, read before either run.
	chunks, err := state.Load(bucketsFile)
	if err != nil {
		t.Fatalf("load bucket file: %v", err)
	}
	totalTargets := 0
	for _, ch := range chunks {
		totalTargets += ch.TotalCount
	}
	if totalTargets == 0 {
		t.Fatal("bucket file declares no targets; the fixture is not exercising anything")
	}

	// The third record write stops run 1.
	runErr := Run(context.Background(), scanConfigurationFromFixture(t, cfg), &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{
		DisableKeyboard:    true,
		Dial:               refusingDial,
		batchOutputsOpener: failingScanWriterOpener(3),
	})
	if !errors.Is(runErr, errInjectedWriteFailure) {
		t.Fatalf("run 1 should fail with the injected write error, got: %v", runErr)
	}

	// Run 2 uses the corrected snapshot from the first run.
	if err := Run(context.Background(), scanConfigurationFromFixture(t, cfg), &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{
		DisableKeyboard: true,
		Dial:            refusingDial,
	}); err != nil {
		t.Fatalf("re-running the documented recovery must succeed, got: %v", err)
	}

	// All targets stay in one recorded output file.
	outputs, err := filepath.Glob(filepath.Join(tmp, "scan_results-*.csv"))
	if err != nil {
		t.Fatalf("glob outputs: %v", err)
	}
	best, bestPath := 0, ""
	for _, path := range outputs {
		_, rows := readCSVRows(t, path)
		if len(rows) > best {
			best, bestPath = len(rows), path
		}
	}
	if best < totalTargets {
		t.Fatalf("resume after a corrected save lost targets: the fullest of %d output file(s) (%s) holds %d rows, but the bucket declares %d targets",
			len(outputs), bestPath, best, totalTargets)
	}
	if len(outputs) != 1 {
		t.Fatalf("expected one recorded scan output, got %d (%v)", len(outputs), outputs)
	}

	// The scan opens both output families together.
	openOnly, err := filepath.Glob(filepath.Join(tmp, "opened_results-*.csv"))
	if err != nil {
		t.Fatalf("glob open-only outputs: %v", err)
	}
	if len(openOnly) != len(outputs) {
		t.Fatalf("the two result families left different leftover shapes: %d scan_results-* vs %d opened_results-*; the recovery guidance names both and assumes they match",
			len(outputs), len(openOnly))
	}
}

func TestRun_OutputFailureInjectionUsesRealWritersAndSavesRecoveryState(t *testing.T) {
	cfg, _, bucketsFile := newInterruptibleScanConfig(t)
	var snapshotTelemetry SnapshotTelemetry

	runErr := Run(context.Background(), scanConfigurationFromFixture(t, cfg), &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{
		DisableKeyboard: true,
		Dial:            refusingDial,
		OutputFailure:   &OutputFailureInjection{FailOnResult: 3},
		SnapshotTelemetryObserver: func(telemetry SnapshotTelemetry) {
			snapshotTelemetry = telemetry
		},
	})
	if !errors.Is(runErr, ErrInjectedOutputFailure) {
		t.Fatalf("Run() error = %v, want injected output failure", runErr)
	}
	snapshot, err := state.LoadSnapshot(bucketsFile)
	if err != nil {
		t.Fatalf("load recovery snapshot: %v", err)
	}
	var remaining int
	for _, chunk := range snapshot.Chunks {
		remaining += chunk.Remaining()
	}
	if remaining == 0 {
		t.Fatal("output failure saved no remaining work")
	}
	if snapshotTelemetry.RewoundChunks == 0 {
		t.Fatalf("snapshot telemetry = %+v, want rewound chunks", snapshotTelemetry)
	}
}

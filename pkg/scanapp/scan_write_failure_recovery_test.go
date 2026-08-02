package scanapp

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/state"
)

// TestRun_AfterDeclinedSaveOnWriteFailure_ReResumeCoversEveryTarget pins the
// recovery path the operator is told to take.
//
// The other write-failure tests assert the *proxy* for issue #51 — that no
// resume snapshot is left behind. This one asserts the property that actually
// matters, end to end: after an output-write failure, re-running the same
// `scan -resume` command still covers every target. That is the claim the
// error message, README, and release notes make, so it needs a test that would
// go red if the claim stopped being true.
//
// It is also the most direct regression test for the bug itself. If the fix
// were reverted, run 1 would persist a dispatch cursor covering rows it never
// wrote, run 2 would resume past them, and no output file would hold all the
// targets — exactly the silent data loss #51 describes.
func TestRun_AfterDeclinedSaveOnWriteFailure_ReResumeCoversEveryTarget(t *testing.T) {
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

	// Run 1: writing the 3rd record fails, so the snapshot is declined.
	runErr := Run(context.Background(), cfg, &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{
		DisableKeyboard:    true,
		Dial:               refusingDial,
		batchOutputsOpener: failingScanWriterOpener(3),
	})
	if !errors.Is(runErr, errInjectedWriteFailure) {
		t.Fatalf("run 1 should fail with the injected write error, got: %v", runErr)
	}

	// Run 2: the documented recovery — the same command, nothing else changed.
	if err := Run(context.Background(), cfg, &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{
		DisableKeyboard: true,
		Dial:            refusingDial,
	}); err != nil {
		t.Fatalf("re-running the documented recovery must succeed, got: %v", err)
	}

	// Every target must be covered. The failed run's partial rows are still on
	// disk — here in a separate timestamped file, because this bucket carried no
	// recorded output path; when it does carry one the recovery appends to it and
	// the partial rows become duplicates instead. Either way nothing is lost,
	// which is what the guidance promises, so the assertion is "some output file
	// holds a complete set", not "there is exactly one file".
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
		t.Fatalf("re-resume after a declined save lost targets: the fullest of %d output file(s) (%s) holds %d rows, but the bucket declares %d targets",
			len(outputs), bestPath, best, totalTargets)
	}
}

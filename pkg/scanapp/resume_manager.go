package scanapp

import (
	"errors"
	"fmt"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
	"github.com/xuxiping/port-scan-mk3/pkg/state"
)

func loadResumeSnapshot(cfg config.Config) (state.Snapshot, error) {
	if cfg.Resume == "" {
		return state.Snapshot{}, nil
	}
	return state.LoadSnapshot(cfg.Resume)
}

func persistResumeState(cfg config.Config, opts RunOptions, logger *scanLogger, runtimes []*chunkRuntime, dispatchErr, runErr error) error {
	return persistResumeSnapshot(cfg, opts, logger, runtimes, state.PreScanPingState{}, nil, dispatchErr, runErr)
}

// persistResumeSnapshot writes the resume snapshot when there is resumable work
// (incomplete chunks, a hard error, or a graceful cancel). The output paths are
// recorded so a subsequent -resume appends to the same result files (§3.7). The
// early return when the run finished cleanly is intentional: a fully-scanned run
// has nothing to resume, so recording the output path only matters on the
// cancel/incomplete save — which is exactly when this saves.
//
// One run error is disqualifying: an output-write failure (errScanOutputWrite).
// The dispatch cursor advances when a task is enqueued, so after a write failure
// it covers rows that never reached the output file; saving that cursor would
// make the next -resume treat those rows as done and skip them silently (issue
// #51). Rather than persist a snapshot that quietly loses data, this declines to
// save and returns an error saying so. Every other error class — pressure API,
// executor panic, graceful cancel/Ctrl+C — keeps saving as before, preserving
// the 2.1.0 graceful-interrupt durability contract.
func persistResumeSnapshot(cfg config.Config, opts RunOptions, logger *scanLogger, runtimes []*chunkRuntime, preScanPing state.PreScanPingState, output *state.OutputState, dispatchErr, runErr error) error {
	incomplete := hasIncomplete(runtimes)
	if !incomplete && runErr == nil && !shouldSaveOnDispatchErr(dispatchErr) {
		return nil
	}

	savePath := resumePath(cfg, opts)
	if errors.Is(runErr, errScanOutputWrite) {
		logger.eventf("resume_state_not_saved", "", 0, "resume_state_not_saved", LogEventRuntimeErr, map[string]any{
			"reason":      "scan_output_write_failed",
			"resume_path": savePath,
			"action":      "re-running the same scan -resume covers every target, but this run's already-written rows stay on disk; reconcile before consuming, or re-run generate-buckets for a clean single output",
		})
		return fmt.Errorf(
			"resume state was deliberately NOT saved to %s: writing scan output failed, so the saved dispatch cursor would cover rows that never reached the output file and the next -resume would skip them silently. "+
				"The bucket file is unchanged, still holding the cursor from before this run, so re-running the same scan -resume command covers every target - but the rows this run already wrote stay on disk, either appended again to the same output or left behind as a separate partial scan_results-*.csv, so reconcile before consuming. "+
				"For a clean single output, re-run generate-buckets and scan into a fresh path: %w",
			savePath, runErr)
	}
	if err := state.SaveSnapshot(savePath, state.Snapshot{
		Chunks:      collectChunkStates(runtimes),
		PreScanPing: preScanPing,
		Output:      output,
	}); err != nil {
		return err
	}
	logger.infof("resume state saved to %s", savePath)
	return nil
}

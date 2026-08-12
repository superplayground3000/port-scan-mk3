package scanapp

import (
	"errors"

	"github.com/xuxiping/port-scan-mk3/pkg/state"
)

// persistResumeSnapshot writes the resume snapshot when there is resumable work
// (incomplete chunks, a hard error, or a graceful cancel). The output paths are
// recorded so a subsequent -resume appends to the same result files (§3.7). The
// early return when the run finished cleanly is intentional: a fully-scanned run
// has nothing to resume, so recording the output path only matters on the
// cancel/incomplete save — which is exactly when this saves.
//
// After an output-write failure, the result loop marks each dispatched result
// that did not reach all output writers. The trackers rewind to the first such
// task before this function saves the snapshot. A resumed run can write some
// rows again, but it cannot skip an unwritten row.
func persistResumeSnapshot(savePath string, logger *scanLogger, runtimes []*chunkRuntime, preScanPing state.PreScanPingState, output *state.OutputState, richDenyExcluded bool, dispatchErr, runErr error) (int, error) {
	rewoundChunks := 0
	for _, rt := range runtimes {
		if rt.tracker.RewindUnwritten() {
			rewoundChunks++
		}
	}
	incomplete := hasIncomplete(runtimes)
	if !incomplete && runErr == nil && !shouldSaveOnDispatchErr(dispatchErr) {
		return rewoundChunks, nil
	}

	if err := state.SaveSnapshot(savePath, state.Snapshot{
		Chunks:           collectChunkStates(runtimes),
		PreScanPing:      preScanPing,
		Output:           output,
		RichDenyExcluded: richDenyExcluded,
	}); err != nil {
		return rewoundChunks, err
	}
	if rewoundChunks > 0 {
		reason := "queued_tasks_abandoned"
		if errors.Is(runErr, errScanOutputWrite) {
			reason = "scan_output_write_failed"
		} else if runErr != nil {
			reason = "terminal_error"
		}
		logger.eventf("resume_state_rewound", "", 0, "resume_state_rewound", LogEventRuntimeErr, map[string]any{
			"reason":           reason,
			"resume_path":      savePath,
			"rewound_chunks":   rewoundChunks,
			"duplicate_policy": "resuming can duplicate persisted rows, but it cannot skip an unwritten row",
		})
	}
	logger.infof("resume state saved to %s", savePath)
	return rewoundChunks, nil
}

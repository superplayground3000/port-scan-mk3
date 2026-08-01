package scanapp

import (
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
func persistResumeSnapshot(cfg config.Config, opts RunOptions, logger *scanLogger, runtimes []*chunkRuntime, preScanPing state.PreScanPingState, output *state.OutputState, dispatchErr, runErr error) error {
	incomplete := hasIncomplete(runtimes)
	if !incomplete && runErr == nil && !shouldSaveOnDispatchErr(dispatchErr) {
		return nil
	}

	savePath := resumePath(cfg, opts)
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

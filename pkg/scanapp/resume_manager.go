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
	return persistResumeSnapshot(cfg, opts, logger, runtimes, state.PreScanPingState{}, dispatchErr, runErr)
}

func persistResumeSnapshot(cfg config.Config, opts RunOptions, logger *scanLogger, runtimes []*chunkRuntime, preScanPing state.PreScanPingState, dispatchErr, runErr error) error {
	incomplete := hasIncomplete(runtimes)
	if !incomplete && runErr == nil && !shouldSaveOnDispatchErr(dispatchErr) {
		return nil
	}

	savePath := resumePath(cfg, opts)
	if err := state.SaveSnapshot(savePath, state.Snapshot{
		Chunks:      collectChunkStates(runtimes),
		PreScanPing: preScanPing,
	}); err != nil {
		return err
	}
	logger.infof("resume state saved to %s", savePath)
	return nil
}

package scanapp

import (
	"context"
	"io"

	"github.com/xuxiping/port-scan-mk3/pkg/speedctrl"
)

// resultLoopChannels groups the four channels the scan result loop selects
// over. Grouping them keeps runResultLoop's signature readable and lets a test
// drive the loop directly with pre-primed channels (issue #59).
type resultLoopChannels struct {
	apiErrCh      <-chan error
	executorErrCh <-chan error
	dispatchErrCh <-chan error
	resultCh      <-chan scanResult
	abandonedCh   <-chan scanTask
}

// resultLoopDeps carries the collaborators the loop needs to persist results
// and emit progress/telemetry. Kept as a struct so runResultLoop stays
// unit-testable without reconstructing the whole Run pipeline.
type resultLoopDeps struct {
	outputs                *batchOutputs
	runtimes               []*chunkRuntime
	resultObserver         resultTelemetryObserver
	stdout                 io.Writer
	logger                 *scanLogger
	ctrl                   *speedctrl.Controller
	progressStep           int
	quiet                  bool
	outputFlushResults     int
	resultObserverCallback func(completed uint64)
}

// runResultLoop consumes the scan pipeline's channels until dispatch is done,
// every result has been drained, and the executor error channel has been
// drained to nil, then returns the terminal outcome. It is the single copy of
// the result-loop logic: Run delegates to it so production and test exercise
// exactly the same code (issue #59).
//
// dispatchDone is the loop's initial dispatch-completion state. Run always
// passes false; it is a parameter only so a test can enter the loop with
// dispatch already complete and the result channel already drained, which is
// the exact state in which a pending executor error was previously dropped.
//
// cancel is invoked the first time a fatal error (API, executor, or output
// write) is observed, matching Run's prior behavior. Error precedence is
// preserved: the first non-nil runErr wins, and dispatchErr is reported
// separately so the caller can keep runErr-beats-dispatchErr ordering.
func runResultLoop(cancel context.CancelFunc, dispatchDone bool, chans resultLoopChannels, deps resultLoopDeps) (summary resultSummary, dispatchErr error, runErr error) {
	apiErrCh := chans.apiErrCh
	executorErrCh := chans.executorErrCh
	dispatchErrCh := chans.dispatchErrCh
	resultCh := chans.resultCh
	abandonedCh := chans.abandonedCh
	var committer *outputCommitter
	if deps.outputs != nil {
		committer = newOutputCommitter(outputCommitterConfig{
			outputs:                deps.outputs,
			flushInterval:          deps.outputFlushResults,
			runtimes:               deps.runtimes,
			resultObserver:         deps.resultObserver,
			resultObserverCallback: deps.resultObserverCallback,
			stdout:                 deps.stdout,
			logger:                 deps.logger,
			ctrl:                   deps.ctrl,
			progressStep:           deps.progressStep,
			quiet:                  deps.quiet,
		})
	}

	// The loop must also keep running while executorErrCh is still open
	// (executorErrCh != nil): a recovered worker panic and the result-channel
	// close can become ready together, and a select that consumed the close and
	// dispatch-done without ever consuming the pending executor error dropped it,
	// so Run returned nil after a scan in which a worker died (issue #59).
	// executorErrCh is always eventually closed — reportFatal closes it after
	// sending, and the workerWG waiter closes it via the same sync.Once after all
	// workers finish — so draining it to nil is guaranteed and the loop still
	// terminates.
	for !dispatchDone || resultCh != nil || executorErrCh != nil || abandonedCh != nil {
		select {
		case apiErr := <-apiErrCh:
			if apiErr != nil && runErr == nil {
				runErr = apiErr
				cancel()
			}
		case executorErr, ok := <-executorErrCh:
			if !ok {
				executorErrCh = nil
				continue
			}
			if executorErr != nil && runErr == nil {
				runErr = executorErr
				cancel()
			}
		case err := <-dispatchErrCh:
			dispatchDone = true
			dispatchErr = err
			dispatchErrCh = nil
		case res, ok := <-resultCh:
			if !ok {
				resultCh = nil
				continue
			}
			// Only a result that reached the output file counts as scanned. A late
			// in-flight result is still written after a non-output fatal error. An
			// output error prevents later writes because their result is unknown.
			if committer != nil {
				if err := committer.Accept(res); err != nil {
					runErr = err
					cancel()
				}
			}
		case abandoned, ok := <-abandonedCh:
			if !ok {
				abandonedCh = nil
				continue
			}
			deps.runtimes[abandoned.chunkIdx].tracker.MarkUnwritten(abandoned.taskIdx)
		}
	}
	if committer != nil {
		if err := committer.Finish(); err != nil {
			runErr = err
			cancel()
		}
		summary = committer.Summary()
	}

	return summary, dispatchErr, runErr
}

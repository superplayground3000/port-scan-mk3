package scanapp

import (
	"errors"
	"testing"
)

// TestRunResultLoop_WhenDispatchDoneAndExecutorErrorQueued_SurfacesError drives
// the seam directly into the exact state that previously dropped a recovered
// worker panic (issue #59): dispatch already complete, the result channel
// already drained to nil, and a fatal error queued on the executor error
// channel (then closed). In that state the old exit condition
// (!dispatchDone || resultCh != nil) is already false, so the loop body never
// runs and the queued error is returned as nil — deterministically, every run,
// with no sleeps and no retry wrapper. The fix must consume the executor error
// before deciding the outcome.
func TestRunResultLoop_WhenDispatchDoneAndExecutorErrorQueued_SurfacesError(t *testing.T) {
	wantErr := errors.New("executor worker panic: boom")
	executorErrCh := make(chan error, 1)
	executorErrCh <- wantErr
	close(executorErrCh)

	canceled := false
	cancel := func() { canceled = true }

	_, dispatchErr, runErr := runResultLoop(cancel, true, resultLoopChannels{
		apiErrCh:      nil,
		executorErrCh: executorErrCh,
		dispatchErrCh: nil,
		resultCh:      nil,
	}, resultLoopDeps{})

	if runErr == nil {
		t.Fatalf("expected runResultLoop to surface the queued executor error, got runErr=nil (dispatchErr=%v)", dispatchErr)
	}
	if !errors.Is(runErr, wantErr) {
		t.Fatalf("expected runErr to be the queued executor error %v, got %v", wantErr, runErr)
	}
	if !canceled {
		t.Fatal("expected cancel() to be called when a fatal executor error is observed")
	}
}

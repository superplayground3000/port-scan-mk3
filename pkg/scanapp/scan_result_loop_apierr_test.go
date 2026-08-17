package scanapp

import (
	"errors"
	"sync"
	"testing"
)

// TestRunResultLoop_WhenDispatchDoneAndAPIErrorQueued_SurfacesError is the
// issue #59 regression test with the fatal error moved one channel over, to
// apiErrCh.
//
// The loop selects over apiErrCh but its exit condition is
//
//	!dispatchDone || resultCh != nil || executorErrCh != nil || abandonedCh != nil
//
// which never mentions apiErrCh. Entering with dispatch complete and the other
// three channels already drained to nil makes that condition false on its first
// evaluation, so the loop body never runs and a queued pressure-API failure is
// returned as nil. Run then reports success for a scan that died.
//
// This mirrors TestRunResultLoop_WhenDispatchDoneAndExecutorErrorQueued_SurfacesError
// exactly. That test passes because executorErrCh was added to the exit
// condition when #59 was fixed; apiErrCh was not.
func TestRunResultLoop_WhenDispatchDoneAndAPIErrorQueued_SurfacesError(t *testing.T) {
	wantErr := errors.New("pressure api failed 3 times: scripted pressure failure")
	// Left open on purpose: pollPressureAPI never closes apiErrCh, so closing it
	// here would test a protocol production does not use.
	apiErrCh := make(chan error, 1)
	apiErrCh <- wantErr

	canceled := false
	cancel := func() { canceled = true }

	_, dispatchErr, runErr := runResultLoop(cancel, true, resultLoopChannels{
		apiErrCh:      apiErrCh,
		executorErrCh: nil,
		dispatchErrCh: nil,
		resultCh:      nil,
		abandonedCh:   nil,
	}, resultLoopDeps{})

	if runErr == nil {
		t.Fatalf("expected runResultLoop to surface the queued pressure API error, got runErr=nil (dispatchErr=%v)", dispatchErr)
	}
	if !errors.Is(runErr, wantErr) {
		t.Fatalf("expected runErr to be the queued pressure API error %v, got %v", wantErr, runErr)
	}
	if !canceled {
		t.Fatal("expected cancel() to be called when a fatal pressure API error is observed")
	}
}

// TestRunResultLoop_WhenAPIErrorArrivesAfterAnEarlierFatal_KeepsTheFirstOne
// locks the runErr == nil guard on the drain.
//
// An earlier attempt queued both errors before entry. That is not testable:
// two ready cases have no deterministic winner. The cancel callback is the
// synchronization point that makes the order definite. It runs only after the
// executor error has been consumed, so queueing the pressure error from inside
// it guarantees the executor error came first.
func TestRunResultLoop_WhenAPIErrorArrivesAfterAnEarlierFatal_KeepsTheFirstOne(t *testing.T) {
	executorErr := errors.New("executor worker panic: boom")
	executorErrCh := make(chan error, 1)
	executorErrCh <- executorErr

	apiErrCh := make(chan error, 1)
	var once sync.Once
	cancel := func() {
		once.Do(func() {
			apiErrCh <- errors.New("pressure api failed 3 times: scripted pressure failure")
			close(executorErrCh)
		})
	}

	_, _, runErr := runResultLoop(cancel, true, resultLoopChannels{
		apiErrCh:      apiErrCh,
		executorErrCh: executorErrCh,
	}, resultLoopDeps{})

	if !errors.Is(runErr, executorErr) {
		t.Fatalf("expected the earlier executor error (%v) to win, got %v", executorErr, runErr)
	}
}

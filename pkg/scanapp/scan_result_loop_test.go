package scanapp

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/speedctrl"
	"github.com/xuxiping/port-scan-mk3/pkg/task"
	"github.com/xuxiping/port-scan-mk3/pkg/writer"
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

func TestRunResultLoopFlushesPendingOutputAfterANonOutputError(t *testing.T) {
	wantErr := errors.New("injected executor failure")
	executorErrCh := make(chan error, 1)
	executorErrCh <- wantErr
	close(executorErrCh)
	resultCh := make(chan scanResult, 1)
	resultCh <- scanResult{chunkIdx: 0, taskIdx: 0, record: writer.Record{IP: "192.0.2.1", Status: "open"}}
	close(resultCh)
	scanWriter := &flushCountingWriter{}
	openWriter := &flushCountingWriter{}
	chunk := &task.Chunk{CIDR: "192.0.2.1/32", TotalCount: 1, NextIndex: 1, Status: "scanning"}
	runtimes := []*chunkRuntime{{state: chunk, tracker: newChunkStateTracker(chunk)}}

	summary, _, runErr := runResultLoop(func() {}, true, resultLoopChannels{
		executorErrCh: executorErrCh,
		resultCh:      resultCh,
	}, resultLoopDeps{
		outputs: &batchOutputs{
			scanPath:       "scan.csv",
			openOnlyPath:   "opened.csv",
			scanWriter:     scanWriter,
			openOnlyWriter: openWriter,
		},
		runtimes:           runtimes,
		logger:             newLogger("error", false, &bytes.Buffer{}),
		ctrl:               speedctrl.NewController(),
		outputFlushResults: 1000,
	})

	if !errors.Is(runErr, wantErr) {
		t.Fatalf("run error = %v, want executor failure", runErr)
	}
	if summary.written != 1 || runtimes[0].tracker.ScannedCount() != 1 {
		t.Fatalf("final committed state = summary:%+v chunk:%+v", summary, runtimes[0].tracker.Snapshot())
	}
	if scanWriter.flushes != 1 || openWriter.flushes != 1 {
		t.Fatalf("final flushes = scan:%d open:%d, want one each", scanWriter.flushes, openWriter.flushes)
	}
	if errors.Is(runErr, context.Canceled) {
		t.Fatalf("run error lost the non-output cause: %v", runErr)
	}
}

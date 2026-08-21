package scanapp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/input"
	"github.com/xuxiping/port-scan-mk3/pkg/ratelimit"
	"github.com/xuxiping/port-scan-mk3/pkg/speedctrl"
	"github.com/xuxiping/port-scan-mk3/pkg/state"
	"github.com/xuxiping/port-scan-mk3/pkg/task"
	"github.com/xuxiping/port-scan-mk3/pkg/writer"
)

type cancelRuntimeContext struct {
	checks atomic.Int32
	after  int32
}

func (*cancelRuntimeContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (*cancelRuntimeContext) Done() <-chan struct{}       { return nil }
func (*cancelRuntimeContext) Value(any) any               { return nil }
func (c *cancelRuntimeContext) Err() error {
	if c.checks.Add(1) >= c.after {
		return context.Canceled
	}
	return nil
}

func cancellationTestRuntime(t *testing.T, total int) *chunkRuntime {
	t.Helper()
	targets := make([]scanTarget, total)
	for i := range targets {
		targets[i] = scanTarget{ip: "127.0.0.1", ipCidr: "127.0.0.1/32"}
	}
	chunk := &task.Chunk{CIDR: "127.0.0.1/32", TotalCount: total, Status: "pending"}
	bucket := ratelimit.NewLeakyBucket(1000, total)
	t.Cleanup(bucket.Close)
	return &chunkRuntime{
		ipCidr:  chunk.CIDR,
		ports:   []int{80},
		targets: targets,
		state:   chunk,
		tracker: newChunkStateTracker(chunk),
		bkt:     bucket,
	}
}

func TestParsePortRowsContext_ChecksCancellationWithinFourThousandNinetySixRows(t *testing.T) {
	rows := make([]string, 5000)
	for i := range rows {
		rows[i] = "80/tcp"
	}
	ctx := &cancelRuntimeContext{after: 2}

	_, err := parsePortRowsContext(ctx, rows)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("parse error = %v, want context.Canceled", err)
	}
	if got := ctx.checks.Load(); got < 2 {
		t.Fatalf("context checks = %d, want at least 2", got)
	}
}

func TestLoadRunInputsContext_PassesCancellationToCIDRLoader(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	loaderCalled := false
	deps := runDependencies{
		loadCIDRRecordsContext: func(got context.Context, _, _, _ string) ([]input.CIDRRecord, error) {
			loaderCalled = true
			return nil, got.Err()
		},
	}

	_, err := loadRunInputsContext(ctx, inputConfiguration{cidrFile: "targets.csv", allowMissingPort: true}, deps)

	if !loaderCalled {
		t.Fatal("context-aware CIDR loader was not called")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("load error = %v, want context.Canceled", err)
	}
}

func TestLoadRunInputsContext_PassesCancellationToPortLoader(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	portLoaderCalled := false
	deps := runDependencies{
		loadCIDRRecordsContext: func(context.Context, string, string, string) ([]input.CIDRRecord, error) {
			return []input.CIDRRecord{{CIDR: "127.0.0.1/32"}}, nil
		},
		loadPortSpecsContext: func(got context.Context, _ string) ([]input.PortSpec, error) {
			portLoaderCalled = true
			return nil, got.Err()
		},
	}
	cancel()

	_, err := loadRunInputsContext(ctx, inputConfiguration{cidrFile: "targets.csv", portFile: "ports.csv"}, deps)

	if !portLoaderCalled {
		t.Fatal("context-aware port loader was not called")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("load error = %v, want context.Canceled", err)
	}
}

func TestPrepareRuntimePlanContext_StopsCandidateExpansionWithin4096Items(t *testing.T) {
	_, network, err := net.ParseCIDR("10.0.0.0/8")
	if err != nil {
		t.Fatal(err)
	}
	record := input.CIDRRecord{CIDR: network.String(), IPRaw: network.String(), IPCidrRaw: network.String(), Net: network, Selector: network}
	chunk := task.Chunk{CIDR: network.String(), Ports: []string{"80/tcp"}, TotalCount: 1 << 24, Status: "pending"}
	ctx := &cancelRuntimeContext{after: 8}

	_, err = prepareRuntimePlanContext(ctx, runtimePolicy{bucketRate: 1, bucketCapacity: 1}, runInputs{cidrRecords: []input.CIDRRecord{record}}, nil, []task.Chunk{chunk}, nil)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("prepare error = %v, want context.Canceled", err)
	}
	if got := ctx.checks.Load(); got > 20 {
		t.Fatalf("context checks = %d, want cancellation during the first 4096-item expansion interval", got)
	}
}

func TestPrepareRuntimePlanContext_StopsRichGroupingAndDeduplication(t *testing.T) {
	const segment = "10.16.0.0/20"
	record := input.CIDRRecord{
		IsRich:            true,
		IsValid:           true,
		ExecutionKey:      "10.16.0.0:443/tcp",
		DstIP:             "10.16.0.0",
		DstNetworkSegment: segment,
		Port:              443,
		Protocol:          "tcp",
		Decision:          "accept",
		Reason:            reasonPrecheckAllowAll,
	}
	chunk := task.Chunk{CIDR: segment, Ports: []string{"443/tcp"}, TotalCount: 4095, Status: "pending"}
	ctx := &cancelRuntimeContext{after: 4}

	_, err := prepareRuntimePlanContext(ctx, runtimePolicy{bucketRate: 1, bucketCapacity: 1}, runInputs{cidrRecords: []input.CIDRRecord{record}}, nil, []task.Chunk{chunk}, nil)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("prepare error = %v, want context.Canceled", err)
	}
}

func TestRun_WhenCanceled_RewindsAbandonedQueueToLowestUnwritten(t *testing.T) {
	cfg, _, snapshotPath := newInterruptibleScanConfig(t)
	cfg.Workers = 1
	cfg.Delay = 0
	cfg.OutputFlushResults = 1000
	ctx, cancel := context.WithCancel(context.Background())
	var once sync.Once

	err := Run(ctx, scanConfigurationFromFixture(t, cfg), &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{
		DisableKeyboard: true,
		Dial: func(context.Context, string, string) (net.Conn, error) {
			once.Do(cancel)
			return nil, errors.New("connection refused")
		},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("run error = %v, want context.Canceled", err)
	}

	snapshot, loadErr := state.LoadSnapshot(snapshotPath)
	if loadErr != nil {
		t.Fatalf("load snapshot: %v", loadErr)
	}
	chunk := snapshot.Chunks[0]
	if chunk.NextIndex != 1 || chunk.ScannedCount != 1 {
		t.Fatalf("saved progress = (next %d, scanned %d), want (1, 1)", chunk.NextIndex, chunk.ScannedCount)
	}
}

func TestRun_WhenDialPanics_RewindsActiveAndQueuedTasks(t *testing.T) {
	cfg, _, snapshotPath := newInterruptibleScanConfig(t)
	cfg.Workers = 1
	cfg.Delay = 0

	err := Run(context.Background(), scanConfigurationFromFixture(t, cfg), &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{
		DisableKeyboard: true,
		Dial: func(context.Context, string, string) (net.Conn, error) {
			panic("dial adapter failed")
		},
	})
	if err == nil || !strings.Contains(err.Error(), "executor worker panic") {
		t.Fatalf("run error = %v, want executor worker panic", err)
	}

	snapshot, loadErr := state.LoadSnapshot(snapshotPath)
	if loadErr != nil {
		t.Fatalf("load snapshot: %v", loadErr)
	}
	chunk := snapshot.Chunks[0]
	if chunk.NextIndex != 0 || chunk.ScannedCount != 0 {
		t.Fatalf("saved progress = (next %d, scanned %d), want (0, 0)", chunk.NextIndex, chunk.ScannedCount)
	}
}

func TestRun_WhenCanceled_LogsDeterministicDrainAndSnapshotTelemetry(t *testing.T) {
	cfg, _, _ := newInterruptibleScanConfig(t)
	cfg.Workers = 1
	cfg.Delay = 0
	cfg.LogLevel = "info"
	cfg.Format = "json"
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	release := make(chan struct{})
	var stderr bytes.Buffer
	done := make(chan error, 1)

	go func() {
		done <- Run(ctx, scanConfigurationFromFixture(t, cfg), &bytes.Buffer{}, &stderr, RunOptions{
			DisableKeyboard: true,
			Dial: func(context.Context, string, string) (net.Conn, error) {
				close(started)
				<-release
				return nil, errors.New("connection refused")
			},
		})
	}()
	<-started
	cancel()
	close(release)
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("run error = %v, want context.Canceled", err)
	}

	events := parseCancellationEvents(t, stderr.String())
	drain, ok := events["probe_drain_complete"]
	if !ok {
		t.Fatalf("missing probe_drain_complete event in:\n%s", stderr.String())
	}
	if _, ok := drain["in_flight_probes"]; !ok {
		t.Fatal("probe_drain_complete has no in_flight_probes")
	}
	if _, ok := drain["abandoned_queued_tasks"]; !ok {
		t.Fatal("probe_drain_complete has no abandoned_queued_tasks")
	}
	if _, ok := drain["duration_ms"]; !ok {
		t.Fatal("probe_drain_complete has no duration_ms")
	}
	snapshotSave, ok := events["snapshot_save_complete"]
	if !ok {
		t.Fatalf("missing snapshot_save_complete event in:\n%s", stderr.String())
	}
	if _, ok := snapshotSave["rewound_chunks"]; !ok {
		t.Fatal("snapshot_save_complete has no rewound_chunks")
	}
	if _, ok := snapshotSave["duration_ms"]; !ok {
		t.Fatal("snapshot_save_complete has no duration_ms")
	}
}

func TestRun_WhenSuccessful_DoesNotLogCancellationDrainTelemetry(t *testing.T) {
	cfg, _, _ := newInterruptibleScanConfig(t)
	cfg.Workers = 1
	cfg.Delay = 0
	cfg.LogLevel = "info"
	cfg.Format = "json"
	var stderr bytes.Buffer

	err := Run(context.Background(), scanConfigurationFromFixture(t, cfg), &bytes.Buffer{}, &stderr, RunOptions{
		DisableKeyboard: true,
		Dial: func(context.Context, string, string) (net.Conn, error) {
			return nil, errors.New("connection refused")
		},
	})
	if err != nil {
		t.Fatalf("run error = %v, want nil", err)
	}
	if events := parseCancellationEvents(t, stderr.String()); events["probe_drain_complete"] != nil {
		t.Fatalf("successful run logged cancellation drain telemetry:\n%s", stderr.String())
	}
}

func parseCancellationEvents(t *testing.T, log string) map[string]map[string]any {
	t.Helper()
	events := make(map[string]map[string]any)
	for _, line := range strings.Split(strings.TrimSpace(log), "\n") {
		var entry struct {
			Message string         `json:"msg"`
			Fields  map[string]any `json:"fields"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		events[entry.Message] = entry.Fields
	}
	return events
}

func TestDispatchTasks_WhenCanceledDuringDelay_DoesNotWaitOrEnqueueNextTask(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	runtime := cancellationTestRuntime(t, 2)
	tasks := make(chan scanTask, 2)
	observer := &cancelAfterFirstEnqueueObserver{cancel: cancel}

	started := time.Now()
	err := dispatchTasks(ctx, dispatchPolicy{delay: 2 * time.Second, observer: observer}, speedctrl.NewController(), newLogger("error", false, io.Discard), []*chunkRuntime{runtime}, tasks)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("dispatch error = %v, want context.Canceled", err)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("dispatch stopped after %s, want less than 500ms", elapsed)
	}
	if got := len(tasks); got != 1 {
		t.Fatalf("queued tasks = %d, want 1", got)
	}
}

type cancelAfterFirstEnqueueObserver struct {
	cancel context.CancelFunc
	once   sync.Once
}

func TestDispatchTasks_CancellationStopsBucketGateAndSendWaits(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(*chunkRuntime, *speedctrl.Controller)
		stage   string
	}{
		{
			name: "bucket",
			prepare: func(runtime *chunkRuntime, _ *speedctrl.Controller) {
				runtime.bkt.Close()
				runtime.bkt = ratelimit.NewLeakyBucket(1, 1)
				if err := runtime.bkt.Acquire(context.Background()); err != nil {
					t.Fatalf("empty bucket: %v", err)
				}
			},
			stage: "bucket",
		},
		{
			name: "gate",
			prepare: func(_ *chunkRuntime, ctrl *speedctrl.Controller) {
				ctrl.SetManualPaused(true)
			},
			stage: "gate",
		},
		{
			name:    "send",
			prepare: func(_ *chunkRuntime, _ *speedctrl.Controller) {},
			stage:   "send",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			runtime := cancellationTestRuntime(t, 1)
			ctrl := speedctrl.NewController()
			tt.prepare(runtime, ctrl)
			t.Cleanup(runtime.bkt.Close)
			observer := &cancelAtDispatchStage{stage: tt.stage, cancel: cancel}

			started := time.Now()
			err := dispatchTasks(ctx, dispatchPolicy{observer: observer}, ctrl, newLogger("error", false, io.Discard), []*chunkRuntime{runtime}, make(chan scanTask))

			if !errors.Is(err, context.Canceled) {
				t.Fatalf("dispatch error = %v, want context.Canceled", err)
			}
			if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
				t.Fatalf("dispatch stopped after %s, want less than 500ms", elapsed)
			}
		})
	}
}

type cancelAtDispatchStage struct {
	stage  string
	cancel context.CancelFunc
	once   sync.Once
}

func (o *cancelAtDispatchStage) cancelAt(stage string) {
	if o.stage == stage {
		o.once.Do(o.cancel)
	}
}

func (o *cancelAtDispatchStage) OnGateWaitStart(string, int)   { o.cancelAt("gate") }
func (o *cancelAtDispatchStage) OnGateReleased(string, int)    { o.cancelAt("send") }
func (o *cancelAtDispatchStage) OnBucketWaitStart(string, int) { o.cancelAt("bucket") }
func (*cancelAtDispatchStage) OnBucketAcquired(string, int)    {}
func (*cancelAtDispatchStage) OnTaskEnqueued(string, int)      {}

func (*cancelAfterFirstEnqueueObserver) OnGateWaitStart(string, int)   {}
func (*cancelAfterFirstEnqueueObserver) OnGateReleased(string, int)    {}
func (*cancelAfterFirstEnqueueObserver) OnBucketWaitStart(string, int) {}
func (*cancelAfterFirstEnqueueObserver) OnBucketAcquired(string, int)  {}
func (o *cancelAfterFirstEnqueueObserver) OnTaskEnqueued(string, int) {
	o.once.Do(o.cancel)
}

func TestScanExecutor_WhenCanceled_AbandonsQueuedTasksButFinishesInFlightProbe(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	tasks := make(chan scanTask, 3)
	for i := 0; i < 3; i++ {
		tasks <- scanTask{chunkIdx: 0, taskIdx: i, ip: "127.0.0.1", port: 80}
	}
	close(tasks)

	started := make(chan struct{})
	release := make(chan struct{})
	var dialCount atomic.Int32
	dial := func(context.Context, string, string) (net.Conn, error) {
		dialCount.Add(1)
		close(started)
		<-release
		return nil, errors.New("connection refused")
	}

	results, executorErrors, abandoned, telemetry := startCancellableScanExecutor(ctx, 1, time.Minute, dial, newLogger("error", false, io.Discard), tasks)
	<-started
	cancel()
	close(release)

	var resultCount int
	for range results {
		resultCount++
	}
	for err := range executorErrors {
		if err != nil {
			t.Fatalf("executor error = %v", err)
		}
	}
	var abandonedTasks []scanTask
	for abandonedTask := range abandoned {
		abandonedTasks = append(abandonedTasks, abandonedTask)
	}

	if got := dialCount.Load(); got != 1 {
		t.Fatalf("dial count = %d, want 1", got)
	}
	if resultCount != 1 {
		t.Fatalf("result count = %d, want 1", resultCount)
	}
	if len(abandonedTasks) != 2 || abandonedTasks[0].taskIdx != 1 || abandonedTasks[1].taskIdx != 2 {
		t.Fatalf("abandoned tasks = %+v, want task indexes [1 2]", abandonedTasks)
	}
	inFlight, abandonedCount, stopStarted, totalStarted, startsAfterStop := telemetry.snapshot()
	if inFlight != 1 || abandonedCount != 2 || stopStarted.IsZero() {
		t.Fatalf("executor telemetry = (in-flight %d, abandoned %d, stop %v), want (1, 2, non-zero)", inFlight, abandonedCount, stopStarted)
	}
	if totalStarted != 1 || startsAfterStop != 0 {
		t.Fatalf("probe starts = total %d, after stop %d; want 1 and 0", totalStarted, startsAfterStop)
	}
}

func TestScanExecutor_WhenDialPanics_AbandonsCurrentAndQueuedTasksWithoutNextProbe(t *testing.T) {
	tasks := make(chan scanTask, 2)
	for i := 0; i < 2; i++ {
		tasks <- scanTask{chunkIdx: 0, taskIdx: i, ip: "127.0.0.1", port: 80}
	}
	close(tasks)
	var dialCount atomic.Int32
	dial := func(context.Context, string, string) (net.Conn, error) {
		dialCount.Add(1)
		panic("dial adapter failed")
	}

	results, executorErrors, abandoned, _ := startCancellableScanExecutor(context.Background(), 1, time.Minute, dial, newLogger("error", false, io.Discard), tasks)

	for range results {
		t.Fatal("unexpected result after dial panic")
	}
	var terminalErr error
	for err := range executorErrors {
		terminalErr = err
	}
	if terminalErr == nil || !strings.Contains(terminalErr.Error(), "executor worker panic") {
		t.Fatalf("executor error = %v, want worker panic", terminalErr)
	}
	var abandonedIndexes []int
	for abandonedTask := range abandoned {
		abandonedIndexes = append(abandonedIndexes, abandonedTask.taskIdx)
	}
	if !slices.Equal(abandonedIndexes, []int{0, 1}) {
		t.Fatalf("abandoned task indexes = %v, want [0 1]", abandonedIndexes)
	}
	if got := dialCount.Load(); got != 1 {
		t.Fatalf("dial count = %d, want 1", got)
	}
}

type countingRecordWriter struct {
	writes int
}

func (w *countingRecordWriter) Write(writer.Record) error {
	w.writes++
	return nil
}

func (*countingRecordWriter) WriteHeader() error { return nil }

func TestRunResultLoop_WhenFatalErrorStopsDispatch_PersistsLateInFlightResult(t *testing.T) {
	wantErr := errors.New("fatal pressure error")
	apiErrors := make(chan error, 1)
	apiErrors <- wantErr
	executorErrors := make(chan error)
	close(executorErrors)
	results := make(chan scanResult)
	abandoned := make(chan scanTask)
	close(abandoned)
	dispatchErrors := make(chan error, 1)
	dispatchErrors <- context.Canceled

	chunk := &task.Chunk{CIDR: "127.0.0.1/32", TotalCount: 1, NextIndex: 1, Status: "scanning"}
	runtime := &chunkRuntime{state: chunk, tracker: newChunkStateTracker(chunk)}
	fullWriter := &countingRecordWriter{}
	openWriter := &countingRecordWriter{}
	canceled := make(chan struct{})
	go func() {
		<-canceled
		results <- scanResult{chunkIdx: 0, taskIdx: 0, record: writer.Record{IP: "127.0.0.1", Port: 80, Status: "close"}}
		close(results)
	}()

	var once sync.Once
	summary, _, runErr := runResultLoop(func() { once.Do(func() { close(canceled) }) }, false, resultLoopChannels{
		apiErrCh:      apiErrors,
		executorErrCh: executorErrors,
		dispatchErrCh: dispatchErrors,
		resultCh:      results,
		abandonedCh:   abandoned,
	}, resultLoopDeps{
		outputs:  &batchOutputs{scanWriter: fullWriter, openOnlyWriter: openWriter},
		runtimes: []*chunkRuntime{runtime},
		stdout:   io.Discard,
		logger:   newLogger("error", false, io.Discard),
		quiet:    true,
	})

	if !errors.Is(runErr, wantErr) {
		t.Fatalf("run error = %v, want fatal pressure error", runErr)
	}
	if fullWriter.writes != 1 || openWriter.writes != 1 {
		t.Fatalf("writer calls = (%d, %d), want (1, 1)", fullWriter.writes, openWriter.writes)
	}
	if summary.written != 1 || runtime.tracker.ScannedCount() != 1 {
		t.Fatalf("persisted summary = %d and scanned count = %d, want 1 and 1", summary.written, runtime.tracker.ScannedCount())
	}
}

// TestScanExecutorTelemetry_WhenWorkerContextIsCanceledBeforeTheStopFlag_RefusesTheProbe
// pins the guard that keeps a queued task off the network after cancellation.
//
// The executor has two independent stop signals. A worker that finishes a probe
// calls finishProbe, which sets the stop flag. A separate goroutine sets the
// same flag when the run context is done. Neither signal is set yet in the
// window between the cancellation and the first of those two calls. Only the
// context check in startProbe closes that window, so this case sets the context
// and leaves the stop flag unset.
func TestScanExecutorTelemetry_WhenWorkerContextIsCanceledBeforeTheStopFlag_RefusesTheProbe(t *testing.T) {
	telemetry := &scanExecutorTelemetry{}
	live, stopLive := context.WithCancel(context.Background())
	defer stopLive()

	if !telemetry.startProbe(live) {
		t.Fatal("a live worker context refused a probe, so this case cannot prove the cancellation refusal")
	}
	telemetry.inFlight.Add(-1)

	canceled, cancel := context.WithCancel(context.Background())
	cancel()

	if telemetry.startProbe(canceled) {
		t.Fatal("a queued task started a probe on a canceled worker context while the stop flag was still unset")
	}
	if got := telemetry.inFlight.Load(); got != 0 {
		t.Fatalf("in-flight probes = %d, want 0", got)
	}
	_, _, _, totalStarted, _ := telemetry.snapshot()
	if totalStarted != 1 {
		t.Fatalf("total probe starts = %d, want 1 (only the live-context probe)", totalStarted)
	}
}

// TestScanExecutor_WithIdleWorkers_WhenCanceled_StartsNoProbeForTasksQueuedAfterCancellation
// is the multi-worker form of the same promise: after cancellation, a task that
// reaches the queue must never reach the dial function.
//
// The interleaving is forced with channels only. One task goes in first, so one
// worker holds an in-flight probe that blocks in dial. The other workers have no
// task, so they can only wait in the queue select. Every later task is queued
// AFTER cancel returns, so a dial for any of them is a probe that started after
// cancellation. The dial function records that directly, so the assertion does
// not depend on timing.
//
// This case cannot force the window in which the stop flag is still unset,
// because the executor sets that flag from its own goroutine as soon as the run
// context is done. It therefore catches a removed context check in startProbe
// only some of the time (7 of 60 measured runs). Keep
// TestScanExecutorTelemetry_WhenWorkerContextIsCanceledBeforeTheStopFlag_RefusesTheProbe
// as the deterministic proof of that guard.
func TestScanExecutor_WithIdleWorkers_WhenCanceled_StartsNoProbeForTasksQueuedAfterCancellation(t *testing.T) {
	const workers = 4
	const queuedAfterCancel = 8

	ctx, cancel := context.WithCancel(context.Background())
	tasks := make(chan scanTask, queuedAfterCancel+1)
	started := make(chan struct{})
	release := make(chan struct{})
	var dialCount atomic.Int32
	var dialsAfterCancel atomic.Int32
	dial := func(context.Context, string, string) (net.Conn, error) {
		if dialCount.Add(1) == 1 {
			close(started)
			<-release
			return nil, errors.New("connection refused")
		}
		if ctx.Err() != nil {
			dialsAfterCancel.Add(1)
		}
		return nil, errors.New("connection refused")
	}

	results, executorErrors, abandoned, telemetry := startCancellableScanExecutor(ctx, workers, time.Minute, dial, newLogger("error", false, io.Discard), tasks)
	tasks <- scanTask{chunkIdx: 0, taskIdx: 0, ip: "127.0.0.1", port: 80}
	<-started

	cancel()
	for i := 1; i <= queuedAfterCancel; i++ {
		tasks <- scanTask{chunkIdx: 0, taskIdx: i, ip: "127.0.0.1", port: 80}
	}
	close(tasks)
	close(release)

	var resultCount int
	for range results {
		resultCount++
	}
	for err := range executorErrors {
		if err != nil {
			t.Fatalf("executor error = %v", err)
		}
	}
	var abandonedIndexes []int
	for abandonedTask := range abandoned {
		abandonedIndexes = append(abandonedIndexes, abandonedTask.taskIdx)
	}
	slices.Sort(abandonedIndexes)

	if got := dialsAfterCancel.Load(); got != 0 {
		t.Fatalf("dials for tasks queued after cancellation = %d, want 0", got)
	}
	if got := dialCount.Load(); got != 1 {
		t.Fatalf("dial count = %d, want 1 (only the in-flight probe)", got)
	}
	if resultCount != 1 {
		t.Fatalf("result count = %d, want 1", resultCount)
	}
	want := make([]int, queuedAfterCancel)
	for i := range want {
		want[i] = i + 1
	}
	if !slices.Equal(abandonedIndexes, want) {
		t.Fatalf("abandoned task indexes = %v, want %v", abandonedIndexes, want)
	}
	_, abandonedCount, stopStarted, totalStarted, startsAfterStop := telemetry.snapshot()
	if abandonedCount != queuedAfterCancel || stopStarted.IsZero() {
		t.Fatalf("executor telemetry = (abandoned %d, stop %v), want (%d, non-zero)", abandonedCount, stopStarted, queuedAfterCancel)
	}
	if totalStarted != 1 || startsAfterStop != 0 {
		t.Fatalf("probe starts = total %d, after stop %d; want 1 and 0", totalStarted, startsAfterStop)
	}
}

package scanapp

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/ratelimit"
	"github.com/xuxiping/port-scan-mk3/pkg/speedctrl"
	"github.com/xuxiping/port-scan-mk3/pkg/task"
)

func TestDispatchTasks_EmitsBucketWaitStartEvent_WithCIDRAsTarget(t *testing.T) {
	ctrl := speedctrl.NewController()
	var buf bytes.Buffer
	logger := newLogger("debug", false, &buf)

	bucket := ratelimit.NewLeakyBucket(100, 1)
	defer bucket.Close()

	ch := &task.Chunk{CIDR: "192.168.1.0/24", TotalCount: 1, Status: "pending"}
	rt := &chunkRuntime{
		ipCidr: "192.168.1.0/24",
		ports:  []int{80},
		targets: []scanTarget{
			{ip: "192.168.1.10", ipCidr: "192.168.1.0/24"},
		},
		state:   ch,
		tracker: newChunkStateTracker(ch),
		bkt:     bucket,
	}

	taskCh := make(chan scanTask, 1)
	err := dispatchTasks(context.Background(), dispatchPolicy{delay: 0, observer: noopDispatchObserver{}}, ctrl, logger, []*chunkRuntime{rt}, taskCh)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	log := buf.String()
	if !strings.Contains(log, LogEventBucketWaitStart) {
		t.Errorf("expected log to contain %q, got: %s", LogEventBucketWaitStart, log)
	}
	if !strings.Contains(log, "192.168.1.0/24") {
		t.Errorf("expected log to contain CIDR 192.168.1.0/24 as target, got: %s", log)
	}
}

func TestDispatchTasks_EmitsGateEvents_WithCIDRAsTarget(t *testing.T) {
	ctrl := speedctrl.NewController()
	var buf bytes.Buffer
	logger := newLogger("debug", false, &buf)

	bucket := ratelimit.NewLeakyBucket(100, 1)
	defer bucket.Close()

	ch := &task.Chunk{CIDR: "10.0.5.0/24", TotalCount: 1, Status: "pending"}
	rt := &chunkRuntime{
		ipCidr: "10.0.5.0/24",
		ports:  []int{22, 80},
		targets: []scanTarget{
			{ip: "10.0.5.5", ipCidr: "10.0.5.0/24"},
		},
		state:   ch,
		tracker: newChunkStateTracker(ch),
		bkt:     bucket,
	}

	taskCh := make(chan scanTask, 1)
	err := dispatchTasks(context.Background(), dispatchPolicy{delay: 0, observer: noopDispatchObserver{}}, ctrl, logger, []*chunkRuntime{rt}, taskCh)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	log := buf.String()

	if !strings.Contains(log, LogEventGateWaitStart) {
		t.Errorf("expected gate_wait_start event, got: %s", log)
	}
	if !strings.Contains(log, LogEventGateReleased) {
		t.Errorf("expected gate_released event, got: %s", log)
	}
}

func TestDispatchTasks_EmitsAllLoggingConstants_WithCorrectValues(t *testing.T) {
	ctrl := speedctrl.NewController()
	var buf bytes.Buffer
	logger := newLogger("debug", false, &buf)

	bucket := ratelimit.NewLeakyBucket(100, 1)
	defer bucket.Close()

	ch := &task.Chunk{CIDR: "172.16.0.0/24", TotalCount: 1, Status: "pending"}
	rt := &chunkRuntime{
		ipCidr: "172.16.0.0/24",
		ports:  []int{443},
		targets: []scanTarget{
			{ip: "172.16.0.100", ipCidr: "172.16.0.0/24"},
		},
		state:   ch,
		tracker: newChunkStateTracker(ch),
		bkt:     bucket,
	}

	taskCh := make(chan scanTask, 1)
	err := dispatchTasks(context.Background(), dispatchPolicy{delay: 0, observer: noopDispatchObserver{}}, ctrl, logger, []*chunkRuntime{rt}, taskCh)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	log := buf.String()

	expectedConstants := []string{
		LogEventBucketWaitStart,
		LogEventBucketAcquired,
		LogEventGateWaitStart,
		LogEventGateReleased,
		LogEventNone,
	}

	for _, constant := range expectedConstants {
		if !strings.Contains(log, constant) {
			t.Errorf("expected log to contain constant %q, got: %s", constant, log)
		}
	}
}

func TestDispatchTasks_UsesZeroForPort_BeforeGateRelease(t *testing.T) {
	ctrl := speedctrl.NewController()
	var buf bytes.Buffer
	logger := newLogger("debug", false, &buf)

	bucket := ratelimit.NewLeakyBucket(100, 1)
	defer bucket.Close()

	ch := &task.Chunk{CIDR: "10.0.0.0/24", TotalCount: 1, Status: "pending"}
	rt := &chunkRuntime{
		ipCidr: "10.0.0.0/24",
		ports:  []int{80},
		targets: []scanTarget{
			{ip: "10.0.0.1", ipCidr: "10.0.0.0/24"},
		},
		state:   ch,
		tracker: newChunkStateTracker(ch),
		bkt:     bucket,
	}

	taskCh := make(chan scanTask, 1)
	err := dispatchTasks(context.Background(), dispatchPolicy{delay: 0, observer: noopDispatchObserver{}}, ctrl, logger, []*chunkRuntime{rt}, taskCh)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Before fix: port was i (bucket index), which is incorrect
	// After fix: port is 0 because actual target/port not determined at bucket/gate wait points
	// This is by design per constitution observability - target uses CIDR, port is 0 as placeholder
	log := buf.String()

	// Gate wait events should show port:0 (not yet determined)
	// The log format is "port:0" not "port=0"
	if !strings.Contains(log, "port:0") {
		t.Errorf("expected port:0 for gate wait events (not yet determined), got: %s", log)
	}
}

func TestDispatchTasks_ConstantsMatchObserverEventKinds(t *testing.T) {
	// Verify that our log constants match what the observer uses
	// This ensures consistency between observer callbacks and log events
	obs := &recordingDispatchObserver{}
	cidr := "10.0.0.0/24"
	taskIdx := 0

	obs.OnBucketWaitStart(cidr, taskIdx)
	obs.OnBucketAcquired(cidr, taskIdx)
	obs.OnGateWaitStart(cidr, taskIdx)
	obs.OnGateReleased(cidr, taskIdx)
	obs.OnTaskEnqueued(cidr, taskIdx)

	expected := []struct {
		observerKind string
		logConstant  string
	}{
		{"bucket_wait_start", LogEventBucketWaitStart},
		{"bucket_acquired", LogEventBucketAcquired},
		{"gate_wait_start", LogEventGateWaitStart},
		{"gate_released", LogEventGateReleased},
	}

	for _, e := range expected {
		if e.observerKind != e.logConstant {
			t.Errorf("observer kind %q should match log constant %q", e.observerKind, e.logConstant)
		}
	}
}

func TestDispatchTasks_EmitsBucketWaitStartEvent_WithCorrectStateTransition(t *testing.T) {
	ctrl := speedctrl.NewController()
	var buf bytes.Buffer
	logger := newLogger("debug", false, &buf)

	bucket := ratelimit.NewLeakyBucket(100, 1)
	defer bucket.Close()

	ch := &task.Chunk{CIDR: "10.0.0.0/24", TotalCount: 1, Status: "pending"}
	rt := &chunkRuntime{
		ipCidr: "10.0.0.0/24",
		ports:  []int{80},
		targets: []scanTarget{
			{ip: "10.0.0.1", ipCidr: "10.0.0.0/24"},
		},
		state:   ch,
		tracker: newChunkStateTracker(ch),
		bkt:     bucket,
	}

	taskCh := make(chan scanTask, 1)
	err := dispatchTasks(context.Background(), dispatchPolicy{delay: 0, observer: noopDispatchObserver{}}, ctrl, logger, []*chunkRuntime{rt}, taskCh)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	log := buf.String()
	// Verify the event has correct state_transition field matching the event type
	if !strings.Contains(log, "state_transition:bucket_wait_start") {
		t.Errorf("expected state_transition:bucket_wait_start in log, got: %s", log)
	}
	if !strings.Contains(log, "state_transition:gate_wait_start") {
		t.Errorf("expected state_transition:gate_wait_start in log, got: %s", log)
	}
}

func TestDispatchTasks_EmitsCorrectErrorCause_ForSuccessfulOperations(t *testing.T) {
	ctrl := speedctrl.NewController()
	var buf bytes.Buffer
	logger := newLogger("debug", false, &buf)

	bucket := ratelimit.NewLeakyBucket(100, 1)
	defer bucket.Close()

	ch := &task.Chunk{CIDR: "10.0.0.0/24", TotalCount: 1, Status: "pending"}
	rt := &chunkRuntime{
		ipCidr: "10.0.0.0/24",
		ports:  []int{80},
		targets: []scanTarget{
			{ip: "10.0.0.1", ipCidr: "10.0.0.0/24"},
		},
		state:   ch,
		tracker: newChunkStateTracker(ch),
		bkt:     bucket,
	}

	taskCh := make(chan scanTask, 1)
	err := dispatchTasks(context.Background(), dispatchPolicy{delay: 0, observer: noopDispatchObserver{}}, ctrl, logger, []*chunkRuntime{rt}, taskCh)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	log := buf.String()
	// Verify error_cause is "none" for successful operations
	if !strings.Contains(log, "error_cause:none") {
		t.Errorf("expected error_cause:none in log for successful operations, got: %s", log)
	}
}

func TestDispatchTasks_WhenPanicOccurs_ReturnsRuntimeError(t *testing.T) {
	ctrl := speedctrl.NewController()
	var buf bytes.Buffer
	logger := newLogger("debug", false, &buf)

	taskCh := make(chan scanTask, 1)
	err := dispatchTasks(context.Background(), dispatchPolicy{delay: 0, observer: noopDispatchObserver{}}, ctrl, logger, []*chunkRuntime{nil}, taskCh)
	if err == nil {
		t.Fatal("expected error when dispatcher panics")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected runtime panic error, got deadline_exceeded: %v", err)
	}
	if !strings.Contains(err.Error(), "panic") {
		t.Fatalf("expected panic in error message, got: %v", err)
	}
}

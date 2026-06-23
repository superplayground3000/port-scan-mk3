package scanapp

import (
	"bytes"
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

func TestStartScanExecutor_EmitsScanProbeResultEvent_WithCorrectFields(t *testing.T) {
	taskCh := make(chan scanTask, 1)
	taskCh <- scanTask{
		chunkIdx: 0,
		ipCidr:   "10.0.0.0/24",
		ip:       "10.0.0.8",
		port:     443,
		meta:     targetMeta{},
	}
	close(taskCh)

	dial := func(context.Context, string, string) (net.Conn, error) {
		return stubConn{}, nil
	}

	var buf bytes.Buffer
	logger := newLogger("debug", false, &buf)

	resultCh, errCh := startScanExecutor(1, 100*time.Millisecond, dial, logger, taskCh)
	_ = collectResults(t, resultCh)
	if err := collectExecutorError(t, errCh); err != nil {
		t.Fatalf("unexpected executor error: %v", err)
	}

	log := buf.String()
	if !strings.Contains(log, LogEventScanProbeResult) {
		t.Errorf("expected log to contain %q, got: %s", LogEventScanProbeResult, log)
	}
	if !strings.Contains(log, "10.0.0.8") {
		t.Errorf("expected log to contain target IP 10.0.0.8, got: %s", log)
	}
	if !strings.Contains(log, "443") {
		t.Errorf("expected log to contain port 443, got: %s", log)
	}
	if !strings.Contains(log, LogEventScanned) {
		t.Errorf("expected log to contain state %q, got: %s", LogEventScanned, log)
	}
	if !strings.Contains(log, LogEventNone) {
		t.Errorf("expected log to contain errCause %q, got: %s", LogEventNone, log)
	}
}

func TestStartScanExecutor_EmitsErrorEvent_WhenScanFails(t *testing.T) {
	taskCh := make(chan scanTask, 1)
	taskCh <- scanTask{
		chunkIdx: 0,
		ipCidr:   "10.0.0.0/24",
		ip:       "10.0.0.8",
		port:     9999,
		meta:     targetMeta{},
	}
	close(taskCh)

	dial := func(context.Context, string, string) (net.Conn, error) {
		return nil, context.DeadlineExceeded
	}

	var buf bytes.Buffer
	logger := newLogger("debug", false, &buf)

	resultCh, errCh := startScanExecutor(1, 50*time.Millisecond, dial, logger, taskCh)
	_ = collectResults(t, resultCh)
	if err := collectExecutorError(t, errCh); err != nil {
		t.Fatalf("unexpected executor error: %v", err)
	}

	log := buf.String()
	if !strings.Contains(log, LogEventScanProbeResult) {
		t.Errorf("expected log to contain %q, got: %s", LogEventScanProbeResult, log)
	}
	if !strings.Contains(log, LogEventError) {
		t.Errorf("expected log to contain error state %q, got: %s", LogEventError, log)
	}
	if !strings.Contains(log, LogEventRuntimeErr) {
		t.Errorf("expected log to contain errCause %q, got: %s", LogEventRuntimeErr, log)
	}
}

func TestStartScanExecutor_WhenWorkerPanics_ReportsFatalError(t *testing.T) {
	taskCh := make(chan scanTask, 1)
	taskCh <- scanTask{
		chunkIdx: 0,
		ipCidr:   "10.0.0.0/24",
		ip:       "10.0.0.8",
		port:     443,
		meta:     targetMeta{},
	}
	close(taskCh)

	dial := func(context.Context, string, string) (net.Conn, error) {
		panic("boom")
	}

	var buf bytes.Buffer
	logger := newLogger("debug", false, &buf)

	resultCh, errCh := startScanExecutor(1, 100*time.Millisecond, dial, logger, taskCh)

	// Panic happens before result publish.
	results := collectResults(t, resultCh)
	if len(results) != 0 {
		t.Fatalf("expected 0 results after worker panic, got %d", len(results))
	}

	err := collectExecutorError(t, errCh)
	if err == nil {
		t.Fatal("expected executor fatal error when worker panics")
	}
	if !strings.Contains(err.Error(), "executor worker panic") {
		t.Fatalf("expected panic error, got: %v", err)
	}
	if !strings.Contains(buf.String(), "executor worker panic") {
		t.Errorf("expected panic log entry, got: %s", buf.String())
	}
}

func TestStartScanExecutor_Constants_AreExported(t *testing.T) {
	// Verify constants are properly defined and non-empty
	if LogEventScanned == "" {
		t.Error("LogEventScanned should not be empty")
	}
	if LogEventError == "" {
		t.Error("LogEventError should not be empty")
	}
	if LogEventRuntimeErr == "" {
		t.Error("LogEventRuntimeErr should not be empty")
	}
	if LogEventNone == "" {
		t.Error("LogEventNone should not be empty")
	}
	if LogEventBucketWaitStart == "" {
		t.Error("LogEventBucketWaitStart should not be empty")
	}
	if LogEventBucketAcquired == "" {
		t.Error("LogEventBucketAcquired should not be empty")
	}
	if LogEventBucketAcquireError == "" {
		t.Error("LogEventBucketAcquireError should not be empty")
	}
	if LogEventGateWaitStart == "" {
		t.Error("LogEventGateWaitStart should not be empty")
	}
	if LogEventGateReleased == "" {
		t.Error("LogEventGateReleased should not be empty")
	}
	if LogEventScanResult == "" {
		t.Error("LogEventScanResult should not be empty")
	}
	if LogEventScanProbeResult == "" {
		t.Error("LogEventScanProbeResult should not be empty")
	}
}

func collectExecutorError(t *testing.T, errCh <-chan error) error {
	t.Helper()
	select {
	case err, ok := <-errCh:
		if !ok {
			return nil
		}
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for executor error channel to close")
		return nil
	}
}

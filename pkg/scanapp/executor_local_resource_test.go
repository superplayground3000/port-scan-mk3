package scanapp

import (
	"bytes"
	"context"
	"net"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

// dialErrnoFailure reproduces the error shape the stdlib net package returns
// from a failed DialContext on every platform: *net.OpError wrapping
// *os.SyscallError wrapping a syscall.Errno. On Windows the same shape carries
// the Winsock codes (WSAECONNREFUSED, WSAENOBUFS, WSAEADDRNOTAVAIL, WSAEACCES).
// Tests use it instead of errors.New so the scanner classifies them the way it
// classifies a real dial failure.
func dialErrnoFailure(errno syscall.Errno) error {
	return &net.OpError{
		Op:  "dial",
		Net: "tcp",
		Err: os.NewSyscallError("connect", errno),
	}
}

func localResourceTaskCh() chan scanTask {
	taskCh := make(chan scanTask, 1)
	taskCh <- scanTask{
		chunkIdx: 0,
		ipCidr:   "10.0.0.0/24",
		ip:       "10.0.0.8",
		port:     443,
		meta:     targetMeta{executionKey: "10.0.0.8:443/tcp"},
	}
	close(taskCh)
	return taskCh
}

// A local resource failure says nothing about the remote port, so the row the
// executor emits must not claim the target was confirmed closed.
func TestStartScanExecutor_WhenLocalResourceFailure_DoesNotRecordCloseStatus(t *testing.T) {
	dial := func(context.Context, string, string) (net.Conn, error) {
		return nil, dialErrnoFailure(syscall.ENOBUFS)
	}

	resultCh, errCh := startScanExecutor(1, 100*time.Millisecond, dial, newLogger("debug", false, &bytes.Buffer{}), localResourceTaskCh())
	results := collectResults(t, resultCh)
	if err := collectExecutorError(t, errCh); err != nil {
		t.Fatalf("a local resource failure is non-fatal for the run, got executor error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	got := results[0].record.Status
	if got == "close" || got == "close(timeout)" {
		t.Fatalf("local resource failure recorded as a confirmed closed port: status %q", got)
	}
	if got != "error(local)" {
		t.Fatalf("expected status error(local), got %q", got)
	}
}

// The failure must be visible in the structured log, not only in the CSV.
func TestStartScanExecutor_WhenLocalResourceFailure_SurfacesItInTheLog(t *testing.T) {
	dial := func(context.Context, string, string) (net.Conn, error) {
		return nil, dialErrnoFailure(syscall.EACCES)
	}

	var buf bytes.Buffer
	resultCh, errCh := startScanExecutor(1, 100*time.Millisecond, dial, newLogger("debug", false, &buf), localResourceTaskCh())
	_ = collectResults(t, resultCh)
	if err := collectExecutorError(t, errCh); err != nil {
		t.Fatalf("unexpected executor error: %v", err)
	}

	log := buf.String()
	if !strings.Contains(log, "local_resource") {
		t.Fatalf("expected the log to name the local_resource outcome, got: %s", log)
	}
	if !strings.Contains(log, LogEventError) {
		t.Fatalf("expected the log to mark the probe as an error, got: %s", log)
	}
}

// Connection refused is a confirmed refusal and must keep producing "close".
func TestStartScanExecutor_WhenConnectionRefused_StillRecordsClose(t *testing.T) {
	dial := func(context.Context, string, string) (net.Conn, error) {
		return nil, dialErrnoFailure(syscall.ECONNREFUSED)
	}

	resultCh, errCh := startScanExecutor(1, 100*time.Millisecond, dial, newLogger("debug", false, &bytes.Buffer{}), localResourceTaskCh())
	results := collectResults(t, resultCh)
	if err := collectExecutorError(t, errCh); err != nil {
		t.Fatalf("unexpected executor error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if got := results[0].record.Status; got != "close" {
		t.Fatalf("expected status close for a refused connection, got %q", got)
	}
}

package scanapp

import (
	"context"
	"io"
	"net"
	"testing"
	"time"
)

func TestStartScanExecutor_WhenTasksComplete_EmitsResultsAndClosesChannel(t *testing.T) {
	taskCh := make(chan scanTask, 2)
	taskCh <- scanTask{
		chunkIdx: 0,
		ipCidr:   "10.0.0.0/24",
		ip:       "10.0.0.8",
		port:     443,
		meta: targetMeta{
			fabName:           "fab-1",
			cidrName:          "web",
			serviceLabel:      "https",
			decision:          "accept",
			policyID:          "P-1",
			reason:            "approved",
			executionKey:      "10.0.0.8:443/tcp",
			srcIP:             "192.168.1.10",
			srcNetworkSegment: "192.168.1.0/24",
		},
	}
	close(taskCh)

	dial := func(context.Context, string, string) (net.Conn, error) {
		return stubConn{}, nil
	}

	resultCh, errCh := startScanExecutor(1, 100*time.Millisecond, dial, newLogger("debug", false, io.Discard), taskCh)
	results := collectResults(t, resultCh)
	if err := collectExecutorError(t, errCh); err != nil {
		t.Fatalf("unexpected executor error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].chunkIdx != 0 || results[0].record.Status() != "open" || results[0].record.ExecutionKey() != "10.0.0.8:443/tcp" {
		t.Fatalf("unexpected result: %+v", results[0])
	}
}

func TestStartScanExecutor_WhenTaskChannelClosedImmediately_ClosesWithoutResults(t *testing.T) {
	taskCh := make(chan scanTask)
	close(taskCh)

	resultCh, errCh := startScanExecutor(2, 100*time.Millisecond, func(context.Context, string, string) (net.Conn, error) {
		return stubConn{}, nil
	}, newLogger("debug", false, io.Discard), taskCh)
	results := collectResults(t, resultCh)
	if err := collectExecutorError(t, errCh); err != nil {
		t.Fatalf("unexpected executor error: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected no results, got %d", len(results))
	}
}

func collectResults(t *testing.T, resultCh <-chan scanResult) []scanResult {
	t.Helper()
	var out []scanResult
	timeout := time.After(2 * time.Second)
	for {
		select {
		case res, ok := <-resultCh:
			if !ok {
				return out
			}
			out = append(out, res)
		case <-timeout:
			t.Fatal("timed out waiting for executor results to close")
		}
	}
}

type stubConn struct{}

func (stubConn) Read(_ []byte) (int, error)         { return 0, nil }
func (stubConn) Write(b []byte) (int, error)        { return len(b), nil }
func (stubConn) Close() error                       { return nil }
func (stubConn) LocalAddr() net.Addr                { return &net.TCPAddr{} }
func (stubConn) RemoteAddr() net.Addr               { return &net.TCPAddr{} }
func (stubConn) SetDeadline(time.Time) error        { return nil }
func (stubConn) SetReadDeadline(time.Time) error    { return nil }
func (stubConn) SetWriteDeadline(time.Time) error   { return nil }

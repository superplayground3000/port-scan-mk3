package scanner

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return false }

func TestScanTCP_WhenDialTimeout_ReturnsCloseTimeoutStatus(t *testing.T) {
	dial := func(context.Context, string, string) (net.Conn, error) {
		return nil, timeoutErr{}
	}
	res := ScanTCP(dial, "127.0.0.1", 80, 10*time.Millisecond)
	if res.Status != "close(timeout)" {
		t.Fatalf("expected close(timeout), got %s", res.Status)
	}
}

// dialErr builds the error shape the stdlib net package actually returns from a
// failed DialContext: *net.OpError wrapping *os.SyscallError wrapping a
// syscall.Errno. Tests classify on that structure, never on message text.
func dialErr(errno syscall.Errno) error {
	return &net.OpError{
		Op:   "dial",
		Net:  "tcp",
		Addr: &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 80},
		Err:  os.NewSyscallError("connect", errno),
	}
}

func TestScanTCP_WhenConnectionRefused_ReturnsCloseStatus(t *testing.T) {
	dial := func(context.Context, string, string) (net.Conn, error) {
		return nil, dialErr(syscall.ECONNREFUSED)
	}
	res := ScanTCP(dial, "127.0.0.1", 80, 10*time.Millisecond)
	if res.Status != "close" {
		t.Fatalf("expected close, got %s", res.Status)
	}
}

// A local resource failure (here: ENOBUFS / WSAENOBUFS-class buffer
// exhaustion on the scanning host) says nothing about the remote port. It must
// never be recorded as a confirmed closed port.
func TestScanTCP_WhenLocalResourceFailure_DoesNotReportClose(t *testing.T) {
	dial := func(context.Context, string, string) (net.Conn, error) {
		return nil, dialErr(syscall.ENOBUFS)
	}
	res := ScanTCP(dial, "127.0.0.1", 80, 10*time.Millisecond)
	if res.Status == "close" || res.Status == "close(timeout)" {
		t.Fatalf("local resource failure must not be reported as closed, got status %q", res.Status)
	}
	if res.Status != "error(local)" {
		t.Fatalf("expected status error(local), got %q", res.Status)
	}
}

func TestScanTCP_WhenLocalPermissionFailure_DoesNotReportClose(t *testing.T) {
	dial := func(context.Context, string, string) (net.Conn, error) {
		return nil, dialErr(syscall.EACCES)
	}
	res := ScanTCP(dial, "127.0.0.1", 80, 10*time.Millisecond)
	if res.Status != "error(local)" {
		t.Fatalf("expected status error(local), got %q", res.Status)
	}
}

// An error that carries no recognizable errno is indeterminate: the port state
// is simply unknown, which is not the same claim as "closed".
func TestScanTCP_WhenUnclassifiableError_ReportsUnknown(t *testing.T) {
	dial := func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("connection refused")
	}
	res := ScanTCP(dial, "127.0.0.1", 80, 10*time.Millisecond)
	if res.Status != "unknown" {
		t.Fatalf("expected status unknown for an unclassifiable error, got %q", res.Status)
	}
}

func TestScanTCP_WhenContextDeadlineExceeded_ReturnsCloseTimeoutStatus(t *testing.T) {
	dial := func(context.Context, string, string) (net.Conn, error) {
		return nil, context.DeadlineExceeded
	}
	res := ScanTCP(dial, "127.0.0.1", 80, 10*time.Millisecond)
	if res.Status != "close(timeout)" {
		t.Fatalf("expected close(timeout), got %s", res.Status)
	}
}

type closeErrorConn struct{}

func (closeErrorConn) Read([]byte) (int, error)         { return 0, io.EOF }
func (closeErrorConn) Write([]byte) (int, error)        { return 0, io.EOF }
func (closeErrorConn) Close() error                     { return errors.New("close boom") }
func (closeErrorConn) LocalAddr() net.Addr              { return &net.IPAddr{IP: net.IPv4(127, 0, 0, 1)} }
func (closeErrorConn) RemoteAddr() net.Addr             { return &net.IPAddr{IP: net.IPv4(127, 0, 0, 2)} }
func (closeErrorConn) SetDeadline(time.Time) error      { return nil }
func (closeErrorConn) SetReadDeadline(time.Time) error  { return nil }
func (closeErrorConn) SetWriteDeadline(time.Time) error { return nil }

func TestScanTCP_WhenConnCloseFails_PreservesOpenAndReturnsCloseError(t *testing.T) {
	dial := func(context.Context, string, string) (net.Conn, error) {
		return closeErrorConn{}, nil
	}
	res := ScanTCP(dial, "127.0.0.1", 80, 10*time.Millisecond)
	if res.Status != "open" {
		t.Fatalf("expected open status, got %s", res.Status)
	}
	if !strings.Contains(res.Error, "close failed") {
		t.Fatalf("expected close failure error, got %q", res.Error)
	}
}

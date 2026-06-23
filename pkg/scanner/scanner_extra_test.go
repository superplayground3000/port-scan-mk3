package scanner

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
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

func TestScanTCP_WhenConnectionRefused_ReturnsCloseStatus(t *testing.T) {
	dial := func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("connection refused")
	}
	res := ScanTCP(dial, "127.0.0.1", 80, 10*time.Millisecond)
	if res.Status != "close" {
		t.Fatalf("expected close, got %s", res.Status)
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

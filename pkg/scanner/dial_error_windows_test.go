//go:build windows

package scanner

import (
	"context"
	"net"
	"runtime"
	"syscall"
	"testing"
	"time"
)

// This file runs ONLY on the windows-latest CI runner (the go:build windows
// constraint excludes it everywhere else). It is the native counterpart to
// dial_error_test.go, which reaches the Windows classification table by passing
// the literal string "windows" together with synthetic syscall.Errno values.
// Those simulated tests prove the table's logic on any build host, but they
// cannot prove two things only the real platform can answer:
//  1. that a real Windows dial failure actually arrives in the
//     *net.OpError -> *os.SyscallError -> syscall.Errno shape that dialErrno's
//     fast path assumes; and
//  2. that the real runtime.GOOS drives the Windows branch and the hardcoded
//     Winsock numbers equal what Windows itself emits.
// The tests below exercise errors Windows produces, on Windows.

// TestWinsockTable_MatchesRealWindowsConstants pins the hardcoded Winsock codes
// in dial_error.go to the values Go's own syscall package reports on Windows,
// for the two WSA* constants Go actually exposes there (WSAEACCES and
// WSAECONNRESET; the rest are matched by number because Go declares no name).
// It then drives the real platform constants through the real runtime.GOOS so
// the classification is the genuine Windows path, not a simulated one: WSAEACCES
// is a local-host failure that must never be reported as a confirmed closed
// port, and WSAECONNRESET is the remote answering, which is a confirmed close.
func TestWinsockTable_MatchesRealWindowsConstants(t *testing.T) {
	if wsaeACCES != syscall.WSAEACCES {
		t.Fatalf("wsaeACCES table value %d != real syscall.WSAEACCES %d", uintptr(wsaeACCES), uintptr(syscall.WSAEACCES))
	}
	if wsaeCONNRESET != syscall.WSAECONNRESET {
		t.Fatalf("wsaeCONNRESET table value %d != real syscall.WSAECONNRESET %d", uintptr(wsaeCONNRESET), uintptr(syscall.WSAECONNRESET))
	}
	if got := classifyDialError(runtime.GOOS, opErr(syscall.WSAEACCES)); got != OutcomeLocalResource {
		t.Fatalf("real WSAEACCES on runtime.GOOS=%q classified %q, want %q", runtime.GOOS, got, OutcomeLocalResource)
	}
	if got := classifyDialError(runtime.GOOS, opErr(syscall.WSAECONNRESET)); got != OutcomeRefused {
		t.Fatalf("real WSAECONNRESET on runtime.GOOS=%q classified %q, want %q", runtime.GOOS, got, OutcomeRefused)
	}
}

// TestScanTCP_RealWindowsRefusedDial performs an actual dial against a closed
// loopback port and asserts the whole chain on the real OS: Windows produces a
// genuine Winsock refusal, dialErrno extracts its errno through the stdlib
// nesting, classifyDialError(runtime.GOOS, ...) calls it a refusal, and ScanTCP
// reports StatusClose. This is the exact runtime error shape the Linux-only
// tests can only assume. A just-closed loopback port is the standard isolated
// way to force a refusal without a real host (constitution V).
func TestScanTCP_RealWindowsRefusedDial(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().(*net.TCPAddr)
	if err := ln.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}

	conn, dialErr := (&net.Dialer{}).DialContext(context.Background(), "tcp", addr.String())
	if dialErr == nil {
		_ = conn.Close()
		t.Fatal("dial to a closed loopback port unexpectedly succeeded; cannot observe a refusal")
	}

	// The refusal must reach us as a real syscall.Errno through dialErrno's
	// fast-path type assertions — the shape the Linux tests presuppose.
	errno, ok := dialErrno(dialErr)
	if !ok {
		t.Fatalf("dialErrno could not extract an errno from a real Windows dial error: %#v", dialErr)
	}
	if errno != wsaeCONNREFUSED && errno != wsaeCONNRESET {
		t.Fatalf("real closed-port dial produced errno %d, want WSAECONNREFUSED=%d or WSAECONNRESET=%d (%#v)",
			uintptr(errno), uintptr(wsaeCONNREFUSED), uintptr(wsaeCONNRESET), dialErr)
	}
	if got := classifyDialError(runtime.GOOS, dialErr); got != OutcomeRefused {
		t.Fatalf("real refused dial classified %q, want %q (errno %d)", got, OutcomeRefused, uintptr(errno))
	}

	res := ScanTCP(func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}, addr.IP.String(), addr.Port, time.Second)
	if res.Status != StatusClose || res.Outcome != OutcomeRefused {
		t.Fatalf("ScanTCP on a real refused Windows dial: Status=%q Outcome=%q, want Status=%q Outcome=%q",
			res.Status, res.Outcome, StatusClose, OutcomeRefused)
	}
}

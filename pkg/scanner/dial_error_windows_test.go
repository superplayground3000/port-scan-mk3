//go:build windows

package scanner

import (
	"context"
	"net"
	"os"
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
//
// Both the refusal path and the LOCAL-RESOURCE path are exercised with errors
// Windows actually produces. The local-resource path matters most: issue #62
// exists because a local Winsock failure used to be written as a confirmed
// closed port, and a synthetic errno cannot prove the real one arrives in a
// shape the classifier recognises.

// assertNativeErrnoShape asserts, with explicit type assertions, that err has
// the exact *net.OpError -> *os.SyscallError -> syscall.Errno nesting that
// dialErrno's fast path assumes, and returns the errno.
//
// This deliberately does NOT rely on dialErrno to establish the shape: that
// helper falls back to errors.As, so a test written on top of it still passes
// if Windows stops producing this nesting and only the slow path works.
// Asserting the shape directly is the only way to detect that regression;
// dialErrno is then cross-checked for agreement.
func assertNativeErrnoShape(t *testing.T, err error) syscall.Errno {
	t.Helper()

	opError, ok := err.(*net.OpError)
	if !ok {
		t.Fatalf("real Windows dial error is not *net.OpError, so dialErrno's fast path no longer matches reality: %T (%#v)", err, err)
	}
	sysErr, ok := opError.Err.(*os.SyscallError)
	if !ok {
		t.Fatalf("(*net.OpError).Err is not *os.SyscallError: %T (%#v)", opError.Err, opError.Err)
	}
	errno, ok := sysErr.Err.(syscall.Errno)
	if !ok {
		t.Fatalf("(*os.SyscallError).Err is not syscall.Errno: %T (%#v)", sysErr.Err, sysErr.Err)
	}

	viaHelper, ok := dialErrno(err)
	if !ok {
		t.Fatalf("dialErrno failed to extract an errno that the explicit shape assertions found (%d)", uintptr(errno))
	}
	if viaHelper != errno {
		t.Fatalf("dialErrno returned errno %d but the real nesting holds %d", uintptr(viaHelper), uintptr(errno))
	}
	return errno
}

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
// genuine Winsock refusal in the shape dialErrno assumes, classifyDialError
// calls it a refusal, and ScanTCP reports StatusClose.
//
// Freeing an ephemeral port and immediately redialling it races the OS: another
// process on the runner can bind the released port first, which would make the
// dial succeed or fail with an unrelated errno even though the classifier is
// correct. That is a flaky test, not a real defect, so the loop below DISCARDS
// any attempt whose port was reused and retries with a fresh one, failing
// loudly only if no attempt within the budget produced a clean refusal.
func TestScanTCP_RealWindowsRefusedDial(t *testing.T) {
	const attempts = 8

	var (
		addr     *net.TCPAddr
		dialErr  error
		errno    syscall.Errno
		discards int
	)

	for i := 0; i < attempts; i++ {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
		candidate := ln.Addr().(*net.TCPAddr)
		if err := ln.Close(); err != nil {
			t.Fatalf("close listener: %v", err)
		}

		conn, err := (&net.Dialer{}).DialContext(context.Background(), "tcp", candidate.String())
		if err == nil {
			// Another process bound the port between Close and Dial.
			_ = conn.Close()
			discards++
			continue
		}

		got, ok := dialErrno(err)
		if !ok || (got != wsaeCONNREFUSED && got != wsaeCONNRESET) {
			// Not a clean refusal, most likely because the port was rebound by
			// a process that then failed us differently. Try another port.
			discards++
			continue
		}

		addr, dialErr, errno = candidate, err, got
		break
	}

	if dialErr == nil {
		t.Fatalf("no clean refusal in %d attempts (%d ports were reused or produced an unrelated error); cannot observe a Windows refusal on this runner", attempts, discards)
	}

	// The refusal must arrive in the exact nesting dialErrno's fast path assumes.
	if shape := assertNativeErrnoShape(t, dialErr); shape != errno {
		t.Fatalf("shape assertion produced errno %d, dialErrno produced %d", uintptr(shape), uintptr(errno))
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

// TestScanTCP_RealWindowsLocalResourceFailure is the test issue #62 actually
// needs: a REAL Windows local-resource dial failure, not a synthetic errno.
//
// Binding the local end of the socket to an address this host does not own
// makes Winsock fail the bind with WSAEADDRNOTAVAIL (10049) before any packet
// leaves the machine. That is deterministic, needs no network, and touches no
// host other than the scanning one (constitution V) — 203.0.113.0/24 is
// TEST-NET-3, reserved for documentation and never routable.
//
// The whole point of #62 is that such a failure must NOT be recorded as a
// confirmed closed port, so this asserts the full chain through to the
// caller-visible status, not just the classifier.
func TestScanTCP_RealWindowsLocalResourceFailure(t *testing.T) {
	unassigned := &net.TCPAddr{IP: net.IPv4(203, 0, 113, 1)}
	dialer := &net.Dialer{LocalAddr: unassigned}

	// The destination is irrelevant: the local bind fails first. Use a closed
	// loopback port anyway so nothing is ever sent even if a future Windows
	// build changed the ordering.
	conn, dialErr := dialer.DialContext(context.Background(), "tcp", "127.0.0.1:9")
	if dialErr == nil {
		_ = conn.Close()
		t.Fatal("dialing from an unassigned local address unexpectedly succeeded; cannot observe a local-resource failure")
	}

	errno := assertNativeErrnoShape(t, dialErr)

	// WSAEADDRNOTAVAIL is what Windows returns for a bind to an address the host
	// does not hold. Accept the other genuinely-local codes rather than pinning
	// one number, but reject anything the table would call a refusal — that is
	// precisely the bug this test exists to catch.
	switch errno {
	case wsaeADDRNOTAVAIL, wsaeACCES, wsaeADDRINUSE:
		// expected: a local-host failure
	default:
		t.Fatalf("bind to an unassigned local address produced errno %d, want a local-resource code such as WSAEADDRNOTAVAIL=%d (%#v)",
			uintptr(errno), uintptr(wsaeADDRNOTAVAIL), dialErr)
	}

	if got := classifyDialError(runtime.GOOS, dialErr); got != OutcomeLocalResource {
		t.Fatalf("real Windows local-resource dial failure (errno %d) classified %q, want %q — a local failure must never be reported as a confirmed closed port",
			uintptr(errno), got, OutcomeLocalResource)
	}

	res := ScanTCP(func(ctx context.Context, network, address string) (net.Conn, error) {
		return dialer.DialContext(ctx, network, address)
	}, "127.0.0.1", 9, time.Second)
	if res.Status != StatusLocalError || res.Outcome != OutcomeLocalResource {
		t.Fatalf("ScanTCP on a real Windows local-resource failure: Status=%q Outcome=%q, want Status=%q Outcome=%q — issue #62 is that this used to be reported as %q",
			res.Status, res.Outcome, StatusLocalError, OutcomeLocalResource, StatusClose)
	}
}

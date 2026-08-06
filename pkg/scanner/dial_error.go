package scanner

import (
	"context"
	"errors"
	"net"
	"os"
	"syscall"
)

// Outcome is the explicit classification of one TCP dial attempt.
//
// A port scanner can claim "this port is closed" only when the *remote* end
// said so. Each other condition is a different statement: a timeout, a failure
// of the local machine, or an unrecognized transport error. Each of these
// conditions gets its own outcome, so no condition can be mistaken for a
// confirmed closed port.
type Outcome string

const (
	// OutcomeOpen: the TCP handshake completed.
	OutcomeOpen Outcome = "open"
	// OutcomeRefused: the remote end actively refused or reset the connection
	// (ECONNREFUSED / WSAECONNREFUSED, ECONNRESET / WSAECONNRESET). This is the
	// only outcome that means "confirmed closed".
	OutcomeRefused Outcome = "refused"
	// OutcomeTimeout: no answer within the scan timeout (net.Error.Timeout,
	// context deadline, ETIMEDOUT / WSAETIMEDOUT).
	OutcomeTimeout Outcome = "timeout"
	// OutcomeLocalResource: the dial failed on the *scanning host* — address or
	// buffer exhaustion, descriptor/handle limits, or a permission denial. The
	// packet never characterized the target, so the port state is unknown.
	OutcomeLocalResource Outcome = "local_resource"
	// OutcomeIndeterminate: any other transport/infrastructure error (host or
	// network unreachable, an error carrying no recognizable errno). The port
	// state is unknown.
	OutcomeIndeterminate Outcome = "indeterminate"
)

// Status strings that the scan writes to the scan CSV. They are part of the
// output contract (see docs/specs/SPEC-05-SCANNER-SYSTEM.md). Downstream tools
// filter on StatusOpen and StatusClose. The two non-probing outcomes therefore
// use different values on purpose, and no "is it closed?" filter matches them.
const (
	StatusOpen         = "open"
	StatusClose        = "close"
	StatusCloseTimeout = "close(timeout)"
	StatusLocalError   = "error(local)"
	StatusUnknown      = "unknown"
)

// Winsock error codes. Go's syscall package on Windows exposes only a couple of
// WSA* names, and its Unix-style E* constants there are synthetic values above
// syscall.APPLICATION_ERROR (1<<29) that never equal a Winsock code — so a
// Windows dial failure can only be matched by its numeric Winsock value. The
// values are the stable WSAGetLastError codes; declaring them here (rather than
// in a //go:build windows file) keeps one classification table that Linux CI can
// unit-test.
const (
	wsaeACCES        = syscall.Errno(10013) // WSAEACCES: permission denied
	wsaeMFILE        = syscall.Errno(10024) // WSAEMFILE: no more socket handles
	wsaeADDRINUSE    = syscall.Errno(10048) // WSAEADDRINUSE: local address in use
	wsaeADDRNOTAVAIL = syscall.Errno(10049) // WSAEADDRNOTAVAIL: no ephemeral port available
	wsaeCONNRESET    = syscall.Errno(10054) // WSAECONNRESET: remote reset the connection
	wsaeNOBUFS       = syscall.Errno(10055) // WSAENOBUFS: no buffer space available
	wsaeTIMEDOUT     = syscall.Errno(10060) // WSAETIMEDOUT: connection timed out
	wsaeCONNREFUSED  = syscall.Errno(10061) // WSAECONNREFUSED: remote refused the connection
	wsaePROCLIM      = syscall.Errno(10067) // WSAEPROCLIM: too many Winsock processes
)

// classifyDialError maps a dial error to its Outcome.
//
// goos is passed in (rather than read from runtime.GOOS) so the Windows table
// is testable from any build platform — the same seam used by
// pkg/scanapp.pingProcessTimeout. Classification is structural only: net.Error,
// context sentinels and errors.As on syscall.Errno. Error *text* is never
// inspected, because it is localized on Windows.
func classifyDialError(goos string, err error) Outcome {
	if err == nil {
		return OutcomeOpen
	}

	// The errno lookup comes first because it is the common case on the dial hot
	// path and because it answers timeouts too (ETIMEDOUT / WSAETIMEDOUT). Only
	// errors it does not recognize pay for the remaining checks.
	if errno, ok := dialErrno(err); ok {
		if outcome, matched := classifyErrno(goos, errno); matched {
			return outcome
		}
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return OutcomeTimeout
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return OutcomeTimeout
	}
	return OutcomeIndeterminate
}

// dialErrno extracts the syscall.Errno carried by a dial error.
//
// It first walks the exact shape the stdlib produces on both platforms —
// *net.OpError -> *os.SyscallError -> syscall.Errno — with plain type
// assertions, and only then falls back to errors.As for any other wrapping.
// The fast path exists for cost, not for correctness: errors.As forces its
// target to escape to the heap, and this runs once per scanned target on the
// dial hot path (see BenchmarkScanTCPDialFailure). The errors.As fallback keeps
// every other wrapping (fmt.Errorf("%w"), custom dialers) classified correctly.
func dialErrno(err error) (syscall.Errno, bool) {
	if opErr, ok := err.(*net.OpError); ok {
		inner := opErr.Err
		if sysErr, ok := inner.(*os.SyscallError); ok {
			inner = sysErr.Err
		}
		if errno, ok := inner.(syscall.Errno); ok {
			return errno, true
		}
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return errno, true
	}
	return 0, false
}

// classifyErrno reports the Outcome for a raw errno, and whether it recognized
// it at all. On Windows the Winsock table is consulted first; the Unix-style
// table below it is still consulted because the synthetic Windows E* values
// cannot collide with Winsock codes.
func classifyErrno(goos string, errno syscall.Errno) (Outcome, bool) {
	if goos == "windows" {
		switch errno {
		case wsaeACCES, wsaeMFILE, wsaeADDRINUSE, wsaeADDRNOTAVAIL, wsaeNOBUFS, wsaePROCLIM:
			return OutcomeLocalResource, true
		case wsaeCONNREFUSED, wsaeCONNRESET:
			return OutcomeRefused, true
		case wsaeTIMEDOUT:
			return OutcomeTimeout, true
		}
	}

	switch errno {
	case syscall.EACCES, syscall.EPERM, syscall.EADDRNOTAVAIL, syscall.EADDRINUSE,
		syscall.ENOBUFS, syscall.EMFILE, syscall.ENFILE, syscall.ENOMEM:
		return OutcomeLocalResource, true
	case syscall.ECONNREFUSED, syscall.ECONNRESET:
		return OutcomeRefused, true
	case syscall.ETIMEDOUT:
		return OutcomeTimeout, true
	}
	return OutcomeIndeterminate, false
}

// statusForOutcome maps an Outcome to the status string written to the CSV.
func statusForOutcome(outcome Outcome) string {
	switch outcome {
	case OutcomeOpen:
		return StatusOpen
	case OutcomeRefused:
		return StatusClose
	case OutcomeTimeout:
		return StatusCloseTimeout
	case OutcomeLocalResource:
		return StatusLocalError
	default:
		return StatusUnknown
	}
}

package scanner

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
	"testing"
)

// opErr wraps an errno the way the stdlib net package does on every platform:
// *net.OpError -> *os.SyscallError -> syscall.Errno. Classification must work
// through that nesting via errors.As, never by reading the message.
func opErr(errno syscall.Errno) error {
	return &net.OpError{
		Op:   "dial",
		Net:  "tcp",
		Addr: &net.TCPAddr{IP: net.IPv4(10, 1, 2, 3), Port: 445},
		Err:  os.NewSyscallError("connectex", errno),
	}
}

func TestClassifyDialError_WindowsWinsockCodes(t *testing.T) {
	cases := []struct {
		name  string
		errno syscall.Errno
		want  Outcome
	}{
		// The three failures named in issue #62.
		{"WSAEADDRNOTAVAIL", syscall.Errno(10049), OutcomeLocalResource},
		{"WSAENOBUFS", syscall.Errno(10055), OutcomeLocalResource},
		{"WSAEACCES", syscall.Errno(10013), OutcomeLocalResource},
		// Neighbouring local-exhaustion codes from the same family.
		{"WSAEMFILE", syscall.Errno(10024), OutcomeLocalResource},
		{"WSAEADDRINUSE", syscall.Errno(10048), OutcomeLocalResource},
		{"WSAEPROCLIM", syscall.Errno(10067), OutcomeLocalResource},
		// Remote answers: still a confirmed closed port.
		{"WSAECONNREFUSED", syscall.Errno(10061), OutcomeRefused},
		{"WSAECONNRESET", syscall.Errno(10054), OutcomeRefused},
		{"WSAETIMEDOUT", syscall.Errno(10060), OutcomeTimeout},
		// Remote/network conditions that characterize nothing about the port.
		{"WSAEHOSTUNREACH", syscall.Errno(10065), OutcomeIndeterminate},
		{"WSAENETUNREACH", syscall.Errno(10051), OutcomeIndeterminate},
		{"WSAENETDOWN", syscall.Errno(10050), OutcomeIndeterminate},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyDialError("windows", opErr(tc.errno)); got != tc.want {
				t.Fatalf("classifyDialError(windows, %s=%d) = %q, want %q", tc.name, uintptr(tc.errno), got, tc.want)
			}
		})
	}
}

// A Winsock code must not be honoured on a non-Windows host: there errno 10055
// is not a syscall the kernel produces, so the honest answer is indeterminate,
// never "closed".
func TestClassifyDialError_WinsockCodesAreNotHonouredOnUnix(t *testing.T) {
	for _, errno := range []syscall.Errno{10049, 10055, 10013, 10061} {
		if got := classifyDialError("linux", opErr(errno)); got == OutcomeRefused {
			t.Fatalf("errno %d on linux must not classify as refused, got %q", uintptr(errno), got)
		}
	}
}

func TestClassifyDialError_PortableErrnos(t *testing.T) {
	cases := []struct {
		name  string
		errno syscall.Errno
		want  Outcome
	}{
		{"EADDRNOTAVAIL", syscall.EADDRNOTAVAIL, OutcomeLocalResource},
		{"EADDRINUSE", syscall.EADDRINUSE, OutcomeLocalResource},
		{"ENOBUFS", syscall.ENOBUFS, OutcomeLocalResource},
		{"EACCES", syscall.EACCES, OutcomeLocalResource},
		{"EPERM", syscall.EPERM, OutcomeLocalResource},
		{"EMFILE", syscall.EMFILE, OutcomeLocalResource},
		{"ENFILE", syscall.ENFILE, OutcomeLocalResource},
		{"ENOMEM", syscall.ENOMEM, OutcomeLocalResource},
		{"ECONNREFUSED", syscall.ECONNREFUSED, OutcomeRefused},
		{"ECONNRESET", syscall.ECONNRESET, OutcomeRefused},
		{"EHOSTUNREACH", syscall.EHOSTUNREACH, OutcomeIndeterminate},
		{"ENETUNREACH", syscall.ENETUNREACH, OutcomeIndeterminate},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// The current platform's own table, exercised through both goos
			// branches so the Windows branch never shadows the portable one.
			for _, goos := range []string{"linux", "windows"} {
				if got := classifyDialError(goos, opErr(tc.errno)); got != tc.want {
					t.Fatalf("classifyDialError(%s, %s) = %q, want %q", goos, tc.name, got, tc.want)
				}
			}
		})
	}
}

func TestClassifyDialError_TimeoutsAndSentinels(t *testing.T) {
	if got := classifyDialError("linux", nil); got != OutcomeOpen {
		t.Fatalf("nil error = %q, want %q", got, OutcomeOpen)
	}
	if got := classifyDialError("linux", timeoutErr{}); got != OutcomeTimeout {
		t.Fatalf("net.Error timeout = %q, want %q", got, OutcomeTimeout)
	}
	if got := classifyDialError("linux", fmt.Errorf("dial: %w", context.DeadlineExceeded)); got != OutcomeTimeout {
		t.Fatalf("wrapped context.DeadlineExceeded = %q, want %q", got, OutcomeTimeout)
	}
	// ETIMEDOUT already reports Timeout() through net.Error, but must classify
	// as a timeout even when it arrives bare.
	if got := classifyDialError("linux", syscall.ETIMEDOUT); got != OutcomeTimeout {
		t.Fatalf("bare ETIMEDOUT = %q, want %q", got, OutcomeTimeout)
	}
	// An error carrying no errno at all: indeterminate, never "close". English
	// text that looks like a refusal must not influence the classification.
	if got := classifyDialError("linux", errors.New("connection refused")); got != OutcomeIndeterminate {
		t.Fatalf("text-only error = %q, want %q", got, OutcomeIndeterminate)
	}
}

func TestStatusForOutcome_MapsEveryOutcome(t *testing.T) {
	cases := map[Outcome]string{
		OutcomeOpen:          StatusOpen,
		OutcomeRefused:       StatusClose,
		OutcomeTimeout:       StatusCloseTimeout,
		OutcomeLocalResource: StatusLocalError,
		OutcomeIndeterminate: StatusUnknown,
		Outcome("bogus"):     StatusUnknown,
	}
	for outcome, want := range cases {
		if got := statusForOutcome(outcome); got != want {
			t.Fatalf("statusForOutcome(%q) = %q, want %q", outcome, got, want)
		}
	}
}

func TestScanTCP_PopulatesOutcomeAlongsideStatus(t *testing.T) {
	dial := func(context.Context, string, string) (net.Conn, error) {
		return nil, opErr(syscall.ENOBUFS)
	}
	res := ScanTCP(dial, "127.0.0.1", 80, 0)
	if res.Outcome != OutcomeLocalResource {
		t.Fatalf("expected outcome %q, got %q", OutcomeLocalResource, res.Outcome)
	}
	if res.Status != StatusLocalError {
		t.Fatalf("expected status %q, got %q", StatusLocalError, res.Status)
	}
	if res.Error == "" {
		t.Fatal("expected the underlying error text to be preserved for diagnostics")
	}
}

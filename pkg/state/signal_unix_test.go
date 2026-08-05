//go:build !windows

package state

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"testing"
	"time"
)

// TestInterruptSignals_OnNonWindows_IsExactlyOSInterrupt pins the POSIX list.
// It is deliberately exact rather than a "contains" check: SIGTERM, SIGHUP and
// SIGQUIT all reach a scan in normal operation (systemd stop, `docker stop`, a
// closed terminal), and quietly promoting any of them to a graceful cancel
// would change what a `kill` means for every deployment. That is a separate,
// deliberate decision -- see docs/interrupt-handling.md -- not something to
// drift into.
func TestInterruptSignals_OnNonWindows_IsExactlyOSInterrupt(t *testing.T) {
	got := interruptSignals()

	if len(got) != 1 || got[0] != os.Interrupt {
		t.Fatalf("interruptSignals() = %v on %s, want exactly [interrupt]", got, runtime.GOOS)
	}
}

// TestWithSIGINTCancel_WhenRealSIGINTDelivered_CancelsContext is the POSIX
// counterpart of the Windows Ctrl+Break subprocess test: it proves the
// SUBSCRIPTION works, not merely that the returned CancelFunc works. A test
// that only calls cancel() (state_extra_test.go) passes even if the signal list
// is empty, so it cannot catch a regression in what WithSIGINTCancel subscribes
// to.
//
// The signal really is delivered to this process. Windows has no kill(2) and no
// SIGINT to self, which is why the Windows side has to spawn the real EXE in
// its own process group and drive it with GenerateConsoleCtrlEvent instead.
func TestWithSIGINTCancel_WhenRealSIGINTDelivered_CancelsContext(t *testing.T) {
	// A deliberately un-Stopped subscription. It guarantees SIGINT never falls
	// back to its default action (terminate) inside this test binary, even if
	// the signal is delivered after the assertion below has already given up.
	// Losing the default action for the rest of the package's tests is
	// harmless; killing the test binary mid-run would not be.
	swallow := make(chan os.Signal, 1)
	signal.Notify(swallow, os.Interrupt)

	ctx, cancel := WithSIGINTCancel(context.Background())
	defer cancel()

	if err := syscall.Kill(syscall.Getpid(), syscall.SIGINT); err != nil {
		t.Fatalf("failed to deliver SIGINT to self: %v", err)
	}

	// Polled with a generous deadline rather than a fixed sleep: signal
	// delivery is asynchronous and CI schedulers are not real-time.
	select {
	case <-ctx.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("context was not canceled within 10s of a real SIGINT; WithSIGINTCancel is not subscribed to it")
	}

	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatalf("ctx.Err() = %v, want context.Canceled", ctx.Err())
	}
}

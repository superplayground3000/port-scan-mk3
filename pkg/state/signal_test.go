package state

import (
	"os"
	"testing"
)

// TestInterruptSignals_AlwaysIncludesOSInterrupt pins the one guarantee that
// holds on every platform: Ctrl+C (os.Interrupt / SIGINT) must always cancel
// the scan context. Platform-specific additions are asserted in the
// build-tagged siblings (signal_windows_test.go, signal_unix_test.go).
func TestInterruptSignals_AlwaysIncludesOSInterrupt(t *testing.T) {
	got := interruptSignals()

	for _, sig := range got {
		if sig == os.Interrupt {
			return
		}
	}
	t.Fatalf("interruptSignals() = %v, which does not include os.Interrupt", got)
}

// TestInterruptSignals_IsNeverEmpty guards a trap in os/signal that is easy to
// walk into while refactoring this list: signal.Notify (and therefore
// signal.NotifyContext) documents that "if no signals are provided, all
// incoming signals will be relayed to c". An empty list is thus NOT a
// no-op subscription -- it subscribes to EVERY signal, so routine traffic like
// SIGCHLD or SIGWINCH would cancel a running scan. A mutation that emptied the
// list was measured to survive the SIGINT delivery test for exactly this
// reason, which is why the invariant is pinned separately here.
func TestInterruptSignals_IsNeverEmpty(t *testing.T) {
	if got := interruptSignals(); len(got) == 0 {
		t.Fatal("interruptSignals() is empty; signal.Notify treats that as 'relay every signal', so any signal would cancel the scan")
	}
}

// TestInterruptSignals_ContainsNoNilSignal guards the platform split itself: a
// per-platform list that returns a nil os.Signal would be handed to
// signal.Notify, which treats an unrecognised signal value as "every signal"
// on some platforms and panics on others. Neither is an acceptable way to
// express "this platform has no extra interrupt signal" -- the non-Windows
// list must simply be shorter.
func TestInterruptSignals_ContainsNoNilSignal(t *testing.T) {
	for i, sig := range interruptSignals() {
		if sig == nil {
			t.Fatalf("interruptSignals()[%d] is nil; an absent platform signal must be omitted, not nil", i)
		}
	}
}

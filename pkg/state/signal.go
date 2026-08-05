package state

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// interruptSignals returns every OS signal that must cancel the scan context.
//
// The list is os.Interrupt and nothing else, and that single entry deliberately
// covers Ctrl+Break on Windows as well as Ctrl+C (issue #68). The Windows
// console raises Ctrl+C and Ctrl+Break as two different control events, but the
// Go runtime folds both into one signal -- runtime.ctrlHandler maps
// CTRL_C_EVENT and CTRL_BREAK_EVENT to SIGINT, which os/signal surfaces as
// os.Interrupt. The standard library pins that mapping in its own test
// (os/signal.TestCtrlBreak fails if a Ctrl+Break arrives as anything other than
// os.Interrupt), so subscribing to os.Interrupt is the correct and complete way
// to handle Ctrl+Break; there is no syscall.SIGBREAK in Go to subscribe to.
// Ctrl+Break is not a curiosity, either: a scan launched in its own process
// group (CREATE_NEW_PROCESS_GROUP, which is how wrapper scripts and job
// schedulers commonly start one) has Ctrl+C disabled by Windows, leaving
// Ctrl+Break as the only console interrupt that can reach it.
//
// Two traps are worth stating for whoever edits this list next.
//
// First, it must never become empty. signal.Notify documents that "if no
// signals are provided, all incoming signals will be relayed", so an empty list
// subscribes to EVERYTHING and ordinary signal traffic would cancel a running
// scan. It must also never contain a nil os.Signal for the same reason.
//
// Second, the remaining Windows console events are a separate decision, not an
// oversight. Console close, logoff and shutdown arrive as SIGTERM (the runtime
// blocks in its handler to allow cleanup, but Windows still terminates the
// process on its own deadline), while Task Manager "End task", `taskkill /F`
// and a service stop cannot be intercepted at all. This program deliberately
// subscribes to neither SIGTERM nor anything else here; docs/interrupt-handling.md
// records which events reach the scan, which do not, and what an operator
// should expect from the resume snapshot in each case.
func interruptSignals() []os.Signal {
	// MUTATION PROBE (issue #68, DO NOT MERGE): os.Interrupt removed, so the
	// scan subscribes to SIGTERM instead and no console control event reaches
	// it. This exists only to prove the Ctrl+Break test is not vacuous.
	return []os.Signal{syscall.SIGTERM}
}

// WithSIGINTCancel returns a context that is canceled when the process receives
// an OS interrupt: Ctrl+C on every platform, and equally Ctrl+Break on Windows,
// which the Go runtime delivers as the same os.Interrupt signal. Both give the
// same graceful shutdown -- resume snapshot written, output handles released,
// exit code 130 -- so an operator who breaks out of a long scan never loses
// progress regardless of which key combination they used. This is used to
// gracefully shut down the scan pipeline on user request.
//
// Events that do NOT cancel this context, and why, are listed in
// docs/interrupt-handling.md.
//
// # Parameters
//
//	parent: The parent context, used for cancellation propagation.
//
// # Returns
//
//	A new context that inherits from parent and cancels on any signal in
//	interruptSignals; the associated cancel function.
//
// # Example
//
//	ctx, cancel := state.WithSIGINTCancel(context.Background())
//	defer cancel()
//	// ctx is canceled when the user presses Ctrl+C (or Ctrl+Break on Windows)
func WithSIGINTCancel(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := signal.NotifyContext(parent, interruptSignals()...)
	return ctx, cancel
}

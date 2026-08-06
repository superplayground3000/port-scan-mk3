package state

import (
	"context"
	"os"
	"os/signal"
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
// Second, the remaining Windows terminations are a separate decision, not an
// oversight, and they do not all behave the same way. Closing the console
// WINDOW raises CTRL_CLOSE_EVENT, which does reach Go as SIGTERM (the runtime
// blocks in its handler to allow cleanup, but Windows still terminates the
// process on its own short deadline) -- so it is subscribable and deliberately
// not subscribed. Ending the PROCESS is different: taskkill /F, Task Scheduler
// and anything else routed through TerminateProcess run no user code at all and
// cannot be intercepted by any means. Logoff and shutdown fall in between: those
// events are delivered only to services, so an interactive scan never sees them.
// A service wrapper's stop behaviour is whatever the wrapper does. This program
// subscribes to none of them; docs/interrupt-handling.md records which events
// reach the scan, which do not, and what an operator should expect from the
// resume snapshot in each case.
func interruptSignals() []os.Signal {
	return []os.Signal{os.Interrupt}
}

// WithSIGINTCancel returns a context that cancels when the process receives an
// OS interrupt. The interrupt is Ctrl+C on every platform, and equally
// Ctrl+Break on Windows. The Go runtime delivers Ctrl+Break as the same
// os.Interrupt signal.
//
// Both key combinations give the same graceful shutdown: the run writes the
// resume snapshot, releases the output handles, and exits with code 130. An
// operator who stops a long scan therefore never loses progress, whichever key
// combination they used. The scan pipeline uses this context for a graceful
// shutdown on user request.
//
// The file docs/interrupt-handling.md lists the events that do NOT cancel this
// context, and the reason for each one.
//
// # Parameters
//
//	parent: The parent context for cancellation propagation.
//
// # Returns
//
//	A new context that inherits from parent and cancels on any signal in
//	interruptSignals. The associated cancel function.
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

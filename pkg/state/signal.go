package state

import (
	"context"
	"os"
	"os/signal"
)

// WithSIGINTCancel returns a context that is canceled when the process receives
// an OS interrupt signal (Ctrl+C, SIGINT). This is used to gracefully shut down
// the scan pipeline on user request.
//
// # Parameters
//
//	parent: The parent context, used for cancellation propagation.
//
// # Returns
//
//	A new context that inherits from parent and cancels on SIGINT;
//	the associated cancel function.
//
// # Example
//
//	ctx, cancel := state.WithSIGINTCancel(context.Background())
//	defer cancel()
//	// ctx is canceled when the user presses Ctrl+C
func WithSIGINTCancel(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := signal.NotifyContext(parent, os.Interrupt)
	return ctx, cancel
}

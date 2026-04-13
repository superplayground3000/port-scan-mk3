package speedctrl

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"

	"golang.org/x/term"
)

var (
	keyboardIsTerminal                           = term.IsTerminal
	keyboardMakeRaw                              = term.MakeRaw
	keyboardRestore                              = term.Restore
	keyboardEnableOutputPostProcessing           = enableOutputPostProcessing
	keyboardFD                                   = func() int { return int(os.Stdin.Fd()) }
	keyboardInput                      io.Reader = os.Stdin
)

// StartKeyboardLoop enables raw-mode keyboard handling on a terminal. When
// running in a terminal, pressing the space bar toggles manual pause/resume
// via the Controller. In non-terminal environments (piped stdin, CI), it
// returns nil immediately without starting any goroutines.
//
// The terminal is restored to its previous state when ctx is canceled or
// StartKeyboardLoop returns.
//
// # Parameters
//
//	ctx: Context whose cancellation restores the terminal.
//	c:   Controller whose manual pause state is toggled by the space bar.
//
// # Returns
//
//	nil in terminal environments; nil immediately in non-terminal environments;
//	error if terminal setup (MakeRaw) fails.
//
// # Example
//
//	ctrl := speedctrl.NewController()
//	if err := speedctrl.StartKeyboardLoop(ctx, ctrl); err != nil {
//	    log.Printf("keyboard control unavailable: %v", err)
//	}
func StartKeyboardLoop(ctx context.Context, c *Controller) error {
	fd := keyboardFD()
	isTerminal := keyboardIsTerminal
	makeRaw := keyboardMakeRaw
	restore := keyboardRestore
	input := keyboardInput

	if !isTerminal(fd) {
		return nil
	}

	oldState, err := makeRaw(fd)
	if err != nil {
		return err
	}
	if err := keyboardEnableOutputPostProcessing(fd); err != nil {
		if err := restore(fd, oldState); err != nil {
			fmt.Fprintf(os.Stderr, "speedctrl: failed to restore terminal state: %v\n", err)
		}
		return err
	}
	var restoreOnce sync.Once
	restoreTerminal := func() {
		restoreOnce.Do(func() {
			if err := restore(fd, oldState); err != nil {
				fmt.Fprintf(os.Stderr, "speedctrl: failed to restore terminal state: %v\n", err)
			}
		})
	}

	go func() {
		<-ctx.Done()
		restoreTerminal()
	}()

	go func() {
		defer func() {
			restoreTerminal()
		}()
		buf := make([]byte, 1)
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			n, readErr := input.Read(buf)
			if readErr != nil || n == 0 {
				return
			}
			select {
			case <-ctx.Done():
				return
			default:
			}
			if buf[0] == ' ' {
				c.ToggleManualPaused()
			}
		}
	}()

	return nil
}

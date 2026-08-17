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
	keyboardEnableSignalCharacters               = enableSignalCharacters
	keyboardFD                                   = func() int { return int(os.Stdin.Fd()) }
	keyboardInput                      io.Reader = os.Stdin
)

// StartKeyboardLoop enables raw-mode keyboard handling on a terminal. In a
// terminal, the space bar toggles the manual pause and resume through the
// Controller. In a non-terminal environment (piped stdin, CI), StartKeyboardLoop
// returns nil immediately and starts no goroutines.
//
// StartKeyboardLoop restores the terminal to its previous state when ctx is
// canceled or when StartKeyboardLoop returns.
//
// # Parameters
//
//	ctx: Context whose cancellation restores the terminal.
//	c:   Controller whose manual pause state the space bar toggles.
//
// # Returns
//
//	nil in a terminal environment. nil immediately in a non-terminal
//	environment. An error if the terminal setup (MakeRaw) fails.
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
	// MakeRaw also clears ISIG, so the terminal stops turning the INTR
	// character (Ctrl+C, byte 0x03) into SIGINT. The keyboard loop reads only
	// the space bar and discards that byte, so the operator loses the only way
	// to cancel a scan from the console. Put ISIG back.
	if err := keyboardEnableSignalCharacters(fd); err != nil {
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

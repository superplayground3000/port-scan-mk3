//go:build linux

package speedctrl

import "golang.org/x/sys/unix"

// enableSignalCharacters puts the ISIG flag back on a raw-mode terminal.
// term.MakeRaw clears ISIG, and a terminal without ISIG stops turning the INTR
// character (Ctrl+C, byte 0x03) into SIGINT. It also stops QUIT (Ctrl+\) and
// SUSP (Ctrl+Z). The scan then has no way to hear Ctrl+C, because the keyboard
// loop reads the byte and discards it. Do not remove this call.
func enableSignalCharacters(fd int) error {
	state, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		return err
	}
	state.Lflag |= unix.ISIG
	return unix.IoctlSetTermios(fd, unix.TCSETS, state)
}

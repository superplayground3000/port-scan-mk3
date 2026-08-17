//go:build windows

package speedctrl

import "golang.org/x/sys/windows"

var (
	consoleGetMode = windows.GetConsoleMode
	consoleSetMode = windows.SetConsoleMode
)

// enableSignalCharacters puts the ENABLE_PROCESSED_INPUT flag back on a
// raw-mode console. It is the Windows analogue of ISIG on a Unix terminal.
// term.MakeRaw clears the flag, and a console without it stops turning Ctrl+C
// into CTRL_C_EVENT. The keyboard loop then reads the byte 0x03 and discards
// it, so the operator cannot cancel a scan. Do not remove this call.
//
// Ctrl+Break is not affected, because CTRL_BREAK_EVENT does not use this flag.
func enableSignalCharacters(fd int) error {
	handle := windows.Handle(fd)
	var mode uint32
	if err := consoleGetMode(handle, &mode); err != nil {
		return err
	}
	return consoleSetMode(handle, mode|windows.ENABLE_PROCESSED_INPUT)
}

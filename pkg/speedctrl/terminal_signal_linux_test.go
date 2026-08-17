//go:build linux

package speedctrl

import (
	"fmt"
	"testing"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// openPTY opens a real pseudo-terminal and returns the master and slave file
// descriptors. The slave side is a real terminal, so termios ioctls behave
// exactly as they do on an operator's console.
func openPTY(t *testing.T) (master, slave int) {
	t.Helper()

	master, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Fatalf("open /dev/ptmx: %v", err)
	}
	t.Cleanup(func() { _ = unix.Close(master) })

	if err := unix.IoctlSetPointerInt(master, unix.TIOCSPTLCK, 0); err != nil {
		t.Fatalf("unlock pty slave: %v", err)
	}
	number, err := unix.IoctlGetInt(master, unix.TIOCGPTN)
	if err != nil {
		t.Fatalf("read pty slave number: %v", err)
	}

	slavePath := fmt.Sprintf("/dev/pts/%d", number)
	slave, err = unix.Open(slavePath, unix.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Fatalf("open %s: %v", slavePath, err)
	}
	t.Cleanup(func() { _ = unix.Close(slave) })

	return master, slave
}

func lflag(t *testing.T, fd int) uint32 {
	t.Helper()
	state, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		t.Fatalf("read termios: %v", err)
	}
	return state.Lflag
}

func TestEnableSignalCharacters_OnRawTerminal_SetsISIG(t *testing.T) {
	_, fd := openPTY(t)

	if _, err := term.MakeRaw(fd); err != nil {
		t.Fatalf("MakeRaw: %v", err)
	}

	// Without this negative half the test passes on a terminal that never had
	// ISIG cleared, and proves nothing about the fix.
	if before := lflag(t, fd); before&unix.ISIG != 0 {
		t.Fatalf("expected MakeRaw to clear ISIG, got Lflag=%#x", before)
	}

	if err := enableSignalCharacters(fd); err != nil {
		t.Fatalf("enableSignalCharacters: %v", err)
	}

	after := lflag(t, fd)
	if after&unix.ISIG == 0 {
		t.Fatalf("expected ISIG set after enableSignalCharacters, got Lflag=%#x", after)
	}
}

func TestEnableSignalCharacters_OnRawTerminal_KeepsICANONClear(t *testing.T) {
	_, fd := openPTY(t)

	if _, err := term.MakeRaw(fd); err != nil {
		t.Fatalf("MakeRaw: %v", err)
	}
	if err := enableSignalCharacters(fd); err != nil {
		t.Fatalf("enableSignalCharacters: %v", err)
	}

	// The space bar toggles pause on the first byte. ICANON must stay clear, or
	// the terminal holds the byte until the operator presses Enter.
	after := lflag(t, fd)
	if after&unix.ICANON != 0 {
		t.Fatalf("expected ICANON to stay clear, got Lflag=%#x", after)
	}
}

func TestEnableSignalCharacters_OnInvalidDescriptor_ReturnsError(t *testing.T) {
	if err := enableSignalCharacters(-1); err == nil {
		t.Fatal("expected an error on an invalid descriptor")
	}
}

func TestEnableSignalCharacters_OnRawTerminal_StillDeliversSpaceAtOnce(t *testing.T) {
	master, slave := openPTY(t)

	if _, err := term.MakeRaw(slave); err != nil {
		t.Fatalf("MakeRaw: %v", err)
	}
	if err := enableSignalCharacters(slave); err != nil {
		t.Fatalf("enableSignalCharacters: %v", err)
	}

	// The keyboard loop toggles pause on a single space byte, with no Enter
	// key. ISIG must not bring canonical line buffering back.
	if _, err := unix.Write(master, []byte{' '}); err != nil {
		t.Fatalf("write to pty master: %v", err)
	}

	// Poll first. If canonical mode ever comes back, the byte never arrives and
	// this reports the regression instead of blocking until the test timeout.
	fds := []unix.PollFd{{Fd: int32(slave), Events: unix.POLLIN}}
	ready, err := unix.Poll(fds, 2000)
	if err != nil {
		t.Fatalf("poll pty slave: %v", err)
	}
	if ready == 0 {
		t.Fatal("expected the space byte to be readable at once, got nothing in 2s")
	}

	buf := make([]byte, 1)
	n, err := unix.Read(slave, buf)
	if err != nil {
		t.Fatalf("read from pty slave: %v", err)
	}
	if n != 1 || buf[0] != ' ' {
		t.Fatalf("expected one space byte, got n=%d buf=%q", n, buf[:n])
	}
}

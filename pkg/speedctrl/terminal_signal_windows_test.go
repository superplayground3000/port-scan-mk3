//go:build windows

package speedctrl

import (
	"errors"
	"testing"

	"golang.org/x/sys/windows"
)

func stubConsoleMode(t *testing.T, get func(windows.Handle, *uint32) error, set func(windows.Handle, uint32) error) {
	t.Helper()
	oldGet := consoleGetMode
	oldSet := consoleSetMode
	t.Cleanup(func() {
		consoleGetMode = oldGet
		consoleSetMode = oldSet
	})
	consoleGetMode = get
	consoleSetMode = set
}

func TestEnableSignalCharacters_OnRawConsole_SetsEnableProcessedInput(t *testing.T) {
	// x/term clears ENABLE_PROCESSED_INPUT in its Windows makeRaw. This is the
	// mode such a console reports, with the other input flags left alone.
	const rawMode = windows.ENABLE_WINDOW_INPUT | windows.ENABLE_MOUSE_INPUT

	var gotHandle windows.Handle
	var gotMode uint32
	setCalled := false
	stubConsoleMode(t,
		func(handle windows.Handle, mode *uint32) error {
			gotHandle = handle
			*mode = rawMode
			return nil
		},
		func(handle windows.Handle, mode uint32) error {
			setCalled = true
			gotHandle = handle
			gotMode = mode
			return nil
		},
	)

	if err := enableSignalCharacters(42); err != nil {
		t.Fatalf("enableSignalCharacters: %v", err)
	}
	if !setCalled {
		t.Fatal("expected the console mode to be written back")
	}
	if gotHandle != windows.Handle(42) {
		t.Fatalf("expected handle 42, got %v", gotHandle)
	}
	if gotMode&windows.ENABLE_PROCESSED_INPUT == 0 {
		t.Fatalf("expected ENABLE_PROCESSED_INPUT set, got mode=%#x", gotMode)
	}
	if gotMode&rawMode != rawMode {
		t.Fatalf("expected the other raw-mode flags kept, got mode=%#x", gotMode)
	}
}

func TestEnableSignalCharacters_WhenGetConsoleModeFails_ReturnsError(t *testing.T) {
	wantErr := errors.New("get failed")
	stubConsoleMode(t,
		func(windows.Handle, *uint32) error { return wantErr },
		func(windows.Handle, uint32) error {
			t.Fatal("expected no write after a failed read")
			return nil
		},
	)

	if err := enableSignalCharacters(1); !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
}

func TestEnableSignalCharacters_WhenSetConsoleModeFails_ReturnsError(t *testing.T) {
	wantErr := errors.New("set failed")
	stubConsoleMode(t,
		func(_ windows.Handle, mode *uint32) error {
			*mode = 0
			return nil
		},
		func(windows.Handle, uint32) error { return wantErr },
	)

	if err := enableSignalCharacters(1); !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
}

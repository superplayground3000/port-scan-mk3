package speedctrl

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"golang.org/x/term"
)

func TestControllerGate_WhenNotPaused_IsOpenAndKeyboardLoopStarts(t *testing.T) {
	c := NewController()
	select {
	case <-c.Gate():
	default:
		t.Fatal("expected open gate")
	}
	if err := StartKeyboardLoop(context.Background(), c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestControllerToggleManualPaused_WhenCalled_TogglesState(t *testing.T) {
	c := NewController()
	if c.ManualPaused() {
		t.Fatal("expected manual paused false initially")
	}
	if paused := c.ToggleManualPaused(); !paused {
		t.Fatal("expected paused after toggle")
	}
	if !c.ManualPaused() {
		t.Fatal("expected manual paused true")
	}
}

func TestStartKeyboardLoop_WhenSpacePressed_TogglesManualPause(t *testing.T) {
	oldIsTerminal := keyboardIsTerminal
	oldMakeRaw := keyboardMakeRaw
	oldRestore := keyboardRestore
	oldEnableOutputPostProcessing := keyboardEnableOutputPostProcessing
	oldEnableSignalCharacters := keyboardEnableSignalCharacters
	oldFD := keyboardFD
	oldInput := keyboardInput
	t.Cleanup(func() {
		keyboardIsTerminal = oldIsTerminal
		keyboardMakeRaw = oldMakeRaw
		keyboardRestore = oldRestore
		keyboardEnableOutputPostProcessing = oldEnableOutputPostProcessing
		keyboardEnableSignalCharacters = oldEnableSignalCharacters
		keyboardFD = oldFD
		keyboardInput = oldInput
	})

	keyboardIsTerminal = func(fd int) bool { return true }
	keyboardMakeRaw = func(fd int) (*term.State, error) { return &term.State{}, nil }
	keyboardRestore = func(fd int, state *term.State) error { return nil }
	keyboardEnableOutputPostProcessing = func(fd int) error { return nil }
	keyboardEnableSignalCharacters = func(fd int) error { return nil }
	keyboardFD = func() int { return 0 }
	keyboardInput = bytes.NewBuffer([]byte{' '})

	c := NewController()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := StartKeyboardLoop(ctx, c); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	if !c.ManualPaused() {
		t.Fatal("expected space key to toggle manual pause")
	}
}

func TestStartKeyboardLoop_WhenMakeRawFails_ReturnsError(t *testing.T) {
	oldIsTerminal := keyboardIsTerminal
	oldMakeRaw := keyboardMakeRaw
	oldEnableOutputPostProcessing := keyboardEnableOutputPostProcessing
	oldEnableSignalCharacters := keyboardEnableSignalCharacters
	oldFD := keyboardFD
	t.Cleanup(func() {
		keyboardIsTerminal = oldIsTerminal
		keyboardMakeRaw = oldMakeRaw
		keyboardEnableOutputPostProcessing = oldEnableOutputPostProcessing
		keyboardEnableSignalCharacters = oldEnableSignalCharacters
		keyboardFD = oldFD
	})

	keyboardIsTerminal = func(fd int) bool { return true }
	keyboardMakeRaw = func(fd int) (*term.State, error) { return nil, errors.New("raw failed") }
	keyboardEnableOutputPostProcessing = func(fd int) error { return nil }
	keyboardEnableSignalCharacters = func(fd int) error { return nil }
	keyboardFD = func() int { return 0 }

	err := StartKeyboardLoop(context.Background(), NewController())
	if err == nil {
		t.Fatal("expected make raw error")
	}
}

func TestStartKeyboardLoop_WhenRawModeEnabled_EnablesOutputPostProcessing(t *testing.T) {
	oldIsTerminal := keyboardIsTerminal
	oldMakeRaw := keyboardMakeRaw
	oldRestore := keyboardRestore
	oldEnableOutputPostProcessing := keyboardEnableOutputPostProcessing
	oldEnableSignalCharacters := keyboardEnableSignalCharacters
	oldFD := keyboardFD
	oldInput := keyboardInput
	t.Cleanup(func() {
		keyboardIsTerminal = oldIsTerminal
		keyboardMakeRaw = oldMakeRaw
		keyboardRestore = oldRestore
		keyboardEnableOutputPostProcessing = oldEnableOutputPostProcessing
		keyboardEnableSignalCharacters = oldEnableSignalCharacters
		keyboardFD = oldFD
		keyboardInput = oldInput
	})

	keyboardIsTerminal = func(fd int) bool { return true }
	keyboardMakeRaw = func(fd int) (*term.State, error) { return &term.State{}, nil }
	keyboardRestore = func(fd int, state *term.State) error { return nil }
	keyboardEnableSignalCharacters = func(fd int) error { return nil }
	keyboardFD = func() int { return 123 }
	keyboardInput = bytes.NewBuffer(nil)

	enabledFD := -1
	keyboardEnableOutputPostProcessing = func(fd int) error {
		enabledFD = fd
		return nil
	}

	if err := StartKeyboardLoop(context.Background(), NewController()); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if enabledFD != 123 {
		t.Fatalf("expected output post-processing to run on fd=123, got %d", enabledFD)
	}
}

func TestStartKeyboardLoop_WhenEnableOutputPostProcessingFails_RestoresStateAndReturnsError(t *testing.T) {
	oldIsTerminal := keyboardIsTerminal
	oldMakeRaw := keyboardMakeRaw
	oldRestore := keyboardRestore
	oldEnableOutputPostProcessing := keyboardEnableOutputPostProcessing
	oldEnableSignalCharacters := keyboardEnableSignalCharacters
	oldFD := keyboardFD
	t.Cleanup(func() {
		keyboardIsTerminal = oldIsTerminal
		keyboardMakeRaw = oldMakeRaw
		keyboardRestore = oldRestore
		keyboardEnableOutputPostProcessing = oldEnableOutputPostProcessing
		keyboardEnableSignalCharacters = oldEnableSignalCharacters
		keyboardFD = oldFD
	})

	keyboardIsTerminal = func(fd int) bool { return true }
	keyboardEnableSignalCharacters = func(fd int) error { return nil }
	keyboardFD = func() int { return 7 }
	rawState := &term.State{}
	keyboardMakeRaw = func(fd int) (*term.State, error) { return rawState, nil }

	restoreCalled := false
	keyboardRestore = func(fd int, state *term.State) error {
		if fd == 7 && state == rawState {
			restoreCalled = true
		}
		return nil
	}
	keyboardEnableOutputPostProcessing = func(fd int) error { return errors.New("output flags failed") }

	err := StartKeyboardLoop(context.Background(), NewController())
	if err == nil {
		t.Fatal("expected output post-processing error")
	}
	if !restoreCalled {
		t.Fatal("expected restore to be called when output post-processing setup fails")
	}
}

func TestStartKeyboardLoop_WhenRawModeEnabled_EnablesSignalCharacters(t *testing.T) {
	oldIsTerminal := keyboardIsTerminal
	oldMakeRaw := keyboardMakeRaw
	oldRestore := keyboardRestore
	oldEnableOutputPostProcessing := keyboardEnableOutputPostProcessing
	oldEnableSignalCharacters := keyboardEnableSignalCharacters
	oldFD := keyboardFD
	oldInput := keyboardInput
	t.Cleanup(func() {
		keyboardIsTerminal = oldIsTerminal
		keyboardMakeRaw = oldMakeRaw
		keyboardRestore = oldRestore
		keyboardEnableOutputPostProcessing = oldEnableOutputPostProcessing
		keyboardEnableSignalCharacters = oldEnableSignalCharacters
		keyboardFD = oldFD
		keyboardInput = oldInput
	})

	keyboardIsTerminal = func(fd int) bool { return true }
	keyboardMakeRaw = func(fd int) (*term.State, error) { return &term.State{}, nil }
	keyboardRestore = func(fd int, state *term.State) error { return nil }
	keyboardEnableOutputPostProcessing = func(fd int) error { return nil }
	keyboardFD = func() int { return 321 }
	keyboardInput = bytes.NewBuffer(nil)

	enabledFD := -1
	keyboardEnableSignalCharacters = func(fd int) error {
		enabledFD = fd
		return nil
	}

	if err := StartKeyboardLoop(context.Background(), NewController()); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if enabledFD != 321 {
		t.Fatalf("expected signal characters to be enabled on fd=321, got %d", enabledFD)
	}
}

func TestStartKeyboardLoop_WhenEnableSignalCharactersFails_RestoresStateAndReturnsError(t *testing.T) {
	oldIsTerminal := keyboardIsTerminal
	oldMakeRaw := keyboardMakeRaw
	oldRestore := keyboardRestore
	oldEnableOutputPostProcessing := keyboardEnableOutputPostProcessing
	oldEnableSignalCharacters := keyboardEnableSignalCharacters
	oldFD := keyboardFD
	t.Cleanup(func() {
		keyboardIsTerminal = oldIsTerminal
		keyboardMakeRaw = oldMakeRaw
		keyboardRestore = oldRestore
		keyboardEnableOutputPostProcessing = oldEnableOutputPostProcessing
		keyboardEnableSignalCharacters = oldEnableSignalCharacters
		keyboardFD = oldFD
	})

	keyboardIsTerminal = func(fd int) bool { return true }
	keyboardFD = func() int { return 11 }
	rawState := &term.State{}
	keyboardMakeRaw = func(fd int) (*term.State, error) { return rawState, nil }
	keyboardEnableOutputPostProcessing = func(fd int) error { return nil }

	restoreCalled := false
	keyboardRestore = func(fd int, state *term.State) error {
		if fd == 11 && state == rawState {
			restoreCalled = true
		}
		return nil
	}
	keyboardEnableSignalCharacters = func(fd int) error { return errors.New("signal flags failed") }

	err := StartKeyboardLoop(context.Background(), NewController())
	if err == nil {
		t.Fatal("expected signal character error")
	}
	if !restoreCalled {
		t.Fatal("expected restore to be called when signal character setup fails")
	}
}

type blockingReader struct {
	started chan struct{}
	release chan struct{}
}

func (r *blockingReader) Read(p []byte) (int, error) {
	select {
	case <-r.started:
	default:
		close(r.started)
	}
	<-r.release
	return 0, io.EOF
}

func TestStartKeyboardLoop_WhenContextCanceledDuringBlockingRead_RestoresTerminal(t *testing.T) {
	oldIsTerminal := keyboardIsTerminal
	oldMakeRaw := keyboardMakeRaw
	oldRestore := keyboardRestore
	oldEnableOutputPostProcessing := keyboardEnableOutputPostProcessing
	oldEnableSignalCharacters := keyboardEnableSignalCharacters
	oldFD := keyboardFD
	oldInput := keyboardInput
	t.Cleanup(func() {
		keyboardIsTerminal = oldIsTerminal
		keyboardMakeRaw = oldMakeRaw
		keyboardRestore = oldRestore
		keyboardEnableOutputPostProcessing = oldEnableOutputPostProcessing
		keyboardEnableSignalCharacters = oldEnableSignalCharacters
		keyboardFD = oldFD
		keyboardInput = oldInput
	})

	keyboardIsTerminal = func(fd int) bool { return true }
	keyboardMakeRaw = func(fd int) (*term.State, error) { return &term.State{}, nil }
	keyboardEnableOutputPostProcessing = func(fd int) error { return nil }
	keyboardEnableSignalCharacters = func(fd int) error { return nil }
	keyboardFD = func() int { return 9 }

	reader := &blockingReader{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	keyboardInput = reader

	restored := make(chan struct{}, 1)
	keyboardRestore = func(fd int, state *term.State) error {
		select {
		case restored <- struct{}{}:
		default:
		}
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := StartKeyboardLoop(ctx, NewController()); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	select {
	case <-reader.started:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected keyboard loop to start blocking read")
	}

	cancel()

	select {
	case <-restored:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected restore to run after context cancellation")
	}

	close(reader.release)
}

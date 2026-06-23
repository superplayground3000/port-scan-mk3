package scanapp

import (
	"context"
	"errors"
	"os/exec"
	"reflect"
	"testing"
	"time"
)

func TestBuildPingCommand_WindowsUsesCountAndTimeoutFlags(t *testing.T) {
	cmd, args, err := buildPingCommand("windows", "10.0.0.7", 1500*time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cmd != "ping" {
		t.Fatalf("unexpected command: %s", cmd)
	}

	want := []string{"-n", "1", "-w", "1500", "10.0.0.7"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("unexpected args: got %v want %v", args, want)
	}
}

func TestBuildPingCommand_LinuxAndDarwinUseNonWindowsPath(t *testing.T) {
	tests := []struct {
		name string
		goos string
	}{
		{name: "linux", goos: "linux"},
		{name: "darwin", goos: "darwin"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd, args, err := buildPingCommand(tc.goos, "10.0.0.7", 1500*time.Millisecond)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if cmd != "ping" {
				t.Fatalf("unexpected command: %s", cmd)
			}

			want := []string{"-c", "1", "10.0.0.7"}
			if !reflect.DeepEqual(args, want) {
				t.Fatalf("unexpected args: got %v want %v", args, want)
			}
		})
	}
}

func TestCommandReachabilityChecker_ReachesWhenRunnerReturnsNil(t *testing.T) {
	runner := &fakeReachabilityRunner{runErr: nil}
	checker := &commandReachabilityChecker{goos: "linux", runner: runner}

	got := checker.Check(context.Background(), "10.0.0.7", 200*time.Millisecond)

	if !got.Reachable {
		t.Fatalf("expected reachable result, got %#v", got)
	}
	if got.FailureText != "" {
		t.Fatalf("expected empty failure text, got %q", got.FailureText)
	}
	if got.IP != "10.0.0.7" {
		t.Fatalf("unexpected ip: %q", got.IP)
	}
	if runner.name != "ping" {
		t.Fatalf("unexpected command: %s", runner.name)
	}
	wantArgs := []string{"-c", "1", "10.0.0.7"}
	if !reflect.DeepEqual(runner.args, wantArgs) {
		t.Fatalf("unexpected runner args: got %v want %v", runner.args, wantArgs)
	}
}

func TestCommandReachabilityChecker_MarksUnreachableOnDeadlineOrExitError(t *testing.T) {
	t.Run("deadline", func(t *testing.T) {
		runner := &fakeReachabilityRunner{waitForContext: true}
		checker := &commandReachabilityChecker{goos: "linux", runner: runner}

		got := checker.Check(context.Background(), "10.0.0.7", 10*time.Millisecond)

		if got.Reachable {
			t.Fatalf("expected unreachable result, got %#v", got)
		}
		if got.FailureText == "" {
			t.Fatalf("expected failure text, got %#v", got)
		}
		if !errors.Is(runner.observedCtx.Err(), context.DeadlineExceeded) {
			t.Fatalf("expected deadline exceeded context, got %v", runner.observedCtx.Err())
		}
	})

	t.Run("exit error", func(t *testing.T) {
		exitErr := errors.New("exit status 1")
		runner := &fakeReachabilityRunner{runErr: exitErr}
		checker := &commandReachabilityChecker{goos: "linux", runner: runner}

		got := checker.Check(context.Background(), "10.0.0.7", 200*time.Millisecond)

		if got.Reachable {
			t.Fatalf("expected unreachable result, got %#v", got)
		}
		if got.FailureText != exitErr.Error() {
			t.Fatalf("unexpected failure text: %q", got.FailureText)
		}
	})
}

func TestCommandReachabilityChecker_CheckDetailed_ReturnsFatalErrorOnToolLevelFailure(t *testing.T) {
	execErr := &exec.Error{Name: "ping", Err: exec.ErrNotFound}
	runner := &fakeReachabilityRunner{runErr: execErr}
	checker := &commandReachabilityChecker{goos: "linux", runner: runner}

	got, err := checker.CheckDetailed(context.Background(), "10.0.0.7", 200*time.Millisecond)

	if !errors.As(err, &execErr) {
		t.Fatalf("expected exec error, got %v", err)
	}
	if got.Reachable {
		t.Fatalf("expected tool-level failure to stay unreachable, got %#v", got)
	}
	if got.FailureText != execErr.Error() {
		t.Fatalf("unexpected failure text: %q", got.FailureText)
	}
}

func TestCommandReachabilityChecker_CheckDetailed_ReturnsFatalErrorOnParentContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	runner := &fakeReachabilityRunner{waitForContext: true}
	checker := &commandReachabilityChecker{goos: "linux", runner: runner}

	got, err := checker.CheckDetailed(ctx, "10.0.0.7", 200*time.Millisecond)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled error, got %v", err)
	}
	if got.Reachable {
		t.Fatalf("expected canceled check to remain unreachable, got %#v", got)
	}
	if got.FailureText != context.Canceled.Error() {
		t.Fatalf("unexpected failure text: %q", got.FailureText)
	}
}

func TestCommandReachabilityChecker_CheckDetailed_TreatsPerIPTimeoutAsUnreachable(t *testing.T) {
	runner := &fakeReachabilityRunner{waitForContext: true}
	checker := &commandReachabilityChecker{goos: "linux", runner: runner}

	got, err := checker.CheckDetailed(context.Background(), "10.0.0.7", 10*time.Millisecond)

	if err != nil {
		t.Fatalf("expected per-ip timeout to stay non-fatal, got %v", err)
	}
	if got.Reachable {
		t.Fatalf("expected unreachable result, got %#v", got)
	}
	if got.FailureText == "" {
		t.Fatalf("expected timeout failure text, got %#v", got)
	}
	if !errors.Is(runner.observedCtx.Err(), context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded context, got %v", runner.observedCtx.Err())
	}
}

type fakeReachabilityRunner struct {
	runErr         error
	waitForContext bool
	name           string
	args           []string
	observedCtx    context.Context
}

func (f *fakeReachabilityRunner) Run(ctx context.Context, name string, args ...string) error {
	f.observedCtx = ctx
	f.name = name
	f.args = append([]string(nil), args...)
	if f.waitForContext {
		<-ctx.Done()
		return ctx.Err()
	}
	return f.runErr
}

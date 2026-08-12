//go:build !windows

package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"
)

const interruptProcessHelper = "PORT_SCAN_INTERRUPT_PROCESS_HELPER"

func TestScanInterruptContext_OnLinux_ProcessSecondSIGINTExits130(t *testing.T) {
	if os.Getenv(interruptProcessHelper) == "1" {
		ctx, stop := newScanInterruptContext(context.Background(), os.Stderr, os.Exit)
		defer stop()
		fmt.Fprintln(os.Stderr, "interrupt helper ready")
		<-ctx.Done()
		fmt.Fprintln(os.Stderr, "interrupt helper graceful stop started")
		select {}
	}

	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("find test executable: %v", err)
	}
	child := exec.Command(executable, "-test.run=^TestScanInterruptContext_OnLinux_ProcessSecondSIGINTExits130$")
	child.Env = append(os.Environ(), interruptProcessHelper+"=1")
	stderr, err := child.StderrPipe()
	if err != nil {
		t.Fatalf("open child stderr: %v", err)
	}
	if err := child.Start(); err != nil {
		t.Fatalf("start child: %v", err)
	}
	killed := false
	defer func() {
		if !killed {
			_ = child.Process.Kill()
		}
	}()

	lines := make(chan string, 16)
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		close(lines)
	}()
	waitForInterruptLine(t, lines, "interrupt helper ready")
	if err := child.Process.Signal(os.Interrupt); err != nil {
		t.Fatalf("send first SIGINT: %v", err)
	}
	waitForInterruptLine(t, lines, "graceful finalization is in progress")
	if err := child.Process.Signal(os.Interrupt); err != nil {
		t.Fatalf("send second SIGINT: %v", err)
	}

	err = child.Wait()
	killed = true
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 130 {
		t.Fatalf("child exit error = %v, want exit code 130", err)
	}
}

func waitForInterruptLine(t *testing.T, lines <-chan string, substring string) {
	t.Helper()
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	for {
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatalf("child stderr closed before %q", substring)
			}
			if strings.Contains(line, substring) {
				return
			}
		case <-timer.C:
			t.Fatalf("child stderr did not contain %q", substring)
		}
	}
}

func TestScanInterruptContext_OnLinux_FirstSIGINTExplainsGracefulStopAndSecondExits130(t *testing.T) {
	swallow := make(chan os.Signal, 1)
	signal.Notify(swallow, os.Interrupt)
	defer signal.Stop(swallow)

	var stderr bytes.Buffer
	exitCodes := make(chan int, 1)
	ctx, stop := newScanInterruptContext(context.Background(), &stderr, func(code int) {
		exitCodes <- code
	})
	defer stop()

	if err := syscall.Kill(syscall.Getpid(), syscall.SIGINT); err != nil {
		t.Fatalf("send first SIGINT: %v", err)
	}
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("first SIGINT did not cancel the scan context")
	}
	message := stderr.String()
	if !strings.Contains(message, "graceful finalization is in progress") || !strings.Contains(message, "again to force exit 130") {
		t.Fatalf("first-interrupt message = %q", message)
	}

	if err := syscall.Kill(syscall.Getpid(), syscall.SIGINT); err != nil {
		t.Fatalf("send second SIGINT: %v", err)
	}
	select {
	case code := <-exitCodes:
		if code != 130 {
			t.Fatalf("exit code = %d, want 130", code)
		}
	case <-time.After(time.Second):
		t.Fatal("second SIGINT did not request exit 130")
	}
}

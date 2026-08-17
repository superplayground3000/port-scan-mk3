//go:build windows

package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

const windowsInterruptProcessHelper = "PORT_SCAN_WINDOWS_INTERRUPT_PROCESS_HELPER"

var (
	windowsKernel32                     = syscall.NewLazyDLL("kernel32.dll")
	windowsGenerateConsoleCtrlEventProc = windowsKernel32.NewProc("GenerateConsoleCtrlEvent")
	windowsAllocConsoleProc             = windowsKernel32.NewProc("AllocConsole")
)

func TestScanInterruptContext_OnWindows_ProcessSecondCtrlBreakExits130(t *testing.T) {
	if os.Getenv(windowsInterruptProcessHelper) == "1" {
		ctx, stop := newScanInterruptContext(context.Background(), os.Stderr, os.Exit)
		defer stop()
		fmt.Fprintln(os.Stderr, "interrupt helper ready")
		<-ctx.Done()
		fmt.Fprintln(os.Stderr, "interrupt helper graceful stop started")
		select {}
	}

	ensureInterruptTestConsole(t)
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("find test executable: %v", err)
	}
	child := exec.Command(executable, "-test.run=^TestScanInterruptContext_OnWindows_ProcessSecondCtrlBreakExits130$")
	child.Env = append(os.Environ(), windowsInterruptProcessHelper+"=1")
	child.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
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
	waitForWindowsInterruptLine(t, lines, "interrupt helper ready")
	sendInterruptTestCtrlBreak(t, child.Process.Pid)
	waitForWindowsInterruptLine(t, lines, "graceful finalization is in progress")
	sendInterruptTestCtrlBreak(t, child.Process.Pid)

	err = waitForForcedInterruptExit(t, child)
	killed = true
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 130 {
		t.Fatalf("child exit error = %v, want exit code 130", err)
	}
}

func ensureInterruptTestConsole(t *testing.T) {
	t.Helper()
	ok, _, err := windowsAllocConsoleProc.Call()
	if ok != 0 {
		return
	}
	if errno, isErrno := err.(syscall.Errno); isErrno && errno == syscall.ERROR_ACCESS_DENIED {
		return
	}
	t.Fatalf("attach test console: %v", err)
}

func sendInterruptTestCtrlBreak(t *testing.T, pid int) {
	t.Helper()
	const ctrlBreakEvent = 1
	ok, _, err := windowsGenerateConsoleCtrlEventProc.Call(uintptr(ctrlBreakEvent), uintptr(pid))
	if ok == 0 {
		t.Fatalf("send Ctrl+Break to process %d: %v", pid, err)
	}
}

func waitForWindowsInterruptLine(t *testing.T, lines <-chan string, substring string) {
	t.Helper()
	timer := time.NewTimer(30 * time.Second)
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

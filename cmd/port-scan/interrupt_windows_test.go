//go:build windows

package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/state"
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

// TestRunScan_OnWindows_ProcessFirstCtrlBreakSavesResumeStateAndExits130 is the
// Windows half of the graceful contract. It is the twin of the Linux case in
// interrupt_unix_test.go.
//
// Windows needs its own case because the signal path differs, not because the
// exit mapping differs. The console driver delivers Ctrl+Break to a process
// group, so the child runs in a new group and the parent calls
// GenerateConsoleCtrlEvent. Issue #156 showed that a terminal mode change can
// stop an interrupt from reaching the process at all, and only a real signal to
// a real process can catch that class of defect.
//
// This case sends ONE Ctrl+Break, so exit code 130 must come from the run
// itself. The "scan canceled" line separates the graceful branch from the
// forced branch, because both exit with 130.
func TestRunScan_OnWindows_ProcessFirstCtrlBreakSavesResumeStateAndExits130(t *testing.T) {
	if os.Getenv(gracefulInterruptProcessHelper) == "1" {
		os.Exit(runScan(gracefulInterruptScanArgs(os.Getenv(gracefulInterruptProcessDir)), os.Stdout, os.Stderr))
	}

	dir := t.TempDir()
	cidrFile := filepath.Join(dir, "cidr.csv")
	portFile := filepath.Join(dir, "ports.csv")
	// Loopback only, and no listener: every probe is refused locally
	// (constitution V). The /24 gives the run more targets than one interrupt
	// window can consume, so the child is always still scanning when it stops.
	if err := os.WriteFile(cidrFile, []byte("fab_name,ip,ip_cidr,cidr_name\nfab1,127.0.0.1/24,127.0.0.1/24,loopback\n"), 0o644); err != nil {
		t.Fatalf("write cidr file: %v", err)
	}
	if err := os.WriteFile(portFile, []byte("1/tcp\n2/tcp\n3/tcp\n"), 0o644); err != nil {
		t.Fatalf("write port file: %v", err)
	}
	snapshotPath := mustGenerateBucketSnapshot(t, dir, cidrFile, portFile)

	ensureInterruptTestConsole(t)
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("find test executable: %v", err)
	}
	child := exec.Command(executable, "-test.run=^TestRunScan_OnWindows_ProcessFirstCtrlBreakSavesResumeStateAndExits130$")
	child.Env = append(os.Environ(), gracefulInterruptProcessHelper+"=1", gracefulInterruptProcessDir+"="+dir)
	child.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
	var childStderr bytes.Buffer
	child.Stdout = &bytes.Buffer{}
	child.Stderr = &childStderr
	if err := child.Start(); err != nil {
		t.Fatalf("start child: %v", err)
	}
	killed := false
	defer func() {
		if !killed {
			_ = child.Process.Kill()
		}
	}()

	// A written result row proves the scan loop is running, so the interrupt
	// handler is installed and the run has progress worth saving.
	waitForScanResultRow(t, dir)
	sendInterruptTestCtrlBreak(t, child.Process.Pid)

	err = waitForGracefulInterruptExit(t, child)
	killed = true
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 130 {
		t.Fatalf("child exit error = %v, want exit code 130; stderr:\n%s", err, childStderr.String())
	}
	if !strings.Contains(childStderr.String(), "scan canceled") {
		t.Fatalf("child did not take the graceful branch; stderr:\n%s", childStderr.String())
	}

	snapshot, loadErr := state.LoadSnapshot(snapshotPath)
	if loadErr != nil {
		t.Fatalf("load resume snapshot: %v", loadErr)
	}
	scanned := 0
	for _, chunk := range snapshot.Chunks {
		scanned += chunk.ScannedCount
	}
	if scanned == 0 {
		t.Fatalf("resume snapshot saved no scanned targets over %d chunks", len(snapshot.Chunks))
	}
}

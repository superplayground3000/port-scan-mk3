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
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/state"
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

	err = waitForForcedInterruptExit(t, child)
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

const (
	gracefulInterruptProcessHelper = "PORT_SCAN_GRACEFUL_INTERRUPT_PROCESS_HELPER"
	gracefulInterruptProcessDir    = "PORT_SCAN_GRACEFUL_INTERRUPT_PROCESS_DIR"
)

// TestRunScan_OnLinux_ProcessFirstSIGINTSavesResumeStateAndExits130 covers the
// graceful branch at process level: one real interrupt to a real child process.
//
// The forced branch, which the second interrupt takes, exits 130 from the signal
// handler. This case sends one signal only, so the code must come from the run
// itself: scanapp.Run returns context.Canceled, runScan maps it to 130, and it
// prints "scan canceled" first. The message is what tells the two branches
// apart, because both exit with 130.
func TestRunScan_OnLinux_ProcessFirstSIGINTSavesResumeStateAndExits130(t *testing.T) {
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

	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("find test executable: %v", err)
	}
	child := exec.Command(executable, "-test.run=^TestRunScan_OnLinux_ProcessFirstSIGINTSavesResumeStateAndExits130$")
	child.Env = append(os.Environ(), gracefulInterruptProcessHelper+"=1", gracefulInterruptProcessDir+"="+dir)
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
	if err := child.Process.Signal(os.Interrupt); err != nil {
		t.Fatalf("send the only SIGINT: %v", err)
	}

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

func gracefulInterruptScanArgs(dir string) []string {
	return []string{
		"-cidr-file", filepath.Join(dir, "cidr.csv"),
		"-port-file", filepath.Join(dir, "ports.csv"),
		"-resume", filepath.Join(dir, "buckets.json"),
		"-output", filepath.Join(dir, "scan_results.csv"),
		"-output-flush-results", "1",
		"-workers", "1",
		"-delay", "20ms",
		"-timeout", "100ms",
		"-disable-api=true",
	}
}

func waitForScanResultRow(t *testing.T, dir string) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		matches, globErr := filepath.Glob(filepath.Join(dir, "scan_results-*.csv"))
		if globErr != nil {
			t.Fatalf("find the scan output: %v", globErr)
		}
		for _, match := range matches {
			data, readErr := os.ReadFile(match)
			if readErr != nil {
				continue
			}
			if len(bytes.Split(bytes.TrimSpace(data), []byte("\n"))) >= 2 {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the scan child wrote no result row")
}

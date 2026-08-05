//go:build windows

package state

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// ctrlBreakEvent is the CTRL_BREAK_EVENT constant from wincon.h. It is the ONLY
// console control event this test can send: CTRL_C_EVENT is ignored by a process
// created with CREATE_NEW_PROCESS_GROUP (Windows disables Ctrl+C for the new
// group), and the remaining events (CTRL_CLOSE/LOGOFF/SHUTDOWN) cannot be
// generated with GenerateConsoleCtrlEvent at all. See docs/interrupt-handling.md
// for what that means for the events this suite deliberately does not simulate.
const ctrlBreakEvent = 1

var (
	kernel32                     = syscall.NewLazyDLL("kernel32.dll")
	procGenerateConsoleCtrlEvent = kernel32.NewProc("GenerateConsoleCtrlEvent")
	procGetConsoleWindow         = kernel32.NewProc("GetConsoleWindow")
	procAllocConsole             = kernel32.NewProc("AllocConsole")
)

// TestWithSIGINTCancel_OnWindows_CtrlBreakExits130AndLeavesResumableSnapshot is
// the real thing: it builds the actual port-scan EXE, starts a paced
// loopback-only scan in its OWN process group, and interrupts it with a genuine
// Windows console control event via GenerateConsoleCtrlEvent -- not a simulated
// signal, and not a direct call to the CancelFunc.
//
// That distinction is the whole point. Windows has no kill(2) and
// os.Process.Signal(os.Interrupt) is unsupported there, so every existing test of
// this code path either cancels the context directly or provokes a failure
// through the pressure API. Neither exercises the console-event delivery path
// that a human pressing Ctrl+Break actually uses, so neither could show whether
// Ctrl+Break is handled at all (issue #68). It is -- the Go runtime folds
// CTRL_BREAK_EVENT into SIGINT / os.Interrupt -- but until this test that was an
// undocumented property of the toolchain that nothing in this repo verified, on
// the exact Go version this repo pins. Dropping os.Interrupt from
// interruptSignals turns this test red; that mutation was run on windows-latest
// to prove the test is not vacuous.
//
// What it asserts after the break:
//   - the process exits 130 (the graceful-cancel contract, cmd/port-scan/command_handlers.go)
//   - the resume snapshot on disk parses as a valid Snapshot and is mid-flight
//   - the run really was interrupted (some rows written, not all)
//   - every output handle was released, checked by renaming the files, which
//     Windows refuses with a sharing violation while a handle is open
//
// Isolation (constitution V): every target is 127.0.0.1 on ports this test
// itself binds. Nothing external is dialed.
func TestWithSIGINTCancel_OnWindows_CtrlBreakExits130AndLeavesResumableSnapshot(t *testing.T) {
	ensureConsoleForCtrlEvents(t)

	ws := t.TempDir()
	exe := buildPortScanExe(t, ws)

	// One real listener. Its accept is the event that proves the scan is
	// actively dialing, so the interrupt lands mid-flight structurally rather
	// than after a hopeful sleep (this repo has a history of sleep-based
	// Windows flakes: issues #59, #79).
	openListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to bind the loopback listener: %v", err)
	}
	defer openListener.Close()
	openPort := openListener.Addr().(*net.TCPAddr).Port

	dialed := make(chan struct{})
	go func() {
		conn, acceptErr := openListener.Accept()
		if acceptErr != nil {
			return
		}
		_ = conn.Close()
		close(dialed)
	}()

	// The open port goes FIRST so the accept fires on the very first dial,
	// leaving the remaining targets (~15s of work at 2/s) still to do when the
	// break arrives. The rest are ports this test bound and released, so a
	// connect to them is refused rather than left hanging.
	ports := []int{openPort}
	for i := 0; i < 29; i++ {
		ports = append(ports, reserveClosedLoopbackPort(t))
	}

	cidrFile := filepath.Join(ws, "cidr.csv")
	writeTestFile(t, cidrFile, "fab_name,ip,ip_cidr,cidr_name\r\nfab1,127.0.0.1,127.0.0.1/32,loopback\r\n")

	portLines := make([]string, 0, len(ports))
	for _, p := range ports {
		portLines = append(portLines, fmt.Sprintf("%d/tcp", p))
	}
	portFile := filepath.Join(ws, "ports.csv")
	writeTestFile(t, portFile, strings.Join(portLines, "\r\n")+"\r\n")

	buckets := filepath.Join(ws, "buckets.json")
	outAnchor := filepath.Join(ws, "scan_results.csv")

	genOut, err := exec.Command(exe, "generate-buckets",
		"-cidr-file", cidrFile,
		"-port-file", portFile,
		"-buckets-out", buckets,
		"-log-level", "error",
	).CombinedOutput()
	if err != nil {
		t.Fatalf("generate-buckets failed: %v\n%s", err, genOut)
	}

	stdoutPath := filepath.Join(ws, "scan-stdout.log")
	stderrPath := filepath.Join(ws, "scan-stderr.log")
	stdoutFile := createTestFile(t, stdoutPath)
	defer stdoutFile.Close()
	stderrFile := createTestFile(t, stderrPath)
	defer stderrFile.Close()

	// bucket-rate 2 over 30 targets is ~15s of work with a single worker, so
	// the scan is guaranteed to still be running when the first dial completes.
	scan := exec.Command(exe, "scan",
		"-cidr-file", cidrFile,
		"-resume", buckets,
		"-output", outAnchor,
		"-disable-api",
		"-workers", "1",
		"-bucket-rate", "2",
		"-bucket-capacity", "2",
		"-delay", "0ms",
		"-timeout", "200ms",
		"-log-level", "error",
	)
	scan.Dir = ws
	scan.Stdout = stdoutFile
	scan.Stderr = stderrFile
	// CREATE_NEW_PROCESS_GROUP makes the child its own process-group leader, so
	// its group id equals its pid and GenerateConsoleCtrlEvent can target it
	// without the event also reaching this test process (or the CI shell that
	// shares the same console).
	scan.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}

	if err := scan.Start(); err != nil {
		t.Fatalf("failed to start the scan subprocess: %v", err)
	}

	waitDone := make(chan error, 1)
	go func() { waitDone <- scan.Wait() }()

	// If anything below fails the child must not outlive the test.
	killed := false
	defer func() {
		if !killed {
			_ = scan.Process.Kill()
		}
	}()

	select {
	case <-dialed:
	case err := <-waitDone:
		killed = true
		t.Fatalf("the scan exited (%v) before it dialed the loopback listener; it never reached the interruptible phase\nstdout:\n%s\nstderr:\n%s",
			err, readTestFile(t, stdoutPath), readTestFile(t, stderrPath))
	case <-time.After(90 * time.Second):
		t.Fatalf("the scan never dialed the loopback listener within 90s\nstdout:\n%s\nstderr:\n%s",
			readTestFile(t, stdoutPath), readTestFile(t, stderrPath))
	}

	sendCtrlBreak(t, scan.Process.Pid)

	var exitCode int
	select {
	case err := <-waitDone:
		killed = true
		exitCode = exitCodeOf(t, err)
	case <-time.After(90 * time.Second):
		t.Fatalf("the scan did not exit within 90s of CTRL_BREAK_EVENT; Ctrl+Break did not reach the interrupt handler\nstdout:\n%s\nstderr:\n%s",
			readTestFile(t, stdoutPath), readTestFile(t, stderrPath))
	}

	if exitCode != 130 {
		t.Fatalf("scan exited %d after Ctrl+Break, want 130 (graceful cancel)\nstdout:\n%s\nstderr:\n%s",
			exitCode, readTestFile(t, stdoutPath), readTestFile(t, stderrPath))
	}

	// The snapshot must be readable by the very loader a -resume run uses.
	snap, err := LoadSnapshot(buckets)
	if err != nil {
		t.Fatalf("the resume snapshot left by the interrupted scan does not load: %v", err)
	}
	if len(snap.Chunks) == 0 {
		t.Fatal("the resume snapshot holds no chunks, so nothing could be resumed")
	}
	var dispatched, total int
	for _, c := range snap.Chunks {
		dispatched += c.NextIndex
		total += c.TotalCount
	}
	if total != len(ports) {
		t.Fatalf("the snapshot declares %d total tasks, want %d", total, len(ports))
	}
	if dispatched <= 0 || dispatched >= total {
		t.Fatalf("the snapshot's dispatch cursor is %d of %d; want a mid-flight cursor (0 < cursor < total), "+
			"which is what makes the interrupt resumable rather than a no-op or a completed run", dispatched, total)
	}
	if snap.Output == nil || snap.Output.ScanPath == "" {
		t.Fatal("the snapshot records no output path, so a -resume run could not append to the partial results file")
	}

	scanCSV := singleFileMatching(t, ws, "scan_results-*.csv", "interrupted scan")
	openedCSV := singleFileMatching(t, ws, "opened_results-*.csv", "interrupted scan")

	rows := csvDataRowCount(t, scanCSV)
	if rows == 0 {
		t.Fatal("the interrupted scan wrote no result rows at all")
	}
	if rows >= len(ports) {
		t.Fatalf("the scan wrote %d of %d rows, so it finished instead of being interrupted mid-flight", rows, len(ports))
	}

	// Renaming is the direct Windows test that the process closed its files:
	// an open handle makes the rename fail with a sharing violation. The Linux
	// gates cannot check this at all.
	assertHandleReleased(t, scanCSV, "scan_results after Ctrl+Break")
	assertHandleReleased(t, openedCSV, "opened_results after Ctrl+Break")
	assertHandleReleased(t, buckets, "resume snapshot after Ctrl+Break")
}

// ensureConsoleForCtrlEvents guarantees this process has a console.
// GenerateConsoleCtrlEvent only reaches process groups attached to the CALLER's
// console, and a test binary launched by a CI runner service may have none. It
// fails the test rather than skipping: a Ctrl+Break test that quietly opts out
// on the one platform it exists for would be worse than no test.
func ensureConsoleForCtrlEvents(t *testing.T) {
	t.Helper()

	if hwnd, _, _ := procGetConsoleWindow.Call(); hwnd != 0 {
		return
	}
	if ok, _, err := procAllocConsole.Call(); ok == 0 {
		t.Fatalf("this process has no console and AllocConsole failed (%v), so no console control event can be generated", err)
	}
}

// sendCtrlBreak raises CTRL_BREAK_EVENT for the process group led by pid. The
// child was started with CREATE_NEW_PROCESS_GROUP, so its group id is its pid
// and the event cannot leak back into this test process.
func sendCtrlBreak(t *testing.T, pid int) {
	t.Helper()

	ok, _, err := procGenerateConsoleCtrlEvent.Call(uintptr(ctrlBreakEvent), uintptr(pid))
	if ok == 0 {
		t.Fatalf("GenerateConsoleCtrlEvent(CTRL_BREAK_EVENT, %d) failed: %v", pid, err)
	}
}

// buildPortScanExe compiles the real cmd/port-scan binary into dir. The test
// drives the shipped CLI, not an in-process stand-in, because the behaviour
// under test (console event -> signal -> context cancel -> exit code) only
// exists in a separate process.
func buildPortScanExe(t *testing.T, dir string) string {
	t.Helper()

	exe := filepath.Join(dir, "port-scan-ctrlbreak-test.exe")
	build := exec.Command("go", "build", "-o", exe, "./cmd/port-scan")
	build.Dir = filepath.Join("..", "..")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("failed to build cmd/port-scan: %v\n%s", err, out)
	}
	return exe
}

// reserveClosedLoopbackPort binds a loopback port and releases it immediately,
// so a later connect to it is refused rather than routed anywhere.
func reserveClosedLoopbackPort(t *testing.T) int {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to reserve a closed loopback port: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	if err := l.Close(); err != nil {
		t.Fatalf("failed to release the reserved loopback port: %v", err)
	}
	return port
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write %s: %v", path, err)
	}
}

func createTestFile(t *testing.T, path string) *os.File {
	t.Helper()

	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("failed to create %s: %v", path, err)
	}
	return f
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Sprintf("<unreadable %s: %v>", path, err)
	}
	return string(data)
}

// exitCodeOf extracts a process exit code from the error exec.Cmd.Wait
// returned. A nil error means the process exited 0.
func exitCodeOf(t *testing.T, err error) int {
	t.Helper()

	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("the scan subprocess failed for a reason other than a non-zero exit: %v", err)
	}
	return exitErr.ExitCode()
}

func singleFileMatching(t *testing.T, dir, pattern, what string) string {
	t.Helper()

	matches, err := filepath.Glob(filepath.Join(dir, pattern))
	if err != nil {
		t.Fatalf("failed to glob %q in %s: %v", pattern, dir, err)
	}
	if len(matches) != 1 {
		t.Fatalf("%s: expected exactly 1 file matching %q in %s, found %d: %v", what, pattern, dir, len(matches), matches)
	}
	return matches[0]
}

func csvDataRowCount(t *testing.T, path string) int {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read %s: %v", path, err)
	}
	var n int
	for i, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		if i == 0 || strings.TrimSpace(line) == "" {
			continue
		}
		n++
	}
	return n
}

// assertHandleReleased renames path aside and back. On Windows a still-open
// handle makes the rename fail with a sharing violation, so a successful
// round trip proves the exited process released the file.
func assertHandleReleased(t *testing.T, path, what string) {
	t.Helper()

	probe := path + ".handle-probe"
	if err := os.Rename(path, probe); err != nil {
		t.Fatalf("%s: %s could not be renamed after the process exited, so a file handle is still open: %v", what, path, err)
	}
	if err := os.Rename(probe, path); err != nil {
		t.Fatalf("%s: failed to restore %s after the handle probe: %v", what, path, err)
	}
}

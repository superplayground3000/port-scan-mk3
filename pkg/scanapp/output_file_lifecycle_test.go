package scanapp

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
	"github.com/xuxiping/port-scan-mk3/pkg/state"
	"github.com/xuxiping/port-scan-mk3/pkg/writer"
)

// This file covers the output-file LIFECYCLE through the public seam (Run) and
// the filesystem only. Every assertion here is cheap on Linux and genuinely
// discriminating on Windows:
//
//   - Windows refuses to rename or delete a file that the process still holds
//     open; POSIX allows it unconditionally. So "os.Rename succeeds after Run
//     returned" is a no-op guard on Linux and a real leaked-handle detector on
//     Windows.
//   - Windows share modes make a second os.OpenFile of a still-open file fail
//     with a sharing violation; POSIX never does. So the -resume append-reopen
//     of the SAME output file is the Windows-only failure mode that the
//     existing (Linux-passing) append tests cannot see.
//
// Nothing here is build-tagged: Linux runs it as a cheap regression guard, and
// the Windows CI job is where it bites.

// outputLifecycleFixture is a deterministic single-host scan: one real local
// listener supplies a guaranteed-open port and the remaining ports are closed
// on loopback, so every probe's outcome is well defined on both platforms.
// 127.0.0.1/32 keeps every address well defined (Windows typically binds only
// 127.0.0.1) and /32 is exempt from broadcast filtering
// (pkg/task/broadcast.go:30-32).
type outputLifecycleFixture struct {
	dir        string
	bucketFile string
	cfg        config.Config
}

func newOutputLifecycleFixture(t *testing.T, closedPorts int) outputLifecycleFixture {
	t.Helper()

	dir := t.TempDir()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start local listener: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			_ = conn.Close()
		}
	}()
	openPort := listener.Addr().(*net.TCPAddr).Port

	cidrFile := filepath.Join(dir, "cidr.csv")
	if err := os.WriteFile(cidrFile, []byte(
		"fab_name,ip,ip_cidr,cidr_name\nfab1,127.0.0.1,127.0.0.1/32,loopback\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// The listening port is first so at least one probe returns an open result
	// (which exercises the opened_results writer too).
	ports := []string{strconv.Itoa(openPort) + "/tcp"}
	for p := 1; p <= closedPorts; p++ {
		ports = append(ports, strconv.Itoa(p)+"/tcp")
	}
	portFile := filepath.Join(dir, "ports.csv")
	if err := os.WriteFile(portFile, []byte(strings.Join(ports, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{
		CIDRFile:           cidrFile,
		PortFile:           portFile,
		Output:             filepath.Join(dir, "out.csv"),
		Timeout:            50 * time.Millisecond,
		BucketRate:         1000,
		BucketCapacity:     1000,
		Workers:            1,
		PressureInterval:   10 * time.Second,
		DisableAPI:         true,
		DisablePreScanPing: true,
		LogLevel:           "error",
	}
	bucketFile := generateBucketFile(t, cfg, filepath.Join(dir, "buckets.json"), "")
	cfg.Resume = bucketFile

	return outputLifecycleFixture{dir: dir, bucketFile: bucketFile, cfg: cfg}
}

// cancelOnFirstDial returns a DialFunc that performs a real dial and cancels the
// run once the first probe has returned. Cancelling on an observed event rather
// than after a fixed sleep makes "at least one result was written, and work is
// still pending" a precondition instead of a bet on machine speed.
func cancelOnFirstDial(cancel context.CancelFunc) DialFunc {
	dialer := &net.Dialer{}
	var once sync.Once
	return func(dialCtx context.Context, network, address string) (net.Conn, error) {
		conn, dialErr := dialer.DialContext(dialCtx, network, address)
		once.Do(cancel)
		return conn, dialErr
	}
}

// assertReleasedHandle proves the process holds no open handle on path: on
// Windows renaming an open file fails (ERROR_SHARING_VIOLATION / access
// denied), on Linux it always succeeds. The file is renamed back so later
// assertions and globs still see the original layout.
func assertReleasedHandle(t *testing.T, what, path string) {
	t.Helper()
	moved := path + ".moved"
	if err := os.Rename(path, moved); err != nil {
		t.Fatalf("%s (%s) could not be renamed after Run returned; a still-open file handle blocks rename on Windows: %v",
			what, path, err)
	}
	if err := os.Rename(moved, path); err != nil {
		t.Fatalf("failed to restore %s after the rename probe: %v", moved, err)
	}
}

func findSingleOutputFile(t *testing.T, pattern string) string {
	t.Helper()
	matches, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatalf("glob failed for %s: %v", pattern, err)
	}
	sort.Strings(matches)
	if len(matches) != 1 {
		t.Fatalf("expected exactly one match for %s, got %d (%v)", pattern, len(matches), matches)
	}
	return matches[0]
}

// countCSVDataRows parses path as CSV and returns the number of rows after the
// header. Parsing (rather than counting newlines) is required because rich
// result fields can carry embedded newlines that encoding/csv quotes.
func countCSVDataRows(t *testing.T, path string) int {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer func() { _ = f.Close() }()
	records, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatalf("read csv %s: %v", path, err)
	}
	if len(records) == 0 {
		return 0
	}
	return len(records) - 1
}

// assertHeaderWrittenExactlyOnce guards the append-reopen contract
// (output_files.go:40-54): a resumed run must continue an existing file, never
// re-emit the canonical header into the middle of it.
func assertHeaderWrittenExactlyOnce(t *testing.T, what, path string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if n := bytes.Count(raw, []byte(writer.CanonicalHeader())); n != 1 {
		t.Fatalf("expected the canonical header exactly once in %s (%s), got %d occurrences:\n%s",
			what, path, n, string(raw))
	}
}

func snapshotTotalTargets(t *testing.T, bucketFile string) int {
	t.Helper()
	snap, err := state.LoadSnapshot(bucketFile)
	if err != nil {
		t.Fatalf("load snapshot %s: %v", bucketFile, err)
	}
	total := 0
	for _, chunk := range snap.Chunks {
		total += chunk.TotalCount
	}
	if total == 0 {
		t.Fatalf("expected a non-empty bucket snapshot at %s", bucketFile)
	}
	return total
}

// TestRun_WhenCompleted_ReleasesOutputFileHandles is the normal-exit half of the
// handle-release contract: once Run has returned, the process must hold no open
// handle on the files it wrote (scan_results, opened_results) or read (the
// bucket/resume snapshot it was pointed at).
func TestRun_WhenCompleted_ReleasesOutputFileHandles(t *testing.T) {
	fx := newOutputLifecycleFixture(t, 7)

	if err := Run(context.Background(), fx.cfg, &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{
		DisableKeyboard: true,
	}); err != nil {
		t.Fatalf("completed run: %v", err)
	}

	scanPath := findSingleOutputFile(t, filepath.Join(fx.dir, "scan_results-*.csv"))
	openPath := findSingleOutputFile(t, filepath.Join(fx.dir, "opened_results-*.csv"))
	if rows := countCSVDataRows(t, scanPath); rows == 0 {
		t.Fatalf("expected the completed run to write result rows to %s", scanPath)
	}

	assertReleasedHandle(t, "scan results file", scanPath)
	assertReleasedHandle(t, "open-only results file", openPath)
	assertReleasedHandle(t, "input bucket snapshot", fx.bucketFile)
}

// TestRun_WhenCanceled_ReleasesOutputFileHandles is the interrupt half, and the
// more valuable one: the cancel path unwinds through defers and an extra
// snapshot write, so it is where a handle is most likely to be leaked. Every
// file the run touched must still be renameable afterwards.
func TestRun_WhenCanceled_ReleasesOutputFileHandles(t *testing.T) {
	fx := newOutputLifecycleFixture(t, 63)
	resumeStateFile := filepath.Join(fx.dir, "resume_state.json")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := Run(ctx, fx.cfg, &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{
		DisableKeyboard: true,
		ResumeStatePath: resumeStateFile,
		Dial:            cancelOnFirstDial(cancel),
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled from the interrupted run, got: %v", err)
	}

	scanPath := findSingleOutputFile(t, filepath.Join(fx.dir, "scan_results-*.csv"))
	openPath := findSingleOutputFile(t, filepath.Join(fx.dir, "opened_results-*.csv"))
	if _, statErr := os.Stat(resumeStateFile); statErr != nil {
		t.Fatalf("expected the canceled run to persist resume state at %s: %v", resumeStateFile, statErr)
	}

	assertReleasedHandle(t, "scan results file", scanPath)
	assertReleasedHandle(t, "open-only results file", openPath)
	assertReleasedHandle(t, "input bucket snapshot", fx.bucketFile)
	assertReleasedHandle(t, "persisted resume state file", resumeStateFile)
}

// TestRun_WhenResumedIntoSameOutputFile_ReopensAndAppendsWithoutDuplicatingHeader
// covers the append-reopen path (output_files.go:55-87) end to end in a single
// process: run 1 is interrupted, run 2 reopens the SAME output files in append
// mode. On Windows a handle leaked by run 1 makes run 2's os.OpenFile fail with
// a sharing violation; on Linux the reopen always succeeds, so only the
// row/header accounting bites there.
func TestRun_WhenResumedIntoSameOutputFile_ReopensAndAppendsWithoutDuplicatingHeader(t *testing.T) {
	fx := newOutputLifecycleFixture(t, 63)
	totalTargets := snapshotTotalTargets(t, fx.bucketFile)

	// Run 1: interrupted, so work is left for the resume to append.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := Run(ctx, fx.cfg, &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{
		DisableKeyboard: true,
		Dial:            cancelOnFirstDial(cancel),
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("run 1: expected context.Canceled, got: %v", err)
	}

	scanPath := findSingleOutputFile(t, filepath.Join(fx.dir, "scan_results-*.csv"))
	openPath := findSingleOutputFile(t, filepath.Join(fx.dir, "opened_results-*.csv"))
	scanRows1 := countCSVDataRows(t, scanPath)
	openRows1 := countCSVDataRows(t, openPath)
	if scanRows1 < 1 {
		t.Fatalf("run 1 wrote no rows to %s; the append case needs rows on both sides", scanPath)
	}
	if scanRows1 >= totalTargets {
		t.Fatalf("run 1 scanned %d of %d targets, so nothing is left to append; the interrupt did not take effect",
			scanRows1, totalTargets)
	}

	// Run 2: -resume reopens the same files in append mode. A leaked handle from
	// run 1 surfaces here as an open failure on Windows.
	if err := Run(context.Background(), fx.cfg, &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{
		DisableKeyboard: true,
	}); err != nil {
		t.Fatalf("run 2 (resume/append into %s): %v", scanPath, err)
	}

	// No second timestamped file was minted: the resume continued the originals.
	if got := findSingleOutputFile(t, filepath.Join(fx.dir, "scan_results-*.csv")); got != scanPath {
		t.Fatalf("resume wrote to %s instead of appending to %s", got, scanPath)
	}
	if got := findSingleOutputFile(t, filepath.Join(fx.dir, "opened_results-*.csv")); got != openPath {
		t.Fatalf("resume wrote to %s instead of appending to %s", got, openPath)
	}

	scanRowsFinal := countCSVDataRows(t, scanPath)
	openRowsFinal := countCSVDataRows(t, openPath)
	scanRows2 := scanRowsFinal - scanRows1
	if scanRows2 < 1 {
		t.Fatalf("run 2 appended no rows to %s (run 1 wrote %d, final has %d)", scanPath, scanRows1, scanRowsFinal)
	}
	// Every target appears exactly once across the two runs: run1 + run2 rows
	// equals the bucket's total, so nothing was lost or duplicated by the reopen.
	if scanRows1+scanRows2 != totalTargets {
		t.Fatalf("expected run1(%d) + run2(%d) = %d rows in %s, got %d",
			scanRows1, scanRows2, totalTargets, scanPath, scanRowsFinal)
	}
	if openRowsFinal < openRows1 {
		t.Fatalf("open-only file lost rows across the resume: had %d, now %d", openRows1, openRowsFinal)
	}

	assertHeaderWrittenExactlyOnce(t, "scan results file", scanPath)
	assertHeaderWrittenExactlyOnce(t, "open-only results file", openPath)

	assertReleasedHandle(t, "scan results file", scanPath)
	assertReleasedHandle(t, "open-only results file", openPath)
}

package scanapp

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
	"github.com/xuxiping/port-scan-mk3/pkg/state"
)

// newRelativeOutputScanConfig builds a scan config whose -output is RELATIVE
// (the shipped default shape, `scan_results.csv`) while every other path the run
// needs is absolute, so the run can be started from an arbitrary working
// directory and only the output location depends on the cwd. The bucket snapshot
// is generated up front and returned with the config.
func newRelativeOutputScanConfig(t *testing.T, inputsDir string) (config.Config, string) {
	t.Helper()
	cidrFile := filepath.Join(inputsDir, "rich.csv")
	bucketsFile := filepath.Join(inputsDir, "buckets.json")

	// A rich PRECHECK_ALLOW_ALL segment over a /24 expands to many targets, so an
	// interrupt on the first dial reliably leaves work pending.
	if err := os.WriteFile(cidrFile, []byte(
		"src_ip,src_network_segment,dst_ip,dst_network_segment,service_label,protocol,port,decision,matched_policy_id,reason\n"+
			"10.9.0.1,10.9.0.0/24,10.9.0.0,10.9.0.0/24,web,tcp,80,accept,P-1,PRECHECK_ALLOW_ALL\n",
	), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{
		CIDRFile:         cidrFile,
		Output:           "scan_results.csv", // relative on purpose: this is the default
		Timeout:          20 * time.Millisecond,
		BucketRate:       1000,
		BucketCapacity:   1000,
		Workers:          1,
		PressureInterval: 5 * time.Second,
		DisableAPI:       true,
		LogLevel:         "error",
	}
	generateBucketFile(t, cfg, bucketsFile, "")
	cfg.Resume = bucketsFile
	return cfg, bucketsFile
}

// cancelOnFirstRefusedDial returns a dial func that cancels the run on its first call
// and always reports a closed port, so a run stops with work still pending after
// at least one result has been written.
func cancelOnFirstRefusedDial(cancel context.CancelFunc) DialFunc {
	var once sync.Once
	return func(context.Context, string, string) (net.Conn, error) {
		once.Do(cancel)
		return nil, errors.New("forced dial failure")
	}
}

// TestRun_WhenResumedFromDifferentWorkingDirectory_AppendsToOriginalOutputs is
// the issue #61 acceptance test: a scan started with the RELATIVE default
// -output from directory A and interrupted must, when the same snapshot is
// resumed from directory B, keep appending to the ORIGINAL files in A. Before
// the fix the snapshot recorded a cwd-relative path, so the resume re-resolved
// it against B and silently created a SECOND output set there (duplicate header,
// split rows).
func TestRun_WhenResumedFromDifferentWorkingDirectory_AppendsToOriginalOutputs(t *testing.T) {
	base := t.TempDir()
	dirA := filepath.Join(base, "dir-a")
	dirB := filepath.Join(base, "dir-b")
	inputsDir := filepath.Join(base, "inputs")
	for _, dir := range []string{dirA, dirB, inputsDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	cfg, bucketsFile := newRelativeOutputScanConfig(t, inputsDir)

	// Run 1 from directory A: interrupted with work still pending.
	t.Chdir(dirA)
	ctx, cancel := context.WithCancel(context.Background())
	if err := Run(ctx, cfg, &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{
		DisableKeyboard: true,
		Dial:            cancelOnFirstRefusedDial(cancel),
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("run 1 (dir A): expected context.Canceled, got: %v", err)
	}
	cancel()

	scanPath := mustFindOne(t, filepath.Join(dirA, "scan_results-*.csv"))
	openPath := mustFindOne(t, filepath.Join(dirA, "opened_results-*.csv"))
	_, rows1 := readCSVRows(t, scanPath)
	if len(rows1) < 1 {
		t.Fatalf("run 1 wrote no rows; the test cannot discriminate")
	}
	snap, err := state.LoadSnapshot(bucketsFile)
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	total := snap.Chunks[0].TotalCount

	// Run 2 from directory B: same snapshot, different cwd.
	t.Chdir(dirB)
	if err := Run(context.Background(), cfg, &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{
		DisableKeyboard: true,
		Dial:            func(context.Context, string, string) (net.Conn, error) { return nil, errors.New("closed") },
	}); err != nil {
		t.Fatalf("run 2 (dir B, resume): %v", err)
	}

	// No second output set anywhere near directory B.
	assertNoBatchOutputs(t, dirB)

	// Directory A still holds exactly one pair, now complete and single-headed.
	if got := mustFindOne(t, filepath.Join(dirA, "scan_results-*.csv")); got != scanPath {
		t.Fatalf("scan output moved: got %s want %s", got, scanPath)
	}
	if got := mustFindOne(t, filepath.Join(dirA, "opened_results-*.csv")); got != openPath {
		t.Fatalf("open output moved: got %s want %s", got, openPath)
	}
	header, rowsFinal := readCSVRows(t, scanPath)
	if len(header) == 0 || header[0] != "ip" {
		t.Fatalf("expected header row, got %v", header)
	}
	if len(rowsFinal) != total {
		t.Fatalf("expected %d continuous rows in %s (no lost/dupe), got %d (run 1 wrote %d)",
			total, scanPath, len(rowsFinal), len(rows1))
	}
	raw, err := os.ReadFile(scanPath)
	if err != nil {
		t.Fatal(err)
	}
	if n := bytes.Count(raw, []byte("ip,ip_cidr,port,status")); n != 1 {
		t.Fatalf("expected exactly one header line in %s, got %d", scanPath, n)
	}

	// Issue #61's last acceptance bullet: "the snapshot continues to reference the
	// intended files". A run that finishes cleanly does not re-save the snapshot
	// (persistResumeSnapshot returns early when nothing is resumable), so what is
	// on disk here is exactly what run 1 recorded from directory A. Before the fix
	// that was a bare cwd-relative name - the very string that made this dir-B
	// resume open a second output set - so this assertion discriminates on the
	// recorded value itself, not merely on which files happen to exist.
	after, err := state.LoadSnapshot(bucketsFile)
	if err != nil {
		t.Fatalf("load snapshot after the cross-directory resume: %v", err)
	}
	if after.Output == nil {
		t.Fatal("expected the snapshot to still record its output paths")
	}
	if after.Output.ScanPath != scanPath {
		t.Fatalf("snapshot scan_path must still reference %s, got %q", scanPath, after.Output.ScanPath)
	}
	if after.Output.OpenPath != openPath {
		t.Fatalf("snapshot open_path must still reference %s, got %q", openPath, after.Output.OpenPath)
	}
}

// TestRun_WhenSnapshotHasRelativeOutputPaths_ResumesInPlaceAndRecordsAbsolute is
// the documented compatibility rule for snapshots written BEFORE the #61 fix:
// their relative output paths are resolved against the process working directory
// (exactly what the old build did implicitly), so a same-directory resume keeps
// appending to the same files; the snapshot this run saves records the resolved
// ABSOLUTE paths, which repairs the ambiguity from that point on.
func TestRun_WhenSnapshotHasRelativeOutputPaths_ResumesInPlaceAndRecordsAbsolute(t *testing.T) {
	base := t.TempDir()
	workDir := filepath.Join(base, "work")
	inputsDir := filepath.Join(base, "inputs")
	for _, dir := range []string{workDir, inputsDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	cfg, bucketsFile := newRelativeOutputScanConfig(t, inputsDir)

	t.Chdir(workDir)
	ctx, cancel := context.WithCancel(context.Background())
	if err := Run(ctx, cfg, &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{
		DisableKeyboard: true,
		Dial:            cancelOnFirstRefusedDial(cancel),
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("run 1: expected context.Canceled, got: %v", err)
	}
	cancel()

	scanPath := mustFindOne(t, filepath.Join(workDir, "scan_results-*.csv"))
	openPath := mustFindOne(t, filepath.Join(workDir, "opened_results-*.csv"))
	_, rows1 := readCSVRows(t, scanPath)

	// Downgrade the snapshot to the legacy (pre-fix) shape: bare relative names.
	snap, err := state.LoadSnapshot(bucketsFile)
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	snap.Output = &state.OutputState{
		ScanPath: filepath.Base(scanPath),
		OpenPath: filepath.Base(openPath),
	}
	if err := state.SaveSnapshot(bucketsFile, snap); err != nil {
		t.Fatalf("save legacy snapshot: %v", err)
	}

	// Resume from the SAME directory, interrupted again so a snapshot is saved.
	ctx2, cancel2 := context.WithCancel(context.Background())
	if err := Run(ctx2, cfg, &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{
		DisableKeyboard: true,
		Dial:            cancelOnFirstRefusedDial(cancel2),
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("run 2 (legacy resume): expected context.Canceled, got: %v", err)
	}
	cancel2()

	// Compatibility: still exactly one pair, appended in place, one header.
	if got := mustFindOne(t, filepath.Join(workDir, "scan_results-*.csv")); got != scanPath {
		t.Fatalf("legacy resume moved the scan output: got %s want %s", got, scanPath)
	}
	_, rows2 := readCSVRows(t, scanPath)
	if len(rows2) <= len(rows1) {
		t.Fatalf("expected the legacy resume to append rows: had %d, now %d", len(rows1), len(rows2))
	}
	raw, err := os.ReadFile(scanPath)
	if err != nil {
		t.Fatal(err)
	}
	if n := bytes.Count(raw, []byte("ip,ip_cidr,port,status")); n != 1 {
		t.Fatalf("expected exactly one header line in %s, got %d", scanPath, n)
	}

	// Repair: the newly saved snapshot must carry absolute paths.
	upgraded, err := state.LoadSnapshot(bucketsFile)
	if err != nil {
		t.Fatalf("load upgraded snapshot: %v", err)
	}
	if upgraded.Output == nil {
		t.Fatal("expected the resumed run to record output paths")
	}
	if !filepath.IsAbs(upgraded.Output.ScanPath) || upgraded.Output.ScanPath != scanPath {
		t.Fatalf("expected snapshot scan_path upgraded to %s, got %q", scanPath, upgraded.Output.ScanPath)
	}
	if !filepath.IsAbs(upgraded.Output.OpenPath) || upgraded.Output.OpenPath != openPath {
		t.Fatalf("expected snapshot open_path upgraded to %s, got %q", openPath, upgraded.Output.OpenPath)
	}
}

// TestResolveBatchOutputPaths_WhenOutputIsRelative_ReturnsAbsolutePaths pins the
// chosen rule at its single point of origin: batch output paths are made
// absolute (and cleaned) before they are opened or persisted, so nothing
// downstream can re-resolve them against a different working directory (#61).
func TestResolveBatchOutputPaths_WhenOutputIsRelative_ReturnsAbsolutePaths(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	now := time.Date(2026, 3, 2, 1, 30, 45, 0, time.UTC)

	paths, err := resolveBatchOutputPaths("scan_results.csv", now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for name, got := range map[string]string{
		"scanPath":        paths.scanPath,
		"openPath":        paths.openPath,
		"unreachablePath": paths.unreachablePath,
	} {
		if !filepath.IsAbs(got) {
			t.Fatalf("%s must be absolute, got %q", name, got)
		}
		if filepath.Dir(got) != filepath.Clean(dir) {
			t.Fatalf("%s must live in the cwd %s, got %q", name, dir, got)
		}
	}
	if base := filepath.Base(paths.scanPath); base != "scan_results-20260302T013045Z.csv" {
		t.Fatalf("unexpected scan file name %q", base)
	}
}

// TestResolveBatchOutputPaths_WhenOutputIsNestedRelative_ReturnsAbsolutePaths
// covers a relative -output that carries a subdirectory: the directory is still
// created and the returned paths are absolute.
func TestResolveBatchOutputPaths_WhenOutputIsNestedRelative_ReturnsAbsolutePaths(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	now := time.Date(2026, 3, 2, 1, 30, 45, 0, time.UTC)

	paths, err := resolveBatchOutputPaths(filepath.Join("out", "nested", "scan_results.csv"), now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantDir := filepath.Join(filepath.Clean(dir), "out", "nested")
	if !filepath.IsAbs(paths.scanPath) || filepath.Dir(paths.scanPath) != wantDir {
		t.Fatalf("expected absolute path under %s, got %q", wantDir, paths.scanPath)
	}
	if _, err := os.Stat(wantDir); err != nil {
		t.Fatalf("expected the nested output directory to be created: %v", err)
	}
}

// TestResolvePersistedOutputPaths covers the snapshot-compatibility helper
// directly: absolute recorded paths pass through cleaned and unchanged (the
// documented "existing absolute paths remain compatible" rule), while a legacy
// relative path is anchored to the process working directory.
func TestResolvePersistedOutputPaths(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	cwd := filepath.Clean(dir)

	absScan := filepath.Join(cwd, "kept", "scan_results-x.csv")
	absOpen := filepath.Join(cwd, "kept", "opened_results-x.csv")
	got, err := resolvePersistedOutputPaths(state.OutputState{ScanPath: absScan, OpenPath: absOpen})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ScanPath != absScan || got.OpenPath != absOpen {
		t.Fatalf("absolute recorded paths must pass through unchanged: got %+v", got)
	}

	got, err = resolvePersistedOutputPaths(state.OutputState{
		ScanPath: "scan_results-x.csv",
		OpenPath: filepath.Join("sub", "opened_results-x.csv"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ScanPath != filepath.Join(cwd, "scan_results-x.csv") {
		t.Fatalf("relative scan_path must anchor to the cwd, got %q", got.ScanPath)
	}
	if got.OpenPath != filepath.Join(cwd, "sub", "opened_results-x.csv") {
		t.Fatalf("relative open_path must anchor to the cwd, got %q", got.OpenPath)
	}

	// Degenerate snapshot: an Output block with empty names carries no location
	// to anchor. It must stay empty rather than become the working directory
	// itself, so the subsequent open fails on the empty name instead of trying to
	// write a CSV over a directory.
	got, err = resolvePersistedOutputPaths(state.OutputState{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ScanPath != "" || got.OpenPath != "" {
		t.Fatalf("empty recorded paths must stay empty, got %+v", got)
	}
}

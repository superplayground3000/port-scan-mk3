package scanapp

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
	"github.com/xuxiping/port-scan-mk3/pkg/state"
)

// TestRun_WhenResumed_EmitsBucketParsePhaseLogs is the Phase 5 (Job B)
// observability guarantee: the pre-scan runtime rebuild logs
// bucket_parse_start -> bucket_parse_progress(>=1) -> bucket_parse_complete ->
// scan_start, and the existing per-result scan_progress / scan_completion
// events still appear afterward (result_aggregator.go is untouched).
func TestRun_WhenResumed_EmitsBucketParsePhaseLogs(t *testing.T) {
	cfg, _, _ := newInterruptibleScanConfig(t)
	cfg.LogLevel = "info" // bucket_parse_* / scan_* are info-level structured events

	var stderr bytes.Buffer
	// Every dial fails "closed" so the scan runs to completion (no cancel), which
	// exercises the full log sequence including scan_completion.
	dial := func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("connection refused")
	}
	if err := Run(context.Background(), testScanConfigurationFromLegacy(t, cfg), &bytes.Buffer{}, &stderr, RunOptions{
		DisableKeyboard:  true,
		Dial:             dial,
		ProgressInterval: 1, // tick every chunk/result so progress lines always emit
	}); err != nil {
		t.Fatalf("resume run: %v", err)
	}

	log := stderr.String()
	idx := func(sub string) int { return strings.Index(log, sub) }

	iStart := idx("bucket_parse_start")
	iProg := idx("bucket_parse_progress")
	iComplete := idx("bucket_parse_complete")
	iScanStart := idx("scan_start")
	iScanProg := idx("scan_progress")
	iScanDone := idx("scan_completion")

	for name, i := range map[string]int{
		"bucket_parse_start":    iStart,
		"bucket_parse_progress": iProg,
		"bucket_parse_complete": iComplete,
		"scan_start":            iScanStart,
		"scan_progress":         iScanProg,
		"scan_completion":       iScanDone,
	} {
		if i < 0 {
			t.Fatalf("expected %s in stderr, got:\n%s", name, log)
		}
	}

	// Phase-parse sequence, then the scan begins, then per-result events follow.
	if !(iStart < iProg && iProg < iComplete && iComplete < iScanStart) {
		t.Fatalf("bucket parse phase out of order: start=%d progress=%d complete=%d scan_start=%d\n%s",
			iStart, iProg, iComplete, iScanStart, log)
	}
	if !(iScanStart < iScanProg && iScanStart < iScanDone) {
		t.Fatalf("scan events must follow scan_start: scan_start=%d scan_progress=%d scan_completion=%d\n%s",
			iScanStart, iScanProg, iScanDone, log)
	}
}

// richCancelBucketConfig builds a basic-mode config whose single /29 chunk
// expands to several targets, so a scan can be interrupted with work still
// pending. Returns the config (with Resume set) and the temp dir.
func newInterruptibleScanConfig(t *testing.T) (config.Config, string, string) {
	t.Helper()
	tmp := t.TempDir()
	cidrFile := filepath.Join(tmp, "rich.csv")
	outFile := filepath.Join(tmp, "out.csv")
	bucketsFile := filepath.Join(tmp, "buckets.json")

	// A rich PRECHECK_ALLOW_ALL segment over a /24 expands to many targets, so an
	// interrupt on the first dial reliably leaves work pending (dispatch is still
	// enqueuing when cancel is observed).
	if err := os.WriteFile(cidrFile, []byte(
		"src_ip,src_network_segment,dst_ip,dst_network_segment,service_label,protocol,port,decision,matched_policy_id,reason\n"+
			"10.9.0.1,10.9.0.0/24,10.9.0.0,10.9.0.0/24,web,tcp,80,accept,P-1,PRECHECK_ALLOW_ALL\n",
	), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{
		CIDRFile:         cidrFile,
		Output:           outFile,
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
	return cfg, tmp, bucketsFile
}

// readCSVRows returns the header and data rows of a CSV file.
func readCSVRows(t *testing.T, path string) ([]string, [][]string) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer func() { _ = f.Close() }()
	r := csv.NewReader(f)
	records, err := r.ReadAll()
	if err != nil {
		t.Fatalf("read csv %s: %v", path, err)
	}
	if len(records) == 0 {
		return nil, nil
	}
	return records[0], records[1:]
}

// TestRun_WhenCanceledMidScan_DeliversRowsToFinalPath is the Phase 3 durability
// guarantee: rows written before a graceful Ctrl+C land in the FINAL scan_results
// path (not a stranded .tmp), and the persisted snapshot records that path with
// advanced cursors so -resume can continue.
func TestRun_WhenCanceledMidScan_DeliversRowsToFinalPath(t *testing.T) {
	cfg, tmp, bucketsFile := newInterruptibleScanConfig(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var once sync.Once
	dial := func(context.Context, string, string) (net.Conn, error) {
		once.Do(cancel)
		return nil, errors.New("forced dial failure")
	}

	err := Run(ctx, testScanConfigurationFromLegacy(t, cfg), &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{DisableKeyboard: true, Dial: dial})
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled from interrupted scan, got: %v", err)
	}

	// The final scan_results path must exist and hold the already-scanned rows.
	scanPath := mustFindOne(t, filepath.Join(tmp, "scan_results-*.csv"))
	if _, statErr := os.Stat(scanPath + ".tmp"); !os.IsNotExist(statErr) {
		t.Fatalf("expected no stranded .tmp file, stat err: %v", statErr)
	}
	header, rows := readCSVRows(t, scanPath)
	if len(header) == 0 || header[0] != "ip" {
		t.Fatalf("expected header row, got %v", header)
	}
	if len(rows) < 1 {
		t.Fatalf("expected at least one scanned row in final file, got %d", len(rows))
	}

	// The snapshot must record the output path and advanced (but incomplete) cursors.
	snap, err := state.LoadSnapshot(bucketsFile)
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	if snap.Output == nil || snap.Output.ScanPath != scanPath {
		t.Fatalf("expected snapshot Output.ScanPath=%s, got %+v", scanPath, snap.Output)
	}
	if len(snap.Chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(snap.Chunks))
	}
	ch := snap.Chunks[0]
	if ch.NextIndex < 1 || ch.NextIndex >= ch.TotalCount {
		t.Fatalf("expected advanced-but-incomplete cursor, got NextIndex=%d TotalCount=%d", ch.NextIndex, ch.TotalCount)
	}
}

// TestRun_WhenResumed_AppendsToSameOutputFile is the Phase 4 append guarantee:
// a -resume continues writing into the SAME scan_results file the first run
// created, producing one continuous file with exactly one header and no
// lost/duplicated rows.
func TestRun_WhenResumed_AppendsToSameOutputFile(t *testing.T) {
	cfg, tmp, bucketsFile := newInterruptibleScanConfig(t)

	// Run 1: interrupt mid-scan.
	ctx, cancel := context.WithCancel(context.Background())
	var once sync.Once
	dial := func(context.Context, string, string) (net.Conn, error) {
		once.Do(cancel)
		return nil, errors.New("forced dial failure")
	}
	if err := Run(ctx, testScanConfigurationFromLegacy(t, cfg), &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{DisableKeyboard: true, Dial: dial}); !errors.Is(err, context.Canceled) {
		t.Fatalf("run 1: expected context.Canceled, got: %v", err)
	}
	cancel()

	scanPath := mustFindOne(t, filepath.Join(tmp, "scan_results-*.csv"))
	_, rows1 := readCSVRows(t, scanPath)
	total := 0
	if snap, err := state.LoadSnapshot(bucketsFile); err != nil {
		t.Fatalf("load snapshot: %v", err)
	} else {
		total = snap.Chunks[0].TotalCount
	}

	// Run 2: resume to completion; must append to the SAME file.
	if err := Run(context.Background(), testScanConfigurationFromLegacy(t, cfg), &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{
		DisableKeyboard: true,
		Dial:            func(context.Context, string, string) (net.Conn, error) { return nil, errors.New("forced dial failure") },
	}); err != nil {
		t.Fatalf("run 2 (resume): %v", err)
	}

	// Still exactly one scan_results file.
	matches, _ := filepath.Glob(filepath.Join(tmp, "scan_results-*.csv"))
	if len(matches) != 1 {
		t.Fatalf("expected exactly one scan_results file after resume, got %v", matches)
	}
	header, rowsFinal := readCSVRows(t, scanPath)
	if header[0] != "ip" {
		t.Fatalf("expected header, got %v", header)
	}
	if len(rowsFinal) != total {
		t.Fatalf("expected %d continuous rows (no lost/dupe), got %d (run1 had %d)", total, len(rowsFinal), len(rows1))
	}
	// Exactly one header line: raw file must contain the header string once.
	raw, err := os.ReadFile(scanPath)
	if err != nil {
		t.Fatal(err)
	}
	if n := bytes.Count(raw, []byte("ip,ip_cidr,port,status")); n != 1 {
		t.Fatalf("expected exactly one header line, got %d", n)
	}
}

// TestRun_WhenResumedWithPriorOutputDeleted_RecreatesWithHeader covers the
// edge where the prior output file was removed before resume: the run must
// recreate it with a header and not error.
func TestRun_WhenResumedWithPriorOutputDeleted_RecreatesWithHeader(t *testing.T) {
	cfg, _, bucketsFile := newInterruptibleScanConfig(t)

	ctx, cancel := context.WithCancel(context.Background())
	var once sync.Once
	dial := func(context.Context, string, string) (net.Conn, error) {
		once.Do(cancel)
		return nil, errors.New("forced dial failure")
	}
	if err := Run(ctx, testScanConfigurationFromLegacy(t, cfg), &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{DisableKeyboard: true, Dial: dial}); !errors.Is(err, context.Canceled) {
		t.Fatalf("run 1: expected context.Canceled, got: %v", err)
	}
	cancel()

	snap, err := state.LoadSnapshot(bucketsFile)
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	scanPath := snap.Output.ScanPath
	openPath := snap.Output.OpenPath
	if err := os.Remove(scanPath); err != nil {
		t.Fatalf("remove scan file: %v", err)
	}
	if err := os.Remove(openPath); err != nil {
		t.Fatalf("remove open file: %v", err)
	}

	if err := Run(context.Background(), testScanConfigurationFromLegacy(t, cfg), &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{
		DisableKeyboard: true,
		Dial:            func(context.Context, string, string) (net.Conn, error) { return nil, errors.New("forced dial failure") },
	}); err != nil {
		t.Fatalf("run 2 (resume with deleted output): %v", err)
	}

	header, rows := readCSVRows(t, scanPath)
	if len(header) == 0 || header[0] != "ip" {
		t.Fatalf("expected recreated file to have a header, got %v", header)
	}
	if len(rows) < 1 {
		t.Fatalf("expected recreated file to hold the remaining rows, got %d", len(rows))
	}
}

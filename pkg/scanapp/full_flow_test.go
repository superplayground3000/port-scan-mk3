package scanapp

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
	"github.com/xuxiping/port-scan-mk3/pkg/state"
	"github.com/xuxiping/port-scan-mk3/pkg/writer"
)

// TestFullFlow_PrePingGenerateBucketsScanResume_ProducesOneContinuousResultSet
// is the platform-neutral replacement for the Docker e2e suite on Windows: it
// drives the whole three-step product pipeline end to end through the exported
// package seam only —
//
//	RunPrePing -> GenerateBuckets -> Run (first pass) -> Run (resume pass)
//
// — against a real loopback TCP listener, with no Docker, no external network,
// and no fixed-duration sleeps. Until this test existed the complete flow had
// only ever run inside `e2e/run_e2e.sh` (Docker Compose, Linux-only), so on
// Windows it had never executed at all.
//
// Every step is observed only through its public entry point and the files it
// produces, which is exactly the handoff a CLI user gets:
//
//   - pre-ping writes unreachable_results-<ts>.csv and prints its path
//     (pre_ping.go:80-85), which generate-buckets consumes as -unreachable-file.
//   - generate-buckets writes the bucket snapshot to cfg.BucketsOut
//     (bucketgen.go:115-125), which scan consumes as -resume.
//   - scan's first pass is interrupted, so it persists a resume snapshot that
//     records the output paths (resume_manager.go:25-40, scan.go:288), which the
//     resume pass reopens in append mode (scan.go:95-106).
func TestFullFlow_PrePingGenerateBucketsScanResume_ProducesOneContinuousResultSet(t *testing.T) {
	// A real listener on an OS-assigned loopback port is the only "service" in
	// play; 127.0.0.1/32 is exempt from broadcast filtering (task/broadcast.go:26-29)
	// so the /32 expands to exactly the one host we control.
	openPort := fullFlowStartListener(t)
	closedPorts := fullFlowReserveClosedPorts(t, 15)
	ports := append(append([]int{}, closedPorts...), openPort)
	totalTasks := len(ports)

	tmp := t.TempDir()
	cidrFile := filepath.Join(tmp, "cidr.csv")
	portFile := filepath.Join(tmp, "ports.csv")
	outFile := filepath.Join(tmp, "out.csv")
	bucketsPath := filepath.Join(tmp, "buckets.json")
	resumeStatePath := filepath.Join(tmp, "resume_state.json")

	// 127.0.0.2 is never dialed: pre-ping reports it unreachable, so
	// generate-buckets subtracts it and no chunk ever contains it. This keeps the
	// blocklist handoff meaningful without depending on 127.0.0.2 being bound,
	// which does not hold on Windows.
	const unreachableIP = "127.0.0.2"
	if err := os.WriteFile(cidrFile, []byte(
		"fab_name,ip,ip_cidr,cidr_name\n"+
			"fab1,127.0.0.1,127.0.0.1/32,loopback\n"+
			"fab1,"+unreachableIP+","+unreachableIP+"/32,loopback-down\n",
	), 0o644); err != nil {
		t.Fatalf("write cidr file: %v", err)
	}
	portLines := make([]string, 0, len(ports))
	for _, p := range ports {
		portLines = append(portLines, strconv.Itoa(p)+"/tcp")
	}
	if err := os.WriteFile(portFile, []byte(strings.Join(portLines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write port file: %v", err)
	}

	baseCfg := config.Config{
		CIDRFile:           cidrFile,
		PortFile:           portFile,
		Output:             outFile,
		Timeout:            200 * time.Millisecond,
		Delay:              0,
		BucketRate:         1000,
		BucketCapacity:     1000,
		Workers:            1,
		PressureInterval:   5 * time.Second,
		DisableAPI:         true,
		LogLevel:           "error",
		ProgressInterval:   100,
		PreScanPingTimeout: 100 * time.Millisecond,
	}

	// ---------------------------------------------------------------- step 1.
	// RunPrePing. The checker is injected through the documented public seam
	// (pre_ping.go:22-24, RunOptions.ReachabilityChecker) so the step is decided
	// by our fixture rather than by whether ICMP works on the CI runner.
	prepCfg := baseCfg
	var prepStdout, prepStderr bytes.Buffer
	prepErr := RunPrePing(context.Background(), mustPrePingConfig(t, config.PrePingValues{
		CIDRFile:         prepCfg.CIDRFile,
		CIDRIPCol:        "ip",
		CIDRIPCidrCol:    "ip_cidr",
		Output:           prepCfg.Output,
		Workers:          prepCfg.Workers,
		PingTimeout:      prepCfg.PreScanPingTimeout,
		ProgressInterval: prepCfg.ProgressInterval,
	}), &prepStdout, &prepStderr, RunOptions{
		ReachabilityChecker: fullFlowChecker{unreachable: map[string]bool{unreachableIP: true}},
	})
	if prepErr != nil {
		t.Fatalf("step 1 (pre-ping) failed: %v\nstderr: %s", prepErr, prepStderr.String())
	}

	// pre_ping.go:84 prints the resolved path so the next step can chain off it.
	unreachablePath := strings.TrimSpace(prepStdout.String())
	if unreachablePath == "" {
		t.Fatal("step 1 (pre-ping): expected the unreachable output path on stdout, got nothing")
	}
	if got := mustFindOne(t, filepath.Join(tmp, "unreachable_results-*.csv")); got != unreachablePath {
		t.Fatalf("step 1 (pre-ping): stdout path %q does not name the produced file %q", unreachablePath, got)
	}
	// Schema is writer.UnreachableWriter's fixed contract (unreachable_writer.go:29-41);
	// the status/reason values come from pre_scan_ping.go:199-212 and pre_ping.go:55.
	unreachHeader, unreachRows := readCSVRows(t, unreachablePath)
	if len(unreachHeader) == 0 || unreachHeader[0] != "ip" || unreachHeader[2] != "status" {
		t.Fatalf("step 1 (pre-ping): unexpected unreachable header %v", unreachHeader)
	}
	if len(unreachRows) != 1 {
		t.Fatalf("step 1 (pre-ping): expected exactly 1 unreachable row, got %d: %v", len(unreachRows), unreachRows)
	}
	if unreachRows[0][0] != unreachableIP || unreachRows[0][2] != "unreachable" {
		t.Fatalf("step 1 (pre-ping): expected %s marked unreachable, got %v", unreachableIP, unreachRows[0])
	}

	// ---------------------------------------------------------------- step 2.
	// GenerateBuckets consumes step 1's file as the blocklist and writes the
	// snapshot scan will resume from.
	genCfg := mustGenerateBucketsConfig(t, config.GenerateBucketsValues{
		CIDRFile:         baseCfg.CIDRFile,
		CIDRIPCol:        "ip",
		CIDRIPCidrCol:    "ip_cidr",
		PortFile:         baseCfg.PortFile,
		BlocklistFile:    unreachablePath,
		SnapshotOutput:   bucketsPath,
		Workers:          baseCfg.Workers,
		ProgressInterval: baseCfg.ProgressInterval,
	})
	var genStderr bytes.Buffer
	if err := GenerateBuckets(context.Background(), genCfg, &genStderr, GenerateBucketsOptions{}); err != nil {
		t.Fatalf("step 2 (generate-buckets) failed: %v\nstderr: %s", err, genStderr.String())
	}

	bucketSnap, err := state.LoadSnapshot(bucketsPath)
	if err != nil {
		t.Fatalf("step 2 (generate-buckets): load snapshot: %v", err)
	}
	// bucketgen.go:115-122 stamps enabled=true and carries the blocklist through.
	if !bucketSnap.PreScanPing.Enabled {
		t.Fatal("step 2 (generate-buckets): expected pre_scan_ping.enabled=true in the snapshot")
	}
	const unreachableIPU32 = 0x7F000002 // 127.0.0.2
	if len(bucketSnap.PreScanPing.UnreachableIPv4U32) != 1 || bucketSnap.PreScanPing.UnreachableIPv4U32[0] != unreachableIPU32 {
		t.Fatalf("step 2 (generate-buckets): expected blocklist [%d] for %s, got %v",
			unreachableIPU32, unreachableIP, bucketSnap.PreScanPing.UnreachableIPv4U32)
	}
	// The blocked /32 group is dropped entirely (group_builder.go:221-224), so a
	// single chunk covering 127.0.0.1 x all ports remains.
	if len(bucketSnap.Chunks) != 1 {
		t.Fatalf("step 2 (generate-buckets): expected 1 chunk (the blocked /32 group is dropped), got %d: %+v",
			len(bucketSnap.Chunks), bucketSnap.Chunks)
	}
	chunk := bucketSnap.Chunks[0]
	if chunk.CIDR != "127.0.0.1/32" || chunk.TotalCount != totalTasks {
		t.Fatalf("step 2 (generate-buckets): expected chunk 127.0.0.1/32 with total_count=%d, got cidr=%s total_count=%d",
			totalTasks, chunk.CIDR, chunk.TotalCount)
	}
	if bucketSnap.Output != nil {
		t.Fatalf("step 2 (generate-buckets): a fresh bucket snapshot must not record output paths, got %+v", bucketSnap.Output)
	}
	bucketBytesBefore, err := os.ReadFile(bucketsPath)
	if err != nil {
		t.Fatalf("read bucket snapshot: %v", err)
	}

	// ---------------------------------------------------------------- step 3.
	// Scan, first pass, interrupted mid-flight. Dials are real TCP dials against
	// the loopback listener; the cancel fires once the first of them has
	// returned, which leaves work pending (scan.go:232-235 stops dispatching on
	// runCtx) and therefore forces the resume snapshot to be written.
	scanCfg := baseCfg
	scanCfg.Resume = bucketsPath

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dialer := &net.Dialer{}
	var once sync.Once
	interruptingDial := func(dialCtx context.Context, network, addr string) (net.Conn, error) {
		conn, dialErr := dialer.DialContext(dialCtx, network, addr)
		once.Do(cancel)
		return conn, dialErr
	}

	// ResumeStatePath keeps the resume snapshot in its own file (resume_path.go:9-11)
	// so the step-2 artifact stays intact and each artifact can be asserted apart.
	firstOpts := RunOptions{
		DisableKeyboard: true,
		Dial:            interruptingDial,
		ResumeStatePath: resumeStatePath,
	}
	var firstStderr bytes.Buffer
	firstErr := Run(ctx, testScanConfigurationFromLegacy(t, scanCfg), &bytes.Buffer{}, &firstStderr, firstOpts)
	if !errors.Is(firstErr, context.Canceled) {
		t.Fatalf("step 3 (scan, first pass): expected context.Canceled from the interrupt, got: %v\nstderr: %s",
			firstErr, firstStderr.String())
	}
	cancel()

	bucketBytesAfter, err := os.ReadFile(bucketsPath)
	if err != nil {
		t.Fatalf("re-read bucket snapshot: %v", err)
	}
	if !bytes.Equal(bucketBytesBefore, bucketBytesAfter) {
		t.Fatal("step 3 (scan, first pass): the step-2 bucket snapshot must not be rewritten when ResumeStatePath is set")
	}

	// The resume snapshot is the artifact the resume pass consumes. It must name
	// the files already written (scan.go:107, resume_manager.go:32-36).
	firstSnap, err := state.LoadSnapshot(resumeStatePath)
	if err != nil {
		t.Fatalf("step 3 (scan, first pass): load resume state: %v", err)
	}
	if firstSnap.Output == nil {
		t.Fatal("step 3 (scan, first pass): resume state must record the output paths for the append-reopen")
	}
	scanPath := firstSnap.Output.ScanPath
	openPath := firstSnap.Output.OpenPath
	if got := mustFindOne(t, filepath.Join(tmp, "scan_results-*.csv")); got != scanPath {
		t.Fatalf("step 3 (scan, first pass): recorded scan path %q is not the produced file %q", scanPath, got)
	}
	if got := mustFindOne(t, filepath.Join(tmp, "opened_results-*.csv")); got != openPath {
		t.Fatalf("step 3 (scan, first pass): recorded open path %q is not the produced file %q", openPath, got)
	}

	firstHeader, firstRows := readCSVRows(t, scanPath)
	if strings.Join(firstHeader, ",") != writer.CanonicalHeader() {
		t.Fatalf("step 3 (scan, first pass): header %q != canonical %q", strings.Join(firstHeader, ","), writer.CanonicalHeader())
	}
	if len(firstRows) < 1 || len(firstRows) >= totalTasks {
		t.Fatalf("step 3 (scan, first pass): expected a partial result set in 1..%d rows, got %d",
			totalTasks-1, len(firstRows))
	}

	// Resume-state consistency: every result both writes a row and bumps the
	// tracker in the same loop iteration (scan.go:270-275 then :275
	// applyScanResult -> result_aggregator.go:35), so the persisted
	// scanned_count must equal the rows on disk, and the cursor must sit at an
	// advanced-but-incomplete position.
	if len(firstSnap.Chunks) != 1 {
		t.Fatalf("step 3 (scan, first pass): expected 1 chunk in resume state, got %d", len(firstSnap.Chunks))
	}
	firstChunk := firstSnap.Chunks[0]
	if firstChunk.ScannedCount != len(firstRows) {
		t.Fatalf("step 3 (scan, first pass): resume scanned_count=%d disagrees with %d rows written",
			firstChunk.ScannedCount, len(firstRows))
	}
	if firstChunk.NextIndex < firstChunk.ScannedCount || firstChunk.NextIndex >= firstChunk.TotalCount {
		t.Fatalf("step 3 (scan, first pass): expected scanned_count <= next_index < total_count, got %d/%d/%d",
			firstChunk.ScannedCount, firstChunk.NextIndex, firstChunk.TotalCount)
	}
	if firstChunk.TotalCount != totalTasks {
		t.Fatalf("step 3 (scan, first pass): resume total_count=%d, want %d", firstChunk.TotalCount, totalTasks)
	}
	if firstChunk.Status != "scanning" {
		t.Fatalf("step 3 (scan, first pass): expected chunk status \"scanning\", got %q", firstChunk.Status)
	}

	// ---------------------------------------------------------------- step 4.
	// Scan, resume pass. It reopens the SAME files in append mode, which is the
	// Windows-sensitive part: a leaked handle from the first pass would fail the
	// reopen with a sharing violation (output_files.go:66-72).
	resumeCfg := scanCfg
	resumeCfg.Resume = resumeStatePath
	var resumeStderr bytes.Buffer
	resumeErr := Run(context.Background(), testScanConfigurationFromLegacy(t, resumeCfg), &bytes.Buffer{}, &resumeStderr, RunOptions{
		DisableKeyboard: true,
		Dial:            dialer.DialContext,
		ResumeStatePath: resumeStatePath,
	})
	if resumeErr != nil {
		t.Fatalf("step 4 (scan, resume pass) failed: %v\nstderr: %s", resumeErr, resumeStderr.String())
	}

	// Still exactly one batch: the resume appended, it did not mint new files.
	for _, pattern := range []string{"scan_results-*.csv", "opened_results-*.csv"} {
		matches, globErr := filepath.Glob(filepath.Join(tmp, pattern))
		if globErr != nil {
			t.Fatalf("glob %s: %v", pattern, globErr)
		}
		if len(matches) != 1 {
			t.Fatalf("step 4 (scan, resume pass): expected exactly one %s after resume, got %v", pattern, matches)
		}
	}

	finalHeader, finalRows := readCSVRows(t, scanPath)
	if strings.Join(finalHeader, ",") != writer.CanonicalHeader() {
		t.Fatalf("final scan_results header %q != canonical %q", strings.Join(finalHeader, ","), writer.CanonicalHeader())
	}
	raw, err := os.ReadFile(scanPath)
	if err != nil {
		t.Fatalf("read final scan_results: %v", err)
	}
	if n := bytes.Count(raw, []byte(writer.CanonicalHeader())); n != 1 {
		t.Fatalf("expected the canonical header exactly once in the appended file, got %d", n)
	}

	// No loss and no duplication across the two passes: the first pass's rows are
	// still there byte-for-byte as the file's prefix, and the total is exactly
	// the chunk's total_count.
	if len(finalRows) != totalTasks {
		t.Fatalf("expected %d result rows across both passes (first pass wrote %d), got %d",
			totalTasks, len(firstRows), len(finalRows))
	}
	for i, want := range firstRows {
		if strings.Join(finalRows[i], ",") != strings.Join(want, ",") {
			t.Fatalf("resume rewrote row %d: first pass had %v, final file has %v", i, want, finalRows[i])
		}
	}

	// One row per port, correct status per port, and nothing for the blocked IP.
	seen := make(map[int]string, len(finalRows))
	for _, row := range finalRows {
		if row[0] != "127.0.0.1" {
			t.Fatalf("unexpected ip %q in results (the blocked %s must never be scanned): %v", row[0], unreachableIP, row)
		}
		if row[1] != "127.0.0.1/32" {
			t.Fatalf("unexpected ip_cidr %q in results: %v", row[1], row)
		}
		port, convErr := strconv.Atoi(row[2])
		if convErr != nil {
			t.Fatalf("unparsable port %q in results: %v", row[2], row)
		}
		if prev, dup := seen[port]; dup {
			t.Fatalf("port %d appears twice in the result set (previous status %q)", port, prev)
		}
		seen[port] = row[3]
	}
	for _, p := range ports {
		status, ok := seen[p]
		if !ok {
			t.Fatalf("port %d is missing from the result set", p)
		}
		// scanner.ScanTCP maps a successful dial to "open" and a refused/timed-out
		// dial to "close"/"close(timeout)" (pkg/scanner doc comment).
		if p == openPort {
			if status != "open" {
				t.Fatalf("listening port %d reported %q, want \"open\"", p, status)
			}
			continue
		}
		if !strings.HasPrefix(status, "close") {
			t.Fatalf("non-listening port %d reported %q, want a close* status", p, status)
		}
	}

	// opened_results carries only the open port (writer.OpenOnlyWriter filter).
	openHeader, openRows := readCSVRows(t, openPath)
	if strings.Join(openHeader, ",") != writer.CanonicalHeader() {
		t.Fatalf("opened_results header %q != canonical %q", strings.Join(openHeader, ","), writer.CanonicalHeader())
	}
	if len(openRows) != 1 {
		t.Fatalf("expected exactly 1 opened_results row (the listener), got %d: %v", len(openRows), openRows)
	}
	if openRows[0][2] != strconv.Itoa(openPort) || openRows[0][3] != "open" {
		t.Fatalf("expected opened_results to hold port %d as open, got %v", openPort, openRows[0])
	}
}

// fullFlowChecker is a ReachabilityChecker fixture: every IP is reachable except
// the ones listed. It replaces the OS ping subprocess so the flow test is
// deterministic on any runner (pre_ping.go:94-99 takes the injected checker when
// one is supplied).
type fullFlowChecker struct {
	unreachable map[string]bool
}

func (c fullFlowChecker) Check(_ context.Context, ip string, _ time.Duration) ReachabilityResult {
	if c.unreachable[ip] {
		return ReachabilityResult{IP: ip, Reachable: false, FailureText: "fixture: host down"}
	}
	return ReachabilityResult{IP: ip, Reachable: true}
}

// fullFlowStartListener starts a real loopback TCP listener that accepts and
// immediately closes connections, and returns its OS-assigned port.
func fullFlowStartListener(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen on loopback: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, acceptErr := ln.Accept()
			if acceptErr != nil {
				return
			}
			_ = conn.Close()
		}
	}()
	return fullFlowPortOf(t, ln.Addr())
}

// fullFlowReserveClosedPorts binds n loopback ports, then releases them. The
// bind proves nothing was listening there, and after the close a dial to them
// is refused — a deterministic "closed port" on both Linux and Windows without
// hardcoding well-known port numbers.
func fullFlowReserveClosedPorts(t *testing.T, n int) []int {
	t.Helper()
	listeners := make([]net.Listener, 0, n)
	ports := make([]int, 0, n)
	for i := 0; i < n; i++ {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("reserve loopback port %d: %v", i, err)
		}
		listeners = append(listeners, ln)
		ports = append(ports, fullFlowPortOf(t, ln.Addr()))
	}
	for _, ln := range listeners {
		if err := ln.Close(); err != nil {
			t.Fatalf("release reserved port: %v", err)
		}
	}
	return ports
}

func fullFlowPortOf(t *testing.T, addr net.Addr) int {
	t.Helper()
	_, portStr, err := net.SplitHostPort(addr.String())
	if err != nil {
		t.Fatalf("split host/port %q: %v", addr.String(), err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("parse port %q: %v", portStr, err)
	}
	return port
}

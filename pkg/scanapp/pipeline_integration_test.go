package scanapp

import (
	"bytes"
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
	"github.com/xuxiping/port-scan-mk3/pkg/state"
)

// richPipelineCSV is the shared rich-input fixture for the three-step pipeline
// tests: three targets in one /24, all on port 443. 127.0.0.1 is dialable
// (open), 127.0.0.2 refuses (closed), 127.0.0.3 is the one pre-ping marks
// unreachable so bucket-gen subtracts it before scan ever sees it.
const richPipelineCSV = "src_ip,src_network_segment,dst_ip,dst_network_segment,service_label,protocol,port,decision,matched_policy_id,reason\n" +
	"192.168.1.10,192.168.1.0/24,127.0.0.1,127.0.0.0/24,web,tcp,443,accept,P-1,allow\n" +
	"192.168.1.11,192.168.1.0/24,127.0.0.2,127.0.0.0/24,ssh,tcp,443,accept,P-2,allow\n" +
	"192.168.1.12,192.168.1.0/24,127.0.0.3,127.0.0.0/24,db,tcp,443,accept,P-3,allow\n"

// pipelineBaseConfig returns a config wired for the in-process pipeline: a rich
// CIDR CSV at cidrFile, no PortFile (rich mode), fast timeouts, pressure API
// disabled. Callers set the resume path when the scan step starts.
func pipelineBaseConfig(t *testing.T, cidrFile, outFile string) scanConfigFixture {
	t.Helper()
	if err := os.WriteFile(cidrFile, []byte(richPipelineCSV), 0o644); err != nil {
		t.Fatalf("write rich csv: %v", err)
	}
	return scanConfigFixture{
		CIDRFile:       cidrFile,
		Output:         outFile,
		Timeout:        20 * time.Millisecond,
		BucketRate:     100,
		BucketCapacity: 100,
		Workers:        2,
		Pressure:       pressureConfigFixture{Disabled: true},
		LogLevel:       "error",
	}
}

// dialOpenFor returns a DialFunc that reports the given IP as open (returns a
// live stub connection) and every other address as closed (dial error). It keys
// on the "ip:" prefix so ports do not matter.
func dialOpenFor(openIP string) DialFunc {
	return func(_ context.Context, _ string, addr string) (net.Conn, error) {
		if strings.HasPrefix(addr, openIP+":") {
			return stubConn{}, nil
		}
		// A closed port is a *refusal* from the remote end, so the fixture must
		// carry ECONNREFUSED: since issue #62 the scanner only reports "close"
		// for an errno that proves the target answered.
		return nil, dialErrnoFailure(syscall.ECONNREFUSED)
	}
}

// assertLineStatus asserts that the CSV line mentioning ip also contains the
// expected status token (e.g. "open" / "close").
func assertLineStatus(t *testing.T, data []byte, ip, wantStatus string) {
	t.Helper()
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, ip) {
			if !strings.Contains(line, wantStatus) {
				t.Fatalf("expected target %s row to have status %q, got line: %q", ip, wantStatus, line)
			}
			return
		}
	}
	t.Fatalf("expected a scan row for target %s, none found in:\n%s", ip, string(data))
}

// TestPipeline_PrePingToBucketsToScan exercises the full durable-file hand-off
// chain in process: RunPrePing writes an unreachable CSV, GenerateBuckets reads
// it as a blocklist and writes a resume snapshot, and Run consumes the snapshot.
// It asserts the unreachable target is absent from the final scan output, that
// rich metadata survives end to end, and that open/closed states are correct.
func TestPipeline_PrePingToBucketsToScan(t *testing.T) {
	tmp := t.TempDir()
	cidrFile := filepath.Join(tmp, "rich.csv")
	outFile := filepath.Join(tmp, "out.csv")
	bucketsFile := filepath.Join(tmp, "buckets.json")

	baseCfg := pipelineBaseConfig(t, cidrFile, outFile)

	// Step 1 — pre-ping: mark 127.0.0.3 unreachable, others reachable.
	checker := &fakePreScanChecker{
		results: map[string]ReachabilityResult{
			"127.0.0.3": {IP: "127.0.0.3", Reachable: false, FailureText: "timeout"},
		},
	}
	var prePingStdout bytes.Buffer
	if err := RunPrePing(context.Background(), mustPrePingConfig(t, config.PrePingValues{
		CIDRFile:         baseCfg.CIDRFile,
		CIDRIPCol:        "ip",
		CIDRIPCidrCol:    "ip_cidr",
		Output:           baseCfg.Output,
		Workers:          baseCfg.Workers,
		PingTimeout:      300 * time.Millisecond,
		ProgressInterval: 1,
	}), &prePingStdout, &bytes.Buffer{}, RunOptions{
		ReachabilityChecker: checker,
	}); err != nil {
		t.Fatalf("pre-ping step failed: %v", err)
	}
	unreachableFile := strings.TrimSpace(prePingStdout.String())
	if unreachableFile == "" {
		t.Fatal("pre-ping did not print a resolved unreachable-file path to stdout")
	}
	unreachableData, err := os.ReadFile(unreachableFile)
	if err != nil {
		t.Fatalf("read pre-ping output %s: %v", unreachableFile, err)
	}
	if !bytes.Contains(unreachableData, []byte("127.0.0.3")) {
		t.Fatalf("expected pre-ping to record 127.0.0.3 as unreachable, got:\n%s", unreachableData)
	}

	// Step 2 — generate-buckets: subtract the blocklist, write the snapshot.
	generateBucketFile(t, baseCfg, bucketsFile, unreachableFile)

	// Step 3 — scan: consume the snapshot; 127.0.0.1 open, 127.0.0.2 closed.
	scanCfg := baseCfg
	scanCfg.Resume = bucketsFile
	spy := &failIfCalledChecker{t: t}
	if err := Run(context.Background(), scanConfigurationFromFixture(t, scanCfg), &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{
		DisableKeyboard:     true,
		Dial:                dialOpenFor("127.0.0.1"),
		ReachabilityChecker: spy,
	}); err != nil {
		t.Fatalf("scan step failed: %v", err)
	}
	if spy.called {
		t.Fatal("scan constructed/invoked a reachability checker; the resume path must never ping")
	}

	scanPath := mustFindOne(t, filepath.Join(tmp, "scan_results-*.csv"))
	scanData, err := os.ReadFile(scanPath)
	if err != nil {
		t.Fatalf("read scan results: %v", err)
	}

	// The unreachable target (and its whole row) must be excluded.
	if bytes.Contains(scanData, []byte("127.0.0.3")) {
		t.Fatalf("unreachable target 127.0.0.3 must be absent from scan output, got:\n%s", scanData)
	}
	// Rich metadata must survive the two file hand-offs.
	for _, want := range []string{"web", "accept", "P-1", "192.168.1.10", "192.168.1.0/24"} {
		if !bytes.Contains(scanData, []byte(want)) {
			t.Fatalf("expected rich metadata %q in scan output, got:\n%s", want, scanData)
		}
	}
	// Open/closed states must be correct against the fake dialer.
	assertLineStatus(t, scanData, "127.0.0.1", "open")
	assertLineStatus(t, scanData, "127.0.0.2", "close")
}

// TestPipeline_NoPrePing_ScansAll skips pre-ping entirely: GenerateBuckets with no
// blocklist must produce a snapshot covering every target, and scan must consume
// it without ever constructing a reachability checker.
func TestPipeline_NoPrePing_ScansAll(t *testing.T) {
	tmp := t.TempDir()
	cidrFile := filepath.Join(tmp, "rich.csv")
	outFile := filepath.Join(tmp, "out.csv")
	bucketsFile := filepath.Join(tmp, "buckets.json")

	baseCfg := pipelineBaseConfig(t, cidrFile, outFile)

	// No pre-ping, no -unreachable-file → empty blocklist.
	generateBucketFile(t, baseCfg, bucketsFile, "")

	scanCfg := baseCfg
	scanCfg.Resume = bucketsFile
	spy := &failIfCalledChecker{t: t}
	if err := Run(context.Background(), scanConfigurationFromFixture(t, scanCfg), &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{
		DisableKeyboard:     true,
		Dial:                dialOpenFor("127.0.0.1"),
		ReachabilityChecker: spy,
	}); err != nil {
		t.Fatalf("scan step failed: %v", err)
	}
	if spy.called {
		t.Fatal("scan constructed/invoked a reachability checker; scan must never ping")
	}

	scanPath := mustFindOne(t, filepath.Join(tmp, "scan_results-*.csv"))
	scanData, err := os.ReadFile(scanPath)
	if err != nil {
		t.Fatalf("read scan results: %v", err)
	}
	for _, ip := range []string{"127.0.0.1", "127.0.0.2", "127.0.0.3"} {
		if !bytes.Contains(scanData, []byte(ip)) {
			t.Fatalf("expected target %s in scan output when pre-ping is skipped, got:\n%s", ip, scanData)
		}
	}
}

// TestPipeline_TamperedTotalCount_Rejected guards the F2 invariant: mutating a
// chunk's total_count in the snapshot JSON must make scan reject the resume file
// with the incompatible-target-set error rather than silently scanning.
func TestPipeline_TamperedTotalCount_Rejected(t *testing.T) {
	tmp := t.TempDir()
	cidrFile := filepath.Join(tmp, "rich.csv")
	outFile := filepath.Join(tmp, "out.csv")
	bucketsFile := filepath.Join(tmp, "buckets.json")

	baseCfg := pipelineBaseConfig(t, cidrFile, outFile)
	generateBucketFile(t, baseCfg, bucketsFile, "")

	// Tamper: bump a chunk's total_count so it no longer matches what scan
	// re-derives from the same records.
	snap, err := state.LoadSnapshot(bucketsFile)
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	if len(snap.Chunks) == 0 {
		t.Fatal("expected at least one chunk in the generated snapshot")
	}
	snap.Chunks[0].TotalCount += 99
	if err := state.SaveSnapshot(bucketsFile, snap); err != nil {
		t.Fatalf("save tampered snapshot: %v", err)
	}

	scanCfg := baseCfg
	scanCfg.Resume = bucketsFile
	err = Run(context.Background(), scanConfigurationFromFixture(t, scanCfg), &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{
		DisableKeyboard: true,
		Dial:            dialOpenFor("127.0.0.1"),
	})
	if err == nil {
		t.Fatal("expected scan to reject a snapshot with a tampered total_count, got nil")
	}
	if !strings.Contains(err.Error(), "total_count") {
		t.Fatalf("expected a total_count mismatch error, got: %v", err)
	}
}

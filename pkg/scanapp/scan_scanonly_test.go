package scanapp

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
)

// failIfCalledChecker is a ReachabilityChecker spy that fails the test if any of
// its methods is invoked. Decision B makes scan a pure scanner that constructs
// no checker and never pings; wiring this spy through RunOptions and asserting it
// is never touched proves the "never pings" guarantee holds by construction.
type failIfCalledChecker struct {
	t      *testing.T
	called bool
}

func (f *failIfCalledChecker) Check(_ context.Context, ip string, _ time.Duration) ReachabilityResult {
	f.t.Helper()
	f.called = true
	f.t.Errorf("scan constructed/invoked a reachability checker (Check %s); scan must never ping", ip)
	return ReachabilityResult{IP: ip, Reachable: true}
}

func (f *failIfCalledChecker) CheckDetailed(_ context.Context, ip string, _ time.Duration) (ReachabilityResult, error) {
	f.t.Helper()
	f.called = true
	f.t.Errorf("scan constructed/invoked a reachability checker (CheckDetailed %s); scan must never ping", ip)
	return ReachabilityResult{IP: ip, Reachable: true}, nil
}

// generateBucketFile runs GenerateBuckets (T4) to produce a resume snapshot at
// bucketsOut over cfg's inputs minus the optional unreachableFile blocklist. It
// guarantees the total_count invariant scan re-derives, so scan accepts the
// snapshot unchanged. Returns bucketsOut for convenience.
func generateBucketFile(t *testing.T, cfg config.Config, bucketsOut, unreachableFile string) string {
	t.Helper()
	genCfg := cfg
	genCfg.Resume = ""
	genCfg.BucketsOut = bucketsOut
	genCfg.UnreachableFile = unreachableFile
	if genCfg.ProgressInterval <= 0 {
		genCfg.ProgressInterval = 100
	}
	if err := GenerateBuckets(context.Background(), genCfg, &bytes.Buffer{}, GenerateBucketsOptions{}); err != nil {
		t.Fatalf("generate buckets: %v", err)
	}
	return bucketsOut
}

func assertNoBatchOutputs(t *testing.T, dir string) {
	t.Helper()
	for _, pattern := range []string{"scan_results-*.csv", "opened_results-*.csv", "unreachable_results-*.csv"} {
		matches, err := filepath.Glob(filepath.Join(dir, pattern))
		if err != nil {
			t.Fatalf("glob %s: %v", pattern, err)
		}
		if len(matches) != 0 {
			t.Fatalf("expected no %s files, got %v", pattern, matches)
		}
	}
}

// TestFinalizeUnreachableResults_OpenSuccessAndError covers the retained helper
// directly: an empty row set produces a valid header-only file, and an
// unwritable destination surfaces the open error. (Run no longer calls this
// under decision B; pre-ping owns it, and it now lives in pre_ping.go.)
func TestFinalizeUnreachableResults_OpenSuccessAndError(t *testing.T) {
	tmp := t.TempDir()
	good := filepath.Join(tmp, "unreachable.csv")
	if err := finalizeUnreachableResults(good, nil); err != nil {
		t.Fatalf("expected header-only finalize to succeed, got %v", err)
	}
	if _, statErr := os.Stat(good); statErr != nil {
		t.Fatalf("expected finalized file, got %v", statErr)
	}

	bad := filepath.Join(tmp, "does-not-exist", "unreachable.csv")
	if err := finalizeUnreachableResults(bad, nil); err == nil {
		t.Fatal("expected open error for an unwritable destination path")
	}
}

// TestRun_RequiresResume asserts scan refuses to run without a bucket file and
// creates no output artifacts (no fresh-build fallback under decision B).
func TestRun_RequiresResume(t *testing.T) {
	tmp := t.TempDir()
	cidrFile := filepath.Join(tmp, "cidr.csv")
	portFile := filepath.Join(tmp, "ports.csv")
	outFile := filepath.Join(tmp, "out.csv")

	if err := os.WriteFile(cidrFile, []byte("fab_name,ip,ip_cidr,cidr_name\nfab1,127.0.0.1,127.0.0.1/32,loopback\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portFile, []byte("1/tcp\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{
		CIDRFile:         cidrFile,
		PortFile:         portFile,
		Output:           outFile,
		Timeout:          20 * time.Millisecond,
		BucketRate:       100,
		BucketCapacity:   100,
		Workers:          1,
		PressureInterval: 5 * time.Second,
		DisableAPI:       true,
		LogLevel:         "error",
		// Resume intentionally empty.
	}

	err := Run(context.Background(), cfg, &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{DisableKeyboard: true})
	if err == nil {
		t.Fatal("expected error when -resume is missing, got nil")
	}
	if !errors.Is(err, errScanRequiresResume) {
		t.Fatalf("expected errScanRequiresResume, got: %v", err)
	}
	assertNoBatchOutputs(t, tmp)
}

// TestRun_NeverConstructsChecker asserts that, given a valid bucket snapshot,
// scan never constructs or invokes a reachability checker (guarantee by
// construction). A spy checker injected via RunOptions must remain untouched.
func TestRun_NeverConstructsChecker(t *testing.T) {
	tmp := t.TempDir()
	cidrFile := filepath.Join(tmp, "cidr.csv")
	portFile := filepath.Join(tmp, "ports.csv")
	outFile := filepath.Join(tmp, "out.csv")
	bucketsFile := filepath.Join(tmp, "buckets.json")

	if err := os.WriteFile(cidrFile, []byte("fab_name,ip,ip_cidr,cidr_name\nfab1,127.0.0.1,127.0.0.1/32,loopback\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portFile, []byte("1/tcp\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{
		CIDRFile:         cidrFile,
		PortFile:         portFile,
		Output:           outFile,
		Timeout:          20 * time.Millisecond,
		BucketRate:       100,
		BucketCapacity:   100,
		Workers:          1,
		PressureInterval: 5 * time.Second,
		DisableAPI:       true,
		LogLevel:         "error",
	}
	generateBucketFile(t, cfg, bucketsFile, "")
	cfg.Resume = bucketsFile

	spy := &failIfCalledChecker{t: t}
	if err := Run(context.Background(), cfg, &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{
		DisableKeyboard:     true,
		ReachabilityChecker: spy,
		Dial: func(context.Context, string, string) (net.Conn, error) {
			return nil, dialErrnoFailure(syscall.ECONNREFUSED)
		},
	}); err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if spy.called {
		t.Fatal("reachability checker was invoked; scan must never ping")
	}

	scanPath := mustFindOne(t, filepath.Join(tmp, "scan_results-*.csv"))
	scanData, err := os.ReadFile(scanPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(scanData, []byte("127.0.0.1,127.0.0.1/32,1,close")) {
		t.Fatalf("expected scanned row for reachable target, got %s", string(scanData))
	}
}

// TestRun_ProducesEnrichedRowsFromRichCSVAndSnapshot asserts scan emits rich
// metadata (service_label, decision, matched_policy_id, src_ip,
// src_network_segment) and that the reachable predicate comes from the snapshot
// blocklist (a blocklisted IP is absent from the scan output).
func TestRun_ProducesEnrichedRowsFromRichCSVAndSnapshot(t *testing.T) {
	tmp := t.TempDir()
	cidrFile := filepath.Join(tmp, "rich.csv")
	outFile := filepath.Join(tmp, "out.csv")
	bucketsFile := filepath.Join(tmp, "buckets.json")
	unreachableFile := filepath.Join(tmp, "unreachable.csv")

	if err := os.WriteFile(cidrFile, []byte(
		"src_ip,src_network_segment,dst_ip,dst_network_segment,service_label,protocol,port,decision,matched_policy_id,reason\n"+
			"10.0.0.10,10.0.0.0/24,127.0.0.1,127.0.0.0/24,web,tcp,443,accept,P-1,allow\n"+
			"10.1.0.11,10.1.0.0/24,10.0.0.9,10.0.0.0/24,svc-b,tcp,443,accept,P-2,allow\n",
	), 0o644); err != nil {
		t.Fatal(err)
	}
	// Blocklist 10.0.0.9 via the unreachable-writer "ip" column schema.
	if err := os.WriteFile(unreachableFile, []byte("ip,ip_cidr,status\n10.0.0.9,10.0.0.0/24,unreachable\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{
		CIDRFile:         cidrFile,
		Output:           outFile,
		Timeout:          20 * time.Millisecond,
		BucketRate:       100,
		BucketCapacity:   100,
		Workers:          1,
		PressureInterval: 5 * time.Second,
		DisableAPI:       true,
		LogLevel:         "error",
	}
	generateBucketFile(t, cfg, bucketsFile, unreachableFile)
	cfg.Resume = bucketsFile

	if err := Run(context.Background(), cfg, &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{
		DisableKeyboard: true,
		Dial:            func(context.Context, string, string) (net.Conn, error) { return nil, errors.New("forced dial failure") },
	}); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	scanPath := mustFindOne(t, filepath.Join(tmp, "scan_results-*.csv"))
	scanData, err := os.ReadFile(scanPath)
	if err != nil {
		t.Fatal(err)
	}
	out := string(scanData)
	if bytes.Contains(scanData, []byte("10.0.0.9")) {
		t.Fatalf("blocklisted target must be absent from scan output, got %s", out)
	}
	for _, want := range []string{"127.0.0.1", "web", "accept", "P-1", "10.0.0.10", "10.0.0.0/24"} {
		if !bytes.Contains(scanData, []byte(want)) {
			t.Fatalf("expected rich metadata %q in scan output, got %s", want, out)
		}
	}
}

// TestRun_BasicResumeWithoutPortFile_Succeeds asserts that scan does not require
// -port-file for basic (non-rich) input when resuming: the bucket's chunks
// already carry the ports, so -port-file is genuinely unused at scan time.
// Design §6 calls scan's -port-file "normally ignored"; requiring it for basic
// input contradicts that. generate-buckets (run first, with ports) fills the
// chunks; scan then needs no ports.
func TestRun_BasicResumeWithoutPortFile_Succeeds(t *testing.T) {
	tmp := t.TempDir()
	cidrFile := filepath.Join(tmp, "cidr.csv")
	portFile := filepath.Join(tmp, "ports.csv")
	outFile := filepath.Join(tmp, "out.csv")
	bucketsFile := filepath.Join(tmp, "buckets.json")

	if err := os.WriteFile(cidrFile, []byte("fab_name,ip,ip_cidr,cidr_name\nfab1,127.0.0.1,127.0.0.1/32,loopback\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portFile, []byte("1/tcp\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{
		CIDRFile:         cidrFile,
		PortFile:         portFile, // needed to BUILD the bucket (basic mode)
		Output:           outFile,
		Timeout:          20 * time.Millisecond,
		BucketRate:       100,
		BucketCapacity:   100,
		Workers:          1,
		PressureInterval: 5 * time.Second,
		DisableAPI:       true,
		LogLevel:         "error",
	}
	generateBucketFile(t, cfg, bucketsFile, "")

	// Now scan WITHOUT a port file — the chunks carry the ports.
	cfg.Resume = bucketsFile
	cfg.PortFile = ""
	if err := Run(context.Background(), cfg, &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{
		DisableKeyboard: true,
		Dial:            func(context.Context, string, string) (net.Conn, error) { return nil, errors.New("forced dial failure") },
	}); err != nil {
		t.Fatalf("basic-mode scan with -resume and no -port-file must succeed, got: %v", err)
	}
	scanPath := mustFindOne(t, filepath.Join(tmp, "scan_results-*.csv"))
	if _, err := os.Stat(scanPath); err != nil {
		t.Fatalf("expected scan results, got %v", err)
	}
}

// TestRun_DoesNotWriteUnreachableCSV asserts scan no longer emits an
// unreachable_results CSV (that artifact belongs to pre-ping under decision B).
func TestRun_DoesNotWriteUnreachableCSV(t *testing.T) {
	tmp := t.TempDir()
	cidrFile := filepath.Join(tmp, "cidr.csv")
	portFile := filepath.Join(tmp, "ports.csv")
	outFile := filepath.Join(tmp, "out.csv")
	bucketsFile := filepath.Join(tmp, "buckets.json")

	if err := os.WriteFile(cidrFile, []byte("fab_name,ip,ip_cidr,cidr_name\nfab1,127.0.0.1,127.0.0.1/32,loopback\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portFile, []byte("1/tcp\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{
		CIDRFile:         cidrFile,
		PortFile:         portFile,
		Output:           outFile,
		Timeout:          20 * time.Millisecond,
		BucketRate:       100,
		BucketCapacity:   100,
		Workers:          1,
		PressureInterval: 5 * time.Second,
		DisableAPI:       true,
		LogLevel:         "error",
	}
	generateBucketFile(t, cfg, bucketsFile, "")
	cfg.Resume = bucketsFile

	if err := Run(context.Background(), cfg, &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{
		DisableKeyboard: true,
		Dial:            func(context.Context, string, string) (net.Conn, error) { return nil, errors.New("forced dial failure") },
	}); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	matches, err := filepath.Glob(filepath.Join(tmp, "unreachable_results-*.csv"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("scan must not write unreachable_results CSV, got %v", matches)
	}
	// Scan output itself must still be produced.
	_ = mustFindOne(t, filepath.Join(tmp, "scan_results-*.csv"))
}

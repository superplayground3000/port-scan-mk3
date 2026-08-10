package main

import (
	"bytes"
	"context"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
	"github.com/xuxiping/port-scan-mk3/pkg/scanapp"
)

// mustGenerateBucketSnapshot runs the generate-buckets subcommand to produce the
// resume bucket snapshot that scan now requires (-resume). It writes buckets.json
// into dir and returns its path. portFile is optional (rich CSVs need none).
func mustGenerateBucketSnapshot(t *testing.T, dir, cidrFile, portFile string) string {
	t.Helper()
	snapshot := filepath.Join(dir, "buckets.json")
	args := []string{
		"generate-buckets",
		"-cidr-file", cidrFile,
		"-buckets-out", snapshot,
		"-workers", "1",
	}
	if portFile != "" {
		args = append(args, "-port-file", portFile)
	}
	stderr := &bytes.Buffer{}
	if code := runMain(args, &bytes.Buffer{}, stderr); code != 0 {
		t.Fatalf("generate-buckets failed exit=%d stderr=%s", code, stderr.String())
	}
	return snapshot
}

func TestRunMain_ScanWritesCSV(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()

	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	openPort, _ := strconv.Atoi(portStr)

	tmp := t.TempDir()
	cidrFile := filepath.Join(tmp, "cidr.csv")
	portFile := filepath.Join(tmp, "ports.csv")
	outFile := filepath.Join(tmp, "out.csv")

	if err := os.WriteFile(cidrFile, []byte("fab_name,ip,ip_cidr,cidr_name\nfab1,127.0.0.1,127.0.0.1/32,loopback\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portFile, []byte(strconv.Itoa(openPort)+"/tcp\n1/tcp\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	snapshot := mustGenerateBucketSnapshot(t, tmp, cidrFile, portFile)

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	code := runMain([]string{
		"scan",
		"-cidr-file", cidrFile,
		"-port-file", portFile,
		"-resume", snapshot,
		"-output", outFile,
		"-workers", "1",
		"-delay", "0ms",
		"-timeout", "100ms",
		"-disable-api=true",
	}, stdout, stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d stderr=%s", code, stderr.String())
	}

	scanOutputPath := mustFindOneMain(t, filepath.Join(tmp, "scan_results-*.csv"))
	data, err := os.ReadFile(scanOutputPath)
	if err != nil {
		t.Fatalf("failed to read output csv: %v", err)
	}
	out := string(data)
	if !strings.Contains(out, "ip,ip_cidr,port,status,response_time_ms,fab_name,cidr_name") {
		t.Fatalf("missing header: %s", out)
	}
	if !strings.Contains(out, "127.0.0.1,127.0.0.1/32,"+strconv.Itoa(openPort)+",open") {
		t.Fatalf("missing open row: %s", out)
	}
	if !strings.Contains(out, "127.0.0.1,127.0.0.1/32,1,close") && !strings.Contains(out, "127.0.0.1,127.0.0.1/32,1,close(timeout)") {
		t.Fatalf("missing close row: %s", out)
	}
}

func TestScanApp_CancelSavesResumeState(t *testing.T) {
	tmp := t.TempDir()
	cidrFile := filepath.Join(tmp, "cidr.csv")
	portFile := filepath.Join(tmp, "ports.csv")
	outFile := filepath.Join(tmp, "out.csv")
	resumeFile := filepath.Join(tmp, "resume_state.json")

	if err := os.WriteFile(cidrFile, []byte("fab_name,ip,ip_cidr,cidr_name\nfab1,127.0.0.1/24,127.0.0.1/24,loopback\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portFile, []byte("1/tcp\n2/tcp\n3/tcp\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Scan now requires a bucket snapshot via -resume, so build one first (this
	// used to rely on scan building fresh chunks inline).
	bucketsOut := filepath.Join(tmp, "buckets.json")
	genCfg, err := config.NewGenerateBuckets(config.GenerateBucketsValues{
		CIDRFile:       cidrFile,
		CIDRIPCol:      "ip",
		CIDRIPCidrCol:  "ip_cidr",
		PortFile:       portFile,
		SnapshotOutput: bucketsOut,
		Workers:        1,
	})
	if err != nil {
		t.Fatalf("new bucket configuration: %v", err)
	}
	if err := scanapp.GenerateBuckets(context.Background(), genCfg, &bytes.Buffer{}, scanapp.GenerateBucketsOptions{}); err != nil {
		t.Fatalf("generate buckets: %v", err)
	}

	cfg, err := config.NewScan(config.ScanValues{
		CIDRFile:       cidrFile,
		CIDRIPCol:      "ip",
		CIDRIPCidrCol:  "ip_cidr",
		PortFile:       portFile,
		Output:         outFile,
		DialTimeout:    50 * time.Millisecond,
		DispatchDelay:  5 * time.Millisecond,
		BucketRate:     1,
		BucketCapacity: 1,
		Workers:        1,
		Pressure:       config.PressureDisabled(),
		ResumeInput:    bucketsOut,
		LogLevel:       "error",
		Format:         "human",
	})
	if err != nil {
		t.Fatalf("NewScan() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()

	err = scanapp.Run(ctx, cfg, &bytes.Buffer{}, &bytes.Buffer{}, scanapp.RunOptions{ResumeStatePath: resumeFile})
	if err == nil {
		t.Fatal("expected cancellation error")
	}
	if _, statErr := os.Stat(resumeFile); statErr != nil {
		t.Fatalf("expected resume state file, got err=%v", statErr)
	}
}

func TestRunMain_ScanWritesOpenedResultsCSV(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()

	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	openPort, _ := strconv.Atoi(portStr)

	tmp := t.TempDir()
	cidrFile := filepath.Join(tmp, "cidr.csv")
	portFile := filepath.Join(tmp, "ports.csv")
	outFile := filepath.Join(tmp, "scan_results.csv")

	if err := os.WriteFile(cidrFile, []byte("fab_name,ip,ip_cidr,cidr_name\nfab1,127.0.0.1,127.0.0.1/32,loopback\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portFile, []byte(strconv.Itoa(openPort)+"/tcp\n1/tcp\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	snapshot := mustGenerateBucketSnapshot(t, tmp, cidrFile, portFile)

	code := runMain([]string{
		"scan",
		"-cidr-file", cidrFile,
		"-port-file", portFile,
		"-resume", snapshot,
		"-output", outFile,
		"-workers", "1",
		"-delay", "0ms",
		"-timeout", "100ms",
		"-disable-api=true",
	}, &bytes.Buffer{}, &bytes.Buffer{})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}

	openOnlyFile := mustFindOneMain(t, filepath.Join(tmp, "opened_results-*.csv"))
	data, err := os.ReadFile(openOnlyFile)
	if err != nil {
		t.Fatalf("failed to read opened_results.csv: %v", err)
	}
	out := string(data)
	if !strings.Contains(out, "ip,ip_cidr,port,status,response_time_ms,fab_name,cidr_name") {
		t.Fatalf("missing header: %s", out)
	}
	if !strings.Contains(out, ",open,") {
		t.Fatalf("expected open row: %s", out)
	}
	if strings.Contains(out, ",close,") || strings.Contains(out, "close(timeout)") {
		t.Fatalf("opened_results.csv must contain open rows only: %s", out)
	}
}

// TestRunMain_Scan_RejectsDisablePreScanPingFlag replaces the former
// "...ScanContractStillSucceeds" test. Its original intent — that scan tolerates
// -disable-pre-scan-ping and still writes a (header-only) unreachable file — is
// obsolete: after the split, scan never pings and owns no ping flags or
// unreachable artifact (that is pre-ping's job). The meaningful contract now is
// that the removed flag is an unknown-flag parse error, which this asserts.
// (Complements TestRunMain_Scan_RejectsPingFlags, which covers
// -pre-scan-ping-timeout.)
func TestRunMain_Scan_RejectsDisablePreScanPingFlag(t *testing.T) {
	tmp := t.TempDir()
	cidrFile := filepath.Join(tmp, "cidr.csv")
	if err := os.WriteFile(cidrFile, []byte("fab_name,ip,ip_cidr,cidr_name\nfab1,127.0.0.1,127.0.0.1/32,loopback\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	code := runMain([]string{
		"scan",
		"-cidr-file", cidrFile,
		"-disable-pre-scan-ping=true",
	}, &bytes.Buffer{}, &bytes.Buffer{})
	if code != 2 {
		t.Fatalf("expected exit 2 for scan with -disable-pre-scan-ping, got %d", code)
	}
}

func TestRunMain_WhenScanConfigParseFails_ReturnsExit2AndWritesStderr(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	code := runMain([]string{"scan", "-cidr-file", "", "-port-file", ""}, stdout, stderr)

	if code != 2 {
		t.Fatalf("expected exit code 2, got %d stderr=%s", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected empty stdout, got %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "-cidr-file is required") {
		t.Fatalf("expected parse error on stderr, got %s", stderr.String())
	}
}

func TestRunMain_WhenRichCSVAndPortFileMissing_ScanSucceeds(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()
	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	openPort, _ := strconv.Atoi(portStr)

	tmp := t.TempDir()
	cidrFile := filepath.Join(tmp, "rich.csv")
	requestedOutput := filepath.Join(tmp, "out.csv")
	if err := os.WriteFile(cidrFile, []byte(
		"src_ip,src_network_segment,dst_ip,dst_network_segment,service_label,protocol,port,decision,matched_policy_id,reason\n"+
			"10.0.0.10,10.0.0.0/24,127.0.0.1,127.0.0.0/24,web,tcp,"+strconv.Itoa(openPort)+",accept,P-1,allow\n",
	), 0o644); err != nil {
		t.Fatal(err)
	}

	snapshot := mustGenerateBucketSnapshot(t, tmp, cidrFile, "")

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	code := runMain([]string{
		"scan",
		"-cidr-file", cidrFile,
		"-resume", snapshot,
		"-output", requestedOutput,
		"-workers", "1",
		"-delay", "0ms",
		"-timeout", "100ms",
		"-disable-api=true",
	}, stdout, stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d stderr=%s", code, stderr.String())
	}
	scanOutputPath := mustFindOneMain(t, filepath.Join(tmp, "scan_results-*.csv"))
	data, err := os.ReadFile(scanOutputPath)
	if err != nil {
		t.Fatalf("failed to read output csv: %v", err)
	}
	out := string(data)
	if !strings.Contains(out, ","+strconv.Itoa(openPort)+",open") {
		t.Fatalf("expected open rich-mode row, got: %s", out)
	}
}

// TestRunMain_WhenGenerateBucketsBasicCSVAndPortFileMissing_ReturnsExit1 carries
// forward the intent of the former scan-side "default CSV + missing port-file"
// test. After the split, port ownership moved to generate-buckets (scan reads
// ports from the snapshot), so the "-port-file is required" failure for a basic
// (non-rich) CSV now surfaces there. Missing -resume on scan is a *parse* error
// (exit 2), covered by TestRunMain_Scan_RequiresResume; this asserts the runtime
// input error (exit 1) at the step that now owns -port-file.
func TestRunMain_WhenGenerateBucketsBasicCSVAndPortFileMissing_ReturnsExit1(t *testing.T) {
	tmp := t.TempDir()
	cidrFile := filepath.Join(tmp, "cidr.csv")
	if err := os.WriteFile(cidrFile, []byte("fab_name,ip,ip_cidr,cidr_name\nfab1,127.0.0.1,127.0.0.1/32,loopback\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	code := runMain([]string{
		"generate-buckets",
		"-cidr-file", cidrFile,
		"-buckets-out", filepath.Join(tmp, "buckets.json"),
		"-workers", "1",
	}, stdout, stderr)
	if code != 1 {
		t.Fatalf("expected exit 1, got %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "-port-file is required") {
		t.Fatalf("expected missing port-file error, got %s", stderr.String())
	}
}

func TestRunMain_ScanSuccess_WritesTimestampedBatchPairInRequestedDirectory(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()

	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	openPort, _ := strconv.Atoi(portStr)

	tmp := t.TempDir()
	cidrFile := filepath.Join(tmp, "cidr.csv")
	portFile := filepath.Join(tmp, "ports.csv")
	requestedOutput := filepath.Join(tmp, "custom-name.csv")

	if err := os.WriteFile(cidrFile, []byte("fab_name,ip,ip_cidr,cidr_name\nfab1,127.0.0.1,127.0.0.1/32,loopback\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portFile, []byte(strconv.Itoa(openPort)+"/tcp\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	snapshot := mustGenerateBucketSnapshot(t, tmp, cidrFile, portFile)

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	code := runMain([]string{
		"scan",
		"-cidr-file", cidrFile,
		"-port-file", portFile,
		"-resume", snapshot,
		"-output", requestedOutput,
		"-workers", "1",
		"-delay", "0ms",
		"-timeout", "100ms",
		"-disable-api=true",
	}, stdout, stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d stderr=%s", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected no stdout for single-result scan, got %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "[INFO] scan_result") {
		t.Fatalf("expected scan_result log on stderr, got %s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "state_transition:completion_summary") {
		t.Fatalf("expected completion summary log on stderr, got %s", stderr.String())
	}

	scanPath := mustFindOneMain(t, filepath.Join(tmp, "scan_results-*.csv"))
	openPath := mustFindOneMain(t, filepath.Join(tmp, "opened_results-*.csv"))
	scanSuffix, openSuffix := mustBatchPairSuffix(t, scanPath, openPath)
	if scanSuffix != openSuffix {
		t.Fatalf("expected matching batch suffixes, got scan=%s open=%s", scanSuffix, openSuffix)
	}
	if _, err := os.Stat(requestedOutput); !os.IsNotExist(err) {
		t.Fatalf("expected requested output path to be used as directory hint only, err=%v", err)
	}
}

func mustFindOneMain(t *testing.T, pattern string) string {
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

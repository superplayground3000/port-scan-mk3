package scanapp

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
)

// expectedUnreachableHeader mirrors the fixed 12-column contract in
// pkg/writer/unreachable_writer.go:29. RunPrePing must not change it.
var expectedUnreachableHeader = []string{
	"ip", "ip_cidr", "status", "reason", "fab_name", "cidr_name",
	"service_label", "decision", "matched_policy_id", "execution_key",
	"src_ip", "src_network_segment",
}

func writePrePingInputs(t *testing.T, cidrCSV, portsCSV string) (cidrFile, portFile, output string) {
	t.Helper()
	tmp := t.TempDir()
	cidrFile = filepath.Join(tmp, "cidr.csv")
	portFile = filepath.Join(tmp, "ports.csv")
	output = filepath.Join(tmp, "scan_results.csv")
	if err := os.WriteFile(cidrFile, []byte(cidrCSV), 0o644); err != nil {
		t.Fatalf("write cidr file: %v", err)
	}
	if err := os.WriteFile(portFile, []byte(portsCSV), 0o644); err != nil {
		t.Fatalf("write port file: %v", err)
	}
	return cidrFile, portFile, output
}

func readCSVRecords(t *testing.T, path string) [][]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open output csv: %v", err)
	}
	defer func() { _ = f.Close() }()
	records, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatalf("parse output csv: %v", err)
	}
	return records
}

func TestRunPrePing_BasicCSVWithoutPortFile_Succeeds(t *testing.T) {
	// Pre-ping is per-IP and needs no ports. A basic (non-rich) CSV without a
	// -port-file must still ping — pre-ping's flag surface intentionally omits
	// -port-file (design §6), so requiring it would make basic-CSV pre-ping
	// impossible via the CLI and regress the pre-split behavior.
	tmp := t.TempDir()
	cidrFile := filepath.Join(tmp, "cidr.csv")
	output := filepath.Join(tmp, "scan_results.csv")
	if err := os.WriteFile(cidrFile,
		[]byte("fab_name,ip,ip_cidr,cidr_name\nfab1,10.0.0.1,10.0.0.1/32,web\nfab2,10.0.0.2,10.0.0.2/32,web\n"),
		0o644); err != nil {
		t.Fatalf("write cidr file: %v", err)
	}
	checker := &fakePreScanChecker{
		results: map[string]ReachabilityResult{
			"10.0.0.2": {IP: "10.0.0.2", Reachable: false, FailureText: "timeout"},
		},
	}

	var stdout, stderr bytes.Buffer
	err := RunPrePing(context.Background(), mustPrePingConfig(t, config.PrePingValues{
		CIDRFile:         cidrFile,
		CIDRIPCol:        "ip",
		CIDRIPCidrCol:    "ip_cidr",
		Output:           output,
		Workers:          4,
		PingTimeout:      300 * time.Millisecond,
		ProgressInterval: 1,
	}), &stdout, &stderr, RunOptions{ReachabilityChecker: checker})
	if err != nil {
		t.Fatalf("RunPrePing on basic CSV without -port-file errored: %v", err)
	}

	path := strings.TrimSpace(stdout.String())
	records := readCSVRecords(t, path)
	if len(records) != 2 {
		t.Fatalf("expected header + 1 unreachable row, got %d rows: %v", len(records), records)
	}
	if records[1][0] != "10.0.0.2" {
		t.Fatalf("expected unreachable ip 10.0.0.2, got %q", records[1][0])
	}
}

func TestRunPrePing_WritesUnreachableCSVWithExistingSchema(t *testing.T) {
	cidrFile, _, output := writePrePingInputs(t,
		"fab_name,ip,ip_cidr,cidr_name\nfab1,10.0.0.1,10.0.0.1/32,web\nfab2,10.0.0.2,10.0.0.2/32,web\n",
		"80/tcp\n",
	)
	checker := &fakePreScanChecker{
		results: map[string]ReachabilityResult{
			"10.0.0.1": {IP: "10.0.0.1", Reachable: false, FailureText: "timeout"},
		},
	}

	var stdout, stderr bytes.Buffer
	err := RunPrePing(context.Background(), mustPrePingConfig(t, config.PrePingValues{
		CIDRFile:         cidrFile,
		CIDRIPCol:        "ip",
		CIDRIPCidrCol:    "ip_cidr",
		Output:           output,
		Workers:          4,
		PingTimeout:      300 * time.Millisecond,
		ProgressInterval: 1,
	}), &stdout, &stderr, RunOptions{ReachabilityChecker: checker})
	if err != nil {
		t.Fatalf("RunPrePing error: %v", err)
	}

	path := strings.TrimSpace(stdout.String())
	records := readCSVRecords(t, path)
	if len(records) != 2 {
		t.Fatalf("expected header + 1 data row, got %d rows: %v", len(records), records)
	}
	if !reflect.DeepEqual(records[0], expectedUnreachableHeader) {
		t.Fatalf("header mismatch:\n got  %v\n want %v", records[0], expectedUnreachableHeader)
	}
	row := records[1]
	if row[0] != "10.0.0.1" {
		t.Fatalf("expected unreachable ip 10.0.0.1, got %q", row[0])
	}
	if row[2] != "unreachable" {
		t.Fatalf("expected status=unreachable, got %q", row[2])
	}
	if !strings.Contains(row[3], "300ms") {
		t.Fatalf("expected reason to contain the timeout, got %q", row[3])
	}
}

func TestRunPrePing_ReportsProgressOverUniqueIPs(t *testing.T) {
	cidrFile, _, output := writePrePingInputs(t,
		"fab_name,ip,ip_cidr,cidr_name\nfab1,10.0.0.1,10.0.0.1/32,web\nfab2,10.0.0.2,10.0.0.2/32,web\nfab3,10.0.0.3,10.0.0.3/32,web\n",
		"80/tcp\n",
	)
	checker := &fakePreScanChecker{} // all reachable by default

	var stdout, stderr bytes.Buffer
	if err := RunPrePing(context.Background(), mustPrePingConfig(t, config.PrePingValues{
		CIDRFile:         cidrFile,
		CIDRIPCol:        "ip",
		CIDRIPCidrCol:    "ip_cidr",
		Output:           output,
		Workers:          3,
		PingTimeout:      200 * time.Millisecond,
		ProgressInterval: 1,
	}), &stdout, &stderr, RunOptions{ReachabilityChecker: checker}); err != nil {
		t.Fatalf("RunPrePing error: %v", err)
	}

	lines := splitNonEmptyLines(stderr.String())
	// interval=1 → one progress line per checked IP (3) + one final Done line.
	if len(lines) != 4 {
		t.Fatalf("expected 3 increment lines + 1 done line, got %d: %v", len(lines), lines)
	}
	final := lines[len(lines)-1]
	if !strings.Contains(final, "3/3") || !strings.Contains(final, "100.0%") {
		t.Fatalf("expected final Done line to report 3/3 (100.0%%), got %q", final)
	}
}

func TestRunPrePing_PrintsResolvedPath(t *testing.T) {
	cidrFile, _, output := writePrePingInputs(t,
		"fab_name,ip,ip_cidr,cidr_name\nfab1,10.0.0.1,10.0.0.1/32,web\n",
		"80/tcp\n",
	)
	checker := &fakePreScanChecker{}

	var stdout, stderr bytes.Buffer
	if err := RunPrePing(context.Background(), mustPrePingConfig(t, config.PrePingValues{
		CIDRFile:      cidrFile,
		CIDRIPCol:     "ip",
		CIDRIPCidrCol: "ip_cidr",
		Output:        output,
		Workers:       2,
		PingTimeout:   100 * time.Millisecond,
	}), &stdout, &stderr, RunOptions{ReachabilityChecker: checker}); err != nil {
		t.Fatalf("RunPrePing error: %v", err)
	}

	path := strings.TrimSpace(stdout.String())
	if path == "" {
		t.Fatal("expected RunPrePing to print the resolved unreachable path to stdout")
	}
	base := filepath.Base(path)
	if !strings.HasPrefix(base, "unreachable_results-") || !strings.HasSuffix(base, ".csv") {
		t.Fatalf("expected timestamped unreachable_results-*.csv path, got %q", path)
	}
	if filepath.Dir(path) != filepath.Dir(output) {
		t.Fatalf("expected path in the -output directory %q, got %q", filepath.Dir(output), filepath.Dir(path))
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected resolved file to exist: %v", err)
	}
}

func TestRunPrePing_NoTargets_WritesHeaderOnlyValidCSV(t *testing.T) {
	// The CIDR loader rejects a header-only file (it requires >=1 usable row), so
	// a genuinely empty target set is unreachable through file input. The
	// realizable header-only case is "nothing unreachable to write": every target
	// is reachable, so no data rows are emitted and only the fixed header remains.
	cidrFile, _, output := writePrePingInputs(t,
		"fab_name,ip,ip_cidr,cidr_name\nfab1,10.0.0.1,10.0.0.1/32,web\n",
		"80/tcp\n",
	)
	checker := &fakePreScanChecker{} // all reachable by default → zero unreachable rows

	var stdout, stderr bytes.Buffer
	if err := RunPrePing(context.Background(), mustPrePingConfig(t, config.PrePingValues{
		CIDRFile:      cidrFile,
		CIDRIPCol:     "ip",
		CIDRIPCidrCol: "ip_cidr",
		Output:        output,
		Workers:       4,
		PingTimeout:   100 * time.Millisecond,
	}), &stdout, &stderr, RunOptions{ReachabilityChecker: checker}); err != nil {
		t.Fatalf("RunPrePing error: %v", err)
	}

	path := strings.TrimSpace(stdout.String())
	records := readCSVRecords(t, path)
	if len(records) != 1 {
		t.Fatalf("expected header-only CSV, got %d rows: %v", len(records), records)
	}
	if !reflect.DeepEqual(records[0], expectedUnreachableHeader) {
		t.Fatalf("header mismatch:\n got  %v\n want %v", records[0], expectedUnreachableHeader)
	}
}

// TestRunPrePing_WhenContextCanceledMidFlight_AbortsWithoutWritingOutput drives
// RunPrePing (the live production pre-ping entry) against a checker that blocks
// until the context is canceled, cancels while a reachability check is in
// flight, and asserts the run aborts with context.Canceled without writing a
// partial/fake unreachable CSV or printing a resolved path. This cancellation is
// reachable in production only via RunPrePing's runReachabilityChecksWithProgress
// path, so it must stay covered. It would fail (a CSV would be written and a path
// printed) if the worker pool swallowed the cancellation instead of propagating
// it.
func TestRunPrePing_WhenContextCanceledMidFlight_AbortsWithoutWritingOutput(t *testing.T) {
	cidrFile, _, output := writePrePingInputs(t,
		"fab_name,ip,ip_cidr,cidr_name\nfab1,10.0.0.1,10.0.0.1/32,web\n",
		"80/tcp\n",
	)
	checker := &blockingReachabilityChecker{started: make(chan struct{})}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var stdout, stderr bytes.Buffer
	prePingConfig := mustPrePingConfig(t, config.PrePingValues{
		CIDRFile:         cidrFile,
		CIDRIPCol:        "ip",
		CIDRIPCidrCol:    "ip_cidr",
		Output:           output,
		Workers:          1,
		PingTimeout:      time.Hour,
		ProgressInterval: 1,
	})
	errCh := make(chan error, 1)
	go func() {
		errCh <- RunPrePing(ctx, prePingConfig, &stdout, &stderr, RunOptions{ReachabilityChecker: checker})
	}()

	select {
	case <-checker.started:
	case <-time.After(2 * time.Second):
		t.Fatal("checker was never invoked; cannot cancel mid-flight")
	}
	cancel()

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled from mid-flight cancel, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RunPrePing did not return after cancellation")
	}

	// No unreachable CSV may be written on cancel (no partial/fake output).
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(output), "unreachable_results-*.csv"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("expected no unreachable output on cancel, got %v", matches)
	}
	// No resolved path may be printed on cancel (nothing to chain).
	if strings.TrimSpace(stdout.String()) != "" {
		t.Fatalf("expected no resolved path printed on cancel, got %q", stdout.String())
	}
}

// blockingReachabilityChecker signals started on its first check and then blocks
// until the caller's context is canceled, returning ctx.Err(). It lets a test
// deterministically cancel RunPrePing while a reachability check is in flight.
type blockingReachabilityChecker struct {
	started   chan struct{}
	startOnce sync.Once
}

func (b *blockingReachabilityChecker) Check(ctx context.Context, ip string, timeout time.Duration) ReachabilityResult {
	result, _ := b.CheckDetailed(ctx, ip, timeout)
	return result
}

func (b *blockingReachabilityChecker) CheckDetailed(ctx context.Context, ip string, _ time.Duration) (ReachabilityResult, error) {
	b.startOnce.Do(func() { close(b.started) })
	<-ctx.Done()
	return ReachabilityResult{IP: ip}, ctx.Err()
}

func splitNonEmptyLines(s string) []string {
	out := make([]string, 0)
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out
}

package scanapp

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// progressIntervalScanFixture builds a scan over four loopback ports. Every
// dial fails, so the scan writes four results and then completes.
func progressIntervalScanFixture(t *testing.T, progressInterval int) scanConfigFixture {
	t.Helper()

	tmp := t.TempDir()
	cidrFile := filepath.Join(tmp, "cidr.csv")
	portFile := filepath.Join(tmp, "ports.csv")
	if err := os.WriteFile(cidrFile, []byte("fab_name,ip,ip_cidr,cidr_name\nfab1,127.0.0.1,127.0.0.1/32,loopback\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portFile, []byte("1/tcp\n2/tcp\n3/tcp\n4/tcp\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := scanConfigFixture{
		CIDRFile:         cidrFile,
		PortFile:         portFile,
		Output:           filepath.Join(tmp, "scan_results.csv"),
		Timeout:          50 * time.Millisecond,
		Delay:            0,
		BucketRate:       100,
		BucketCapacity:   100,
		Workers:          1,
		Pressure:         pressureConfigFixture{Disabled: true},
		LogLevel:         "info",
		Format:           "json",
		ProgressInterval: progressInterval,
	}
	cfg.Resume = generateBucketFile(t, cfg, filepath.Join(tmp, "buckets.json"), "")
	return cfg
}

// runProgressIntervalScan runs the scan and returns the count of scan_progress
// events on stderr.
func runProgressIntervalScan(t *testing.T, cfg scanConfigFixture) int {
	t.Helper()

	stderr := &bytes.Buffer{}
	dial := func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("dial failed for progress interval test")
	}
	if err := Run(context.Background(), scanConfigurationFromFixture(t, cfg), io.Discard, stderr, RunOptions{
		DisableKeyboard: true,
		Dial:            dial,
	}); err != nil {
		t.Fatalf("run failed: %v", err)
	}
	return strings.Count(stderr.String(), `"msg":"scan_progress"`)
}

// TestRun_WhenConfigurationSetsProgressInterval_UsesThatCadence proves that the
// resolved scan configuration, not RunOptions, decides the progress cadence.
// Issue #158: the -progress-interval flag was parsed and discarded, so the
// cadence was always the built-in 100.
func TestRun_WhenConfigurationSetsProgressInterval_UsesThatCadence(t *testing.T) {
	got := runProgressIntervalScan(t, progressIntervalScanFixture(t, 1))
	if got != 4 {
		t.Fatalf("scan_progress events = %d, want 4 (one per written result)", got)
	}
}

// TestRun_WhenConfiguredProgressIntervalIsNotPositive_UsesDefaultCadence pins
// the agreed contract: a value that is not positive selects the built-in
// cadence of 100, the same as pre-ping and generate-buckets. Four results are
// far below that cadence, so no progress event appears.
func TestRun_WhenConfiguredProgressIntervalIsNotPositive_UsesDefaultCadence(t *testing.T) {
	for _, interval := range []int{0, -5} {
		got := runProgressIntervalScan(t, progressIntervalScanFixture(t, interval))
		if got != 0 {
			t.Fatalf("scan_progress events with interval %d = %d, want 0 (default cadence 100)", interval, got)
		}
	}
}

package scanapp

import (
	"bytes"
	"context"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// quietScanFixture builds a one-target, three-port scan whose logger is driven
// through the production path in scan.go, so the assertions describe what an
// operator sees on the real streams for a given -quiet / -log-level pair.
func quietScanFixture(t *testing.T, logLevel string, ports string, quiet bool) scanConfigFixture {
	t.Helper()

	tmp := t.TempDir()
	cidrFile := filepath.Join(tmp, "cidr.csv")
	portFile := filepath.Join(tmp, "ports.csv")
	if err := os.WriteFile(cidrFile, []byte("fab_name,ip,ip_cidr,cidr_name\nfab1,127.0.0.1,127.0.0.1/32,loopback\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portFile, []byte(ports), 0o644); err != nil {
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
		LogLevel:         logLevel,
		Format:           "human",
		Quiet:            quiet,
		ProgressInterval: 1,
	}
	cfg.Resume = generateBucketFile(t, cfg, filepath.Join(tmp, "buckets.json"), "")
	return cfg
}

func TestRun_WhenQuietAndWorkerPanics_StillLogsTheFatalErrorToStderr(t *testing.T) {
	cfg := quietScanFixture(t, "info", "1/tcp\n2/tcp\n3/tcp\n", true)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stderr := &bytes.Buffer{}
	err := Run(ctx, scanConfigurationFromFixture(t, cfg), io.Discard, stderr, RunOptions{
		DisableKeyboard: true,
		Dial: func(context.Context, string, string) (net.Conn, error) {
			panic("boom in dial")
		},
	})
	if err == nil {
		t.Fatal("expected a runtime error when the executor worker panics")
	}
	if !strings.Contains(stderr.String(), "executor worker panic") {
		t.Fatalf("quiet run hid the fatal worker panic from stderr; stderr = %q", stderr.String())
	}
}

func TestRun_WhenQuietAndLocalResourceFailure_StillWarnsRowsAreNotConfirmedClosed(t *testing.T) {
	cfg := quietScanFixture(t, "info", "1/tcp\n2/tcp\n", true)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stderr := &bytes.Buffer{}
	err := Run(ctx, scanConfigurationFromFixture(t, cfg), io.Discard, stderr, RunOptions{
		DisableKeyboard: true,
		Dial: func(context.Context, string, string) (net.Conn, error) {
			return nil, syscall.EADDRNOTAVAIL
		},
	})
	if err != nil {
		t.Fatalf("a local resource failure must not end the run: %v", err)
	}

	logs := stderr.String()
	if !strings.Contains(logs, "local resource failure while dialing") {
		t.Fatalf("quiet run hid the local resource failure warning; stderr = %q", logs)
	}
	if !strings.Contains(logs, "NOT confirmed closed") {
		t.Fatalf("quiet run hid the %q warning that keeps error(local) rows from reading as closed; stderr = %q", "NOT confirmed closed", logs)
	}
}

func TestRun_WhenQuiet_StillSuppressesThePeriodicProgressLine(t *testing.T) {
	dial := func(context.Context, string, string) (net.Conn, error) {
		return nil, syscall.ECONNREFUSED
	}

	runOnce := func(t *testing.T, quiet bool) string {
		t.Helper()
		cfg := quietScanFixture(t, "info", "1/tcp\n2/tcp\n3/tcp\n", quiet)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		stdout := &bytes.Buffer{}
		if err := Run(ctx, scanConfigurationFromFixture(t, cfg), stdout, io.Discard, RunOptions{
			DisableKeyboard: true,
			Dial:            dial,
		}); err != nil {
			t.Fatalf("run failed: %v", err)
		}
		return stdout.String()
	}

	if loud := runOnce(t, false); !strings.Contains(loud, "progress cidr=") {
		t.Fatalf("without -quiet the progress line must appear, so this case can detect its loss; stdout = %q", loud)
	}
	if quiet := runOnce(t, true); strings.Contains(quiet, "progress cidr=") {
		t.Fatalf("-quiet must still suppress the periodic progress line; stdout = %q", quiet)
	}
}

func TestRun_WhenQuiet_LogLevelAloneDecidesWhetherInfoLinesAppear(t *testing.T) {
	dial := func(context.Context, string, string) (net.Conn, error) {
		return nil, syscall.ECONNREFUSED
	}

	runOnce := func(t *testing.T, logLevel string) string {
		t.Helper()
		cfg := quietScanFixture(t, logLevel, "1/tcp\n2/tcp\n", true)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		stderr := &bytes.Buffer{}
		if err := Run(ctx, scanConfigurationFromFixture(t, cfg), io.Discard, stderr, RunOptions{
			DisableKeyboard: true,
			Dial:            dial,
		}); err != nil {
			t.Fatalf("run failed: %v", err)
		}
		return stderr.String()
	}

	if info := runOnce(t, "info"); !strings.Contains(info, "scan_result") {
		t.Fatalf("-quiet at -log-level info must keep the info-level scan_result lines; stderr = %q", info)
	}
	if quiet := runOnce(t, "error"); strings.Contains(quiet, "scan_result") {
		t.Fatalf("-quiet -log-level error must restore a silent run; stderr = %q", quiet)
	}
}

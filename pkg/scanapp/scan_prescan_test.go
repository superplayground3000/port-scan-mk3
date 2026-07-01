package scanapp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
)

func TestRun_WhenPreScanPingFindsUnreachable_FinalizesUnreachableOutputBeforeFirstDial(t *testing.T) {
	tmp := t.TempDir()
	cidrFile := filepath.Join(tmp, "cidr.csv")
	portFile := filepath.Join(tmp, "ports.csv")
	outFile := filepath.Join(tmp, "out.csv")

	if err := os.WriteFile(cidrFile, []byte("fab_name,ip,ip_cidr,cidr_name\nfab1,10.0.0.1,10.0.0.1/32,blocked\nfab2,127.0.0.1,127.0.0.1/32,loopback\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portFile, []byte("1/tcp\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	checker := &fakeRunReachabilityChecker{
		results: map[string]ReachabilityResult{
			"10.0.0.1":  {IP: "10.0.0.1", Reachable: false},
			"127.0.0.1": {IP: "127.0.0.1", Reachable: true},
		},
	}

	var (
		hookOnce   sync.Once
		hookCalled bool
		hookErr    error
	)
	dial := func(context.Context, string, string) (net.Conn, error) {
		hookOnce.Do(func() {
			hookCalled = true
			path := mustFindOne(t, filepath.Join(tmp, "unreachable_results-*.csv"))
			if strings.HasSuffix(path, ".tmp") {
				hookErr = fmt.Errorf("expected final unreachable path, got tmp path %s", path)
				return
			}
			if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
				hookErr = fmt.Errorf("expected no unreachable tmp file before first dial, err=%v", err)
				return
			}
			data, err := os.ReadFile(path)
			if err != nil {
				hookErr = err
				return
			}
			if !strings.Contains(string(data), "10.0.0.1,10.0.0.1/32,unreachable") {
				hookErr = fmt.Errorf("expected unreachable row before first dial, got %s", string(data))
			}
		})
		return nil, errors.New("forced dial failure")
	}

	cfg := config.Config{
		CIDRFile:         cidrFile,
		PortFile:         portFile,
		Output:           outFile,
		Timeout:          20 * time.Millisecond,
		Delay:            0,
		BucketRate:       100,
		BucketCapacity:   100,
		Workers:          1,
		PressureInterval: 5 * time.Second,
		DisableAPI:       true,
		LogLevel:         "error",
	}

	if err := Run(context.Background(), cfg, &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{
		Dial:                dial,
		DisableKeyboard:     true,
		ReachabilityChecker: checker,
	}); err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if !hookCalled {
		t.Fatal("expected first dial hook to run")
	}
	if hookErr != nil {
		t.Fatalf("first dial barrier check failed: %v", hookErr)
	}

	unreachablePath := mustFindOne(t, filepath.Join(tmp, "unreachable_results-*.csv"))
	unreachableData, err := os.ReadFile(unreachablePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(unreachableData), "10.0.0.1,10.0.0.1/32,unreachable") {
		t.Fatalf("expected unreachable csv row, got %s", string(unreachableData))
	}

	scanPath := mustFindOne(t, filepath.Join(tmp, "scan_results-*.csv"))
	scanData, err := os.ReadFile(scanPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(scanData), "10.0.0.1") {
		t.Fatalf("did not expect unreachable target in scan output, got %s", string(scanData))
	}
	if !strings.Contains(string(scanData), "127.0.0.1,127.0.0.1/32,1,close") {
		t.Fatalf("expected reachable target to be scanned, got %s", string(scanData))
	}
}

func TestRun_WhenPreScanPingDisabled_SkipsCheckerAndDoesNotFilterTargets(t *testing.T) {
	tmp := t.TempDir()
	cidrFile := filepath.Join(tmp, "cidr.csv")
	portFile := filepath.Join(tmp, "ports.csv")
	outFile := filepath.Join(tmp, "out.csv")

	if err := os.WriteFile(cidrFile, []byte("fab_name,ip,ip_cidr,cidr_name\nfab1,10.0.0.1,10.0.0.1/32,blocked\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portFile, []byte("1/tcp\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	checker := &fakeRunReachabilityChecker{
		results: map[string]ReachabilityResult{
			"10.0.0.1": {IP: "10.0.0.1", Reachable: false},
		},
	}

	cfg := config.Config{
		CIDRFile:           cidrFile,
		PortFile:           portFile,
		Output:             outFile,
		Timeout:            20 * time.Millisecond,
		Delay:              0,
		BucketRate:         100,
		BucketCapacity:     100,
		Workers:            1,
		PressureInterval:   5 * time.Second,
		DisableAPI:         true,
		DisablePreScanPing: true,
		LogLevel:           "error",
	}

	if err := Run(context.Background(), cfg, &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{
		Dial:                func(context.Context, string, string) (net.Conn, error) { return nil, errors.New("forced dial failure") },
		DisableKeyboard:     true,
		ReachabilityChecker: checker,
	}); err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if calls := checker.calls(); len(calls) != 0 {
		t.Fatalf("expected disabled pre-scan ping to skip checker, got %v", calls)
	}

	unreachablePath := mustFindOne(t, filepath.Join(tmp, "unreachable_results-*.csv"))
	unreachableData, err := os.ReadFile(unreachablePath)
	if err != nil {
		t.Fatal(err)
	}
	if lineCount(string(unreachableData)) != 1 {
		t.Fatalf("expected unreachable output header only when disabled, got %s", string(unreachableData))
	}

	scanPath := mustFindOne(t, filepath.Join(tmp, "scan_results-*.csv"))
	scanData, err := os.ReadFile(scanPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(scanData), "10.0.0.1,10.0.0.1/32,1,close") {
		t.Fatalf("expected target to remain in scan output when pre-scan disabled, got %s", string(scanData))
	}
}

func TestRun_WhenPreScanPingContextCanceled_AbortsWithoutWritingFakeUnreachableResults(t *testing.T) {
	tmp := t.TempDir()
	cidrFile := filepath.Join(tmp, "cidr.csv")
	portFile := filepath.Join(tmp, "ports.csv")
	outFile := filepath.Join(tmp, "out.csv")

	if err := os.WriteFile(cidrFile, []byte("fab_name,ip,ip_cidr,cidr_name\nfab1,10.0.0.1,10.0.0.1/32,blocked\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portFile, []byte("1/tcp\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	checker := &fakeRunReachabilityChecker{
		waitForContext: map[string]bool{
			"10.0.0.1": true,
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	dialCount := 0
	cfg := config.Config{
		CIDRFile:         cidrFile,
		PortFile:         portFile,
		Output:           outFile,
		Timeout:          20 * time.Millisecond,
		Delay:            0,
		BucketRate:       100,
		BucketCapacity:   100,
		Workers:          1,
		PressureInterval: 5 * time.Second,
		DisableAPI:       true,
		LogLevel:         "error",
	}

	err := Run(ctx, cfg, &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{
		Dial: func(context.Context, string, string) (net.Conn, error) {
			dialCount++
			return nil, errors.New("unexpected dial")
		},
		DisableKeyboard:     true,
		ReachabilityChecker: checker,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled error, got %v", err)
	}
	if dialCount != 0 {
		t.Fatalf("expected canceled pre-scan to skip tcp dials, got %d", dialCount)
	}
	if matches, globErr := filepath.Glob(filepath.Join(tmp, "unreachable_results-*.csv")); globErr != nil {
		t.Fatalf("unexpected glob error: %v", globErr)
	} else if len(matches) != 0 {
		t.Fatalf("expected no finalized unreachable output on canceled pre-scan, got %v", matches)
	}
}

func TestRun_WhenAllTargetsUnreachable_SucceedsWithHeaderOnlyScanOutputs(t *testing.T) {
	tmp := t.TempDir()
	cidrFile := filepath.Join(tmp, "cidr.csv")
	portFile := filepath.Join(tmp, "ports.csv")
	outFile := filepath.Join(tmp, "out.csv")

	if err := os.WriteFile(cidrFile, []byte("fab_name,ip,ip_cidr,cidr_name\nfab1,10.0.0.1,10.0.0.1/32,blocked\nfab2,10.0.0.2,10.0.0.2/32,blocked-2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portFile, []byte("1/tcp\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	checker := &fakeRunReachabilityChecker{
		results: map[string]ReachabilityResult{
			"10.0.0.1": {IP: "10.0.0.1", Reachable: false},
			"10.0.0.2": {IP: "10.0.0.2", Reachable: false},
		},
	}

	dialCount := 0
	cfg := config.Config{
		CIDRFile:         cidrFile,
		PortFile:         portFile,
		Output:           outFile,
		Timeout:          20 * time.Millisecond,
		Delay:            0,
		BucketRate:       100,
		BucketCapacity:   100,
		Workers:          1,
		PressureInterval: 5 * time.Second,
		DisableAPI:       true,
		LogLevel:         "error",
	}

	if err := Run(context.Background(), cfg, &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{
		Dial: func(context.Context, string, string) (net.Conn, error) {
			dialCount++
			return nil, errors.New("unexpected dial")
		},
		DisableKeyboard:     true,
		ReachabilityChecker: checker,
	}); err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if dialCount != 0 {
		t.Fatalf("expected no tcp dials for all-unreachable run, got %d", dialCount)
	}

	scanPath := mustFindOne(t, filepath.Join(tmp, "scan_results-*.csv"))
	scanData, err := os.ReadFile(scanPath)
	if err != nil {
		t.Fatal(err)
	}
	if lineCount(string(scanData)) != 1 {
		t.Fatalf("expected scan output header only, got %s", string(scanData))
	}

	openPath := mustFindOne(t, filepath.Join(tmp, "opened_results-*.csv"))
	openData, err := os.ReadFile(openPath)
	if err != nil {
		t.Fatal(err)
	}
	if lineCount(string(openData)) != 1 {
		t.Fatalf("expected opened output header only, got %s", string(openData))
	}

	unreachablePath := mustFindOne(t, filepath.Join(tmp, "unreachable_results-*.csv"))
	unreachableData, err := os.ReadFile(unreachablePath)
	if err != nil {
		t.Fatal(err)
	}
	if lineCount(string(unreachableData)) != 3 {
		t.Fatalf("expected unreachable output header plus two rows, got %s", string(unreachableData))
	}
}

func TestRun_WhenRichAllTargetsUnreachable_SucceedsWithoutDispatchingTCP(t *testing.T) {
	tmp := t.TempDir()
	cidrFile := filepath.Join(tmp, "rich.csv")
	outFile := filepath.Join(tmp, "out.csv")

	if err := os.WriteFile(cidrFile, []byte(
		"src_ip,src_network_segment,dst_ip,dst_network_segment,service_label,protocol,port,decision,matched_policy_id,reason\n"+
			"10.1.0.10,10.1.0.0/24,10.0.0.9,10.0.0.0/24,svc-a,tcp,443,accept,P-1,allow\n"+
			"10.1.1.11,10.1.1.0/24,10.0.0.9,10.0.0.0/24,svc-b,tcp,443,accept,P-2,allow\n",
	), 0o644); err != nil {
		t.Fatal(err)
	}

	checker := &fakeRunReachabilityChecker{
		results: map[string]ReachabilityResult{
			"10.0.0.9": {IP: "10.0.0.9", Reachable: false},
		},
	}

	dialCount := 0
	cfg := config.Config{
		CIDRFile:         cidrFile,
		Output:           outFile,
		Timeout:          20 * time.Millisecond,
		Delay:            0,
		BucketRate:       100,
		BucketCapacity:   100,
		Workers:          1,
		PressureInterval: 5 * time.Second,
		DisableAPI:       true,
		LogLevel:         "error",
	}

	if err := Run(context.Background(), cfg, &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{
		Dial: func(context.Context, string, string) (net.Conn, error) {
			dialCount++
			return nil, errors.New("unexpected dial")
		},
		DisableKeyboard:     true,
		ReachabilityChecker: checker,
	}); err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if dialCount != 0 {
		t.Fatalf("expected rich all-unreachable run to skip tcp dials, got %d", dialCount)
	}

	scanPath := mustFindOne(t, filepath.Join(tmp, "scan_results-*.csv"))
	scanData, err := os.ReadFile(scanPath)
	if err != nil {
		t.Fatal(err)
	}
	if lineCount(string(scanData)) != 1 {
		t.Fatalf("expected rich scan output header only, got %s", string(scanData))
	}

	openPath := mustFindOne(t, filepath.Join(tmp, "opened_results-*.csv"))
	openData, err := os.ReadFile(openPath)
	if err != nil {
		t.Fatal(err)
	}
	if lineCount(string(openData)) != 1 {
		t.Fatalf("expected rich opened output header only, got %s", string(openData))
	}

	unreachablePath := mustFindOne(t, filepath.Join(tmp, "unreachable_results-*.csv"))
	unreachableData, err := os.ReadFile(unreachablePath)
	if err != nil {
		t.Fatal(err)
	}
	if lineCount(string(unreachableData)) != 2 {
		t.Fatalf("expected rich unreachable output header plus merged row, got %s", string(unreachableData))
	}
}

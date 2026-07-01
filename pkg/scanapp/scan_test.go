package scanapp

import (
	"bytes"
	"context"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
	"github.com/xuxiping/port-scan-mk3/pkg/input"
	"github.com/xuxiping/port-scan-mk3/pkg/task"
)

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

type fakeRunReachabilityChecker struct {
	mu             sync.Mutex
	results        map[string]ReachabilityResult
	called         []string
	detailedErrs   map[string]error
	waitForContext map[string]bool
}

func (f *fakeRunReachabilityChecker) Check(_ context.Context, ip string, _ time.Duration) ReachabilityResult {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.recordLocked(ip)
}

func (f *fakeRunReachabilityChecker) CheckDetailed(ctx context.Context, ip string, _ time.Duration) (ReachabilityResult, error) {
	f.mu.Lock()
	result := f.recordLocked(ip)
	wait := f.waitForContext[ip]
	err := f.detailedErrs[ip]
	f.mu.Unlock()

	if wait {
		<-ctx.Done()
		result.FailureText = ctx.Err().Error()
		return result, ctx.Err()
	}
	if err != nil {
		result.FailureText = err.Error()
		return result, err
	}
	return result, nil
}

func (f *fakeRunReachabilityChecker) calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := append([]string(nil), f.called...)
	sort.Strings(out)
	return out
}

func (f *fakeRunReachabilityChecker) recordLocked(ip string) ReachabilityResult {
	f.called = append(f.called, ip)
	if f.results != nil {
		if result, ok := f.results[ip]; ok {
			return result
		}
	}
	return ReachabilityResult{IP: ip, Reachable: true}
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func TestParsePortRows_WhenRowsContainTCPOnly_ReturnsPortsOrError(t *testing.T) {
	ports, err := parsePortRows([]string{"80/tcp", "443/tcp"})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(ports) != 2 || ports[0] != 80 || ports[1] != 443 {
		t.Fatalf("unexpected ports: %#v", ports)
	}

	if _, err := parsePortRows([]string{"53/udp"}); err == nil {
		t.Fatal("expected invalid protocol error")
	}
}

func TestBuildRuntime_WhenChunkPortsEmpty_UsesDefaultInputPorts(t *testing.T) {
	_, ipNet, _ := net.ParseCIDR("10.0.0.0/30")
	records := []input.CIDRRecord{{
		FabName:  "fab1",
		CIDR:     "10.0.0.0/30",
		CIDRName: "x",
		Net:      ipNet,
	}}
	chunks := []task.Chunk{{
		CIDR:       "10.0.0.0/30",
		CIDRName:   "x",
		Ports:      nil,
		TotalCount: 0,
	}}
	ports := []input.PortSpec{{Number: 80, Proto: "tcp", Raw: "80/tcp"}}

	rts, err := buildRuntime(chunks, records, ports, runtimePolicy{
		bucketRate:     10,
		bucketCapacity: 10,
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(rts) != 1 {
		t.Fatalf("unexpected runtime len: %d", len(rts))
	}
	if rts[0].state.TotalCount != 4 {
		t.Fatalf("unexpected total count: %d", rts[0].state.TotalCount)
	}
}

func TestScanLogger_WhenTextOrJSONEnabled_FormatsOutputByMode(t *testing.T) {
	textOut := &bytes.Buffer{}
	l := newLogger("debug", false, textOut)
	l.debugf("x=%d", 1)
	if !strings.Contains(textOut.String(), "[DEBUG] x=1") {
		t.Fatalf("unexpected text log: %s", textOut.String())
	}

	jsonOut := &bytes.Buffer{}
	l = newLogger("info", true, jsonOut)
	l.infof("hello")
	if !strings.Contains(jsonOut.String(), `"level":"info"`) {
		t.Fatalf("unexpected json log: %s", jsonOut.String())
	}
}

func TestRun_WhenIPColumnListsSubset_ScansOnlyListedIPs(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
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

	if err := os.WriteFile(cidrFile, []byte(
		"fab_name,ip,ip_cidr,cidr_name\n"+
			"fab1,127.0.0.1,127.0.0.0/30,subset\n",
	), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portFile, []byte(strconv.Itoa(openPort)+"/tcp\n1/tcp\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{
		CIDRFile:           cidrFile,
		PortFile:           portFile,
		Output:             outFile,
		Timeout:            100 * time.Millisecond,
		Delay:              0,
		BucketRate:         100,
		BucketCapacity:     100,
		Workers:            1,
		PressureInterval:   5 * time.Second,
		DisableAPI:         true,
		LogLevel:           "error",
		PreScanPingTimeout: 100 * time.Millisecond,
	}
	if err := Run(context.Background(), cfg, &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{DisableKeyboard: true}); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	scanOutputPath := mustFindOne(t, filepath.Join(tmp, "scan_results-*.csv"))
	outBytes, err := os.ReadFile(scanOutputPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(outBytes)), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected header + 2 rows for 1 listed ip x 2 ports, got %d lines: %s", len(lines), string(outBytes))
	}
	if strings.Contains(string(outBytes), "127.0.0.2") || strings.Contains(string(outBytes), "127.0.0.3") {
		t.Fatalf("unexpected non-listed ip in output: %s", string(outBytes))
	}
}

func TestRun_WhenCIDRColumnNamesBlank_UsesDefaultInputColumns(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
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

	if err := os.WriteFile(cidrFile, []byte(
		"fab_name,ip,ip_cidr,cidr_name\n"+
			"fab1,127.0.0.1,127.0.0.1/32,loopback\n",
	), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portFile, []byte(strconv.Itoa(openPort)+"/tcp\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{
		CIDRFile:           cidrFile,
		PortFile:           portFile,
		Output:             outFile,
		Timeout:            100 * time.Millisecond,
		Delay:              0,
		BucketRate:         100,
		BucketCapacity:     100,
		Workers:            1,
		PressureInterval:   5 * time.Second,
		DisableAPI:         true,
		LogLevel:           "error",
		CIDRIPCol:          "",
		CIDRIPCidrCol:      "",
		PreScanPingTimeout: 100 * time.Millisecond,
	}
	if err := Run(context.Background(), cfg, &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{DisableKeyboard: true}); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	scanOutputPath := mustFindOne(t, filepath.Join(tmp, "scan_results-*.csv"))
	outBytes, err := os.ReadFile(scanOutputPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(outBytes), "127.0.0.1,127.0.0.1/32,"+strconv.Itoa(openPort)+",open") {
		t.Fatalf("expected open row using default input columns, got: %s", string(outBytes))
	}
}

func TestRun_WhenScanCompletes_WritesOpenRecordsToOpenedResultsCSV(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
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

	if err := os.WriteFile(cidrFile, []byte(
		"fab_name,ip,ip_cidr,cidr_name\n"+
			"fab1,127.0.0.1,127.0.0.1/32,loopback\n",
	), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portFile, []byte(strconv.Itoa(openPort)+"/tcp\n1/tcp\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{
		CIDRFile:           cidrFile,
		PortFile:           portFile,
		Output:             outFile,
		Timeout:            100 * time.Millisecond,
		Delay:              0,
		BucketRate:         100,
		BucketCapacity:     100,
		Workers:            1,
		PressureInterval:   5 * time.Second,
		DisableAPI:         true,
		LogLevel:           "error",
		PreScanPingTimeout: 100 * time.Millisecond,
	}
	if err := Run(context.Background(), cfg, &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{DisableKeyboard: true}); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	openOnlyPath := mustFindOne(t, filepath.Join(tmp, "opened_results-*.csv"))
	openOnlyBytes, err := os.ReadFile(openOnlyPath)
	if err != nil {
		t.Fatalf("read opened_results.csv failed: %v", err)
	}
	openOnly := string(openOnlyBytes)
	if !strings.Contains(openOnly, ",open,") {
		t.Fatalf("expected at least one open record, got: %s", openOnly)
	}
	if strings.Contains(openOnly, ",close,") || strings.Contains(openOnly, "close(timeout)") {
		t.Fatalf("opened_results.csv must include open records only, got: %s", openOnly)
	}
}

func mustFindOne(t *testing.T, pattern string) string {
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

func lineCount(data string) int {
	trimmed := strings.TrimSpace(data)
	if trimmed == "" {
		return 0
	}
	return len(strings.Split(trimmed, "\n"))
}

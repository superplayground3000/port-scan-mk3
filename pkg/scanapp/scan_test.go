package scanapp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
	"github.com/xuxiping/port-scan-mk3/pkg/input"
	"github.com/xuxiping/port-scan-mk3/pkg/ratelimit"
	"github.com/xuxiping/port-scan-mk3/pkg/speedctrl"
	"github.com/xuxiping/port-scan-mk3/pkg/state"
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

func TestRun_WhenResumeStateFileProvided_ContinuesFromNextIndex(t *testing.T) {
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
	outFile := filepath.Join(tmp, "out.csv")
	resumeFile := filepath.Join(tmp, "resume.json")

	if err := os.WriteFile(cidrFile, []byte("fab_name,ip,ip_cidr,cidr_name\nfab1,127.0.0.1,127.0.0.1/32,loopback\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portFile, []byte(strconv.Itoa(openPort)+"/tcp\n1/tcp\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	initial := []task.Chunk{{
		CIDR:         "127.0.0.1/32",
		CIDRName:     "loopback",
		Ports:        []string{strconv.Itoa(openPort) + "/tcp", "1/tcp"},
		NextIndex:    1,
		ScannedCount: 1,
		TotalCount:   2,
		Status:       "scanning",
	}}
	if err := state.Save(resumeFile, initial); err != nil {
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
		Resume:             resumeFile,
		LogLevel:           "error",
		PreScanPingTimeout: 100 * time.Millisecond,
	}
	if err := Run(context.Background(), cfg, &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{DisableKeyboard: true}); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	scanOutputPath := mustFindOne(t, filepath.Join(tmp, "scan_results-*.csv"))
	data, err := os.ReadFile(scanOutputPath)
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	if !strings.Contains(out, "127.0.0.1,127.0.0.1/32,1,close") && !strings.Contains(out, "127.0.0.1,127.0.0.1/32,1,close(timeout)") {
		t.Fatalf("expected resumed scan row, got: %s", out)
	}
	if strings.Contains(out, "127.0.0.1,127.0.0.1/32,"+strconv.Itoa(openPort)+",open") {
		t.Fatalf("did not expect already-scanned port row, got: %s", out)
	}
}

func TestRun_WhenPressureAPIFailsThreeTimes_ReturnsFatalErrorAndSavesResumeState(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "fail", http.StatusInternalServerError)
	}))
	defer api.Close()

	tmp := t.TempDir()
	cidrFile := filepath.Join(tmp, "cidr.csv")
	portFile := filepath.Join(tmp, "ports.csv")
	outFile := filepath.Join(tmp, "out.csv")
	resumeFile := filepath.Join(tmp, "resume_state.json")

	if err := os.WriteFile(cidrFile, []byte("fab_name,ip,ip_cidr,cidr_name\nfab1,127.0.0.0/24,127.0.0.0/24,loopback\n"), 0o644); err != nil {
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
		Delay:            0,
		BucketRate:       1,
		BucketCapacity:   1,
		Workers:          1,
		PressureAPI:      api.URL,
		PressureInterval: 5 * time.Millisecond,
		DisableAPI:       false,
		LogLevel:         "error",
	}
	// Decision B: scan requires a bucket file. ResumeStatePath still directs where
	// the fatal-error resume snapshot is written (it wins over cfg.Resume).
	cfg.Resume = generateBucketFile(t, cfg, filepath.Join(tmp, "buckets.json"), "")

	err := Run(context.Background(), cfg, &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{
		DisableKeyboard: true,
		ResumeStatePath: resumeFile,
		PressureHTTP:    &http.Client{Timeout: 500 * time.Millisecond},
	})
	if err == nil {
		t.Fatal("expected api failure error")
	}
	if !strings.Contains(err.Error(), "pressure api failed 3 times") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, statErr := os.Stat(resumeFile); statErr != nil {
		t.Fatalf("expected resume state on fatal api error, got: %v", statErr)
	}
	// Decision B: scan persists the snapshot's pre-scan-ping metadata (stamped by
	// generate-buckets) unchanged, so a subsequent resume keeps the blocklist.
	persisted, loadErr := state.LoadSnapshot(resumeFile)
	if loadErr != nil {
		t.Fatalf("expected loadable persisted snapshot, got: %v", loadErr)
	}
	if !persisted.PreScanPing.Enabled {
		t.Fatalf("expected persisted snapshot to preserve pre-scan-ping metadata, got %+v", persisted.PreScanPing)
	}
}

func TestRun_WhenSnapshotBlocklistPresent_BlocksUnreachableIPsWithoutChecker(t *testing.T) {
	tmp := t.TempDir()
	cidrFile := filepath.Join(tmp, "cidr.csv")
	portFile := filepath.Join(tmp, "ports.csv")
	outFile := filepath.Join(tmp, "out.csv")
	resumeFile := filepath.Join(tmp, "resume.json")

	if err := os.WriteFile(cidrFile, []byte("fab_name,ip,ip_cidr,cidr_name\nfab1,10.0.0.1,10.0.0.1/32,blocked\nfab2,127.0.0.1,127.0.0.1/32,loopback\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portFile, []byte("1/tcp\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := state.SaveSnapshot(resumeFile, state.Snapshot{
		Chunks: []task.Chunk{{
			CIDR:       "127.0.0.1/32",
			CIDRName:   "loopback",
			Ports:      []string{"1/tcp"},
			TotalCount: 1,
			Status:     "pending",
		}},
		PreScanPing: state.PreScanPingState{
			Enabled:            true,
			TimeoutMS:          100,
			UnreachableIPv4U32: []uint32{ipv4ToUint32("10.0.0.1")},
		},
	}); err != nil {
		t.Fatal(err)
	}

	checker := &fakeRunReachabilityChecker{}
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
		Resume:           resumeFile,
		LogLevel:         "error",
	}

	if err := Run(context.Background(), cfg, &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{
		Dial: func(context.Context, string, string) (net.Conn, error) {
			return nil, dialErrnoFailure(syscall.ECONNREFUSED)
		},
		DisableKeyboard:     true,
		ReachabilityChecker: checker,
	}); err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if calls := checker.calls(); len(calls) != 0 {
		t.Fatalf("expected scan-only run to never invoke the checker, got %v", calls)
	}

	// Decision B: scan does not write the unreachable CSV (that is pre-ping's
	// artifact). It only excludes the snapshot's blocklisted IPs from scanning.
	if matches, _ := filepath.Glob(filepath.Join(tmp, "unreachable_results-*.csv")); len(matches) != 0 {
		t.Fatalf("scan must not write unreachable_results CSV, got %v", matches)
	}

	scanPath := mustFindOne(t, filepath.Join(tmp, "scan_results-*.csv"))
	scanData, err := os.ReadFile(scanPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(scanData), "10.0.0.1") {
		t.Fatalf("did not expect saved unreachable ip in scan output, got %s", string(scanData))
	}
	if !strings.Contains(string(scanData), "127.0.0.1,127.0.0.1/32,1,close") {
		t.Fatalf("expected reachable resume chunk to scan, got %s", string(scanData))
	}
}

func TestRun_WhenResumeSnapshotPreScanStateAndContextCanceled_AbortsWithoutWritingOutputs(t *testing.T) {
	tmp := t.TempDir()
	cidrFile := filepath.Join(tmp, "cidr.csv")
	portFile := filepath.Join(tmp, "ports.csv")
	outFile := filepath.Join(tmp, "out.csv")
	resumeFile := filepath.Join(tmp, "resume.json")

	if err := os.WriteFile(cidrFile, []byte("fab_name,ip,ip_cidr,cidr_name\nfab1,10.0.0.1,10.0.0.1/32,blocked\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portFile, []byte("1/tcp\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := state.SaveSnapshot(resumeFile, state.Snapshot{
		Chunks: []task.Chunk{{
			CIDR:       "10.0.0.1/32",
			CIDRName:   "blocked",
			Ports:      []string{"1/tcp"},
			TotalCount: 1,
			Status:     "pending",
		}},
		PreScanPing: state.PreScanPingState{
			Enabled:            true,
			TimeoutMS:          100,
			UnreachableIPv4U32: []uint32{ipv4ToUint32("10.0.0.1")},
		},
	}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	dialCount := 0
	checker := &fakeRunReachabilityChecker{}
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
		Resume:           resumeFile,
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
		t.Fatalf("expected canceled saved pre-scan to skip tcp dials, got %d", dialCount)
	}
	if calls := checker.calls(); len(calls) != 0 {
		t.Fatalf("expected saved pre-scan state to skip checker even on cancel, got %v", calls)
	}
	if matches, globErr := filepath.Glob(filepath.Join(tmp, "unreachable_results-*.csv")); globErr != nil {
		t.Fatalf("unexpected glob error: %v", globErr)
	} else if len(matches) != 0 {
		t.Fatalf("expected no finalized unreachable output on canceled saved pre-scan, got %v", matches)
	}
	if matches, globErr := filepath.Glob(filepath.Join(tmp, "scan_results-*.csv")); globErr != nil {
		t.Fatalf("unexpected glob error: %v", globErr)
	} else if len(matches) != 0 {
		t.Fatalf("expected no finalized scan output on canceled saved pre-scan, got %v", matches)
	}
}

func TestRun_WhenResumeReusesChunksAndBroadcastExclusionChangesTotal_FailsWithClearError(t *testing.T) {
	// -disable-pre-scan-ping makes shouldUseResumeChunks return true, so the saved
	// chunks are reused unchanged. A snapshot written before broadcast exclusion
	// carries the old (inclusive) TotalCount; the runtime now expects one fewer
	// target and must fail with an actionable "start fresh" message rather than a
	// silent mismatch.
	tmp := t.TempDir()
	cidrFile := filepath.Join(tmp, "cidr.csv")
	portFile := filepath.Join(tmp, "ports.csv")
	resumeFile := filepath.Join(tmp, "resume.json")

	if err := os.WriteFile(cidrFile, []byte("fab_name,ip,ip_cidr,cidr_name\nfab1,10.0.0.0/30,10.0.0.0/30,seg\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portFile, []byte("1/tcp\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := state.Save(resumeFile, []task.Chunk{{
		CIDR:         "10.0.0.0/30",
		CIDRName:     "seg",
		Ports:        []string{"1/tcp"},
		NextIndex:    1,
		ScannedCount: 1,
		TotalCount:   4, // pre-broadcast-exclusion count (now expected: 3)
		Status:       "scanning",
	}}); err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{
		CIDRFile:           cidrFile,
		PortFile:           portFile,
		Output:             filepath.Join(tmp, "out.csv"),
		Timeout:            20 * time.Millisecond,
		BucketRate:         100,
		BucketCapacity:     100,
		Workers:            1,
		PressureInterval:   5 * time.Second,
		DisableAPI:         true,
		DisablePreScanPing: true,
		Resume:             resumeFile,
		LogLevel:           "error",
	}

	err := Run(context.Background(), cfg, &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{
		Dial:            func(context.Context, string, string) (net.Conn, error) { return nil, errors.New("dial") },
		DisableKeyboard: true,
	})
	if err == nil {
		t.Fatal("expected resume-incompatibility error, got nil")
	}
	if !strings.Contains(err.Error(), "incompatible with the current target set") ||
		!strings.Contains(err.Error(), "fresh scan") {
		t.Fatalf("expected actionable resume message, got: %v", err)
	}
}

func TestRun_WhenResumeCIDRIsEntirelyBroadcast_FailsWithClearError(t *testing.T) {
	// Edge case: a resumed CIDR whose only target is its boundary broadcast now
	// filters to an empty group, so the reused chunk's CIDR is absent from the
	// rebuilt groups. That must still yield the actionable "start fresh" guidance,
	// not an opaque "not found in cidr file" error.
	tmp := t.TempDir()
	cidrFile := filepath.Join(tmp, "cidr.csv")
	portFile := filepath.Join(tmp, "ports.csv")
	resumeFile := filepath.Join(tmp, "resume.json")

	if err := os.WriteFile(cidrFile, []byte("fab_name,ip,ip_cidr,cidr_name\nfab1,10.0.0.255,10.0.0.0/24,seg\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portFile, []byte("1/tcp\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := state.Save(resumeFile, []task.Chunk{{
		CIDR:         "10.0.0.0/24",
		CIDRName:     "seg",
		Ports:        []string{"1/tcp"},
		NextIndex:    0,
		ScannedCount: 0,
		TotalCount:   1, // the broadcast was a target before 1.4.0
		Status:       "scanning",
	}}); err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{
		CIDRFile:           cidrFile,
		PortFile:           portFile,
		Output:             filepath.Join(tmp, "out.csv"),
		Timeout:            20 * time.Millisecond,
		BucketRate:         100,
		BucketCapacity:     100,
		Workers:            1,
		PressureInterval:   5 * time.Second,
		DisableAPI:         true,
		DisablePreScanPing: true,
		Resume:             resumeFile,
		LogLevel:           "error",
	}

	err := Run(context.Background(), cfg, &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{
		Dial:            func(context.Context, string, string) (net.Conn, error) { return nil, errors.New("dial") },
		DisableKeyboard: true,
	})
	if err == nil {
		t.Fatal("expected clear resume error, got nil")
	}
	if !strings.Contains(err.Error(), "no scannable targets") || !strings.Contains(err.Error(), "fresh scan") {
		t.Fatalf("expected actionable resume message, got: %v", err)
	}
}

func TestRun_WhenAllTargetsBlocklisted_SucceedsWithHeaderOnlyScanOutputs(t *testing.T) {
	tmp := t.TempDir()
	cidrFile := filepath.Join(tmp, "cidr.csv")
	portFile := filepath.Join(tmp, "ports.csv")
	outFile := filepath.Join(tmp, "out.csv")
	unreachableFile := filepath.Join(tmp, "unreachable.csv")

	if err := os.WriteFile(cidrFile, []byte("fab_name,ip,ip_cidr,cidr_name\nfab1,10.0.0.1,10.0.0.1/32,blocked\nfab2,10.0.0.2,10.0.0.2/32,blocked-2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portFile, []byte("1/tcp\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Both targets blocklisted → generate-buckets yields zero chunks, so scan has
	// nothing to dispatch and produces header-only outputs.
	if err := os.WriteFile(unreachableFile, []byte("ip,ip_cidr,status\n10.0.0.1,10.0.0.1/32,unreachable\n10.0.0.2,10.0.0.2/32,unreachable\n"), 0o644); err != nil {
		t.Fatal(err)
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
	cfg.Resume = generateBucketFile(t, cfg, filepath.Join(tmp, "buckets.json"), unreachableFile)

	if err := Run(context.Background(), cfg, &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{
		Dial: func(context.Context, string, string) (net.Conn, error) {
			dialCount++
			return nil, errors.New("unexpected dial")
		},
		DisableKeyboard: true,
	}); err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if dialCount != 0 {
		t.Fatalf("expected no tcp dials for all-blocklisted run, got %d", dialCount)
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

	// Scan no longer writes the unreachable CSV under decision B.
	if matches, _ := filepath.Glob(filepath.Join(tmp, "unreachable_results-*.csv")); len(matches) != 0 {
		t.Fatalf("scan must not write unreachable_results CSV, got %v", matches)
	}
}

func TestRun_WhenRichAllTargetsBlocklisted_SucceedsWithoutDispatchingTCP(t *testing.T) {
	tmp := t.TempDir()
	cidrFile := filepath.Join(tmp, "rich.csv")
	outFile := filepath.Join(tmp, "out.csv")
	unreachableFile := filepath.Join(tmp, "unreachable.csv")

	if err := os.WriteFile(cidrFile, []byte(
		"src_ip,src_network_segment,dst_ip,dst_network_segment,service_label,protocol,port,decision,matched_policy_id,reason\n"+
			"10.1.0.10,10.1.0.0/24,10.0.0.9,10.0.0.0/24,svc-a,tcp,443,accept,P-1,allow\n"+
			"10.1.1.11,10.1.1.0/24,10.0.0.9,10.0.0.0/24,svc-b,tcp,443,accept,P-2,allow\n",
	), 0o644); err != nil {
		t.Fatal(err)
	}
	// Both rich records target 10.0.0.9; blocklisting it yields zero chunks.
	if err := os.WriteFile(unreachableFile, []byte("ip,ip_cidr,status\n10.0.0.9,10.0.0.0/24,unreachable\n"), 0o644); err != nil {
		t.Fatal(err)
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
	cfg.Resume = generateBucketFile(t, cfg, filepath.Join(tmp, "buckets.json"), unreachableFile)

	if err := Run(context.Background(), cfg, &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{
		Dial: func(context.Context, string, string) (net.Conn, error) {
			dialCount++
			return nil, errors.New("unexpected dial")
		},
		DisableKeyboard: true,
	}); err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if dialCount != 0 {
		t.Fatalf("expected rich all-blocklisted run to skip tcp dials, got %d", dialCount)
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

	// Scan no longer writes the unreachable CSV under decision B.
	if matches, _ := filepath.Glob(filepath.Join(tmp, "unreachable_results-*.csv")); len(matches) != 0 {
		t.Fatalf("scan must not write unreachable_results CSV, got %v", matches)
	}
}

func TestFetchPressure_WhenResponseShapesVary_ReturnsParsedPressureOrError(t *testing.T) {
	okAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintln(w, `{"pressure":95}`)
	}))
	defer okAPI.Close()

	n, err := fetchPressure(&http.Client{Timeout: time.Second}, okAPI.URL)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if n != 95.0 {
		t.Fatalf("unexpected pressure: %.1f", n)
	}

	strAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintln(w, `{"pressure":"88"}`)
	}))
	defer strAPI.Close()

	n, err = fetchPressure(&http.Client{Timeout: time.Second}, strAPI.URL)
	if err != nil || n != 88.0 {
		t.Fatalf("unexpected parse result n=%.1f err=%v", n, err)
	}

	badAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "fail", http.StatusInternalServerError)
	}))
	defer badAPI.Close()
	if _, err := fetchPressure(&http.Client{Timeout: time.Second}, badAPI.URL); err == nil {
		t.Fatal("expected status error")
	}
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
	// /30 expands to 3 targets (broadcast excluded) x 1 default port => 3.
	if rts[0].state.TotalCount != 3 {
		t.Fatalf("unexpected total count: %d", rts[0].state.TotalCount)
	}
}

func TestShouldSaveOnDispatchErr_WhenDispatchErrorVaries_ReturnsExpectedDecision(t *testing.T) {
	if shouldSaveOnDispatchErr(nil) {
		t.Fatal("expected false for nil err")
	}
	if !shouldSaveOnDispatchErr(context.Canceled) {
		t.Fatal("expected true for context canceled")
	}
	if !shouldSaveOnDispatchErr(context.DeadlineExceeded) {
		t.Fatal("expected true for deadline exceeded")
	}
	if shouldSaveOnDispatchErr(errors.New("other")) {
		t.Fatal("expected false for other err")
	}
}

func TestPersistResumeState_WhenRuntimeIncomplete_SavesResumeSnapshot(t *testing.T) {
	tmp := t.TempDir()
	resumeFile := filepath.Join(tmp, "resume.json")
	logger := newLogger("error", false, &bytes.Buffer{})
	ch := &task.Chunk{
		CIDR:         "10.0.0.0/24",
		NextIndex:    2,
		ScannedCount: 2,
		TotalCount:   4,
		Status:       "scanning",
	}
	runtimes := []*chunkRuntime{{
		state:   ch,
		tracker: newChunkStateTracker(ch),
	}}

	if err := persistResumeState(config.Config{}, RunOptions{ResumeStatePath: resumeFile}, logger, runtimes, nil, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	chunks, err := state.Load(resumeFile)
	if err != nil {
		t.Fatalf("expected saved resume state, got %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 saved chunk, got %d", len(chunks))
	}
	if chunks[0].NextIndex != 2 || chunks[0].ScannedCount != 2 || chunks[0].Status != "scanning" {
		t.Fatalf("unexpected saved chunk state: %+v", chunks[0])
	}
}

func TestPersistResumeSnapshot_WhenPreScanStateProvided_SavesEnvelope(t *testing.T) {
	tmp := t.TempDir()
	resumeFile := filepath.Join(tmp, "resume.json")
	logger := newLogger("error", false, &bytes.Buffer{})
	ch := &task.Chunk{
		CIDR:         "10.0.0.0/24",
		NextIndex:    1,
		ScannedCount: 1,
		TotalCount:   4,
		Status:       "scanning",
	}
	runtimes := []*chunkRuntime{{
		state:   ch,
		tracker: newChunkStateTracker(ch),
	}}

	preScanPing := state.PreScanPingState{
		Enabled:            true,
		TimeoutMS:          100,
		UnreachableIPv4U32: []uint32{ipv4ToUint32("10.0.0.7")},
	}
	if err := persistResumeSnapshot(config.Config{}, RunOptions{ResumeStatePath: resumeFile}, logger, runtimes, preScanPing, nil, nil, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	snapshot, err := state.LoadSnapshot(resumeFile)
	if err != nil {
		t.Fatalf("expected saved snapshot, got %v", err)
	}
	if len(snapshot.Chunks) != 1 || snapshot.Chunks[0].NextIndex != 1 {
		t.Fatalf("unexpected saved chunks: %+v", snapshot.Chunks)
	}
	if !snapshot.PreScanPing.Enabled || snapshot.PreScanPing.TimeoutMS != 100 {
		t.Fatalf("unexpected pre-scan ping metadata: %+v", snapshot.PreScanPing)
	}
	if len(snapshot.PreScanPing.UnreachableIPv4U32) != 1 || snapshot.PreScanPing.UnreachableIPv4U32[0] != ipv4ToUint32("10.0.0.7") {
		t.Fatalf("unexpected unreachable ip list: %+v", snapshot.PreScanPing.UnreachableIPv4U32)
	}
}

func TestPersistResumeState_WhenRunCompletesCleanly_SkipsWrite(t *testing.T) {
	tmp := t.TempDir()
	resumeFile := filepath.Join(tmp, "resume.json")
	logger := newLogger("error", false, &bytes.Buffer{})
	ch := &task.Chunk{
		CIDR:         "10.0.0.0/24",
		NextIndex:    4,
		ScannedCount: 4,
		TotalCount:   4,
		Status:       "completed",
	}
	runtimes := []*chunkRuntime{{
		state:   ch,
		tracker: newChunkStateTracker(ch),
	}}

	if err := persistResumeState(config.Config{}, RunOptions{ResumeStatePath: resumeFile}, logger, runtimes, nil, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(resumeFile); !os.IsNotExist(err) {
		t.Fatalf("expected no resume file on clean completion, got err=%v", err)
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

func TestPollPressureAPI_WhenPressureCrossesThreshold_TogglesPauseAndLogsTransition(t *testing.T) {
	values := []int{95, 20}
	var mu sync.Mutex
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		v := values[0]
		if len(values) > 1 {
			values = values[1:]
		}
		mu.Unlock()
		_, _ = fmt.Fprintf(w, `{"pressure":%d}`, v)
	}))
	defer api.Close()

	cfg := config.Config{
		PressureAPI:      api.URL,
		PressureInterval: 5 * time.Millisecond,
	}
	ctrl := speedctrl.NewController()
	logOut := &lockedBuffer{}
	logger := newLogger("info", false, logOut)
	errCh := make(chan error, 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go pollPressureAPI(ctx, cfg, RunOptions{PressureLimit: 90, PressureHTTP: &http.Client{Timeout: time.Second}}, ctrl, logger, errCh)

	time.Sleep(40 * time.Millisecond)
	cancel()
	time.Sleep(10 * time.Millisecond)

	select {
	case err := <-errCh:
		t.Fatalf("unexpected err: %v", err)
	default:
	}
	if ctrl.IsPaused() {
		t.Fatal("expected resumed after pressure drop")
	}
	if !strings.Contains(logOut.String(), "scan automatically paused") || !strings.Contains(logOut.String(), "scan automatically resumed") {
		t.Fatalf("expected pause/resume logs, got: %s", logOut.String())
	}
}

func TestDispatchTasks_WhenRuntimeReady_EmitsTasksAndAdvancesNextIndex(t *testing.T) {
	ctrl := speedctrl.NewController()
	logOut := &lockedBuffer{}
	logger := newLogger("debug", false, logOut)
	bucket := ratelimit.NewLeakyBucket(100, 100)
	defer bucket.Close()

	ch := &task.Chunk{CIDR: "10.0.0.0/24", TotalCount: 4, Status: "pending"}
	rt := &chunkRuntime{
		ipCidr: "10.0.0.0/24",
		ports:  []int{80, 443},
		targets: []scanTarget{
			{ip: "10.0.0.1", ipCidr: "10.0.0.0/24"},
			{ip: "10.0.0.2", ipCidr: "10.0.0.0/24"},
		},
		state:   ch,
		tracker: newChunkStateTracker(ch),
		bkt:     bucket,
	}
	taskCh := make(chan scanTask, 8)

	err := dispatchTasks(context.Background(), dispatchPolicy{delay: 0}, ctrl, logger, []*chunkRuntime{rt}, taskCh)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	snap := rt.tracker.Snapshot()
	if snap.NextIndex != 4 {
		t.Fatalf("expected next index 4, got %d", snap.NextIndex)
	}
	if snap.Status != "scanning" {
		t.Fatalf("expected scanning status during dispatch, got %s", snap.Status)
	}
	if len(taskCh) != 4 {
		t.Fatalf("expected 4 queued tasks, got %d", len(taskCh))
	}
}

func TestDispatchTasks_WhenPausedDuringDispatch_DoesNotLeakTokensBeforeGate(t *testing.T) {
	ctrl := speedctrl.NewController()
	logOut := &lockedBuffer{}
	logger := newLogger("debug", false, logOut)
	bucket := ratelimit.NewLeakyBucket(100, 100)
	defer bucket.Close()

	rt := &chunkRuntime{
		ipCidr: "10.0.0.0/24",
		ports:  []int{80},
		targets: []scanTarget{
			{ip: "10.0.0.1", ipCidr: "10.0.0.0/24"},
			{ip: "10.0.0.2", ipCidr: "10.0.0.0/24"},
		},
		state:   &task.Chunk{CIDR: "10.0.0.0/24", TotalCount: 2, Status: "pending"},
		tracker: newChunkStateTracker(&task.Chunk{CIDR: "10.0.0.0/24", TotalCount: 2, Status: "pending"}),
		bkt:     bucket,
	}
	taskCh := make(chan scanTask, 4)

	// Pause immediately, then unpause after short delay
	ctrl.SetAPIPaused(true)
	go func() {
		time.Sleep(20 * time.Millisecond)
		ctrl.SetAPIPaused(false)
	}()

	err := dispatchTasks(context.Background(), dispatchPolicy{delay: 0}, ctrl, logger, []*chunkRuntime{rt}, taskCh)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(taskCh) != 2 {
		t.Fatalf("expected 2 tasks dispatched, got %d", len(taskCh))
	}
}

func TestStartManualPauseMonitor_WhenManualPauseChanges_LogsStateTransitions(t *testing.T) {
	ctrl := speedctrl.NewController()
	out := &lockedBuffer{}
	logger := newLogger("info", false, out)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	startManualPauseMonitor(ctx, ctrl, logger)
	time.Sleep(50 * time.Millisecond)
	ctrl.SetManualPaused(true)
	time.Sleep(250 * time.Millisecond)
	ctrl.SetManualPaused(false)
	time.Sleep(250 * time.Millisecond)
	cancel()
	time.Sleep(50 * time.Millisecond)

	logs := out.String()
	if !strings.Contains(logs, "scan manually paused") || !strings.Contains(logs, "scan manually resumed") {
		t.Fatalf("expected manual pause/resume logs, got: %s", logs)
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
	cfg.Resume = generateBucketFile(t, cfg, filepath.Join(tmp, "buckets.json"), "")
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
	cfg.Resume = generateBucketFile(t, cfg, filepath.Join(tmp, "buckets.json"), "")
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
	cfg.Resume = generateBucketFile(t, cfg, filepath.Join(tmp, "buckets.json"), "")
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

func TestRun_WhenScanCompletes_DoesNotWriteResumeState(t *testing.T) {
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
	resumeFile := filepath.Join(tmp, "resume_state.json")

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
		CIDRFile:         cidrFile,
		PortFile:         portFile,
		Output:           outFile,
		Timeout:          100 * time.Millisecond,
		Delay:            0,
		BucketRate:       100,
		BucketCapacity:   100,
		Workers:          1,
		PressureInterval: 5 * time.Second,
		DisableAPI:       true,
		LogLevel:         "error",
	}
	cfg.Resume = generateBucketFile(t, cfg, filepath.Join(tmp, "buckets.json"), "")
	if err := Run(context.Background(), cfg, &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{
		DisableKeyboard: true,
		ResumeStatePath: resumeFile,
	}); err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if _, statErr := os.Stat(resumeFile); !os.IsNotExist(statErr) {
		t.Fatalf("expected no resume file on successful completion, got err=%v", statErr)
	}
}

func TestRun_WhenCanceled_EmitsCanceledCompletionSummaryAndPersistsResume(t *testing.T) {
	tmp := t.TempDir()
	cidrFile := filepath.Join(tmp, "cidr.csv")
	portFile := filepath.Join(tmp, "ports.csv")
	outFile := filepath.Join(tmp, "scan_results.csv")
	resumeFile := filepath.Join(tmp, "resume_state.json")

	if err := os.WriteFile(cidrFile, []byte("fab_name,ip,ip_cidr,cidr_name\nfab1,127.0.0.0/24,127.0.0.0/24,loopback\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portFile, []byte("1/tcp\n2/tcp\n3/tcp\n4/tcp\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{
		CIDRFile:         cidrFile,
		PortFile:         portFile,
		Output:           outFile,
		Timeout:          50 * time.Millisecond,
		Delay:            5 * time.Millisecond,
		BucketRate:       1,
		BucketCapacity:   1,
		Workers:          1,
		PressureInterval: 10 * time.Second,
		DisableAPI:       true,
		LogLevel:         "info",
		Format:           "json",
	}
	cfg.Resume = generateBucketFile(t, cfg, filepath.Join(tmp, "buckets.json"), "")

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()

	// A separate ResumeStatePath receives the persisted resume snapshot; it does
	// not pre-exist, so its presence proves the cancel path wrote resume state.
	// (Decision B removed the implicit default-location fallback for scan: scan
	// always has an input -resume, so persistence targets ResumeStatePath here.)
	err := Run(ctx, cfg, stdout, stderr, RunOptions{DisableKeyboard: true, ResumeStatePath: resumeFile})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled error, got %v", err)
	}
	if !strings.Contains(stderr.String(), `"state_transition":"completion_summary"`) {
		t.Fatalf("expected completion summary in logs, got %s", stderr.String())
	}
	if !strings.Contains(stderr.String(), `"error_cause":"canceled"`) {
		t.Fatalf("expected canceled error cause in logs, got %s", stderr.String())
	}
	if _, statErr := os.Stat(resumeFile); statErr != nil {
		t.Fatalf("expected persisted resume file %s, got err=%v", resumeFile, statErr)
	}
}

func TestRun_WhenCanceled_ResumeStateReflectsAllCompletedScans(t *testing.T) {
	tmp := t.TempDir()
	cidrFile := filepath.Join(tmp, "cidr.csv")
	portFile := filepath.Join(tmp, "ports.csv")
	outFile := filepath.Join(tmp, "scan_results.csv")
	resumeFile := filepath.Join(tmp, "resume.json")

	// A real local listener makes the first probe's outcome deterministic on
	// every platform. The previous target, 127.0.0.0/30, relied on Linux
	// treating all of 127.0.0.0/8 as loopback (127.0.0.2 refuses instantly);
	// Windows typically binds only 127.0.0.1, so the other addresses may behave
	// differently. 127.0.0.1/32 keeps every address well-defined, and /32 is not
	// filtered as a broadcast address (pkg/task/broadcast.go:30-32).
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start local listener: %v", err)
	}
	defer listener.Close()
	go func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			_ = conn.Close()
		}
	}()
	openPort := listener.Addr().(*net.TCPAddr).Port

	// 1 IP x 16 ports = 16 tasks, rate-limited to 2/s so the scan is guaranteed
	// to still have work queued when the cancel arrives. The listening port is
	// probed first so at least one probe completes immediately.
	if err := os.WriteFile(cidrFile, []byte("fab_name,ip,ip_cidr,cidr_name\nfab1,127.0.0.1,127.0.0.1/32,loopback\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ports := []string{strconv.Itoa(openPort) + "/tcp"}
	for p := 1; p <= 15; p++ {
		ports = append(ports, strconv.Itoa(p)+"/tcp")
	}
	if err := os.WriteFile(portFile, []byte(strings.Join(ports, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{
		CIDRFile:           cidrFile,
		PortFile:           portFile,
		Output:             outFile,
		Timeout:            50 * time.Millisecond,
		Delay:              10 * time.Millisecond,
		BucketRate:         2,
		BucketCapacity:     2,
		Workers:            2,
		PressureInterval:   10 * time.Second,
		DisableAPI:         true,
		DisablePreScanPing: true,
		LogLevel:           "error",
	}

	// Cancel on an observed event rather than after a fixed sleep: the first
	// completed dial makes "at least one scan finished" a precondition instead
	// of a bet on how fast the machine is. Once a dial has returned, its result
	// is queued and the run loop drains every queued result before exiting.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	firstDialDone := make(chan struct{})
	var dialOnce sync.Once
	dialer := &net.Dialer{}
	go func() {
		select {
		case <-firstDialDone:
		case <-time.After(30 * time.Second):
		}
		cancel()
	}()

	cfg.Resume = generateBucketFile(t, cfg, filepath.Join(tmp, "buckets.json"), "")
	_ = Run(ctx, cfg, &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{
		DisableKeyboard: true,
		ResumeStatePath: resumeFile,
		Dial: func(dialCtx context.Context, network, address string) (net.Conn, error) {
			conn, dialErr := dialer.DialContext(dialCtx, network, address)
			dialOnce.Do(func() { close(firstDialDone) })
			return conn, dialErr
		},
	})

	chunks, err := state.Load(resumeFile)
	if err != nil {
		t.Fatalf("expected resume state, got: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected at least 1 chunk in resume state")
	}

	scanned := 0
	for _, chunk := range chunks {
		scanned += chunk.ScannedCount
	}
	if scanned == 0 {
		t.Fatal("expected ScannedCount > 0 after draining in-flight results")
	}

	// The 2.1.0 durability contract is that the persisted ScannedCount accounts
	// for exactly the results that reached the output file — not merely that
	// "something was scanned". A mismatch means resume would re-scan or skip
	// work after an interruption.
	scanPath := mustFindOne(t, filepath.Join(tmp, "scan_results-*.csv"))
	data, readErr := os.ReadFile(scanPath)
	if readErr != nil {
		t.Fatalf("failed to read scan output %s: %v", scanPath, readErr)
	}
	dataRows := lineCount(string(data)) - 1 // minus the header row
	if scanned != dataRows {
		t.Fatalf("resume ScannedCount=%d does not match %d data rows written to %s:\n%s",
			scanned, dataRows, scanPath, string(data))
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

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
	"testing"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
	"github.com/xuxiping/port-scan-mk3/pkg/state"
	"github.com/xuxiping/port-scan-mk3/pkg/task"
)

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

func TestRun_WhenCanceledWithoutResumePath_SavesFallbackResumeState(t *testing.T) {
	tmp := t.TempDir()
	cidrFile := filepath.Join(tmp, "cidr.csv")
	portFile := filepath.Join(tmp, "ports.csv")
	outFile := filepath.Join(tmp, "out.csv")

	if err := os.WriteFile(cidrFile, []byte("fab_name,ip,ip_cidr,cidr_name\nfab1,127.0.0.0/24,127.0.0.0/24,loopback\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portFile, []byte("1/tcp\n2/tcp\n3/tcp\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{
		CIDRFile:           cidrFile,
		PortFile:           portFile,
		Output:             outFile,
		Timeout:            50 * time.Millisecond,
		Delay:              5 * time.Millisecond,
		BucketRate:         1,
		BucketCapacity:     1,
		Workers:            1,
		PressureInterval:   10 * time.Second,
		DisableAPI:         true,
		DisablePreScanPing: true,
		LogLevel:           "error",
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()

	err := Run(ctx, cfg, &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{DisableKeyboard: true})
	if err == nil {
		t.Fatal("expected cancellation error")
	}
	resumeFile := filepath.Join(tmp, defaultResumeStateFile)
	if _, statErr := os.Stat(resumeFile); statErr != nil {
		t.Fatalf("expected fallback resume file %s, got err=%v", resumeFile, statErr)
	}
}

func TestRun_WhenResumeSnapshotContainsPreScanState_ReusesCheckerAndBlocksSavedUnreachableIPs(t *testing.T) {
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
		Dial:                func(context.Context, string, string) (net.Conn, error) { return nil, errors.New("forced dial failure") },
		DisableKeyboard:     true,
		ReachabilityChecker: checker,
	}); err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if calls := checker.calls(); len(calls) != 0 {
		t.Fatalf("expected saved pre-scan state to skip checker, got %v", calls)
	}

	unreachablePath := mustFindOne(t, filepath.Join(tmp, "unreachable_results-*.csv"))
	unreachableData, err := os.ReadFile(unreachablePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(unreachableData), "10.0.0.1,10.0.0.1/32,unreachable") {
		t.Fatalf("expected saved unreachable ip to be written, got %s", string(unreachableData))
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

func TestRun_WhenLegacyResumeAndCurrentPreScanFiltersUnreachable_SucceedsWithoutChunkMismatch(t *testing.T) {
	tmp := t.TempDir()
	cidrFile := filepath.Join(tmp, "cidr.csv")
	portFile := filepath.Join(tmp, "ports.csv")
	outFile := filepath.Join(tmp, "out.csv")
	resumeFile := filepath.Join(tmp, "resume.json")

	if err := os.WriteFile(cidrFile, []byte("fab_name,ip,ip_cidr,cidr_name\nfab1,127.0.0.0/30,127.0.0.0/30,loopback\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portFile, []byte("1/tcp\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := state.Save(resumeFile, []task.Chunk{{
		CIDR:         "127.0.0.0/30",
		CIDRName:     "loopback",
		Ports:        []string{"1/tcp"},
		NextIndex:    1,
		ScannedCount: 1,
		TotalCount:   4,
		Status:       "scanning",
	}}); err != nil {
		t.Fatal(err)
	}

	checker := &fakeRunReachabilityChecker{
		results: map[string]ReachabilityResult{
			"127.0.0.1": {IP: "127.0.0.1", Reachable: false},
		},
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
		Resume:           resumeFile,
		LogLevel:         "error",
	}

	if err := Run(context.Background(), cfg, &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{
		Dial:                func(context.Context, string, string) (net.Conn, error) { return nil, errors.New("forced dial failure") },
		DisableKeyboard:     true,
		ReachabilityChecker: checker,
	}); err != nil {
		t.Fatalf("expected legacy resume to continue without chunk mismatch, got %v", err)
	}

	scanPath := mustFindOne(t, filepath.Join(tmp, "scan_results-*.csv"))
	scanData, err := os.ReadFile(scanPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(scanData), "127.0.0.1") {
		t.Fatalf("did not expect filtered unreachable ip in scan output, got %s", string(scanData))
	}
	if lineCount(string(scanData)) != 4 {
		t.Fatalf("expected header plus three scanned rows after filtering, got %s", string(scanData))
	}

	unreachablePath := mustFindOne(t, filepath.Join(tmp, "unreachable_results-*.csv"))
	unreachableData, err := os.ReadFile(unreachablePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(unreachableData), "127.0.0.1,127.0.0.0/30,unreachable") {
		t.Fatalf("expected unreachable row for filtered legacy resume ip, got %s", string(unreachableData))
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
	if err := persistResumeSnapshot(config.Config{}, RunOptions{ResumeStatePath: resumeFile}, logger, runtimes, preScanPing, nil, nil); err != nil {
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

func TestRun_WhenCanceled_EmitsCanceledCompletionSummaryAndFallbackResume(t *testing.T) {
	tmp := t.TempDir()
	cidrFile := filepath.Join(tmp, "cidr.csv")
	portFile := filepath.Join(tmp, "ports.csv")
	outFile := filepath.Join(tmp, "scan_results.csv")

	if err := os.WriteFile(cidrFile, []byte("fab_name,ip,ip_cidr,cidr_name\nfab1,127.0.0.0/24,127.0.0.0/24,loopback\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portFile, []byte("1/tcp\n2/tcp\n3/tcp\n4/tcp\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{
		CIDRFile:           cidrFile,
		PortFile:           portFile,
		Output:             outFile,
		Timeout:            50 * time.Millisecond,
		Delay:              5 * time.Millisecond,
		BucketRate:         1,
		BucketCapacity:     1,
		Workers:            1,
		PressureInterval:   10 * time.Second,
		DisableAPI:         true,
		DisablePreScanPing: true,
		LogLevel:           "info",
		Format:             "json",
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()

	err := Run(ctx, cfg, stdout, stderr, RunOptions{DisableKeyboard: true})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled error, got %v", err)
	}
	if !strings.Contains(stderr.String(), `"state_transition":"completion_summary"`) {
		t.Fatalf("expected completion summary in logs, got %s", stderr.String())
	}
	if !strings.Contains(stderr.String(), `"error_cause":"canceled"`) {
		t.Fatalf("expected canceled error cause in logs, got %s", stderr.String())
	}
	resumeFile := filepath.Join(tmp, defaultResumeStateFile)
	if _, statErr := os.Stat(resumeFile); statErr != nil {
		t.Fatalf("expected fallback resume file %s, got err=%v", resumeFile, statErr)
	}
}

func TestRun_WhenCanceled_ResumeStateReflectsAllCompletedScans(t *testing.T) {
	tmp := t.TempDir()
	cidrFile := filepath.Join(tmp, "cidr.csv")
	portFile := filepath.Join(tmp, "ports.csv")
	outFile := filepath.Join(tmp, "scan_results.csv")
	resumeFile := filepath.Join(tmp, "resume.json")

	// 4 IPs x 4 ports = 16 tasks, slow enough to cancel mid-scan
	if err := os.WriteFile(cidrFile, []byte("fab_name,ip,ip_cidr,cidr_name\nfab1,127.0.0.0/30,127.0.0.0/30,loopback\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portFile, []byte("1/tcp\n2/tcp\n3/tcp\n4/tcp\n"), 0o644); err != nil {
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

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(80 * time.Millisecond)
		cancel()
	}()

	_ = Run(ctx, cfg, &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{
		DisableKeyboard: true,
		ResumeStatePath: resumeFile,
	})

	chunks, err := state.Load(resumeFile)
	if err != nil {
		t.Fatalf("expected resume state, got: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected at least 1 chunk in resume state")
	}
	// ScannedCount should be > 0 (workers completed some scans before drain)
	if chunks[0].ScannedCount == 0 {
		t.Fatal("expected ScannedCount > 0 after draining in-flight results")
	}
}

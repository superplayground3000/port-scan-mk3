package scanapp

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xuxiping/port-scan-mk3/internal/testkit"
	"github.com/xuxiping/port-scan-mk3/pkg/pressure"
	"github.com/xuxiping/port-scan-mk3/pkg/speedctrl"
	"github.com/xuxiping/port-scan-mk3/pkg/state"
	"github.com/xuxiping/port-scan-mk3/pkg/task"
	"github.com/xuxiping/port-scan-mk3/pkg/writer"
)

type dashboardSnapshotRecorder struct {
	mu    sync.Mutex
	snaps []dashboardSnapshot

	onRender func(dashboardSnapshot)
}

func (r *dashboardSnapshotRecorder) Render(_ io.Writer, snap dashboardSnapshot) error {
	r.mu.Lock()
	r.snaps = append(r.snaps, snap)
	onRender := r.onRender
	r.mu.Unlock()

	if onRender != nil {
		onRender(snap)
	}
	return nil
}

func (r *dashboardSnapshotRecorder) snapshots() []dashboardSnapshot {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]dashboardSnapshot, len(r.snaps))
	copy(out, r.snaps)
	return out
}

type scriptedPressureResult struct {
	pressure float64
	err      error
}

type scriptedPressureSource struct {
	mu      sync.Mutex
	results []scriptedPressureResult
}

func (f *scriptedPressureSource) Sample(context.Context) (pressure.Sample, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if len(f.results) == 0 {
		return pressure.Sample{}, errors.New("no scripted pressure results configured")
	}
	result := f.results[0]
	if len(f.results) > 1 {
		f.results = f.results[1:]
	}
	return pressure.Sample{Maximum: result.pressure}, result.err
}

type scriptedSourcePressureResult struct {
	pressure float64
	sources  []pressure.SourceResult
	err      error
}

type scriptedMultiPressureSource struct {
	mu      sync.Mutex
	results []scriptedSourcePressureResult
}

func (f *scriptedMultiPressureSource) Sample(context.Context) (pressure.Sample, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if len(f.results) == 0 {
		return pressure.Sample{}, errors.New("no scripted source pressure results configured")
	}
	result := f.results[0]
	if len(f.results) > 1 {
		f.results = f.results[1:]
	}
	return pressure.Sample{Maximum: result.pressure, Sources: result.sources}, result.err
}

type pressureTelemetryRecorder struct {
	mu    sync.Mutex
	polls []pressurePoll
}

type controllerTelemetryRecorder struct {
	mu        sync.Mutex
	callbacks int
	statuses  []string
}

func (r *controllerTelemetryRecorder) OnController(manualPaused, apiPaused bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.callbacks++
	r.statuses = append(r.statuses, dashboardControllerStatus(manualPaused, apiPaused))
}

func (r *pressureTelemetryRecorder) OnPressurePoll(poll pressurePoll) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.polls = append(r.polls, poll)
}

func TestRun_WhenObservabilityJSONEnabled_EmitsProgressAndCompletionEvents(t *testing.T) {
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
	if err := os.WriteFile(cidrFile, []byte("fab_name,ip,ip_cidr,cidr_name\nfab1,127.0.0.1,127.0.0.1/32,loopback\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portFile, []byte(strconv.Itoa(openPort)+"/tcp\n1/tcp\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := scanConfigFixture{
		CIDRFile:         cidrFile,
		PortFile:         portFile,
		Output:           outFile,
		Timeout:          100 * time.Millisecond,
		Delay:            0,
		BucketRate:       100,
		BucketCapacity:   100,
		Workers:          1,
		Pressure:         pressureConfigFixture{Disabled: true},
		LogLevel:         "info",
		Format:           "json",
		ProgressInterval: 1,
	}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cfg.Resume = generateBucketFile(t, cfg, filepath.Join(tmp, "buckets.json"), "")
	if err := Run(context.Background(), scanConfigurationFromFixture(t, cfg), stdout, stderr, RunOptions{DisableKeyboard: true}); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	logs := stderr.String()
	for _, required := range []string{
		"\"target\"",
		"\"port\"",
		"\"state_transition\"",
		"\"error_cause\"",
		"\"state_transition\":\"progress\"",
		"\"state_transition\":\"completion_summary\"",
		"\"success\":true",
	} {
		if !strings.Contains(logs, required) {
			t.Fatalf("missing observability marker %s in logs: %s", required, logs)
		}
	}

	if !strings.Contains(stdout.String(), "progress cidr=") {
		t.Fatalf("expected progress output on stdout, got %s", stdout.String())
	}
}

func TestRun_WhenObservabilityJSONEnabled_EmitsSingleScanResultEventPerTask(t *testing.T) {
	tmp := t.TempDir()
	cidrFile := filepath.Join(tmp, "cidr.csv")
	portFile := filepath.Join(tmp, "ports.csv")
	outFile := filepath.Join(tmp, "scan_results.csv")
	if err := os.WriteFile(cidrFile, []byte("fab_name,ip,ip_cidr,cidr_name\nfab1,127.0.0.1,127.0.0.1/32,loopback\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portFile, []byte("1/tcp\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := scanConfigFixture{
		CIDRFile:         cidrFile,
		PortFile:         portFile,
		Output:           outFile,
		Timeout:          50 * time.Millisecond,
		Delay:            0,
		BucketRate:       100,
		BucketCapacity:   100,
		Workers:          1,
		Pressure:         pressureConfigFixture{Disabled: true},
		LogLevel:         "info",
		Format:           "json",
		ProgressInterval: 1,
	}

	stderr := &bytes.Buffer{}
	cfg.Resume = generateBucketFile(t, cfg, filepath.Join(tmp, "buckets.json"), "")
	err := Run(context.Background(), scanConfigurationFromFixture(t, cfg), io.Discard, stderr, RunOptions{
		DisableKeyboard: true,
		Dial: func(context.Context, string, string) (net.Conn, error) {
			return nil, errors.New("dial failed for observability test")
		},
	})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}

	logs := stderr.String()
	if got := strings.Count(logs, `"msg":"scan_result"`); got != 1 {
		t.Fatalf("expected exactly 1 scan_result event for 1 task, got %d, logs=%s", got, logs)
	}
}

func TestRun_WhenExecutorWorkerPanics_ReturnsRuntimeError(t *testing.T) {
	tmp := t.TempDir()
	cidrFile := filepath.Join(tmp, "cidr.csv")
	portFile := filepath.Join(tmp, "ports.csv")
	outFile := filepath.Join(tmp, "scan_results.csv")
	if err := os.WriteFile(cidrFile, []byte("fab_name,ip,ip_cidr,cidr_name\nfab1,127.0.0.1,127.0.0.1/32,loopback\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 3 tasks with workers=1 and queue size=2 reproduces blocked dispatch when worker exits unexpectedly.
	if err := os.WriteFile(portFile, []byte("1/tcp\n2/tcp\n3/tcp\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := scanConfigFixture{
		CIDRFile:         cidrFile,
		PortFile:         portFile,
		Output:           outFile,
		Timeout:          50 * time.Millisecond,
		Delay:            0,
		BucketRate:       100,
		BucketCapacity:   100,
		Workers:          1,
		Pressure:         pressureConfigFixture{Disabled: true},
		LogLevel:         "info",
		Format:           "json",
		ProgressInterval: 1,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	stderr := &bytes.Buffer{}
	cfg.Resume = generateBucketFile(t, cfg, filepath.Join(tmp, "buckets.json"), "")
	err := Run(ctx, scanConfigurationFromFixture(t, cfg), io.Discard, stderr, RunOptions{
		DisableKeyboard: true,
		Dial: func(context.Context, string, string) (net.Conn, error) {
			panic("boom in dial")
		},
	})
	if err == nil {
		t.Fatal("expected runtime error when executor worker panics")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected runtime panic error, got deadline_exceeded: %v", err)
	}
	if !strings.Contains(err.Error(), "executor worker panic") {
		t.Fatalf("expected panic error message, got: %v", err)
	}
}

func TestRun_WhenRichDashboardEnabled_ReceivesLiveTelemetryState(t *testing.T) {
	tmp := t.TempDir()
	cidrFile := filepath.Join(tmp, "cidr.csv")
	portFile := filepath.Join(tmp, "ports.csv")
	outFile := filepath.Join(tmp, "scan_results.csv")

	if err := os.WriteFile(cidrFile, []byte("fab_name,ip,ip_cidr,cidr_name\nfab1,127.0.0.1,127.0.0.1/32,loopback\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portFile, []byte("1/tcp\n2/tcp\n3/tcp\n4/tcp\n5/tcp\n6/tcp\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := scanConfigFixture{
		CIDRFile:         cidrFile,
		PortFile:         portFile,
		Output:           outFile,
		Timeout:          100 * time.Millisecond,
		Delay:            10 * time.Millisecond,
		BucketRate:       100,
		BucketCapacity:   100,
		Workers:          1,
		Pressure:         pressureConfigFixture{Interval: 10 * time.Millisecond},
		LogLevel:         "error",
		Format:           "human",
		ProgressInterval: 1,
	}

	recorder := &dashboardSnapshotRecorder{}
	cfg.Resume = generateBucketFile(t, cfg, filepath.Join(tmp, "buckets.json"), "")
	err := Run(context.Background(), scanConfigurationFromFixture(t, cfg), &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{
		DisableKeyboard: true,
		Dial: func(context.Context, string, string) (net.Conn, error) {
			time.Sleep(25 * time.Millisecond)
			return nil, errors.New("dial refused for test")
		},
		PressureLimit: 90,
		PressureSource: &scriptedMultiPressureSource{results: []scriptedSourcePressureResult{
			{
				pressure: 95,
				sources: []pressure.SourceResult{
					{Name: "src1", Pressure: 95},
					{Name: "src2", Pressure: 44},
				},
			},
			{
				pressure: 20,
				sources: []pressure.SourceResult{
					{Name: "src1", Pressure: 20},
					{Name: "src2", Pressure: 18},
				},
			},
			{
				pressure: 20,
				sources: []pressure.SourceResult{
					{Name: "src1", Pressure: 20},
					{Name: "src2", Pressure: 18},
				},
			},
		}},
		dashboardTerminalDetector: func(io.Writer) bool { return true },
		dashboardRefreshInterval:  10 * time.Millisecond,
		dashboardRenderer:         recorder,
	})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}

	snaps := recorder.snapshots()
	if len(snaps) == 0 {
		t.Fatal("expected dashboard snapshots during run")
	}

	var (
		sawCIDR            bool
		sawBucketStatus    bool
		sawDispatchRate    bool
		sawResultsRate     bool
		sawControllerState bool
		sawPressure        bool
		sawAPIHealth       bool
		sawAPISources      bool
	)
	for _, snap := range snaps {
		if snap.CurrentCIDR != "" {
			sawCIDR = true
		}
		switch snap.BucketStatus {
		case "waiting_bucket", "waiting_gate", "enqueued":
			sawBucketStatus = true
		}
		if snap.DispatchPerSecond > 0 {
			sawDispatchRate = true
		}
		if snap.ResultsPerSecond > 0 {
			sawResultsRate = true
		}
		switch snap.ControllerStatus {
		case "RUNNING", "PAUSED(API)", "PAUSED(MANUAL)", "PAUSED(API+MANUAL)":
			sawControllerState = true
		}
		if snap.PressurePercent > 0 && !snap.LastPressureUpdateAt.IsZero() {
			sawPressure = true
		}
		if snap.APIHealthText == "ok" {
			sawAPIHealth = true
		}
		if len(snap.APISources) == 2 && snap.APISources[0].Name == "src1" && snap.APISources[1].Name == "src2" {
			sawAPISources = true
		}
	}

	if !sawCIDR {
		t.Fatalf("expected CurrentCIDR to be populated, got snapshots=%#v", snaps)
	}
	if !sawBucketStatus {
		t.Fatalf("expected BucketStatus transition in snapshots, got %#v", snaps)
	}
	if !sawDispatchRate {
		t.Fatalf("expected DispatchPerSecond > 0 in snapshots, got %#v", snaps)
	}
	if !sawResultsRate {
		t.Fatalf("expected ResultsPerSecond > 0 in snapshots, got %#v", snaps)
	}
	if !sawControllerState {
		t.Fatalf("expected controller status snapshots, got %#v", snaps)
	}
	if !sawPressure {
		t.Fatalf("expected pressure samples with timestamp in snapshots, got %#v", snaps)
	}
	if !sawAPIHealth {
		t.Fatalf("expected API health text update in snapshots, got %#v", snaps)
	}
	if !sawAPISources {
		t.Fatalf("expected per-source API health in snapshots, got %#v", snaps)
	}
}

func TestRun_WhenResumeAndRichDashboardEnabled_ProgressStartsFromResume(t *testing.T) {
	tmp := t.TempDir()
	cidrFile := filepath.Join(tmp, "cidr.csv")
	portFile := filepath.Join(tmp, "ports.csv")
	outFile := filepath.Join(tmp, "scan_results.csv")
	resumeFile := filepath.Join(tmp, "resume.json")

	if err := os.WriteFile(cidrFile, []byte("fab_name,ip,ip_cidr,cidr_name\nfab1,127.0.0.1,127.0.0.1/32,loopback\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portFile, []byte("1/tcp\n2/tcp\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := state.Save(resumeFile, []task.Chunk{{
		CIDR:         "127.0.0.1/32",
		CIDRName:     "loopback",
		Ports:        []string{"1/tcp", "2/tcp"},
		NextIndex:    1,
		ScannedCount: 1,
		TotalCount:   2,
		Status:       "scanning",
	}}); err != nil {
		t.Fatal(err)
	}

	cfg := scanConfigFixture{
		CIDRFile:         cidrFile,
		PortFile:         portFile,
		Output:           outFile,
		Timeout:          100 * time.Millisecond,
		Delay:            0,
		BucketRate:       100,
		BucketCapacity:   100,
		Workers:          1,
		Pressure:         pressureConfigFixture{Disabled: true},
		Resume:           resumeFile,
		LogLevel:         "error",
		Format:           "human",
		ProgressInterval: 1,
	}

	firstSnapshotSeen := make(chan struct{})
	var firstSnapshotOnce sync.Once
	recorder := &dashboardSnapshotRecorder{
		onRender: func(dashboardSnapshot) {
			firstSnapshotOnce.Do(func() {
				close(firstSnapshotSeen)
			})
		},
	}

	err := Run(context.Background(), scanConfigurationFromFixture(t, cfg), &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{
		DisableKeyboard: true,
		Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			select {
			case <-firstSnapshotSeen:
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(200 * time.Millisecond):
				return nil, errors.New("timed out waiting for first dashboard snapshot")
			}
			return nil, errors.New("dial refused for test")
		},
		dashboardTerminalDetector: func(io.Writer) bool { return true },
		dashboardRefreshInterval:  10 * time.Millisecond,
		dashboardRenderer:         recorder,
	})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}

	snaps := recorder.snapshots()
	if len(snaps) == 0 {
		t.Fatal("expected dashboard snapshots during resumed run")
	}
	first := snaps[0]
	if first.ScannedTasks != 1 {
		t.Fatalf("expected first snapshot ScannedTasks=1 from resume state, got %#v", first)
	}
	if first.TotalTasks != 2 {
		t.Fatalf("expected first snapshot TotalTasks=2, got %#v", first)
	}
	if first.Percent != 50 {
		t.Fatalf("expected first snapshot Percent=50, got %#v", first)
	}
}

func TestPollPressureAPI_WhenJSONLoggerEnabled_EmitsPauseResumeMessages(t *testing.T) {
	server := newScriptedPressureServer(t)
	ctrl := speedctrl.NewController()
	logOut := &lockedBuffer{}
	logger := newLogger("info", true, logOut)
	poller := startTestPressurePoller(t, pressurePollFixture{
		Pressure: pressureConfigFixture{API: server.server.URL, Interval: 5 * time.Millisecond},
	}, RunOptions{
		PressureLimit: 90,
	}, ctrl, logger)

	server.respond(t, scriptedPressureHTTPResponse{
		statusCode: http.StatusOK,
		body:       `{"pressure":95}`,
	})
	testkit.WaitFor(t, pressureTestTimeout,
		"controller to pause and emit the pause message", func() bool {
			return ctrl.APIPaused() && strings.Contains(logOut.String(), "scan automatically paused")
		})

	server.respond(t, scriptedPressureHTTPResponse{
		statusCode: http.StatusOK,
		body:       `{"pressure":20}`,
	})
	testkit.WaitFor(t, pressureTestTimeout,
		"controller to resume and emit the resume message", func() bool {
			return !ctrl.APIPaused() && strings.Contains(logOut.String(), "scan automatically resumed")
		})

	poller.stop(t)
	poller.makeSureNoError(t)

	logs := logOut.String()
	if !strings.Contains(logs, `"level":"info"`) {
		t.Fatalf("expected json info logs, got %s", logs)
	}
	if !strings.Contains(logs, "scan automatically paused") || !strings.Contains(logs, "scan automatically resumed") {
		t.Fatalf("expected pause/resume messages, got %s", logs)
	}
}

func TestPollPressureAPI_WhenObserverInjected_ReportsSamplesAndFailures(t *testing.T) {
	ctrl := speedctrl.NewController()
	logOut := &lockedBuffer{}
	logger := newLogger("info", false, logOut)
	observer := &pressureTelemetryRecorder{}
	controllerObserver := &controllerTelemetryRecorder{}
	boom1 := errors.New("boom-1")
	boom2 := errors.New("boom-2")

	poller := startTestPressurePoller(t, pressurePollFixture{
		Pressure: pressureConfigFixture{Interval: 5 * time.Millisecond},
	}, RunOptions{
		PressureLimit:      90,
		PressureSource:     &scriptedPressureSource{results: []scriptedPressureResult{{err: boom1}, {err: boom2}, {pressure: 42}}},
		pressureObserver:   observer,
		controllerObserver: controllerObserver,
	}, ctrl, logger)

	testkit.WaitFor(t, pressureTestTimeout,
		"pressure observer to report two failures and one sample", func() bool {
			observer.mu.Lock()
			done := len(observer.polls) >= 3
			observer.mu.Unlock()
			return done
		})

	poller.stop(t)
	poller.makeSureNoError(t)

	observer.mu.Lock()
	defer observer.mu.Unlock()

	if !errors.Is(observer.polls[0].err, boom1) || observer.polls[0].failureCount != 1 {
		t.Fatalf("first pressure poll = %#v, want boom-1 with failure count 1", observer.polls[0])
	}
	if !errors.Is(observer.polls[1].err, boom2) || observer.polls[1].failureCount != 2 {
		t.Fatalf("second pressure poll = %#v, want boom-2 with failure count 2", observer.polls[1])
	}
	if observer.polls[2].err != nil || observer.polls[2].failureCount != 0 || observer.polls[2].sample.Maximum != 42 {
		t.Fatalf("third pressure poll = %#v, want successful pressure 42", observer.polls[2])
	}
	for i, poll := range observer.polls[:3] {
		if poll.sampledAt.IsZero() {
			t.Fatalf("pressure poll %d has zero sample time", i)
		}
	}

	controllerObserver.mu.Lock()
	defer controllerObserver.mu.Unlock()

	if controllerObserver.callbacks != 0 {
		t.Fatalf("expected no controller callbacks from pressure poll path, got %d with statuses %#v", controllerObserver.callbacks, controllerObserver.statuses)
	}
}

func TestEmitScanResultEvents_WhenProgressStepReached_EmitsProgressSnapshot(t *testing.T) {
	stdout := &bytes.Buffer{}
	logOut := &lockedBuffer{}
	logger := newLogger("info", true, logOut)
	ctrl := speedctrl.NewController()
	ch := &task.Chunk{
		CIDR:         "10.0.0.0/24",
		ScannedCount: 1,
		TotalCount:   4,
	}
	runtimes := []*chunkRuntime{{
		state:   ch,
		tracker: newChunkStateTracker(ch),
	}}
	summary := &resultSummary{written: 2}

	emitScanResultEvents(stdout, logger, ctrl, 2, runtimes, scanResult{
		chunkIdx: 0,
		record: writer.Record{
			IP:         "10.0.0.1",
			IPCidr:     "10.0.0.0/24",
			Port:       80,
			Status:     "open",
			ResponseMS: 7,
		},
	}, summary, false)

	if !strings.Contains(stdout.String(), "progress cidr=10.0.0.0/24 scanned=1/4 paused=false") {
		t.Fatalf("expected progress snapshot on stdout, got %s", stdout.String())
	}
	logs := logOut.String()
	for _, required := range []string{
		"\"state_transition\":\"scanned\"",
		"\"state_transition\":\"progress\"",
		"\"scanned_count\":1",
		"\"total_count\":4",
		"\"completion_rate\":0.25",
	} {
		if !strings.Contains(logs, required) {
			t.Fatalf("missing %s in logs: %s", required, logs)
		}
	}
}

func TestEmitCompletionSummary_WhenResultsMixed_EmitsOutcomeBreakdown(t *testing.T) {
	logOut := &lockedBuffer{}
	logger := newLogger("info", true, logOut)

	emitCompletionSummary(logger, resultSummary{
		written:      3,
		openCount:    1,
		closeCount:   1,
		timeoutCount: 1,
	}, time.Now().Add(-20*time.Millisecond), context.DeadlineExceeded)

	logs := logOut.String()
	for _, required := range []string{
		"\"state_transition\":\"completion_summary\"",
		"\"total_tasks\":3",
		"\"open_count\":1",
		"\"close_count\":1",
		"\"timeout_count\":1",
		"\"success\":false",
		"\"error_cause\":\"deadline_exceeded\"",
	} {
		if !strings.Contains(logs, required) {
			t.Fatalf("missing %s in logs: %s", required, logs)
		}
	}
}

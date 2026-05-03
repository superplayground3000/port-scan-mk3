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
		Resume:           resumeFile,
		LogLevel:         "error",
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
		LogLevel:         "error",
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
	if rts[0].state.TotalCount != 4 {
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
		CIDRIPCol:        "",
		CIDRIPCidrCol:    "",
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
		CIDRFile:         cidrFile,
		PortFile:         portFile,
		Output:           outFile,
		Timeout:          50 * time.Millisecond,
		Delay:            10 * time.Millisecond,
		BucketRate:       2,
		BucketCapacity:   2,
		Workers:          2,
		PressureInterval: 10 * time.Second,
		DisableAPI:       true,
		LogLevel:         "error",
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

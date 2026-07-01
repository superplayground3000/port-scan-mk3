package scanapp

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
	"github.com/xuxiping/port-scan-mk3/pkg/ratelimit"
	"github.com/xuxiping/port-scan-mk3/pkg/speedctrl"
	"github.com/xuxiping/port-scan-mk3/pkg/task"
)

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
		CIDRFile:           cidrFile,
		PortFile:           portFile,
		Output:             outFile,
		Timeout:            20 * time.Millisecond,
		Delay:              0,
		BucketRate:         1,
		BucketCapacity:     1,
		Workers:            1,
		PressureAPI:        api.URL,
		PressureInterval:   5 * time.Millisecond,
		DisableAPI:         false,
		DisablePreScanPing: true,
		LogLevel:           "error",
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

package scanapp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
	"github.com/xuxiping/port-scan-mk3/pkg/speedctrl"
)

// -------------------------------------------------------------------
// GAP 1: 3 consecutive API failures trigger error exit (circuit-breaker)
// -------------------------------------------------------------------

func TestPollPressureAPI_ThreeConsecutiveFailures_SendsErrorAndExits(t *testing.T) {
	failCount := atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		failCount.Add(1)
		http.Error(w, "server error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	cfg := config.Config{PressureAPI: srv.URL, PressureInterval: 10 * time.Millisecond}
	ctrl := speedctrl.NewController()
	errCh := make(chan error, 1)
	logger := newTestLogger()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	go pollPressureAPI(ctx, cfg, RunOptions{}, ctrl, logger, errCh)

	var err error
	select {
	case e := <-errCh:
		err = e
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected error after 3 failures, but pollPressureAPI did not exit")
	}

	if err == nil {
		t.Fatal("expected non-nil error after 3 consecutive failures")
	}
	// Should be the circuit-breaker error: "pressure api failed 3 times: ..."
	if !strings.Contains(err.Error(), "pressure api failed 3 times") {
		t.Fatalf("expected circuit-breaker error, got: %v", err)
	}

	if got := failCount.Load(); got < 3 {
		t.Errorf("expected at least 3 failures, got %d", got)
	}
}

func TestPollPressureAPI_FailureRecoveryAfterTwoFails_SkipsThirdAndContinues(t *testing.T) {
	// After 2 failures (not reaching 3), if a request succeeds, consecutiveFailures should reset to 0.
	callCount := atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := callCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if c <= 2 {
			http.Error(w, "server error", http.StatusInternalServerError)
			return
		}
		// Third call succeeds
		_ = json.NewEncoder(w).Encode(map[string]int{"pressure": 50})
	}))
	defer srv.Close()

	cfg := config.Config{PressureAPI: srv.URL, PressureInterval: 10 * time.Millisecond}
	ctrl := speedctrl.NewController()
	errCh := make(chan error, 1)
	logger := newTestLogger()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	go pollPressureAPI(ctx, cfg, RunOptions{}, ctrl, logger, errCh)

	// Wait enough time for at least 3 polls
	time.Sleep(150 * time.Millisecond)

	select {
	case e := <-errCh:
		t.Fatalf("unexpected error (should not have failed with 3 successes after 2 initial failures): %v", e)
	default:
		// Expected: no error
	}

	// Should not be paused since pressure=50 < threshold=90
	if ctrl.IsPaused() {
		t.Error("expected controller not to be paused at pressure=50")
	}
}

// -------------------------------------------------------------------
// GAP 2: Pressure exactly equals threshold (90 >= 90) triggers pause
// -------------------------------------------------------------------

func TestPollPressureAPI_PressureExactlyAtThreshold_Pauses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]int{"pressure": 90})
	}))
	defer srv.Close()

	cfg := config.Config{PressureAPI: srv.URL, PressureInterval: 10 * time.Millisecond}
	ctrl := speedctrl.NewController()
	errCh := make(chan error, 1)
	logger := newTestLogger()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	go pollPressureAPI(ctx, cfg, RunOptions{PressureLimit: 90}, ctrl, logger, errCh)

	time.Sleep(30 * time.Millisecond)

	if !ctrl.IsPaused() {
		t.Error("expected controller to be paused when pressure=90 and threshold=90")
	}
}

func TestPollPressureAPI_PressureJustBelowThreshold_DoesNotPause(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]int{"pressure": 89})
	}))
	defer srv.Close()

	cfg := config.Config{PressureAPI: srv.URL, PressureInterval: 10 * time.Millisecond}
	ctrl := speedctrl.NewController()
	errCh := make(chan error, 1)
	logger := newTestLogger()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	go pollPressureAPI(ctx, cfg, RunOptions{PressureLimit: 90}, ctrl, logger, errCh)

	time.Sleep(30 * time.Millisecond)

	if ctrl.IsPaused() {
		t.Error("expected controller not to be paused when pressure=89 and threshold=90")
	}
}

func TestPollPressureAPI_PressureAboveThreshold_Pauses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]int{"pressure": 91})
	}))
	defer srv.Close()

	cfg := config.Config{PressureAPI: srv.URL, PressureInterval: 10 * time.Millisecond}
	ctrl := speedctrl.NewController()
	errCh := make(chan error, 1)
	logger := newTestLogger()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	go pollPressureAPI(ctx, cfg, RunOptions{PressureLimit: 90}, ctrl, logger, errCh)

	time.Sleep(30 * time.Millisecond)

	if !ctrl.IsPaused() {
		t.Error("expected controller to be paused when pressure=91 and threshold=90")
	}
}

// -------------------------------------------------------------------
// GAP 3: Pause → Resume transition (pressure drops below threshold)
// -------------------------------------------------------------------

func TestPollPressureAPI_PressureDropsBelowThreshold_Resumes(t *testing.T) {
	// Values: 95 (pause), then 30 (resume) — enough values to avoid cycling during test.
	// With 20ms interval and 100ms context, ~5 polls fire. Use 5 values: 95,30,30,30,30.
	idx := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		values := []int{95, 30, 30, 30, 30}
		_ = json.NewEncoder(w).Encode(map[string]int{"pressure": values[idx%len(values)]})
		idx++
	}))
	defer srv.Close()

	cfg := config.Config{PressureAPI: srv.URL, PressureInterval: 20 * time.Millisecond}
	ctrl := speedctrl.NewController()
	errCh := make(chan error, 1)
	logger := newTestLogger()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	go pollPressureAPI(ctx, cfg, RunOptions{PressureLimit: 90}, ctrl, logger, errCh)

	// First poll: should pause
	time.Sleep(30 * time.Millisecond)
	if !ctrl.IsPaused() {
		t.Fatal("expected paused after first poll (pressure=95 >= 90)")
	}

	// Second poll: should resume
	time.Sleep(30 * time.Millisecond)
	if ctrl.IsPaused() {
		t.Error("expected resumed after second poll (pressure=30 < 90)")
	}
}

// -------------------------------------------------------------------
// GAP 4: Rapid oscillation 89→91→89→91 (no hysteresis, choppy behavior)
// -------------------------------------------------------------------

func TestPollPressureAPI_RapidOscillation_RepeatedlyPausesAndResumes(t *testing.T) {
	// Values: 95, 30, 95, 30 — rapid pause/resume cycles
	idx := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		values := []int{95, 30, 95, 30}
		_ = json.NewEncoder(w).Encode(map[string]int{"pressure": values[idx%len(values)]})
		idx++
	}))
	defer srv.Close()

	cfg := config.Config{PressureAPI: srv.URL, PressureInterval: 10 * time.Millisecond}
	ctrl := speedctrl.NewController()
	errCh := make(chan error, 1)
	logger := newTestLogger()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	go pollPressureAPI(ctx, cfg, RunOptions{PressureLimit: 90}, ctrl, logger, errCh)

	// Track pause/resume transitions
	var transitionCount int
	prevPaused := ctrl.IsPaused()

	for i := 0; i < 20; i++ {
		time.Sleep(15 * time.Millisecond)
		currPaused := ctrl.IsPaused()
		if currPaused != prevPaused {
			transitionCount++
			prevPaused = currPaused
		}
	}

	// Without hysteresis, every threshold crossing toggles state.
	// 4 values = up to 3 transitions expected (95→30, 30→95, 95→30).
	// At minimum we expect at least 2 transitions due to oscillation.
	if transitionCount < 2 {
		t.Errorf("expected at least 2 pause/resume transitions during rapid oscillation, got %d", transitionCount)
	}
}

// -------------------------------------------------------------------
// GAP 5: Pressure = 0 (valid, should not pause)
// -------------------------------------------------------------------

func TestPollPressureAPI_ZeroPressure_DoesNotPause(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]int{"pressure": 0})
	}))
	defer srv.Close()

	cfg := config.Config{PressureAPI: srv.URL, PressureInterval: 10 * time.Millisecond}
	ctrl := speedctrl.NewController()
	errCh := make(chan error, 1)
	logger := newTestLogger()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	go pollPressureAPI(ctx, cfg, RunOptions{PressureLimit: 90}, ctrl, logger, errCh)

	time.Sleep(30 * time.Millisecond)

	if ctrl.IsPaused() {
		t.Error("expected controller not to be paused when pressure=0")
	}
}

// -------------------------------------------------------------------
// GAP 6: Negative pressure value (API returns -1 or similar — edge case)
// -------------------------------------------------------------------

func TestPollPressureAPI_NegativePressureValue_DoesNotPause(t *testing.T) {
	// Some APIs might return -1 for unknown/missing pressure.
	// The fetcher parses it as a number, normalizePressure passes it through.
	// At integration level: pressure=-1 < threshold=90, should not pause.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]int{"pressure": -1})
	}))
	defer srv.Close()

	cfg := config.Config{PressureAPI: srv.URL, PressureInterval: 10 * time.Millisecond}
	ctrl := speedctrl.NewController()
	errCh := make(chan error, 1)
	logger := newTestLogger()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	go pollPressureAPI(ctx, cfg, RunOptions{PressureLimit: 90}, ctrl, logger, errCh)

	time.Sleep(30 * time.Millisecond)

	if ctrl.IsPaused() {
		t.Error("expected controller not to be paused when pressure=-1")
	}
}

// -------------------------------------------------------------------
// GAP 7: Threshold boundary rounding — 89.95 becomes 90.0
// -------------------------------------------------------------------

func TestPollPressureAPI_FractionalPressureRoundsUp_TriggersPause(t *testing.T) {
	// normalizePressure: math.Round(v*10)/10, so 89.95*10 = 899.5 → round = 900 → 900/10 = 90.0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// API returns 89.95 as a float string
		_ = json.NewEncoder(w).Encode(map[string]float64{"pressure": 89.95})
	}))
	defer srv.Close()

	cfg := config.Config{PressureAPI: srv.URL, PressureInterval: 10 * time.Millisecond}
	ctrl := speedctrl.NewController()
	errCh := make(chan error, 1)
	logger := newTestLogger()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	go pollPressureAPI(ctx, cfg, RunOptions{PressureLimit: 90}, ctrl, logger, errCh)

	time.Sleep(30 * time.Millisecond)

	// 89.95 rounds to 90.0 via normalizePressure, which is >= 90, so it should pause.
	if !ctrl.IsPaused() {
		t.Error("expected controller to be paused when pressure=89.95 (rounds to 90.0) >= threshold=90")
	}
}

func TestPollPressureAPI_FractionalPressureJustBelow_StaysActive(t *testing.T) {
	// 89.94 rounds to 89.9, which is < 90, should not pause.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]float64{"pressure": 89.94})
	}))
	defer srv.Close()

	cfg := config.Config{PressureAPI: srv.URL, PressureInterval: 10 * time.Millisecond}
	ctrl := speedctrl.NewController()
	errCh := make(chan error, 1)
	logger := newTestLogger()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	go pollPressureAPI(ctx, cfg, RunOptions{PressureLimit: 90}, ctrl, logger, errCh)

	time.Sleep(30 * time.Millisecond)

	// 89.94 rounds to 89.9, which is < 90, so should not pause.
	if ctrl.IsPaused() {
		t.Error("expected controller not to be paused when pressure=89.94 (rounds to 89.9) < threshold=90")
	}
}

// -------------------------------------------------------------------
// GAP 8: Manual pause first, then API pause (ordering)
// -------------------------------------------------------------------

func TestPollPressureAPI_ManualPausedThenAPIPauses_BothBlocksTogether(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]int{"pressure": 95})
	}))
	defer srv.Close()

	cfg := config.Config{PressureAPI: srv.URL, PressureInterval: 20 * time.Millisecond}
	ctrl := speedctrl.NewController(speedctrl.WithAPIEnabled(true))
	errCh := make(chan error, 1)
	logger := newTestLogger()

	// Use a cancellable context so we can stop the polling goroutine after observing API pause.
	pollCtx, pollCancel := context.WithCancel(context.Background())
	defer pollCancel()

	go pollPressureAPI(pollCtx, cfg, RunOptions{PressureLimit: 90}, ctrl, logger, errCh)

	// Start manual paused first
	ctrl.SetManualPaused(true)

	// Wait for API to also set paused (first tick fires at ~20ms)
	time.Sleep(40 * time.Millisecond)

	// Both flags should be set
	if !ctrl.ManualPaused() {
		t.Error("manual pause should still be active")
	}
	if !ctrl.APIPaused() {
		t.Error("API should have set paused due to pressure >= 90")
	}
	if !ctrl.IsPaused() {
		t.Error("IsPaused should be true when either flag is set")
	}

	// Stop the polling goroutine so it won't re-trigger APIPaused during our checks.
	pollCancel()
	time.Sleep(5 * time.Millisecond) // allow goroutine to exit

	// Resume API first — scan should still be paused (manual still active)
	ctrl.SetAPIPaused(false)
	time.Sleep(5 * time.Millisecond)

	if !ctrl.IsPaused() {
		t.Error("IsPaused should still be true (manual remains set)")
	}

	// Resume manual — scan should now resume
	ctrl.SetManualPaused(false)
	time.Sleep(5 * time.Millisecond)

	if ctrl.IsPaused() {
		t.Error("IsPaused should be false after both flags cleared")
	}
}

// -------------------------------------------------------------------
// scanLogger helper (uses real concrete type with io.Discard output)
// -------------------------------------------------------------------

func newTestLogger() *scanLogger {
	return newLogger("info", false, io.Discard)
}

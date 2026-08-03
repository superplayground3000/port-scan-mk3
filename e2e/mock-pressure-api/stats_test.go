package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/xuxiping/port-scan-mk3/internal/testkit"
)

// The e2e failure scenarios must not race a tiny scan against the pressure
// poller (issue #71). Instead the harness waits until this mock has actually
// SERVED enough failing pressure responses to cross the scanner's fatal
// threshold. These tests pin the counter contract the harness depends on.

func decodeStats(t *testing.T, mux http.Handler) (requests, failures int) {
	t.Helper()

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/stats", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /admin/stats: got status %d, want %d (body=%q)", rec.Code, http.StatusOK, rec.Body.String())
	}

	var body struct {
		PressureRequests int `json:"pressure_requests"`
		PressureFailures int `json:"pressure_failures"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode /admin/stats body %q: %v", rec.Body.String(), err)
	}
	return body.PressureRequests, body.PressureFailures
}

func newStatsFixture(t *testing.T, mode string, delayMS int) (http.Handler, *pressureStats) {
	t.Helper()

	stats := newPressureStats()
	mux := newMuxWithStats(stats)
	mux.HandleFunc("/api/pressure", newPressureHandlerWithStats(mode, delayMS, newPressureState(20, nil, false), nil, stats))
	return mux, stats
}

func TestAdminStats_CountsEvery5xxPressureResponseServed(t *testing.T) {
	mux, _ := newStatsFixture(t, "fail", 0)

	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/pressure", nil))
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("request %d: got status %d, want %d", i, rec.Code, http.StatusInternalServerError)
		}
	}

	requests, failures := decodeStats(t, mux)
	if requests != 2 {
		t.Errorf("pressure_requests = %d, want 2", requests)
	}
	if failures != 2 {
		t.Errorf("pressure_failures = %d, want 2", failures)
	}
}

// The scanner never sees a response in "timeout" mode — its HTTP client gives
// up while the handler is still sleeping. So the stall itself is the failure
// and MUST be counted when it STARTS. Counting it after the sleep would make
// the counter useless to a harness that waits on it.
func TestAdminStats_CountsTimeoutStallWhenItStartsNotWhenItEnds(t *testing.T) {
	const stallMS = 2000
	mux, stats := newStatsFixture(t, "timeout", stallMS)

	// Fire and forget: nobody ever reads this response, exactly like the
	// scanner's client after its own timeout fires.
	go mux.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/pressure", nil))

	testkit.WaitFor(t, time.Duration(stallMS/2)*time.Millisecond,
		"timeout-mode stall to be counted as a failure while it is still stalling",
		func() bool {
			_, failures := stats.snapshot()
			return failures == 1
		})

	requests, _ := stats.snapshot()
	if requests != 1 {
		t.Errorf("pressure_requests = %d, want 1", requests)
	}
}

func TestAdminStats_DoesNotCountSuccessfulPressureResponsesAsFailures(t *testing.T) {
	mux, _ := newStatsFixture(t, "ok", 0)

	for i := 0; i < 3; i++ {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/pressure", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: got status %d, want %d", i, rec.Code, http.StatusOK)
		}
	}

	requests, failures := decodeStats(t, mux)
	if requests != 3 {
		t.Errorf("pressure_requests = %d, want 3", requests)
	}
	if failures != 0 {
		t.Errorf("pressure_failures = %d, want 0", failures)
	}
}

// The mock serves every scanner poll on its own net/http goroutine, so the
// counters are shared mutable state and must be mutex-guarded.
func TestPressureStats_ConcurrentRecordingIsRaceFree(t *testing.T) {
	stats := newPressureStats()

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			stats.recordRequest()
			stats.recordFailure()
			_, _ = stats.snapshot()
		}()
	}
	wg.Wait()

	requests, failures := stats.snapshot()
	if requests != 32 {
		t.Errorf("pressure_requests = %d, want 32", requests)
	}
	if failures != 32 {
		t.Errorf("pressure_failures = %d, want 32", failures)
	}
}

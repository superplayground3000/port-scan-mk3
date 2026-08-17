package scanapp

import (
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xuxiping/port-scan-mk3/internal/testkit"
	"github.com/xuxiping/port-scan-mk3/pkg/pressure"
	"github.com/xuxiping/port-scan-mk3/pkg/speedctrl"
)

type contextPressureSource struct {
	started chan struct{}
}

func (s contextPressureSource) Sample(ctx context.Context) (pressure.Sample, error) {
	close(s.started)
	<-ctx.Done()
	return pressure.Sample{}, ctx.Err()
}

func TestPollPressureAPI_ContextCancellationDuringSampleDoesNotRecordFailure(t *testing.T) {
	observer := &pressureTelemetryRecorder{}
	started := make(chan struct{})
	poller := startTestPressurePoller(t, pressurePollFixture{
		Pressure: pressureConfigFixture{Interval: time.Millisecond},
	}, RunOptions{
		PressureSource:   contextPressureSource{started: started},
		pressureObserver: observer,
	}, speedctrl.NewController(), newTestLogger())

	select {
	case <-started:
	case <-time.After(pressureTestTimeout):
		t.Fatal("timed out waiting for pressure sample")
	}
	poller.stop(t)
	poller.makeSureNoError(t)

	observer.mu.Lock()
	defer observer.mu.Unlock()
	if len(observer.polls) != 0 {
		t.Fatalf("pressure polls = %#v, want no failure after context cancellation", observer.polls)
	}
}

func TestPollPressureAPI_ThreeConsecutiveFailures_SendsErrorAndExits(t *testing.T) {
	server := newScriptedPressureServer(t)
	ctrl := speedctrl.NewController()
	poller := startTestPressurePoller(t, pressurePollFixture{
		Pressure: pressureConfigFixture{API: server.server.URL, Interval: 10 * time.Millisecond},
	}, RunOptions{}, ctrl, newTestLogger())

	for range 3 {
		server.respond(t, scriptedPressureHTTPResponse{
			statusCode: http.StatusInternalServerError,
			body:       "server error",
		})
	}

	var err error
	select {
	case err = <-poller.errCh:
	case <-time.After(pressureTestTimeout):
		t.Fatal("the pressure poller did not return an error before the timeout")
	}

	if err == nil {
		t.Fatal("expected an error after three consecutive API failures")
	}
	if !strings.Contains(err.Error(), "pressure api failed 3 times") {
		t.Fatalf("expected circuit-breaker error, got: %v", err)
	}

	select {
	case <-poller.done:
	case <-time.After(pressureTestTimeout):
		t.Fatal("the pressure poller did not exit after the third failure")
	}

	poller.stop(t)
}

func TestPollPressureAPI_NonFiniteAggregatePreservesStateForFirstTwoFailures(t *testing.T) {
	for _, initialPaused := range []bool{false, true} {
		t.Run(strconv.FormatBool(initialPaused), func(t *testing.T) {
			source := newControlledPressureSource()
			observer := &pressureTelemetryRecorder{}
			ctrl := speedctrl.NewController()
			ctrl.SetAPIPaused(initialPaused)
			poller := startTestPressurePoller(t, pressurePollFixture{
				Pressure: pressureConfigFixture{Interval: time.Millisecond},
			}, RunOptions{
				PressureSource:   source,
				pressureObserver: observer,
			}, ctrl, newTestLogger())

			values := []struct {
				value    float64
				wantKind string
			}{
				{value: math.NaN(), wantKind: "NaN"},
				{value: math.Inf(1), wantKind: "positive infinity"},
			}
			for index, value := range values {
				source.respond(t, controlledPressureResponse{sample: pressure.Sample{Maximum: value.value}})
				wantPolls := index + 1
				testkit.WaitFor(t, pressureTestTimeout, "non-finite sample to become a pressure failure", func() bool {
					observer.mu.Lock()
					defer observer.mu.Unlock()
					return len(observer.polls) >= wantPolls
				})
				if ctrl.APIPaused() != initialPaused {
					t.Errorf("failure %d changed API pause from %t to %t", wantPolls, initialPaused, ctrl.APIPaused())
				}
			}

			poller.stop(t)
			poller.makeSureNoError(t)

			observer.mu.Lock()
			defer observer.mu.Unlock()
			for index, poll := range observer.polls[:2] {
				if poll.err == nil || poll.failureCount != index+1 {
					t.Errorf("poll %d = %#v, want consecutive failure %d", index, poll, index+1)
				}
				for _, detail := range []string{"Maximum", values[index].wantKind} {
					if !strings.Contains(poll.err.Error(), detail) {
						t.Errorf("poll %d error = %q, want detail %q", index, poll.err, detail)
					}
				}
			}
		})
	}
}

func TestPollPressureAPI_MixedFailuresReachFatalThirdFailure(t *testing.T) {
	source := newControlledPressureSource()
	observer := &pressureTelemetryRecorder{}
	poller := startTestPressurePoller(t, pressurePollFixture{
		Pressure: pressureConfigFixture{Interval: time.Millisecond},
	}, RunOptions{
		PressureSource:   source,
		pressureObserver: observer,
	}, speedctrl.NewController(), newTestLogger())

	responses := []controlledPressureResponse{
		{err: errors.New("HTTP request failed")},
		{sample: pressure.Sample{Maximum: math.NaN()}},
		{err: errors.New("status=503")},
	}
	for index, response := range responses {
		source.respond(t, response)
		wantPolls := index + 1
		testkit.WaitFor(t, pressureTestTimeout, "mixed pressure failure to be observed", func() bool {
			observer.mu.Lock()
			defer observer.mu.Unlock()
			return len(observer.polls) >= wantPolls
		})
	}

	select {
	case err := <-poller.errCh:
		if err == nil || !strings.Contains(err.Error(), "pressure api failed 3 times") || !strings.Contains(err.Error(), "status=503") {
			t.Fatalf("fatal error = %v, want the third status failure", err)
		}
	case <-time.After(pressureTestTimeout):
		t.Fatal("the mixed third failure did not become fatal")
	}
	poller.stop(t)

	observer.mu.Lock()
	defer observer.mu.Unlock()
	for index, poll := range observer.polls[:3] {
		if poll.err == nil || poll.failureCount != index+1 {
			t.Errorf("poll %d = %#v, want consecutive failure %d", index, poll, index+1)
		}
	}
}

func TestPollPressureAPI_NonFiniteSourceResultKeepsTelemetryAndOverallStreak(t *testing.T) {
	source := newControlledPressureSource()
	state := newDashboardState(1, time.Now)
	recorder := &pressureTelemetryRecorder{}
	poller := startTestPressurePoller(t, pressurePollFixture{
		Pressure: pressureConfigFixture{Interval: time.Millisecond},
	}, RunOptions{
		PressureSource:   source,
		pressureObserver: appendPressureTelemetryObservers(state, recorder),
	}, speedctrl.NewController(), newTestLogger())

	source.respond(t, controlledPressureResponse{sample: pressure.Sample{
		Maximum: 60,
		Sources: []pressure.SourceResult{
			{Name: "src1", Pressure: 50},
			{Name: "src2", Pressure: 60},
		},
	}})
	testkit.WaitFor(t, pressureTestTimeout, "initial finite source values", func() bool {
		return state.Snapshot().APIHealthText == "ok"
	})

	source.respond(t, controlledPressureResponse{err: errors.New("HTTP request failed")})
	testkit.WaitFor(t, pressureTestTimeout, "first overall pressure failure", func() bool {
		return state.Snapshot().APIHealthText == "fail streak 1"
	})

	source.respond(t, controlledPressureResponse{sample: pressure.Sample{
		Maximum: 55,
		Sources: []pressure.SourceResult{
			{Name: "src1", Pressure: 55},
			{Name: "src2", Pressure: math.Inf(-1)},
		},
	}})
	testkit.WaitFor(t, pressureTestTimeout, "non-finite source result to keep the overall streak", func() bool {
		return state.Snapshot().APIHealthText == "fail streak 2"
	})

	snapshot := state.Snapshot()
	if snapshot.PressurePercent != 60 {
		t.Errorf("aggregate pressure = %d, want retained finite value 60", snapshot.PressurePercent)
	}
	if len(snapshot.APISources) != 2 {
		t.Fatalf("source telemetry = %#v, want two sources", snapshot.APISources)
	}
	if got := snapshot.APISources[0]; got.Name != "src1" || got.PressurePercent != 55 || got.HealthText != "ok" {
		t.Errorf("src1 telemetry = %#v, want current finite success", got)
	}
	if got := snapshot.APISources[1]; got.Name != "src2" || got.PressurePercent != 60 || got.HealthText != "fail streak 1" {
		t.Errorf("src2 telemetry = %#v, want retained value and source failure", got)
	}
	testkit.WaitFor(t, pressureTestTimeout, "non-finite source error to reach the recorder", func() bool {
		recorder.mu.Lock()
		defer recorder.mu.Unlock()
		return len(recorder.polls) >= 3
	})
	recorder.mu.Lock()
	nonFinitePoll := recorder.polls[2]
	recorder.mu.Unlock()
	for _, detail := range []string{"src2", "Pressure", "negative infinity"} {
		if !strings.Contains(nonFinitePoll.err.Error(), detail) {
			t.Errorf("source result error = %q, want detail %q", nonFinitePoll.err, detail)
		}
	}

	source.respond(t, controlledPressureResponse{sample: pressure.Sample{
		Maximum: 45,
		Sources: []pressure.SourceResult{
			{Name: "src1", Pressure: 45},
			{Name: "src2", Pressure: 44},
		},
	}})
	testkit.WaitFor(t, pressureTestTimeout, "complete successful poll to reset the overall streak", func() bool {
		return state.Snapshot().APIHealthText == "ok"
	})

	source.respond(t, controlledPressureResponse{err: errors.New("status=502")})
	testkit.WaitFor(t, pressureTestTimeout, "failure after reset to start at one", func() bool {
		return state.Snapshot().APIHealthText == "fail streak 1"
	})

	poller.stop(t)
	poller.makeSureNoError(t)
}

func TestPollPressureAPI_FailureRecoveryAfterTwoFails_SkipsThirdAndContinues(t *testing.T) {
	server := newScriptedPressureServer(t)
	ctrl := speedctrl.NewController()
	ctrl.SetAPIPaused(true)
	poller := startTestPressurePoller(t, pressurePollFixture{
		Pressure: pressureConfigFixture{API: server.server.URL, Interval: 10 * time.Millisecond},
	}, RunOptions{}, ctrl, newTestLogger())

	for range 2 {
		server.respond(t, scriptedPressureHTTPResponse{
			statusCode: http.StatusInternalServerError,
			body:       "server error",
		})
	}
	server.respond(t, scriptedPressureHTTPResponse{
		statusCode: http.StatusOK,
		body:       `{"pressure":50}`,
	})

	testkit.WaitFor(t, pressureTestTimeout,
		"controller to resume when pressure=50", func() bool { return !ctrl.APIPaused() })

	server.respond(t, scriptedPressureHTTPResponse{
		statusCode: http.StatusInternalServerError,
		body:       "server error",
	})
	ctrl.SetAPIPaused(true)
	server.respond(t, scriptedPressureHTTPResponse{
		statusCode: http.StatusOK,
		body:       `{"pressure":50}`,
	})
	testkit.WaitFor(t, pressureTestTimeout,
		"controller to resume after a recovered pressure API failure", func() bool { return !ctrl.APIPaused() })

	poller.stop(t)
	poller.makeSureNoError(t)
}

func TestPollPressureAPI_PressureExactlyAtThreshold_Pauses(t *testing.T) {
	server := newScriptedPressureServer(t)
	ctrl := speedctrl.NewController()
	poller := startTestPressurePoller(t, pressurePollFixture{
		Pressure: pressureConfigFixture{API: server.server.URL, Interval: 10 * time.Millisecond},
	}, RunOptions{PressureLimit: 90}, ctrl, newTestLogger())

	server.respond(t, scriptedPressureHTTPResponse{
		statusCode: http.StatusOK,
		body:       `{"pressure":90}`,
	})

	testkit.WaitFor(t, pressureTestTimeout,
		"controller to pause when pressure=90 and threshold=90", ctrl.APIPaused)
	poller.stop(t)
	poller.makeSureNoError(t)
}

func TestPollPressureAPI_PressureJustBelowThreshold_DoesNotPause(t *testing.T) {
	server := newScriptedPressureServer(t)
	ctrl := speedctrl.NewController()
	ctrl.SetAPIPaused(true)
	poller := startTestPressurePoller(t, pressurePollFixture{
		Pressure: pressureConfigFixture{API: server.server.URL, Interval: 10 * time.Millisecond},
	}, RunOptions{PressureLimit: 90}, ctrl, newTestLogger())

	server.respond(t, scriptedPressureHTTPResponse{
		statusCode: http.StatusOK,
		body:       `{"pressure":89}`,
	})

	testkit.WaitFor(t, pressureTestTimeout,
		"controller to resume when pressure=89 and threshold=90", func() bool { return !ctrl.APIPaused() })
	poller.stop(t)
	poller.makeSureNoError(t)
}

func TestPollPressureAPI_PausesAtNinetyOneWhenThresholdIsNinety(t *testing.T) {
	server := newScriptedPressureServer(t)
	ctrl := speedctrl.NewController()
	poller := startTestPressurePoller(t, pressurePollFixture{
		Pressure: pressureConfigFixture{API: server.server.URL, Interval: 10 * time.Millisecond},
	}, RunOptions{PressureLimit: 90}, ctrl, newTestLogger())

	server.respond(t, scriptedPressureHTTPResponse{
		statusCode: http.StatusOK,
		body:       `{"pressure":91}`,
	})

	testkit.WaitFor(t, pressureTestTimeout,
		"controller to be paused when pressure=91 and threshold=90", ctrl.APIPaused)
	poller.stop(t)
	poller.makeSureNoError(t)
}

func TestPollPressureAPI_PressureDropsBelowThreshold_Resumes(t *testing.T) {
	server := newScriptedPressureServer(t)
	ctrl := speedctrl.NewController()
	poller := startTestPressurePoller(t, pressurePollFixture{
		Pressure: pressureConfigFixture{API: server.server.URL, Interval: 10 * time.Millisecond},
	}, RunOptions{PressureLimit: 90}, ctrl, newTestLogger())

	server.respond(t, scriptedPressureHTTPResponse{
		statusCode: http.StatusOK,
		body:       `{"pressure":95}`,
	})
	testkit.WaitFor(t, pressureTestTimeout,
		"controller to pause when pressure=95", ctrl.APIPaused)

	server.respond(t, scriptedPressureHTTPResponse{
		statusCode: http.StatusOK,
		body:       `{"pressure":30}`,
	})
	testkit.WaitFor(t, pressureTestTimeout,
		"controller to resume when pressure=30", func() bool { return !ctrl.APIPaused() })
	poller.stop(t)
	poller.makeSureNoError(t)
}

func TestPollPressureAPI_RapidOscillation_RepeatedlyPausesAndResumes(t *testing.T) {
	server := newScriptedPressureServer(t)
	ctrl := speedctrl.NewController()
	poller := startTestPressurePoller(t, pressurePollFixture{
		Pressure: pressureConfigFixture{API: server.server.URL, Interval: 10 * time.Millisecond},
	}, RunOptions{PressureLimit: 90}, ctrl, newTestLogger())

	for _, step := range []struct {
		pressure int
		paused   bool
	}{
		{pressure: 95, paused: true},
		{pressure: 30, paused: false},
		{pressure: 95, paused: true},
		{pressure: 30, paused: false},
	} {
		server.respond(t, scriptedPressureHTTPResponse{
			statusCode: http.StatusOK,
			body:       `{"pressure":` + strconv.Itoa(step.pressure) + `}`,
		})
		testkit.WaitFor(t, pressureTestTimeout, "controller to match the scripted pressure state", func() bool {
			return ctrl.APIPaused() == step.paused
		})
	}
	poller.stop(t)
	poller.makeSureNoError(t)
}

func TestPollPressureAPI_ZeroPressure_DoesNotPause(t *testing.T) {
	server := newScriptedPressureServer(t)
	ctrl := speedctrl.NewController()
	ctrl.SetAPIPaused(true)
	poller := startTestPressurePoller(t, pressurePollFixture{
		Pressure: pressureConfigFixture{API: server.server.URL, Interval: 10 * time.Millisecond},
	}, RunOptions{PressureLimit: 90}, ctrl, newTestLogger())

	server.respond(t, scriptedPressureHTTPResponse{
		statusCode: http.StatusOK,
		body:       `{"pressure":0}`,
	})

	testkit.WaitFor(t, pressureTestTimeout,
		"controller to resume when pressure=0", func() bool { return !ctrl.APIPaused() })
	poller.stop(t)
	poller.makeSureNoError(t)
}

func TestPollPressureAPI_NegativePressureValue_DoesNotPause(t *testing.T) {
	server := newScriptedPressureServer(t)
	ctrl := speedctrl.NewController()
	ctrl.SetAPIPaused(true)
	poller := startTestPressurePoller(t, pressurePollFixture{
		Pressure: pressureConfigFixture{API: server.server.URL, Interval: 10 * time.Millisecond},
	}, RunOptions{PressureLimit: 90}, ctrl, newTestLogger())

	server.respond(t, scriptedPressureHTTPResponse{
		statusCode: http.StatusOK,
		body:       `{"pressure":-1}`,
	})

	testkit.WaitFor(t, pressureTestTimeout,
		"controller to resume when pressure=-1", func() bool { return !ctrl.APIPaused() })
	poller.stop(t)
	poller.makeSureNoError(t)
}

func TestPollPressureAPI_FractionalPressureRoundsUp_TriggersPause(t *testing.T) {
	// 89.95 rounds to 90.0, so the controller pauses at threshold 90.
	server := newScriptedPressureServer(t)
	ctrl := speedctrl.NewController()
	poller := startTestPressurePoller(t, pressurePollFixture{
		Pressure: pressureConfigFixture{API: server.server.URL, Interval: 10 * time.Millisecond},
	}, RunOptions{PressureLimit: 90}, ctrl, newTestLogger())

	server.respond(t, scriptedPressureHTTPResponse{
		statusCode: http.StatusOK,
		body:       `{"pressure":89.95}`,
	})

	testkit.WaitFor(t, pressureTestTimeout,
		"controller to pause when pressure=89.95 rounds to 90.0", ctrl.APIPaused)
	poller.stop(t)
	poller.makeSureNoError(t)
}

func TestPollPressureAPI_FractionalPressureJustBelow_StaysActive(t *testing.T) {
	server := newScriptedPressureServer(t)
	ctrl := speedctrl.NewController()
	ctrl.SetAPIPaused(true)
	poller := startTestPressurePoller(t, pressurePollFixture{
		Pressure: pressureConfigFixture{API: server.server.URL, Interval: 10 * time.Millisecond},
	}, RunOptions{PressureLimit: 90}, ctrl, newTestLogger())

	server.respond(t, scriptedPressureHTTPResponse{
		statusCode: http.StatusOK,
		body:       `{"pressure":89.94}`,
	})

	testkit.WaitFor(t, pressureTestTimeout,
		"controller to resume when pressure=89.94 rounds to 89.9", func() bool { return !ctrl.APIPaused() })
	poller.stop(t)
	poller.makeSureNoError(t)
}

func TestPollPressureAPI_ManualPausedThenAPIPauses_BothBlocksTogether(t *testing.T) {
	server := newScriptedPressureServer(t)
	ctrl := speedctrl.NewController(speedctrl.WithAPIEnabled(true))
	ctrl.SetManualPaused(true)
	poller := startTestPressurePoller(t, pressurePollFixture{
		Pressure: pressureConfigFixture{API: server.server.URL, Interval: 10 * time.Millisecond},
	}, RunOptions{PressureLimit: 90}, ctrl, newTestLogger())

	server.respond(t, scriptedPressureHTTPResponse{
		statusCode: http.StatusOK,
		body:       `{"pressure":95}`,
	})
	testkit.WaitFor(t, pressureTestTimeout,
		"API pause to activate when pressure=95", ctrl.APIPaused)

	if !ctrl.ManualPaused() {
		t.Error("manual pause must remain active")
	}
	if !ctrl.APIPaused() {
		t.Error("API pause must be active")
	}
	if !ctrl.IsPaused() {
		t.Error("the controller must remain paused while either pause is active")
	}

	poller.stop(t)
	poller.makeSureNoError(t)

	ctrl.SetAPIPaused(false)
	if !ctrl.IsPaused() {
		t.Error("the controller must remain paused while the manual pause is active")
	}

	ctrl.SetManualPaused(false)
	if ctrl.IsPaused() {
		t.Error("the controller must resume after both pauses are inactive")
	}
}

func newTestLogger() *scanLogger {
	return newLogger("info", false, io.Discard)
}

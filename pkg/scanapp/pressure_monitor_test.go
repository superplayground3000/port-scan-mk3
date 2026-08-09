package scanapp

import (
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xuxiping/port-scan-mk3/internal/testkit"
	"github.com/xuxiping/port-scan-mk3/pkg/config"
	"github.com/xuxiping/port-scan-mk3/pkg/speedctrl"
)

func TestPollPressureAPI_ThreeConsecutiveFailures_SendsErrorAndExits(t *testing.T) {
	server := newScriptedPressureServer(t)
	ctrl := speedctrl.NewController()
	poller := startTestPressurePoller(t, config.Config{
		PressureAPI:      server.server.URL,
		PressureInterval: 10 * time.Millisecond,
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

func TestPollPressureAPI_FailureRecoveryAfterTwoFails_SkipsThirdAndContinues(t *testing.T) {
	server := newScriptedPressureServer(t)
	ctrl := speedctrl.NewController()
	ctrl.SetAPIPaused(true)
	poller := startTestPressurePoller(t, config.Config{
		PressureAPI:      server.server.URL,
		PressureInterval: 10 * time.Millisecond,
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
	poller := startTestPressurePoller(t, config.Config{
		PressureAPI:      server.server.URL,
		PressureInterval: 10 * time.Millisecond,
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
	poller := startTestPressurePoller(t, config.Config{
		PressureAPI:      server.server.URL,
		PressureInterval: 10 * time.Millisecond,
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
	poller := startTestPressurePoller(t, config.Config{
		PressureAPI:      server.server.URL,
		PressureInterval: 10 * time.Millisecond,
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
	poller := startTestPressurePoller(t, config.Config{
		PressureAPI:      server.server.URL,
		PressureInterval: 10 * time.Millisecond,
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
	poller := startTestPressurePoller(t, config.Config{
		PressureAPI:      server.server.URL,
		PressureInterval: 10 * time.Millisecond,
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
	poller := startTestPressurePoller(t, config.Config{
		PressureAPI:      server.server.URL,
		PressureInterval: 10 * time.Millisecond,
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
	poller := startTestPressurePoller(t, config.Config{
		PressureAPI:      server.server.URL,
		PressureInterval: 10 * time.Millisecond,
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
	poller := startTestPressurePoller(t, config.Config{
		PressureAPI:      server.server.URL,
		PressureInterval: 10 * time.Millisecond,
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
	poller := startTestPressurePoller(t, config.Config{
		PressureAPI:      server.server.URL,
		PressureInterval: 10 * time.Millisecond,
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
	poller := startTestPressurePoller(t, config.Config{
		PressureAPI:      server.server.URL,
		PressureInterval: 10 * time.Millisecond,
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

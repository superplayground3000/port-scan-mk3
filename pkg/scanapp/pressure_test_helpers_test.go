package scanapp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
	"github.com/xuxiping/port-scan-mk3/pkg/speedctrl"
)

const pressureTestTimeout = 5 * time.Second

type scriptedPressureHTTPResponse struct {
	statusCode int
	body       string
}

type scriptedPressureServer struct {
	server    *httptest.Server
	requests  chan struct{}
	responses chan scriptedPressureHTTPResponse
}

func newScriptedPressureServer(t *testing.T) *scriptedPressureServer {
	t.Helper()

	scriptedServer := &scriptedPressureServer{
		requests:  make(chan struct{}),
		responses: make(chan scriptedPressureHTTPResponse),
	}
	scriptedServer.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case scriptedServer.requests <- struct{}{}:
		case <-r.Context().Done():
			return
		}

		select {
		case response := <-scriptedServer.responses:
			w.WriteHeader(response.statusCode)
			if _, err := io.WriteString(w, response.body); err != nil {
				return
			}
		case <-r.Context().Done():
			return
		}
	}))
	t.Cleanup(scriptedServer.server.Close)

	return scriptedServer
}

func (s *scriptedPressureServer) respond(t *testing.T, response scriptedPressureHTTPResponse) {
	t.Helper()

	select {
	case <-s.requests:
	case <-time.After(pressureTestTimeout):
		t.Fatal("timed out waiting for a pressure API request")
	}

	select {
	case s.responses <- response:
	case <-time.After(pressureTestTimeout):
		t.Fatal("timed out sending a scripted pressure API response")
	}
}

type testPressurePoller struct {
	cancel context.CancelFunc
	done   chan struct{}
	errCh  chan error
	once   sync.Once
}

func startTestPressurePoller(t *testing.T, cfg config.Config, opts RunOptions, ctrl *speedctrl.Controller, logger *scanLogger) *testPressurePoller {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	poller := &testPressurePoller{
		cancel: cancel,
		done:   make(chan struct{}),
		errCh:  make(chan error, 1),
	}
	go func() {
		defer close(poller.done)
		pollPressureAPI(ctx, cfg, opts, ctrl, logger, poller.errCh)
	}()
	t.Cleanup(func() {
		poller.stop(t)
		poller.makeSureNoError(t)
	})

	return poller
}

func (p *testPressurePoller) stop(t *testing.T) {
	t.Helper()

	p.once.Do(func() {
		p.cancel()
		select {
		case <-p.done:
		case <-time.After(pressureTestTimeout):
			t.Fatal("timed out waiting for the pressure poller to stop")
		}
	})
}

func (p *testPressurePoller) makeSureNoError(t *testing.T) {
	t.Helper()

	select {
	case err := <-p.errCh:
		if err != nil {
			t.Fatalf("pressure poller returned an error: %v", err)
		}
	default:
	}
}

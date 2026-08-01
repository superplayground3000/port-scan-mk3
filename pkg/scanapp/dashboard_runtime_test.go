package scanapp

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
)

type dashboardRendererStub struct {
	render func(io.Writer, dashboardSnapshot) error
}

func (s dashboardRendererStub) Render(w io.Writer, snap dashboardSnapshot) error {
	return s.render(w, snap)
}

func TestDashboardRuntime_ShouldEnableOnlyForHumanTTYStderr(t *testing.T) {
	t.Parallel()

	stderr := &bytes.Buffer{}
	tests := []struct {
		name  string
		cfg   config.Config
		isTTY bool
		want  bool
	}{
		{
			name:  "json format disables dashboard",
			cfg:   config.Config{Format: "json"},
			isTTY: true,
			want:  false,
		},
		{
			name:  "non tty stderr disables dashboard",
			cfg:   config.Config{Format: "human"},
			isTTY: false,
			want:  false,
		},
		{
			name:  "tty stderr and human format enables dashboard",
			cfg:   config.Config{Format: "human"},
			isTTY: true,
			want:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldEnableDashboard(tc.cfg, stderr, RunOptions{
				dashboardTerminalDetector: func(io.Writer) bool { return tc.isTTY },
			})
			if got != tc.want {
				t.Fatalf("shouldEnableDashboard() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDashboardRuntime_StartRefreshLoopUntilStopped(t *testing.T) {
	t.Parallel()

	state := newDashboardState(3, time.Now)
	stderr := &bytes.Buffer{}
	logger := newLogger("error", false, io.Discard)
	var renders atomic.Int32

	// The refresh loop is asynchronous, so the test waits for the event (the
	// second render) instead of sleeping a fixed budget and hoping it was long
	// enough. Windows' default timer granularity (~15.6ms) is coarser than the
	// 10ms interval and CI runners are loaded, so a fixed budget measures how
	// busy the machine is, not whether the loop refreshes.
	renderedTwice := make(chan struct{})
	runtime := newDashboardRuntime(state, stderr, dashboardRuntimeOptions{
		refreshInterval: 10 * time.Millisecond,
		renderer: dashboardRendererStub{render: func(w io.Writer, _ dashboardSnapshot) error {
			if renders.Add(1) == 2 {
				close(renderedTwice)
			}
			_, err := io.WriteString(w, "render\n")
			return err
		}},
		logger: logger,
	})

	ctx, cancel := context.WithCancel(context.Background())
	runtime.Start(ctx)

	select {
	case <-renderedTwice:
	case <-time.After(5 * time.Second):
		t.Fatalf("expected refresh loop to render at least twice within 5s, got %d", renders.Load())
	}

	cancel()
	runtime.Stop()

	afterStop := renders.Load()
	time.Sleep(20 * time.Millisecond)
	if renders.Load() != afterStop {
		t.Fatalf("expected refresh loop to stop after Stop, got %d -> %d", afterStop, renders.Load())
	}
	if !bytes.Contains(stderr.Bytes(), []byte("render\n")) {
		t.Fatalf("expected dashboard render output on stderr, got %q", stderr.String())
	}
}

func TestRun_WhenRichDashboardEnabled_RendersPeriodicUpdatesToStderr(t *testing.T) {
	tmp := t.TempDir()
	cidrFile := filepath.Join(tmp, "cidr.csv")
	portFile := filepath.Join(tmp, "ports.csv")
	outFile := filepath.Join(tmp, "scan_results.csv")

	if err := os.WriteFile(cidrFile, []byte("fab_name,ip,ip_cidr,cidr_name\nfab1,127.0.0.1,127.0.0.1/32,loopback\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portFile, []byte("1/tcp\n2/tcp\n3/tcp\n4/tcp\n"), 0o644); err != nil {
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
		Format:             "human",
		PreScanPingTimeout: 100 * time.Millisecond,
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var renders atomic.Int32
	cfg.Resume = generateBucketFile(t, cfg, filepath.Join(tmp, "buckets.json"), "")
	err := Run(ctx, cfg, stdout, stderr, RunOptions{
		DisableKeyboard: true,
		Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
		dashboardTerminalDetector: func(io.Writer) bool { return true },
		dashboardRefreshInterval:  10 * time.Millisecond,
		dashboardRenderer: dashboardRendererStub{render: func(w io.Writer, _ dashboardSnapshot) error {
			n := renders.Add(1)
			_, err := io.WriteString(w, "dashboard tick\n")
			if n >= 2 {
				cancel()
			}
			return err
		}},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation after dashboard refresh, got %v", err)
	}
	if renders.Load() < 2 {
		t.Fatalf("expected dashboard to render at least twice, got %d", renders.Load())
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected dashboard to avoid stdout, got %q", stdout.String())
	}
	if !bytes.Contains(stderr.Bytes(), []byte("dashboard tick\n")) {
		t.Fatalf("expected dashboard output on stderr, got %q", stderr.String())
	}
}

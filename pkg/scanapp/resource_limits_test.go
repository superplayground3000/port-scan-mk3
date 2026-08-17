package scanapp

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
	"github.com/xuxiping/port-scan-mk3/pkg/pressure"
	"github.com/xuxiping/port-scan-mk3/pkg/speedctrl"
	"github.com/xuxiping/port-scan-mk3/pkg/state"
	"github.com/xuxiping/port-scan-mk3/pkg/task"
)

func TestRunPrePingRejectsCIDRRecordLimitBeforePingOrOutput(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cidrPath := filepath.Join(dir, "targets.csv")
	if err := os.WriteFile(cidrPath, []byte("ip,ip_cidr\n192.0.2.1,192.0.2.0/24\n192.0.2.2,192.0.2.0/24\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.ParsePrePing([]string{"-cidr-file", cidrPath, "-output", filepath.Join(dir, "results.csv"), "-cidr-input-record-limit", "1"})
	if err != nil {
		t.Fatal(err)
	}
	checker := &fakePreScanChecker{}
	err = RunPrePing(context.Background(), cfg, &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{ReachabilityChecker: checker})
	if err == nil || !strings.Contains(err.Error(), "-cidr-input-record-limit") {
		t.Fatalf("RunPrePing() error = %v, want CIDR record limit", err)
	}
	if calls := checker.calls(); len(calls) != 0 {
		t.Fatalf("ping calls = %v, want none", calls)
	}
	matches, globErr := filepath.Glob(filepath.Join(dir, "unreachable_results-*.csv"))
	if globErr != nil {
		t.Fatal(globErr)
	}
	if len(matches) != 0 {
		t.Fatalf("output files = %v, want none", matches)
	}
}

func TestPressureResponseLimitFailureUsesThreeFailurePolicy(t *testing.T) {
	t.Parallel()

	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = w.Write([]byte(`{"pressure":85.1}`))
	}))
	defer server.Close()
	policy, err := config.SimplePressure(server.URL, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	source, err := newPressureSource(policy, pressure.ResponseLimits{MaxBytes: 8})
	if err != nil {
		t.Fatal(err)
	}
	errCh := make(chan error, 1)
	done := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		pollPressureAPI(ctx, time.Millisecond, source, RunOptions{}, speedctrl.NewController(), newTestLogger(), errCh)
		close(done)
	}()
	select {
	case err = <-errCh:
		if !strings.Contains(err.Error(), "pressure api failed 3 times") || !strings.Contains(err.Error(), "-pressure-response-size-limit-mb") {
			t.Fatalf("poll error = %v, want limit failure after three attempts", err)
		}
	case <-time.After(time.Second):
		t.Fatal("pressure poller did not stop after three limit failures")
	}
	<-done
	if requests.Load() != 3 {
		t.Fatalf("request count = %d, want 3", requests.Load())
	}
}

func TestRunRejectsSnapshotLimitsBeforeOutputOrTCP(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cidrPath := filepath.Join(dir, "targets.csv")
	snapshotPath := filepath.Join(dir, "resume.json")
	outputPath := filepath.Join(dir, "results.csv")
	if err := os.WriteFile(cidrPath, []byte("ip,ip_cidr\n192.0.2.1,192.0.2.0/24\n198.51.100.1,198.51.100.0/24\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := state.SaveSnapshot(snapshotPath, state.Snapshot{Chunks: []task.Chunk{
		{CIDR: "192.0.2.0/24", Ports: []string{"80/tcp"}, TotalCount: 1, Status: "pending"},
		{CIDR: "198.51.100.0/24", Ports: []string{"80/tcp"}, TotalCount: 1, Status: "pending"},
	}, RichDenyExcluded: true}); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.ParseScan([]string{
		"-cidr-file", cidrPath,
		"-resume", snapshotPath,
		"-output", outputPath,
		"-disable-api",
		"-snapshot-chunk-limit", "1",
	})
	if err != nil {
		t.Fatal(err)
	}
	var dials atomic.Int64
	err = Run(context.Background(), cfg, &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{
		DisableKeyboard: true,
		Dial: func(context.Context, string, string) (net.Conn, error) {
			dials.Add(1)
			return nil, errors.New("unexpected dial")
		},
	})
	if err == nil || !strings.Contains(err.Error(), "-snapshot-chunk-limit") {
		t.Fatalf("Run() error = %v, want snapshot chunk limit", err)
	}
	if dials.Load() != 0 {
		t.Fatalf("dial count = %d, want 0", dials.Load())
	}
	if _, statErr := os.Stat(outputPath); !os.IsNotExist(statErr) {
		t.Fatalf("output stat error = %v, want no output", statErr)
	}
}

func TestGenerateBucketsRejectsPortAndSnapshotLimitsWithoutSnapshotArtifact(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cidrPath := filepath.Join(dir, "targets.csv")
	portPath := filepath.Join(dir, "ports.csv")
	if err := os.WriteFile(cidrPath, []byte("ip,ip_cidr\n192.0.2.1,192.0.2.0/24\n198.51.100.1,198.51.100.0/24\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portPath, []byte("80/tcp\n443/tcp\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	for name, flags := range map[string][]string{
		"port":     {"-port-input-record-limit", "1"},
		"snapshot": {"-snapshot-chunk-limit", "1"},
	} {
		t.Run(name, func(t *testing.T) {
			out := filepath.Join(dir, name+".json")
			args := []string{"-cidr-file", cidrPath, "-port-file", portPath, "-buckets-out", out}
			cfg, err := config.ParseGenerateBuckets(append(args, flags...))
			if err != nil {
				t.Fatal(err)
			}
			err = GenerateBuckets(context.Background(), cfg, &bytes.Buffer{}, GenerateBucketsOptions{})
			if err == nil || !strings.Contains(err.Error(), "limit") {
				t.Fatalf("GenerateBuckets() error = %v, want limit error", err)
			}
			if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
				t.Fatalf("snapshot stat error = %v, want no artifact", statErr)
			}
		})
	}
}

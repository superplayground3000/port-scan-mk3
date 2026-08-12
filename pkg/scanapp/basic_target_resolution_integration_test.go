package scanapp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
)

func TestRun_CurrentBasicSnapshotUsesRecordedFallbackWithoutPortFile(t *testing.T) {
	tmp := t.TempDir()
	cidrFile := filepath.Join(tmp, "basic.csv")
	portFile := filepath.Join(tmp, "ports.csv")
	snapshotFile := filepath.Join(tmp, "buckets.json")
	if err := os.WriteFile(cidrFile, []byte(
		"ip,ip_cidr,port\n"+
			"192.0.2.1,192.0.2.0/24,\n"+
			"192.0.2.2,192.0.2.0/24,8443\n",
	), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portFile, []byte("80/tcp\n443/tcp\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	bucketCfg := mustGenerateBucketsConfig(t, config.GenerateBucketsValues{
		CIDRFile:       cidrFile,
		CIDRIPCol:      "ip",
		CIDRIPCidrCol:  "ip_cidr",
		PortFile:       portFile,
		SnapshotOutput: snapshotFile,
		Workers:        4,
	})
	if err := GenerateBuckets(context.Background(), bucketCfg, &bytes.Buffer{}, GenerateBucketsOptions{}); err != nil {
		t.Fatalf("GenerateBuckets() error = %v", err)
	}

	var (
		mu    sync.Mutex
		tasks []string
	)
	scanCfg := scanConfigFixture{
		CIDRFile:       cidrFile,
		Output:         filepath.Join(tmp, "results.csv"),
		Timeout:        10 * time.Millisecond,
		BucketRate:     1000,
		BucketCapacity: 1000,
		Workers:        4,
		Pressure:       pressureConfigFixture{Disabled: true},
		Resume:         snapshotFile,
		LogLevel:       "error",
	}
	if err := Run(context.Background(), scanConfigurationFromFixture(t, scanCfg), &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{
		Dial: func(context.Context, string, string) (net.Conn, error) {
			return nil, errors.New("closed")
		},
		DisableKeyboard: true,
		TaskObserver: func(ip string, port int) {
			mu.Lock()
			tasks = append(tasks, fmt.Sprintf("%s:%d/tcp", ip, port))
			mu.Unlock()
		},
	}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	mu.Lock()
	slices.Sort(tasks)
	got := append([]string(nil), tasks...)
	mu.Unlock()
	want := []string{"192.0.2.1:443/tcp", "192.0.2.1:80/tcp", "192.0.2.2:8443/tcp"}
	if !slices.Equal(got, want) {
		t.Fatalf("dispatched tasks = %v, want %v", got, want)
	}
}

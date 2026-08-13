//go:build linux && !race

package scanapp

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
)

func TestRunPrePing_AllReachableRichInputKeepsAllocationsWithinScaleBudget(t *testing.T) {
	const records = 100_000
	tmp := t.TempDir()
	cidrFile := writeAllReachableRichFixture(t, tmp, records)
	output := filepath.Join(tmp, "results.csv")
	cfg := mustPrePingConfig(t, config.PrePingValues{
		CIDRFile:         cidrFile,
		CIDRIPCol:        "src_ip",
		CIDRIPCidrCol:    "src_network_segment",
		Output:           output,
		Workers:          16,
		PingTimeout:      time.Second,
		ProgressInterval: records + 1,
	})

	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	var stdout bytes.Buffer
	if err := RunPrePing(context.Background(), cfg, &stdout, &bytes.Buffer{}, RunOptions{
		ReachabilityChecker: &fakePreScanChecker{},
	}); err != nil {
		t.Fatal(err)
	}
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	allocated := after.TotalAlloc - before.TotalAlloc

	const maximumAllocated = uint64(450_000_000)
	t.Logf("allocated bytes = %d", allocated)
	if allocated > maximumAllocated {
		t.Fatalf("allocated bytes = %d, want at most %d", allocated, maximumAllocated)
	}
}

func TestRunPrePing_AllUnreachableRichInputKeepsAllocationsWithinScaleBudget(t *testing.T) {
	const records = 100_000
	tmp := t.TempDir()
	cidrFile := writeAllReachableRichFixture(t, tmp, records)
	output := filepath.Join(tmp, "results.csv")
	cfg := mustPrePingConfig(t, config.PrePingValues{
		CIDRFile:         cidrFile,
		CIDRIPCol:        "src_ip",
		CIDRIPCidrCol:    "src_network_segment",
		Output:           output,
		Workers:          16,
		PingTimeout:      time.Second,
		ProgressInterval: records + 1,
	})

	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	if err := RunPrePing(context.Background(), cfg, &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{
		ReachabilityChecker: unreachableScaleChecker{},
	}); err != nil {
		t.Fatal(err)
	}
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	allocated := after.TotalAlloc - before.TotalAlloc

	const maximumAllocated = uint64(500_000_000)
	t.Logf("allocated bytes = %d", allocated)
	if allocated > maximumAllocated {
		t.Fatalf("allocated bytes = %d, want at most %d", allocated, maximumAllocated)
	}
}

func BenchmarkRunPrePingRichTenThousandRows(b *testing.B) {
	const records = 10_000
	tmp := b.TempDir()
	cidrFile := writeAllReachableRichFixture(b, tmp, records)
	cfg := mustPrePingConfig(b, config.PrePingValues{
		CIDRFile:         cidrFile,
		CIDRIPCol:        "src_ip",
		CIDRIPCidrCol:    "src_network_segment",
		Output:           filepath.Join(tmp, "results.csv"),
		Workers:          16,
		PingTimeout:      time.Second,
		ProgressInterval: records + 1,
	})
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if err := RunPrePing(context.Background(), cfg, &bytes.Buffer{}, &bytes.Buffer{}, RunOptions{
			ReachabilityChecker: unreachableScaleChecker{},
		}); err != nil {
			b.Fatal(err)
		}
	}
}

type unreachableScaleChecker struct{}

func (unreachableScaleChecker) Check(_ context.Context, ip string, _ time.Duration) ReachabilityResult {
	return ReachabilityResult{IP: ip, Reachable: false}
}

func writeAllReachableRichFixture(t testing.TB, dir string, records int) string {
	t.Helper()
	path := filepath.Join(dir, "rich.csv")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := bufio.NewWriter(file)
	if _, err := fmt.Fprintln(writer, "src_ip,src_network_segment,dst_ip,dst_network_segment,service_label,protocol,port,decision,matched_policy_id,reason"); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < records; index++ {
		value := index + 1
		a := byte(value >> 16)
		b := byte(value >> 8)
		c := byte(value)
		if _, err := fmt.Fprintf(writer, "10.%d.%d.%d,10.0.0.0/8,127.%d.%d.%d,127.0.0.0/8,service,tcp,443,accept,P-1,allow\n", a, b, c, a, b, c); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

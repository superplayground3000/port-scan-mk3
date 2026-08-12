//go:build linux && !race

package input_test

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/input"
)

func TestLoadCIDRsHundredThousandRowsKeepsPeakHeapWithinScaleBudget(t *testing.T) {
	const records = 100_000
	path := writeCIDRMemoryFixture(t, records)

	peak := peakHeapDuring(t, func() {
		loaded, err := input.LoadCIDRsFileWithColumnsContext(context.Background(), path, "ip", "ip_cidr", input.CIDRLimits{})
		if err != nil {
			t.Fatal(err)
		}
		if len(loaded) != records {
			t.Fatalf("record count = %d, want %d", len(loaded), records)
		}
		runtime.KeepAlive(loaded)
	})
	const maximumPeakHeap = uint64(70_000_000)
	t.Logf("peak heap increase = %d bytes", peak)
	if peak > maximumPeakHeap {
		t.Fatalf("peak heap increase = %d bytes, want at most %d", peak, maximumPeakHeap)
	}
}

func BenchmarkLoadCIDRsHundredThousandRows(b *testing.B) {
	benchmarkLoadCIDRs(b, 100_000)
}

func BenchmarkLoadCIDRsMillionRows(b *testing.B) {
	benchmarkLoadCIDRs(b, 1_000_000)
}

func benchmarkLoadCIDRs(b *testing.B, records int) {
	path := writeCIDRMemoryFixture(b, records)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		loaded, err := input.LoadCIDRsFileWithColumnsContext(context.Background(), path, "ip", "ip_cidr", input.CIDRLimits{})
		if err != nil {
			b.Fatal(err)
		}
		runtime.KeepAlive(loaded)
	}
}

func writeCIDRMemoryFixture(t testing.TB, records int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "input.csv")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := bufio.NewWriter(file)
	if _, err := fmt.Fprintln(writer, "ip,ip_cidr,fab_name,cidr_name"); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < records; index++ {
		address := fmt.Sprintf("127.%d.%d.%d", byte((index+1)>>16), byte((index+1)>>8), byte(index+1))
		if _, err := fmt.Fprintf(writer, "%s,127.0.0.0/8,fab,xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx\n", address); err != nil {
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

func peakHeapDuring(t *testing.T, action func()) uint64 {
	t.Helper()
	runtime.GC()
	debug.FreeOSMemory()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	var peak atomic.Uint64
	peak.Store(before.HeapInuse)
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		ticker := time.NewTicker(100 * time.Microsecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				var sample runtime.MemStats
				runtime.ReadMemStats(&sample)
				for current := peak.Load(); sample.HeapInuse > current && !peak.CompareAndSwap(current, sample.HeapInuse); current = peak.Load() {
				}
			}
		}
	}()
	action()
	close(done)
	<-finished
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	if after.HeapInuse > peak.Load() {
		peak.Store(after.HeapInuse)
	}
	if peak.Load() < before.HeapInuse {
		return 0
	}
	return peak.Load() - before.HeapInuse
}

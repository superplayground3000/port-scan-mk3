package scanapp

import (
	"context"
	"encoding/binary"
	"net"
	"runtime"
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/input"
	"github.com/xuxiping/port-scan-mk3/pkg/task"
)

const boundedFallbackTasks = 65_535

func boundedFallbackPlanInput(t testing.TB) (runInputs, []task.Chunk) {
	t.Helper()
	_, network, err := net.ParseCIDR("10.42.0.0/16")
	if err != nil {
		t.Fatal(err)
	}
	return runInputs{
		cidrRecords: []input.CIDRRecord{{
			CIDR: network.String(), Net: network, Selector: network, IPRaw: network.String(), RowNumber: 1,
		}},
		portSpecs: []input.PortSpec{{Number: 443, Proto: "tcp", Raw: "443/tcp"}},
	}, []task.Chunk{{CIDR: network.String(), Ports: []string{"443/tcp"}, TotalCount: boundedFallbackTasks, Status: "pending"}}
}

func TestPrepareRuntimePlanBasicFallbackHasBoundedAllocation(t *testing.T) {
	inputs, chunks := boundedFallbackPlanInput(t)
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	plan, err := prepareRuntimePlanContext(context.Background(), runtimePolicy{bucketRate: 1_000_000, bucketCapacity: 1}, inputs, nil, chunks, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range plan.runtimes {
		if item.bkt != nil {
			item.bkt.Close()
		}
	}
	runtime.ReadMemStats(&after)
	allocated := after.TotalAlloc - before.TotalAlloc
	const maxAllocated = uint64(26_214_000)
	if allocated > maxAllocated {
		t.Fatalf("basic fallback allocated %d bytes for %d tasks, want <= %d", allocated, boundedFallbackTasks, maxAllocated)
	}
}

func BenchmarkPrepareRuntimePlanBasicFallback65535(b *testing.B) {
	inputs, template := boundedFallbackPlanInput(b)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		chunks := append([]task.Chunk(nil), template...)
		plan, err := prepareRuntimePlanContext(context.Background(), runtimePolicy{bucketRate: 1_000_000, bucketCapacity: 1}, inputs, nil, chunks, nil)
		if err != nil {
			b.Fatal(err)
		}
		for _, item := range plan.runtimes {
			if item.bkt != nil {
				item.bkt.Close()
			}
		}
	}
}

func TestBasicFallbackFilterCompactsItsExclusiveTargetSlice(t *testing.T) {
	t.Parallel()

	targets := []scanTarget{{ip: "10.0.0.1"}, {ip: "10.0.0.2"}, {ip: "10.0.0.3"}}
	firstSlot := &targets[0]
	filtered, err := filterBasicFallbackTargetsContext(context.Background(), targets, func(ip string) bool {
		return ip != "10.0.0.1"
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 2 || &filtered[0] != firstSlot || filtered[0].ip != "10.0.0.2" || filtered[1].ip != "10.0.0.3" {
		t.Fatalf("in-place filtered targets = %+v", filtered)
	}
}

func TestBasicFallbackOverlapUsesSortedCIDRWinnerAndTargetOrder(t *testing.T) {
	t.Parallel()

	record := func(boundary, selector string) input.CIDRRecord {
		_, network, err := net.ParseCIDR(boundary)
		if err != nil {
			t.Fatal(err)
		}
		_, selection, err := net.ParseCIDR(selector)
		if err != nil {
			t.Fatal(err)
		}
		return input.CIDRRecord{CIDR: network.String(), Net: network, Selector: selection, IPRaw: selector}
	}
	resolution, err := resolveBasicFallbackTargetsContext(context.Background(), []input.CIDRRecord{
		record("10.0.0.0/24", "10.0.0.0/30"),
		record("10.0.0.0/23", "10.0.0.1/32"),
	}, []int{443}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wide, err := resolution.groupForChunk(task.Chunk{CIDR: "10.0.0.0/23", Ports: []string{"443/tcp"}})
	if err != nil {
		t.Fatal(err)
	}
	narrow, err := resolution.groupForChunk(task.Chunk{CIDR: "10.0.0.0/24", Ports: []string{"443/tcp"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(wide.targets) != 1 || wide.targets[0].ip != "10.0.0.1" {
		t.Fatalf("lexically first CIDR targets = %+v", wide.targets)
	}
	want := []uint32{ipv4Value("10.0.0.0"), ipv4Value("10.0.0.2"), ipv4Value("10.0.0.3")}
	if len(narrow.targets) != len(want) {
		t.Fatalf("narrow targets = %+v", narrow.targets)
	}
	for index := range want {
		if narrow.targets[index].ipU32 != want[index] {
			t.Fatalf("narrow target[%d] = %s", index, narrow.targets[index].ip)
		}
	}
}

func ipv4Value(raw string) uint32 {
	return binary.BigEndian.Uint32(net.ParseIP(raw).To4())
}

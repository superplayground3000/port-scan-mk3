package scanapp

import (
	"context"
	"encoding/binary"
	"net"
	"runtime"
	"testing"
	"unsafe"

	"github.com/xuxiping/port-scan-mk3/pkg/input"
	"github.com/xuxiping/port-scan-mk3/pkg/task"
)

const boundedFallbackTasks = 65_535

func TestBasicScanTargetFitsCompactBudget(t *testing.T) {
	t.Parallel()

	if size := unsafe.Sizeof(basicScanTarget{}); size > 32 {
		t.Fatalf("basicScanTarget size = %d, want <= 32", size)
	}
}

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
	const maxAllocated = uint64(13_107_000)
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

	targets := []basicScanTarget{{ip: "10.0.0.1"}, {ip: "10.0.0.2"}, {ip: "10.0.0.3"}}
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
	if len(wide.basicTargets) != 1 || wide.basicTargets[0].ip != "10.0.0.1" {
		t.Fatalf("lexically first CIDR targets = %+v", wide.basicTargets)
	}
	want := []uint32{ipv4Value("10.0.0.0"), ipv4Value("10.0.0.2"), ipv4Value("10.0.0.3")}
	if len(narrow.basicTargets) != len(want) {
		t.Fatalf("narrow targets = %+v", narrow.basicTargets)
	}
	for index := range want {
		if narrow.basicTargets[index].ipU32 != want[index] {
			t.Fatalf("narrow target[%d] = %s", index, narrow.basicTargets[index].ip)
		}
	}
}

func ipv4Value(raw string) uint32 {
	return binary.BigEndian.Uint32(net.ParseIP(raw).To4())
}

func TestPrepareRuntimePlanBasicFallbackPreservesPerRowMetadataAndResumeOrder(t *testing.T) {
	t.Parallel()

	_, boundary, err := net.ParseCIDR("10.0.0.0/24")
	if err != nil {
		t.Fatal(err)
	}
	record := func(ip, fabric, name string) input.CIDRRecord {
		parsed := net.ParseIP(ip)
		selector := &net.IPNet{IP: parsed, Mask: net.CIDRMask(32, 32)}
		return input.CIDRRecord{
			CIDR: boundary.String(), Net: boundary, Selector: selector, IPRaw: ip,
			FabName: fabric, CIDRName: name,
		}
	}
	inputs := runInputs{
		cidrRecords: []input.CIDRRecord{
			record("10.0.0.2", "fabric-b", "name-b"),
			record("10.0.0.1", "fabric-a", "name-a"),
		},
		portSpecs: []input.PortSpec{{Number: 443, Proto: "tcp", Raw: "443/tcp"}},
	}
	chunks := []task.Chunk{{
		CIDR: boundary.String(), Ports: []string{"443/tcp"}, NextIndex: 1, TotalCount: 2, Status: "scanning",
	}}
	plan, err := prepareRuntimePlanContext(context.Background(), runtimePolicy{bucketRate: 1_000_000, bucketCapacity: 1}, inputs, nil, chunks, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer plan.runtimes[0].bkt.Close()
	if len(plan.runtimes) != 1 || len(plan.runtimes[0].targets) != 0 || len(plan.runtimes[0].basicTargets) != 2 {
		t.Fatalf("runtime target storage = %+v", plan.runtimes)
	}
	first, firstPort, err := indexToChunkRuntimeTarget(plan.runtimes[0], 0)
	if err != nil {
		t.Fatal(err)
	}
	second, secondPort, err := indexToChunkRuntimeTarget(plan.runtimes[0], 1)
	if err != nil {
		t.Fatal(err)
	}
	if first.ip != "10.0.0.1" || firstPort != 443 || first.meta.fabName != "fabric-a" || first.meta.cidrName != "name-a" {
		t.Fatalf("first target = %+v port=%d", first, firstPort)
	}
	if second.ip != "10.0.0.2" || secondPort != 443 || second.meta.fabName != "fabric-b" || second.meta.cidrName != "name-b" {
		t.Fatalf("second target = %+v port=%d", second, secondPort)
	}
	if plan.runtimes[0].tracker.Snapshot().NextIndex != 1 {
		t.Fatalf("resume cursor = %+v", plan.runtimes[0].tracker.Snapshot())
	}
}

func TestCIDRGroupRejectsMixedTargetStorage(t *testing.T) {
	t.Parallel()

	err := (cidrGroup{targets: []scanTarget{{ip: "10.0.0.1"}}, basicTargets: []basicScanTarget{{ip: "10.0.0.2"}}}).validateTargetStorage()
	if err == nil {
		t.Fatal("mixed target storage was accepted")
	}
}

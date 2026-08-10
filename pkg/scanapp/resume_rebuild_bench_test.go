package scanapp

import (
	"fmt"
	"net"
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/input"
	"github.com/xuxiping/port-scan-mk3/pkg/task"
)

// resume_rebuild_bench_test.go — Phase 0 of docs/speed-up-scan-prepare.
//
// These benchmarks reproduce the reported "6 hours in pre-scan preparation"
// path (the resume runtime rebuild, buildRuntimeWithPredicate) so a CPU profile
// can confirm which bottleneck dominates before any optimization:
//
//	A: re-expanding the ENTIRE input CSV every resume     -> basic_amplified_*
//	B: O(N^2) rich execution-key merge                    -> rich_precheck_scaling_*
//	C: double net.ParseIP per IP + ParseIP-per-sort-compare (taxes A and B)
//
// Run:
//	go test ./pkg/scanapp -run=XXX -bench=BenchmarkResumeRebuild -benchmem \
//	    -cpuprofile=/tmp/prep.prof
//	go tool pprof -top -nodecount=25 /tmp/prep.prof

const benchUnreachableCount = 42587

// benchBasicCIDR returns the i-th distinct /24 in 10.0.0.0/8 (up to 65536 of them).
func benchBasicCIDR(i int) string {
	return fmt.Sprintf("10.%d.%d.0/24", (i/256)%256, i%256)
}

func benchBasicRecord(i int) input.CIDRRecord {
	cidr := benchBasicCIDR(i)
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		panic(err)
	}
	return input.CIDRRecord{
		CIDR:     cidr,
		CIDRName: "http",
		Net:      ipnet,
		Selector: ipnet,
	}
}

func benchBasicRecords(n int) []input.CIDRRecord {
	recs := make([]input.CIDRRecord, 0, n)
	for i := 0; i < n; i++ {
		recs = append(recs, benchBasicRecord(i))
	}
	return recs
}

// benchUnreachable returns benchUnreachableCount uint32 IPv4s inside 10.0.0.0/8,
// overlapping the basic record space so the reachable predicate does real work.
func benchUnreachable(n int) []uint32 {
	// Spread sparsely (every 8th host) so no single /24 is entirely unreachable —
	// a realistic ping result leaves most CIDRs partially scannable.
	base := ipv4ToUint32("10.0.0.1")
	out := make([]uint32, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, base+uint32(i)*8)
	}
	return out
}

// benchRichPrecheckRecord is a single rich PRECHECK_ALLOW_ALL row whose
// dst_network_segment expands to every host in the segment, each getting a
// distinct execution key — the input shape that drives the O(N^2) merge.
func benchRichPrecheckRecord(segment string) input.CIDRRecord {
	_, ipnet, err := net.ParseCIDR(segment)
	if err != nil {
		panic(err)
	}
	return input.CIDRRecord{
		IsRich:            true,
		IsValid:           true,
		CIDR:              segment,
		Net:               ipnet,
		CIDRName:          "http",
		DstNetworkSegment: segment,
		ServiceLabel:      "web",
		Protocol:          "tcp",
		Port:              80,
		Decision:          "accept",
		PolicyID:          "P1",
		Reason:            reasonPrecheckAllowAll,
		ExecutionKey:      "seed:80/tcp",
		RowNumber:         1,
	}
}

// benchmarkBasicBuild profiles the basic-mode group build over `totalRecords`
// input rows. This is exactly the work buildRuntimeWithPredicate does on resume
// before it maps the pending chunks — and it always processes EVERY input row,
// so a 4000-row CSV pays for 4000 even when only 130 chunks are pending (problem
// A). The leaky-bucket construction is deliberately excluded: it spawns
// background timer goroutines that are not part of the parse cost.
func benchmarkBasicBuild(b *testing.B, totalRecords int) {
	records := benchBasicRecords(totalRecords)
	reachable := reachablePredicate(benchUnreachable(benchUnreachableCount))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := buildCIDRGroupsWithPredicate(records, reachable); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkRichPrecheck(b *testing.B, segment string) {
	records := []input.CIDRRecord{benchRichPrecheckRecord(segment)}
	reachable := reachablePredicate(benchUnreachable(benchUnreachableCount))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := buildRichGroupsWithPredicate(records, reachable); err != nil {
			b.Fatal(err)
		}
	}
}

// benchmarkResumeRuntime_IncompleteOfMany profiles the FULL resume runtime
// rebuild (buildRuntimeWithPredicate) for the reported shape: a bucket of
// totalChunks basic chunks over a totalChunks-row CSV, where only the first
// `incomplete` chunks still have work. Phase 2 skips expansion for the completed
// chunks, so cost should track `incomplete`, not `totalChunks`.
//
// buildRuntimeWithPredicate allocates a leaky-bucket goroutine per INCOMPLETE
// chunk; the buckets are Closed each iteration so goroutines do not accumulate
// across b.N and the incomplete count (130) bounds the background noise.
func benchmarkResumeRuntime_IncompleteOfMany(b *testing.B, totalChunks, incomplete int) {
	records := benchBasicRecords(totalChunks)
	ports := []input.PortSpec{{Number: 80, Proto: "tcp", Raw: "80/tcp"}}
	reachable := reachablePredicate(benchUnreachable(benchUnreachableCount))
	policy := runtimePolicy{bucketRate: 1000000, bucketCapacity: 1}

	// Build the resume chunk set: every /24 is a chunk; only the first
	// `incomplete` are still pending, the rest are fully scanned.
	base, err := buildFreshChunksForTest(records, ports, reachable)
	if err != nil {
		b.Fatal(err)
	}
	template := make([]task.Chunk, len(base))
	for i := range base {
		template[i] = base[i]
		if i >= incomplete {
			template[i].NextIndex = template[i].TotalCount
			template[i].ScannedCount = template[i].TotalCount
			template[i].Status = "completed"
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		chunks := make([]task.Chunk, len(template))
		copy(chunks, template)
		runtimes, err := buildRuntimeWithPredicate(chunks, records, ports, policy, reachable, nil)
		if err != nil {
			b.Fatal(err)
		}
		for _, rt := range runtimes {
			if rt.bkt != nil {
				rt.bkt.Close()
			}
		}
	}
}

func BenchmarkResumeRebuild(b *testing.B) {
	// A: input CSV is exactly the 130 pending rows.
	b.Run("basic_matched_130", func(b *testing.B) { benchmarkBasicBuild(b, 130) })
	// A amplified: only 130 chunks pending, but the input CSV holds 4000 rows —
	// the current code re-expands all 4000 and discards 3870.
	b.Run("basic_amplified_4000", func(b *testing.B) { benchmarkBasicBuild(b, 4000) })

	// B: single PRECHECK_ALLOW_ALL segment. Doubling the host count should ~4x
	// the time if the merge is quadratic (O(N^2)), ~2x if linear.
	b.Run("rich_precheck_scaling_slash24_256", func(b *testing.B) { benchmarkRichPrecheck(b, "10.20.0.0/24") })
	b.Run("rich_precheck_scaling_slash23_512", func(b *testing.B) { benchmarkRichPrecheck(b, "10.20.0.0/23") })
	b.Run("rich_precheck_scaling_slash22_1024", func(b *testing.B) { benchmarkRichPrecheck(b, "10.20.0.0/22") })
	b.Run("rich_precheck_scaling_slash21_2048", func(b *testing.B) { benchmarkRichPrecheck(b, "10.20.0.0/21") })
	b.Run("rich_precheck_scaling_slash20_4096", func(b *testing.B) { benchmarkRichPrecheck(b, "10.20.0.0/20") })

	// Phase 2 (design.md §3.2): the reported resume shape — 130 incomplete chunks
	// among 4000, over a 4000-row CSV. Pre-Phase-2 this expanded all 4000 chunks
	// every resume; now only the 130 incomplete ones are expanded. The
	// all-incomplete control isolates the completed-chunk skip win.
	b.Run("resume_runtime_130_of_4000", func(b *testing.B) { benchmarkResumeRuntime_IncompleteOfMany(b, 4000, 130) })
	b.Run("resume_runtime_4000_of_4000", func(b *testing.B) { benchmarkResumeRuntime_IncompleteOfMany(b, 4000, 4000) })
}

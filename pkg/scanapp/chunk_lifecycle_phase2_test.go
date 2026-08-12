package scanapp

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/input"
	"github.com/xuxiping/port-scan-mk3/pkg/speedctrl"
	"github.com/xuxiping/port-scan-mk3/pkg/task"
)

// chunk_lifecycle_phase2_test.go — Phase 2 of docs/speed-up-scan-prepare
// (design.md §3.2, §3.4). buildRuntimeWithPredicate must:
//   - preserve byte-for-byte the runtimes it produces for INCOMPLETE (still-to-scan)
//     work (differential golden below), and
//   - stop expanding COMPLETED chunks entirely, while keeping them represented in
//     the returned runtimes (so persistResumeSnapshot re-saves them verbatim).

// ---------------------------------------------------------------------------
// Reference implementation: the pre-Phase-2 algorithm (build groups over the
// ENTIRE record set, then map every chunk against those groups). The Phase-1
// group builders are unchanged by Phase 2, so this faithfully reproduces the old
// buildRuntimeWithPredicate target/port/meta/order output for any chunk set. The
// differential golden asserts the refactored function matches it whenever every
// chunk is incomplete (the case where filtering keeps all records).
func referenceBuildRuntime(t *testing.T, chunks []task.Chunk, records []input.CIDRRecord, defaultPorts []input.PortSpec, reachable func(string) bool) []*chunkRuntime {
	t.Helper()
	richMode := hasRichRecords(records)
	var (
		groups map[string]cidrGroup
		err    error
	)
	if richMode {
		groups, err = buildRichGroupsWithPredicate(records, reachable)
	} else {
		groups, err = buildCIDRGroupsWithPredicate(records, reachable)
	}
	if err != nil {
		t.Fatalf("reference group build failed: %v", err)
	}

	runtimes := make([]*chunkRuntime, 0, len(chunks))
	for i := range chunks {
		ch := &chunks[i]
		group, ok := groups[ch.CIDR]
		if !ok {
			t.Fatalf("reference: chunk %s has no group", ch.CIDR)
		}
		portRows := ch.Ports
		if len(portRows) == 0 {
			if richMode {
				richPort := 1
				if len(group.targets) > 0 && group.targets[0].port > 0 {
					richPort = group.targets[0].port
				}
				portRows = []string{fmt.Sprintf("%d/tcp", richPort)}
			} else {
				for _, p := range defaultPorts {
					portRows = append(portRows, p.Raw)
				}
			}
			ch.Ports = append(ch.Ports, portRows...)
		}
		ports, err := parsePortRows(portRows)
		if err != nil {
			t.Fatalf("reference: parse ports for %s: %v", ch.CIDR, err)
		}
		if ch.TotalCount == 0 {
			ch.TotalCount = len(group.targets) * len(ports)
		}
		runtimes = append(runtimes, &chunkRuntime{
			ipCidr:  ch.CIDR,
			ports:   ports,
			targets: group.targets,
			state:   ch,
		})
	}
	return runtimes
}

func cloneChunks(chunks []task.Chunk) []task.Chunk {
	out := make([]task.Chunk, len(chunks))
	for i := range chunks {
		out[i] = chunks[i]
		out[i].Ports = append([]string(nil), chunks[i].Ports...)
	}
	return out
}

// assertRuntimesEquivalent compares two runtime slices by the observable target
// content (ip, ipU32, port, full meta, order), ports, and total_count.
func assertRuntimesEquivalent(t *testing.T, got, want []*chunkRuntime) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("runtime count: got %d, want %d", len(got), len(want))
	}
	for i := range want {
		g, w := got[i], want[i]
		if g.ipCidr != w.ipCidr {
			t.Fatalf("runtime[%d] ipCidr: got %q, want %q", i, g.ipCidr, w.ipCidr)
		}
		if fmt.Sprintf("%v", g.ports) != fmt.Sprintf("%v", w.ports) {
			t.Fatalf("runtime[%d] %s ports: got %v, want %v", i, w.ipCidr, g.ports, w.ports)
		}
		if g.state.TotalCount != w.state.TotalCount {
			t.Fatalf("runtime[%d] %s task count: got %d, want %d", i, w.ipCidr, g.state.TotalCount, w.state.TotalCount)
		}
		for j := 0; j < w.state.TotalCount; j++ {
			gt, gp, err := indexToChunkRuntimeTarget(g, j)
			if err != nil {
				t.Fatalf("runtime[%d] target[%d]: %v", i, j, err)
			}
			wt, wp, err := indexToChunkRuntimeTarget(w, j)
			if err != nil {
				t.Fatalf("reference runtime[%d] target[%d]: %v", i, j, err)
			}
			if gt.ip != wt.ip || gt.ipU32 != wt.ipU32 || gp != wp || gt.ipCidr != wt.ipCidr || gt.meta != wt.meta {
				t.Fatalf("runtime[%d] %s target[%d]:\n got  %+v\n want %+v", i, w.ipCidr, j, gt, wt)
			}
		}
		if g.state.TotalCount != w.state.TotalCount {
			t.Fatalf("runtime[%d] %s TotalCount: got %d, want %d", i, w.ipCidr, g.state.TotalCount, w.state.TotalCount)
		}
	}
}

func richRecord(row int, segment, dstIP, execKey, reason string) input.CIDRRecord {
	return input.CIDRRecord{
		IsRich:            true,
		IsValid:           true,
		CIDR:              segment,
		DstNetworkSegment: segment,
		DstIP:             dstIP,
		ExecutionKey:      execKey,
		Port:              80,
		Protocol:          "tcp",
		Reason:            reason,
		FabName:           "fab",
		CIDRName:          "seg-" + segment,
		ServiceLabel:      "web",
		Decision:          "accept",
		PolicyID:          "P1",
		RowNumber:         row,
	}
}

// richMatrixRecords assembles the design.md §3.4 rich-shape matrix:
//   - one large PRECHECK_ALLOW_ALL segment (10.10.0.0/24),
//   - several small MATCH_POLICY_ACCEPT segments each a single dst_ip,
//   - one segment (10.30.0.0/29) assembled from many rows, some sharing an
//     execution key (exercises de-dup + metadata merge).
func richMatrixRecords() []input.CIDRRecord {
	recs := []input.CIDRRecord{
		richRecord(1, "10.10.0.0/24", "10.10.0.0", "seed:80/tcp", reasonPrecheckAllowAll),
	}
	for i := 0; i < 5; i++ {
		seg := fmt.Sprintf("10.20.%d.0/32", i)
		ip := fmt.Sprintf("10.20.%d.0", i)
		recs = append(recs, richRecord(10+i, seg, ip, fmt.Sprintf("%s:80/tcp", ip), reasonMatchPolicyAccept))
	}
	// One segment from many rows: three distinct dst_ips plus a duplicate of the
	// first row's key carrying extra metadata to merge.
	seg := "10.30.0.0/29"
	recs = append(recs,
		richRecord(20, seg, "10.30.0.1", "10.30.0.1:80/tcp", reasonMatchPolicyAccept),
		richRecord(21, seg, "10.30.0.2", "10.30.0.2:80/tcp", reasonMatchPolicyAccept),
		richRecord(22, seg, "10.30.0.3", "10.30.0.3:80/tcp", reasonMatchPolicyAccept),
	)
	dup := richRecord(23, seg, "10.30.0.1", "10.30.0.1:80/tcp", reasonMatchPolicyAccept)
	dup.ServiceLabel = "web-alt"
	dup.PolicyID = "P2"
	recs = append(recs, dup)
	return recs
}

// TestBuildRuntime_DifferentialGolden_AllIncomplete is the safety net for the
// Phase 2 refactor: for the rich matrix and a basic fixture with EVERY chunk
// incomplete, the refactored buildRuntimeWithPredicate must produce runtimes
// identical to the pre-refactor whole-input build.
func TestBuildRuntime_DifferentialGolden_AllIncomplete(t *testing.T) {
	cases := []struct {
		name      string
		records   []input.CIDRRecord
		ports     []input.PortSpec
		reachable func(string) bool
	}{
		{
			name:    "rich_matrix_all_reachable",
			records: richMatrixRecords(),
		},
		{
			name:    "rich_matrix_mixed_reachable",
			records: richMatrixRecords(),
			// Drop a handful of interior IPs of the big PRECHECK segment.
			reachable: reachablePredicate([]uint32{
				ipv4ToUint32("10.10.0.5"),
				ipv4ToUint32("10.10.0.9"),
				ipv4ToUint32("10.10.0.200"),
			}),
		},
		{
			name: "basic_fixture",
			records: []input.CIDRRecord{
				benchBasicRecord(0),
				benchBasicRecord(1),
				benchBasicRecord(2),
			},
			ports: []input.PortSpec{
				{Number: 80, Proto: "tcp", Raw: "80/tcp"},
				{Number: 443, Proto: "tcp", Raw: "443/tcp"},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			baseChunks, err := buildFreshChunksForTest(tc.records, tc.ports, tc.reachable)
			if err != nil {
				t.Fatalf("build chunks: %v", err)
			}
			if len(baseChunks) == 0 {
				t.Fatalf("expected chunks, got none")
			}
			// Every chunk incomplete: reset progress cursors.
			for i := range baseChunks {
				baseChunks[i].NextIndex = 0
				baseChunks[i].ScannedCount = 0
				baseChunks[i].Status = "pending"
			}

			want := referenceBuildRuntime(t, cloneChunks(baseChunks), tc.records, tc.ports, tc.reachable)
			got, err := buildRuntimeWithPredicate(cloneChunks(baseChunks), tc.records, tc.ports, runtimePolicy{bucketRate: 1, bucketCapacity: 1}, tc.reachable, nil)
			if err != nil {
				t.Fatalf("buildRuntimeWithPredicate: %v", err)
			}
			assertRuntimesEquivalent(t, got, want)
			for _, rt := range got {
				if rt.bkt != nil {
					rt.bkt.Close()
				}
			}
		})
	}
}

// TestBuildRuntime_CompletedChunk_NotExpanded asserts a completed chunk is not
// expanded (its CIDR is ABSENT from the CSV, yet no error), carries nil targets
// and nil bucket, is preserved verbatim by collectChunkStates, and that only the
// incomplete chunk carries targets + a bucket.
func TestBuildRuntime_CompletedChunk_NotExpanded(t *testing.T) {
	// Only the incomplete segment's records are present in the CSV; the completed
	// segment (10.99.0.0/30) is intentionally absent.
	records := []input.CIDRRecord{
		richRecord(1, "10.1.0.0/30", "10.1.0.0", "10.1.0.0:80/tcp", reasonPrecheckAllowAll),
	}
	// Incomplete chunk: /30 PRECHECK expands to .0,.1,.2 (.3 broadcast excluded) = 3.
	chunks := []task.Chunk{
		{CIDR: "10.99.0.0/30", CIDRName: "done", Ports: []string{"80/tcp"}, NextIndex: 4, ScannedCount: 4, TotalCount: 4, Status: "completed"},
		{CIDR: "10.1.0.0/30", CIDRName: "seg", Ports: []string{"80/tcp"}, NextIndex: 0, ScannedCount: 0, TotalCount: 3, Status: "pending"},
	}

	runtimes, err := buildRuntimeWithPredicate(chunks, records, nil, runtimePolicy{bucketRate: 1, bucketCapacity: 1}, nil, nil)
	if err != nil {
		t.Fatalf("resume with a completed chunk absent from the CSV must not error: %v", err)
	}
	if len(runtimes) != 2 {
		t.Fatalf("expected 2 runtimes (order preserved), got %d", len(runtimes))
	}

	completed := runtimes[0]
	if completed.ipCidr != "" && completed.state.CIDR != "10.99.0.0/30" {
		t.Fatalf("completed runtime lost its chunk order/identity: %+v", completed.state)
	}
	if completed.state.CIDR != "10.99.0.0/30" {
		t.Fatalf("runtime[0] should be the completed chunk, got %s", completed.state.CIDR)
	}
	if completed.targets != nil {
		t.Fatalf("completed chunk must not be expanded, got %d targets", len(completed.targets))
	}
	if completed.bkt != nil {
		t.Fatalf("completed chunk must not allocate a leaky bucket")
	}

	incomplete := runtimes[1]
	if incomplete.state.CIDR != "10.1.0.0/30" {
		t.Fatalf("runtime[1] should be the incomplete chunk, got %s", incomplete.state.CIDR)
	}
	if len(incomplete.targets) != 3 {
		t.Fatalf("incomplete chunk should expand to 3 targets, got %d", len(incomplete.targets))
	}
	if incomplete.bkt == nil {
		t.Fatalf("incomplete chunk should allocate a leaky bucket")
	}
	incomplete.bkt.Close()

	// collectChunkStates must preserve the completed chunk verbatim (progress
	// cursors, ports, total) so persistResumeSnapshot re-saves the finished work.
	states := collectChunkStates(runtimes)
	if states[0].CIDR != "10.99.0.0/30" || states[0].NextIndex != 4 || states[0].ScannedCount != 4 || states[0].TotalCount != 4 {
		t.Fatalf("completed chunk not preserved verbatim: %+v", states[0])
	}
	if states[0].Status != "completed" {
		t.Fatalf("completed chunk status: got %q, want completed", states[0].Status)
	}
	if len(states[0].Ports) != 1 || states[0].Ports[0] != "80/tcp" {
		t.Fatalf("completed chunk ports mutated: %+v", states[0].Ports)
	}
}

// TestBuildRuntime_CompletedChunk_AbsentCIDR_ResumesWithoutError locks the
// intentional Phase 2 behavior change: pre-Phase-2, a completed chunk whose CIDR
// was removed from the CSV errored ("no scannable targets"); now it resumes
// cleanly because completed chunks are never looked up.
func TestBuildRuntime_CompletedChunk_AbsentCIDR_ResumesWithoutError(t *testing.T) {
	records := []input.CIDRRecord{
		richRecord(1, "10.1.0.0/30", "10.1.0.0", "10.1.0.0:80/tcp", reasonPrecheckAllowAll),
	}
	chunks := []task.Chunk{
		{CIDR: "10.42.0.0/24", CIDRName: "gone", Ports: []string{"80/tcp"}, NextIndex: 254, ScannedCount: 254, TotalCount: 254, Status: "completed"},
		{CIDR: "10.1.0.0/30", CIDRName: "seg", Ports: []string{"80/tcp"}, NextIndex: 0, ScannedCount: 0, TotalCount: 3, Status: "pending"},
	}
	runtimes, err := buildRuntimeWithPredicate(chunks, records, nil, runtimePolicy{bucketRate: 1, bucketCapacity: 1}, nil, nil)
	if err != nil {
		t.Fatalf("completed chunk absent from CSV must resume without error, got: %v", err)
	}
	for _, rt := range runtimes {
		if rt.bkt != nil {
			rt.bkt.Close()
		}
	}
}

// TestBuildRuntime_StatusCompletedButUnfinished_IsDispatchSafe guards a malformed
// snapshot where Status=="completed" but NextIndex<TotalCount: the chunk still has
// work, so it must take the incomplete path (real targets + leaky bucket). If it
// were treated as completed (lightweight, nil bucket), dispatch — which skips only
// on NextIndex>=TotalCount (task_dispatcher.go) — would reach rt.bkt.Acquire on a
// nil bucket and panic.
func TestBuildRuntime_StatusCompletedButUnfinished_IsDispatchSafe(t *testing.T) {
	records := []input.CIDRRecord{
		richRecord(1, "10.1.0.0/30", "10.1.0.0", "10.1.0.0:80/tcp", reasonPrecheckAllowAll),
	}
	// /30 PRECHECK -> 3 targets. Status lies "completed" while NextIndex(0) < TotalCount(3).
	chunks := []task.Chunk{
		{CIDR: "10.1.0.0/30", Ports: []string{"80/tcp"}, NextIndex: 0, ScannedCount: 0, TotalCount: 3, Status: "completed"},
	}
	runtimes, err := buildRuntimeWithPredicate(chunks, records, nil, runtimePolicy{bucketRate: 1, bucketCapacity: 1}, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(runtimes) != 1 {
		t.Fatalf("expected 1 runtime, got %d", len(runtimes))
	}
	rt := runtimes[0]
	if rt.bkt == nil {
		t.Fatalf("chunk with remaining work must get a leaky bucket, else dispatch nil-derefs on rt.bkt.Acquire")
	}
	if len(rt.targets) != 3 {
		t.Fatalf("chunk with remaining work must be expanded, got %d targets", len(rt.targets))
	}
	rt.bkt.Close()
}

func TestBuildRuntime_RemainingWorkWithinInitialCapacity_DispatchesWithoutBucket(t *testing.T) {
	records := []input.CIDRRecord{
		richRecord(1, "10.1.0.0/30", "10.1.0.0", "10.1.0.0:80/tcp", reasonPrecheckAllowAll),
	}
	chunks := []task.Chunk{{
		CIDR: "10.1.0.0/30", Ports: []string{"80/tcp"}, NextIndex: 2, ScannedCount: 2, TotalCount: 3, Status: "scanning",
	}}
	runtimes, err := buildRuntimeWithPredicate(chunks, records, nil, runtimePolicy{bucketRate: 1, bucketCapacity: 1}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(runtimes) != 1 {
		t.Fatalf("runtime count = %d, want 1", len(runtimes))
	}
	if runtimes[0].bkt != nil {
		runtimes[0].bkt.Close()
		t.Fatal("one remaining task allocated a rate limiter even though the initial capacity already covers it")
	}

	taskCh := make(chan scanTask, 1)
	err = dispatchTasks(context.Background(), dispatchPolicy{}, speedctrl.NewController(), newLogger("error", true, nil), runtimes, taskCh)
	if err != nil {
		t.Fatalf("dispatch one task without a rate limiter: %v", err)
	}
	if got := <-taskCh; got.taskIdx != 2 || got.ip != "10.1.0.2" || got.port != 80 {
		t.Fatalf("dispatched task = %+v", got)
	}
}

// TestBuildRuntime_LegacyZeroTotalCount_PreservesOwnership guards the byte-for-byte
// contract for legacy snapshots that omit total_count. Filtering out a completed
// segment's rows would hand a cross-segment execution key back to an incomplete
// segment; with total_count==0 the invariant check can't catch that, so the filter
// must be disabled and whole-input ownership preserved. Same ownership shape as
// TestBuildRuntime_DivergenceGuard_TotalCountMismatch, but B has no saved total.
func TestBuildRuntime_LegacyZeroTotalCount_PreservesOwnership(t *testing.T) {
	recA := richRecord(1, "10.0.0.0/30", "10.0.0.1", "SHARED:80/tcp", reasonMatchPolicyAccept)
	recB1 := richRecord(2, "10.1.0.0/30", "10.1.0.0", "10.1.0.0:80/tcp", reasonPrecheckAllowAll)
	recBShared := richRecord(3, "10.1.0.0/30", "10.1.0.1", "SHARED:80/tcp", reasonMatchPolicyAccept)
	records := []input.CIDRRecord{recA, recB1, recBShared}

	chunks := []task.Chunk{
		{CIDR: "10.0.0.0/30", Ports: []string{"80/tcp"}, NextIndex: 1, ScannedCount: 1, TotalCount: 1, Status: "completed"},
		// Legacy: incomplete B has NO saved total_count (decodes to 0).
		{CIDR: "10.1.0.0/30", Ports: []string{"80/tcp"}, NextIndex: 0, ScannedCount: 0, TotalCount: 0, Status: "pending"},
	}
	runtimes, err := buildRuntimeWithPredicate(chunks, records, nil, runtimePolicy{bucketRate: 1, bucketCapacity: 1}, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var b *chunkRuntime
	for _, rt := range runtimes {
		if rt.state.CIDR == "10.1.0.0/30" {
			b = rt
		}
		if rt.bkt != nil {
			rt.bkt.Close()
		}
	}
	if b == nil {
		t.Fatalf("incomplete chunk B missing from runtimes")
	}
	// Whole-input ownership: A owns SHARED, so B keeps its 3 PRECHECK targets, not 4.
	if len(b.targets) != 3 {
		t.Fatalf("legacy zero-total resume diverged: B has %d targets, want 3 (whole-input ownership)", len(b.targets))
	}
	if b.state.TotalCount != 3 {
		t.Fatalf("B TotalCount: got %d, want 3", b.state.TotalCount)
	}
}

// TestBuildRuntime_MixedTotalCountPresence_FallsBackWholeInput covers the case
// Codex flagged as untested: a resume where some incomplete chunks have a saved
// total_count and one does not. allIncompleteHaveTotal is then false, so filtering
// is disabled and the whole-input rebuild runs — the total-bearing chunk is still
// validated, and the legacy chunk gets its total assigned, both correctly.
func TestBuildRuntime_MixedTotalCountPresence_FallsBackWholeInput(t *testing.T) {
	records := []input.CIDRRecord{
		richRecord(1, "10.0.0.0/30", "10.0.0.0", "10.0.0.0:80/tcp", reasonPrecheckAllowAll), // completed
		richRecord(2, "10.1.0.0/30", "10.1.0.0", "10.1.0.0:80/tcp", reasonPrecheckAllowAll), // incomplete, total present
		richRecord(3, "10.2.0.0/30", "10.2.0.0", "10.2.0.0:80/tcp", reasonPrecheckAllowAll), // incomplete, legacy total==0
	}
	chunks := []task.Chunk{
		{CIDR: "10.0.0.0/30", Ports: []string{"80/tcp"}, NextIndex: 3, ScannedCount: 3, TotalCount: 3, Status: "completed"},
		{CIDR: "10.1.0.0/30", Ports: []string{"80/tcp"}, NextIndex: 0, ScannedCount: 0, TotalCount: 3, Status: "pending"},
		{CIDR: "10.2.0.0/30", Ports: []string{"80/tcp"}, NextIndex: 0, ScannedCount: 0, TotalCount: 0, Status: "pending"},
	}
	runtimes, err := buildRuntimeWithPredicate(chunks, records, nil, runtimePolicy{bucketRate: 1, bucketCapacity: 1}, nil, nil)
	if err != nil {
		t.Fatalf("mixed total_count presence must not error: %v", err)
	}
	if len(runtimes) != 3 {
		t.Fatalf("expected 3 runtimes, got %d", len(runtimes))
	}
	for _, rt := range runtimes {
		if rt.bkt != nil {
			rt.bkt.Close()
		}
	}
	if runtimes[0].targets != nil || runtimes[0].bkt != nil {
		t.Fatalf("completed chunk must stay lightweight")
	}
	if len(runtimes[1].targets) != 3 || runtimes[1].state.TotalCount != 3 {
		t.Fatalf("total-present incomplete chunk: %d targets, total %d, want 3/3", len(runtimes[1].targets), runtimes[1].state.TotalCount)
	}
	if len(runtimes[2].targets) != 3 || runtimes[2].state.TotalCount != 3 {
		t.Fatalf("legacy incomplete chunk: %d targets, total %d, want 3/3 (total assigned)", len(runtimes[2].targets), runtimes[2].state.TotalCount)
	}
}

// TestBuildRuntime_DivergenceGuard_TotalCountMismatch asserts the retained
// total_count invariant fires loudly when a filtered-out (completed) segment had
// first-claimed an execution key that an incomplete segment also produces. In the
// original whole-input build the completed segment stole the key, so the
// incomplete segment's saved total_count is short by one; rebuilding it alone
// gives it the key back, so the rebuilt group is larger than saved -> error.
func TestBuildRuntime_DivergenceGuard_TotalCountMismatch(t *testing.T) {
	// Completed segment A first-claims the shared key "SHARED:80/tcp".
	recA := richRecord(1, "10.0.0.0/30", "10.0.0.1", "SHARED:80/tcp", reasonMatchPolicyAccept)
	// Incomplete segment B: three of its own IPs (PRECHECK) plus a MATCH row
	// producing the SAME shared key.
	recB1 := richRecord(2, "10.1.0.0/30", "10.1.0.0", "10.1.0.0:80/tcp", reasonPrecheckAllowAll)
	recBShared := richRecord(3, "10.1.0.0/30", "10.1.0.1", "SHARED:80/tcp", reasonMatchPolicyAccept)
	records := []input.CIDRRecord{recA, recB1, recBShared}

	chunks := []task.Chunk{
		{CIDR: "10.0.0.0/30", Ports: []string{"80/tcp"}, NextIndex: 1, ScannedCount: 1, TotalCount: 1, Status: "completed"},
		// B saved total_count=3 (whole-input build: A stole SHARED, B kept 3).
		{CIDR: "10.1.0.0/30", Ports: []string{"80/tcp"}, NextIndex: 0, ScannedCount: 0, TotalCount: 3, Status: "pending"},
	}

	_, err := buildRuntimeWithPredicate(chunks, records, nil, runtimePolicy{bucketRate: 1, bucketCapacity: 1}, nil, nil)
	if err == nil {
		t.Fatalf("expected the total_count invariant to fire on the diverged incomplete segment")
	}
	if !strings.Contains(err.Error(), "incompatible") {
		t.Fatalf("expected an 'incompatible' resume-state error, got: %v", err)
	}
}

package scanapp

import (
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/input"
)

// These tests lock the primary correctness invariant of the split
// pre-ping/generate-buckets/scan feature (plan T1, design §5.3, risk R1):
//
//	the TotalCount stamped onto a chunk by the bucket-generation path is exactly
//	the value scan's runtime
//	re-derivation (buildRuntimeWithPredicate) expects
//	(group target count * len(ports)).
//
// If the two ever diverge, buildRuntimeWithPredicate returns the mismatch
// error at chunk_lifecycle.go:151 and every scan of a generated bucket file
// fails. Today the invariant holds by construction because both paths count
// over the same grouping helpers (buildRichGroupsWithPredicate /
// buildCIDRGroupsWithPredicate) under the same reachable predicate, so these
// tests pass immediately; they are a regression guard before T4 depends on it.
//
// Both scenarios deliberately combine a blocklist predicate (removing some
// reachable targets) with the boundary-broadcast exclusion (commit d03296d),
// so the counts must account for excluded addresses identically on both sides.

// assertBuildRuntimeParity builds fresh chunks over records+predicate+ports and
// feeds the very same records+predicate into the runtime re-derivation, then
// asserts no mismatch error and that each runtime chunk's TotalCount equals the
// TotalCount the fresh build stamped (and matches the caller's expectation).
func assertBuildRuntimeParity(
	t *testing.T,
	records []input.CIDRRecord,
	ports []input.PortSpec,
	reachable func(string) bool,
	wantTotalByCIDR map[string]int,
) {
	t.Helper()

	chunks, err := buildFreshChunksForTest(records, ports, reachable)
	if err != nil {
		t.Fatalf("fresh chunk build failed: %v", err)
	}
	if len(chunks) != len(wantTotalByCIDR) {
		t.Fatalf("expected %d chunks, got %d: %+v", len(wantTotalByCIDR), len(chunks), chunks)
	}

	// Capture what the fresh build stamped before the runtime path mutates the
	// chunks slice in place, and confirm it matches the hand-computed totals so
	// the parity check is not tautological.
	builtByCIDR := make(map[string]int, len(chunks))
	for i := range chunks {
		ch := chunks[i]
		builtByCIDR[ch.CIDR] = ch.TotalCount
		want, ok := wantTotalByCIDR[ch.CIDR]
		if !ok {
			t.Fatalf("fresh build produced unexpected CIDR %q", ch.CIDR)
		}
		if ch.TotalCount != want {
			t.Fatalf("fresh build TotalCount for %s = %d, want %d", ch.CIDR, ch.TotalCount, want)
		}
		if ch.TotalCount == 0 {
			t.Fatalf("fresh build TotalCount for %s must be non-zero to exercise the guard", ch.CIDR)
		}
	}

	runtimes, err := buildRuntimeWithPredicate(chunks, records, ports, runtimePolicy{bucketRate: 1, bucketCapacity: 1}, reachable, nil)
	if err != nil {
		t.Fatalf("runtime re-derivation rejected freshly built chunks (invariant broken): %v", err)
	}
	if len(runtimes) != len(chunks) {
		t.Fatalf("expected %d runtimes, got %d", len(chunks), len(runtimes))
	}

	for _, rt := range runtimes {
		built, ok := builtByCIDR[rt.ipCidr]
		if !ok {
			t.Fatalf("runtime references CIDR %q absent from fresh build", rt.ipCidr)
		}
		expected := rt.targetCount() * len(rt.ports)
		if rt.state.TotalCount != built {
			t.Fatalf("runtime TotalCount for %s = %d, fresh build = %d", rt.ipCidr, rt.state.TotalCount, built)
		}
		if rt.state.TotalCount != expected {
			t.Fatalf("runtime TotalCount for %s = %d, but targetCount*len(ports) = %d", rt.ipCidr, rt.state.TotalCount, expected)
		}
	}
}

func TestChunkBuild_TotalCountMatchesRuntimeExpected_Rich(t *testing.T) {
	// Two rich PRECHECK_ALLOW_ALL segments, each a /30: the segment expands to
	// .0/.1/.2/.3, the boundary broadcast (.3) is excluded, then the blocklist
	// removes one interior IP per segment. Rich chunks carry one port per target,
	// so TotalCount == reachable target count.
	records := []input.CIDRRecord{
		{
			IsRich:            true,
			IsValid:           true,
			ExecutionKey:      "10.0.0.0:443/tcp",
			DstIP:             "10.0.0.0",
			DstNetworkSegment: "10.0.0.0/30",
			Port:              443,
			Reason:            reasonPrecheckAllowAll,
			FabName:           "fab-a",
			CIDRName:          "seg-a",
			ServiceLabel:      "https",
			Decision:          "accept",
			PolicyID:          "P-a",
		},
		{
			IsRich:            true,
			IsValid:           true,
			ExecutionKey:      "10.1.0.0:443/tcp",
			DstIP:             "10.1.0.0",
			DstNetworkSegment: "10.1.0.0/30",
			Port:              443,
			Reason:            reasonPrecheckAllowAll,
			FabName:           "fab-b",
			CIDRName:          "seg-b",
			ServiceLabel:      "https",
			Decision:          "accept",
			PolicyID:          "P-b",
		},
	}

	// Remove 10.0.0.1 from segment A and 10.1.0.2 from segment B.
	reachable := reachablePredicate([]uint32{
		ipv4ToUint32("10.0.0.1"),
		ipv4ToUint32("10.1.0.2"),
	})

	// Segment A: {.0,.1,.2} minus .1 => {.0,.2} => 2 targets * 1 port = 2.
	// Segment B: {.0,.1,.2} minus .2 => {.0,.1} => 2 targets * 1 port = 2.
	assertBuildRuntimeParity(t, records, nil, reachable, map[string]int{
		"10.0.0.0/30": 2,
		"10.1.0.0/30": 2,
	})
}

func TestChunkBuild_TotalCountMatchesRuntimeExpected_Basic(t *testing.T) {
	// Basic mode with multiple ports. Each record's selector is a full /30 (so it
	// expands to .0/.1/.2/.3); Net is the /30 so the boundary broadcast (.3) is
	// excluded; the blocklist removes one interior IP per segment. TotalCount ==
	// reachable target count * number of ports.
	netA := mustSelectorNet(t, "10.0.0.0/30")
	netB := mustSelectorNet(t, "10.2.0.0/30")
	records := []input.CIDRRecord{
		{CIDR: "10.0.0.0/30", IPRaw: "10.0.0.0/30", Net: netA, FabName: "fab-a", CIDRName: "seg-a"},
		{CIDR: "10.2.0.0/30", IPRaw: "10.2.0.0/30", Net: netB, FabName: "fab-b", CIDRName: "seg-b"},
	}

	ports := []input.PortSpec{
		{Number: 80, Proto: "tcp", Raw: "80/tcp"},
		{Number: 443, Proto: "tcp", Raw: "443/tcp"},
	}

	// Remove 10.0.0.1 from segment A and 10.2.0.2 from segment B.
	reachable := reachablePredicate([]uint32{
		ipv4ToUint32("10.0.0.1"),
		ipv4ToUint32("10.2.0.2"),
	})

	// Segment A: {.0,.1,.2} minus .1 => {.0,.2} => 2 targets * 2 ports = 4.
	// Segment B: {.0,.1,.2} minus .2 => {.0,.1} => 2 targets * 2 ports = 4.
	assertBuildRuntimeParity(t, records, ports, reachable, map[string]int{
		"10.0.0.0/30": 4,
		"10.2.0.0/30": 4,
	})
}

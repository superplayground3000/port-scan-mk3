package scanapp

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
	"github.com/xuxiping/port-scan-mk3/pkg/input"
)

// TestCollectUnreachableRows_RichRowsAggregateToSingleRowWithDistinctMergedMetadata
// exercises the live rich aggregation path of collectUnreachableRows (used by
// RunPrePing): two rich records for the same dst IP but different ports/metadata
// collapse to one unreachable row whose metadata columns are pipe-merged. This
// covers richUnreachableRowKey + mergeUnreachableRecord, which only the rich
// branch reaches.
func TestCollectUnreachableRows_RichRowsAggregateToSingleRowWithDistinctMergedMetadata(t *testing.T) {
	inputs := runInputs{
		cidrRecords: []input.CIDRRecord{
			{
				IsRich:            true,
				IsValid:           true,
				ExecutionKey:      "10.0.0.9:443/tcp",
				DstIP:             "10.0.0.9",
				DstNetworkSegment: "10.0.0.0/24",
				Port:              443,
				FabName:           "fab-a",
				CIDRName:          "seg-a",
				ServiceLabel:      "svc-a",
				Decision:          "accept",
				PolicyID:          "P-1",
				Reason:            "MATCH_POLICY_ACCEPT",
				SrcIP:             "192.168.1.10",
				SrcNetworkSegment: "192.168.1.0/24",
			},
			{
				IsRich:            true,
				IsValid:           true,
				ExecutionKey:      "10.0.0.9:8443/tcp",
				DstIP:             "10.0.0.9",
				DstNetworkSegment: "10.0.0.0/24",
				Port:              8443,
				FabName:           "fab-b",
				CIDRName:          "seg-b",
				ServiceLabel:      "svc-b",
				Decision:          "deny",
				PolicyID:          "P-2",
				Reason:            "MATCH_POLICY_ACCEPT",
				SrcIP:             "192.168.1.11",
				SrcNetworkSegment: "192.168.2.0/24",
			},
		},
	}

	// Marking 10.0.0.9 unreachable makes the predicate return false for it, so
	// collectUnreachableRows emits the (merged) row for that target.
	rows, err := collectUnreachableRows(
		inputs,
		reachablePredicate([]uint32{ipv4ToUint32("10.0.0.9")}),
		"ping failed within 100ms",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(rows) != 1 {
		t.Fatalf("expected single aggregated rich unreachable row, got %+v", rows)
	}
	got := rows[0]
	if got.IP != "10.0.0.9" || got.IPCidr != "10.0.0.0/24" {
		t.Fatalf("unexpected aggregated row identity: %+v", got)
	}
	if got.Status != "unreachable" || got.Reason != "ping failed within 100ms" {
		t.Fatalf("unexpected aggregated row status/reason: %+v", got)
	}
	if got.FabName != "fab-a|fab-b" {
		t.Fatalf("unexpected merged fab_name: %s", got.FabName)
	}
	if got.CIDRName != "seg-a|seg-b" {
		t.Fatalf("unexpected merged cidr_name: %s", got.CIDRName)
	}
	if got.ServiceLabel != "svc-a|svc-b" {
		t.Fatalf("unexpected merged service_label: %s", got.ServiceLabel)
	}
	if got.Decision != "accept|deny" {
		t.Fatalf("unexpected merged decision: %s", got.Decision)
	}
	if got.PolicyID != "P-1|P-2" {
		t.Fatalf("unexpected merged policy_id: %s", got.PolicyID)
	}
	if got.ExecutionKey != "10.0.0.9:443/tcp|10.0.0.9:8443/tcp" {
		t.Fatalf("unexpected merged execution_key: %s", got.ExecutionKey)
	}
	if got.SrcIP != "192.168.1.10|192.168.1.11" {
		t.Fatalf("unexpected merged src_ip: %s", got.SrcIP)
	}
	if got.SrcNetworkSegment != "192.168.1.0/24|192.168.2.0/24" {
		t.Fatalf("unexpected merged src_network_segment: %s", got.SrcNetworkSegment)
	}
}

// TestRunReachabilityChecksWithProgress_FailsFastOnFatalCheckerError drives the
// live worker-pool entry (the one RunPrePing uses) and asserts a tool-level
// CheckDetailed error is fatal and stops the pool after the first IP.
func TestRunReachabilityChecksWithProgress_FailsFastOnFatalCheckerError(t *testing.T) {
	checker := &fakePreScanChecker{
		detailedErrs: map[string]error{
			"10.0.0.1": errors.New("ping binary missing"),
		},
	}

	_, err := runReachabilityChecksWithProgress(context.Background(), checker, []string{"10.0.0.1", "10.0.0.2"}, 1, 100*time.Millisecond, nil)
	if err == nil {
		t.Fatal("expected fatal checker error")
	}
	if calls := checker.calls(); len(calls) != 1 || calls[0] != "10.0.0.1" {
		t.Fatalf("expected fail-fast to stop after first ip, got %v", calls)
	}
}

func TestReachablePredicate_WhenIPIsMarkedUnreachable_FiltersItOut(t *testing.T) {
	predicate := reachablePredicate([]uint32{
		ipv4ToUint32("10.0.0.2"),
		ipv4ToUint32("10.0.0.5"),
	})

	if predicate("10.0.0.2") {
		t.Fatal("expected unreachable ip to be filtered")
	}
	if !predicate("10.0.0.3") {
		t.Fatal("expected other ips to remain reachable")
	}
}

func TestBuildCIDRGroupsWithPredicate_SkipsUnreachableTargets(t *testing.T) {
	rows := []input.CIDRRecord{
		{CIDR: "10.0.0.0/24", Selector: mustSelectorNet(t, "10.0.0.1/32"), FabName: "fab", CIDRName: "name"},
		{CIDR: "10.0.0.0/24", Selector: mustSelectorNet(t, "10.0.0.2/32"), FabName: "fab", CIDRName: "name"},
	}

	groups, err := buildCIDRGroupsWithPredicate(rows, reachablePredicate([]uint32{ipv4ToUint32("10.0.0.2")}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := groups["10.0.0.0/24"].targets
	if len(got) != 1 || got[0].ip != "10.0.0.1" {
		t.Fatalf("expected unreachable target to be skipped, got %+v", got)
	}
}

func TestBuildRichGroupsWithPredicate_SkipsUnreachableTargetsAndPreservesDistinctMetadata(t *testing.T) {
	rows := []input.CIDRRecord{
		{
			IsRich:            true,
			IsValid:           true,
			ExecutionKey:      "10.0.0.1:443/tcp",
			DstIP:             "10.0.0.1",
			DstNetworkSegment: "10.0.0.0/24",
			Port:              443,
			PolicyID:          "P-1",
			Decision:          "accept",
			Reason:            "MATCH_POLICY_ACCEPT",
			SrcIP:             "192.168.1.10",
		},
		{
			IsRich:            true,
			IsValid:           true,
			ExecutionKey:      "10.0.0.1:443/tcp",
			DstIP:             "10.0.0.1",
			DstNetworkSegment: "10.0.0.0/24",
			Port:              443,
			PolicyID:          "P-2",
			Decision:          "deny",
			Reason:            "MATCH_POLICY_ACCEPT",
			SrcIP:             "192.168.1.11",
		},
		{
			IsRich:            true,
			IsValid:           true,
			ExecutionKey:      "10.0.0.2:443/tcp",
			DstIP:             "10.0.0.2",
			DstNetworkSegment: "10.0.0.0/24",
			Port:              443,
			PolicyID:          "P-drop",
			Decision:          "deny",
			Reason:            "MATCH_POLICY_ACCEPT",
			SrcIP:             "192.168.1.12",
		},
	}

	groups, err := buildRichGroupsWithPredicate(rows, reachablePredicate([]uint32{ipv4ToUint32("10.0.0.2")}))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	got := groups["10.0.0.0/24"].targets
	if len(got) != 1 {
		t.Fatalf("expected only reachable rich target to remain, got %+v", got)
	}
	if got[0].ip != "10.0.0.1" {
		t.Fatalf("unexpected rich target ip: %+v", got[0])
	}
	if got[0].meta.policyID != "P-1|P-2" {
		t.Fatalf("unexpected merged policy id: %s", got[0].meta.policyID)
	}
	if got[0].meta.decision != "accept|deny" {
		t.Fatalf("unexpected merged decision: %s", got[0].meta.decision)
	}
}

func TestBuildRichGroupsWithPredicate_WhenAllValidTargetsFiltered_ReturnsEmptyGroupsWithoutError(t *testing.T) {
	rows := []input.CIDRRecord{
		{
			IsRich:            true,
			IsValid:           true,
			ExecutionKey:      "10.0.0.9:443/tcp",
			DstIP:             "10.0.0.9",
			DstNetworkSegment: "10.0.0.0/24",
			Port:              443,
			Reason:            "MATCH_POLICY_ACCEPT",
		},
	}

	groups, err := buildRichGroupsWithPredicate(rows, reachablePredicate([]uint32{ipv4ToUint32("10.0.0.9")}))
	if err != nil {
		t.Fatalf("expected all-filtered rich groups to return empty result, got err=%v", err)
	}
	if len(groups) != 0 {
		t.Fatalf("expected empty groups after filtering all rich targets, got %+v", groups)
	}
}

func TestBuildRichChunksWithPredicate_WhenAllValidTargetsFiltered_ReturnsEmptyChunksWithoutError(t *testing.T) {
	rows := []input.CIDRRecord{
		{
			IsRich:            true,
			IsValid:           true,
			ExecutionKey:      "10.0.0.9:443/tcp",
			DstIP:             "10.0.0.9",
			DstNetworkSegment: "10.0.0.0/24",
			Port:              443,
			Reason:            "MATCH_POLICY_ACCEPT",
		},
	}

	chunks, err := buildRichChunksWithPredicate(rows, reachablePredicate([]uint32{ipv4ToUint32("10.0.0.9")}))
	if err != nil {
		t.Fatalf("expected all-filtered rich chunks to return empty result, got err=%v", err)
	}
	if len(chunks) != 0 {
		t.Fatalf("expected empty chunks after filtering all rich targets, got %+v", chunks)
	}
}

func TestLoadOrBuildChunksWithPredicate_SkipsUnreachableTargetsFromChunkTotals(t *testing.T) {
	rows := []input.CIDRRecord{
		{CIDR: "10.0.0.0/24", Selector: mustSelectorNet(t, "10.0.0.1/32"), CIDRName: "web"},
		{CIDR: "10.0.0.0/24", Selector: mustSelectorNet(t, "10.0.0.2/32"), CIDRName: "web"},
	}

	chunks, err := loadOrBuildChunksWithPredicate(config.Config{}, rows, []input.PortSpec{
		{Number: 80, Proto: "tcp", Raw: "80/tcp"},
		{Number: 443, Proto: "tcp", Raw: "443/tcp"},
	}, reachablePredicate([]uint32{ipv4ToUint32("10.0.0.2")}))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	if len(chunks) != 1 {
		t.Fatalf("expected one chunk, got %+v", chunks)
	}
	if chunks[0].TotalCount != 2 {
		t.Fatalf("expected only reachable target to contribute to total count, got %+v", chunks[0])
	}
}

type fakePreScanChecker struct {
	mu           sync.Mutex
	called       []string
	timeouts     map[string]time.Duration
	results      map[string]ReachabilityResult
	detailedErrs map[string]error
}

func (f *fakePreScanChecker) Check(_ context.Context, ip string, timeout time.Duration) ReachabilityResult {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.recordLocked(ip, timeout)
}

func (f *fakePreScanChecker) CheckDetailed(_ context.Context, ip string, timeout time.Duration) (ReachabilityResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	result := f.recordLocked(ip, timeout)
	if err := f.detailedErrs[ip]; err != nil {
		result.FailureText = err.Error()
		return result, err
	}
	return result, nil
}

func (f *fakePreScanChecker) calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := append([]string(nil), f.called...)
	sort.Strings(out)
	return out
}

func (f *fakePreScanChecker) recordLocked(ip string, timeout time.Duration) ReachabilityResult {
	f.called = append(f.called, ip)
	if f.timeouts == nil {
		f.timeouts = make(map[string]time.Duration)
	}
	f.timeouts[ip] = timeout
	if f.results == nil {
		return ReachabilityResult{IP: ip, Reachable: true}
	}
	if result, ok := f.results[ip]; ok {
		if result.IP == "" {
			result.IP = ip
		}
		return result
	}
	return ReachabilityResult{IP: ip, Reachable: true}
}

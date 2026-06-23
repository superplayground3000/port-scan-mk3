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
	"github.com/xuxiping/port-scan-mk3/pkg/state"
)

func TestPreScanPing_Run_DedupesCheckerCallsAcrossDuplicateIPs(t *testing.T) {
	checker := &fakePreScanChecker{
		results: map[string]ReachabilityResult{
			"10.0.0.1": {IP: "10.0.0.1", Reachable: false, FailureText: "timeout"},
		},
	}

	outcome, err := runPreScanPing(context.Background(), runInputs{
		cidrRecords: []input.CIDRRecord{
			{CIDR: "10.0.0.0/24", Selector: mustSelectorNet(t, "10.0.0.1/32"), FabName: "fab-a", CIDRName: "cidr-a"},
			{CIDR: "10.0.0.0/24", Selector: mustSelectorNet(t, "10.0.0.1/32"), FabName: "fab-b", CIDRName: "cidr-b"},
		},
		portSpecs: []input.PortSpec{{Number: 80, Proto: "tcp", Raw: "80/tcp"}},
	}, config.Config{
		Timeout: 250 * time.Millisecond,
		Workers: 4,
		}, checker, state.PreScanPingState{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(checker.calls()) != 1 || checker.calls()[0] != "10.0.0.1" {
		t.Fatalf("expected single checker call for duplicated ip, got %v", checker.calls())
	}
	if !outcome.State.Enabled {
		t.Fatalf("expected pre-scan state enabled, got %+v", outcome.State)
	}
	if outcome.State.TimeoutMS != 100 {
		t.Fatalf("unexpected timeout ms: %+v", outcome.State)
	}
	if got := checker.timeoutFor("10.0.0.1"); got != 100*time.Millisecond {
		t.Fatalf("expected fixed pre-scan timeout, got %v", got)
	}
	if len(outcome.UnreachableIPv4U32) != 1 || outcome.UnreachableIPv4U32[0] != ipv4ToUint32("10.0.0.1") {
		t.Fatalf("unexpected unreachable set: %+v", outcome.UnreachableIPv4U32)
	}
	if len(outcome.UnreachableRows) != 2 {
		t.Fatalf("expected two unreachable rows for two scan contexts, got %+v", outcome.UnreachableRows)
	}
}

func TestPreScanPing_Run_AggregatesUnreachableRowsPerContextWithoutPortExpansion(t *testing.T) {
	checker := &fakePreScanChecker{
		results: map[string]ReachabilityResult{
			"10.0.0.2": {IP: "10.0.0.2", Reachable: false, FailureText: "timeout"},
		},
	}

	outcome, err := runPreScanPing(context.Background(), runInputs{
		cidrRecords: []input.CIDRRecord{
			{CIDR: "10.0.0.0/24", Selector: mustSelectorNet(t, "10.0.0.2/32"), FabName: "fab-a", CIDRName: "cidr-a"},
		},
		portSpecs: []input.PortSpec{
			{Number: 80, Proto: "tcp", Raw: "80/tcp"},
			{Number: 443, Proto: "tcp", Raw: "443/tcp"},
		},
	}, config.Config{
		Timeout: 100 * time.Millisecond,
		Workers: 2,
		}, checker, state.PreScanPingState{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(outcome.UnreachableRows) != 1 {
		t.Fatalf("expected one unreachable row for one input context, got %+v", outcome.UnreachableRows)
	}
	got := outcome.UnreachableRows[0]
	if got.IP != "10.0.0.2" || got.IPCidr != "10.0.0.0/24" {
		t.Fatalf("unexpected unreachable row: %+v", got)
	}
	if got.Status != "unreachable" || got.Reason != "ping failed within 100ms" {
		t.Fatalf("unexpected unreachable row status/reason: %+v", got)
	}
}

func TestPreScanPing_Run_ReusesSavedUnreachableStateWithoutCallingChecker(t *testing.T) {
	checker := &fakePreScanChecker{}
	saved := state.PreScanPingState{
		Enabled:            true,
		TimeoutMS:          500,
		UnreachableIPv4U32: []uint32{ipv4ToUint32("10.0.0.3")},
	}

	outcome, err := runPreScanPing(context.Background(), runInputs{
		cidrRecords: []input.CIDRRecord{
			{CIDR: "10.0.0.0/24", Selector: mustSelectorNet(t, "10.0.0.3/32"), FabName: "fab-a", CIDRName: "cidr-a"},
			{CIDR: "10.0.0.0/24", Selector: mustSelectorNet(t, "10.0.0.4/32"), FabName: "fab-b", CIDRName: "cidr-b"},
		},
		portSpecs: []input.PortSpec{{Number: 80, Proto: "tcp", Raw: "80/tcp"}},
	}, config.Config{
		Timeout: 100 * time.Millisecond,
		Workers: 4,
		}, checker, saved)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(checker.calls()) != 0 {
		t.Fatalf("expected saved state reuse to skip checker, got calls %v", checker.calls())
	}
	if len(outcome.UnreachableRows) != 1 || outcome.UnreachableRows[0].IP != "10.0.0.3" {
		t.Fatalf("unexpected unreachable rows from saved state: %+v", outcome.UnreachableRows)
	}
	if outcome.State.TimeoutMS != 100 || !outcome.State.Enabled {
		t.Fatalf("expected saved state to be reused, got %+v", outcome.State)
	}
}

func TestPreScanPing_Run_WithSavedStateAndCanceledContext_Aborts(t *testing.T) {
	checker := &fakePreScanChecker{}
	saved := state.PreScanPingState{
		Enabled:            true,
		TimeoutMS:          100,
		UnreachableIPv4U32: []uint32{ipv4ToUint32("10.0.0.3")},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	outcome, err := runPreScanPing(ctx, runInputs{
		cidrRecords: []input.CIDRRecord{
			{CIDR: "10.0.0.0/24", Selector: mustSelectorNet(t, "10.0.0.3/32"), FabName: "fab-a", CIDRName: "cidr-a"},
		},
		portSpecs: []input.PortSpec{{Number: 80, Proto: "tcp", Raw: "80/tcp"}},
	}, config.Config{
		Timeout: 100 * time.Millisecond,
		Workers: 1,
	}, checker, saved)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled context error, got %v", err)
	}
	if len(checker.calls()) != 0 {
		t.Fatalf("expected saved-state cancel to skip checker, got %v", checker.calls())
	}
	if outcome.State.Enabled || len(outcome.UnreachableIPv4U32) != 0 || len(outcome.UnreachableRows) != 0 {
		t.Fatalf("expected empty outcome on canceled saved-state pre-scan, got %+v", outcome)
	}
}

func TestPreScanPing_Run_RichRowsAggregateToSingleUnreachableRowWithDistinctMergedMetadata(t *testing.T) {
	checker := &fakePreScanChecker{
		results: map[string]ReachabilityResult{
			"10.0.0.9": {IP: "10.0.0.9", Reachable: false, FailureText: "timeout"},
		},
	}

	outcome, err := runPreScanPing(context.Background(), runInputs{
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
	}, config.Config{
		Timeout: 5 * time.Second,
		Workers: 2,
		}, checker, state.PreScanPingState{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(outcome.UnreachableRows) != 1 {
		t.Fatalf("expected single aggregated rich unreachable row, got %+v", outcome.UnreachableRows)
	}
	got := outcome.UnreachableRows[0]
	if got.IP != "10.0.0.9" || got.IPCidr != "10.0.0.0/24" {
		t.Fatalf("unexpected aggregated row identity: %+v", got)
	}
	if got.Reason != "ping failed within 100ms" {
		t.Fatalf("unexpected aggregated row reason: %+v", got)
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

func TestPreScanPing_Run_FailsOnToolLevelCheckerError(t *testing.T) {
	checker := &fakePreScanChecker{
		detailedErrs: map[string]error{
			"10.0.0.1": errors.New("ping binary missing"),
		},
	}

	outcome, err := runPreScanPing(context.Background(), runInputs{
		cidrRecords: []input.CIDRRecord{
			{CIDR: "10.0.0.0/24", Selector: mustSelectorNet(t, "10.0.0.1/32"), FabName: "fab-a", CIDRName: "cidr-a"},
		},
		portSpecs: []input.PortSpec{{Number: 80, Proto: "tcp", Raw: "80/tcp"}},
	}, config.Config{
		Timeout: 250 * time.Millisecond,
		Workers: 1,
	}, checker, state.PreScanPingState{})
	if err == nil {
		t.Fatal("expected tool-level checker failure")
	}
	if outcome.State.Enabled || len(outcome.UnreachableIPv4U32) != 0 || len(outcome.UnreachableRows) != 0 {
		t.Fatalf("expected empty outcome on fatal checker failure, got %+v", outcome)
	}
}

func TestRunReachabilityChecks_FailsFastOnFatalCheckerError(t *testing.T) {
	checker := &fakePreScanChecker{
		detailedErrs: map[string]error{
			"10.0.0.1": errors.New("ping binary missing"),
		},
	}

	_, err := runReachabilityChecks(context.Background(), checker, []string{"10.0.0.1", "10.0.0.2"}, 1)
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

func (f *fakePreScanChecker) timeoutFor(ip string) time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.timeouts[ip]
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

package scanapp

import (
	"fmt"
	"sort"
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/input"
)

// group_builder_richmerge_test.go — Phase 1a of docs/speed-up-scan-prepare.
//
// These are behavior-preserving characterization tests for
// buildRichGroupsWithPredicate. They pin the exact target set, order, and
// merged metadata that the O(N^2) merge produces so the map-indexed rewrite
// (design.md §3.1) cannot silently change semantics. The quadratic-vs-linear
// scaling guard lives in resume_rebuild_bench_test.go
// (BenchmarkResumeRebuild/rich_precheck_scaling_*).

// TestBuildRichGroups_LargePrecheckSegment_DistinctKeysSortedAndComplete covers
// case (a): one PRECHECK_ALLOW_ALL segment expands to every host, each host
// getting a distinct execution key. Asserts the full target set is present,
// de-duplicated, ordered by IP, and every execution key is distinct.
func TestBuildRichGroups_LargePrecheckSegment_DistinctKeysSortedAndComplete(t *testing.T) {
	const segment = "10.50.0.0/24"
	rows := []input.CIDRRecord{
		{
			IsRich:            true,
			IsValid:           true,
			ExecutionKey:      "seed:80/tcp",
			DstNetworkSegment: segment,
			Port:              80,
			CIDRName:          "http",
			ServiceLabel:      "web",
			Protocol:          "tcp",
			Decision:          "accept",
			PolicyID:          "P-ALL",
			Reason:            reasonPrecheckAllowAll,
			RowNumber:         1,
		},
	}

	groups, err := buildRichGroupsWithPredicate(rows, nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	got := groups[segment]

	// /24 => 256 addresses; broadcast .255 excluded, network .0 kept => 255.
	if len(got.targets) != 255 {
		t.Fatalf("expected 255 expanded targets, got %d", len(got.targets))
	}

	seenKeys := make(map[string]bool, len(got.targets))
	for i, target := range got.targets {
		if target.port != 80 {
			t.Fatalf("target %d unexpected port %d", i, target.port)
		}
		wantKey := fmt.Sprintf("%s:80/tcp", target.ip)
		if target.meta.executionKey != wantKey {
			t.Fatalf("target %d execution key = %s, want %s", i, target.meta.executionKey, wantKey)
		}
		if seenKeys[target.meta.executionKey] {
			t.Fatalf("duplicate execution key %s at idx %d", target.meta.executionKey, i)
		}
		seenKeys[target.meta.executionKey] = true
		if i > 0 {
			prev := ipv4ToUint32(got.targets[i-1].ip)
			cur := ipv4ToUint32(target.ip)
			if prev >= cur {
				t.Fatalf("targets not sorted ascending by ip at idx %d: %s then %s", i, got.targets[i-1].ip, target.ip)
			}
		}
	}
	// First host is the network address, last kept host is .254.
	if got.targets[0].ip != "10.50.0.0" {
		t.Fatalf("first target ip = %s, want 10.50.0.0", got.targets[0].ip)
	}
	if got.targets[len(got.targets)-1].ip != "10.50.0.254" {
		t.Fatalf("last target ip = %s, want 10.50.0.254", got.targets[len(got.targets)-1].ip)
	}
}

// TestBuildRichGroups_ManyRowsSharingKeys_DedupAndMergeMetadata covers case (b):
// one segment assembled from many rows that all share the same execution key.
// Asserts a single de-duplicated target with metadata merged across every row
// in row order (the pipe-join contract of mergeFieldValue).
func TestBuildRichGroups_ManyRowsSharingKeys_DedupAndMergeMetadata(t *testing.T) {
	const (
		segment = "10.60.0.0/24"
		key     = "10.60.0.5:443/tcp"
	)
	rows := make([]input.CIDRRecord, 0, 5)
	for i := 0; i < 5; i++ {
		rows = append(rows, input.CIDRRecord{
			IsRich:            true,
			IsValid:           true,
			ExecutionKey:      key,
			DstIP:             "10.60.0.5",
			DstNetworkSegment: segment,
			Port:              443,
			Protocol:          "tcp",
			Reason:            reasonMatchPolicyAccept,
			Decision:          "accept",
			PolicyID:          fmt.Sprintf("P-%d", i),
			ServiceLabel:      fmt.Sprintf("svc-%d", i),
			RowNumber:         i + 1,
		})
	}

	groups, err := buildRichGroupsWithPredicate(rows, nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	got := groups[segment]
	if len(got.targets) != 1 {
		t.Fatalf("expected 1 de-duplicated target, got %d", len(got.targets))
	}
	merged := got.targets[0]
	if merged.ip != "10.60.0.5" || merged.port != 443 {
		t.Fatalf("unexpected merged target: %+v", merged)
	}
	if merged.meta.executionKey != key {
		t.Fatalf("unexpected execution key: %s", merged.meta.executionKey)
	}
	if wantPolicy := "P-0|P-1|P-2|P-3|P-4"; merged.meta.policyID != wantPolicy {
		t.Fatalf("merged policyID = %s, want %s", merged.meta.policyID, wantPolicy)
	}
	if wantSvc := "svc-0|svc-1|svc-2|svc-3|svc-4"; merged.meta.serviceLabel != wantSvc {
		t.Fatalf("merged serviceLabel = %s, want %s", merged.meta.serviceLabel, wantSvc)
	}
}

// TestBuildRichGroups_CrossSegmentOwnership_FirstSegmentKeepsSharedKey covers
// case (c): the same execution key is produced under two different segments.
// The first segment to claim the key owns it; the later segment's copy is
// redirected into the owner's group and merged, and the later segment keeps
// only its own distinct keys.
func TestBuildRichGroups_CrossSegmentOwnership_FirstSegmentKeepsSharedKey(t *testing.T) {
	const (
		segA      = "10.0.0.0/24"
		segB      = "10.1.0.0/24"
		sharedKey = "10.0.0.5:80/tcp"
		ownKeyB   = "10.1.0.5:80/tcp"
	)
	rows := []input.CIDRRecord{
		{ // segment A claims the shared key first
			IsRich:            true,
			IsValid:           true,
			ExecutionKey:      sharedKey,
			DstIP:             "10.0.0.5",
			DstNetworkSegment: segA,
			Port:              80,
			Protocol:          "tcp",
			Reason:            reasonMatchPolicyAccept,
			PolicyID:          "PA",
			RowNumber:         1,
		},
		{ // segment B has its own distinct target
			IsRich:            true,
			IsValid:           true,
			ExecutionKey:      ownKeyB,
			DstIP:             "10.1.0.5",
			DstNetworkSegment: segB,
			Port:              80,
			Protocol:          "tcp",
			Reason:            reasonMatchPolicyAccept,
			PolicyID:          "PB-own",
			RowNumber:         2,
		},
		{ // segment B also emits the shared key -> redirected to A's group
			IsRich:            true,
			IsValid:           true,
			ExecutionKey:      sharedKey,
			DstIP:             "10.0.0.5",
			DstNetworkSegment: segB,
			Port:              80,
			Protocol:          "tcp",
			Reason:            reasonMatchPolicyAccept,
			PolicyID:          "PB-shared",
			RowNumber:         3,
		},
	}

	groups, err := buildRichGroupsWithPredicate(rows, nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d (%v)", len(groups), groupKeys(groups))
	}

	a := groups[segA]
	if len(a.targets) != 1 {
		t.Fatalf("owner segment A: expected 1 target, got %d", len(a.targets))
	}
	if a.targets[0].meta.executionKey != sharedKey {
		t.Fatalf("owner segment A: unexpected key %s", a.targets[0].meta.executionKey)
	}
	if want := "PA|PB-shared"; a.targets[0].meta.policyID != want {
		t.Fatalf("owner segment A merged policyID = %s, want %s", a.targets[0].meta.policyID, want)
	}

	b := groups[segB]
	if len(b.targets) != 1 {
		t.Fatalf("segment B: expected 1 target (own key only), got %d", len(b.targets))
	}
	if b.targets[0].meta.executionKey != ownKeyB {
		t.Fatalf("segment B: unexpected key %s", b.targets[0].meta.executionKey)
	}
	if b.targets[0].meta.policyID != "PB-own" {
		t.Fatalf("segment B: unexpected policyID %s", b.targets[0].meta.policyID)
	}
}

func groupKeys(groups map[string]cidrGroup) []string {
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

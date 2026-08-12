package task

import (
	"math"
	"net"
	"strings"
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/input"
)

func mustIPv4Network(t *testing.T, raw string) *net.IPNet {
	t.Helper()
	_, network, err := net.ParseCIDR(raw)
	if err != nil {
		t.Fatal(err)
	}
	return network
}

func TestEstimateIPSelectors_DefaultLimitsPermitSlash9AndRejectSlash8(t *testing.T) {
	estimate, err := EstimateIPSelectors([]SelectorInput{{Row: 7, Selector: "10.0.0.0/9"}}, DefaultExpansionLimits())
	if err != nil {
		t.Fatalf("EstimateIPSelectors(/9) error = %v", err)
	}
	if estimate.CandidateCount != 8_388_608 {
		t.Fatalf("/9 candidate count = %d, want 8388608", estimate.CandidateCount)
	}

	_, err = EstimateIPSelectors([]SelectorInput{{Row: 8, Selector: "10.0.0.0/8"}}, DefaultExpansionLimits())
	if err == nil {
		t.Fatal("EstimateIPSelectors(/8) error = nil, want limit error")
	}
	for _, text := range []string{
		"row 8",
		"10.0.0.0/8",
		"candidate count 16777216",
		"count limit 10000000",
		"estimated 26.166 GB",
		"memory limit 16 GB",
		"-target-count-limit",
		"-target-memory-limit-gb",
	} {
		if !strings.Contains(err.Error(), text) {
			t.Errorf("limit error %q does not contain %q", err, text)
		}
	}
}

func TestEstimateIPSelectors_TenMillionCandidatesEstimateSixteenGB(t *testing.T) {
	estimate, err := EstimateCandidateCounts([]CandidateInput{{Row: 1, CIDR: "synthetic", Count: 10_000_000}}, DefaultExpansionLimits())
	if err != nil {
		t.Fatalf("EstimateCandidateCounts() error = %v", err)
	}
	if estimate.EstimatedBytes != 16_000_000_000 {
		t.Fatalf("estimated bytes = %d, want 16000000000", estimate.EstimatedBytes)
	}
}

func TestNewExpansionLimits_OverrideDisableAndInvalidValues(t *testing.T) {
	limits, err := NewExpansionLimits(20_000_000, 32)
	if err != nil {
		t.Fatalf("NewExpansionLimits(override) error = %v", err)
	}
	if limits.CandidateLimit() != 20_000_000 || limits.MemoryLimitGB() != 32 {
		t.Fatalf("override limits = (%d, %d), want (20000000, 32)", limits.CandidateLimit(), limits.MemoryLimitGB())
	}

	disabled, err := NewExpansionLimits(0, 0)
	if err != nil {
		t.Fatalf("NewExpansionLimits(disabled) error = %v", err)
	}
	if disabled.CandidateLimit() != 0 || disabled.MemoryLimitGB() != 0 {
		t.Fatalf("disabled limits = (%d, %d), want (0, 0)", disabled.CandidateLimit(), disabled.MemoryLimitGB())
	}

	for _, tc := range []struct {
		count int64
		gb    int64
		want  string
	}{
		{count: -1, gb: 16, want: "-target-count-limit must be >= 0"},
		{count: 10_000_000, gb: -1, want: "-target-memory-limit-gb must be >= 0"},
		{count: math.MaxInt64, gb: math.MaxInt64, want: "-target-memory-limit-gb is too large"},
	} {
		_, err := NewExpansionLimits(tc.count, tc.gb)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("NewExpansionLimits(%d, %d) error = %v, want %q", tc.count, tc.gb, err, tc.want)
		}
	}
}

func TestEstimateCandidateCounts_CountsRowsBeforeDedupAndDetectsOverflow(t *testing.T) {
	limits, err := NewExpansionLimits(8, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = EstimateIPSelectors([]SelectorInput{
		{Row: 2, Selector: "192.0.2.0/30"},
		{Row: 3, Selector: "192.0.2.0/30"},
		{Row: 4, Selector: "192.0.2.1"},
	}, limits)
	if err == nil || !strings.Contains(err.Error(), "candidate count 9") || !strings.Contains(err.Error(), "row 4") {
		t.Fatalf("repeated selector error = %v, want row 4 count 9", err)
	}

	_, err = EstimateCandidateCounts([]CandidateInput{
		{Row: 1, CIDR: "first", Count: math.MaxUint64},
		{Row: 2, CIDR: "second", Count: 1},
	}, ExpansionLimits{})
	if err == nil || !strings.Contains(err.Error(), "overflow") {
		t.Fatalf("overflow error = %v, want arithmetic overflow", err)
	}
}

func TestEstimateIPSelectors_StopsAtTheFirstCIDRThatCrossesTheLimit(t *testing.T) {
	_, err := EstimateIPSelectors([]SelectorInput{
		{Row: 2, Selector: "10.0.0.0/8"},
		{Row: 3, Selector: "not-a-selector"},
	}, DefaultExpansionLimits())
	if err == nil || !strings.Contains(err.Error(), "row 2") || strings.Contains(err.Error(), "row 3") {
		t.Fatalf("EstimateIPSelectors() error = %v, want first CIDR limit at row 2", err)
	}
}

func TestEstimateIPSelectors_CountOverrideChangesCIDRLimitAndZeroHandlesSlashZero(t *testing.T) {
	override, err := NewExpansionLimits(20_000_000, 0)
	if err != nil {
		t.Fatal(err)
	}
	estimate, err := EstimateIPSelectors([]SelectorInput{{Row: 1, Selector: "10.0.0.0/8"}}, override)
	if err != nil {
		t.Fatalf("EstimateIPSelectors(/8 override) error = %v", err)
	}
	if estimate.CandidateCount != 16_777_216 {
		t.Fatalf("/8 candidate count = %d, want 16777216", estimate.CandidateCount)
	}

	bypass, err := NewExpansionLimits(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	estimate, err = EstimateIPSelectors([]SelectorInput{{Row: 1, Selector: "0.0.0.0/0"}}, bypass)
	if err != nil {
		t.Fatalf("EstimateIPSelectors(/0 bypass) error = %v", err)
	}
	if estimate.CandidateCount != 4_294_967_296 {
		t.Fatalf("/0 candidate count = %d, want 4294967296", estimate.CandidateCount)
	}
}

func TestEstimateAuthorizedCIDRRecords_BasicRowsCountBeforeDedupAndIncompleteFiltering(t *testing.T) {
	segmentA := mustIPv4Network(t, "192.0.2.0/24")
	segmentB := mustIPv4Network(t, "198.51.100.0/24")
	selector := mustIPv4Network(t, "192.0.2.0/30")
	records := []input.CIDRRecord{
		{RowNumber: 2, CIDR: segmentA.String(), Net: segmentA, IPRaw: "192.0.2.0/30", Selector: selector},
		{RowNumber: 3, CIDR: segmentA.String(), Net: segmentA, IPRaw: "192.0.2.0/30", Selector: selector},
		{RowNumber: 4, CIDR: segmentB.String(), Net: segmentB, IPRaw: "198.51.100.1"},
	}
	limits, err := NewExpansionLimits(8, 0)
	if err != nil {
		t.Fatal(err)
	}

	estimate, err := EstimateAuthorizedCIDRRecords(records, limits, map[string]struct{}{segmentA.String(): {}})
	if err != nil {
		t.Fatalf("EstimateAuthorizedCIDRRecords() error = %v", err)
	}
	if estimate.CandidateCount != 8 {
		t.Fatalf("candidate count = %d, want 8", estimate.CandidateCount)
	}

	_, err = EstimateAuthorizedCIDRRecords(records, limits, nil)
	if err == nil || !strings.Contains(err.Error(), "row 4") || !strings.Contains(err.Error(), "candidate count 9") {
		t.Fatalf("all-row error = %v, want row 4 count 9", err)
	}

	estimate, err = EstimateAuthorizedCIDRRecords(records, limits, map[string]struct{}{})
	if err != nil {
		t.Fatalf("empty incomplete set error = %v", err)
	}
	if estimate.CandidateCount != 0 {
		t.Fatalf("empty incomplete candidate count = %d, want 0", estimate.CandidateCount)
	}
}

func TestEstimateAuthorizedCIDRRecords_RichDenyWinsAndPrecheckSubtractsDeniedAddresses(t *testing.T) {
	records := []input.CIDRRecord{
		{
			RowNumber:         2,
			IsRich:            true,
			IsValid:           true,
			DstIP:             "192.0.2.1",
			DstNetworkSegment: "192.0.2.0/30",
			Decision:          "accept",
			ExecutionKey:      "192.0.2.1:443/tcp",
			Port:              443,
			Protocol:          "tcp",
			Reason:            "PRECHECK_ALLOW_ALL",
		},
		{
			RowNumber:         3,
			IsRich:            true,
			IsValid:           true,
			DstIP:             "192.0.2.1",
			DstNetworkSegment: "192.0.2.0/30",
			Decision:          "deny",
			ExecutionKey:      "192.0.2.1:443/tcp",
			Port:              443,
			Protocol:          "tcp",
		},
		{
			RowNumber:         4,
			IsRich:            true,
			IsValid:           true,
			DstIP:             "198.51.100.7",
			DstNetworkSegment: "198.51.100.0/24",
			Decision:          "accept",
			ExecutionKey:      "198.51.100.7:8443/tcp",
			Port:              8443,
			Protocol:          "tcp",
		},
		{
			RowNumber:         5,
			IsRich:            true,
			IsValid:           true,
			DstIP:             "198.51.100.7",
			DstNetworkSegment: "198.51.100.0/24",
			Decision:          "deny",
			ExecutionKey:      "198.51.100.7:8443/tcp",
			Port:              8443,
			Protocol:          "tcp",
		},
	}
	limits, err := NewExpansionLimits(3, 0)
	if err != nil {
		t.Fatal(err)
	}

	estimate, err := EstimateAuthorizedCIDRRecords(records, limits, nil)
	if err != nil {
		t.Fatalf("EstimateAuthorizedCIDRRecords() error = %v", err)
	}
	if estimate.CandidateCount != 3 {
		t.Fatalf("authorized candidate count = %d, want 3", estimate.CandidateCount)
	}
}

func TestEstimateAuthorizedCIDRRecords_InvalidAuthorizedSelectorReturnsRowError(t *testing.T) {
	records := []input.CIDRRecord{{
		RowNumber: 9,
		CIDR:      "192.0.2.0/24",
		IPRaw:     "not-a-selector",
	}}

	_, err := EstimateAuthorizedCIDRRecords(records, DefaultExpansionLimits(), nil)
	if err == nil || !strings.Contains(err.Error(), "row 9") || !strings.Contains(err.Error(), "not-a-selector") {
		t.Fatalf("error = %v, want row 9 invalid selector", err)
	}
}

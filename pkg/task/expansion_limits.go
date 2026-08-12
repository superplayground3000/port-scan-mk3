package task

import (
	"encoding/binary"
	"fmt"
	"math"
	"net"
	"sort"
	"strings"

	"github.com/xuxiping/port-scan-mk3/pkg/input"
)

const (
	// DefaultTargetCandidateLimit is the default number of candidate addresses.
	DefaultTargetCandidateLimit uint64 = 10_000_000
	// DefaultTargetMemoryLimitGB is the default memory budget in decimal GB.
	DefaultTargetMemoryLimitGB uint64 = 16
	decimalGB                         = uint64(1_000_000_000)
	expansionBaseBytes                = uint64(1_000_000_000)
	expansionBytesPerCandidate        = uint64(1_500)
)

// ExpansionLimits contains the limits for one complete target expansion.
// A zero value disables both limits.
type ExpansionLimits struct {
	candidateLimit  uint64
	memoryLimitGB   uint64
	memoryLimitByte uint64
}

// DefaultExpansionLimits returns the default count and memory limits.
func DefaultExpansionLimits() ExpansionLimits {
	limits, err := NewExpansionLimits(int64(DefaultTargetCandidateLimit), int64(DefaultTargetMemoryLimitGB))
	if err != nil {
		panic(err)
	}
	return limits
}

// NewExpansionLimits verifies limits from the two command flags.
// A zero value disables its related limit.
// It returns the verified count and decimal-GB limits.
// It returns an error for a negative value or memory-limit conversion overflow.
func NewExpansionLimits(candidateLimit, memoryLimitGB int64) (ExpansionLimits, error) {
	if candidateLimit < 0 {
		return ExpansionLimits{}, fmt.Errorf("-target-count-limit must be >= 0")
	}
	if memoryLimitGB < 0 {
		return ExpansionLimits{}, fmt.Errorf("-target-memory-limit-gb must be >= 0")
	}
	if uint64(memoryLimitGB) > math.MaxUint64/decimalGB {
		return ExpansionLimits{}, fmt.Errorf("-target-memory-limit-gb is too large")
	}
	return ExpansionLimits{
		candidateLimit:  uint64(candidateLimit),
		memoryLimitGB:   uint64(memoryLimitGB),
		memoryLimitByte: uint64(memoryLimitGB) * decimalGB,
	}, nil
}

// CandidateLimit returns the configured candidate limit.
// A zero value means that the count limit is disabled.
func (l ExpansionLimits) CandidateLimit() uint64 {
	return l.candidateLimit
}

// MemoryLimitGB returns the configured memory limit in decimal GB.
// A zero value means that the memory limit is disabled.
func (l ExpansionLimits) MemoryLimitGB() uint64 {
	return l.memoryLimitGB
}

// SelectorInput identifies one input selector for count estimation.
type SelectorInput struct {
	Row      int
	Selector string
}

// CandidateInput identifies a known count for one input row and CIDR.
type CandidateInput struct {
	Row   int
	CIDR  string
	Count uint64
}

// ExpansionEstimate contains the count and estimated allocation size.
type ExpansionEstimate struct {
	CandidateCount uint64
	EstimatedBytes uint64
}

// EstimateAuthorizedCIDRRecords calculates target expansion for authorized rows.
// It counts each authorized row before de-duplication and target filtering.
// A non-nil CIDR set selects the rows for incomplete resume chunks.
// The result contains the candidate count and estimated bytes.
// The function returns an error for invalid selectors, overflow, or a limit failure.
func EstimateAuthorizedCIDRRecords(records []input.CIDRRecord, limits ExpansionLimits, includedCIDRs map[string]struct{}) (ExpansionEstimate, error) {
	denied := make(map[string]struct{})
	for _, record := range records {
		if record.IsRich && record.IsValid && strings.EqualFold(strings.TrimSpace(record.Decision), "deny") {
			if key := strings.TrimSpace(record.ExecutionKey); key != "" {
				denied[key] = struct{}{}
			}
		}
	}
	deniedCandidates := newDeniedCandidateIndex(records, denied)

	estimate := ExpansionEstimate{EstimatedBytes: expansionBaseBytes}
	for i, record := range records {
		row := record.RowNumber
		if row <= 0 {
			row = i + 1
		}
		if record.IsRich {
			if !record.IsValid || strings.EqualFold(strings.TrimSpace(record.Decision), "deny") {
				continue
			}
			if _, excluded := denied[strings.TrimSpace(record.ExecutionKey)]; excluded && !recordUsesPrecheckExpansion(record) {
				continue
			}
		}

		cidr := recordCIDR(record)
		if includedCIDRs != nil {
			if _, included := includedCIDRs[cidr]; !included {
				continue
			}
		}
		selector, err := recordExpansionSelector(record, cidr)
		if err != nil {
			return ExpansionEstimate{}, fmt.Errorf("row %d: %w", row, err)
		}
		count, normalized, err := countSelectorCandidates(selector)
		if err != nil {
			return ExpansionEstimate{}, fmt.Errorf("row %d selector %q: %w", row, selector, err)
		}
		if recordUsesPrecheckExpansion(record) {
			count -= deniedCandidates.count(record, selector)
		}
		estimate, err = addCandidateCount(estimate.CandidateCount, CandidateInput{Row: row, CIDR: normalized, Count: count}, limits)
		if err != nil {
			return ExpansionEstimate{}, err
		}
	}
	return estimate, nil
}

// EstimateIPSelectors calculates candidate counts without enumeration.
// The result contains the candidate count and estimated bytes.
// The function returns an error for an invalid selector, overflow, or a limit failure.
func EstimateIPSelectors(inputs []SelectorInput, limits ExpansionLimits) (ExpansionEstimate, error) {
	estimate := ExpansionEstimate{EstimatedBytes: expansionBaseBytes}
	for _, item := range inputs {
		count, cidr, err := countSelectorCandidates(item.Selector)
		if err != nil {
			return ExpansionEstimate{}, fmt.Errorf("row %d selector %q: %w", item.Row, item.Selector, err)
		}
		estimate, err = addCandidateCount(estimate.CandidateCount, CandidateInput{Row: item.Row, CIDR: cidr, Count: count}, limits)
		if err != nil {
			return ExpansionEstimate{}, err
		}
	}
	return estimate, nil
}

// EstimateCandidateCounts calculates one complete expansion from known counts.
// It counts every row before target de-duplication and filtering.
// The result contains the candidate count and estimated bytes.
// The function returns an error for overflow or a limit failure.
func EstimateCandidateCounts(inputs []CandidateInput, limits ExpansionLimits) (ExpansionEstimate, error) {
	estimate := ExpansionEstimate{EstimatedBytes: expansionBaseBytes}
	for _, item := range inputs {
		var err error
		estimate, err = addCandidateCount(estimate.CandidateCount, item, limits)
		if err != nil {
			return ExpansionEstimate{}, err
		}
	}
	return estimate, nil
}

func addCandidateCount(total uint64, item CandidateInput, limits ExpansionLimits) (ExpansionEstimate, error) {
	if item.Count > math.MaxUint64-total {
		return ExpansionEstimate{}, overflowError(item, "candidate count")
	}
	total += item.Count
	estimated, err := estimateExpansionBytes(total)
	if err != nil {
		return ExpansionEstimate{}, overflowError(item, "memory estimate")
	}
	if exceedsExpansionLimits(total, estimated, limits) {
		return ExpansionEstimate{}, expansionLimitError(item, total, estimated, limits)
	}
	return ExpansionEstimate{CandidateCount: total, EstimatedBytes: estimated}, nil
}

func countSelectorCandidates(raw string) (uint64, string, error) {
	selector := strings.TrimSpace(raw)
	if selector == "" {
		return 0, selector, fmt.Errorf("empty selector")
	}
	if ip := net.ParseIP(selector); ip != nil {
		if ip.To4() == nil {
			return 0, selector, fmt.Errorf("only ipv4 is supported: %s", selector)
		}
		return 1, selector, nil
	}
	_, network, err := net.ParseCIDR(selector)
	if err != nil {
		return 0, selector, fmt.Errorf("invalid selector %q: %w", selector, err)
	}
	ones, bits := network.Mask.Size()
	if bits != 32 || network.IP.To4() == nil {
		return 0, selector, fmt.Errorf("only ipv4 is supported: %s", selector)
	}
	return uint64(1) << uint(bits-ones), selector, nil
}

func recordCIDR(record input.CIDRRecord) string {
	if record.IsRich {
		if cidr := strings.TrimSpace(record.DstNetworkSegment); cidr != "" {
			return cidr
		}
	}
	if cidr := strings.TrimSpace(record.CIDR); cidr != "" {
		return cidr
	}
	if record.Net != nil {
		return record.Net.String()
	}
	return ""
}

func recordExpansionSelector(record input.CIDRRecord, cidr string) (string, error) {
	if record.IsRich {
		if recordUsesPrecheckExpansion(record) {
			if cidr == "" {
				return "", fmt.Errorf("rich record missing dst_network_segment")
			}
			return cidr, nil
		}
		if selector := strings.TrimSpace(record.DstIP); selector != "" {
			return selector, nil
		}
		return "", fmt.Errorf("rich record missing dst_ip")
	}
	if selector := strings.TrimSpace(record.IPRaw); selector != "" {
		return selector, nil
	}
	if record.Selector != nil {
		return record.Selector.String(), nil
	}
	if record.Net != nil {
		return record.Net.String(), nil
	}
	return "", fmt.Errorf("record missing selector")
}

func recordUsesPrecheckExpansion(record input.CIDRRecord) bool {
	return record.IsRich && strings.EqualFold(strings.TrimSpace(record.Reason), "PRECHECK_ALLOW_ALL")
}

type deniedCandidateGroup struct {
	port     int
	protocol string
}

type deniedCandidateIndex map[deniedCandidateGroup][]uint32

func newDeniedCandidateIndex(records []input.CIDRRecord, denied map[string]struct{}) deniedCandidateIndex {
	sets := make(map[deniedCandidateGroup]map[uint32]struct{})
	for _, record := range records {
		if !record.IsRich || !record.IsValid || !strings.EqualFold(strings.TrimSpace(record.Decision), "deny") {
			continue
		}
		ip := net.ParseIP(strings.TrimSpace(record.DstIP))
		if ip == nil || ip.To4() == nil {
			continue
		}
		protocol := normalizedProtocol(record.Protocol)
		key, err := BuildExecutionKey(ip.String(), record.Port, protocol)
		if err != nil {
			continue
		}
		if _, excluded := denied[key]; !excluded {
			continue
		}
		group := deniedCandidateGroup{port: record.Port, protocol: protocol}
		if sets[group] == nil {
			sets[group] = make(map[uint32]struct{})
		}
		sets[group][binary.BigEndian.Uint32(ip.To4())] = struct{}{}
	}

	index := make(deniedCandidateIndex, len(sets))
	for group, set := range sets {
		addresses := make([]uint32, 0, len(set))
		for address := range set {
			addresses = append(addresses, address)
		}
		sort.Slice(addresses, func(i, j int) bool { return addresses[i] < addresses[j] })
		index[group] = addresses
	}
	return index
}

func (index deniedCandidateIndex) count(record input.CIDRRecord, selector string) uint64 {
	_, network, err := net.ParseCIDR(selector)
	if err != nil {
		return 0
	}
	start := network.IP.To4()
	if start == nil {
		return 0
	}
	startN := binary.BigEndian.Uint32(start)
	maskN := binary.BigEndian.Uint32(network.Mask)
	endN := startN | ^maskN
	addresses := index[deniedCandidateGroup{port: record.Port, protocol: normalizedProtocol(record.Protocol)}]
	first := sort.Search(len(addresses), func(i int) bool { return addresses[i] >= startN })
	last := sort.Search(len(addresses), func(i int) bool { return addresses[i] > endN })
	return uint64(last - first)
}

func normalizedProtocol(protocol string) string {
	protocol = strings.ToLower(strings.TrimSpace(protocol))
	if protocol == "" {
		return "tcp"
	}
	return protocol
}

func estimateExpansionBytes(candidateCount uint64) (uint64, error) {
	if candidateCount > (math.MaxUint64-expansionBaseBytes)/expansionBytesPerCandidate {
		return 0, fmt.Errorf("memory estimate overflow")
	}
	return expansionBaseBytes + candidateCount*expansionBytesPerCandidate, nil
}

func exceedsExpansionLimits(count, estimated uint64, limits ExpansionLimits) bool {
	return limits.candidateLimit > 0 && count > limits.candidateLimit ||
		limits.memoryLimitByte > 0 && estimated > limits.memoryLimitByte
}

func expansionLimitError(item CandidateInput, count, estimated uint64, limits ExpansionLimits) error {
	return fmt.Errorf(
		"target expansion limit exceeded at row %d CIDR %s: candidate count %d, count limit %d, estimated %.3f GB, memory limit %d GB; use -target-count-limit and -target-memory-limit-gb to override the limits",
		item.Row,
		item.CIDR,
		count,
		limits.candidateLimit,
		float64(estimated)/float64(decimalGB),
		limits.memoryLimitGB,
	)
}

func overflowError(item CandidateInput, operation string) error {
	return fmt.Errorf(
		"target expansion %s overflow at row %d CIDR %s; review -target-count-limit and -target-memory-limit-gb",
		operation,
		item.Row,
		item.CIDR,
	)
}

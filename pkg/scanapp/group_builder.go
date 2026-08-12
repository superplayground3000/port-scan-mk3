package scanapp

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strings"

	"github.com/xuxiping/port-scan-mk3/pkg/input"
	"github.com/xuxiping/port-scan-mk3/pkg/task"
)

type cidrGroup struct {
	targets []scanTarget
}

type groupBuildStrategy interface {
	ShouldInclude(rec input.CIDRRecord) bool
	Key(rec input.CIDRRecord) (string, error)
	NewGroup(rec input.CIDRRecord) (cidrGroup, error)
	MergeGroup(existing cidrGroup, rec input.CIDRRecord) (cidrGroup, error)
	RequireNonEmpty() bool
}

func buildGroups(records []input.CIDRRecord, strategy groupBuildStrategy) (map[string]cidrGroup, error) {
	out := make(map[string]cidrGroup)
	for _, rec := range records {
		if !strategy.ShouldInclude(rec) {
			continue
		}

		key, err := strategy.Key(rec)
		if err != nil {
			return nil, err
		}

		group, ok := out[key]
		if !ok {
			group, err = strategy.NewGroup(rec)
			if err != nil {
				return nil, err
			}
		} else {
			group, err = strategy.MergeGroup(group, rec)
			if err != nil {
				return nil, err
			}
		}
		out[key] = group
	}

	if len(out) == 0 && strategy.RequireNonEmpty() {
		return nil, fmt.Errorf("no usable input rows")
	}

	for key, group := range out {
		sort.Slice(group.targets, func(i, j int) bool {
			return group.targets[i].ipU32 < group.targets[j].ipU32
		})
		out[key] = group
	}

	return out, nil
}

type basicGroupStrategy struct{}

func (basicGroupStrategy) ShouldInclude(_ input.CIDRRecord) bool { return true }

func (basicGroupStrategy) Key(rec input.CIDRRecord) (string, error) {
	cidr := rec.CIDR
	if cidr == "" && rec.Net != nil {
		cidr = rec.Net.String()
	}
	if cidr == "" {
		return "", fmt.Errorf("record missing ip_cidr")
	}
	return cidr, nil
}

func (s basicGroupStrategy) NewGroup(rec input.CIDRRecord) (cidrGroup, error) {
	targets, err := s.targets(rec)
	if err != nil {
		return cidrGroup{}, err
	}
	return cidrGroup{targets: targets}, nil
}

func (s basicGroupStrategy) MergeGroup(existing cidrGroup, rec input.CIDRRecord) (cidrGroup, error) {
	targets, err := s.targets(rec)
	if err != nil {
		return cidrGroup{}, err
	}
	existing.targets = append(existing.targets, targets...)
	return existing, nil
}

func (basicGroupStrategy) RequireNonEmpty() bool { return false }

func (basicGroupStrategy) targets(rec input.CIDRRecord) ([]scanTarget, error) {
	return (basicGroupStrategy{}).targetsContext(context.Background(), rec)
}

func (basicGroupStrategy) targetsContext(ctx context.Context, rec input.CIDRRecord) ([]scanTarget, error) {
	cidr := rec.CIDR
	if cidr == "" && rec.Net != nil {
		cidr = rec.Net.String()
	}

	selector := ""
	switch {
	case rec.Selector != nil:
		selector = rec.Selector.String()
	case strings.TrimSpace(rec.IPRaw) != "":
		selector = strings.TrimSpace(rec.IPRaw)
	case rec.Net != nil:
		selector = rec.Net.String()
	default:
		return nil, fmt.Errorf("record for cidr %s missing selector", cidr)
	}

	ips, err := task.ExpandIPSelectorsContext(ctx, []string{selector})
	if err != nil {
		return nil, fmt.Errorf("expand selector failed for cidr %s: %w", cidr, err)
	}
	// The broadcast address of the boundary subnet is not a scannable host and
	// is excluded regardless of whether it arrived via CIDR expansion or as an
	// explicitly listed single IP.
	ips = task.FilterBoundaryBroadcast(ips, rec.Net)

	targets := make([]scanTarget, 0, len(ips))
	for i, ip := range ips {
		if i%4096 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		targets = append(targets, scanTarget{
			ip:     ip,
			ipCidr: cidr,
			ipU32:  ipv4ToUint32(ip),
			meta: targetMeta{
				fabName:  rec.FabName,
				cidrName: rec.CIDRName,
			},
		})
	}
	return targets, nil
}

type richGroupStrategy struct{}

func (richGroupStrategy) ShouldInclude(rec input.CIDRRecord) bool {
	return rec.IsRich && rec.IsValid
}

func (richGroupStrategy) Key(rec input.CIDRRecord) (string, error) {
	return richCIDRKey(rec)
}

func (richGroupStrategy) NewGroup(rec input.CIDRRecord) (cidrGroup, error) {
	targets, err := richTargetsFromRecord(rec)
	if err != nil {
		return cidrGroup{}, err
	}
	return cidrGroup{
		targets: targets,
	}, nil
}

func (richGroupStrategy) MergeGroup(existing cidrGroup, rec input.CIDRRecord) (cidrGroup, error) {
	incomingTargets, err := richTargetsFromRecord(rec)
	if err != nil {
		return cidrGroup{}, err
	}
	for _, incoming := range incomingTargets {
		key := strings.TrimSpace(incoming.meta.executionKey)
		if key == "" {
			return cidrGroup{}, fmt.Errorf("rich record missing execution_key at row %d", rec.RowNumber)
		}
		idx := richTargetIndexByExecutionKey(existing.targets, key)
		if idx < 0 {
			existing.targets = append(existing.targets, incoming)
			continue
		}
		if err := mergeRichTargetValues(&existing.targets[idx], incoming); err != nil {
			return cidrGroup{}, err
		}
	}
	return existing, nil
}

func (richGroupStrategy) RequireNonEmpty() bool { return true }

func mergeFieldValue(existing, incoming string) string {
	existing = strings.TrimSpace(existing)
	incoming = strings.TrimSpace(incoming)
	if incoming == "" || existing == incoming {
		return existing
	}
	if existing == "" {
		return incoming
	}
	parts := strings.Split(existing, "|")
	for _, p := range parts {
		if p == incoming {
			return existing
		}
	}
	return existing + "|" + incoming
}

func buildCIDRGroups(cidrRecords []input.CIDRRecord) (map[string]cidrGroup, error) {
	return buildCIDRGroupsWithPredicate(cidrRecords, nil)
}

func buildCIDRGroupsWithPredicate(cidrRecords []input.CIDRRecord, reachable func(string) bool) (map[string]cidrGroup, error) {
	return buildCIDRGroupsWithPredicateContext(context.Background(), cidrRecords, reachable)
}

func buildCIDRGroupsWithPredicateContext(ctx context.Context, cidrRecords []input.CIDRRecord, reachable func(string) bool) (map[string]cidrGroup, error) {
	strategy := basicGroupStrategy{}
	predicate := normalizeReachablePredicate(reachable)
	out := make(map[string]cidrGroup)
	for i, rec := range cidrRecords {
		if i%4096 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		key, err := strategy.Key(rec)
		if err != nil {
			return nil, err
		}
		targets, err := strategy.targetsContext(ctx, rec)
		if err != nil {
			return nil, err
		}
		targets, err = filterScanTargetsContext(ctx, targets, predicate)
		if err != nil {
			return nil, err
		}
		if len(targets) == 0 {
			continue
		}

		group := out[key]
		group.targets = append(group.targets, targets...)
		out[key] = group
	}

	for key, group := range out {
		sort.Slice(group.targets, func(i, j int) bool {
			return group.targets[i].ipU32 < group.targets[j].ipU32
		})
		out[key] = group
	}
	return out, nil
}

func buildRichGroups(cidrRecords []input.CIDRRecord) (map[string]cidrGroup, error) {
	return buildRichGroupsWithPredicate(cidrRecords, nil)
}

func buildRichGroupsWithPredicate(cidrRecords []input.CIDRRecord, reachable func(string) bool) (map[string]cidrGroup, error) {
	return buildRichGroupsWithPredicateContext(context.Background(), cidrRecords, reachable)
}

func buildRichGroupsWithPredicateContext(ctx context.Context, cidrRecords []input.CIDRRecord, reachable func(string) bool) (map[string]cidrGroup, error) {
	predicate := normalizeReachablePredicate(reachable)
	deniedKeys := deniedRichExecutionKeys(cidrRecords)
	builders := make(map[string]*richGroupBuilder)
	ownerByExecutionKey := make(map[string]string)
	hasValidRichInput := false

	for recordIndex, rec := range cidrRecords {
		if recordIndex%4096 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		if !rec.IsRich || !rec.IsValid {
			continue
		}
		hasValidRichInput = true
		if richRecordDenied(rec) {
			continue
		}
		cidr, err := richCIDRKey(rec)
		if err != nil {
			return nil, err
		}
		targets, err := richTargetsFromRecordContext(ctx, rec)
		if err != nil {
			return nil, err
		}
		targets = filterAuthorizedRichTargets(targets, deniedKeys)
		targets, err = filterScanTargetsContext(ctx, targets, predicate)
		if err != nil {
			return nil, err
		}
		if len(targets) == 0 {
			continue
		}

		for targetIndex, target := range targets {
			if targetIndex%4096 == 0 {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
			}
			// Normalize the execution key once, here at ingest, instead of on
			// every intra-group comparison (design.md §3.1).
			key := strings.TrimSpace(target.meta.executionKey)
			if key == "" {
				return nil, fmt.Errorf("rich record missing execution_key at row %d", rec.RowNumber)
			}
			// Cross-segment first-claim ownership: the first segment to produce a
			// key owns it; a later, different segment's copy is redirected into
			// the owner's group (preserved from the original linear-scan build).
			ownerCIDR, ok := ownerByExecutionKey[key]
			if !ok {
				ownerCIDR = cidr
				ownerByExecutionKey[key] = cidr
			}
			b := builders[ownerCIDR]
			if b == nil {
				b = newRichGroupBuilder()
				builders[ownerCIDR] = b
			}
			if err := b.mergeTarget(key, target); err != nil {
				return nil, err
			}
		}
	}

	if len(builders) == 0 {
		if hasValidRichInput {
			return make(map[string]cidrGroup), nil
		}
		return nil, fmt.Errorf("no usable input rows")
	}

	groups := make(map[string]cidrGroup, len(builders))
	groupIndex := 0
	for cidr, b := range builders {
		if groupIndex%4096 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		groups[cidr] = cidrGroup{targets: b.targets}
		groupIndex++
	}
	sortRichGroups(groups)
	return groups, nil
}

func deniedRichExecutionKeys(records []input.CIDRRecord) map[string]struct{} {
	var denied map[string]struct{}
	for _, rec := range records {
		if !richRecordDenied(rec) {
			continue
		}
		key := strings.TrimSpace(rec.ExecutionKey)
		if key != "" {
			if denied == nil {
				denied = make(map[string]struct{})
			}
			denied[key] = struct{}{}
		}
	}
	return denied
}

func richRecordDenied(rec input.CIDRRecord) bool {
	return rec.IsRich && rec.IsValid && strings.EqualFold(strings.TrimSpace(rec.Decision), "deny")
}

func filterAuthorizedRichTargets(targets []scanTarget, denied map[string]struct{}) []scanTarget {
	if len(denied) == 0 {
		return targets
	}
	authorized := make([]scanTarget, 0, len(targets))
	for _, target := range targets {
		if _, denied := denied[strings.TrimSpace(target.meta.executionKey)]; denied {
			continue
		}
		authorized = append(authorized, target)
	}
	return authorized
}

// richGroupBuilder accumulates the targets of a single rich group during
// buildRichGroupsWithPredicate, indexing them by normalized execution key so
// that de-duplication/metadata merges are O(1) amortized instead of a linear
// scan per inserted target (the O(N^2) fix in design.md §3.1). It is a
// build-time helper only; the finished target slice is copied into the public
// cidrGroup once the build completes, so cidrGroup's shape is unchanged.
type richGroupBuilder struct {
	targets []scanTarget
	index   map[string]int // normalized execution key -> index into targets
}

func newRichGroupBuilder() *richGroupBuilder {
	return &richGroupBuilder{index: make(map[string]int)}
}

// mergeTarget appends target as a new entry, or merges its metadata into the
// existing entry that already owns key. key must be the normalized (TrimSpace)
// execution key of target.
func (b *richGroupBuilder) mergeTarget(key string, target scanTarget) error {
	if idx, ok := b.index[key]; ok {
		return mergeRichTargetValues(&b.targets[idx], target)
	}
	b.index[key] = len(b.targets)
	b.targets = append(b.targets, target)
	return nil
}

func richCIDRKey(rec input.CIDRRecord) (string, error) {
	if cidr := strings.TrimSpace(rec.DstNetworkSegment); cidr != "" {
		return cidr, nil
	}
	if cidr := strings.TrimSpace(rec.CIDR); cidr != "" {
		return cidr, nil
	}
	if rec.Net != nil {
		return rec.Net.String(), nil
	}
	return "", fmt.Errorf("rich record missing dst_network_segment at row %d", rec.RowNumber)
}

func richTargetsFromRecord(rec input.CIDRRecord) ([]scanTarget, error) {
	return richTargetsFromRecordContext(context.Background(), rec)
}

func richTargetsFromRecordContext(ctx context.Context, rec input.CIDRRecord) ([]scanTarget, error) {
	key := strings.TrimSpace(rec.ExecutionKey)
	if key == "" {
		return nil, fmt.Errorf("rich record missing execution_key at row %d", rec.RowNumber)
	}
	cidr, err := richCIDRKey(rec)
	if err != nil {
		return nil, err
	}
	ips, err := richTargetIPsContext(ctx, rec)
	if err != nil {
		return nil, err
	}
	// Exclude the broadcast address of the destination network segment, whether
	// it came from expanding the segment or from an explicit dst_ip.
	if _, seg, perr := net.ParseCIDR(cidr); perr == nil {
		ips = task.FilterBoundaryBroadcast(ips, seg)
	}
	targets := make([]scanTarget, 0, len(ips))
	for i, ip := range ips {
		if i%4096 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		executionKey := key
		if strings.TrimSpace(ip) != strings.TrimSpace(rec.DstIP) {
			executionKey, err = task.BuildExecutionKey(ip, rec.Port, richProtocol(rec))
			if err != nil {
				return nil, fmt.Errorf("build execution key for row %d failed: %w", rec.RowNumber, err)
			}
		}
		targets = append(targets, scanTarget{
			ip:     ip,
			ipCidr: cidr,
			ipU32:  ipv4ToUint32(ip),
			port:   rec.Port,
			meta: targetMeta{
				fabName:           rec.FabName,
				cidrName:          rec.CIDRName,
				serviceLabel:      rec.ServiceLabel,
				decision:          rec.Decision,
				policyID:          rec.PolicyID,
				reason:            rec.Reason,
				executionKey:      executionKey,
				srcIP:             rec.SrcIP,
				srcNetworkSegment: rec.SrcNetworkSegment,
			},
		})
	}
	return targets, nil
}

const (
	reasonPrecheckAllowAll  = "PRECHECK_ALLOW_ALL"
	reasonMatchPolicyAccept = "MATCH_POLICY_ACCEPT"
)

func richTargetIPs(rec input.CIDRRecord) ([]string, error) {
	return richTargetIPsContext(context.Background(), rec)
}

func richTargetIPsContext(ctx context.Context, rec input.CIDRRecord) ([]string, error) {
	reason := strings.TrimSpace(rec.Reason)
	switch {
	case strings.EqualFold(reason, reasonPrecheckAllowAll):
		cidr, err := richCIDRKey(rec)
		if err != nil {
			return nil, err
		}
		ips, err := task.ExpandIPSelectorsContext(ctx, []string{cidr})
		if err != nil {
			return nil, fmt.Errorf("expand selector failed for cidr %s: %w", cidr, err)
		}
		return ips, nil
	case strings.EqualFold(reason, reasonMatchPolicyAccept):
		dstIP := strings.TrimSpace(rec.DstIP)
		if dstIP == "" {
			return nil, fmt.Errorf("rich record missing dst_ip at row %d", rec.RowNumber)
		}
		return []string{dstIP}, nil
	default:
		dstIP := strings.TrimSpace(rec.DstIP)
		if dstIP == "" {
			return nil, fmt.Errorf("rich record missing dst_ip at row %d", rec.RowNumber)
		}
		return []string{dstIP}, nil
	}
}

func richProtocol(rec input.CIDRRecord) string {
	protocol := strings.ToLower(strings.TrimSpace(rec.Protocol))
	if protocol == "" {
		return "tcp"
	}
	return protocol
}

func mergeRichMetadataFromRecord(target *scanTarget, rec input.CIDRRecord) {
	target.meta.fabName = mergeFieldValue(target.meta.fabName, rec.FabName)
	target.meta.cidrName = mergeFieldValue(target.meta.cidrName, rec.CIDRName)
	target.meta.serviceLabel = mergeFieldValue(target.meta.serviceLabel, rec.ServiceLabel)
	target.meta.decision = mergeFieldValue(target.meta.decision, rec.Decision)
	target.meta.policyID = mergeFieldValue(target.meta.policyID, rec.PolicyID)
	target.meta.reason = mergeFieldValue(target.meta.reason, rec.Reason)
	target.meta.srcIP = mergeFieldValue(target.meta.srcIP, rec.SrcIP)
	target.meta.srcNetworkSegment = mergeFieldValue(target.meta.srcNetworkSegment, rec.SrcNetworkSegment)
}

// richTargetIndexByExecutionKey is an O(N) linear scan retained ONLY for the
// groupBuildStrategy path (richGroupStrategy.MergeGroup), which is exercised by
// tests, not production. The production rich build (buildRichGroupsWithPredicate)
// de-duplicates via a per-group execution-key map (richGroupBuilder) and does not
// call this. Do not route large rich input through buildGroups(richGroupStrategy{}):
// it would fall back to the O(N^2) merge this function makes.
func richTargetIndexByExecutionKey(targets []scanTarget, executionKey string) int {
	for i := range targets {
		if strings.TrimSpace(targets[i].meta.executionKey) == executionKey {
			return i
		}
	}
	return -1
}

func mergeRichTargetValues(dst *scanTarget, incoming scanTarget) error {
	key := strings.TrimSpace(dst.meta.executionKey)
	if key == "" {
		return fmt.Errorf("destination rich target missing execution key")
	}
	if strings.TrimSpace(incoming.meta.executionKey) != key {
		return fmt.Errorf("cannot merge rich targets with different execution keys: %s vs %s", key, strings.TrimSpace(incoming.meta.executionKey))
	}
	if dst.port != incoming.port {
		return fmt.Errorf("execution key %s has inconsistent port", key)
	}
	mergeRichMetadataFromRecord(dst, input.CIDRRecord{
		FabName:           incoming.meta.fabName,
		CIDRName:          incoming.meta.cidrName,
		ServiceLabel:      incoming.meta.serviceLabel,
		Decision:          incoming.meta.decision,
		PolicyID:          incoming.meta.policyID,
		Reason:            incoming.meta.reason,
		SrcIP:             incoming.meta.srcIP,
		SrcNetworkSegment: incoming.meta.srcNetworkSegment,
	})
	return nil
}

func normalizeReachablePredicate(reachable func(string) bool) func(string) bool {
	if reachable == nil {
		return func(string) bool { return true }
	}
	return reachable
}

func filterScanTargets(targets []scanTarget, reachable func(string) bool) []scanTarget {
	filtered, _ := filterScanTargetsContext(context.Background(), targets, reachable)
	return filtered
}

func filterScanTargetsContext(ctx context.Context, targets []scanTarget, reachable func(string) bool) ([]scanTarget, error) {
	filtered := make([]scanTarget, 0, len(targets))
	for i, target := range targets {
		if i%4096 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		if reachable(target.ip) {
			filtered = append(filtered, target)
		}
	}
	return filtered, nil
}

func sortRichGroups(groups map[string]cidrGroup) {
	for cidr, group := range groups {
		sort.Slice(group.targets, func(i, j int) bool {
			left := group.targets[i]
			right := group.targets[j]
			if left.ipU32 != right.ipU32 {
				return left.ipU32 < right.ipU32
			}
			if left.port != right.port {
				return left.port < right.port
			}
			return strings.TrimSpace(left.meta.executionKey) < strings.TrimSpace(right.meta.executionKey)
		})
		groups[cidr] = group
	}
}

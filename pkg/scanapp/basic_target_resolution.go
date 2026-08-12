package scanapp

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/xuxiping/port-scan-mk3/pkg/input"
	"github.com/xuxiping/port-scan-mk3/pkg/task"
)

type basicTargetResolution struct {
	groups map[string]cidrGroup
}

type basicResolvedIP struct {
	target   scanTarget
	ports    []int
	portSeen map[int]struct{}
}

type basicGroupStrategy struct{}

func (basicGroupStrategy) ShouldInclude(_ input.CIDRRecord) bool { return true }

func (basicGroupStrategy) Key(record input.CIDRRecord) (string, error) {
	cidr := record.CIDR
	if cidr == "" && record.Net != nil {
		cidr = record.Net.String()
	}
	if cidr == "" {
		return "", fmt.Errorf("record missing ip_cidr")
	}
	return cidr, nil
}

func (strategy basicGroupStrategy) NewGroup(record input.CIDRRecord) (cidrGroup, error) {
	targets, err := strategy.targets(record)
	if err != nil {
		return cidrGroup{}, fmt.Errorf("resolve basic group targets: %w", err)
	}
	return cidrGroup{targets: targets}, nil
}

func (strategy basicGroupStrategy) MergeGroup(existing cidrGroup, record input.CIDRRecord) (cidrGroup, error) {
	targets, err := strategy.targets(record)
	if err != nil {
		return cidrGroup{}, fmt.Errorf("merge basic group targets: %w", err)
	}
	existing.targets = append(existing.targets, targets...)
	return existing, nil
}

func (basicGroupStrategy) RequireNonEmpty() bool { return false }

func (basicGroupStrategy) targets(record input.CIDRRecord) ([]scanTarget, error) {
	targets, err := (basicGroupStrategy{}).targetsContext(context.Background(), record)
	if err != nil {
		return nil, fmt.Errorf("resolve basic record targets: %w", err)
	}
	return targets, nil
}

func (basicGroupStrategy) targetsContext(ctx context.Context, record input.CIDRRecord) ([]scanTarget, error) {
	cidr, ips, err := basicTargetIPsContext(ctx, record)
	if err != nil {
		return nil, err
	}
	targets := make([]scanTarget, 0, len(ips))
	for i, ip := range ips {
		if i%4096 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, fmt.Errorf("build basic scan targets: %w", err)
			}
		}
		targets = append(targets, scanTarget{
			ip:     ip,
			ipCidr: cidr,
			ipU32:  ipv4ToUint32(ip),
			meta: targetMeta{
				fabName:  record.FabName,
				cidrName: record.CIDRName,
			},
		})
	}
	return targets, nil
}

func basicTargetIPsContext(ctx context.Context, record input.CIDRRecord) (string, []string, error) {
	cidr := record.CIDR
	if cidr == "" && record.Net != nil {
		cidr = record.Net.String()
	}

	selector := ""
	switch {
	case record.Selector != nil:
		selector = record.Selector.String()
	case strings.TrimSpace(record.IPRaw) != "":
		selector = strings.TrimSpace(record.IPRaw)
	case record.Net != nil:
		selector = record.Net.String()
	default:
		return "", nil, fmt.Errorf("record for cidr %s missing selector", cidr)
	}

	ips, err := task.ExpandIPSelectorsContextWithLimits(ctx, []string{selector}, task.ExpansionLimits{})
	if err != nil {
		return "", nil, fmt.Errorf("expand selector failed for cidr %s: %w", cidr, err)
	}
	ips = task.FilterBoundaryBroadcast(ips, record.Net)
	return cidr, ips, nil
}

func resolveBasicTargetsContext(ctx context.Context, records []input.CIDRRecord, fallback []input.PortSpec, reachable func(string) bool) (basicTargetResolution, error) {
	fallbackPorts := make([]int, 0, len(fallback))
	fallbackSeen := make(map[int]struct{}, len(fallback))
	for _, port := range fallback {
		if _, exists := fallbackSeen[port.Number]; exists {
			continue
		}
		fallbackSeen[port.Number] = struct{}{}
		fallbackPorts = append(fallbackPorts, port.Number)
	}
	if !hasBasicRowPorts(records) && len(fallbackPorts) > 0 {
		resolution, err := resolveBasicFallbackTargetsContext(ctx, records, fallbackPorts, reachable)
		if err != nil {
			return basicTargetResolution{}, fmt.Errorf("resolve basic fallback targets: %w", err)
		}
		return resolution, nil
	}

	resolvedByCIDR := make(map[string]map[string]*basicResolvedIP)
	seenTasks := make(map[string]struct{})
	predicate := normalizeReachablePredicate(reachable)
	strategy := basicGroupStrategy{}
	for i, record := range records {
		if i%4096 == 0 {
			if err := ctx.Err(); err != nil {
				return basicTargetResolution{}, fmt.Errorf("resolve basic targets: %w", err)
			}
		}
		cidr, err := strategy.Key(record)
		if err != nil {
			return basicTargetResolution{}, fmt.Errorf("resolve basic target CIDR: %w", err)
		}
		targets, err := strategy.targetsContext(ctx, record)
		if err != nil {
			return basicTargetResolution{}, fmt.Errorf("expand basic target selector: %w", err)
		}
		targets, err = filterScanTargetsContext(ctx, targets, predicate)
		if err != nil {
			return basicTargetResolution{}, fmt.Errorf("filter basic targets: %w", err)
		}
		if len(targets) == 0 {
			continue
		}

		ports := fallbackPorts
		if record.Port > 0 {
			ports = []int{record.Port}
		}
		if len(ports) == 0 {
			return basicTargetResolution{}, fmt.Errorf("basic row %d has no port source; set its port or provide -port-file (-port-file is required for blank basic row ports)", record.RowNumber)
		}

		for _, target := range targets {
			for _, port := range ports {
				executionKey, err := task.BuildExecutionKey(target.ip, port, "tcp")
				if err != nil {
					return basicTargetResolution{}, fmt.Errorf("build basic execution key: %w", err)
				}
				if _, exists := seenTasks[executionKey]; exists {
					continue
				}
				seenTasks[executionKey] = struct{}{}
				byIP := resolvedByCIDR[cidr]
				if byIP == nil {
					byIP = make(map[string]*basicResolvedIP)
					resolvedByCIDR[cidr] = byIP
				}
				resolved := byIP[target.ip]
				if resolved == nil {
					resolved = &basicResolvedIP{target: target, portSeen: make(map[int]struct{})}
					byIP[target.ip] = resolved
				}
				if _, exists := resolved.portSeen[port]; !exists {
					resolved.portSeen[port] = struct{}{}
					resolved.ports = append(resolved.ports, port)
				}
			}
		}
	}

	groups := make(map[string]cidrGroup)
	for cidr, byIP := range resolvedByCIDR {
		for _, resolved := range byIP {
			portNumbers := resolved.ports
			portRows := formatPortRows(portNumbers)
			key := basicResolutionGroupKey(cidr, portNumbers)
			group := groups[key]
			group.cidr = cidr
			group.ports = portRows
			group.targets = append(group.targets, resolved.target)
			groups[key] = group
		}
	}
	for key, group := range groups {
		sort.Slice(group.targets, func(i, j int) bool {
			return group.targets[i].ipU32 < group.targets[j].ipU32
		})
		group.totalCount = len(group.targets) * len(group.ports)
		groups[key] = group
	}
	return basicTargetResolution{groups: groups}, nil
}

func resolveBasicFallbackTargetsContext(ctx context.Context, records []input.CIDRRecord, fallbackPorts []int, reachable func(string) bool) (basicTargetResolution, error) {
	cidrGroups, err := buildBasicFallbackGroupsContext(ctx, records, reachable)
	if err != nil {
		return basicTargetResolution{}, fmt.Errorf("build fallback CIDR groups: %w", err)
	}
	cidrs := make([]string, 0, len(cidrGroups))
	for cidr := range cidrGroups {
		cidrs = append(cidrs, cidr)
	}
	sort.Strings(cidrs)

	groups := make(map[string]cidrGroup, len(cidrGroups))
	seenIPs := make(map[uint32]struct{})
	for _, cidr := range cidrs {
		group := cidrGroups[cidr]
		targets := group.basicTargets[:0]
		for _, target := range group.basicTargets {
			if _, exists := seenIPs[target.ipU32]; exists {
				continue
			}
			seenIPs[target.ipU32] = struct{}{}
			targets = append(targets, target)
		}
		if len(targets) == 0 {
			continue
		}
		group.cidr = cidr
		group.ports = formatPortRows(fallbackPorts)
		group.basicTargets = targets
		group.totalCount = len(targets) * len(fallbackPorts)
		groups[basicResolutionGroupKey(cidr, fallbackPorts)] = group
	}
	return basicTargetResolution{groups: groups}, nil
}

func buildBasicFallbackGroupsContext(ctx context.Context, records []input.CIDRRecord, reachable func(string) bool) (map[string]cidrGroup, error) {
	strategy := basicGroupStrategy{}
	predicate := normalizeReachablePredicate(reachable)
	groups := make(map[string]cidrGroup)
	for index, record := range records {
		if index%4096 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		cidr, err := strategy.Key(record)
		if err != nil {
			return nil, err
		}
		_, ips, err := basicTargetIPsContext(ctx, record)
		if err != nil {
			return nil, err
		}
		meta := &targetMeta{fabName: record.FabName, cidrName: record.CIDRName}
		targets := make([]basicScanTarget, 0, len(ips))
		for ipIndex, ip := range ips {
			if ipIndex%4096 == 0 {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
			}
			targets = append(targets, basicScanTarget{ip: ip, ipU32: ipv4ToUint32(ip), meta: meta})
		}
		targets, err = filterBasicFallbackTargetsContext(ctx, targets, predicate)
		if err != nil {
			return nil, err
		}
		if len(targets) == 0 {
			continue
		}
		group, exists := groups[cidr]
		if !exists {
			group.basicTargets = targets
		} else {
			group.basicTargets = append(group.basicTargets, targets...)
		}
		groups[cidr] = group
	}
	for cidr, group := range groups {
		sort.Slice(group.basicTargets, func(i, j int) bool {
			return group.basicTargets[i].ipU32 < group.basicTargets[j].ipU32
		})
		groups[cidr] = group
	}
	return groups, nil
}

func filterBasicFallbackTargetsContext(ctx context.Context, targets []basicScanTarget, reachable func(string) bool) ([]basicScanTarget, error) {
	kept := targets[:0]
	for index := range targets {
		if index%4096 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		if reachable(targets[index].ip) {
			kept = append(kept, targets[index])
		}
	}
	return kept, nil
}

func (resolution basicTargetResolution) groupForChunk(chunk task.Chunk) (cidrGroup, error) {
	ports, err := parsePortRows(chunk.Ports)
	if err != nil {
		return cidrGroup{}, fmt.Errorf("parse resume chunk ports: %w", err)
	}
	if len(ports) == 0 {
		return cidrGroup{}, fmt.Errorf("resume state for %s has no ports", chunk.CIDR)
	}
	group, ok := resolution.groups[basicResolutionGroupKey(chunk.CIDR, ports)]
	if !ok {
		return cidrGroup{}, fmt.Errorf("resume state references %s ports %v, which have no scannable targets in the current input; start a fresh scan or run generate-buckets to create a new snapshot", chunk.CIDR, chunk.Ports)
	}
	return group, nil
}

func basicResolutionGroupKey(cidr string, ports []int) string {
	var builder strings.Builder
	builder.WriteString(cidr)
	builder.WriteByte(0)
	for _, port := range ports {
		builder.WriteString(strconv.Itoa(port))
		builder.WriteByte(',')
	}
	return builder.String()
}

func formatPortRows(ports []int) []string {
	rows := make([]string, 0, len(ports))
	for _, port := range ports {
		rows = append(rows, fmt.Sprintf("%d/tcp", port))
	}
	return rows
}

func inputPortSpecsFromRows(rows []string) ([]input.PortSpec, error) {
	ports, err := parsePortRows(rows)
	if err != nil {
		return nil, fmt.Errorf("parse snapshot basic port fallback: %w", err)
	}
	specs := make([]input.PortSpec, 0, len(ports))
	for i, port := range ports {
		specs = append(specs, input.PortSpec{Number: port, Proto: "tcp", Raw: rows[i]})
	}
	return specs, nil
}

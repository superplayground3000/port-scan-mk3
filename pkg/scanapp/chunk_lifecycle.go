package scanapp

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
	"github.com/xuxiping/port-scan-mk3/pkg/input"
	"github.com/xuxiping/port-scan-mk3/pkg/ratelimit"
	"github.com/xuxiping/port-scan-mk3/pkg/state"
	"github.com/xuxiping/port-scan-mk3/pkg/task"
)

type runtimePolicy struct {
	bucketRate     int
	bucketCapacity int
}

func runtimePolicyFromConfig(cfg config.Config) runtimePolicy {
	return runtimePolicy{
		bucketRate:     cfg.BucketRate,
		bucketCapacity: cfg.BucketCapacity,
	}
}

func shouldSaveOnDispatchErr(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func hasIncomplete(runtimes []*chunkRuntime) bool {
	for _, rt := range runtimes {
		snap := rt.tracker.Snapshot()
		if snap.ScannedCount < snap.TotalCount {
			return true
		}
	}
	return false
}

func collectChunkStates(runtimes []*chunkRuntime) []task.Chunk {
	out := make([]task.Chunk, 0, len(runtimes))
	for _, rt := range runtimes {
		out = append(out, rt.tracker.Snapshot())
	}
	return out
}

func loadOrBuildChunks(cfg config.Config, cidrRecords []input.CIDRRecord, portSpecs []input.PortSpec) ([]task.Chunk, error) {
	return loadOrBuildChunksWithPredicate(cfg, cidrRecords, portSpecs, nil)
}

func loadOrBuildChunksWithPredicate(cfg config.Config, cidrRecords []input.CIDRRecord, portSpecs []input.PortSpec, reachable func(string) bool) ([]task.Chunk, error) {
	if cfg.Resume != "" {
		return state.Load(cfg.Resume)
	}
	if hasRichRecords(cidrRecords) {
		return buildRichChunksWithPredicate(cidrRecords, reachable)
	}
	groups, err := buildCIDRGroupsWithPredicate(cidrRecords, reachable)
	if err != nil {
		return nil, err
	}
	rawPorts := make([]string, 0, len(portSpecs))
	for _, p := range portSpecs {
		rawPorts = append(rawPorts, p.Raw)
	}
	cidrs := make([]string, 0, len(groups))
	for cidr := range groups {
		cidrs = append(cidrs, cidr)
	}
	sort.Strings(cidrs)

	out := make([]task.Chunk, 0, len(cidrs))
	for _, cidr := range cidrs {
		out = append(out, basicChunkFromGroup(cidr, groups[cidr], rawPorts))
	}
	return out, nil
}

// basicChunkFromGroup builds the basic-mode chunk for a single CIDR group. Each
// target is scanned across every rawPort, so TotalCount == len(targets) *
// len(rawPorts). This is the single source of truth for basic-mode counting;
// both fresh scan builds and generate-buckets route through it so the
// total_count invariant (buildRuntimeWithPredicate) holds by construction.
func basicChunkFromGroup(cidr string, g cidrGroup, rawPorts []string) task.Chunk {
	cidrName := ""
	if len(g.targets) > 0 {
		cidrName = g.targets[0].meta.cidrName
	}
	return task.Chunk{
		CIDR:         cidr,
		CIDRName:     cidrName,
		Ports:        rawPorts,
		NextIndex:    0,
		ScannedCount: 0,
		TotalCount:   len(g.targets) * len(rawPorts),
		Status:       "pending",
	}
}

func buildRuntime(chunks []task.Chunk, cidrRecords []input.CIDRRecord, defaultPorts []input.PortSpec, policy runtimePolicy) ([]*chunkRuntime, error) {
	return buildRuntimeWithPredicate(chunks, cidrRecords, defaultPorts, policy, nil)
}

func buildRuntimeWithPredicate(chunks []task.Chunk, cidrRecords []input.CIDRRecord, defaultPorts []input.PortSpec, policy runtimePolicy, reachable func(string) bool) ([]*chunkRuntime, error) {
	var (
		groups map[string]cidrGroup
		err    error
	)
	richMode := hasRichRecords(cidrRecords)
	if richMode {
		groups, err = buildRichGroupsWithPredicate(cidrRecords, reachable)
	} else {
		groups, err = buildCIDRGroupsWithPredicate(cidrRecords, reachable)
	}
	if err != nil {
		return nil, err
	}

	runtimes := make([]*chunkRuntime, 0, len(chunks))
	for i := range chunks {
		ch := &chunks[i]
		group, ok := groups[ch.CIDR]
		if !ok {
			return nil, fmt.Errorf("resume state references %s, which has no scannable targets in the current input (it may have been removed from the CSV, or all of its targets are now excluded such as broadcast addresses); start a fresh scan (remove -resume or delete the resume file)", ch.CIDR)
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
				portRows = make([]string, 0, len(defaultPorts))
				for _, p := range defaultPorts {
					portRows = append(portRows, p.Raw)
				}
			}
			ch.Ports = append(ch.Ports, portRows...)
		}
		ports, err := parsePortRows(portRows)
		if err != nil {
			return nil, err
		}

		expectedTotal := len(group.targets) * len(ports)
		if ch.TotalCount == 0 {
			ch.TotalCount = expectedTotal
		}
		if ch.TotalCount != expectedTotal {
			return nil, fmt.Errorf("resume state for %s is incompatible with the current target set (saved total_count=%d, now expected=%d); this happens when the input CSV changed or after upgrading to a build that excludes broadcast addresses. Start a fresh scan (remove -resume or delete the resume file)", ch.CIDR, ch.TotalCount, expectedTotal)
		}
		if ch.NextIndex >= ch.TotalCount {
			ch.Status = "completed"
		} else if ch.Status == "" {
			ch.Status = "pending"
		}
		rt := &chunkRuntime{
			ipCidr:  ch.CIDR,
			ports:   ports,
			targets: group.targets,
			state:   ch,
			tracker: newChunkStateTracker(ch),
			bkt:     ratelimit.NewLeakyBucket(policy.bucketRate, policy.bucketCapacity),
		}
		runtimes = append(runtimes, rt)
	}
	return runtimes, nil
}

func hasRichRecords(cidrRecords []input.CIDRRecord) bool {
	for _, rec := range cidrRecords {
		if rec.IsRich {
			return true
		}
	}
	return false
}

func buildRichChunks(cidrRecords []input.CIDRRecord) ([]task.Chunk, error) {
	return buildRichChunksWithPredicate(cidrRecords, nil)
}

func buildRichChunksWithPredicate(cidrRecords []input.CIDRRecord, reachable func(string) bool) ([]task.Chunk, error) {
	groups, err := buildRichGroupsWithPredicate(cidrRecords, reachable)
	if err != nil {
		return nil, err
	}
	if len(groups) == 0 {
		return []task.Chunk{}, nil
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]task.Chunk, 0, len(keys))
	for _, key := range keys {
		g := groups[key]
		if len(g.targets) == 0 {
			continue
		}
		out = append(out, richChunkFromGroup(key, g))
	}
	return out, nil
}

// richChunkFromGroup builds the rich-mode chunk for a single CIDR group. Rich
// groups carry one dedicated port per target, so TotalCount == len(targets) and
// Ports holds a single representative "<port>/tcp" entry. This is the single
// source of truth for rich-mode counting; both fresh scan builds and
// generate-buckets route through it so the total_count invariant
// (buildRuntimeWithPredicate) holds by construction.
func richChunkFromGroup(cidr string, g cidrGroup) task.Chunk {
	cidrName := ""
	port := 1
	if len(g.targets) > 0 {
		cidrName = g.targets[0].meta.cidrName
		if g.targets[0].port > 0 {
			port = g.targets[0].port
		}
	}
	return task.Chunk{
		CIDR:         cidr,
		CIDRName:     cidrName,
		Ports:        []string{fmt.Sprintf("%d/tcp", port)},
		NextIndex:    0,
		ScannedCount: 0,
		TotalCount:   len(g.targets),
		Status:       "pending",
	}
}

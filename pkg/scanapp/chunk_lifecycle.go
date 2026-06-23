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
		g := groups[cidr]
		total := len(g.targets) * len(portSpecs)
		cidrName := ""
		if len(g.targets) > 0 {
			cidrName = g.targets[0].meta.cidrName
		}
		out = append(out, task.Chunk{
			CIDR:         cidr,
			CIDRName:     cidrName,
			Ports:        rawPorts,
			NextIndex:    0,
			ScannedCount: 0,
			TotalCount:   total,
			Status:       "pending",
		})
	}
	return out, nil
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
			return nil, fmt.Errorf("cidr %s from chunk not found in cidr file", ch.CIDR)
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
			return nil, fmt.Errorf("chunk total_count mismatch for %s: state=%d expected=%d", ch.CIDR, ch.TotalCount, expectedTotal)
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
		cidrName := ""
		cidrName = g.targets[0].meta.cidrName
		port := g.targets[0].port
		if port <= 0 {
			port = 1
		}
		out = append(out, task.Chunk{
			CIDR:         key,
			CIDRName:     cidrName,
			Ports:        []string{fmt.Sprintf("%d/tcp", port)},
			NextIndex:    0,
			ScannedCount: 0,
			TotalCount:   len(g.targets),
			Status:       "pending",
		})
	}
	return out, nil
}

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

// buildRuntimeWithPredicate re-derives the in-memory runtime plan for a set of
// chunks. On resume the chunk set carries every chunk of the original run, most
// of them already finished; expanding their targets is pure waste. So it:
//
//   - partitions chunks into completed (already fully scanned) and incomplete
//     (still-to-scan). Completed chunks get a lightweight runtime with nil
//     targets and nil bucket — dispatch skips them (task_dispatcher.go:
//     NextIndex >= TotalCount) and never reads their targets, but they stay in
//     the returned slice so persistResumeSnapshot re-saves the finished work
//     (design.md §3.2a); and
//   - expands only the records that feed an incomplete chunk (design.md §3.2b),
//     filtering by group key in one O(rows) pass with no IP expansion, then runs
//     the existing group builder on just those rows.
//
// For the incomplete (still-to-scan) work the output is byte-for-byte identical
// to expanding the whole input: a record contributes only to its own group key,
// so keeping just the incomplete keys yields the same groups for well-formed
// (disjoint-segment) input. The one case filtering changes is the malformed one
// where a filtered-out segment first-claimed an execution key an incomplete
// segment also produces — caught loudly by the retained total_count invariant
// check below (design.md §3.2), not silently.
//
// Intentional, benign behavior change: a completed chunk whose CIDR was removed
// from the CSV no longer errors, because completed chunks are never looked up.
func buildRuntimeWithPredicate(chunks []task.Chunk, cidrRecords []input.CIDRRecord, defaultPorts []input.PortSpec, policy runtimePolicy, reachable func(string) bool) ([]*chunkRuntime, error) {
	incompleteKeys := make(map[string]struct{}, len(chunks))
	allIncompleteHaveTotal := true
	for i := range chunks {
		if chunkIsCompleted(&chunks[i]) {
			continue
		}
		incompleteKeys[chunks[i].CIDR] = struct{}{}
		if chunks[i].TotalCount == 0 {
			allIncompleteHaveTotal = false
		}
	}

	// Filter records down to just the incomplete chunks' rows — the completed-chunk
	// expansion skip — ONLY when every incomplete chunk has a known total_count. The
	// total_count invariant below is what catches a cross-segment execution-key
	// ownership divergence the filter can introduce (a filtered-out segment that
	// first-claimed a key an incomplete segment also produces). With total_count
	// omitted (legacy snapshots decode it to 0) that guard is disabled, so filtering
	// would silently change the incomplete target set; fall back to the whole-input
	// build to reproduce ownership exactly.
	records := cidrRecords
	if len(incompleteKeys) > 0 && allIncompleteHaveTotal {
		records = filterRecordsByChunkKey(cidrRecords, incompleteKeys)
	}
	// Decide richMode on exactly the records the build consumes, so the group
	// builder and the port-defaulting branch below agree on the mode.
	richMode := hasRichRecords(records)

	var (
		groups map[string]cidrGroup
		err    error
	)
	if len(incompleteKeys) > 0 {
		if richMode {
			groups, err = buildRichGroupsWithPredicate(records, reachable)
		} else {
			groups, err = buildCIDRGroupsWithPredicate(records, reachable)
		}
		if err != nil {
			return nil, err
		}
	}

	runtimes := make([]*chunkRuntime, 0, len(chunks))
	for i := range chunks {
		ch := &chunks[i]
		if chunkIsCompleted(ch) {
			// Lightweight runtime: no expansion, no leaky-bucket goroutine. Match
			// the pre-Phase-2 status normalization for a finished chunk.
			ch.Status = "completed"
			runtimes = append(runtimes, &chunkRuntime{
				ipCidr:  ch.CIDR,
				state:   ch,
				tracker: newChunkStateTracker(ch),
			})
			continue
		}

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

// chunkIsCompleted reports whether a chunk is already fully scanned and needs no
// target expansion. It mirrors the dispatcher's skip condition EXACTLY
// (task_dispatcher.go: NextIndex >= TotalCount), so a completed chunk's lightweight
// runtime (nil targets, nil bucket) is never reached by dispatch. Status is not
// trusted on its own: a malformed snapshot with Status=="completed" but
// NextIndex<TotalCount still has work and must take the incomplete path (real
// targets + bucket), otherwise dispatch would nil-deref on rt.bkt.Acquire. The
// TotalCount>0 guard keeps a legacy chunk (total_count omitted -> 0) on the
// incomplete path so NextIndex(0) >= TotalCount(0) is not mistaken for "done".
func chunkIsCompleted(ch *task.Chunk) bool {
	return ch.TotalCount > 0 && ch.NextIndex >= ch.TotalCount
}

// filterRecordsByChunkKey keeps only the records whose group key is in keys, in
// input order, in a single O(rows) pass with no IP expansion. A record with no
// keyable segment/CIDR cannot belong to any chunk (chunk keys are always valid
// CIDR strings), so it is skipped rather than erroring.
func filterRecordsByChunkKey(records []input.CIDRRecord, keys map[string]struct{}) []input.CIDRRecord {
	if len(keys) == 0 {
		return nil
	}
	out := make([]input.CIDRRecord, 0, len(records))
	for _, rec := range records {
		key, err := chunkKeyForRecord(rec)
		if err != nil {
			continue
		}
		if _, ok := keys[key]; ok {
			out = append(out, rec)
		}
	}
	return out
}

// chunkKeyForRecord returns the group key a record contributes to: the rich
// segment key for rich records, the CIDR for basic records. It mirrors the keys
// the group builders (buildRichGroupsWithPredicate / buildCIDRGroupsWithPredicate)
// use, so filtering by it keeps exactly the rows a chunk needs.
func chunkKeyForRecord(rec input.CIDRRecord) (string, error) {
	if rec.IsRich {
		return richCIDRKey(rec)
	}
	return basicGroupStrategy{}.Key(rec)
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

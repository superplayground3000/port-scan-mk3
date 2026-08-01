package scanapp

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/input"
	"github.com/xuxiping/port-scan-mk3/pkg/writer"
)

func reachablePredicate(sortedUnreachable []uint32) func(string) bool {
	blocked := sortedUniqueIPv4U32(sortedUnreachable)
	return func(ip string) bool {
		parsed := net.ParseIP(strings.TrimSpace(ip))
		v4 := parsed.To4()
		if parsed == nil || v4 == nil {
			return true
		}
		// Derive the uint32 directly from the already-parsed IPv4 bytes instead
		// of stringifying and re-parsing (the redundant second net.ParseIP that
		// Phase 0 profiling flagged as the top CPU frame — design.md §3.3).
		ipv4 := binary.BigEndian.Uint32(v4)
		idx := sort.Search(len(blocked), func(i int) bool {
			return blocked[i] >= ipv4
		})
		return idx >= len(blocked) || blocked[idx] != ipv4
	}
}

func sortedUniqueIPv4U32(values []uint32) []uint32 {
	if len(values) == 0 {
		return nil
	}
	uniq := make(map[uint32]struct{}, len(values))
	for _, value := range values {
		uniq[value] = struct{}{}
	}
	out := make([]uint32, 0, len(uniq))
	for value := range uniq {
		out = append(out, value)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func collectUniquePreScanIPs(inputs runInputs) ([]string, error) {
	uniq := make(map[uint32]string)
	for _, rec := range inputs.cidrRecords {
		targets, err := preScanTargetsFromRecord(rec)
		if err != nil {
			return nil, err
		}
		for _, target := range targets {
			uniq[ipv4ToUint32(target.ip)] = target.ip
		}
	}

	keys := make([]uint32, 0, len(uniq))
	for key := range uniq {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })

	ips := make([]string, 0, len(keys))
	for _, key := range keys {
		ips = append(ips, uniq[key])
	}
	return ips, nil
}

// runReachabilityChecksWithProgress pings every IP concurrently over a worker
// pool and returns the sorted set of unreachable IPs (as uint32). onChecked,
// when non-nil, is invoked exactly once per IP after its reachability check
// returns (reachable, unreachable, or errored), so callers can tick a progress
// reporter per checked IP. It is called from worker goroutines, so onChecked
// must be safe for concurrent use (progress.Reporter is). Passing nil skips
// progress reporting. RunPreping is the sole production caller.
func runReachabilityChecksWithProgress(ctx context.Context, checker ReachabilityChecker, ips []string, workers int, timeout time.Duration, onChecked func()) ([]uint32, error) {
	if len(ips) == 0 {
		return nil, nil
	}
	if checker == nil {
		return nil, fmt.Errorf("reachability checker is required")
	}
	if workers <= 0 {
		workers = 1
	}
	if workers > len(ips) {
		workers = len(ips)
	}

	type workerResult struct {
		ipv4        uint32
		unreachable bool
		err         error
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobs := make(chan string)
	results := make(chan workerResult, len(ips))

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-runCtx.Done():
					return
				case ip, ok := <-jobs:
					if !ok {
						return
					}

					result, err := checkReachability(runCtx, checker, ip, timeout)
					if onChecked != nil {
						onChecked()
					}
					if err != nil {
						select {
						case results <- workerResult{err: err}:
						case <-runCtx.Done():
						}
						cancel()
						return
					}
					if !result.Reachable {
						select {
						case results <- workerResult{ipv4: ipv4ToUint32(ip), unreachable: true}:
						case <-runCtx.Done():
							return
						}
					}
				}
			}
		}()
	}

	go func() {
		defer close(jobs)
		for _, ip := range ips {
			select {
			case <-runCtx.Done():
				return
			case jobs <- ip:
			}
		}
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	unreachable := make([]uint32, 0, len(ips))
	var fatalErr error
	for result := range results {
		if result.err != nil && fatalErr == nil {
			fatalErr = result.err
			continue
		}
		if result.unreachable {
			unreachable = append(unreachable, result.ipv4)
		}
	}
	if fatalErr != nil {
		return nil, fatalErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sort.Slice(unreachable, func(i, j int) bool { return unreachable[i] < unreachable[j] })
	return unreachable, nil
}

func collectUnreachableRows(inputs runInputs, reachable func(string) bool, reason string) ([]writer.UnreachableRecord, error) {
	predicate := normalizeReachablePredicate(reachable)
	rows := make([]writer.UnreachableRecord, 0)
	richOrder := make([]string, 0)
	richRows := make(map[string]writer.UnreachableRecord)
	for _, rec := range inputs.cidrRecords {
		targets, err := preScanTargetsFromRecord(rec)
		if err != nil {
			return nil, err
		}
		for _, target := range targets {
			if predicate(target.ip) {
				continue
			}
			row := writer.UnreachableRecord{
				IP:                target.ip,
				IPCidr:            target.ipCidr,
				Status:            "unreachable",
				Reason:            reason,
				FabName:           target.meta.fabName,
				CIDRName:          target.meta.cidrName,
				ServiceLabel:      target.meta.serviceLabel,
				Decision:          target.meta.decision,
				PolicyID:          target.meta.policyID,
				ExecutionKey:      target.meta.executionKey,
				SrcIP:             target.meta.srcIP,
				SrcNetworkSegment: target.meta.srcNetworkSegment,
			}
			if !rec.IsRich {
				rows = append(rows, row)
				continue
			}

			key := richUnreachableRowKey(row)
			existing, ok := richRows[key]
			if !ok {
				richRows[key] = row
				richOrder = append(richOrder, key)
				continue
			}
			richRows[key] = mergeUnreachableRecord(existing, row)
		}
	}
	for _, key := range richOrder {
		rows = append(rows, richRows[key])
	}
	return rows, nil
}

func preScanTargetsFromRecord(rec input.CIDRRecord) ([]scanTarget, error) {
	if rec.IsRich {
		if !rec.IsValid {
			return nil, nil
		}
		return richTargetsFromRecord(rec)
	}

	strategy := basicGroupStrategy{}
	if _, err := strategy.Key(rec); err != nil {
		return nil, err
	}
	return strategy.targets(rec)
}

func richUnreachableRowKey(row writer.UnreachableRecord) string {
	return row.IP + "\x00" + row.IPCidr
}

func mergeUnreachableRecord(existing, incoming writer.UnreachableRecord) writer.UnreachableRecord {
	existing.FabName = mergeFieldValue(existing.FabName, incoming.FabName)
	existing.CIDRName = mergeFieldValue(existing.CIDRName, incoming.CIDRName)
	existing.ServiceLabel = mergeFieldValue(existing.ServiceLabel, incoming.ServiceLabel)
	existing.Decision = mergeFieldValue(existing.Decision, incoming.Decision)
	existing.PolicyID = mergeFieldValue(existing.PolicyID, incoming.PolicyID)
	existing.ExecutionKey = mergeFieldValue(existing.ExecutionKey, incoming.ExecutionKey)
	existing.SrcIP = mergeFieldValue(existing.SrcIP, incoming.SrcIP)
	existing.SrcNetworkSegment = mergeFieldValue(existing.SrcNetworkSegment, incoming.SrcNetworkSegment)
	return existing
}

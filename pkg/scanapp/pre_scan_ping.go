package scanapp

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
	"github.com/xuxiping/port-scan-mk3/pkg/input"
	"github.com/xuxiping/port-scan-mk3/pkg/state"
	"github.com/xuxiping/port-scan-mk3/pkg/writer"
)

type preScanOutcome struct {
	State              state.PreScanPingState
	UnreachableIPv4U32 []uint32
	UnreachableRows    []writer.UnreachableRecord
}

const (
	preScanPingTimeout = 100 * time.Millisecond
	preScanPingReason  = "ping failed within 100ms"
)

func runPreScanPing(ctx context.Context, inputs runInputs, cfg config.Config, checker ReachabilityChecker, saved state.PreScanPingState) (preScanOutcome, error) {
	if cfg.DisablePreScanPing {
		return preScanOutcome{}, nil
	}
	if err := ctx.Err(); err != nil {
		return preScanOutcome{}, err
	}

	if hasSavedPreScanPingState(saved) {
		unreachable := sortedUniqueIPv4U32(saved.UnreachableIPv4U32)
		rows, err := collectUnreachableRows(inputs, reachablePredicate(unreachable))
		if err != nil {
			return preScanOutcome{}, err
		}
		if err := ctx.Err(); err != nil {
			return preScanOutcome{}, err
		}
		return preScanOutcome{
			State:              buildPreScanPingState(unreachable),
			UnreachableIPv4U32: unreachable,
			UnreachableRows:    rows,
		}, nil
	}

	if checker == nil {
		return preScanOutcome{}, fmt.Errorf("reachability checker is required")
	}

	uniqueIPs, err := collectUniquePreScanIPs(inputs)
	if err != nil {
		return preScanOutcome{}, err
	}
	unreachable, err := runReachabilityChecks(ctx, checker, uniqueIPs, cfg.Workers)
	if err != nil {
		return preScanOutcome{}, err
	}
	if err := ctx.Err(); err != nil {
		return preScanOutcome{}, err
	}

	rows, err := collectUnreachableRows(inputs, reachablePredicate(unreachable))
	if err != nil {
		return preScanOutcome{}, err
	}
	if err := ctx.Err(); err != nil {
		return preScanOutcome{}, err
	}

	return preScanOutcome{
		State:              buildPreScanPingState(unreachable),
		UnreachableIPv4U32: unreachable,
		UnreachableRows:    rows,
	}, nil
}

func reachablePredicate(sortedUnreachable []uint32) func(string) bool {
	blocked := sortedUniqueIPv4U32(sortedUnreachable)
	return func(ip string) bool {
		parsed := net.ParseIP(strings.TrimSpace(ip))
		if parsed == nil || parsed.To4() == nil {
			return true
		}
		ipv4 := ipv4ToUint32(parsed.String())
		idx := sort.Search(len(blocked), func(i int) bool {
			return blocked[i] >= ipv4
		})
		return idx >= len(blocked) || blocked[idx] != ipv4
	}
}

func hasSavedPreScanPingState(saved state.PreScanPingState) bool {
	return saved.Enabled || saved.TimeoutMS != 0 || len(saved.UnreachableIPv4U32) > 0
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

func runReachabilityChecks(ctx context.Context, checker ReachabilityChecker, ips []string, workers int) ([]uint32, error) {
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

					result, err := checkReachability(runCtx, checker, ip, preScanPingTimeout)
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

func collectUnreachableRows(inputs runInputs, reachable func(string) bool) ([]writer.UnreachableRecord, error) {
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
				Reason:            preScanPingReason,
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

func buildPreScanPingState(unreachable []uint32) state.PreScanPingState {
	return state.PreScanPingState{
		Enabled:            true,
		TimeoutMS:          int(preScanPingTimeout / time.Millisecond),
		UnreachableIPv4U32: unreachable,
	}
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

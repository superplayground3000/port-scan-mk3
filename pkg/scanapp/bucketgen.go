package scanapp

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"net"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
	"github.com/xuxiping/port-scan-mk3/pkg/progress"
	"github.com/xuxiping/port-scan-mk3/pkg/state"
	"github.com/xuxiping/port-scan-mk3/pkg/task"
)

// GenerateBucketsConfiguration supplies validated values to the bucket workflow.
type GenerateBucketsConfiguration interface {
	Resolve() (config.GenerateBucketsValues, error)
}

// GenerateBucketsOptions customizes bucket generation. All fields are optional.
type GenerateBucketsOptions struct {
	// Reporter receives one tick for each completed CIDR group, and a final
	// Done() summary. If Reporter is nil, GenerateBuckets uses a stderr
	// reporter over the group count.
	Reporter progress.Reporter
}

// GenerateBuckets builds a resume Snapshot for the "generate-buckets" step.
//
// GenerateBuckets resolves its configuration before it reads any files.
// It reads targets and optional ports. It removes targets from the optional
// blocklist. It builds one task.Chunk for each reachable CIDR group.
// The snapshot records that pre-ping is enabled with no timeout value.
//
// The conversion from group to chunk uses the configured worker count.
// Each goroutine writes into its own pre-indexed result slot, so the writes are
// race-free. GenerateBuckets then sorts the chunks by CIDR before
// serialization, so the output is byte-identical for every worker count. CSV
// parsing is sequential and not parallel.
//
// The chunk TotalCount comes from the same builders that scan uses
// (buildRichGroupsWithPredicate / buildCIDRGroupsWithPredicate plus
// richChunkFromGroup / basicChunkFromGroup). The total_count assertion in the
// buildRuntimeWithPredicate function of scan therefore accepts the snapshot
// unchanged.
//
// # Returns
//
//	nil on success. GenerateBuckets returns an error if configuration resolution
//	fails. It also returns an error for invalid input, cancellation, grouping,
//	or snapshot output failures.
func GenerateBuckets(ctx context.Context, configuration GenerateBucketsConfiguration, stderr io.Writer, opts GenerateBucketsOptions) error {
	values, err := configuration.Resolve()
	if err != nil {
		return fmt.Errorf("resolve bucket configuration: %w", err)
	}
	expansion, err := resolveTargetExpansion(configuration)
	if err != nil {
		return err
	}
	resourceLimits, err := resolveGenerateBucketsLimits(configuration)
	if err != nil {
		return err
	}
	if strings.TrimSpace(values.SnapshotOutput) == "" {
		return fmt.Errorf("generate-buckets requires -buckets-out")
	}
	inputs, err := loadRunInputsContext(ctx, inputConfiguration{
		cidrFile:         values.CIDRFile,
		cidrIPCol:        values.CIDRIPCol,
		cidrIPCidrCol:    values.CIDRIPCidrCol,
		portFile:         values.PortFile,
		allowMissingPort: true,
		cidrLimits:       resourceLimits.CIDR,
		portLimits:       resourceLimits.Port,
	}, defaultRunDependencies())
	if err != nil {
		return fmt.Errorf("generate-buckets: load inputs: %w", err)
	}
	expansionEstimate, err := task.EstimateAuthorizedCIDRRecords(inputs.cidrRecords, expansion.Limits, nil)
	if err != nil {
		return fmt.Errorf("generate-buckets: estimate target expansion: %w", err)
	}

	blocklist, err := parseUnreachableBlocklist(values.BlocklistFile)
	if err != nil {
		return fmt.Errorf("generate-buckets: parse blocklist: %w", err)
	}
	reachable := reachablePredicate(blocklist)

	// Group sequentially through the same builders scan uses, so counting is
	// single-sourced and the total_count invariant holds by construction.
	richMode := hasRichRecords(inputs.cidrRecords)
	var groups map[string]cidrGroup
	if richMode {
		groups, err = buildRichGroupsWithPredicate(inputs.cidrRecords, reachable)
	} else {
		resolution, resolveErr := resolveBasicTargetsContext(ctx, inputs.cidrRecords, inputs.portSpecs, reachable)
		if resolveErr != nil {
			err = resolveErr
		} else {
			groups = resolution.groups
		}
	}
	if err != nil {
		return fmt.Errorf("generate-buckets: build CIDR groups: %w", err)
	}

	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	reporter := opts.Reporter
	if reporter == nil {
		reporter = progress.New("generate-buckets", len(keys), values.ProgressInterval, stderr)
	}

	chunks := make([]task.Chunk, len(keys))
	if err := fanOutGroupChunks(ctx, keys, groups, richMode, values.Workers, reporter, chunks); err != nil {
		return fmt.Errorf("generate-buckets: build chunks: %w", err)
	}
	reporter.Done()

	// Deterministic CIDR-sorted output. Indexing already preserves the sorted
	// key order; sorting again is a defensive guarantee independent of the
	// worker pool's completion order.
	sort.Slice(chunks, func(i, j int) bool { return chunks[i].CIDR < chunks[j].CIDR })

	snap := state.Snapshot{
		Chunks:                 chunks,
		RichDenyExcluded:       true,
		TargetSemanticsVersion: state.CurrentTargetSemanticsVersion,
		TargetExpansion: &state.TargetExpansionState{
			CandidateCount: expansionEstimate.CandidateCount,
			CandidateLimit: int64(expansion.Limits.CandidateLimit()),
			MemoryLimitGB:  int64(expansion.Limits.MemoryLimitGB()),
		},
		PreScanPing: state.PreScanPingState{
			Enabled:            true,
			TimeoutMS:          0,
			UnreachableIPv4U32: blocklist,
		},
	}
	if !richMode {
		snap.BasicPortFallback = make([]string, 0, len(inputs.portSpecs))
		for _, port := range inputs.portSpecs {
			snap.BasicPortFallback = append(snap.BasicPortFallback, port.Raw)
		}
	}
	if err := state.SaveSnapshotWithLimits(values.SnapshotOutput, snap, resourceLimits.Snapshot); err != nil {
		return fmt.Errorf("generate-buckets: write snapshot %s: %w", values.SnapshotOutput, err)
	}
	return nil
}

// fanOutGroupChunks converts each CIDR group into a task.Chunk across a pool of
// workers. Worker w reads group indices from a channel and writes only out[i]
// for the index it received; because every index is dispatched exactly once,
// distinct workers touch distinct slice elements and the write is race-free. A
// tick is reported per completed group. It honors ctx cancellation.
func fanOutGroupChunks(
	ctx context.Context,
	keys []string,
	groups map[string]cidrGroup,
	richMode bool,
	workers int,
	reporter progress.Reporter,
	out []task.Chunk,
) error {
	if len(keys) == 0 {
		return ctx.Err()
	}
	if workers <= 0 {
		workers = 1
	}
	if workers > len(keys) {
		workers = len(keys)
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	indexCh := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-runCtx.Done():
					return
				case i, ok := <-indexCh:
					if !ok {
						return
					}
					key := keys[i]
					if richMode {
						out[i] = richChunkFromGroup(key, groups[key])
					} else {
						out[i] = basicChunkFromGroup(groups[key])
					}
					reporter.Inc()
				}
			}
		}()
	}

	go func() {
		defer close(indexCh)
		for i := range keys {
			select {
			case <-runCtx.Done():
				return
			case indexCh <- i:
			}
		}
	}()

	wg.Wait()
	return ctx.Err()
}

// parseUnreachableBlocklist reads the "ip" column of an unreachable CSV
// (pkg/writer.UnreachableWriter schema) and returns the sorted, de-duplicated
// IPv4 addresses as uint32 values, matching the representation scan uses for
// reachablePredicate. A missing or empty path (empty string, a file that does
// not exist, or a header-only file) yields an empty slice with no error.
func parseUnreachableBlocklist(path string) ([]uint32, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open unreachable blocklist %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	reader := csv.NewReader(f)
	reader.FieldsPerRecord = -1

	header, err := reader.Read()
	if err != nil {
		if err == io.EOF {
			return nil, nil
		}
		return nil, fmt.Errorf("read unreachable blocklist %s header: %w", path, err)
	}
	ipIdx := -1
	for i, name := range header {
		if strings.EqualFold(strings.TrimSpace(name), "ip") {
			ipIdx = i
			break
		}
	}
	if ipIdx < 0 {
		return nil, fmt.Errorf("unreachable blocklist %s missing required 'ip' column", path)
	}

	seen := make(map[uint32]struct{})
	out := make([]uint32, 0)
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read unreachable blocklist %s: %w", path, err)
		}
		if ipIdx >= len(row) {
			continue
		}
		ipStr := strings.TrimSpace(row[ipIdx])
		if ipStr == "" {
			continue
		}
		parsed := net.ParseIP(ipStr)
		if parsed == nil || parsed.To4() == nil {
			return nil, fmt.Errorf("unreachable blocklist %s: invalid IPv4 address %q", path, ipStr)
		}
		u := ipv4ToUint32(parsed.String())
		if _, dup := seen[u]; dup {
			continue
		}
		seen[u] = struct{}{}
		out = append(out, u)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

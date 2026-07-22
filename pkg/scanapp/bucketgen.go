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
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
	"github.com/xuxiping/port-scan-mk3/pkg/progress"
	"github.com/xuxiping/port-scan-mk3/pkg/state"
	"github.com/xuxiping/port-scan-mk3/pkg/task"
)

// GenerateBucketsOptions customizes bucket generation. All fields are optional.
type GenerateBucketsOptions struct {
	// Reporter receives one tick per completed CIDR group and a final Done()
	// summary. When nil, a stderr reporter over the group count is used.
	Reporter progress.Reporter
}

// GenerateBuckets builds a resume Snapshot for the "generate-buckets" step.
//
// It reads targets from cfg.CIDRFile (and ports from cfg.PortFile in basic
// mode), subtracts the optional blocklist at cfg.UnreachableFile, builds one
// task.Chunk per CIDR group over the reachable target set (targets − blocklist),
// stamps pre_scan_ping.enabled=true, and writes the snapshot JSON to
// cfg.BucketsOut via state.SaveSnapshot.
//
// The per-group → chunk conversion fans out across cfg.Workers goroutines, each
// writing into its own pre-indexed result slot (race-free). Chunks are then
// sorted by CIDR before serialization, so the output is byte-identical
// regardless of worker count. CSV parsing is sequential (not parallelized).
//
// The chunk TotalCount is computed through the same builders scan uses
// (buildRichGroupsWithPredicate / buildCIDRGroupsWithPredicate plus
// richChunkFromGroup / basicChunkFromGroup), so the resulting snapshot is
// accepted unchanged by scan's buildRuntimeWithPredicate total_count assertion.
//
// # Returns
//
//	nil on success; error if inputs cannot be loaded, the blocklist is malformed,
//	grouping fails, ctx is cancelled, or the snapshot cannot be written.
func GenerateBuckets(ctx context.Context, cfg config.Config, stderr io.Writer, opts GenerateBucketsOptions) error {
	if strings.TrimSpace(cfg.BucketsOut) == "" {
		return fmt.Errorf("generate-buckets requires -buckets-out")
	}
	if strings.TrimSpace(cfg.CIDRIPCol) == "" {
		cfg.CIDRIPCol = "ip"
	}
	if strings.TrimSpace(cfg.CIDRIPCidrCol) == "" {
		cfg.CIDRIPCidrCol = "ip_cidr"
	}

	inputs, err := loadRunInputs(cfg, defaultRunDependencies())
	if err != nil {
		return err
	}

	blocklist, err := parseUnreachableBlocklist(cfg.UnreachableFile)
	if err != nil {
		return err
	}
	reachable := reachablePredicate(blocklist)

	// Group sequentially through the same builders scan uses, so counting is
	// single-sourced and the total_count invariant holds by construction.
	richMode := hasRichRecords(inputs.cidrRecords)
	var groups map[string]cidrGroup
	if richMode {
		groups, err = buildRichGroupsWithPredicate(inputs.cidrRecords, reachable)
	} else {
		groups, err = buildCIDRGroupsWithPredicate(inputs.cidrRecords, reachable)
	}
	if err != nil {
		return err
	}

	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	reporter := opts.Reporter
	if reporter == nil {
		reporter = progress.New("generate-buckets", len(keys), cfg.ProgressInterval, stderr)
	}

	var rawPorts []string
	if !richMode {
		rawPorts = make([]string, 0, len(inputs.portSpecs))
		for _, p := range inputs.portSpecs {
			rawPorts = append(rawPorts, p.Raw)
		}
	}

	chunks := make([]task.Chunk, len(keys))
	if err := fanOutGroupChunks(ctx, keys, groups, richMode, rawPorts, cfg.Workers, reporter, chunks); err != nil {
		return err
	}
	reporter.Done()

	// Deterministic CIDR-sorted output. Indexing already preserves the sorted
	// key order; sorting again is a defensive guarantee independent of the
	// worker pool's completion order.
	sort.Slice(chunks, func(i, j int) bool { return chunks[i].CIDR < chunks[j].CIDR })

	snap := state.Snapshot{
		Chunks: chunks,
		PreScanPing: state.PreScanPingState{
			Enabled:            true,
			TimeoutMS:          int(cfg.PreScanPingTimeout / time.Millisecond),
			UnreachableIPv4U32: blocklist,
		},
	}
	return state.SaveSnapshot(cfg.BucketsOut, snap)
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
	rawPorts []string,
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
						out[i] = basicChunkFromGroup(key, groups[key], rawPorts)
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
		return nil, err
	}
	defer func() { _ = f.Close() }()

	reader := csv.NewReader(f)
	reader.FieldsPerRecord = -1

	header, err := reader.Read()
	if err != nil {
		if err == io.EOF {
			return nil, nil
		}
		return nil, err
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
			return nil, err
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

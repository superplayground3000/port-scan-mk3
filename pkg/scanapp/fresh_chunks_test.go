package scanapp

import (
	"context"
	"fmt"
	"sort"

	"github.com/xuxiping/port-scan-mk3/pkg/input"
	"github.com/xuxiping/port-scan-mk3/pkg/task"
)

// buildFreshChunksForTest builds the same chunk values as GenerateBuckets.
// Production scan code only loads an existing snapshot.
func buildFreshChunksForTest(cidrRecords []input.CIDRRecord, portSpecs []input.PortSpec, reachable func(string) bool) ([]task.Chunk, error) {
	if hasRichRecords(cidrRecords) {
		chunks, err := buildRichChunksWithPredicate(cidrRecords, reachable)
		if err != nil {
			return nil, fmt.Errorf("build rich chunks: %w", err)
		}
		return chunks, nil
	}
	resolution, err := resolveBasicTargetsContext(context.Background(), cidrRecords, portSpecs, reachable)
	if err != nil {
		return nil, fmt.Errorf("build basic groups: %w", err)
	}
	groups := resolution.groups
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	chunks := make([]task.Chunk, 0, len(keys))
	for _, key := range keys {
		chunks = append(chunks, basicChunkFromGroup(groups[key]))
	}
	return chunks, nil
}

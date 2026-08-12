package scanapp

import (
	"fmt"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
	"github.com/xuxiping/port-scan-mk3/pkg/state"
	"github.com/xuxiping/port-scan-mk3/pkg/task"
)

type targetExpansionConfiguration interface {
	ResolveTargetExpansion() (config.TargetExpansionValues, error)
}

func resolveTargetExpansion(configuration any) (config.TargetExpansionValues, error) {
	resolver, ok := configuration.(targetExpansionConfiguration)
	if !ok {
		return config.TargetExpansionValues{Limits: task.DefaultExpansionLimits()}, nil
	}
	values, err := resolver.ResolveTargetExpansion()
	if err != nil {
		return config.TargetExpansionValues{}, fmt.Errorf("resolve target expansion limits: %w", err)
	}
	return values, nil
}

func incompleteChunkKeys(chunks []task.Chunk) map[string]struct{} {
	keys := make(map[string]struct{})
	for i := range chunks {
		if !chunkIsCompleted(&chunks[i]) {
			keys[chunks[i].CIDR] = struct{}{}
		}
	}
	return keys
}

func effectiveScanExpansionLimits(stored *state.TargetExpansionState, command config.TargetExpansionValues) (task.ExpansionLimits, error) {
	count := int64(task.DefaultTargetCandidateLimit)
	memoryGB := int64(task.DefaultTargetMemoryLimitGB)
	if stored != nil {
		count = stored.CandidateLimit
		memoryGB = stored.MemoryLimitGB
	}
	if command.CountSet {
		count = int64(command.Limits.CandidateLimit())
	}
	if command.MemorySet {
		memoryGB = int64(command.Limits.MemoryLimitGB())
	}
	limits, err := task.NewExpansionLimits(count, memoryGB)
	if err != nil {
		return task.ExpansionLimits{}, fmt.Errorf("validate effective scan target expansion limits: %w", err)
	}
	return limits, nil
}

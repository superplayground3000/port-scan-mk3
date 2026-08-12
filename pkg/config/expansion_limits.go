package config

import (
	"flag"
	"fmt"

	"github.com/xuxiping/port-scan-mk3/pkg/task"
)

type targetExpansionFlagValues struct {
	candidateLimit int64
	memoryLimitGB  int64
}

// TargetExpansionValues contains verified target limits and flag presence.
// Scan uses flag presence to select stored or explicit snapshot limits.
type TargetExpansionValues struct {
	Limits    task.ExpansionLimits
	CountSet  bool
	MemorySet bool
}

func defaultTargetExpansionValues() TargetExpansionValues {
	return TargetExpansionValues{Limits: task.DefaultExpansionLimits()}
}

func registerTargetExpansionFlags(fs *flag.FlagSet, values *targetExpansionFlagValues) {
	values.candidateLimit = int64(task.DefaultTargetCandidateLimit)
	values.memoryLimitGB = int64(task.DefaultTargetMemoryLimitGB)
	fs.Int64Var(&values.candidateLimit, "target-count-limit", values.candidateLimit, "maximum candidate addresses. 0 disables this limit")
	fs.Int64Var(&values.memoryLimitGB, "target-memory-limit-gb", values.memoryLimitGB, "target-expansion memory budget in decimal GB. 0 disables this limit")
}

func resolveTargetExpansionFlags(fs *flag.FlagSet, values targetExpansionFlagValues) (TargetExpansionValues, error) {
	limits, err := task.NewExpansionLimits(values.candidateLimit, values.memoryLimitGB)
	if err != nil {
		return TargetExpansionValues{}, fmt.Errorf("validate target expansion flags: %w", err)
	}
	present := make(map[string]bool, 2)
	fs.Visit(func(item *flag.Flag) {
		present[item.Name] = true
	})
	return TargetExpansionValues{
		Limits:    limits,
		CountSet:  present["target-count-limit"],
		MemorySet: present["target-memory-limit-gb"],
	}, nil
}

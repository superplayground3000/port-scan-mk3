package perfharness

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
	"github.com/xuxiping/port-scan-mk3/pkg/task"
)

// TargetLimitSpec defines one target count or memory limit case.
type TargetLimitSpec struct {
	OutputDir string     `json:"output_dir"`
	Flag      string     `json:"flag"`
	Case      BypassCase `json:"case"`
}

// RunTargetLimitCase runs one target limit case without target allocation.
func (suite Suite) RunTargetLimitCase(ctx context.Context, spec TargetLimitSpec) (CaseResult, error) {
	if spec.Flag != "-target-count-limit" && spec.Flag != "-target-memory-limit-gb" {
		return CaseResult{}, fmt.Errorf("unsupported target limit flag %q", spec.Flag)
	}
	observations := make([]Observation, 0, 6)
	for run := 0; run < 6; run++ {
		observation, err := suite.Measure(ctx, 0, 1, func(context.Context) (uint64, error) {
			return 0, executeTargetLimitCase(spec)
		})
		if err != nil {
			return CaseResult{}, fmt.Errorf("run %s %s observation %d: %w", spec.Flag, spec.Case.Kind, run+1, err)
		}
		observations = append(observations, observation)
	}
	result, err := SummarizeCase("limit/"+strings.TrimPrefix(spec.Flag, "-")+"/"+string(spec.Case.Kind), observations)
	if err != nil {
		return CaseResult{}, err
	}
	result.Correctness = Correctness{ExpectedValues: true, Detail: "production limit result matched the case"}
	result.Verdict = Verdict{Passed: true}
	result.Semantic = &SemanticArtifact{Status: "passed"}
	return result, nil
}

func executeTargetLimitCase(spec TargetLimitSpec) error {
	switch spec.Case.Kind {
	case BypassNegative:
		return expectTargetLimitParseFailure(spec.Flag, "-1")
	case BypassOverflow:
		value := strconv.FormatUint(math.MaxUint64, 10)
		if spec.Flag == "-target-memory-limit-gb" {
			value = strconv.FormatInt(math.MaxInt64, 10)
		}
		return expectTargetLimitParseFailure(spec.Flag, value)
	}
	limits, candidates, wantFailure, err := targetLimitInputs(spec)
	if err != nil {
		return err
	}
	_, estimateErr := task.EstimateCandidateCounts([]task.CandidateInput{{Row: 1, CIDR: "performance-case", Count: candidates}}, limits)
	if wantFailure {
		if estimateErr == nil {
			return fmt.Errorf("%s %s accepted %d candidates", spec.Flag, spec.Case.Kind, candidates)
		}
		return nil
	}
	if estimateErr != nil {
		return fmt.Errorf("%s %s rejected %d candidates: %w", spec.Flag, spec.Case.Kind, candidates, estimateErr)
	}
	return nil
}

func targetLimitInputs(spec TargetLimitSpec) (task.ExpansionLimits, uint64, bool, error) {
	countLimit := int64(0)
	memoryLimit := int64(0)
	candidates := task.DefaultTargetCandidateLimit
	wantFailure := false
	if spec.Flag == "-target-count-limit" {
		countLimit = int64(task.DefaultTargetCandidateLimit)
	} else {
		memoryLimit = int64(task.DefaultTargetMemoryLimitGB)
	}
	switch spec.Case.Kind {
	case BypassExactDefault:
	case BypassDefaultPlusOne:
		candidates++
		wantFailure = true
	case BypassPositiveOverride:
		candidates *= 2
		if spec.Flag == "-target-count-limit" {
			countLimit = int64(candidates)
		} else {
			memoryLimit = int64(task.DefaultTargetMemoryLimitGB * 2)
		}
	case BypassDisabledTwice:
		candidates *= 2
		countLimit = 0
		memoryLimit = 0
	default:
		return task.ExpansionLimits{}, 0, false, fmt.Errorf("unsupported bypass kind %q", spec.Case.Kind)
	}
	limits, err := parsedTargetLimits(countLimit, memoryLimit)
	return limits, candidates, wantFailure, err
}

func parsedTargetLimits(countLimit, memoryLimit int64) (task.ExpansionLimits, error) {
	configuration, err := config.ParseValidate([]string{
		"-cidr-file", "performance-input.csv",
		"-target-count-limit", strconv.FormatInt(countLimit, 10),
		"-target-memory-limit-gb", strconv.FormatInt(memoryLimit, 10),
	})
	if err != nil {
		return task.ExpansionLimits{}, err
	}
	values, err := configuration.ResolveTargetExpansion()
	if err != nil {
		return task.ExpansionLimits{}, err
	}
	return values.Limits, nil
}

func expectTargetLimitParseFailure(flagName, value string) error {
	_, err := config.ParseValidate([]string{"-cidr-file", "performance-input.csv", flagName, value})
	if err == nil {
		return fmt.Errorf("%s accepted invalid value %s", flagName, value)
	}
	return nil
}

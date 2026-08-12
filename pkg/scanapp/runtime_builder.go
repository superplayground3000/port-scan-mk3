package scanapp

import (
	"context"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/input"
	"github.com/xuxiping/port-scan-mk3/pkg/task"
)

type runPlan struct {
	runtimes []*chunkRuntime
}

type runDependencies struct {
	loadCIDRRecords        func(path, ipCol, ipCidrCol string) ([]input.CIDRRecord, error)
	loadCIDRRecordsContext func(context.Context, string, string, string) ([]input.CIDRRecord, error)
	loadPortSpecs          func(path string) ([]input.PortSpec, error)
	loadPortSpecsContext   func(context.Context, string) ([]input.PortSpec, error)
	resolveOutputPaths     func(output string, now time.Time) (batchOutputPaths, error)
}

func defaultRunDependencies() runDependencies {
	return runDependencies{
		loadCIDRRecords:        readCIDRFile,
		loadCIDRRecordsContext: readCIDRFileContext,
		loadPortSpecs:          readPortFile,
		loadPortSpecsContext:   readPortFileContext,
		resolveOutputPaths:     resolveBatchOutputPaths,
	}
}

func resolveRunOutputPaths(output string, deps runDependencies, now time.Time) (batchOutputPaths, error) {
	return deps.resolveOutputPaths(output, now)
}

func prepareRuntimePlan(policy runtimePolicy, inputs runInputs, reachable func(string) bool, chunks []task.Chunk, report chunkExpandReporter) (runPlan, error) {
	return prepareRuntimePlanContext(context.Background(), policy, inputs, reachable, chunks, report)
}

func prepareRuntimePlanContext(ctx context.Context, policy runtimePolicy, inputs runInputs, reachable func(string) bool, chunks []task.Chunk, report chunkExpandReporter) (runPlan, error) {
	runtimes, err := buildRuntimeWithPredicateContext(ctx, chunks, inputs.cidrRecords, inputs.portSpecs, policy, reachable, report)
	if err != nil {
		return runPlan{}, err
	}

	return runPlan{runtimes: runtimes}, nil
}

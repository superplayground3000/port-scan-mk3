package scanapp

import (
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/input"
	"github.com/xuxiping/port-scan-mk3/pkg/task"
)

type runPlan struct {
	runtimes []*chunkRuntime
}

type runDependencies struct {
	loadCIDRRecords    func(path, ipCol, ipCidrCol string) ([]input.CIDRRecord, error)
	loadPortSpecs      func(path string) ([]input.PortSpec, error)
	resolveOutputPaths func(output string, now time.Time) (batchOutputPaths, error)
}

func defaultRunDependencies() runDependencies {
	return runDependencies{
		loadCIDRRecords:    readCIDRFile,
		loadPortSpecs:      readPortFile,
		resolveOutputPaths: resolveBatchOutputPaths,
	}
}

func resolveRunOutputPaths(output string, deps runDependencies, now time.Time) (batchOutputPaths, error) {
	return deps.resolveOutputPaths(output, now)
}

func prepareRuntimePlan(policy runtimePolicy, inputs runInputs, reachable func(string) bool, chunks []task.Chunk, report chunkExpandReporter) (runPlan, error) {
	runtimes, err := buildRuntimeWithPredicate(chunks, inputs.cidrRecords, inputs.portSpecs, policy, reachable, report)
	if err != nil {
		return runPlan{}, err
	}

	return runPlan{runtimes: runtimes}, nil
}

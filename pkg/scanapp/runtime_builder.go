package scanapp

import (
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
	"github.com/xuxiping/port-scan-mk3/pkg/input"
	"github.com/xuxiping/port-scan-mk3/pkg/task"
)

type runPlan struct {
	chunks         []task.Chunk
	runtimes       []*chunkRuntime
	outputPaths    batchOutputPaths
	scanOutputPath string
	openOnlyPath   string
}

type runDependencies struct {
	loadCIDRRecords           func(path, ipCol, ipCidrCol string) ([]input.CIDRRecord, error)
	loadPortSpecs             func(path string) ([]input.PortSpec, error)
	loadOrBuildRuntimeChunks  func(cfg config.Config, cidrRecords []input.CIDRRecord, portSpecs []input.PortSpec) ([]task.Chunk, error)
	loadOrBuildFilteredChunks func(cfg config.Config, cidrRecords []input.CIDRRecord, portSpecs []input.PortSpec, reachable func(string) bool) ([]task.Chunk, error)
	buildChunkRuntime         func(chunks []task.Chunk, cidrRecords []input.CIDRRecord, defaultPorts []input.PortSpec, policy runtimePolicy) ([]*chunkRuntime, error)
	buildFilteredRuntime      func(chunks []task.Chunk, cidrRecords []input.CIDRRecord, defaultPorts []input.PortSpec, policy runtimePolicy, reachable func(string) bool) ([]*chunkRuntime, error)
	resolveOutputPaths        func(output string, now time.Time) (batchOutputPaths, error)
}

func defaultRunDependencies() runDependencies {
	return runDependencies{
		loadCIDRRecords:           readCIDRFile,
		loadPortSpecs:             readPortFile,
		loadOrBuildRuntimeChunks:  loadOrBuildChunks,
		loadOrBuildFilteredChunks: loadOrBuildChunksWithPredicate,
		buildChunkRuntime:         buildRuntime,
		buildFilteredRuntime:      buildRuntimeWithPredicate,
		resolveOutputPaths:        resolveBatchOutputPaths,
	}
}

func prepareRunPlan(cfg config.Config, inputs runInputs, deps runDependencies, now time.Time) (runPlan, error) {
	outputPaths, err := resolveRunOutputPaths(cfg, deps, now)
	if err != nil {
		return runPlan{}, err
	}

	chunks, err := deps.loadOrBuildRuntimeChunks(cfg, inputs.cidrRecords, inputs.portSpecs)
	if err != nil {
		return runPlan{}, err
	}
	runtimes, err := deps.buildChunkRuntime(chunks, inputs.cidrRecords, inputs.portSpecs, runtimePolicyFromConfig(cfg))
	if err != nil {
		return runPlan{}, err
	}

	return runPlan{
		chunks:         chunks,
		runtimes:       runtimes,
		outputPaths:    outputPaths,
		scanOutputPath: outputPaths.scanPath,
		openOnlyPath:   outputPaths.openPath,
	}, nil
}

func resolveRunOutputPaths(cfg config.Config, deps runDependencies, now time.Time) (batchOutputPaths, error) {
	return deps.resolveOutputPaths(cfg.Output, now)
}

func prepareRuntimePlan(cfg config.Config, inputs runInputs, deps runDependencies, reachable func(string) bool, resumeChunks []task.Chunk, useResumeChunks bool) (runPlan, error) {
	chunks, err := resolveRuntimeChunks(cfg, inputs, deps, reachable, resumeChunks, useResumeChunks)
	if err != nil {
		return runPlan{}, err
	}

	runtimes, err := buildRuntimePlanRuntimes(cfg, inputs, deps, chunks, reachable)
	if err != nil {
		return runPlan{}, err
	}

	return runPlan{
		chunks:   chunks,
		runtimes: runtimes,
	}, nil
}

func resolveRuntimeChunks(cfg config.Config, inputs runInputs, deps runDependencies, reachable func(string) bool, resumeChunks []task.Chunk, useResumeChunks bool) ([]task.Chunk, error) {
	if useResumeChunks {
		return append([]task.Chunk(nil), resumeChunks...), nil
	}
	buildCfg := cfg
	buildCfg.Resume = ""
	if reachable != nil && deps.loadOrBuildFilteredChunks != nil {
		return deps.loadOrBuildFilteredChunks(buildCfg, inputs.cidrRecords, inputs.portSpecs, reachable)
	}
	return deps.loadOrBuildRuntimeChunks(buildCfg, inputs.cidrRecords, inputs.portSpecs)
}

func buildRuntimePlanRuntimes(cfg config.Config, inputs runInputs, deps runDependencies, chunks []task.Chunk, reachable func(string) bool) ([]*chunkRuntime, error) {
	if reachable != nil && deps.buildFilteredRuntime != nil {
		return deps.buildFilteredRuntime(chunks, inputs.cidrRecords, inputs.portSpecs, runtimePolicyFromConfig(cfg), reachable)
	}
	return deps.buildChunkRuntime(chunks, inputs.cidrRecords, inputs.portSpecs, runtimePolicyFromConfig(cfg))
}

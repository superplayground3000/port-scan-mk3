package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/xuxiping/port-scan-mk3/internal/perfharness"
)

type matrixRunner struct {
	options      commandOptions
	operations   matrixOperations
	contract     perfharness.Contract
	specs        []perfharness.FixtureSpec
	outputMatrix []perfharness.OutputSpec
	casesDir     string
	results      []perfharness.CaseResult
	outputScales []uint64
	stdout       io.Writer
	stderr       io.Writer
}

type matrixOperations struct {
	fixtures     fixtureOperations
	outputs      outputOperations
	limits       limitOperations
	workflows    workflowOperations
	cancellation cancellationOperations
	recovery     recoveryOperations
	rich         richOperations
	platform     platformOperations
	evaluation   evaluationOperations
}

type fixtureOperations struct {
	runSnapshot func(context.Context, string, perfharness.FixtureSpec) ([]perfharness.CaseResult, error)
	runPrePing  func(context.Context, perfharness.ProductionStageSpec) (perfharness.CaseResult, error)
	runBucket   func(context.Context, perfharness.ProductionStageSpec) (perfharness.CaseResult, error)
	runFixture  func(context.Context, string, perfharness.FixtureSpec) (perfharness.CaseResult, error)
}

type outputOperations struct {
	run func(context.Context, perfharness.OutputSpec) (perfharness.CaseResult, error)
}

type limitOperations struct {
	runTarget   func(context.Context, perfharness.TargetLimitSpec) (perfharness.CaseResult, error)
	runResource func(context.Context, perfharness.ResourceLimitSpec) (perfharness.CaseResult, error)
}

type workflowOperations struct {
	runProduction    workflowRunner
	runOrchestration workflowRunner
	runRichDeny      richDenyRunner
	compareSemantic  semanticComparator
}

type cancellationOperations struct {
	run cancellationRunner
}

type recoveryOperations struct {
	runRegression   func(context.Context, perfharness.RegressionBenchmarkSpec) (perfharness.CaseResult, error)
	runResume       resumeRunner
	runFailure      failureRunner
	compareSemantic semanticComparator
}

type richOperations struct {
	run             richRunner
	runOversize     func(context.Context, perfharness.RichOversizeSpec) (perfharness.CaseResult, error)
	compareSemantic semanticComparator
}

type platformOperations struct {
	runProduction   workflowRunner
	runLoopback     workflowRunner
	compareSemantic semanticComparator
}

type evaluationOperations struct {
	evaluate        evaluator
	compareSemantic semanticComparator
	writeReports    func(context.Context, string, perfharness.Report) (perfharness.ReportPaths, error)
}

func newMatrixOperations(suite perfharness.Suite) matrixOperations {
	return matrixOperations{
		fixtures: fixtureOperations{
			runSnapshot: suite.RunSnapshotCases,
			runPrePing:  suite.RunPrePingCase,
			runBucket:   suite.RunBucketCase,
			runFixture:  suite.RunFixtureCase,
		},
		outputs: outputOperations{run: suite.RunOutputCase},
		limits: limitOperations{
			runTarget:   suite.RunTargetLimitCase,
			runResource: suite.RunResourceLimitCase,
		},
		workflows: workflowOperations{
			runProduction:    suite.RunProductionSmoke,
			runOrchestration: suite.RunOrchestrationSmoke,
			runRichDeny:      suite.RunRichDenySmoke,
			compareSemantic:  suite.CompareSemantic,
		},
		cancellation: cancellationOperations{run: suite.RunCancellationSmoke},
		recovery: recoveryOperations{
			runRegression:   suite.RunRegressionBenchmark,
			runResume:       suite.RunResumeSmoke,
			runFailure:      suite.RunFailureSmoke,
			compareSemantic: suite.CompareSemantic,
		},
		rich: richOperations{
			run:             suite.RunRichSmoke,
			runOversize:     suite.RunRichOversizeCase,
			compareSemantic: suite.CompareSemantic,
		},
		platform: platformOperations{
			runProduction:   suite.RunProductionSmoke,
			runLoopback:     suite.RunNativeLoopbackSmoke,
			compareSemantic: suite.CompareSemantic,
		},
		evaluation: evaluationOperations{
			evaluate:        suite.Evaluate,
			compareSemantic: suite.CompareSemantic,
			writeReports:    suite.WriteReports,
		},
	}
}
func newMatrixRunner(options commandOptions, operations matrixOperations, stdout, stderr io.Writer) (*matrixRunner, error) {
	outputMatrix := outputSpecs(options.profile, options.smokeItems)
	if required := requiredOutputBytes(outputMatrix); options.freeDiskBytes > 0 && options.freeDiskBytes < required {
		return nil, fmt.Errorf("insufficient free space for output matrix: have %d bytes, require %d bytes", options.freeDiskBytes, required)
	}
	contract := perfharness.DefaultContract()
	if options.regressionBeforeNS > 0 {
		contract.RegressionBenchmark.BeforeNSPerOp = options.regressionBeforeNS
	}
	if options.regressionBeforeB > 0 {
		contract.RegressionBenchmark.BeforeBPerOp = options.regressionBeforeB
	}
	casesDir := filepath.Join(options.outputDir, "cases")
	if err := os.Mkdir(casesDir, 0o755); err != nil {
		return nil, fmt.Errorf("create cases directory: %w", err)
	}
	specs := fixtureSpecs(options.profile, options.smokeItems, options.smokeSnapshotBytes, contract)
	return &matrixRunner{
		options:      options,
		operations:   operations,
		contract:     contract,
		specs:        specs,
		outputMatrix: outputMatrix,
		casesDir:     casesDir,
		results:      make([]perfharness.CaseResult, 0, len(specs)+5),
		outputScales: make([]uint64, 0, len(outputMatrix)/3),
		stdout:       stdout,
		stderr:       stderr,
	}, nil
}

func (runner *matrixRunner) run() int {
	phases := []func() error{
		runner.runFixtures,
		runner.runOutputs,
		runner.runLimits,
		runner.runWorkflows,
		runner.runCancellations,
		runner.runRecoveryAndFailures,
		runner.runRichCases,
		runner.runPlatformCases,
	}
	for _, phase := range phases {
		if err := phase(); err != nil {
			if writeErr := writeStatus(runner.stderr, "%v\n", err); writeErr != nil {
				return 1
			}
			return 1
		}
	}
	return runner.evaluateAndWriteReport()
}

func (runner *matrixRunner) appendResult(result perfharness.CaseResult) error {
	runner.results = append(runner.results, result)
	return writeStatus(runner.stdout, "case passed: %s\n", result.Name)
}

func (runner *matrixRunner) runFixtures() error {
	for index, spec := range runner.specs {
		caseDir := filepath.Join(runner.casesDir, fmt.Sprintf("%02d-%s", index, spec.Family))
		route, err := fixtureRouteFor(spec)
		if err != nil {
			return fmt.Errorf("route fixture %s: %w", spec.Family, err)
		}
		if route == fixtureRouteSnapshot {
			results, runErr := runner.operations.fixtures.runSnapshot(context.Background(), caseDir, spec)
			if runErr != nil {
				return fmt.Errorf("run snapshot fixture %s: %w", spec.Shape, runErr)
			}
			for _, result := range results {
				if err := runner.appendResult(result); err != nil {
					return err
				}
			}
			continue
		}
		result, skip, runErr := runner.runFixtureRoute(route, caseDir, spec)
		if runErr != nil {
			return fmt.Errorf("run fixture %s: %w", spec.Family, runErr)
		}
		if !skip {
			if err := runner.appendResult(result); err != nil {
				return err
			}
		}
	}
	return nil
}

func (runner *matrixRunner) runFixtureRoute(route fixtureRoute, caseDir string, spec perfharness.FixtureSpec) (perfharness.CaseResult, bool, error) {
	switch route {
	case fixtureRoutePrePing:
		result, err := runner.operations.fixtures.runPrePing(context.Background(), perfharness.ProductionStageSpec{OutputDir: caseDir, Items: spec.Scale.CandidateAddresses, Workers: 16})
		return result, false, err
	case fixtureRouteBuckets:
		result, err := runner.operations.fixtures.runBucket(context.Background(), perfharness.ProductionStageSpec{OutputDir: caseDir, Items: spec.Scale.ProbeTasks, Workers: 16})
		return result, false, err
	case fixtureRouteOutput, fixtureRouteResume:
		return perfharness.CaseResult{}, true, nil
	case fixtureRouteValidate, fixtureRoutePort, fixtureRouteRich, fixtureRouteRichDeny:
		result, err := runner.operations.fixtures.runFixture(context.Background(), caseDir, spec)
		return result, false, err
	default:
		return perfharness.CaseResult{}, false, fmt.Errorf("fixture family %q resolved to invalid production route %d", spec.Family, route)
	}
}

func (runner *matrixRunner) runOutputs() error {
	for index, spec := range runner.outputMatrix {
		spec.OutputDir = filepath.Join(runner.casesDir, fmt.Sprintf("output-%02d-results-%d-flush-%d", index, spec.Results, spec.FlushResults))
		result, err := runner.operations.outputs.run(context.Background(), spec)
		if err != nil {
			return fmt.Errorf("run output results=%d flush=%d: %w", spec.Results, spec.FlushResults, err)
		}
		if spec.FlushResults == 1 {
			runner.outputScales = append(runner.outputScales, spec.Results)
		}
		if err := runner.appendResult(result); err != nil {
			return err
		}
	}
	return nil
}

func (runner *matrixRunner) runLimits() error {
	for _, limits := range runner.contract.Limits {
		for _, bypass := range limits.Cases {
			result, err := runner.runLimit(limits.Flag, bypass)
			if err != nil {
				return fmt.Errorf("run limit %s %s: %w", limits.Flag, bypass.Kind, err)
			}
			if err := runner.appendResult(result); err != nil {
				return err
			}
		}
	}
	return nil
}

func (runner *matrixRunner) runLimit(flag string, bypass perfharness.BypassCase) (perfharness.CaseResult, error) {
	if flag == "-target-count-limit" || flag == "-target-memory-limit-gb" {
		return runner.operations.limits.runTarget(context.Background(), perfharness.TargetLimitSpec{
			OutputDir: filepath.Join(runner.casesDir, "limit-"+strings.TrimPrefix(flag, "-")+"-"+string(bypass.Kind)),
			Flag:      flag,
			Case:      bypass,
		})
	}
	return runner.operations.limits.runResource(context.Background(), perfharness.ResourceLimitSpec{Flag: flag, Case: bypass})
}

func (runner *matrixRunner) runWorkflows() error {
	items := profileItemCount(runner.options.profile, runner.options.smokeItems)
	for _, workers := range runner.contract.FakeWorkers {
		result, err := runOrchestrationCase(context.Background(), runner.operations.workflows.compareSemantic, filepath.Join(runner.casesDir, "orchestration-workers-"+strconv.Itoa(workers)), items, workers, runner.operations.workflows.runOrchestration)
		if err != nil {
			return fmt.Errorf("run workflow workers=%d: %w", workers, err)
		}
		if err := runner.appendResult(result); err != nil {
			return err
		}
	}
	if err := runner.runCompleteWorkflows(items); err != nil {
		return err
	}
	for _, shape := range []string{"deny-only", "accept-deny-conflict"} {
		result, err := runRichDenyCase(context.Background(), runner.operations.workflows.runRichDeny, filepath.Join(runner.casesDir, "rich-deny-"+shape), items, 16, shape)
		if err != nil {
			return fmt.Errorf("run rich-deny shape=%s: %w", shape, err)
		}
		if err := runner.appendResult(result); err != nil {
			return err
		}
	}
	return nil
}

func (runner *matrixRunner) runCompleteWorkflows(items uint64) error {
	result, err := runWorkflowCase(context.Background(), runner.operations.workflows.compareSemantic, filepath.Join(runner.casesDir, "workflow-complete-workers-16"), items, 16, "", runner.operations.workflows.runProduction)
	if err != nil {
		return fmt.Errorf("run complete workflow: %w", err)
	}
	if err := runner.appendResult(result); err != nil {
		return err
	}
	if items < 10 {
		return nil
	}
	result, err = runWorkflowCase(context.Background(), runner.operations.workflows.compareSemantic, filepath.Join(runner.casesDir, "workflow-growth-1x-workers-16"), items/10, 16, "", runner.operations.workflows.runProduction)
	if err != nil {
		return fmt.Errorf("run workflow 1x growth case: %w", err)
	}
	result.Name = "production-workflow/complete/growth-1x/workers-16"
	return runner.appendResult(result)
}

func (runner *matrixRunner) runCancellations() error {
	for _, cancellation := range cancellationCaseSpecs(runner.options.profile, runner.contract) {
		result, err := runCancellationCase(context.Background(), runner.operations.cancellation.run, filepath.Join(runner.casesDir, fmt.Sprintf("cancel-%s-%d", cancellation.Stage, cancellation.Percent)), cancellation.Items, cancellation.Workers, cancellation.Stage, cancellation.Percent, runner.contract.StopWithin)
		if err != nil {
			return fmt.Errorf("run cancellation stage=%s percent=%d: %w", cancellation.Stage, cancellation.Percent, err)
		}
		if err := runner.appendResult(result); err != nil {
			return err
		}
	}
	return nil
}

func (runner *matrixRunner) runRecoveryAndFailures() error {
	result, err := runner.operations.recovery.runRegression(context.Background(), runner.contract.RegressionBenchmark)
	if err != nil {
		return fmt.Errorf("run regression benchmark: %w", err)
	}
	if err := runner.appendResult(result); err != nil {
		return err
	}
	items := profileItemCount(runner.options.profile, runner.options.smokeItems)
	for _, percent := range []int{0, 50, 99} {
		result, err := runResumeCase(context.Background(), runner.operations.recovery.runResume, runner.operations.recovery.compareSemantic, filepath.Join(runner.casesDir, fmt.Sprintf("resume-%d", percent)), items, 16, percent)
		if err != nil {
			return fmt.Errorf("run resume percent=%d: %w", percent, err)
		}
		if err := runner.appendResult(result); err != nil {
			return err
		}
	}
	for _, scenario := range []string{"output-failure", "snapshot-save-failure", "pressure-fatal-error"} {
		result, err := runFailureCase(context.Background(), runner.operations.recovery.runFailure, filepath.Join(runner.casesDir, "failure-"+scenario), fullOrBoundedItems(runner.options.profile, 100), 16, scenario)
		if err != nil {
			return fmt.Errorf("run failure scenario=%s: %w", scenario, err)
		}
		if err := runner.appendResult(result); err != nil {
			return err
		}
	}
	return nil
}

func (runner *matrixRunner) runRichCases() error {
	for _, family := range []perfharness.Family{perfharness.FamilyRichRecordMixed, perfharness.FamilyRichUniqueKey, perfharness.FamilyRichHotKey, perfharness.FamilyRichPrecheck} {
		result, err := runAcceptedRichCase(context.Background(), runner.operations.rich.run, runner.operations.rich.compareSemantic, filepath.Join(runner.casesDir, "rich-"+string(family)), fullOrBoundedItems(runner.options.profile, 100), 16, family)
		if err != nil {
			return fmt.Errorf("run rich family=%s: %w", family, err)
		}
		if err := runner.appendResult(result); err != nil {
			return err
		}
	}
	if runner.options.profile != "full" {
		return nil
	}
	for _, caseName := range runner.contract.RichOversizeCases {
		result, err := runner.operations.rich.runOversize(context.Background(), perfharness.RichOversizeSpec{
			OutputDir:   filepath.Join(runner.casesDir, "rich-oversize-"+caseName),
			Items:       perfharness.FullItemCount,
			Workers:     16,
			TargetBytes: 1_000_000_001,
			LimitBytes:  1_000_000_000,
			Case:        caseName,
		})
		if err != nil {
			return fmt.Errorf("run rich oversize case=%s: %w", caseName, err)
		}
		if err := runner.appendResult(result); err != nil {
			return err
		}
	}
	return nil
}

func (runner *matrixRunner) runPlatformCases() error {
	items := profileItemCount(runner.options.profile, runner.options.smokeItems)
	result, err := runWorkflowCase(context.Background(), runner.operations.platform.compareSemantic, filepath.Join(runner.casesDir, "workflow-crlf-workers-16"), items, 16, "CRLF", runner.operations.platform.runProduction)
	if err != nil {
		return fmt.Errorf("run CRLF workflow: %w", err)
	}
	if err := runner.appendResult(result); err != nil {
		return err
	}
	for _, workers := range runner.contract.LoopbackWorkers {
		result, err := runLoopbackCase(context.Background(), runner.operations.platform.runLoopback, filepath.Join(runner.casesDir, "loopback-workers-"+strconv.Itoa(workers)), workers)
		if err != nil {
			return fmt.Errorf("run loopback workers=%d: %w", workers, err)
		}
		if err := runner.appendResult(result); err != nil {
			return err
		}
	}
	return nil
}

func (runner *matrixRunner) evaluateAndWriteReport() int {
	passed := runner.evaluate()
	report := perfharness.Report{
		SchemaVersion: perfharness.SchemaVersion,
		Contract:      runner.contract,
		Platform:      runtime.GOOS + "/" + runtime.GOARCH,
		Hardware: perfharness.HardwareProfile{
			EvidenceLabel: perfharness.EvidenceLabel(runner.options.evidenceLabel),
			CPU:           runner.options.cpu,
			PhysicalCores: runner.options.physicalCores,
			LogicalCores:  runner.options.logicalCores,
			PowerMode:     runner.options.powerMode,
			RAMBytes:      runner.options.ramBytes,
			Filesystem:    runner.options.filesystem,
			Disk:          runner.options.disk,
			FreeDiskBytes: runner.options.freeDiskBytes,
			GoVersion:     runtime.Version(),
			Commit:        runner.options.commit,
			Constraints:   runner.options.constraints,
		},
		Cases: runner.results,
	}
	paths, err := runner.operations.evaluation.writeReports(context.Background(), filepath.Join(runner.options.outputDir, "report"), report)
	if err != nil {
		if writeErr := writeStatus(runner.stderr, "write reports: %v\n", err); writeErr != nil {
			return 1
		}
		return 1
	}
	if !passed {
		if writeErr := writeStatus(runner.stderr, "performance matrix failed one or more thresholds: JSON=%s Markdown=%s\n", paths.JSON, paths.Markdown); writeErr != nil {
			return 1
		}
		return 1
	}
	if err := writeStatus(runner.stdout, "performance matrix passed: JSON=%s Markdown=%s\n", paths.JSON, paths.Markdown); err != nil {
		return 1
	}
	return 0
}

func (runner *matrixRunner) evaluate() bool {
	passed := applyAbsoluteThresholds(runner.results, runner.operations.evaluation.evaluate, runner.contract)
	if runner.options.profile == "full" {
		passed = applyFixtureCaseContract(runner.results, runner.contract.FixtureCases) && passed
	} else {
		passed = applySnapshotCaseContract(runner.results, runner.specs) && passed
	}
	passed = applyWorkerParity(runner.results, runner.operations.evaluation.compareSemantic, runner.contract.FakeWorkers) && passed
	items := profileItemCount(runner.options.profile, runner.options.smokeItems)
	if items >= 10 {
		passed = applyGrowthThreshold(runner.results, runner.operations.evaluation.evaluate, "production-workflow/complete/growth-1x/workers-16", "production-workflow/complete/workers-16") && passed
	}
	if items >= runner.contract.SmokeItems {
		passed = applyWorkerMemoryThreshold(runner.results, runner.operations.evaluation.evaluate) && passed
	}
	passed = applyOutputThresholds(runner.results, runner.operations.evaluation.evaluate, runner.outputScales) && passed
	return applyInputAndSnapshotGrowthThresholds(runner.results, runner.operations.evaluation.evaluate) && passed
}

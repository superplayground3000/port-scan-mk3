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
	harness      perfharness.Suite
	contract     perfharness.Contract
	specs        []perfharness.FixtureSpec
	outputMatrix []perfharness.OutputSpec
	casesDir     string
	results      []perfharness.CaseResult
	outputScales []uint64
	stdout       io.Writer
	stderr       io.Writer
}

func newMatrixRunner(options commandOptions, harness perfharness.Suite, stdout, stderr io.Writer) (*matrixRunner, error) {
	outputMatrix := outputSpecs(options.profile, options.smokeItems)
	if required := requiredOutputBytes(outputMatrix); options.freeDiskBytes > 0 && options.freeDiskBytes < required {
		return nil, fmt.Errorf("insufficient free space for output matrix: have %d bytes, require %d bytes", options.freeDiskBytes, required)
	}
	if err := os.Mkdir(options.outputDir, 0o755); err != nil {
		return nil, fmt.Errorf("create matrix directory: %w", err)
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
		harness:      harness,
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
			_ = writeStatus(runner.stderr, "%v\n", err)
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
			results, runErr := runner.harness.RunSnapshotCases(context.Background(), caseDir, spec)
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
		result, err := runner.harness.RunPrePingCase(context.Background(), perfharness.ProductionStageSpec{OutputDir: caseDir, Items: spec.Scale.CandidateAddresses, Workers: 16})
		return result, false, err
	case fixtureRouteBuckets:
		result, err := runner.harness.RunBucketCase(context.Background(), perfharness.ProductionStageSpec{OutputDir: caseDir, Items: spec.Scale.ProbeTasks, Workers: 16})
		return result, false, err
	case fixtureRouteOutput, fixtureRouteResume:
		return perfharness.CaseResult{}, true, nil
	case fixtureRouteValidate, fixtureRoutePort, fixtureRouteRich, fixtureRouteRichDeny:
		result, err := runner.harness.RunFixtureCase(context.Background(), caseDir, spec)
		return result, false, err
	default:
		return perfharness.CaseResult{}, false, fmt.Errorf("fixture family %q resolved to invalid production route %d", spec.Family, route)
	}
}

func (runner *matrixRunner) runOutputs() error {
	for index, spec := range runner.outputMatrix {
		spec.OutputDir = filepath.Join(runner.casesDir, fmt.Sprintf("output-%02d-results-%d-flush-%d", index, spec.Results, spec.FlushResults))
		result, err := runner.harness.RunOutputCase(context.Background(), spec)
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
		return runner.harness.RunTargetLimitCase(context.Background(), perfharness.TargetLimitSpec{
			OutputDir: filepath.Join(runner.casesDir, "limit-"+strings.TrimPrefix(flag, "-")+"-"+string(bypass.Kind)),
			Flag:      flag,
			Case:      bypass,
		})
	}
	return runner.harness.RunResourceLimitCase(context.Background(), perfharness.ResourceLimitSpec{Flag: flag, Case: bypass})
}

func (runner *matrixRunner) runWorkflows() error {
	items := profileItemCount(runner.options.profile, runner.options.smokeItems)
	for _, workers := range runner.contract.FakeWorkers {
		result, err := runOrchestrationCase(context.Background(), runner.harness, filepath.Join(runner.casesDir, "orchestration-workers-"+strconv.Itoa(workers)), items, workers)
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
		result, err := runRichDenyCase(context.Background(), runner.harness, filepath.Join(runner.casesDir, "rich-deny-"+shape), items, 16, shape)
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
	result, err := runWorkflowCase(context.Background(), runner.harness, filepath.Join(runner.casesDir, "workflow-complete-workers-16"), items, 16, "")
	if err != nil {
		return fmt.Errorf("run complete workflow: %w", err)
	}
	if err := runner.appendResult(result); err != nil {
		return err
	}
	if items < 10 {
		return nil
	}
	result, err = runWorkflowCase(context.Background(), runner.harness, filepath.Join(runner.casesDir, "workflow-growth-1x-workers-16"), items/10, 16, "")
	if err != nil {
		return fmt.Errorf("run workflow 1x growth case: %w", err)
	}
	result.Name = "production-workflow/complete/growth-1x/workers-16"
	return runner.appendResult(result)
}

func (runner *matrixRunner) runCancellations() error {
	for _, cancellation := range cancellationCaseSpecs(runner.options.profile, runner.contract) {
		result, err := runCancellationCase(context.Background(), runner.harness, filepath.Join(runner.casesDir, fmt.Sprintf("cancel-%s-%d", cancellation.Stage, cancellation.Percent)), cancellation.Items, cancellation.Workers, cancellation.Stage, cancellation.Percent, runner.contract.StopWithin)
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
	result, err := runner.harness.RunRegressionBenchmark(context.Background(), runner.contract.RegressionBenchmark)
	if err != nil {
		return fmt.Errorf("run regression benchmark: %w", err)
	}
	if err := runner.appendResult(result); err != nil {
		return err
	}
	items := profileItemCount(runner.options.profile, runner.options.smokeItems)
	for _, percent := range []int{0, 50, 99} {
		result, err := runResumeCase(context.Background(), runner.harness, filepath.Join(runner.casesDir, fmt.Sprintf("resume-%d", percent)), items, 16, percent)
		if err != nil {
			return fmt.Errorf("run resume percent=%d: %w", percent, err)
		}
		if err := runner.appendResult(result); err != nil {
			return err
		}
	}
	for _, scenario := range []string{"output-failure", "snapshot-save-failure", "pressure-fatal-error"} {
		result, err := runFailureCase(context.Background(), runner.harness, filepath.Join(runner.casesDir, "failure-"+scenario), fullOrBoundedItems(runner.options.profile, 100), 16, scenario)
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
		result, err := runAcceptedRichCase(context.Background(), runner.harness, filepath.Join(runner.casesDir, "rich-"+string(family)), fullOrBoundedItems(runner.options.profile, 100), 16, family)
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
		result, err := runner.harness.RunRichOversizeCase(context.Background(), perfharness.RichOversizeSpec{
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
	result, err := runWorkflowCase(context.Background(), runner.harness, filepath.Join(runner.casesDir, "workflow-crlf-workers-16"), items, 16, "CRLF")
	if err != nil {
		return fmt.Errorf("run CRLF workflow: %w", err)
	}
	if err := runner.appendResult(result); err != nil {
		return err
	}
	for _, workers := range runner.contract.LoopbackWorkers {
		result, err := runLoopbackCase(context.Background(), runner.harness, filepath.Join(runner.casesDir, "loopback-workers-"+strconv.Itoa(workers)), workers)
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
	paths, err := runner.harness.WriteReports(context.Background(), filepath.Join(runner.options.outputDir, "report"), report)
	if err != nil {
		_ = writeStatus(runner.stderr, "write reports: %v\n", err)
		return 1
	}
	if !passed {
		_ = writeStatus(runner.stderr, "performance matrix failed one or more thresholds: JSON=%s Markdown=%s\n", paths.JSON, paths.Markdown)
		return 1
	}
	if err := writeStatus(runner.stdout, "performance matrix passed: JSON=%s Markdown=%s\n", paths.JSON, paths.Markdown); err != nil {
		return 1
	}
	return 0
}

func (runner *matrixRunner) evaluate() bool {
	passed := applyAbsoluteThresholds(runner.results, runner.harness, runner.contract)
	if runner.options.profile == "full" {
		passed = applyFixtureCaseContract(runner.results, runner.contract.FixtureCases) && passed
	} else {
		passed = applySnapshotCaseContract(runner.results, runner.specs) && passed
	}
	passed = applyWorkerParity(runner.results, runner.harness, runner.contract.FakeWorkers) && passed
	items := profileItemCount(runner.options.profile, runner.options.smokeItems)
	if items >= 10 {
		passed = applyGrowthThreshold(runner.results, runner.harness, "production-workflow/complete/growth-1x/workers-16", "production-workflow/complete/workers-16") && passed
	}
	if items >= runner.contract.SmokeItems {
		passed = applyWorkerMemoryThreshold(runner.results, runner.harness) && passed
	}
	passed = applyOutputThresholds(runner.results, runner.harness, runner.outputScales) && passed
	return applyInputAndSnapshotGrowthThresholds(runner.results, runner.harness) && passed
}

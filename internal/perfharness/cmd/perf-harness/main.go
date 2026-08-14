package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/xuxiping/port-scan-mk3/internal/perfharness"
)

func main() {
	os.Exit(runCommand(os.Args[1:], os.Stdout, os.Stderr))
}

func applySnapshotCaseContract(results []perfharness.CaseResult, specs []perfharness.FixtureSpec) bool {
	for _, result := range results {
		if strings.HasPrefix(result.Name, "snapshot-heavy/") {
			return false
		}
	}
	for _, spec := range specs {
		if spec.Family != perfharness.FamilySnapshotHeavy {
			continue
		}
		loadName, saveName := perfharness.SnapshotCaseNames(spec)
		if findCase(results, loadName) == nil || findCase(results, saveName) == nil {
			return false
		}
	}
	return true
}

func applyFixtureCaseContract(results []perfharness.CaseResult, mappings []perfharness.FixtureCaseMapping) bool {
	for _, mapping := range mappings {
		wantItems := fixtureLogicalItems(mapping.Fixture)
		if len(mapping.CaseNames) == 0 || wantItems == 0 {
			return false
		}
		for _, name := range mapping.CaseNames {
			result := findCase(results, name)
			if result == nil || result.LogicalItems != wantItems {
				return false
			}
		}
	}
	return true
}

func fixtureLogicalItems(spec perfharness.FixtureSpec) uint64 {
	for _, count := range []uint64{
		spec.Scale.ExpectedOutputs,
		spec.Scale.ProbeTasks,
		spec.Scale.CandidateAddresses,
		spec.Scale.InputRecords,
		spec.Scale.TargetBytes,
	} {
		if count > 0 {
			return count
		}
	}
	return 0
}

type evaluator func(perfharness.EvaluationInput) perfharness.Verdict
type semanticComparator func(perfharness.SemanticArtifact, perfharness.SemanticArtifact) []string
type reportComparator func(perfharness.Report, perfharness.Report) []string

func applyInputAndSnapshotGrowthThresholds(results []perfharness.CaseResult, evaluate evaluator) bool {
	sequences := [][]string{
		{
			"record-heavy/one-megabyte",
			"record-heavy/ten-megabytes",
			"record-heavy/one-hundred-megabytes",
			"record-heavy/one-gigabyte",
		},
		{
			"snapshot-load/mixed/one-megabyte",
			"snapshot-load/mixed/ten-megabytes",
			"snapshot-load/mixed/one-hundred-megabytes",
			"snapshot-load/mixed/one-gigabyte",
		},
		{
			"snapshot-save/mixed/one-megabyte",
			"snapshot-save/mixed/ten-megabytes",
			"snapshot-save/mixed/one-hundred-megabytes",
			"snapshot-save/mixed/one-gigabyte",
		},
	}
	passed := true
	for _, sequence := range sequences {
		for index := 1; index < len(sequence); index++ {
			if findCase(results, sequence[index-1]) == nil || findCase(results, sequence[index]) == nil {
				continue
			}
			if !applyGrowthThreshold(results, evaluate, sequence[index-1], sequence[index]) {
				passed = false
			}
		}
	}
	return passed
}

func outputSpecs(profile string, smokeItems uint64) []perfharness.OutputSpec {
	counts := []uint64{smokeItems}
	if profile == "full" {
		counts = []uint64{10_000, 100_000, 1_000_000, 10_000_000}
	}
	specs := make([]perfharness.OutputSpec, 0, len(counts)*3)
	for _, count := range counts {
		for _, interval := range []int{1, 1000, 0} {
			specs = append(specs, perfharness.OutputSpec{Results: count, FlushResults: interval})
		}
	}
	return specs
}

func requiredOutputBytes(specs []perfharness.OutputSpec) uint64 {
	var largest uint64
	for _, spec := range specs {
		if spec.Results > largest {
			largest = spec.Results
		}
	}
	const estimatedBytesPerResultForBothFiles = uint64(512)
	return largest * estimatedBytesPerResultForBothFiles
}

func applyOutputThresholds(results []perfharness.CaseResult, evaluate evaluator, scales []uint64) bool {
	passed := true
	for _, interval := range []int{1, 1000, 0} {
		for index := 1; index < len(scales); index++ {
			smallName := fmt.Sprintf("output-heavy/results-%d/flush-%d", scales[index-1], interval)
			largeName := fmt.Sprintf("output-heavy/results-%d/flush-%d", scales[index], interval)
			if !applyGrowthThreshold(results, evaluate, smallName, largeName) {
				passed = false
			}
		}
	}
	if len(scales) < 2 {
		return passed
	}
	for _, scale := range scales[len(scales)-2:] {
		each := findCase(results, fmt.Sprintf("output-heavy/results-%d/flush-1", scale))
		batched := findCase(results, fmt.Sprintf("output-heavy/results-%d/flush-1000", scale))
		disabled := findCase(results, fmt.Sprintf("output-heavy/results-%d/flush-0", scale))
		if each == nil || batched == nil || disabled == nil {
			passed = false
			continue
		}
		if batched.SteadyMedian.WallTime > each.SteadyMedian.WallTime/2 {
			batched.Verdict.Passed = false
			batched.Verdict.Failures = append(batched.Verdict.Failures, perfharness.Failure{
				Rule:   "output-flush-vs-each",
				Detail: "flush interval 1000 is less than twice as fast as interval 1",
			})
			passed = false
		}
		if float64(batched.SteadyMedian.WallTime) > float64(disabled.SteadyMedian.WallTime)*1.15 {
			batched.Verdict.Passed = false
			batched.Verdict.Failures = append(batched.Verdict.Failures, perfharness.Failure{
				Rule:   "output-flush-vs-disabled",
				Detail: "flush interval 1000 is more than 15 percent slower than disabled periodic flushes",
			})
			passed = false
		}
	}
	return passed
}

func applyWorkerMemoryThreshold(results []perfharness.CaseResult, evaluate evaluator) bool {
	workers16 := findCase(results, "scan-orchestration/workers-16")
	workers256 := findCase(results, "scan-orchestration/workers-256")
	if workers16 == nil || workers256 == nil {
		return false
	}
	comparisons := []perfharness.WorkerComparison{
		{Workers16Bytes: workerMemory(workers16.ColdStart), Workers256Bytes: workerMemory(workers256.ColdStart)},
		{Workers16Bytes: workerMemory(workers16.SteadyMedian), Workers256Bytes: workerMemory(workers256.SteadyMedian)},
	}
	for _, comparison := range comparisons {
		verdict := evaluate(perfharness.EvaluationInput{Workers: &comparison})
		if !verdict.Passed {
			workers256.Verdict.Passed = false
			workers256.Verdict.Failures = append(workers256.Verdict.Failures, verdict.Failures...)
		}
	}
	return workers256.Verdict.Passed
}

func applyAbsoluteThresholds(results []perfharness.CaseResult, evaluate evaluator, contract perfharness.Contract) bool {
	passed := true
	for index := range results {
		budget, ok := absoluteBudgetFor(results[index].Name, contract.AbsoluteBudgets)
		if !ok {
			results[index].Verdict.Passed = false
			results[index].Verdict.Failures = append(results[index].Verdict.Failures, perfharness.Failure{
				Rule:   "absolute-budget-missing",
				Detail: "case has no absolute budget",
			})
			passed = false
			continue
		}
		for _, observation := range []perfharness.Observation{results[index].ColdStart, results[index].SteadyMedian} {
			verdict := evaluate(perfharness.EvaluationInput{Observation: observation, Absolute: budget})
			if !verdict.Passed {
				results[index].Verdict.Passed = false
				results[index].Verdict.Failures = append(results[index].Verdict.Failures, verdict.Failures...)
				passed = false
			}
		}
		if !results[index].Verdict.Passed {
			passed = false
		}
	}
	return passed
}

func applyGrowthThreshold(results []perfharness.CaseResult, evaluate evaluator, smallName, largeName string) bool {
	small := findCase(results, smallName)
	large := findCase(results, largeName)
	if small == nil || large == nil {
		return false
	}
	verdict := evaluate(perfharness.EvaluationInput{Growth: &perfharness.GrowthComparison{
		Small: small.SteadyMedian,
		Large: large.SteadyMedian,
	}})
	if !verdict.Passed {
		large.Verdict.Passed = false
		large.Verdict.Failures = append(large.Verdict.Failures, verdict.Failures...)
	}
	return verdict.Passed
}

func applyWorkerParity(results []perfharness.CaseResult, compareSemantic semanticComparator, workers []int) bool {
	if len(workers) == 0 {
		return false
	}
	baseline := findCase(results, "scan-orchestration/workers-"+strconv.Itoa(workers[0]))
	if baseline == nil || baseline.Semantic == nil {
		return false
	}
	passed := true
	for _, workerCount := range workers[1:] {
		candidate := findCase(results, "scan-orchestration/workers-"+strconv.Itoa(workerCount))
		if candidate == nil || candidate.Semantic == nil {
			passed = false
			continue
		}
		differences := compareSemantic(*baseline.Semantic, *candidate.Semantic)
		if len(differences) == 0 {
			continue
		}
		candidate.Verdict.Passed = false
		candidate.Verdict.Failures = append(candidate.Verdict.Failures, perfharness.Failure{
			Rule:   "semantic-parity",
			Detail: "worker profiles differ in " + strings.Join(differences, ", "),
		})
		candidate.Correctness.Detail = "worker semantic parity failed"
		passed = false
	}
	return passed
}

func absoluteBudgetFor(name string, budgets []perfharness.CaseBudget) (perfharness.AbsoluteBudget, bool) {
	var fallback *perfharness.AbsoluteBudget
	for index := range budgets {
		if budgets[index].NamePrefix == "" {
			fallback = &budgets[index].Budget
			continue
		}
		if strings.HasPrefix(name, budgets[index].NamePrefix) {
			return budgets[index].Budget, true
		}
	}
	if fallback != nil {
		return *fallback, true
	}
	return perfharness.AbsoluteBudget{}, false
}

func workerMemory(observation perfharness.Observation) uint64 {
	if observation.LinuxPeakRSSBytes > 0 {
		return observation.LinuxPeakRSSBytes
	}
	if observation.WindowsWorkingSetBytes > 0 {
		return observation.WindowsWorkingSetBytes
	}
	return observation.PeakCommittedBytes
}

func findCase(results []perfharness.CaseResult, name string) *perfharness.CaseResult {
	for index := range results {
		if results[index].Name == name {
			return &results[index]
		}
	}
	return nil
}

func writeStatus(output io.Writer, format string, values ...any) error {
	_, err := fmt.Fprintf(output, format, values...)
	return err
}

func compareReportFiles(leftPath, rightPath string, compareReports reportComparator, stdout, stderr io.Writer) int {
	read := func(path string) (perfharness.Report, error) {
		data, err := os.ReadFile(path)
		if err != nil {
			return perfharness.Report{}, fmt.Errorf("read report %s: %w", path, err)
		}
		var report perfharness.Report
		if err := json.Unmarshal(data, &report); err != nil {
			return perfharness.Report{}, fmt.Errorf("decode report %s: %w", path, err)
		}
		return report, nil
	}
	left, err := read(leftPath)
	if err != nil {
		if writeErr := writeStatus(stderr, "%v\n", err); writeErr != nil {
			return 1
		}
		return 1
	}
	right, err := read(rightPath)
	if err != nil {
		if writeErr := writeStatus(stderr, "%v\n", err); writeErr != nil {
			return 1
		}
		return 1
	}
	differences := compareReports(left, right)
	if len(differences) != 0 {
		if writeErr := writeStatus(stderr, "semantic parity failed: %s\n", strings.Join(differences, ", ")); writeErr != nil {
			return 1
		}
		return 1
	}
	if err := writeStatus(stdout, "semantic parity passed\n"); err != nil {
		return 1
	}
	return 0
}

func fixtureSpecs(profile string, items, snapshotBytes uint64, contract perfharness.Contract) []perfharness.FixtureSpec {
	if profile == "full" {
		return contract.FullFixtures
	}
	return []perfharness.FixtureSpec{
		{Family: perfharness.FamilyRecordHeavy, Scale: perfharness.Scale{InputRecords: items}, Seed: perfharness.DefaultGeneratorSeed},
		{Family: perfharness.FamilySnapshotHeavy, Shape: "mixed", Scale: perfharness.Scale{TargetBytes: snapshotBytes}, Seed: perfharness.DefaultGeneratorSeed},
	}
}

func profileItemCount(profile string, smokeItems uint64) uint64 {
	if profile == "full" {
		return perfharness.FullItemCount
	}
	return smokeItems
}

func fullOrBoundedItems(profile string, bounded uint64) uint64 {
	if profile == "full" {
		return perfharness.FullItemCount
	}
	return bounded
}

type fixtureRoute uint8

const (
	fixtureRouteNone fixtureRoute = iota
	fixtureRouteValidate
	fixtureRoutePrePing
	fixtureRoutePort
	fixtureRouteBuckets
	fixtureRouteOutput
	fixtureRouteSnapshot
	fixtureRouteResume
	fixtureRouteRich
	fixtureRouteRichDeny
)

func fixtureRouteFor(spec perfharness.FixtureSpec) (fixtureRoute, error) {
	switch spec.Family {
	case perfharness.FamilyRecordHeavy:
		return fixtureRouteValidate, nil
	case perfharness.FamilyCandidateHeavy:
		return fixtureRoutePrePing, nil
	case perfharness.FamilyPortHeavy:
		return fixtureRoutePort, nil
	case perfharness.FamilyTaskHeavy:
		return fixtureRouteBuckets, nil
	case perfharness.FamilyOutputHeavy:
		return fixtureRouteOutput, nil
	case perfharness.FamilySnapshotHeavy:
		return fixtureRouteSnapshot, nil
	case perfharness.FamilyResumeHeavy:
		return fixtureRouteResume, nil
	case perfharness.FamilyRichRecordMixed,
		perfharness.FamilyRichUniqueKey,
		perfharness.FamilyRichHotKey,
		perfharness.FamilyRichPrecheck:
		return fixtureRouteRich, nil
	case perfharness.FamilyRichDeny:
		return fixtureRouteRichDeny, nil
	default:
		return fixtureRouteNone, fmt.Errorf("fixture family %q has no production route", spec.Family)
	}
}

func runWorkflowCase(ctx context.Context, compareSemantic semanticComparator, outputDir string, items uint64, workers int, lineEnding string, runWorkflow workflowRunner) (perfharness.CaseResult, error) {
	name := fmt.Sprintf("production-workflow/complete/workers-%d", workers)
	if lineEnding == "CRLF" {
		name = fmt.Sprintf("production-workflow/complete/crlf/workers-%d", workers)
	}
	return runRepeatedWorkflowCase(ctx, compareSemantic, outputDir, items, workers, lineEnding, name, runWorkflow)
}

func runOrchestrationCase(ctx context.Context, compareSemantic semanticComparator, outputDir string, items uint64, workers int, runWorkflow workflowRunner) (perfharness.CaseResult, error) {
	return runRepeatedWorkflowCase(ctx, compareSemantic, outputDir, items, workers, "", fmt.Sprintf("scan-orchestration/workers-%d", workers), runWorkflow)
}

type workflowRunner func(context.Context, perfharness.WorkflowSpec) (perfharness.WorkflowResult, error)

func runRepeatedWorkflowCase(ctx context.Context, compareSemantic semanticComparator, outputDir string, items uint64, workers int, lineEnding, name string, runWorkflow workflowRunner) (perfharness.CaseResult, error) {
	if err := os.Mkdir(outputDir, 0o755); err != nil {
		return perfharness.CaseResult{}, fmt.Errorf("create workflow case directory: %w", err)
	}
	observations := make([]perfharness.Observation, 0, 6)
	fixtureObservations := make([]perfharness.Observation, 0, 6)
	correct := true
	var semantic perfharness.SemanticArtifact
	var expansionOverride *perfharness.ExpansionOverride
	for run := 0; run < 6; run++ {
		runDir := filepath.Join(outputDir, fmt.Sprintf("run-%d", run))
		workflow, err := runWorkflow(ctx, perfharness.WorkflowSpec{OutputDir: runDir, Items: items, Workers: workers, LineEnding: lineEnding})
		if err != nil {
			return perfharness.CaseResult{}, err
		}
		if workflow.ProbeCount != items || workflow.ScanRows != items || workflow.OpenRows != items || !workflow.SnapshotCompleted {
			correct = false
			return perfharness.CaseResult{}, fmt.Errorf("workflow counts are probes=%d scan=%d open=%d, want %d", workflow.ProbeCount, workflow.ScanRows, workflow.OpenRows, items)
		}
		fixtureObservations = append(fixtureObservations, workflow.FixtureGeneration)
		observations = append(observations, workflow.Stage)
		if run == 0 {
			semantic = workflow.Semantic
			expansionOverride = workflow.ExpansionOverride
		} else if differences := compareSemantic(semantic, workflow.Semantic); len(differences) != 0 {
			return perfharness.CaseResult{}, fmt.Errorf("workflow run parity differs in %s", strings.Join(differences, ", "))
		} else if !reflect.DeepEqual(expansionOverride, workflow.ExpansionOverride) {
			return perfharness.CaseResult{}, fmt.Errorf("workflow expansion override changed between runs")
		}
		if err := removeCompletedCaseRun(outputDir, run); err != nil {
			return perfharness.CaseResult{}, err
		}
	}
	result, err := perfharness.SummarizeCase(name, observations)
	if err != nil {
		return perfharness.CaseResult{}, err
	}
	fixtureGeneration, err := perfharness.SummarizePhase("production-workflow fixture generation", fixtureObservations)
	if err != nil {
		return perfharness.CaseResult{}, err
	}
	result.FixtureGeneration = &fixtureGeneration
	result.LogicalItems = items
	detail := ""
	if expansionOverride != nil {
		detail = fmt.Sprintf("Explicit compact-fixture limits: %d raw candidates and %d GB. %s", expansionOverride.CandidateLimit, expansionOverride.MemoryLimitGB, expansionOverride.Reason)
	}
	result.Correctness = perfharness.Correctness{Headers: correct, RowCounts: correct, SnapshotProgress: correct, ExpectedValues: correct, Digests: correct, Detail: detail}
	result.Verdict = perfharness.Verdict{Passed: correct}
	result.Semantic = &semantic
	return result, nil
}

func runLoopbackCase(ctx context.Context, runLoopback workflowRunner, outputDir string, workers int) (perfharness.CaseResult, error) {
	if err := os.Mkdir(outputDir, 0o755); err != nil {
		return perfharness.CaseResult{}, fmt.Errorf("create loopback case directory: %w", err)
	}
	observations := make([]perfharness.Observation, 0, 6)
	fixtureObservations := make([]perfharness.Observation, 0, 6)
	for run := 0; run < 6; run++ {
		runDir := filepath.Join(outputDir, fmt.Sprintf("run-%d", run))
		workflow, err := runLoopback(ctx, perfharness.WorkflowSpec{OutputDir: runDir, Items: 1, Workers: workers})
		if err != nil {
			return perfharness.CaseResult{}, err
		}
		if workflow.ProbeCount != 1 || workflow.ScanRows != 1 || workflow.OpenRows != 1 {
			return perfharness.CaseResult{}, fmt.Errorf("loopback counts are probes=%d scan=%d open=%d", workflow.ProbeCount, workflow.ScanRows, workflow.OpenRows)
		}
		fixtureObservations = append(fixtureObservations, workflow.FixtureGeneration)
		observations = append(observations, workflow.Stage)
		if err := removeCompletedCaseRun(outputDir, run); err != nil {
			return perfharness.CaseResult{}, err
		}
	}
	result, err := perfharness.SummarizeCase(fmt.Sprintf("native-loopback/workers-%d", workers), observations)
	if err != nil {
		return perfharness.CaseResult{}, err
	}
	fixtureGeneration, err := perfharness.SummarizePhase("native-loopback fixture generation", fixtureObservations)
	if err != nil {
		return perfharness.CaseResult{}, err
	}
	result.FixtureGeneration = &fixtureGeneration
	result.LogicalItems = 1
	result.Correctness = perfharness.Correctness{Headers: true, RowCounts: true, SnapshotProgress: true, ExpectedValues: true, Digests: true}
	result.Verdict = perfharness.Verdict{Passed: true}
	return result, nil
}

type richDenyRunner func(context.Context, perfharness.RichDenySpec) (perfharness.WorkflowResult, error)

func runRichDenyCase(ctx context.Context, runRichDeny richDenyRunner, outputDir string, items uint64, workers int, shape string) (perfharness.CaseResult, error) {
	if err := os.Mkdir(outputDir, 0o755); err != nil {
		return perfharness.CaseResult{}, fmt.Errorf("create rich-deny case directory: %w", err)
	}
	observations := make([]perfharness.Observation, 0, 6)
	fixtureObservations := make([]perfharness.Observation, 0, 6)
	for run := 0; run < 6; run++ {
		workflow, err := runRichDeny(ctx, perfharness.RichDenySpec{
			OutputDir: filepath.Join(outputDir, fmt.Sprintf("run-%d", run)),
			Items:     items,
			Workers:   workers,
			Shape:     shape,
		})
		if err != nil {
			return perfharness.CaseResult{}, err
		}
		if workflow.ProbeCount != 0 || workflow.ReachabilityCount != 0 || workflow.ScanRows != 0 || workflow.OpenRows != 0 || !workflow.SnapshotCompleted {
			return perfharness.CaseResult{}, fmt.Errorf("rich-deny shape %s performed denied work: %+v", shape, workflow)
		}
		fixtureObservations = append(fixtureObservations, workflow.FixtureGeneration)
		observations = append(observations, workflow.Stage)
		if err := removeCompletedCaseRun(outputDir, run); err != nil {
			return perfharness.CaseResult{}, err
		}
	}
	result, err := perfharness.SummarizeCase("production-rich-deny/"+shape, observations)
	if err != nil {
		return perfharness.CaseResult{}, err
	}
	fixtureGeneration, err := perfharness.SummarizePhase("production-rich-deny fixture generation", fixtureObservations)
	if err != nil {
		return perfharness.CaseResult{}, err
	}
	result.FixtureGeneration = &fixtureGeneration
	result.LogicalItems = items
	result.Correctness = perfharness.Correctness{Headers: true, RowCounts: true, SnapshotProgress: true, ExpectedValues: true, Digests: true}
	result.Verdict = perfharness.Verdict{Passed: true}
	return result, nil
}

type cancellationRunner func(context.Context, perfharness.CancellationSpec) (perfharness.CancellationResult, error)

func runCancellationCase(ctx context.Context, runCancellation cancellationRunner, outputDir string, items uint64, workers int, stage perfharness.CancellationStage, percent int, stopWithin time.Duration) (perfharness.CaseResult, error) {
	if err := os.Mkdir(outputDir, 0o755); err != nil {
		return perfharness.CaseResult{}, fmt.Errorf("create cancellation case directory: %w", err)
	}
	observations := make([]perfharness.Observation, 0, 6)
	preparations := make([]perfharness.Observation, 0, 6)
	evidence := make([]perfharness.CancellationResult, 0, 6)
	correct := true
	for run := 0; run < 6; run++ {
		cancellation, runErr := runCancellation(ctx, perfharness.CancellationSpec{
			OutputDir: filepath.Join(outputDir, fmt.Sprintf("run-%d", run)),
			Items:     items,
			Workers:   workers,
			Stage:     stage,
			Percent:   percent,
		})
		if !errors.Is(runErr, context.Canceled) {
			return perfharness.CaseResult{}, fmt.Errorf("cancellation stage %s did not return context.Canceled: %w", stage, runErr)
		}
		if !cancellation.Injected || cancellation.StopDuration > stopWithin ||
			!cancellation.ContextCanceled || cancellation.TotalItems != items ||
			cancellation.InjectionThreshold != cancellationThresholdForCase(items, percent) ||
			cancellation.CompletedAtInjection < cancellation.InjectionThreshold || cancellation.ProgressUnit == "" ||
			cancellation.ProbeStartsAfterCancel != 0 {
			correct = false
		}
		if stage == perfharness.CancellationResumeRebuild || stage == perfharness.CancellationResultOutput {
			if cancellation.Recovery == nil || !cancellation.Recovery.RecoveryCompleted ||
				(cancellation.Recovery.Remaining > 0 && !cancellation.Resumable) {
				correct = false
			}
		}
		observations = append(observations, cancellation.StageObservation)
		preparations = append(preparations, cancellation.Preparation)
		evidence = append(evidence, cancellation)
		if err := removeCompletedCaseRun(outputDir, run); err != nil {
			return perfharness.CaseResult{}, err
		}
	}
	result, err := perfharness.SummarizeCase(fmt.Sprintf("production-cancellation/%s/%d", stage, percent), observations)
	if err != nil {
		return perfharness.CaseResult{}, err
	}
	result.Correctness = perfharness.Correctness{Headers: true, RowCounts: true, SnapshotProgress: correct, ExpectedValues: correct, Digests: true}
	preparation, err := perfharness.SummarizePhase(result.Name+" preparation", preparations)
	if err != nil {
		return perfharness.CaseResult{}, err
	}
	result.FixtureGeneration = &preparation
	result.Cancellation = &perfharness.CancellationCaseEvidence{SchemaVersion: perfharness.CancellationEvidenceSchemaVersion, Runs: evidence}
	result.LogicalItems = items
	result.Verdict = perfharness.Verdict{Passed: correct}
	if !correct {
		result.Verdict.Failures = []perfharness.Failure{{Rule: "cancellation-stop", Detail: "production work did not stop within one second"}}
	}
	return result, nil
}

type cancellationCaseSpec struct {
	Items   uint64
	Workers int
	Stage   perfharness.CancellationStage
	Percent int
}

func cancellationCaseSpecs(profile string, contract perfharness.Contract) []cancellationCaseSpec {
	items := fullOrBoundedItems(profile, 1000)
	result := make([]cancellationCaseSpec, 0, len(contract.CancelStages)*len(contract.CancelProgress))
	for _, stage := range contract.CancelStages {
		for _, percent := range contract.CancelProgress {
			result = append(result, cancellationCaseSpec{Items: items, Workers: 16, Stage: stage, Percent: percent})
		}
	}
	return result
}

func cancellationThresholdForCase(total uint64, percent int) uint64 {
	return (total*uint64(percent) + 99) / 100
}

type resumeRunner func(context.Context, perfharness.ResumeSpec) (perfharness.WorkflowResult, error)

func runResumeCase(ctx context.Context, runResume resumeRunner, compareSemantic semanticComparator, outputDir string, items uint64, workers, percent int) (perfharness.CaseResult, error) {
	observations := make([]perfharness.Observation, 0, 6)
	var semantic perfharness.SemanticArtifact
	wantRemaining := items - items*uint64(percent)/100
	for run := 0; run < 6; run++ {
		workflow, err := runResume(ctx, perfharness.ResumeSpec{
			OutputDir:        filepath.Join(outputDir, fmt.Sprintf("run-%d", run)),
			Items:            items,
			Workers:          workers,
			CompletedPercent: percent,
		})
		if err != nil {
			return perfharness.CaseResult{}, err
		}
		if workflow.ProbeCount != wantRemaining || workflow.ScanRows != wantRemaining || workflow.OpenRows != wantRemaining {
			return perfharness.CaseResult{}, fmt.Errorf("resume percent %d produced %+v, want %d remaining", percent, workflow, wantRemaining)
		}
		if run == 0 {
			semantic = workflow.Semantic
		} else if differences := compareSemantic(semantic, workflow.Semantic); len(differences) != 0 {
			return perfharness.CaseResult{}, fmt.Errorf("resume run parity differs in %s", strings.Join(differences, ", "))
		}
		observations = append(observations, workflow.Stage)
		if err := removeCompletedCaseRun(outputDir, run); err != nil {
			return perfharness.CaseResult{}, err
		}
	}
	result, err := perfharness.SummarizeCase(fmt.Sprintf("production-resume/%d", percent), observations)
	if err != nil {
		return perfharness.CaseResult{}, err
	}
	result.Semantic = &semantic
	result.LogicalItems = items
	result.Correctness = perfharness.Correctness{Headers: true, RowCounts: true, SnapshotProgress: true, ExpectedValues: true, Digests: true}
	result.Verdict = perfharness.Verdict{Passed: true}
	return result, nil
}

type failureRunner func(context.Context, perfharness.FailureSpec) (perfharness.FailureResult, error)

func runFailureCase(ctx context.Context, runFailure failureRunner, outputDir string, items uint64, workers int, scenario string) (perfharness.CaseResult, error) {
	observations := make([]perfharness.Observation, 0, 6)
	preparations := make([]perfharness.Observation, 0, 6)
	evidence := make([]perfharness.FailureResult, 0, 6)
	correct := true
	for run := 0; run < 6; run++ {
		failure, err := runFailure(ctx, perfharness.FailureSpec{
			OutputDir: filepath.Join(outputDir, fmt.Sprintf("run-%d", run)),
			Items:     items,
			Workers:   workers,
			Scenario:  scenario,
		})
		if err != nil {
			return perfharness.CaseResult{}, err
		}
		if !failure.Correct() || failure.TotalItems != items {
			correct = false
		}
		observations = append(observations, failure.StageObservation)
		preparations = append(preparations, failure.Preparation)
		evidence = append(evidence, failure)
		if err := removeCompletedCaseRun(outputDir, run); err != nil {
			return perfharness.CaseResult{}, err
		}
	}
	result, err := perfharness.SummarizeCase("production-failure/"+scenario, observations)
	if err != nil {
		return perfharness.CaseResult{}, err
	}
	result.Correctness = perfharness.Correctness{Headers: true, RowCounts: true, SnapshotProgress: true, ExpectedValues: correct, Digests: true}
	preparation, err := perfharness.SummarizePhase(result.Name+" preparation", preparations)
	if err != nil {
		return perfharness.CaseResult{}, err
	}
	result.FixtureGeneration = &preparation
	result.Failure = &perfharness.FailureCaseEvidence{SchemaVersion: perfharness.FailureEvidenceSchemaVersion, Runs: evidence}
	result.LogicalItems = items
	result.Verdict = perfharness.Verdict{Passed: correct}
	return result, nil
}

type richRunner func(context.Context, perfharness.RichSpec) (perfharness.WorkflowResult, error)

func runAcceptedRichCase(ctx context.Context, runRich richRunner, compareSemantic semanticComparator, outputDir string, items uint64, workers int, family perfharness.Family) (perfharness.CaseResult, error) {
	observations := make([]perfharness.Observation, 0, 6)
	fixtureObservations := make([]perfharness.Observation, 0, 6)
	want := items
	if family == perfharness.FamilyRichHotKey {
		want = min(items, uint64(4))
	}
	var semantic perfharness.SemanticArtifact
	for run := 0; run < 6; run++ {
		workflow, err := runRich(ctx, perfharness.RichSpec{
			OutputDir: filepath.Join(outputDir, fmt.Sprintf("run-%d", run)),
			Items:     items,
			Workers:   workers,
			Family:    family,
		})
		if err != nil {
			return perfharness.CaseResult{}, err
		}
		if workflow.ProbeCount != want || workflow.ReachabilityCount != want || workflow.ScanRows != want || workflow.OpenRows != want {
			return perfharness.CaseResult{}, fmt.Errorf("rich family %s produced %+v, want %d", family, workflow, want)
		}
		if run == 0 {
			semantic = workflow.Semantic
		} else if differences := compareSemantic(semantic, workflow.Semantic); len(differences) != 0 {
			return perfharness.CaseResult{}, fmt.Errorf("rich run parity differs in %s", strings.Join(differences, ", "))
		}
		fixtureObservations = append(fixtureObservations, workflow.FixtureGeneration)
		observations = append(observations, workflow.Stage)
		if err := removeCompletedCaseRun(outputDir, run); err != nil {
			return perfharness.CaseResult{}, err
		}
	}
	result, err := perfharness.SummarizeCase("production-rich/"+string(family), observations)
	if err != nil {
		return perfharness.CaseResult{}, err
	}
	fixtureGeneration, err := perfharness.SummarizePhase("production-rich fixture generation", fixtureObservations)
	if err != nil {
		return perfharness.CaseResult{}, err
	}
	result.FixtureGeneration = &fixtureGeneration
	result.LogicalItems = items
	result.Semantic = &semantic
	result.Correctness = perfharness.Correctness{Headers: true, RowCounts: true, SnapshotProgress: true, ExpectedValues: true, Digests: true}
	result.Verdict = perfharness.Verdict{Passed: true}
	return result, nil
}

func removeCompletedCaseRun(outputDir string, run int) error {
	if run == 0 {
		return nil
	}
	path := filepath.Join(outputDir, fmt.Sprintf("run-%d", run))
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("remove completed case run %s: %w", path, err)
	}
	return nil
}

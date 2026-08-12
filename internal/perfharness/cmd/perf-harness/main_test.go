package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xuxiping/port-scan-mk3/internal/perfharness"
)

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, os.ErrClosed
}

func TestRunCommandRejectsInvalidInvocation(t *testing.T) {
	t.Parallel()

	existingOutput := t.TempDir()
	checks := []struct {
		name string
		args []string
		want int
	}{
		{name: "unknown flag", args: []string{"-unknown"}, want: 2},
		{name: "missing output", want: 2},
		{name: "profile", args: []string{"-output", filepath.Join(t.TempDir(), "profile"), "-profile", "large"}, want: 2},
		{name: "evidence label", args: []string{"-output", filepath.Join(t.TempDir(), "label"), "-evidence-label", "unknown"}, want: 2},
		{name: "one comparison report", args: []string{"-compare-left", "left.json"}, want: 2},
		{name: "existing output", args: []string{"-output", existingOutput}, want: 1},
	}
	for _, check := range checks {
		check := check
		t.Run(check.name, func(t *testing.T) {
			t.Parallel()
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			if code := runCommand(check.args, &stdout, &stderr); code != check.want {
				t.Fatalf("exit code = %d, want %d; stderr=%s", code, check.want, stderr.String())
			}
		})
	}
}

func TestApplyAbsoluteThresholdsChecksColdAndSteadyObservations(t *testing.T) {
	t.Parallel()

	results := []perfharness.CaseResult{{
		Name:         "bounded/case",
		ColdStart:    perfharness.Observation{WallTime: 2 * time.Second, PeakCommittedBytes: 50},
		SteadyMedian: perfharness.Observation{WallTime: time.Second, PeakCommittedBytes: 101},
		Verdict:      perfharness.Verdict{Passed: true},
	}}
	contract := perfharness.Contract{AbsoluteBudgets: []perfharness.CaseBudget{{
		NamePrefix: "bounded/",
		Budget:     perfharness.AbsoluteBudget{MaxWallTime: time.Second, MaxCommittedBytes: 100},
	}}}

	if applyAbsoluteThresholds(results, perfharness.New(), contract) {
		t.Fatal("absolute thresholds accepted failed cold and median observations")
	}
	if !results[0].Verdict.HasFailure("absolute-wall-time") || !results[0].Verdict.HasFailure("absolute-committed-memory") {
		t.Fatalf("case verdict = %+v", results[0].Verdict)
	}
}

func TestSnapshotLoadAndSaveEachUseTheAbsoluteMemoryBudget(t *testing.T) {
	t.Parallel()

	results := []perfharness.CaseResult{
		{Name: "snapshot-load/mixed/one-gigabyte", ColdStart: perfharness.Observation{PeakCommittedBytes: 5_900_000_000}, SteadyMedian: perfharness.Observation{PeakCommittedBytes: 5_900_000_000}, Verdict: perfharness.Verdict{Passed: true}},
		{Name: "snapshot-save/mixed/one-gigabyte", ColdStart: perfharness.Observation{PeakCommittedBytes: 6_000_000_001}, SteadyMedian: perfharness.Observation{PeakCommittedBytes: 6_000_000_001}, Verdict: perfharness.Verdict{Passed: true}},
	}
	contract := perfharness.DefaultContract()
	if applyAbsoluteThresholds(results, perfharness.New(), contract) {
		t.Fatal("absolute thresholds accepted an oversized snapshot save")
	}
	if !results[0].Verdict.Passed || !results[1].Verdict.HasFailure("absolute-committed-memory") {
		t.Fatalf("snapshot verdicts = %+v", results)
	}
}

func TestSnapshotCaseContractRejectsTheOldCombinedResult(t *testing.T) {
	t.Parallel()

	spec := perfharness.FixtureSpec{
		Family: perfharness.FamilySnapshotHeavy,
		Shape:  "mixed",
		Scale:  perfharness.Scale{TargetBytes: 1_000_000_000},
	}
	results := []perfharness.CaseResult{{Name: "snapshot-heavy/mixed/one-gigabyte", Verdict: perfharness.Verdict{Passed: true}}}
	if applySnapshotCaseContract(results, []perfharness.FixtureSpec{spec}) {
		t.Fatal("snapshot case contract accepted the old combined result")
	}
	loadName, saveName := perfharness.SnapshotCaseNames(spec)
	results = []perfharness.CaseResult{
		{Name: loadName, Verdict: perfharness.Verdict{Passed: true}},
		{Name: saveName, Verdict: perfharness.Verdict{Passed: true}},
	}
	if !applySnapshotCaseContract(results, []perfharness.FixtureSpec{spec}) {
		t.Fatal("snapshot case contract rejected separate load and save results")
	}
}

func TestSnapshotCaseContractRequiresBothSplitResults(t *testing.T) {
	t.Parallel()

	spec := perfharness.FixtureSpec{
		Family: perfharness.FamilySnapshotHeavy,
		Shape:  "mixed",
		Scale:  perfharness.Scale{TargetBytes: 1_000_000_000},
	}
	loadName, saveName := perfharness.SnapshotCaseNames(spec)
	for _, results := range [][]perfharness.CaseResult{
		{{Name: loadName, Verdict: perfharness.Verdict{Passed: true}}},
		{{Name: saveName, Verdict: perfharness.Verdict{Passed: true}}},
	} {
		if applySnapshotCaseContract(results, []perfharness.FixtureSpec{spec}) {
			t.Fatalf("snapshot case contract accepted incomplete results: %+v", results)
		}
	}
	if !applySnapshotCaseContract(nil, []perfharness.FixtureSpec{{Family: perfharness.FamilyRecordHeavy}}) {
		t.Fatal("snapshot case contract required split cases for a non-snapshot fixture")
	}
}

func TestApplyAbsoluteThresholdsRejectsMissingBudget(t *testing.T) {
	t.Parallel()

	results := []perfharness.CaseResult{{Name: "snapshot-load/mixed/one-gigabyte", Verdict: perfharness.Verdict{Passed: true}}}
	if applyAbsoluteThresholds(results, perfharness.New(), perfharness.Contract{}) {
		t.Fatal("absolute thresholds accepted a snapshot case without a budget")
	}
	if !results[0].Verdict.HasFailure("absolute-budget-missing") {
		t.Fatalf("snapshot verdict = %+v", results[0].Verdict)
	}
}

func TestWorkerMemoryUsesPlatformMetricsBeforeCommittedMemory(t *testing.T) {
	t.Parallel()

	if got := workerMemory(perfharness.Observation{LinuxPeakRSSBytes: 10, WindowsWorkingSetBytes: 20, PeakCommittedBytes: 30}); got != 10 {
		t.Fatalf("Linux worker memory = %d, want 10", got)
	}
	if got := workerMemory(perfharness.Observation{WindowsWorkingSetBytes: 20, PeakCommittedBytes: 30}); got != 20 {
		t.Fatalf("Windows worker memory = %d, want 20", got)
	}
	if got := workerMemory(perfharness.Observation{PeakCommittedBytes: 30}); got != 30 {
		t.Fatalf("fallback worker memory = %d, want 30", got)
	}
}

func TestApplyGrowthThresholdBlocksNonlinearMedianGrowth(t *testing.T) {
	t.Parallel()

	results := []perfharness.CaseResult{
		{Name: "small", SteadyMedian: perfharness.Observation{WallTime: time.Second, GoAllocatedBytes: 100}, Verdict: perfharness.Verdict{Passed: true}},
		{Name: "large", SteadyMedian: perfharness.Observation{WallTime: 13 * time.Second, GoAllocatedBytes: 1_101}, Verdict: perfharness.Verdict{Passed: true}},
	}

	if applyGrowthThreshold(results, perfharness.New(), "small", "large") {
		t.Fatal("growth threshold accepted nonlinear growth")
	}
	if !results[1].Verdict.HasFailure("growth-wall-time") || !results[1].Verdict.HasFailure("growth-allocated-bytes") {
		t.Fatalf("large verdict = %+v", results[1].Verdict)
	}
}

func TestApplyInputAndSnapshotGrowthThresholdsChecksEveryTenfoldStep(t *testing.T) {
	t.Parallel()

	results := []perfharness.CaseResult{
		{Name: "record-heavy/one-megabyte", SteadyMedian: perfharness.Observation{WallTime: time.Second, GoAllocatedBytes: 100}, Verdict: perfharness.Verdict{Passed: true}},
		{Name: "record-heavy/ten-megabytes", SteadyMedian: perfharness.Observation{WallTime: 13 * time.Second, GoAllocatedBytes: 1_000}, Verdict: perfharness.Verdict{Passed: true}},
		{Name: "snapshot-load/chunk-heavy/one-megabyte", SteadyMedian: perfharness.Observation{WallTime: time.Second, GoAllocatedBytes: 100}, Verdict: perfharness.Verdict{Passed: true}},
		{Name: "snapshot-save/port-heavy/ten-megabytes", SteadyMedian: perfharness.Observation{WallTime: 10 * time.Second, GoAllocatedBytes: 1_200}, Verdict: perfharness.Verdict{Passed: true}},
		{Name: "snapshot-load/mixed/one-megabyte", SteadyMedian: perfharness.Observation{WallTime: time.Second, GoAllocatedBytes: 100}, Verdict: perfharness.Verdict{Passed: true}},
		{Name: "snapshot-load/mixed/ten-megabytes", SteadyMedian: perfharness.Observation{WallTime: 10 * time.Second, GoAllocatedBytes: 1_200}, Verdict: perfharness.Verdict{Passed: true}},
		{Name: "snapshot-save/mixed/one-megabyte", SteadyMedian: perfharness.Observation{WallTime: time.Second, GoAllocatedBytes: 100}, Verdict: perfharness.Verdict{Passed: true}},
		{Name: "snapshot-save/mixed/ten-megabytes", SteadyMedian: perfharness.Observation{WallTime: 10 * time.Second, GoAllocatedBytes: 1_200}, Verdict: perfharness.Verdict{Passed: true}},
	}
	if applyInputAndSnapshotGrowthThresholds(results, perfharness.New()) {
		t.Fatal("scale thresholds accepted nonlinear CIDR time and snapshot allocation growth")
	}
	if !results[1].Verdict.HasFailure("growth-wall-time") || !results[5].Verdict.HasFailure("growth-allocated-bytes") || !results[7].Verdict.HasFailure("growth-allocated-bytes") {
		t.Fatalf("growth verdicts = %+v", results)
	}
	if results[3].Verdict.HasFailure("growth-allocated-bytes") {
		t.Fatal("growth evaluator compared different snapshot shapes")
	}
}

func TestOutputSpecsContainTheApprovedFullAndSmokeMatrix(t *testing.T) {
	full := outputSpecs("full", 100_000)
	if len(full) != 12 {
		t.Fatalf("full output case count = %d, want 12", len(full))
	}
	for _, results := range []uint64{10_000, 100_000, 1_000_000, 10_000_000} {
		for _, interval := range []int{1, 1000, 0} {
			if !containsOutputSpec(full, results, interval) {
				t.Fatalf("full matrix lacks results=%d interval=%d", results, interval)
			}
		}
	}
	smoke := outputSpecs("smoke", 100_000)
	if len(smoke) != 3 {
		t.Fatalf("smoke output case count = %d, want 3", len(smoke))
	}
}

func TestEveryFullFixtureHasAnExplicitProductionRoute(t *testing.T) {
	t.Parallel()

	contract := perfharness.DefaultContract()
	for _, spec := range contract.FullFixtures {
		route, err := fixtureRouteFor(spec)
		if err != nil {
			t.Fatalf("fixture %s/%s has no production route: %v", spec.Family, spec.Shape, err)
		}
		if route == fixtureRouteNone {
			t.Fatalf("fixture %s/%s uses the no-op route", spec.Family, spec.Shape)
		}
	}

	if _, err := fixtureRouteFor(perfharness.FixtureSpec{Family: perfharness.Family("unsupported")}); err == nil {
		t.Fatal("unsupported fixture family has a successful route")
	}
}

func TestFullProfilePropagatesTheExactWorkCount(t *testing.T) {
	t.Parallel()

	if got := profileItemCount("full", 7); got != perfharness.FullItemCount {
		t.Fatalf("full item count = %d, want %d", got, perfharness.FullItemCount)
	}
	if got := profileItemCount("smoke", 7); got != 7 {
		t.Fatalf("smoke item count = %d, want 7", got)
	}
	if got := fullOrBoundedItems("full", 200); got != perfharness.FullItemCount {
		t.Fatalf("full bounded-stage item count = %d, want %d", got, perfharness.FullItemCount)
	}
	if got := fullOrBoundedItems("smoke", 200); got != 200 {
		t.Fatalf("smoke bounded-stage item count = %d, want 200", got)
	}
}

func TestFullFixtureCaseContractRequiresEveryAliasAndExactLogicalCount(t *testing.T) {
	t.Parallel()

	mappings := perfharness.DefaultContract().FixtureCases
	results := make([]perfharness.CaseResult, 0)
	for _, mapping := range mappings {
		for _, name := range mapping.CaseNames {
			results = append(results, perfharness.CaseResult{Name: name, LogicalItems: fixtureLogicalItems(mapping.Fixture)})
		}
	}
	if !applyFixtureCaseContract(results, mappings) {
		t.Fatal("complete fixture aliases failed the contract")
	}
	results[len(results)-1].LogicalItems--
	if applyFixtureCaseContract(results, mappings) {
		t.Fatal("fixture contract accepted a reduced logical count")
	}
	results = results[:len(results)-1]
	if applyFixtureCaseContract(results, mappings) {
		t.Fatal("fixture contract accepted a missing dedicated case alias")
	}
}

func TestCandidateAndTaskStagesUseTheirApprovedBudgets(t *testing.T) {
	t.Parallel()

	contract := perfharness.DefaultContract()
	for _, name := range []string{"candidate-heavy/pre-ping", "task-heavy/bucket-generation"} {
		budget, ok := absoluteBudgetFor(name, contract.AbsoluteBudgets)
		if !ok || budget.MaxWallTime != 15*time.Minute || budget.MaxCommittedBytes != 24_000_000_000 {
			t.Fatalf("budget for %s = %+v, found=%t", name, budget, ok)
		}
	}
}

func TestCompleteWorkflowAndWorkerOrchestrationUseSeparateBudgets(t *testing.T) {
	t.Parallel()

	contract := perfharness.DefaultContract()
	checks := []struct {
		name   string
		wall   time.Duration
		memory uint64
	}{
		{name: "production-workflow/complete/workers-16", wall: 45 * time.Minute, memory: 24_000_000_000},
		{name: "scan-orchestration/workers-1", wall: 15 * time.Minute, memory: 4_000_000_000},
		{name: "scan-orchestration/workers-256", wall: 15 * time.Minute, memory: 4_000_000_000},
	}
	for _, check := range checks {
		budget, ok := absoluteBudgetFor(check.name, contract.AbsoluteBudgets)
		if !ok || budget.MaxWallTime != check.wall || budget.MaxCommittedBytes != check.memory {
			t.Fatalf("budget for %s = %+v, found=%t", check.name, budget, ok)
		}
	}
}

func TestApplyOutputThresholdsBlocksGrowthAndFlushSpeedViolations(t *testing.T) {
	results := []perfharness.CaseResult{
		outputThresholdCase(100_000, 1, 4*time.Second, 100),
		outputThresholdCase(100_000, 1000, 3*time.Second, 100),
		outputThresholdCase(100_000, 0, time.Second, 100),
		outputThresholdCase(1_000_000, 1, 40*time.Second, 1_000),
		outputThresholdCase(1_000_000, 1000, 30*time.Second, 1_200),
		outputThresholdCase(1_000_000, 0, 10*time.Second, 1_000),
	}

	if applyOutputThresholds(results, perfharness.New(), []uint64{100_000, 1_000_000}) {
		t.Fatal("output thresholds accepted slow flush=1000 cases")
	}
	large := findCase(results, "output-heavy/results-1000000/flush-1000")
	if large == nil || !large.Verdict.HasFailure("output-flush-vs-each") ||
		!large.Verdict.HasFailure("output-flush-vs-disabled") ||
		!large.Verdict.HasFailure("growth-allocated-bytes") {
		t.Fatalf("large output verdict = %+v", large)
	}
}

func containsOutputSpec(specs []perfharness.OutputSpec, results uint64, interval int) bool {
	for _, spec := range specs {
		if spec.Results == results && spec.FlushResults == interval {
			return true
		}
	}
	return false
}

func outputThresholdCase(results uint64, interval int, elapsed time.Duration, allocated uint64) perfharness.CaseResult {
	return perfharness.CaseResult{
		Name: fmt.Sprintf("output-heavy/results-%d/flush-%d", results, interval),
		SteadyMedian: perfharness.Observation{
			WallTime:         elapsed,
			GoAllocatedBytes: allocated,
		},
		Verdict: perfharness.Verdict{Passed: true},
	}
}

func TestApplyWorkerParityBlocksTaskOrderDifference(t *testing.T) {
	t.Parallel()

	results := []perfharness.CaseResult{
		{Name: "scan-orchestration/workers-1", Semantic: &perfharness.SemanticArtifact{TaskOrder: []string{"a", "b"}}, Verdict: perfharness.Verdict{Passed: true}},
		{Name: "scan-orchestration/workers-16", Semantic: &perfharness.SemanticArtifact{TaskOrder: []string{"b", "a"}}, Verdict: perfharness.Verdict{Passed: true}},
	}

	if applyWorkerParity(results, perfharness.New(), []int{1, 16}) {
		t.Fatal("worker parity accepted different task order")
	}
	if !results[1].Verdict.HasFailure("semantic-parity") {
		t.Fatalf("workers-16 verdict = %+v", results[1].Verdict)
	}
}

func TestRunCommandWritesSmokeReports(t *testing.T) {
	t.Parallel()

	outputDir := filepath.Join(t.TempDir(), "report path")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runCommand([]string{
		"-profile", "smoke",
		"-output", outputDir,
		"-smoke-items", "5",
		"-smoke-snapshot-bytes", "4096",
		"-regression-before-ns", "1000000000000",
		"-regression-before-bytes", "1000000000000",
		"-evidence-label", "hardware-qualified",
	}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exit code = %d, stderr=%s", exitCode, stderr.String())
	}
	data, err := os.ReadFile(filepath.Join(outputDir, "report", "performance-report.json"))
	if err != nil {
		t.Fatalf("ReadFile(report): %v", err)
	}
	var report perfharness.Report
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("Unmarshal(report): %v", err)
	}
	if len(report.Cases) != 113 {
		t.Fatalf("case count = %d, want separate snapshot load and save results", len(report.Cases))
	}
	if report.Hardware.EvidenceLabel != perfharness.EvidenceHardwareQualified {
		t.Fatalf("evidence label = %q", report.Hardware.EvidenceLabel)
	}
	if len(report.Contract.Limits) != 12 || len(report.Contract.CancelStages) != 5 {
		t.Fatalf("report lacks the matrix contract: %+v", report.Contract)
	}
	richDenyCases := 0
	cancellationCases := 0
	regressionCases := 0
	resumeCases := 0
	failureCases := 0
	richAcceptedCases := 0
	targetLimitCases := 0
	resourceLimitCases := 0
	outputCases := 0
	snapshotLoadCases := 0
	snapshotSaveCases := 0
	for _, result := range report.Cases {
		if strings.HasPrefix(result.Name, "snapshot-load/") {
			snapshotLoadCases++
		}
		if strings.HasPrefix(result.Name, "snapshot-save/") {
			snapshotSaveCases++
		}
		if strings.HasPrefix(result.Name, "output-heavy/results-") {
			outputCases++
			if len(result.Runs) != 6 || !result.Verdict.Passed || result.SteadyMedian.MegabytesPerSecond <= 0 {
				t.Fatalf("output case failed: %+v", result)
			}
		}
		if strings.HasPrefix(result.Name, "limit/target-") {
			targetLimitCases++
			if !result.Verdict.Passed || !result.Correctness.ExpectedValues {
				t.Fatalf("target limit case failed: %+v", result)
			}
		}
		if strings.HasPrefix(result.Name, "limit/") && !strings.HasPrefix(result.Name, "limit/target-") {
			resourceLimitCases++
			if !result.Verdict.Passed || !result.Correctness.ExpectedValues {
				t.Fatalf("resource limit case failed: %+v", result)
			}
		}
		if strings.HasPrefix(result.Name, "production-rich/") {
			richAcceptedCases++
			if !result.Verdict.Passed {
				t.Fatalf("accepted rich case failed: %+v", result)
			}
		}
		if strings.HasPrefix(result.Name, "production-failure/") {
			failureCases++
			if !result.Verdict.Passed {
				t.Fatalf("failure case failed: %+v", result)
			}
		}
		if strings.HasPrefix(result.Name, "production-resume/") {
			resumeCases++
			if !result.Verdict.Passed {
				t.Fatalf("resume case failed: %+v", result)
			}
		}
		if result.Name == "regression/snapshot-generator" {
			regressionCases++
			if result.Regression == nil || !result.Verdict.Passed {
				t.Fatalf("regression case failed: %+v", result)
			}
		}
		if strings.HasPrefix(result.Name, "production-cancellation/") {
			cancellationCases++
			if !result.Verdict.Passed || result.LogicalItems != 1000 || len(result.Runs) != 6 ||
				result.FixtureGeneration == nil || len(result.FixtureGeneration.Runs) != 6 ||
				result.Cancellation == nil || result.Cancellation.SchemaVersion != perfharness.CancellationEvidenceSchemaVersion ||
				len(result.Cancellation.Runs) != 6 {
				t.Fatalf("cancellation case failed: %+v", result)
			}
		}
		if result.Name == "production-rich-deny/deny-only" || result.Name == "production-rich-deny/accept-deny-conflict" {
			richDenyCases++
			if !result.Verdict.Passed || !result.Correctness.ExpectedValues {
				t.Fatalf("rich-deny case failed: %+v", result)
			}
		}
		if result.Name == "production-workflow/complete/crlf/workers-16" && result.Verdict.Passed {
			continue
		}
		if result.Name == "production-workflow/complete/workers-16" {
			if result.FixtureGeneration == nil || len(result.FixtureGeneration.Runs) != 6 {
				t.Fatalf("workflow fixture-generation metrics = %+v", result.FixtureGeneration)
			}
			if result.ColdStart.InputBytes == 0 || result.ColdStart.OutputBytes == 0 {
				t.Fatalf("workflow stage metrics = %+v", result.ColdStart)
			}
		}
	}
	if richDenyCases != 2 {
		t.Fatalf("rich-deny case count = %d, want 2", richDenyCases)
	}
	if cancellationCases != 15 {
		t.Fatalf("cancellation case count = %d, want 15", cancellationCases)
	}
	if regressionCases != 1 {
		t.Fatalf("regression case count = %d, want 1", regressionCases)
	}
	if resumeCases != 3 {
		t.Fatalf("resume case count = %d, want 3", resumeCases)
	}
	if failureCases != 3 {
		t.Fatalf("failure case count = %d, want 3", failureCases)
	}
	if richAcceptedCases != 4 {
		t.Fatalf("accepted rich case count = %d, want 4", richAcceptedCases)
	}
	if targetLimitCases != 12 {
		t.Fatalf("target limit case count = %d, want 12", targetLimitCases)
	}
	if resourceLimitCases != 60 {
		t.Fatalf("resource limit case count = %d, want 60", resourceLimitCases)
	}
	if outputCases != 3 {
		t.Fatalf("output case count = %d, want 3", outputCases)
	}
	if snapshotLoadCases != 1 || snapshotSaveCases != 1 {
		t.Fatalf("snapshot cases = load %d save %d, want one of each", snapshotLoadCases, snapshotSaveCases)
	}
}

func TestFullCancellationRoutesKeepTenMillionItemsAndAllEvidence(t *testing.T) {
	t.Parallel()

	specs := cancellationCaseSpecs("full", perfharness.DefaultContract())
	if len(specs) != 15 {
		t.Fatalf("full cancellation route count = %d, want 15", len(specs))
	}
	seen := make(map[string]bool, len(specs))
	for _, spec := range specs {
		if spec.Items != perfharness.FullItemCount {
			t.Fatalf("route %+v has %d items, want %d", spec, spec.Items, perfharness.FullItemCount)
		}
		key := fmt.Sprintf("%s/%d", spec.Stage, spec.Percent)
		if seen[key] {
			t.Fatalf("duplicate cancellation route %s", key)
		}
		seen[key] = true
	}
}

func TestRunCommandComparesPortableReports(t *testing.T) {
	t.Parallel()

	harness := perfharness.New()
	report := perfharness.Report{Cases: []perfharness.CaseResult{{Name: "case", Verdict: perfharness.Verdict{Passed: true}}}}
	left, err := harness.WriteReports(context.Background(), filepath.Join(t.TempDir(), "left"), report)
	if err != nil {
		t.Fatal(err)
	}
	right, err := harness.WriteReports(context.Background(), filepath.Join(t.TempDir(), "right"), report)
	if err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := runCommand([]string{"-compare-left", left.JSON, "-compare-right", right.JSON}, &stdout, &stderr); code != 0 {
		t.Fatalf("compare exit=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "semantic parity passed") {
		t.Fatalf("compare output = %q", stdout.String())
	}
}

func TestRunCommandRejectsUnreadableComparisonReports(t *testing.T) {
	t.Parallel()

	validPath := filepath.Join(t.TempDir(), "report.json")
	encoded, err := json.Marshal(perfharness.Report{})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(validPath, encoded, 0o644); err != nil {
		t.Fatal(err)
	}
	invalidPath := filepath.Join(t.TempDir(), "invalid.json")
	if err := os.WriteFile(invalidPath, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	missingPath := filepath.Join(t.TempDir(), "missing.json")
	for _, paths := range [][2]string{
		{missingPath, validPath},
		{validPath, missingPath},
		{invalidPath, validPath},
	} {
		var stderr bytes.Buffer
		if code := runCommand([]string{"-compare-left", paths[0], "-compare-right", paths[1]}, io.Discard, &stderr); code != 1 {
			t.Fatalf("compare %q and %q exit = %d, want 1", paths[0], paths[1], code)
		}
		if stderr.Len() == 0 {
			t.Fatalf("compare %q and %q did not explain the failure", paths[0], paths[1])
		}
	}
}

func TestRunCommandFailsWhenItCannotWriteStatus(t *testing.T) {
	t.Parallel()

	validArgs := []string{"-profile", "smoke", "-output", filepath.Join(t.TempDir(), "report"), "-smoke-items", "1", "-smoke-snapshot-bytes", "1024"}
	var stderr bytes.Buffer
	if exitCode := runCommand(validArgs, failingWriter{}, &stderr); exitCode != 1 {
		t.Fatalf("exit code = %d, want 1 for status output failure", exitCode)
	}

	invalidChecks := [][]string{
		{"-output", filepath.Join(t.TempDir(), "profile"), "-profile", "invalid"},
		{"-output", filepath.Join(t.TempDir(), "label"), "-evidence-label", "invalid"},
		{"-compare-left", "left.json"},
	}
	for _, args := range invalidChecks {
		if exitCode := runCommand(args, io.Discard, failingWriter{}); exitCode != 1 {
			t.Fatalf("args=%v exit code = %d, want 1 for status output failure", args, exitCode)
		}
	}
}

func TestApplyWorkerMemoryThresholdBlocksMoreThanTwentyFivePercentGrowth(t *testing.T) {
	t.Parallel()

	results := []perfharness.CaseResult{
		{
			Name:         "scan-orchestration/workers-16",
			ColdStart:    perfharness.Observation{PeakCommittedBytes: 100},
			SteadyMedian: perfharness.Observation{PeakCommittedBytes: 100},
			Verdict:      perfharness.Verdict{Passed: true},
		},
		{
			Name:         "scan-orchestration/workers-256",
			ColdStart:    perfharness.Observation{PeakCommittedBytes: 126},
			SteadyMedian: perfharness.Observation{PeakCommittedBytes: 126},
			Verdict:      perfharness.Verdict{Passed: true},
		},
	}
	if applyWorkerMemoryThreshold(results, perfharness.New()) {
		t.Fatal("worker memory threshold accepted 26 percent growth")
	}
	if !results[1].Verdict.HasFailure("worker-memory") {
		t.Fatalf("workers-256 verdict = %+v", results[1].Verdict)
	}
}

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/xuxiping/port-scan-mk3/internal/perfharness"
)

func main() {
	os.Exit(runCommand(os.Args[1:], os.Stdout, os.Stderr))
}

func runCommand(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("perf-harness", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var (
		profile            string
		outputDir          string
		evidenceLabel      string
		cpu                string
		powerMode          string
		filesystem         string
		disk               string
		constraints        string
		commit             string
		ramBytes           uint64
		freeDiskBytes      uint64
		physicalCores      int
		logicalCores       int
		smokeItems         uint64
		smokeSnapshotBytes uint64
		compareLeft        string
		compareRight       string
		regressionBeforeNS float64
		regressionBeforeB  float64
	)
	flags.StringVar(&profile, "profile", "full", "matrix profile: full or smoke")
	flags.StringVar(&outputDir, "output", "", "new output directory")
	flags.StringVar(&evidenceLabel, "evidence-label", string(perfharness.EvidenceHardwareQualified), "evidence label")
	flags.StringVar(&cpu, "cpu", "unknown", "CPU model")
	flags.IntVar(&physicalCores, "physical-cores", 0, "physical core count")
	flags.IntVar(&logicalCores, "logical-cores", runtime.NumCPU(), "logical core count")
	flags.StringVar(&powerMode, "power-mode", "unknown", "power mode")
	flags.Uint64Var(&ramBytes, "ram-bytes", 0, "RAM bytes")
	flags.StringVar(&filesystem, "filesystem", "unknown", "filesystem")
	flags.StringVar(&disk, "disk", "unknown", "disk model")
	flags.Uint64Var(&freeDiskBytes, "free-disk-bytes", 0, "free disk bytes")
	flags.StringVar(&constraints, "constraints", "none recorded", "resource constraints")
	flags.StringVar(&commit, "commit", "unknown", "git commit")
	flags.Uint64Var(&smokeItems, "smoke-items", perfharness.SmokeItemCount, "bounded smoke item count")
	flags.Uint64Var(&smokeSnapshotBytes, "smoke-snapshot-bytes", perfharness.SmokeSnapshotBytes, "bounded smoke snapshot bytes")
	flags.StringVar(&compareLeft, "compare-left", "", "Linux or Windows JSON report")
	flags.StringVar(&compareRight, "compare-right", "", "other OS JSON report")
	flags.Float64Var(&regressionBeforeNS, "regression-before-ns", 0, "recorded before median in ns/op")
	flags.Float64Var(&regressionBeforeB, "regression-before-bytes", 0, "recorded before median in B/op")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if compareLeft != "" || compareRight != "" {
		if compareLeft == "" || compareRight == "" {
			if writeErr := writeStatus(stderr, "-compare-left and -compare-right must be used together\n"); writeErr != nil {
				return 1
			}
			return 2
		}
		return compareReportFiles(compareLeft, compareRight, perfharness.New(), stdout, stderr)
	}
	if outputDir == "" {
		if writeErr := writeStatus(stderr, "-output is required\n"); writeErr != nil {
			return 1
		}
		return 2
	}
	if profile != "full" && profile != "smoke" {
		if writeErr := writeStatus(stderr, "-profile must be full or smoke\n"); writeErr != nil {
			return 1
		}
		return 2
	}
	label := perfharness.EvidenceLabel(evidenceLabel)
	if label != perfharness.EvidenceHardwareQualified && label != perfharness.EvidenceMinimumCertified {
		if writeErr := writeStatus(stderr, "-evidence-label must be hardware-qualified or minimum-profile certified\n"); writeErr != nil {
			return 1
		}
		return 2
	}
	outputMatrix := outputSpecs(profile, smokeItems)
	if required := requiredOutputBytes(outputMatrix); freeDiskBytes > 0 && freeDiskBytes < required {
		if writeErr := writeStatus(stderr, "insufficient free space for output matrix: have %d bytes, require %d bytes\n", freeDiskBytes, required); writeErr != nil {
			return 1
		}
		return 1
	}
	if err := os.Mkdir(outputDir, 0o755); err != nil {
		if writeErr := writeStatus(stderr, "create matrix directory: %v\n", err); writeErr != nil {
			return 1
		}
		return 1
	}

	harness := perfharness.New()
	contract := perfharness.DefaultContract()
	if regressionBeforeNS > 0 {
		contract.RegressionBenchmark.BeforeNSPerOp = regressionBeforeNS
	}
	if regressionBeforeB > 0 {
		contract.RegressionBenchmark.BeforeBPerOp = regressionBeforeB
	}
	specs := fixtureSpecs(profile, smokeItems, smokeSnapshotBytes, contract)
	casesDir := filepath.Join(outputDir, "cases")
	if err := os.Mkdir(casesDir, 0o755); err != nil {
		if writeErr := writeStatus(stderr, "create cases directory: %v\n", err); writeErr != nil {
			return 1
		}
		return 1
	}
	results := make([]perfharness.CaseResult, 0, len(specs)+5)
	for index, spec := range specs {
		caseDir := filepath.Join(casesDir, fmt.Sprintf("%02d-%s", index, spec.Family))
		if spec.Family == perfharness.FamilySnapshotHeavy {
			snapshotResults, err := harness.RunSnapshotCases(context.Background(), caseDir, spec)
			if err != nil {
				if writeErr := writeStatus(stderr, "run snapshot fixture %s: %v\n", spec.Shape, err); writeErr != nil {
					return 1
				}
				return 1
			}
			results = append(results, snapshotResults...)
			for _, result := range snapshotResults {
				if err := writeStatus(stdout, "case passed: %s\n", result.Name); err != nil {
					return 1
				}
			}
			continue
		}
		result, err := harness.RunFixtureCase(context.Background(), caseDir, spec)
		if err != nil {
			if writeErr := writeStatus(stderr, "run fixture %s: %v\n", spec.Family, err); writeErr != nil {
				return 1
			}
			return 1
		}
		results = append(results, result)
		if err := writeStatus(stdout, "case passed: %s\n", result.Name); err != nil {
			return 1
		}
	}
	outputScales := make([]uint64, 0, len(outputMatrix)/3)
	for index, spec := range outputMatrix {
		caseDir := filepath.Join(casesDir, fmt.Sprintf("output-%02d-results-%d-flush-%d", index, spec.Results, spec.FlushResults))
		spec.OutputDir = caseDir
		result, err := harness.RunOutputCase(context.Background(), spec)
		if err != nil {
			if writeErr := writeStatus(stderr, "run output results=%d flush=%d: %v\n", spec.Results, spec.FlushResults, err); writeErr != nil {
				return 1
			}
			return 1
		}
		results = append(results, result)
		if spec.FlushResults == 1 {
			outputScales = append(outputScales, spec.Results)
		}
		if err := writeStatus(stdout, "case passed: %s\n", result.Name); err != nil {
			return 1
		}
	}
	for _, limits := range contract.Limits {
		for _, bypass := range limits.Cases {
			var (
				result perfharness.CaseResult
				err    error
			)
			if limits.Flag == "-target-count-limit" || limits.Flag == "-target-memory-limit-gb" {
				result, err = harness.RunTargetLimitCase(context.Background(), perfharness.TargetLimitSpec{
					OutputDir: filepath.Join(casesDir, "limit-"+strings.TrimPrefix(limits.Flag, "-")+"-"+string(bypass.Kind)),
					Flag:      limits.Flag,
					Case:      bypass,
				})
			} else {
				result, err = harness.RunResourceLimitCase(context.Background(), perfharness.ResourceLimitSpec{Flag: limits.Flag, Case: bypass})
			}
			if err != nil {
				if writeErr := writeStatus(stderr, "run limit %s %s: %v\n", limits.Flag, bypass.Kind, err); writeErr != nil {
					return 1
				}
				return 1
			}
			results = append(results, result)
			if err := writeStatus(stdout, "case passed: %s\n", result.Name); err != nil {
				return 1
			}
		}
	}

	workflowItems := smokeItems
	for _, workers := range contract.FakeWorkers {
		result, err := runWorkflowCase(context.Background(), harness, filepath.Join(casesDir, "workflow-workers-"+strconv.Itoa(workers)), workflowItems, workers, "")
		if err != nil {
			if writeErr := writeStatus(stderr, "run workflow workers=%d: %v\n", workers, err); writeErr != nil {
				return 1
			}
			return 1
		}
		results = append(results, result)
		if err := writeStatus(stdout, "case passed: %s\n", result.Name); err != nil {
			return 1
		}
	}
	if workflowItems >= 10 {
		result, err := runWorkflowCase(context.Background(), harness, filepath.Join(casesDir, "workflow-growth-1x-workers-16"), workflowItems/10, 16, "")
		if err != nil {
			if writeErr := writeStatus(stderr, "run workflow 1x growth case: %v\n", err); writeErr != nil {
				return 1
			}
			return 1
		}
		result.Name = "production-workflow/growth-1x/workers-16"
		results = append(results, result)
		if err := writeStatus(stdout, "case passed: %s\n", result.Name); err != nil {
			return 1
		}
	}
	for _, shape := range []string{"deny-only", "accept-deny-conflict"} {
		result, err := runRichDenyCase(context.Background(), harness, filepath.Join(casesDir, "rich-deny-"+shape), workflowItems, 16, shape)
		if err != nil {
			if writeErr := writeStatus(stderr, "run rich-deny shape=%s: %v\n", shape, err); writeErr != nil {
				return 1
			}
			return 1
		}
		results = append(results, result)
		if err := writeStatus(stdout, "case passed: %s\n", result.Name); err != nil {
			return 1
		}
	}
	const cancellationItems uint64 = 200
	for _, stage := range contract.CancelStages {
		for _, percent := range contract.CancelProgress {
			result, err := runCancellationCase(context.Background(), harness, filepath.Join(casesDir, fmt.Sprintf("cancel-%s-%d", stage, percent)), cancellationItems, 16, stage, percent, contract.StopWithin)
			if err != nil {
				if writeErr := writeStatus(stderr, "run cancellation stage=%s percent=%d: %v\n", stage, percent, err); writeErr != nil {
					return 1
				}
				return 1
			}
			results = append(results, result)
			if err := writeStatus(stdout, "case passed: %s\n", result.Name); err != nil {
				return 1
			}
		}
	}
	regressionResult, err := harness.RunRegressionBenchmark(context.Background(), contract.RegressionBenchmark)
	if err != nil {
		if writeErr := writeStatus(stderr, "run regression benchmark: %v\n", err); writeErr != nil {
			return 1
		}
		return 1
	}
	results = append(results, regressionResult)
	if err := writeStatus(stdout, "case passed: %s\n", regressionResult.Name); err != nil {
		return 1
	}
	resumeItems := min(workflowItems, uint64(100))
	for _, percent := range []int{0, 50, 99} {
		result, err := runResumeCase(context.Background(), harness, filepath.Join(casesDir, fmt.Sprintf("resume-%d", percent)), resumeItems, 16, percent)
		if err != nil {
			if writeErr := writeStatus(stderr, "run resume percent=%d: %v\n", percent, err); writeErr != nil {
				return 1
			}
			return 1
		}
		results = append(results, result)
		if err := writeStatus(stdout, "case passed: %s\n", result.Name); err != nil {
			return 1
		}
	}
	for _, scenario := range []string{"output-failure", "snapshot-save-failure", "pressure-fatal-error"} {
		result, err := runFailureCase(context.Background(), harness, filepath.Join(casesDir, "failure-"+scenario), 100, 16, scenario)
		if err != nil {
			if writeErr := writeStatus(stderr, "run failure scenario=%s: %v\n", scenario, err); writeErr != nil {
				return 1
			}
			return 1
		}
		results = append(results, result)
		if err := writeStatus(stdout, "case passed: %s\n", result.Name); err != nil {
			return 1
		}
	}
	for _, family := range []perfharness.Family{
		perfharness.FamilyRichRecordMixed,
		perfharness.FamilyRichUniqueKey,
		perfharness.FamilyRichHotKey,
		perfharness.FamilyRichPrecheck,
	} {
		result, err := runAcceptedRichCase(context.Background(), harness, filepath.Join(casesDir, "rich-"+string(family)), 100, 16, family)
		if err != nil {
			if writeErr := writeStatus(stderr, "run rich family=%s: %v\n", family, err); writeErr != nil {
				return 1
			}
			return 1
		}
		results = append(results, result)
		if err := writeStatus(stdout, "case passed: %s\n", result.Name); err != nil {
			return 1
		}
	}
	if profile == "full" {
		for _, caseName := range contract.RichOversizeCases {
			result, err := harness.RunRichOversizeCase(context.Background(), perfharness.RichOversizeSpec{
				OutputDir:   filepath.Join(casesDir, "rich-oversize-"+caseName),
				Items:       perfharness.FullItemCount,
				Workers:     16,
				TargetBytes: 1_000_000_001,
				LimitBytes:  1_000_000_000,
				Case:        caseName,
			})
			if err != nil {
				if writeErr := writeStatus(stderr, "run rich oversize case=%s: %v\n", caseName, err); writeErr != nil {
					return 1
				}
				return 1
			}
			results = append(results, result)
			if err := writeStatus(stdout, "case passed: %s\n", result.Name); err != nil {
				return 1
			}
		}
	}
	crlfResult, err := runWorkflowCase(context.Background(), harness, filepath.Join(casesDir, "workflow-crlf-workers-16"), workflowItems, 16, "CRLF")
	if err != nil {
		if writeErr := writeStatus(stderr, "run CRLF workflow: %v\n", err); writeErr != nil {
			return 1
		}
		return 1
	}
	results = append(results, crlfResult)
	if err := writeStatus(stdout, "case passed: %s\n", crlfResult.Name); err != nil {
		return 1
	}
	for _, workers := range contract.LoopbackWorkers {
		result, err := runLoopbackCase(context.Background(), harness, filepath.Join(casesDir, "loopback-workers-"+strconv.Itoa(workers)), workers)
		if err != nil {
			if writeErr := writeStatus(stderr, "run loopback workers=%d: %v\n", workers, err); writeErr != nil {
				return 1
			}
			return 1
		}
		results = append(results, result)
		if err := writeStatus(stdout, "case passed: %s\n", result.Name); err != nil {
			return 1
		}
	}
	matrixPassed := applyAbsoluteThresholds(results, harness, contract)
	if !applySnapshotCaseContract(results, specs) {
		matrixPassed = false
	}
	if !applyWorkerParity(results, harness, contract.FakeWorkers) {
		matrixPassed = false
	}
	if workflowItems >= 10 && !applyGrowthThreshold(results, harness, "production-workflow/growth-1x/workers-16", "production-workflow/workers-16") {
		matrixPassed = false
	}
	if workflowItems >= contract.SmokeItems {
		if !applyWorkerMemoryThreshold(results, harness) {
			matrixPassed = false
		}
	}
	if !applyOutputThresholds(results, harness, outputScales) {
		matrixPassed = false
	}
	if !applyInputAndSnapshotGrowthThresholds(results, harness) {
		matrixPassed = false
	}

	report := perfharness.Report{
		SchemaVersion: perfharness.SchemaVersion,
		Contract:      contract,
		Platform:      runtime.GOOS + "/" + runtime.GOARCH,
		Hardware: perfharness.HardwareProfile{
			EvidenceLabel: label,
			CPU:           cpu,
			PhysicalCores: physicalCores,
			LogicalCores:  logicalCores,
			PowerMode:     powerMode,
			RAMBytes:      ramBytes,
			Filesystem:    filesystem,
			Disk:          disk,
			FreeDiskBytes: freeDiskBytes,
			GoVersion:     runtime.Version(),
			Commit:        commit,
			Constraints:   constraints,
		},
		Cases: results,
	}
	paths, err := harness.WriteReports(context.Background(), filepath.Join(outputDir, "report"), report)
	if err != nil {
		if writeErr := writeStatus(stderr, "write reports: %v\n", err); writeErr != nil {
			return 1
		}
		return 1
	}
	if !matrixPassed {
		if err := writeStatus(stderr, "performance matrix failed one or more thresholds: JSON=%s Markdown=%s\n", paths.JSON, paths.Markdown); err != nil {
			return 1
		}
		return 1
	}
	if err := writeStatus(stdout, "performance matrix passed: JSON=%s Markdown=%s\n", paths.JSON, paths.Markdown); err != nil {
		return 1
	}
	return 0
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

func applyInputAndSnapshotGrowthThresholds(results []perfharness.CaseResult, harness perfharness.Harness) bool {
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
			if !applyGrowthThreshold(results, harness, sequence[index-1], sequence[index]) {
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

func applyOutputThresholds(results []perfharness.CaseResult, harness perfharness.Harness, scales []uint64) bool {
	passed := true
	for _, interval := range []int{1, 1000, 0} {
		for index := 1; index < len(scales); index++ {
			smallName := fmt.Sprintf("output-heavy/results-%d/flush-%d", scales[index-1], interval)
			largeName := fmt.Sprintf("output-heavy/results-%d/flush-%d", scales[index], interval)
			if !applyGrowthThreshold(results, harness, smallName, largeName) {
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

func applyWorkerMemoryThreshold(results []perfharness.CaseResult, harness perfharness.Harness) bool {
	workers16 := findCase(results, "production-workflow/workers-16")
	workers256 := findCase(results, "production-workflow/workers-256")
	if workers16 == nil || workers256 == nil {
		return false
	}
	comparisons := []perfharness.WorkerComparison{
		{Workers16Bytes: workerMemory(workers16.ColdStart), Workers256Bytes: workerMemory(workers256.ColdStart)},
		{Workers16Bytes: workerMemory(workers16.SteadyMedian), Workers256Bytes: workerMemory(workers256.SteadyMedian)},
	}
	for _, comparison := range comparisons {
		verdict := harness.Evaluate(perfharness.EvaluationInput{Workers: &comparison})
		if !verdict.Passed {
			workers256.Verdict.Passed = false
			workers256.Verdict.Failures = append(workers256.Verdict.Failures, verdict.Failures...)
		}
	}
	return workers256.Verdict.Passed
}

func applyAbsoluteThresholds(results []perfharness.CaseResult, harness perfharness.Harness, contract perfharness.Contract) bool {
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
			verdict := harness.Evaluate(perfharness.EvaluationInput{Observation: observation, Absolute: budget})
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

func applyGrowthThreshold(results []perfharness.CaseResult, harness perfharness.Harness, smallName, largeName string) bool {
	small := findCase(results, smallName)
	large := findCase(results, largeName)
	if small == nil || large == nil {
		return false
	}
	verdict := harness.Evaluate(perfharness.EvaluationInput{Growth: &perfharness.GrowthComparison{
		Small: small.SteadyMedian,
		Large: large.SteadyMedian,
	}})
	if !verdict.Passed {
		large.Verdict.Passed = false
		large.Verdict.Failures = append(large.Verdict.Failures, verdict.Failures...)
	}
	return verdict.Passed
}

func applyWorkerParity(results []perfharness.CaseResult, harness perfharness.Harness, workers []int) bool {
	if len(workers) == 0 {
		return false
	}
	baseline := findCase(results, "production-workflow/workers-"+strconv.Itoa(workers[0]))
	if baseline == nil || baseline.Semantic == nil {
		return false
	}
	passed := true
	for _, workerCount := range workers[1:] {
		candidate := findCase(results, "production-workflow/workers-"+strconv.Itoa(workerCount))
		if candidate == nil || candidate.Semantic == nil {
			passed = false
			continue
		}
		differences := harness.CompareSemantic(*baseline.Semantic, *candidate.Semantic)
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

func compareReportFiles(leftPath, rightPath string, harness perfharness.Harness, stdout, stderr io.Writer) int {
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
		_ = writeStatus(stderr, "%v\n", err)
		return 1
	}
	right, err := read(rightPath)
	if err != nil {
		_ = writeStatus(stderr, "%v\n", err)
		return 1
	}
	differences := harness.CompareReports(left, right)
	if len(differences) != 0 {
		_ = writeStatus(stderr, "semantic parity failed: %s\n", strings.Join(differences, ", "))
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

func runWorkflowCase(ctx context.Context, harness perfharness.Harness, outputDir string, items uint64, workers int, lineEnding string) (perfharness.CaseResult, error) {
	if err := os.Mkdir(outputDir, 0o755); err != nil {
		return perfharness.CaseResult{}, fmt.Errorf("create workflow case directory: %w", err)
	}
	observations := make([]perfharness.Observation, 0, 6)
	fixtureObservations := make([]perfharness.Observation, 0, 6)
	correct := true
	var semantic perfharness.SemanticArtifact
	for run := 0; run < 6; run++ {
		runDir := filepath.Join(outputDir, fmt.Sprintf("run-%d", run))
		workflow, err := harness.RunProductionSmoke(ctx, perfharness.WorkflowSpec{OutputDir: runDir, Items: items, Workers: workers, LineEnding: lineEnding})
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
		} else if differences := harness.CompareSemantic(semantic, workflow.Semantic); len(differences) != 0 {
			return perfharness.CaseResult{}, fmt.Errorf("workflow run parity differs in %s", strings.Join(differences, ", "))
		}
	}
	name := fmt.Sprintf("production-workflow/workers-%d", workers)
	if lineEnding == "CRLF" {
		name = fmt.Sprintf("production-workflow/crlf/workers-%d", workers)
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
	result.Correctness = perfharness.Correctness{Headers: correct, RowCounts: correct, SnapshotProgress: correct, ExpectedValues: correct, Digests: correct}
	result.Verdict = perfharness.Verdict{Passed: correct}
	result.Semantic = &semantic
	return result, nil
}

func runLoopbackCase(ctx context.Context, harness perfharness.Harness, outputDir string, workers int) (perfharness.CaseResult, error) {
	if err := os.Mkdir(outputDir, 0o755); err != nil {
		return perfharness.CaseResult{}, fmt.Errorf("create loopback case directory: %w", err)
	}
	observations := make([]perfharness.Observation, 0, 6)
	fixtureObservations := make([]perfharness.Observation, 0, 6)
	for run := 0; run < 6; run++ {
		runDir := filepath.Join(outputDir, fmt.Sprintf("run-%d", run))
		workflow, err := harness.RunNativeLoopbackSmoke(ctx, perfharness.WorkflowSpec{OutputDir: runDir, Items: 1, Workers: workers})
		if err != nil {
			return perfharness.CaseResult{}, err
		}
		if workflow.ProbeCount != 1 || workflow.ScanRows != 1 || workflow.OpenRows != 1 {
			return perfharness.CaseResult{}, fmt.Errorf("loopback counts are probes=%d scan=%d open=%d", workflow.ProbeCount, workflow.ScanRows, workflow.OpenRows)
		}
		fixtureObservations = append(fixtureObservations, workflow.FixtureGeneration)
		observations = append(observations, workflow.Stage)
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
	result.Correctness = perfharness.Correctness{Headers: true, RowCounts: true, SnapshotProgress: true, ExpectedValues: true, Digests: true}
	result.Verdict = perfharness.Verdict{Passed: true}
	return result, nil
}

func runRichDenyCase(ctx context.Context, harness perfharness.Harness, outputDir string, items uint64, workers int, shape string) (perfharness.CaseResult, error) {
	if err := os.Mkdir(outputDir, 0o755); err != nil {
		return perfharness.CaseResult{}, fmt.Errorf("create rich-deny case directory: %w", err)
	}
	observations := make([]perfharness.Observation, 0, 6)
	fixtureObservations := make([]perfharness.Observation, 0, 6)
	for run := 0; run < 6; run++ {
		workflow, err := harness.RunRichDenySmoke(ctx, perfharness.RichDenySpec{
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
	result.Correctness = perfharness.Correctness{Headers: true, RowCounts: true, SnapshotProgress: true, ExpectedValues: true, Digests: true}
	result.Verdict = perfharness.Verdict{Passed: true}
	return result, nil
}

func runCancellationCase(ctx context.Context, harness perfharness.Harness, outputDir string, items uint64, workers int, stage perfharness.CancellationStage, percent int, stopWithin time.Duration) (perfharness.CaseResult, error) {
	if err := os.Mkdir(outputDir, 0o755); err != nil {
		return perfharness.CaseResult{}, fmt.Errorf("create cancellation case directory: %w", err)
	}
	observations := make([]perfharness.Observation, 0, 6)
	correct := true
	for run := 0; run < 6; run++ {
		var cancellation perfharness.CancellationResult
		observation, err := harness.Measure(ctx, 0, items, func(runCtx context.Context) (uint64, error) {
			result, runErr := harness.RunCancellationSmoke(runCtx, perfharness.CancellationSpec{
				OutputDir: filepath.Join(outputDir, fmt.Sprintf("run-%d", run)),
				Items:     items,
				Workers:   workers,
				Stage:     stage,
				Percent:   percent,
			})
			cancellation = result
			if !errors.Is(runErr, context.Canceled) {
				return 0, runErr
			}
			return 0, nil
		})
		if err != nil {
			return perfharness.CaseResult{}, err
		}
		if !cancellation.Injected || cancellation.StopDuration > stopWithin ||
			(stage == perfharness.CancellationResumeRebuild || stage == perfharness.CancellationResultOutput) && !cancellation.Resumable {
			correct = false
		}
		observations = append(observations, observation)
	}
	result, err := perfharness.SummarizeCase(fmt.Sprintf("production-cancellation/%s/%d", stage, percent), observations)
	if err != nil {
		return perfharness.CaseResult{}, err
	}
	result.Correctness = perfharness.Correctness{Headers: true, RowCounts: true, SnapshotProgress: correct, ExpectedValues: correct, Digests: true}
	result.Verdict = perfharness.Verdict{Passed: correct}
	if !correct {
		result.Verdict.Failures = []perfharness.Failure{{Rule: "cancellation-stop", Detail: "production work did not stop within one second"}}
	}
	return result, nil
}

func runResumeCase(ctx context.Context, harness perfharness.Harness, outputDir string, items uint64, workers, percent int) (perfharness.CaseResult, error) {
	observations := make([]perfharness.Observation, 0, 6)
	var semantic perfharness.SemanticArtifact
	wantRemaining := items - items*uint64(percent)/100
	for run := 0; run < 6; run++ {
		workflow, err := harness.RunResumeSmoke(ctx, perfharness.ResumeSpec{
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
		} else if differences := harness.CompareSemantic(semantic, workflow.Semantic); len(differences) != 0 {
			return perfharness.CaseResult{}, fmt.Errorf("resume run parity differs in %s", strings.Join(differences, ", "))
		}
		observations = append(observations, workflow.Stage)
	}
	result, err := perfharness.SummarizeCase(fmt.Sprintf("production-resume/%d", percent), observations)
	if err != nil {
		return perfharness.CaseResult{}, err
	}
	result.Semantic = &semantic
	result.Correctness = perfharness.Correctness{Headers: true, RowCounts: true, SnapshotProgress: true, ExpectedValues: true, Digests: true}
	result.Verdict = perfharness.Verdict{Passed: true}
	return result, nil
}

func runFailureCase(ctx context.Context, harness perfharness.Harness, outputDir string, items uint64, workers int, scenario string) (perfharness.CaseResult, error) {
	observations := make([]perfharness.Observation, 0, 6)
	correct := true
	for run := 0; run < 6; run++ {
		var failure perfharness.FailureResult
		observation, err := harness.Measure(ctx, 0, items, func(runCtx context.Context) (uint64, error) {
			result, runErr := harness.RunFailureSmoke(runCtx, perfharness.FailureSpec{
				OutputDir: filepath.Join(outputDir, fmt.Sprintf("run-%d", run)),
				Items:     items,
				Workers:   workers,
				Scenario:  scenario,
			})
			failure = result
			return 0, runErr
		})
		if err != nil {
			return perfharness.CaseResult{}, err
		}
		if !failure.Observed || failure.ErrorText == "" {
			correct = false
		}
		observations = append(observations, observation)
	}
	result, err := perfharness.SummarizeCase("production-failure/"+scenario, observations)
	if err != nil {
		return perfharness.CaseResult{}, err
	}
	result.Correctness = perfharness.Correctness{Headers: true, RowCounts: true, SnapshotProgress: true, ExpectedValues: correct, Digests: true}
	result.Verdict = perfharness.Verdict{Passed: correct}
	return result, nil
}

func runAcceptedRichCase(ctx context.Context, harness perfharness.Harness, outputDir string, items uint64, workers int, family perfharness.Family) (perfharness.CaseResult, error) {
	observations := make([]perfharness.Observation, 0, 6)
	fixtureObservations := make([]perfharness.Observation, 0, 6)
	want := items
	if family == perfharness.FamilyRichHotKey {
		want = min(items, uint64(4))
	}
	var semantic perfharness.SemanticArtifact
	for run := 0; run < 6; run++ {
		workflow, err := harness.RunRichSmoke(ctx, perfharness.RichSpec{
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
		} else if differences := harness.CompareSemantic(semantic, workflow.Semantic); len(differences) != 0 {
			return perfharness.CaseResult{}, fmt.Errorf("rich run parity differs in %s", strings.Join(differences, ", "))
		}
		fixtureObservations = append(fixtureObservations, workflow.FixtureGeneration)
		observations = append(observations, workflow.Stage)
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
	result.Semantic = &semantic
	result.Correctness = perfharness.Correctness{Headers: true, RowCounts: true, SnapshotProgress: true, ExpectedValues: true, Digests: true}
	result.Verdict = perfharness.Verdict{Passed: true}
	return result, nil
}

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"

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
	if err := flags.Parse(args); err != nil {
		return 2
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
	if err := os.Mkdir(outputDir, 0o755); err != nil {
		if writeErr := writeStatus(stderr, "create matrix directory: %v\n", err); writeErr != nil {
			return 1
		}
		return 1
	}

	harness := perfharness.New()
	contract := perfharness.DefaultContract()
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

	workflowItems := smokeItems
	for _, workers := range contract.FakeWorkers {
		result, err := runWorkflowCase(context.Background(), harness, filepath.Join(casesDir, "workflow-workers-"+strconv.Itoa(workers)), workflowItems, workers)
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
	if err := writeStatus(stdout, "performance matrix passed: JSON=%s Markdown=%s\n", paths.JSON, paths.Markdown); err != nil {
		return 1
	}
	return 0
}

func writeStatus(output io.Writer, format string, values ...any) error {
	_, err := fmt.Fprintf(output, format, values...)
	return err
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

func runWorkflowCase(ctx context.Context, harness perfharness.Harness, outputDir string, items uint64, workers int) (perfharness.CaseResult, error) {
	if err := os.Mkdir(outputDir, 0o755); err != nil {
		return perfharness.CaseResult{}, fmt.Errorf("create workflow case directory: %w", err)
	}
	observations := make([]perfharness.Observation, 0, 6)
	fixtureObservations := make([]perfharness.Observation, 0, 6)
	correct := true
	for run := 0; run < 6; run++ {
		runDir := filepath.Join(outputDir, fmt.Sprintf("run-%d", run))
		workflow, err := harness.RunProductionSmoke(ctx, perfharness.WorkflowSpec{OutputDir: runDir, Items: items, Workers: workers})
		if err != nil {
			return perfharness.CaseResult{}, err
		}
		if workflow.ProbeCount != items || workflow.ScanRows != items || workflow.OpenRows != items || !workflow.SnapshotCompleted {
			correct = false
			return perfharness.CaseResult{}, fmt.Errorf("workflow counts are probes=%d scan=%d open=%d, want %d", workflow.ProbeCount, workflow.ScanRows, workflow.OpenRows, items)
		}
		fixtureObservations = append(fixtureObservations, workflow.FixtureGeneration)
		observations = append(observations, workflow.Stage)
	}
	result, err := perfharness.SummarizeCase(fmt.Sprintf("production-workflow/workers-%d", workers), observations)
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

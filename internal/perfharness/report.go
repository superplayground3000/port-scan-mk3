package perfharness

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// EvidenceLabel states whether the host used the minimum resource profile.
type EvidenceLabel string

const (
	EvidenceMinimumCertified  EvidenceLabel = "minimum-profile certified"
	EvidenceHardwareQualified EvidenceLabel = "hardware-qualified"
)

// HardwareProfile records the host that produced a report.
type HardwareProfile struct {
	EvidenceLabel EvidenceLabel `json:"evidence_label"`
	CPU           string        `json:"cpu"`
	PhysicalCores int           `json:"physical_cores"`
	LogicalCores  int           `json:"logical_cores"`
	PowerMode     string        `json:"power_mode"`
	RAMBytes      uint64        `json:"ram_bytes"`
	Filesystem    string        `json:"filesystem"`
	Disk          string        `json:"disk"`
	FreeDiskBytes uint64        `json:"free_disk_bytes"`
	GoVersion     string        `json:"go_version"`
	Commit        string        `json:"commit"`
	Constraints   string        `json:"resource_constraints"`
}

// Correctness records the post-run artifact checks.
type Correctness struct {
	Headers          bool   `json:"headers"`
	RowCounts        bool   `json:"row_counts"`
	SnapshotProgress bool   `json:"snapshot_progress"`
	ExpectedValues   bool   `json:"expected_values"`
	Digests          bool   `json:"digests"`
	Detail           string `json:"detail,omitempty"`
}

// PhaseResult records the six observations for a separate setup phase.
type PhaseResult struct {
	Runs         []Observation `json:"runs"`
	ColdStart    Observation   `json:"cold_start"`
	SteadyMedian Observation   `json:"steady_state_median"`
}

const CancellationEvidenceSchemaVersion = "1"

// CancellationCaseEvidence retains the six production cancellation results.
type CancellationCaseEvidence struct {
	SchemaVersion string               `json:"schema_version"`
	Runs          []CancellationResult `json:"runs"`
}

// CaseResult records six observations and the evaluated summaries.
type CaseResult struct {
	Name              string                    `json:"name"`
	LogicalItems      uint64                    `json:"logical_items,omitempty"`
	Manifest          *Manifest                 `json:"manifest,omitempty"`
	FixtureGeneration *PhaseResult              `json:"fixture_generation,omitempty"`
	Runs              []Observation             `json:"runs"`
	ColdStart         Observation               `json:"cold_start"`
	SteadyMedian      Observation               `json:"steady_state_median"`
	Correctness       Correctness               `json:"correctness"`
	Verdict           Verdict                   `json:"verdict"`
	Semantic          *SemanticArtifact         `json:"semantic,omitempty"`
	Regression        *RegressionComparison     `json:"regression,omitempty"`
	Cancellation      *CancellationCaseEvidence `json:"cancellation,omitempty"`
}

// Report is the portable evidence document for one matrix run.
type Report struct {
	SchemaVersion string          `json:"schema_version"`
	Contract      Contract        `json:"contract"`
	Platform      string          `json:"platform"`
	Hardware      HardwareProfile `json:"hardware"`
	Cases         []CaseResult    `json:"cases"`
}

// ReportPaths gives the two report artifacts.
type ReportPaths struct {
	JSON     string
	Markdown string
}

// SummarizeCase separates the cold run and calculates the five-run median.
func SummarizeCase(name string, runs []Observation) (CaseResult, error) {
	phase, err := SummarizePhase(name, runs)
	if err != nil {
		return CaseResult{}, err
	}
	return CaseResult{
		Name:         name,
		Runs:         phase.Runs,
		ColdStart:    phase.ColdStart,
		SteadyMedian: phase.SteadyMedian,
	}, nil
}

// SummarizePhase separates one cold observation from five steady observations.
func SummarizePhase(name string, runs []Observation) (PhaseResult, error) {
	if len(runs) != 6 {
		return PhaseResult{}, fmt.Errorf("phase %q has %d observations, want 6", name, len(runs))
	}
	steady := append([]Observation(nil), runs[1:]...)
	return PhaseResult{
		Runs:         append([]Observation(nil), runs...),
		ColdStart:    runs[0],
		SteadyMedian: medianObservation(steady),
	}, nil
}

// WriteReports writes one JSON report and one Markdown report.
func (Suite) WriteReports(ctx context.Context, outputDir string, report Report) (ReportPaths, error) {
	if err := ctx.Err(); err != nil {
		return ReportPaths{}, err
	}
	if report.SchemaVersion == "" {
		report.SchemaVersion = SchemaVersion
	}
	if err := os.Mkdir(outputDir, 0o755); err != nil {
		return ReportPaths{}, fmt.Errorf("create report directory: %w", err)
	}
	paths := ReportPaths{
		JSON:     filepath.Join(outputDir, "performance-report.json"),
		Markdown: filepath.Join(outputDir, "performance-report.md"),
	}
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return ReportPaths{}, fmt.Errorf("encode JSON report: %w", err)
	}
	if err := os.WriteFile(paths.JSON, append(encoded, '\n'), 0o644); err != nil {
		return ReportPaths{}, fmt.Errorf("write JSON report: %w", err)
	}
	if err := os.WriteFile(paths.Markdown, []byte(markdownReport(report)), 0o644); err != nil {
		return ReportPaths{}, fmt.Errorf("write Markdown report: %w", err)
	}
	return paths, nil
}

func markdownReport(report Report) string {
	var text strings.Builder
	fmt.Fprintln(&text, "# Port-scan performance report")
	fmt.Fprintln(&text)
	fmt.Fprintf(&text, "Evidence label: `%s`\n\n", report.Hardware.EvidenceLabel)
	fmt.Fprintf(&text, "Platform: `%s`\n\n", report.Platform)
	fmt.Fprintf(&text, "CPU: `%s`\n\n", report.Hardware.CPU)
	fmt.Fprintf(&text, "RAM: `%d` bytes\n\n", report.Hardware.RAMBytes)
	fmt.Fprintf(&text, "Go: `%s`\n\n", report.Hardware.GoVersion)
	fmt.Fprintf(&text, "Commit: `%s`\n\n", report.Hardware.Commit)
	if len(report.Contract.FixtureCases) > 0 {
		fmt.Fprintln(&text, "## Required fixture mapping")
		fmt.Fprintln(&text)
		fmt.Fprintln(&text, "| Fixture | Shape | Production cases |")
		fmt.Fprintln(&text, "|---|---|---|")
		for _, mapping := range report.Contract.FixtureCases {
			fmt.Fprintf(&text, "| %s | %s | %s |\n", mapping.Fixture.Family, mapping.Fixture.Shape, strings.Join(mapping.CaseNames, "<br>"))
		}
		fmt.Fprintln(&text)
	}
	fmt.Fprintln(&text, "| Case | Logical items | Fixture generation median | Stage cold wall time | Stage steady median | Output bytes | Results/s | MB/s | Allocations | Allocated bytes | Peak heap | Verdict |")
	fmt.Fprintln(&text, "|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---|")
	for _, result := range report.Cases {
		verdict := "fail"
		if result.Verdict.Passed {
			verdict = "pass"
		}
		fixtureGeneration := "n/a"
		if result.FixtureGeneration != nil {
			fixtureGeneration = result.FixtureGeneration.SteadyMedian.WallTime.String()
		}
		fmt.Fprintf(&text, "| %s | %d | %s | %s | %s | %d | %.2f | %.2f | %d | %d | %d | %s |\n",
			result.Name,
			result.LogicalItems,
			fixtureGeneration,
			result.ColdStart.WallTime,
			result.SteadyMedian.WallTime,
			result.SteadyMedian.OutputBytes,
			result.SteadyMedian.ThroughputPerSecond,
			result.SteadyMedian.MegabytesPerSecond,
			result.SteadyMedian.GoAllocationCount,
			result.SteadyMedian.GoAllocatedBytes,
			result.SteadyMedian.GoPeakHeapBytes,
			verdict,
		)
	}
	fmt.Fprintln(&text)
	fmt.Fprintln(&text, "## Cancellation evidence")
	fmt.Fprintln(&text)
	fmt.Fprintln(&text, "| Case | Runs | Progress | Maximum stop | Probe starts after stop | Recovery |")
	fmt.Fprintln(&text, "|---|---:|---|---:|---:|---|")
	for _, result := range report.Cases {
		if result.Cancellation == nil {
			continue
		}
		var maximumStop time.Duration
		var startsAfterStop uint64
		recovery := "n/a"
		for _, run := range result.Cancellation.Runs {
			maximumStop = max(maximumStop, run.StopDuration)
			startsAfterStop += run.ProbeStartsAfterCancel
			if run.Recovery != nil {
				if run.Recovery.RecoveryCompleted {
					recovery = "complete"
				} else {
					recovery = "failed"
				}
			}
		}
		progress := ""
		if len(result.Cancellation.Runs) > 0 {
			run := result.Cancellation.Runs[0]
			progress = fmt.Sprintf("%s at %d", run.ProgressUnit, run.InjectionThreshold)
		}
		fmt.Fprintf(&text, "| %s | %d | %s | %s | %d | %s |\n", result.Name, len(result.Cancellation.Runs), progress, maximumStop, startsAfterStop, recovery)
	}
	return text.String()
}

func medianObservation(values []Observation) Observation {
	return Observation{
		InputBytes:             medianUint64(values, func(value Observation) uint64 { return value.InputBytes }),
		OutputBytes:            medianUint64(values, func(value Observation) uint64 { return value.OutputBytes }),
		WallTime:               medianDuration(values, func(value Observation) time.Duration { return value.WallTime }),
		ThroughputPerSecond:    medianFloat64(values, func(value Observation) float64 { return value.ThroughputPerSecond }),
		MegabytesPerSecond:     medianFloat64(values, func(value Observation) float64 { return value.MegabytesPerSecond }),
		GoAllocatedBytes:       medianUint64(values, func(value Observation) uint64 { return value.GoAllocatedBytes }),
		GoAllocationCount:      medianUint64(values, func(value Observation) uint64 { return value.GoAllocationCount }),
		GoPeakHeapBytes:        medianUint64(values, func(value Observation) uint64 { return value.GoPeakHeapBytes }),
		LinuxPeakRSSBytes:      medianUint64(values, func(value Observation) uint64 { return value.LinuxPeakRSSBytes }),
		WindowsWorkingSetBytes: medianUint64(values, func(value Observation) uint64 { return value.WindowsWorkingSetBytes }),
		PeakCommittedBytes:     medianUint64(values, func(value Observation) uint64 { return value.PeakCommittedBytes }),
		SwapOrPagefileBytes:    medianUint64(values, func(value Observation) uint64 { return value.SwapOrPagefileBytes }),
		PagingReadBytes:        medianUint64(values, func(value Observation) uint64 { return value.PagingReadBytes }),
		PagingWriteBytes:       medianUint64(values, func(value Observation) uint64 { return value.PagingWriteBytes }),
	}
}

func medianUint64(values []Observation, get func(Observation) uint64) uint64 {
	numbers := make([]uint64, len(values))
	for index, value := range values {
		numbers[index] = get(value)
	}
	sort.Slice(numbers, func(left, right int) bool { return numbers[left] < numbers[right] })
	return numbers[len(numbers)/2]
}

func medianFloat64(values []Observation, get func(Observation) float64) float64 {
	numbers := make([]float64, len(values))
	for index, value := range values {
		numbers[index] = get(value)
	}
	sort.Float64s(numbers)
	return numbers[len(numbers)/2]
}

func medianDuration(values []Observation, get func(Observation) time.Duration) time.Duration {
	numbers := make([]time.Duration, len(values))
	for index, value := range values {
		numbers[index] = get(value)
	}
	sort.Slice(numbers, func(left, right int) bool { return numbers[left] < numbers[right] })
	return numbers[len(numbers)/2]
}

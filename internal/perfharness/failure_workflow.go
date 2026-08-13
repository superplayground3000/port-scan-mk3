package perfharness

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
	"github.com/xuxiping/port-scan-mk3/pkg/pressure"
	"github.com/xuxiping/port-scan-mk3/pkg/ratelimit"
	"github.com/xuxiping/port-scan-mk3/pkg/scanapp"
	"github.com/xuxiping/port-scan-mk3/pkg/state"
)

// FailureSpec defines one bounded production failure run.
type FailureSpec struct {
	OutputDir string `json:"output_dir"`
	Items     uint64 `json:"items"`
	Workers   int    `json:"workers"`
	Scenario  string `json:"scenario"`
}

// FailureResult records an expected production failure.
type FailureResult struct {
	Scenario         string                   `json:"scenario"`
	Observed         bool                     `json:"observed"`
	ErrorText        string                   `json:"error_text"`
	ErrorClass       string                   `json:"error_class"`
	Operation        string                   `json:"operation"`
	TotalItems       uint64                   `json:"total_items"`
	Preparation      Observation              `json:"preparation"`
	StageObservation Observation              `json:"stage_observation"`
	Output           *FailureOutputEvidence   `json:"output,omitempty"`
	Snapshot         *FailureSnapshotEvidence `json:"snapshot,omitempty"`
	Pressure         *FailurePressureEvidence `json:"pressure,omitempty"`
}

// FailurePressureEvidence records fatal pressure rewind and recovery correctness.
type FailurePressureEvidence struct {
	PressureFailures        uint64 `json:"pressure_failures"`
	ProbeStarts             uint64 `json:"probe_starts"`
	ProbeStartsAfterFailure uint64 `json:"probe_starts_after_failure"`
	RewoundChunks           uint64 `json:"rewound_chunks"`
	SavedCursor             uint64 `json:"saved_cursor"`
	Remaining               uint64 `json:"remaining"`
	RowsBeforeRecovery      uint64 `json:"rows_before_recovery"`
	OpenRowsBeforeRecovery  uint64 `json:"open_rows_before_recovery"`
	HandlesReleased         bool   `json:"handles_released"`
	RecoveryCompleted       bool   `json:"recovery_completed"`
	RecoveryTaskCount       uint64 `json:"recovery_task_count"`
	RecoveryTaskDigest      string `json:"recovery_task_digest"`
	ReferenceTaskDigest     string `json:"reference_task_digest"`
	FinalScanRows           uint64 `json:"final_scan_rows"`
	FinalOpenRows           uint64 `json:"final_open_rows"`
	FinalCursor             uint64 `json:"final_cursor"`
}

// FailureSnapshotEvidence records atomic-save and precedence correctness.
type FailureSnapshotEvidence struct {
	FailureOperation string `json:"failure_operation"`
	PreviousDigest   string `json:"previous_digest"`
	AfterDigest      string `json:"after_digest"`
	PreviousLoadable bool   `json:"previous_loadable"`
	TempFilesRemoved bool   `json:"temp_files_removed"`
	HandleReleased   bool   `json:"handle_released"`
	ErrorPrecedence  bool   `json:"error_precedence"`
	PressureFailures uint64 `json:"pressure_failures"`
	RewoundChunks    uint64 `json:"rewound_chunks"`
}

// Correct reports whether one failure run has the required stable evidence.
func (result FailureResult) Correct() bool {
	common := result.Observed && result.ErrorText != "" && result.ErrorClass != "" && result.Operation != "" && result.TotalItems > 0
	if !common {
		return false
	}
	if result.Scenario == "snapshot-save-failure" {
		snapshot := result.Snapshot
		return snapshot != nil && snapshot.FailureOperation == string(state.SaveFailureReplace) &&
			snapshot.PreviousDigest != "" && snapshot.PreviousDigest == snapshot.AfterDigest &&
			snapshot.PreviousLoadable && snapshot.TempFilesRemoved && snapshot.HandleReleased &&
			snapshot.ErrorPrecedence && snapshot.PressureFailures == 3
	}
	if result.Scenario == "pressure-fatal-error" {
		pressure := result.Pressure
		return pressure != nil && pressure.PressureFailures == 3 && pressure.ProbeStartsAfterFailure == 0 &&
			pressure.SavedCursor+pressure.Remaining == result.TotalItems &&
			pressure.HandlesReleased && pressure.RecoveryCompleted && pressure.RecoveryTaskCount == pressure.Remaining &&
			pressure.RecoveryTaskDigest != "" && pressure.RecoveryTaskDigest == pressure.ReferenceTaskDigest &&
			pressure.FinalScanRows == pressure.RowsBeforeRecovery+pressure.Remaining &&
			pressure.FinalOpenRows == pressure.OpenRowsBeforeRecovery+pressure.Remaining && pressure.FinalCursor == result.TotalItems
	}
	if result.Scenario != "output-failure" {
		return false
	}
	output := result.Output
	return output != nil && output.FailureAtResult > 0 && output.RewoundChunks > 0 &&
		output.ProbeStartsAfterFailure == 0 && output.SavedCursor+output.Remaining == result.TotalItems &&
		output.HandlesReleased && output.RecoveryCompleted && output.RecoveryTaskCount == output.Remaining &&
		output.RecoveryTaskDigest != "" && output.RecoveryTaskDigest == output.ReferenceTaskDigest &&
		output.FinalScanRows == output.RowsBeforeRecovery+output.Remaining &&
		output.FinalOpenRows == output.OpenRowsBeforeRecovery+output.Remaining && output.FinalCursor == result.TotalItems
}

// FailureOutputEvidence records output rewind and recovery correctness.
type FailureOutputEvidence struct {
	FailureAtResult         uint64 `json:"failure_at_result"`
	RewoundChunks           uint64 `json:"rewound_chunks"`
	ProbeStarts             uint64 `json:"probe_starts"`
	ProbeStartsAfterFailure uint64 `json:"probe_starts_after_failure"`
	SavedCursor             uint64 `json:"saved_cursor"`
	Remaining               uint64 `json:"remaining"`
	RowsBeforeRecovery      uint64 `json:"rows_before_recovery"`
	OpenRowsBeforeRecovery  uint64 `json:"open_rows_before_recovery"`
	HandlesReleased         bool   `json:"handles_released"`
	RecoveryCompleted       bool   `json:"recovery_completed"`
	RecoveryTaskCount       uint64 `json:"recovery_task_count"`
	RecoveryTaskDigest      string `json:"recovery_task_digest"`
	ReferenceTaskDigest     string `json:"reference_task_digest"`
	FinalScanRows           uint64 `json:"final_scan_rows"`
	FinalOpenRows           uint64 `json:"final_open_rows"`
	FinalCursor             uint64 `json:"final_cursor"`
}

type failureInputs struct {
	manifest       Manifest
	portPath       string
	snapshotPath   string
	snapshotDigest string
}

type failureStageState struct {
	scanConfig        config.ScanConfig
	outputFailAt      uint64
	probeTelemetry    scanapp.ProbeTelemetry
	snapshotTelemetry scanapp.SnapshotTelemetry
	pressureFailures  uint64
	errorPrecedence   bool
}

// RunFailureSmoke executes one expected production failure.
func (Suite) RunFailureSmoke(ctx context.Context, spec FailureSpec) (FailureResult, error) {
	if spec.Items == 0 {
		return FailureResult{}, fmt.Errorf("failure smoke items must be positive")
	}
	if err := os.MkdirAll(spec.OutputDir, 0o755); err != nil {
		return FailureResult{}, fmt.Errorf("create failure smoke directory: %w", err)
	}
	var inputs failureInputs
	preparation, err := New().Measure(ctx, 0, spec.Items, func(prepareCtx context.Context) (uint64, error) {
		prepared, prepareErr := prepareFailureInputs(prepareCtx, spec)
		inputs = prepared
		return prepared.manifest.ActualBytes, prepareErr
	})
	if err != nil {
		return FailureResult{}, err
	}
	var result FailureResult
	var stageState failureStageState
	stage, err := New().Measure(ctx, inputs.manifest.ActualBytes, spec.Items, func(stageCtx context.Context) (uint64, error) {
		observed, stageErr := runFailureStage(stageCtx, spec, inputs, &stageState)
		result = observed
		return 0, stageErr
	})
	if err != nil {
		return FailureResult{}, err
	}
	result.TotalItems = spec.Items
	result.Preparation = preparation
	result.StageObservation = stage
	if result.Scenario == "output-failure" {
		result.Output, err = completeOutputFailureEvidence(ctx, spec, inputs, stageState)
		if err != nil {
			return FailureResult{}, err
		}
	} else if result.Scenario == "snapshot-save-failure" {
		result.Snapshot, err = completeSnapshotFailureEvidence(inputs, stageState)
		if err != nil {
			return FailureResult{}, err
		}
	} else if result.Scenario == "pressure-fatal-error" {
		result.Pressure, err = completePressureFailureEvidence(ctx, spec, inputs, stageState)
		if err != nil {
			return FailureResult{}, err
		}
	}
	return result, nil
}

func prepareFailureInputs(ctx context.Context, spec FailureSpec) (failureInputs, error) {
	manifest, err := New().Generate(ctx, FixtureSpec{
		Family: FamilyCandidateHeavy,
		Scale:  Scale{InputRecords: spec.Items, CandidateAddresses: spec.Items},
		Seed:   DefaultGeneratorSeed,
	}, filepath.Join(spec.OutputDir, "fixture"))
	if err != nil {
		return failureInputs{}, err
	}
	inputs := failureInputs{
		manifest: manifest,
		portPath: filepath.Join(spec.OutputDir, "ports.csv"),
	}
	if err := os.WriteFile(inputs.portPath, []byte("443/tcp\n"), 0o644); err != nil {
		return failureInputs{}, fmt.Errorf("write failure smoke ports: %w", err)
	}
	inputs.snapshotPath = filepath.Join(spec.OutputDir, "buckets.json")
	bucketConfig, err := failureBucketConfig(spec, inputs, inputs.snapshotPath)
	if err != nil {
		return failureInputs{}, err
	}
	if err := scanapp.GenerateBuckets(ctx, bucketConfig, io.Discard, scanapp.GenerateBucketsOptions{}); err != nil {
		return failureInputs{}, err
	}
	inputs.snapshotDigest, err = fileDigest(inputs.snapshotPath)
	if err != nil {
		return failureInputs{}, err
	}
	return inputs, nil
}

func failureBucketConfig(spec FailureSpec, inputs failureInputs, snapshotPath string) (config.GenerateBucketsConfig, error) {
	return config.NewGenerateBucketsWithResourceLimits(config.GenerateBucketsValues{
		CIDRFile: inputs.manifest.ArtifactPath, CIDRIPCol: "ip", CIDRIPCidrCol: "ip_cidr",
		PortFile: inputs.portPath, SnapshotOutput: snapshotPath, Workers: spec.Workers,
		ProgressInterval: int(spec.Items) + 1,
	}, cancellationGenerateResourceLimits())
}

func runFailureStage(ctx context.Context, spec FailureSpec, inputs failureInputs, stageState *failureStageState) (FailureResult, error) {
	switch spec.Scenario {
	case "snapshot-save-failure":
		pressurePolicy, err := config.SimplePressure("http://performance.invalid", time.Millisecond)
		if err != nil {
			return FailureResult{}, err
		}
		scanConfig, err := config.NewScanWithResourceLimits(config.ScanValues{
			CIDRFile: inputs.manifest.ArtifactPath, CIDRIPCol: "ip", CIDRIPCidrCol: "ip_cidr",
			PortFile: inputs.portPath, ResumeInput: inputs.snapshotPath, Output: filepath.Join(spec.OutputDir, "results.csv"),
			Workers: spec.Workers, DialTimeout: time.Second, DispatchDelay: time.Millisecond,
			BucketRate: ratelimit.MaxRate, BucketCapacity: ratelimit.MaxCapacity, OutputFlushResults: 1000,
			LogLevel: "error", Format: "json", Quiet: true, Pressure: pressurePolicy,
		}, cancellationScanResourceLimits())
		if err != nil {
			return FailureResult{}, err
		}
		var failures atomic.Uint64
		runErr := scanapp.Run(ctx, scanConfig, io.Discard, io.Discard, scanapp.RunOptions{
			DisableKeyboard: true,
			PressureSource:  failingPressureSource{failures: &failures},
			Dial:            func(context.Context, string, string) (net.Conn, error) { return fakeOpenConn{}, nil },
			SnapshotFailure: &state.SaveFailureInjection{Operation: state.SaveFailureReplace},
			SnapshotTelemetryObserver: func(telemetry scanapp.SnapshotTelemetry) {
				stageState.snapshotTelemetry = telemetry
			},
		})
		stageState.pressureFailures = failures.Load()
		stageState.errorPrecedence = errors.Is(runErr, state.ErrInjectedSnapshotSaveFailure)
		return expectedFailure(spec.Scenario, "snapshot-replace", "snapshot-save", runErr, "injected snapshot save failure")
	case "output-failure":
		scanConfig, err := config.NewScan(config.ScanValues{
			CIDRFile: inputs.manifest.ArtifactPath, CIDRIPCol: "ip", CIDRIPCidrCol: "ip_cidr",
			PortFile: inputs.portPath, ResumeInput: inputs.snapshotPath, Output: filepath.Join(spec.OutputDir, "results.csv"),
			Workers: spec.Workers, DialTimeout: time.Second, DispatchDelay: 0,
			BucketRate: ratelimit.MaxRate, BucketCapacity: ratelimit.MaxCapacity, OutputFlushResults: failureFlushResults(spec.Items),
			LogLevel: "error", Format: "json", Quiet: true, Pressure: config.PressureDisabled(),
		})
		if err != nil {
			return FailureResult{}, err
		}
		stageState.scanConfig = scanConfig
		stageState.outputFailAt = max(uint64(1), spec.Items/2)
		runErr := scanapp.Run(ctx, scanConfig, io.Discard, io.Discard, scanapp.RunOptions{
			DisableKeyboard: true,
			Dial:            func(context.Context, string, string) (net.Conn, error) { return fakeOpenConn{}, nil },
			OutputFailure:   &scanapp.OutputFailureInjection{FailOnResult: stageState.outputFailAt},
			ProbeTelemetryObserver: func(telemetry scanapp.ProbeTelemetry) {
				stageState.probeTelemetry = telemetry
			},
			SnapshotTelemetryObserver: func(telemetry scanapp.SnapshotTelemetry) {
				stageState.snapshotTelemetry = telemetry
			},
		})
		if !errors.Is(runErr, scanapp.ErrInjectedOutputFailure) {
			return FailureResult{}, fmt.Errorf("scenario %s error = %v, want injected output failure", spec.Scenario, runErr)
		}
		return expectedFailure(spec.Scenario, "output-write", "output", runErr, "injected output write failure")
	case "pressure-fatal-error":
		pressurePolicy, err := config.SimplePressure("http://performance.invalid", time.Millisecond)
		if err != nil {
			return FailureResult{}, err
		}
		scanConfig, err := config.NewScanWithResourceLimits(config.ScanValues{
			CIDRFile: inputs.manifest.ArtifactPath, CIDRIPCol: "ip", CIDRIPCidrCol: "ip_cidr",
			PortFile: inputs.portPath, ResumeInput: inputs.snapshotPath, Output: filepath.Join(spec.OutputDir, "results.csv"),
			Workers: spec.Workers, DialTimeout: time.Second, DispatchDelay: time.Millisecond,
			BucketRate: 1, BucketCapacity: 1, OutputFlushResults: 1000, LogLevel: "error", Format: "json", Quiet: true,
			Pressure: pressurePolicy,
		}, cancellationScanResourceLimits())
		if err != nil {
			return FailureResult{}, err
		}
		stageState.scanConfig = scanConfig
		var failures atomic.Uint64
		runErr := scanapp.Run(ctx, scanConfig, io.Discard, io.Discard, scanapp.RunOptions{
			DisableKeyboard: true,
			PressureSource:  failingPressureSource{failures: &failures},
			Dial: func(context.Context, string, string) (net.Conn, error) {
				return fakeOpenConn{}, nil
			},
			ProbeTelemetryObserver: func(telemetry scanapp.ProbeTelemetry) {
				stageState.probeTelemetry = telemetry
			},
			SnapshotTelemetryObserver: func(telemetry scanapp.SnapshotTelemetry) {
				stageState.snapshotTelemetry = telemetry
			},
		})
		stageState.pressureFailures = failures.Load()
		return expectedFailure(spec.Scenario, "pressure-poll", "pressure-fatal", runErr, "pressure api failed 3 times")
	default:
		return FailureResult{}, fmt.Errorf("unsupported failure scenario %q", spec.Scenario)
	}
}

func failureFlushResults(items uint64) int {
	return int(min(uint64(1000), max(uint64(1), items/10)))
}

func completeOutputFailureEvidence(ctx context.Context, spec FailureSpec, inputs failureInputs, stage failureStageState) (*FailureOutputEvidence, error) {
	snapshot, err := state.LoadSnapshot(inputs.snapshotPath)
	if err != nil {
		return nil, fmt.Errorf("load output-failure snapshot: %w", err)
	}
	var savedCursor uint64
	var remaining uint64
	for _, chunk := range snapshot.Chunks {
		savedCursor += uint64(chunk.NextIndex)
		remaining += uint64(chunk.Remaining())
	}
	if snapshot.Output == nil {
		return nil, fmt.Errorf("output-failure snapshot has no output paths")
	}
	scanPath := snapshot.Output.ScanPath
	openPath := snapshot.Output.OpenPath
	scanRows, err := countCSVRows(scanPath)
	if err != nil {
		return nil, err
	}
	openRows, err := countCSVRows(openPath)
	if err != nil {
		return nil, err
	}
	handlesReleased := outputHandlesReleased(scanPath, openPath)
	reference, err := failureCandidateTaskEvidence(snapshot, spec.Items)
	if err != nil {
		return nil, fmt.Errorf("build output-failure task reference: %w", err)
	}
	recoveryTasks := newOrderedTaskEvidence()
	if remaining > 0 {
		if err := scanapp.Run(ctx, stage.scanConfig, io.Discard, io.Discard, scanapp.RunOptions{
			DisableKeyboard: true,
			Dial:            func(context.Context, string, string) (net.Conn, error) { return fakeOpenConn{}, nil },
			TaskObserver: func(ip string, port int) {
				recoveryTasks.Observe(net.JoinHostPort(ip, strconv.Itoa(port)) + "/tcp")
			},
		}); err != nil {
			return nil, fmt.Errorf("recover output-failure scan: %w", err)
		}
	}
	recovered := recoveryTasks.Snapshot()
	finalScanRows, err := countCSVRows(scanPath)
	if err != nil {
		return nil, err
	}
	finalOpenRows, err := countCSVRows(openPath)
	if err != nil {
		return nil, err
	}
	finalCursor := savedCursor + recovered.Count
	return &FailureOutputEvidence{
		FailureAtResult:         stage.outputFailAt,
		RewoundChunks:           stage.snapshotTelemetry.RewoundChunks,
		ProbeStarts:             stage.probeTelemetry.TotalStarted,
		ProbeStartsAfterFailure: stage.probeTelemetry.StartsAfterStop,
		SavedCursor:             savedCursor,
		Remaining:               remaining,
		RowsBeforeRecovery:      scanRows,
		OpenRowsBeforeRecovery:  openRows,
		HandlesReleased:         handlesReleased,
		RecoveryCompleted: savedCursor+remaining == spec.Items && recovered.Count == remaining && recovered.Digest == reference.Digest &&
			finalScanRows == scanRows+remaining && finalOpenRows == openRows+remaining && finalCursor == spec.Items,
		RecoveryTaskCount:   recovered.Count,
		RecoveryTaskDigest:  recovered.Digest,
		ReferenceTaskDigest: reference.Digest,
		FinalScanRows:       finalScanRows,
		FinalOpenRows:       finalOpenRows,
		FinalCursor:         finalCursor,
	}, nil
}

func completeSnapshotFailureEvidence(inputs failureInputs, stage failureStageState) (*FailureSnapshotEvidence, error) {
	afterDigest, err := fileDigest(inputs.snapshotPath)
	if err != nil {
		return nil, err
	}
	_, loadErr := state.LoadSnapshot(inputs.snapshotPath)
	tempFiles, globErr := filepath.Glob(inputs.snapshotPath + ".tmp-*")
	if globErr != nil {
		return nil, fmt.Errorf("find snapshot temp files: %w", globErr)
	}
	return &FailureSnapshotEvidence{
		FailureOperation: string(state.SaveFailureReplace),
		PreviousDigest:   inputs.snapshotDigest,
		AfterDigest:      afterDigest,
		PreviousLoadable: loadErr == nil,
		TempFilesRemoved: len(tempFiles) == 0,
		HandleReleased:   outputHandlesReleased(inputs.snapshotPath),
		ErrorPrecedence:  stage.errorPrecedence,
		PressureFailures: stage.pressureFailures,
		RewoundChunks:    stage.snapshotTelemetry.RewoundChunks,
	}, nil
}

func completePressureFailureEvidence(ctx context.Context, spec FailureSpec, inputs failureInputs, stage failureStageState) (*FailurePressureEvidence, error) {
	snapshot, err := state.LoadSnapshotWithLimits(inputs.snapshotPath, cancellationSnapshotLimits())
	if err != nil {
		return nil, fmt.Errorf("load pressure-fatal snapshot: %w", err)
	}
	if snapshot.Output == nil {
		return nil, fmt.Errorf("pressure-fatal snapshot has no output paths")
	}
	var savedCursor uint64
	var remaining uint64
	for _, chunk := range snapshot.Chunks {
		savedCursor += uint64(chunk.NextIndex)
		remaining += uint64(chunk.Remaining())
	}
	scanPath := snapshot.Output.ScanPath
	openPath := snapshot.Output.OpenPath
	scanRows, err := countCSVRows(scanPath)
	if err != nil {
		return nil, err
	}
	openRows, err := countCSVRows(openPath)
	if err != nil {
		return nil, err
	}
	handlesReleased := outputHandlesReleased(scanPath, openPath)
	reference, err := failureCandidateTaskEvidence(snapshot, spec.Items)
	if err != nil {
		return nil, fmt.Errorf("build pressure-fatal task reference: %w", err)
	}
	recoveryConfig, err := config.NewScanWithResourceLimits(config.ScanValues{
		CIDRFile: inputs.manifest.ArtifactPath, CIDRIPCol: "ip", CIDRIPCidrCol: "ip_cidr",
		PortFile: inputs.portPath, ResumeInput: inputs.snapshotPath, Output: filepath.Join(spec.OutputDir, "results.csv"),
		Workers: spec.Workers, DialTimeout: time.Second, DispatchDelay: 0,
		BucketRate: ratelimit.MaxRate, BucketCapacity: ratelimit.MaxCapacity, OutputFlushResults: 1000,
		LogLevel: "error", Format: "json", Quiet: true, Pressure: config.PressureDisabled(),
	}, cancellationScanResourceLimits())
	if err != nil {
		return nil, err
	}
	recoveryTasks := newOrderedTaskEvidence()
	if remaining > 0 {
		if err := scanapp.Run(ctx, recoveryConfig, io.Discard, io.Discard, scanapp.RunOptions{
			DisableKeyboard: true,
			Dial:            func(context.Context, string, string) (net.Conn, error) { return fakeOpenConn{}, nil },
			TaskObserver: func(ip string, port int) {
				recoveryTasks.Observe(net.JoinHostPort(ip, strconv.Itoa(port)) + "/tcp")
			},
		}); err != nil {
			return nil, fmt.Errorf("recover pressure-fatal scan: %w", err)
		}
	}
	recovered := recoveryTasks.Snapshot()
	finalScanRows, err := countCSVRows(scanPath)
	if err != nil {
		return nil, err
	}
	finalOpenRows, err := countCSVRows(openPath)
	if err != nil {
		return nil, err
	}
	finalCursor := savedCursor + recovered.Count
	return &FailurePressureEvidence{
		PressureFailures:        stage.pressureFailures,
		ProbeStarts:             stage.probeTelemetry.TotalStarted,
		ProbeStartsAfterFailure: stage.probeTelemetry.StartsAfterStop,
		RewoundChunks:           stage.snapshotTelemetry.RewoundChunks,
		SavedCursor:             savedCursor,
		Remaining:               remaining,
		RowsBeforeRecovery:      scanRows,
		OpenRowsBeforeRecovery:  openRows,
		HandlesReleased:         handlesReleased,
		RecoveryCompleted: savedCursor+remaining == spec.Items && recovered.Count == remaining && recovered.Digest == reference.Digest &&
			finalScanRows == scanRows+remaining && finalOpenRows == openRows+remaining && finalCursor == spec.Items,
		RecoveryTaskCount:   recovered.Count,
		RecoveryTaskDigest:  recovered.Digest,
		ReferenceTaskDigest: reference.Digest,
		FinalScanRows:       finalScanRows,
		FinalOpenRows:       finalOpenRows,
		FinalCursor:         finalCursor,
	}, nil
}

func failureCandidateTaskEvidence(snapshot state.Snapshot, items uint64) (taskEvidenceSnapshot, error) {
	if len(snapshot.Chunks) != 1 {
		return taskEvidenceSnapshot{}, fmt.Errorf("failure reference has %d chunks, want one", len(snapshot.Chunks))
	}
	chunk := snapshot.Chunks[0]
	if chunk.TotalCount < 0 || uint64(chunk.TotalCount) != items || chunk.NextIndex < 0 || chunk.NextIndex > chunk.TotalCount ||
		len(chunk.Ports) != 1 || chunk.Ports[0] != "443/tcp" {
		return taskEvidenceSnapshot{}, fmt.Errorf("failure reference snapshot does not match the candidate fixture")
	}
	evidence := newOrderedTaskEvidence()
	for index := uint64(chunk.NextIndex); index < items; index++ {
		evidence.Observe(net.JoinHostPort(fixtureIPv4(index), "443") + "/tcp")
	}
	return evidence.Snapshot(), nil
}

func outputHandlesReleased(paths ...string) bool {
	for index, path := range paths {
		moved := path + fmt.Sprintf(".handle-check-%d", index)
		if err := os.Rename(path, moved); err != nil {
			return false
		}
		if err := os.Rename(moved, path); err != nil {
			return false
		}
	}
	return true
}

func expectedFailure(scenario, operation, errorClass string, err error, expected string) (FailureResult, error) {
	if err == nil || !strings.Contains(err.Error(), expected) {
		return FailureResult{}, fmt.Errorf("scenario %s error = %v, want text %q", scenario, err, expected)
	}
	return FailureResult{Scenario: scenario, Observed: true, ErrorText: err.Error(), Operation: operation, ErrorClass: errorClass}, nil
}

type failingPressureSource struct {
	failures *atomic.Uint64
}

func (source failingPressureSource) Sample(context.Context) (pressure.Sample, error) {
	if source.failures != nil {
		source.failures.Add(1)
	}
	return pressure.Sample{}, errors.New("injected pressure failure")
}

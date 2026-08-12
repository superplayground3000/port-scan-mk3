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
	"sync"
	"sync/atomic"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
	"github.com/xuxiping/port-scan-mk3/pkg/input"
	"github.com/xuxiping/port-scan-mk3/pkg/pressure"
	"github.com/xuxiping/port-scan-mk3/pkg/scanapp"
	"github.com/xuxiping/port-scan-mk3/pkg/state"
	"github.com/xuxiping/port-scan-mk3/pkg/task"
)

// CancellationSpec defines one bounded production cancellation run.
type CancellationSpec struct {
	OutputDir string            `json:"output_dir"`
	Items     uint64            `json:"items"`
	Workers   int               `json:"workers"`
	Stage     CancellationStage `json:"stage"`
	Percent   int               `json:"percent"`
}

// CancellationProgressUnit identifies the work unit that triggers cancellation.
type CancellationProgressUnit string

const (
	CancellationProgressInputRecords  CancellationProgressUnit = "input-records"
	CancellationProgressContextChecks CancellationProgressUnit = "context-checks"
	CancellationProgressBucketTasks   CancellationProgressUnit = "bucket-tasks"
	CancellationProgressResumeItems   CancellationProgressUnit = "resume-items"
	CancellationProgressOutputResults CancellationProgressUnit = "output-results"
)

// CancellationRecovery records the correctness of a production recovery run.
type CancellationRecovery struct {
	InitialCompleted    uint64 `json:"initial_completed"`
	SavedCursor         uint64 `json:"saved_cursor"`
	Remaining           uint64 `json:"remaining"`
	RecoveryCompleted   bool   `json:"recovery_completed"`
	RecoveryTaskCount   uint64 `json:"recovery_task_count"`
	RecoveryTaskDigest  string `json:"recovery_task_digest"`
	ReferenceTaskDigest string `json:"reference_task_digest"`
	FinalScanRows       uint64 `json:"final_scan_rows"`
	FinalOpenRows       uint64 `json:"final_open_rows"`
	FinalCursor         uint64 `json:"final_cursor"`
}

// CancellationResult records the observed stop after injection.
type CancellationResult struct {
	Stage                  CancellationStage        `json:"stage"`
	Percent                int                      `json:"percent"`
	Injected               bool                     `json:"injected"`
	StopDuration           time.Duration            `json:"stop_duration_ns"`
	Resumable              bool                     `json:"resumable"`
	Preparation            Observation              `json:"preparation"`
	StageObservation       Observation              `json:"stage_observation"`
	TotalItems             uint64                   `json:"total_items"`
	InjectionThreshold     uint64                   `json:"injection_threshold"`
	CompletedAtInjection   uint64                   `json:"completed_at_injection"`
	ProgressUnit           CancellationProgressUnit `json:"progress_unit"`
	ContextCanceled        bool                     `json:"context_canceled"`
	ProbeStarts            uint64                   `json:"probe_starts"`
	ProbeStartsAfterCancel uint64                   `json:"probe_starts_after_cancel"`
	Recovery               *CancellationRecovery    `json:"recovery,omitempty"`
}

// RunCancellationSmoke injects cancellation into one production data stage.
func (Suite) RunCancellationSmoke(ctx context.Context, spec CancellationSpec) (CancellationResult, error) {
	if spec.Items == 0 {
		return CancellationResult{}, fmt.Errorf("cancellation items must be positive")
	}
	if err := os.MkdirAll(spec.OutputDir, 0o755); err != nil {
		return CancellationResult{}, fmt.Errorf("create cancellation directory: %w", err)
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	trigger := &cancellationTrigger{cancel: cancel}
	injector, err := NewCancellationInjector(spec.Stage, spec.Percent, spec.Items, trigger.fire)
	if err != nil {
		return CancellationResult{}, err
	}

	result := CancellationResult{
		Stage:              spec.Stage,
		Percent:            spec.Percent,
		TotalItems:         spec.Items,
		InjectionThreshold: cancellationThreshold(spec.Items, spec.Percent),
	}
	var runErr error
	switch spec.Stage {
	case CancellationInputParsing:
		result.ProgressUnit = CancellationProgressInputRecords
		var manifest Manifest
		result.Preparation, err = New().Measure(ctx, 0, spec.Items, func(prepareCtx context.Context) (uint64, error) {
			generated, generateErr := cancellationInputManifest(prepareCtx, spec, "input")
			manifest = generated
			return generated.ActualBytes, generateErr
		})
		if err == nil {
			result.StageObservation, runErr = New().Measure(runCtx, manifest.ActualBytes, spec.Items, func(stageCtx context.Context) (uint64, error) {
				return 0, runInputParsingCancellation(stageCtx, manifest, spec, injector)
			})
		}
	case CancellationRichExpansion:
		result.ProgressUnit = CancellationProgressContextChecks
		var selectors []string
		result.Preparation, err = New().Measure(ctx, 0, spec.Items, func(context.Context) (uint64, error) {
			selectors = make([]string, spec.Items)
			for index := range selectors {
				selectors[index] = fixtureIPv4(uint64(index))
			}
			return 0, nil
		})
		if err == nil {
			result.StageObservation, runErr = New().Measure(runCtx, 0, spec.Items, func(stageCtx context.Context) (uint64, error) {
				return 0, runRichExpansionCancellation(stageCtx, selectors, trigger, result.InjectionThreshold)
			})
		}
	case CancellationBucketBuild:
		result.ProgressUnit = CancellationProgressBucketTasks
		var inputs cancellationInputPaths
		result.Preparation, err = New().Measure(ctx, 0, spec.Items, func(prepareCtx context.Context) (uint64, error) {
			prepared, prepareErr := prepareCancellationInputs(prepareCtx, spec)
			inputs = prepared
			return prepared.manifest.ActualBytes, prepareErr
		})
		if err == nil {
			result.StageObservation, runErr = New().Measure(runCtx, inputs.manifest.ActualBytes, spec.Items, func(stageCtx context.Context) (uint64, error) {
				return 0, runBucketCancellation(stageCtx, spec, injector, inputs)
			})
		}
	case CancellationResumeRebuild, CancellationResultOutput:
		if spec.Stage == CancellationResumeRebuild {
			result.ProgressUnit = CancellationProgressResumeItems
		} else {
			result.ProgressUnit = CancellationProgressOutputResults
		}
		var prepared cancellationScanRun
		result.Preparation, err = New().Measure(ctx, 0, spec.Items, func(prepareCtx context.Context) (uint64, error) {
			var prepareErr error
			prepared, prepareErr = prepareScanCancellation(prepareCtx, spec)
			if prepareErr != nil {
				return 0, prepareErr
			}
			return fileSize(prepared.inputs.snapshotPath)
		})
		if err == nil {
			result.StageObservation, runErr = New().Measure(runCtx, prepared.inputs.manifest.ActualBytes, spec.Items, func(stageCtx context.Context) (uint64, error) {
				return 0, runScanCancellation(stageCtx, spec, injector, trigger, prepared, &result)
			})
		}
	default:
		return CancellationResult{}, fmt.Errorf("unsupported cancellation stage %q", spec.Stage)
	}
	if err != nil {
		return result, err
	}
	result.Injected = trigger.fired()
	result.CompletedAtInjection = injector.completedAtInjection()
	if spec.Stage == CancellationRichExpansion && result.Injected {
		result.CompletedAtInjection = result.InjectionThreshold
	}
	if result.Injected {
		result.StopDuration = time.Since(trigger.at)
	}
	if spec.Stage == CancellationResumeRebuild || spec.Stage == CancellationResultOutput {
		snapshot, loadErr := state.LoadSnapshot(filepath.Join(spec.OutputDir, "buckets.json"))
		if loadErr != nil {
			return result, fmt.Errorf("load canceled production snapshot: %w", loadErr)
		}
		var savedCursor uint64
		var remaining uint64
		for _, chunk := range snapshot.Chunks {
			savedCursor += uint64(chunk.NextIndex)
			remaining += uint64(chunk.Remaining())
			if chunk.NextIndex < chunk.TotalCount {
				result.Resumable = true
			}
		}
		result.Recovery, err = recoverCanceledScan(ctx, spec, preparedScanInputs(spec), snapshot, result.CompletedAtInjection, savedCursor, remaining)
		if err != nil {
			return result, err
		}
	}
	result.ContextCanceled = errors.Is(runErr, context.Canceled)
	if !result.ContextCanceled {
		return result, fmt.Errorf("stage %s did not stop with cancellation: %v", spec.Stage, runErr)
	}
	return result, runErr
}

func cancellationInputManifest(ctx context.Context, spec CancellationSpec, directory string) (Manifest, error) {
	return New().Generate(ctx, FixtureSpec{
		Family: FamilyRecordHeavy,
		Scale:  Scale{InputRecords: spec.Items},
		Seed:   DefaultGeneratorSeed,
	}, filepath.Join(spec.OutputDir, directory))
}

func runInputParsingCancellation(ctx context.Context, manifest Manifest, spec CancellationSpec, injector *CancellationInjector) error {
	file, err := os.Open(manifest.ArtifactPath)
	if err != nil {
		return fmt.Errorf("open cancellation input: %w", err)
	}
	defer func() { _ = file.Close() }()
	reader := &progressReader{reader: file, totalBytes: manifest.ActualBytes, totalItems: spec.Items, injector: injector}
	_, err = input.LoadCIDRsWithColumnsContext(ctx, reader, "ip", "ip_cidr")
	return err
}

func runRichExpansionCancellation(ctx context.Context, selectors []string, trigger *cancellationTrigger, threshold uint64) error {
	_, err := task.ExpandIPSelectorsContext(&checkCountContext{
		Context:   ctx,
		threshold: threshold,
		trigger:   trigger,
	}, selectors)
	return err
}

func runBucketCancellation(ctx context.Context, spec CancellationSpec, injector *CancellationInjector, inputs cancellationInputPaths) error {
	cfg, err := config.NewGenerateBucketsWithResourceLimits(config.GenerateBucketsValues{
		CIDRFile:         inputs.manifest.ArtifactPath,
		CIDRIPCol:        "ip",
		CIDRIPCidrCol:    "ip_cidr",
		PortFile:         inputs.portPath,
		SnapshotOutput:   inputs.snapshotPath,
		Workers:          1,
		ProgressInterval: int(spec.Items) + 1,
	}, cancellationGenerateResourceLimits())
	if err != nil {
		return err
	}
	return scanapp.GenerateBuckets(ctx, cfg, io.Discard, scanapp.GenerateBucketsOptions{Reporter: &injectingReporter{injector: injector}})
}

type cancellationScanRun struct {
	inputs cancellationInputPaths
	config config.ScanConfig
}

func prepareScanCancellation(ctx context.Context, spec CancellationSpec) (cancellationScanRun, error) {
	inputs, err := prepareCancellationInputs(ctx, spec)
	if err != nil {
		return cancellationScanRun{}, err
	}
	bucketConfig, err := config.NewGenerateBucketsWithResourceLimits(config.GenerateBucketsValues{
		CIDRFile: inputs.manifest.ArtifactPath, CIDRIPCol: "ip", CIDRIPCidrCol: "ip_cidr",
		PortFile: inputs.portPath, SnapshotOutput: inputs.snapshotPath, Workers: spec.Workers,
		ProgressInterval: int(spec.Items) + 1,
	}, cancellationGenerateResourceLimits())
	if err != nil {
		return cancellationScanRun{}, err
	}
	if err := scanapp.GenerateBuckets(ctx, bucketConfig, io.Discard, scanapp.GenerateBucketsOptions{}); err != nil {
		return cancellationScanRun{}, err
	}
	scanConfig, err := config.NewScanWithResourceLimits(config.ScanValues{
		CIDRFile: inputs.manifest.ArtifactPath, CIDRIPCol: "ip", CIDRIPCidrCol: "ip_cidr",
		PortFile: inputs.portPath, ResumeInput: inputs.snapshotPath, Output: filepath.Join(spec.OutputDir, "results.csv"),
		Workers: 1, DialTimeout: time.Second, BucketRate: 1,
		BucketCapacity: 1, OutputFlushResults: 1, LogLevel: "error", Format: "json", Quiet: true,
		Pressure: config.PressureDisabled(),
	}, cancellationScanResourceLimits())
	if err != nil {
		return cancellationScanRun{}, err
	}
	return cancellationScanRun{inputs: inputs, config: scanConfig}, nil
}

func runScanCancellation(ctx context.Context, spec CancellationSpec, injector *CancellationInjector, trigger *cancellationTrigger, prepared cancellationScanRun, result *CancellationResult) error {
	opts := scanapp.RunOptions{
		DisableKeyboard: true,
		Dial: func(context.Context, string, string) (net.Conn, error) {
			return fakeOpenConn{}, nil
		},
		ProbeTelemetryObserver: func(telemetry scanapp.ProbeTelemetry) {
			result.ProbeStarts = telemetry.TotalStarted
			result.ProbeStartsAfterCancel = telemetry.StartsAfterStop
		},
	}
	if spec.Stage == CancellationResumeRebuild {
		opts.ResumeObserver = func(completed, _ int) { injector.Tick(uint64(completed)) }
	} else {
		opts.ResultObserver = injector.Tick
	}
	return scanapp.Run(ctx, prepared.config, io.Discard, io.Discard, opts)
}

func preparedScanInputs(spec CancellationSpec) cancellationInputPaths {
	return cancellationInputPaths{
		manifest:     Manifest{ArtifactPath: filepath.Join(spec.OutputDir, "fixture", "input.csv")},
		portPath:     filepath.Join(spec.OutputDir, "ports.csv"),
		snapshotPath: filepath.Join(spec.OutputDir, "buckets.json"),
	}
}

func recoverCanceledScan(ctx context.Context, spec CancellationSpec, inputs cancellationInputPaths, canceled state.Snapshot, initialCompleted, savedCursor, remaining uint64) (*CancellationRecovery, error) {
	reference, err := cancellationSnapshotTaskEvidence(canceled, true)
	if err != nil {
		return nil, fmt.Errorf("build canceled task reference: %w", err)
	}
	tasks := newOrderedTaskEvidence()
	if remaining > 0 {
		recoveryConfig, err := config.NewScanWithResourceLimits(config.ScanValues{
			CIDRFile: inputs.manifest.ArtifactPath, CIDRIPCol: "ip", CIDRIPCidrCol: "ip_cidr",
			PortFile: inputs.portPath, ResumeInput: inputs.snapshotPath, Output: filepath.Join(spec.OutputDir, "results.csv"),
			Workers: spec.Workers, DialTimeout: time.Second, BucketRate: 1,
			BucketCapacity: 1, OutputFlushResults: 1000, LogLevel: "error", Format: "json", Quiet: true,
			Pressure: config.PressureDisabled(),
		}, cancellationScanResourceLimits())
		if err != nil {
			return nil, err
		}
		if err := scanapp.Run(ctx, recoveryConfig, io.Discard, io.Discard, scanapp.RunOptions{
			DisableKeyboard: true,
			Dial: func(context.Context, string, string) (net.Conn, error) {
				return fakeOpenConn{}, nil
			},
			TaskObserver: func(ip string, taskPort int) {
				taskValue := net.JoinHostPort(ip, strconv.Itoa(taskPort)) + "/tcp"
				tasks.Observe(taskValue)
			},
		}); err != nil {
			return nil, fmt.Errorf("recover canceled production scan: %w", err)
		}
	}
	recovered := tasks.Snapshot()
	scanPath, openPath, err := workflowOutputPaths(spec.OutputDir)
	if err != nil {
		return nil, err
	}
	scanRows, err := countCSVRows(scanPath)
	if err != nil {
		return nil, err
	}
	openRows, err := countCSVRows(openPath)
	if err != nil {
		return nil, err
	}
	// A fully completed run does not rewrite the resume snapshot. The logical
	// final cursor therefore comes from the saved prefix plus the observed
	// recovery suffix, not from reloading the intentionally unchanged file.
	finalCursor := savedCursor + recovered.Count
	completed := savedCursor+remaining == spec.Items && recovered.Count == remaining &&
		recovered.Digest == reference.Digest && scanRows == spec.Items && openRows == spec.Items &&
		finalCursor == spec.Items
	return &CancellationRecovery{
		InitialCompleted:    initialCompleted,
		SavedCursor:         savedCursor,
		Remaining:           remaining,
		RecoveryCompleted:   completed,
		RecoveryTaskCount:   recovered.Count,
		RecoveryTaskDigest:  recovered.Digest,
		ReferenceTaskDigest: reference.Digest,
		FinalScanRows:       scanRows,
		FinalOpenRows:       openRows,
		FinalCursor:         finalCursor,
	}, nil
}

func cancellationSnapshotTaskEvidence(snapshot state.Snapshot, remainingOnly bool) (taskEvidenceSnapshot, error) {
	evidence := newOrderedTaskEvidence()
	for _, chunk := range snapshot.Chunks {
		if len(chunk.Ports) != 1 || !strings.HasSuffix(chunk.Ports[0], "/tcp") || chunk.TotalCount != 1 {
			return taskEvidenceSnapshot{}, fmt.Errorf("cancellation reference requires one TCP task per chunk")
		}
		start := 0
		if remainingOnly {
			start = chunk.NextIndex
		}
		if start >= chunk.TotalCount {
			continue
		}
		port, err := strconv.Atoi(strings.TrimSuffix(chunk.Ports[0], "/tcp"))
		if err != nil {
			return taskEvidenceSnapshot{}, fmt.Errorf("parse cancellation reference port: %w", err)
		}
		ip, _, err := net.ParseCIDR(chunk.CIDR)
		if err != nil {
			return taskEvidenceSnapshot{}, fmt.Errorf("parse cancellation reference CIDR: %w", err)
		}
		evidence.Observe(net.JoinHostPort(ip.String(), strconv.Itoa(port)) + "/tcp")
	}
	return evidence.Snapshot(), nil
}

func cancellationInputs(ctx context.Context, spec CancellationSpec) (Manifest, string, string, error) {
	inputs, err := prepareCancellationInputs(ctx, spec)
	return inputs.manifest, inputs.portPath, inputs.snapshotPath, err
}

type cancellationInputPaths struct {
	manifest     Manifest
	portPath     string
	snapshotPath string
}

func prepareCancellationInputs(ctx context.Context, spec CancellationSpec) (cancellationInputPaths, error) {
	manifest, err := New().Generate(ctx, FixtureSpec{
		Family: FamilyCandidateHeavy,
		Shape:  "unique-groups",
		Scale:  Scale{InputRecords: spec.Items, CandidateAddresses: spec.Items},
		Seed:   DefaultGeneratorSeed,
	}, filepath.Join(spec.OutputDir, "fixture"))
	if err != nil {
		return cancellationInputPaths{}, err
	}
	portPath := filepath.Join(spec.OutputDir, "ports.csv")
	if err := os.WriteFile(portPath, []byte("443/tcp\n"), 0o644); err != nil {
		return cancellationInputPaths{}, fmt.Errorf("write cancellation ports: %w", err)
	}
	return cancellationInputPaths{manifest: manifest, portPath: portPath, snapshotPath: filepath.Join(spec.OutputDir, "buckets.json")}, nil
}

type cancellationTrigger struct {
	cancel     context.CancelFunc
	once       sync.Once
	at         time.Time
	firedValue atomic.Bool
}

func (trigger *cancellationTrigger) fire() {
	trigger.once.Do(func() {
		trigger.at = time.Now()
		trigger.firedValue.Store(true)
		trigger.cancel()
	})
}

func (trigger *cancellationTrigger) fired() bool { return trigger.firedValue.Load() }

type progressReader struct {
	reader     io.Reader
	totalBytes uint64
	totalItems uint64
	read       uint64
	injector   *CancellationInjector
}

func (reader *progressReader) Read(data []byte) (int, error) {
	count, err := reader.reader.Read(data)
	reader.read += uint64(count)
	reader.injector.Tick(reader.read * reader.totalItems / reader.totalBytes)
	return count, err
}

type checkCountContext struct {
	context.Context
	checks    uint64
	threshold uint64
	trigger   *cancellationTrigger
}

func (ctx *checkCountContext) Err() error {
	if err := ctx.Context.Err(); err != nil {
		return err
	}
	ctx.checks++
	if ctx.checks >= ctx.threshold {
		ctx.trigger.fire()
		return context.Canceled
	}
	return nil
}

type injectingReporter struct {
	completed atomic.Uint64
	injector  *CancellationInjector
}

func (reporter *injectingReporter) Inc() { reporter.Add(1) }
func (reporter *injectingReporter) Add(n int) {
	completed := reporter.completed.Add(uint64(n))
	reporter.injector.Tick(completed)
}
func (*injectingReporter) Done() {}

func cancellationThreshold(total uint64, percent int) uint64 {
	return (total*uint64(percent) + 99) / 100
}

func cancellationSnapshotLimits() state.SnapshotLimits {
	limits := state.DefaultSnapshotLimits()
	limits.MaxBytes = 0
	return limits
}

func cancellationGenerateResourceLimits() config.GenerateBucketsResourceLimits {
	return config.GenerateBucketsResourceLimits{
		CIDR:     input.DefaultCIDRLimits(""),
		Port:     input.DefaultPortLimits(""),
		Snapshot: cancellationSnapshotLimits(),
	}
}

func cancellationScanResourceLimits() config.ScanResourceLimits {
	return config.ScanResourceLimits{
		CIDR:     input.DefaultCIDRLimits(""),
		Port:     input.DefaultPortLimits(""),
		Snapshot: cancellationSnapshotLimits(),
		Pressure: pressure.DefaultResponseLimits(),
	}
}

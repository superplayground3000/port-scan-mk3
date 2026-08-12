package perfharness

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
	"github.com/xuxiping/port-scan-mk3/pkg/input"
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

// CancellationResult records the observed stop after injection.
type CancellationResult struct {
	Stage        CancellationStage `json:"stage"`
	Percent      int               `json:"percent"`
	Injected     bool              `json:"injected"`
	StopDuration time.Duration     `json:"stop_duration_ns"`
	Resumable    bool              `json:"resumable"`
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

	var runErr error
	switch spec.Stage {
	case CancellationInputParsing:
		runErr = runInputParsingCancellation(runCtx, spec, injector)
	case CancellationRichExpansion:
		runErr = runRichExpansionCancellation(runCtx, spec, trigger)
	case CancellationBucketBuild:
		runErr = runBucketCancellation(runCtx, spec, injector)
	case CancellationResumeRebuild, CancellationResultOutput:
		runErr = runScanCancellation(runCtx, spec, injector)
	default:
		return CancellationResult{}, fmt.Errorf("unsupported cancellation stage %q", spec.Stage)
	}
	result := CancellationResult{Stage: spec.Stage, Percent: spec.Percent, Injected: trigger.fired()}
	if result.Injected {
		result.StopDuration = time.Since(trigger.at)
	}
	if spec.Stage == CancellationResumeRebuild || spec.Stage == CancellationResultOutput {
		snapshot, loadErr := state.LoadSnapshot(filepath.Join(spec.OutputDir, "buckets.json"))
		if loadErr != nil {
			return result, fmt.Errorf("load canceled production snapshot: %w", loadErr)
		}
		for _, chunk := range snapshot.Chunks {
			if chunk.NextIndex < chunk.TotalCount {
				result.Resumable = true
				break
			}
		}
	}
	if !errors.Is(runErr, context.Canceled) {
		return result, fmt.Errorf("stage %s did not stop with cancellation: %v", spec.Stage, runErr)
	}
	return result, runErr
}

func runInputParsingCancellation(ctx context.Context, spec CancellationSpec, injector *CancellationInjector) error {
	manifest, err := New().Generate(ctx, FixtureSpec{
		Family: FamilyRecordHeavy,
		Scale:  Scale{InputRecords: spec.Items},
		Seed:   DefaultGeneratorSeed,
	}, filepath.Join(spec.OutputDir, "input"))
	if err != nil {
		return err
	}
	file, err := os.Open(manifest.ArtifactPath)
	if err != nil {
		return fmt.Errorf("open cancellation input: %w", err)
	}
	defer func() { _ = file.Close() }()
	reader := &progressReader{reader: file, totalBytes: manifest.ActualBytes, totalItems: spec.Items, injector: injector}
	_, err = input.LoadCIDRsWithColumnsContext(ctx, reader, "ip", "ip_cidr")
	return err
}

func runRichExpansionCancellation(ctx context.Context, spec CancellationSpec, trigger *cancellationTrigger) error {
	selectors := make([]string, spec.Items)
	for index := range selectors {
		selectors[index] = fixtureIPv4(uint64(index))
	}
	threshold := cancellationThreshold(spec.Items, spec.Percent)
	_, err := task.ExpandIPSelectorsContext(&checkCountContext{
		Context:   ctx,
		threshold: threshold,
		trigger:   trigger,
	}, selectors)
	return err
}

func runBucketCancellation(ctx context.Context, spec CancellationSpec, injector *CancellationInjector) error {
	manifest, portPath, snapshotPath, err := cancellationInputs(ctx, spec)
	if err != nil {
		return err
	}
	cfg, err := config.NewGenerateBuckets(config.GenerateBucketsValues{
		CIDRFile:         manifest.ArtifactPath,
		CIDRIPCol:        "ip",
		CIDRIPCidrCol:    "ip_cidr",
		PortFile:         portPath,
		SnapshotOutput:   snapshotPath,
		Workers:          1,
		ProgressInterval: int(spec.Items) + 1,
	})
	if err != nil {
		return err
	}
	return scanapp.GenerateBuckets(ctx, cfg, io.Discard, scanapp.GenerateBucketsOptions{Reporter: &injectingReporter{injector: injector}})
}

func runScanCancellation(ctx context.Context, spec CancellationSpec, injector *CancellationInjector) error {
	manifest, portPath, snapshotPath, err := cancellationInputs(ctx, spec)
	if err != nil {
		return err
	}
	bucketConfig, err := config.NewGenerateBuckets(config.GenerateBucketsValues{
		CIDRFile: manifest.ArtifactPath, CIDRIPCol: "ip", CIDRIPCidrCol: "ip_cidr",
		PortFile: portPath, SnapshotOutput: snapshotPath, Workers: spec.Workers,
		ProgressInterval: int(spec.Items) + 1,
	})
	if err != nil {
		return err
	}
	if err := scanapp.GenerateBuckets(ctx, bucketConfig, io.Discard, scanapp.GenerateBucketsOptions{}); err != nil {
		return err
	}
	scanConfig, err := config.NewScan(config.ScanValues{
		CIDRFile: manifest.ArtifactPath, CIDRIPCol: "ip", CIDRIPCidrCol: "ip_cidr",
		PortFile: portPath, ResumeInput: snapshotPath, Output: filepath.Join(spec.OutputDir, "results.csv"),
		Workers: 1, DialTimeout: time.Second, DispatchDelay: 100 * time.Microsecond, BucketRate: 1,
		BucketCapacity: 1, LogLevel: "error", Format: "json", Quiet: true,
		Pressure: config.PressureDisabled(),
	})
	if err != nil {
		return err
	}
	opts := scanapp.RunOptions{
		DisableKeyboard: true,
		Dial: func(context.Context, string, string) (net.Conn, error) {
			return fakeOpenConn{}, nil
		},
	}
	if spec.Stage == CancellationResumeRebuild {
		opts.ResumeObserver = func(completed, _ int) { injector.Tick(uint64(completed)) }
	} else {
		opts.ResultObserver = injector.Tick
	}
	return scanapp.Run(ctx, scanConfig, io.Discard, io.Discard, opts)
}

func cancellationInputs(ctx context.Context, spec CancellationSpec) (Manifest, string, string, error) {
	manifest, err := New().Generate(ctx, FixtureSpec{
		Family: FamilyCandidateHeavy,
		Shape:  "unique-groups",
		Scale:  Scale{InputRecords: spec.Items, CandidateAddresses: spec.Items},
		Seed:   DefaultGeneratorSeed,
	}, filepath.Join(spec.OutputDir, "fixture"))
	if err != nil {
		return Manifest{}, "", "", err
	}
	portPath := filepath.Join(spec.OutputDir, "ports.csv")
	if err := os.WriteFile(portPath, []byte("443/tcp\n"), 0o644); err != nil {
		return Manifest{}, "", "", fmt.Errorf("write cancellation ports: %w", err)
	}
	return manifest, portPath, filepath.Join(spec.OutputDir, "buckets.json"), nil
}

type cancellationTrigger struct {
	cancel context.CancelFunc
	once   sync.Once
	at     time.Time
}

func (trigger *cancellationTrigger) fire() {
	trigger.once.Do(func() {
		trigger.at = time.Now()
		trigger.cancel()
	})
}

func (trigger *cancellationTrigger) fired() bool { return !trigger.at.IsZero() }

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

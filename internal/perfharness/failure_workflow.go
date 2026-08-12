package perfharness

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
	"github.com/xuxiping/port-scan-mk3/pkg/pressure"
	"github.com/xuxiping/port-scan-mk3/pkg/scanapp"
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
	Scenario         string      `json:"scenario"`
	Observed         bool        `json:"observed"`
	ErrorText        string      `json:"error_text"`
	ErrorClass       string      `json:"error_class"`
	Operation        string      `json:"operation"`
	TotalItems       uint64      `json:"total_items"`
	Preparation      Observation `json:"preparation"`
	StageObservation Observation `json:"stage_observation"`
}

type failureInputs struct {
	manifest     Manifest
	portPath     string
	snapshotPath string
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
	stage, err := New().Measure(ctx, inputs.manifest.ActualBytes, spec.Items, func(stageCtx context.Context) (uint64, error) {
		observed, stageErr := runFailureStage(stageCtx, spec, inputs)
		result = observed
		return 0, stageErr
	})
	if err != nil {
		return FailureResult{}, err
	}
	result.TotalItems = spec.Items
	result.Preparation = preparation
	result.StageObservation = stage
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
	if spec.Scenario == "snapshot-save-failure" {
		return inputs, nil
	}
	bucketConfig, err := failureBucketConfig(spec, inputs, inputs.snapshotPath)
	if err != nil {
		return failureInputs{}, err
	}
	if err := scanapp.GenerateBuckets(ctx, bucketConfig, io.Discard, scanapp.GenerateBucketsOptions{}); err != nil {
		return failureInputs{}, err
	}
	return inputs, nil
}

func failureBucketConfig(spec FailureSpec, inputs failureInputs, snapshotPath string) (config.GenerateBucketsConfig, error) {
	return config.NewGenerateBuckets(config.GenerateBucketsValues{
		CIDRFile: inputs.manifest.ArtifactPath, CIDRIPCol: "ip", CIDRIPCidrCol: "ip_cidr",
		PortFile: inputs.portPath, SnapshotOutput: snapshotPath, Workers: spec.Workers,
		ProgressInterval: int(spec.Items) + 1,
	})
}

func runFailureStage(ctx context.Context, spec FailureSpec, inputs failureInputs) (FailureResult, error) {
	switch spec.Scenario {
	case "snapshot-save-failure":
		snapshotPath := filepath.Join(spec.OutputDir, "missing", "buckets.json")
		bucketConfig, err := failureBucketConfig(spec, inputs, snapshotPath)
		if err != nil {
			return FailureResult{}, err
		}
		runErr := scanapp.GenerateBuckets(ctx, bucketConfig, io.Discard, scanapp.GenerateBucketsOptions{})
		return expectedFailure(spec.Scenario, "snapshot-create-temp", "snapshot-save", runErr, "write snapshot")
	case "output-failure":
		blocker := filepath.Join(spec.OutputDir, "output-parent-is-a-file")
		if err := os.WriteFile(blocker, []byte("block output directory creation"), 0o644); err != nil {
			return FailureResult{}, fmt.Errorf("write output blocker: %w", err)
		}
		scanConfig, err := config.NewScan(config.ScanValues{
			CIDRFile: inputs.manifest.ArtifactPath, CIDRIPCol: "ip", CIDRIPCidrCol: "ip_cidr",
			PortFile: inputs.portPath, ResumeInput: inputs.snapshotPath, Output: filepath.Join(blocker, "results.csv"),
			Workers: spec.Workers, DialTimeout: time.Second, DispatchDelay: time.Millisecond,
			BucketRate: 1, BucketCapacity: 1, OutputFlushResults: 1000,
			LogLevel: "error", Format: "json", Quiet: true, Pressure: config.PressureDisabled(),
		})
		if err != nil {
			return FailureResult{}, err
		}
		runErr := scanapp.Run(ctx, scanConfig, io.Discard, io.Discard, scanapp.RunOptions{
			DisableKeyboard: true,
			Dial:            func(context.Context, string, string) (net.Conn, error) { return fakeOpenConn{}, nil },
		})
		return expectedFailure(spec.Scenario, "output-open", "output", runErr, blocker)
	case "pressure-fatal-error":
		pressurePolicy, err := config.SimplePressure("http://performance.invalid", time.Millisecond)
		if err != nil {
			return FailureResult{}, err
		}
		scanConfig, err := config.NewScan(config.ScanValues{
			CIDRFile: inputs.manifest.ArtifactPath, CIDRIPCol: "ip", CIDRIPCidrCol: "ip_cidr",
			PortFile: inputs.portPath, ResumeInput: inputs.snapshotPath, Output: filepath.Join(spec.OutputDir, "results.csv"),
			Workers: spec.Workers, DialTimeout: time.Second, DispatchDelay: time.Millisecond,
			BucketRate: 1, BucketCapacity: 1, OutputFlushResults: 1000, LogLevel: "error", Format: "json", Quiet: true,
			Pressure: pressurePolicy,
		})
		if err != nil {
			return FailureResult{}, err
		}
		runErr := scanapp.Run(ctx, scanConfig, io.Discard, io.Discard, scanapp.RunOptions{
			DisableKeyboard: true,
			PressureSource:  failingPressureSource{},
			Dial: func(context.Context, string, string) (net.Conn, error) {
				return fakeOpenConn{}, nil
			},
		})
		return expectedFailure(spec.Scenario, "pressure-poll", "pressure-fatal", runErr, "pressure api failed 3 times")
	default:
		return FailureResult{}, fmt.Errorf("unsupported failure scenario %q", spec.Scenario)
	}
}

func expectedFailure(scenario, operation, errorClass string, err error, expected string) (FailureResult, error) {
	if err == nil || !strings.Contains(err.Error(), expected) {
		return FailureResult{}, fmt.Errorf("scenario %s error = %v, want text %q", scenario, err, expected)
	}
	return FailureResult{Scenario: scenario, Observed: true, ErrorText: err.Error(), Operation: operation, ErrorClass: errorClass}, nil
}

type failingPressureSource struct{}

func (failingPressureSource) Sample(context.Context) (pressure.Sample, error) {
	return pressure.Sample{}, errors.New("injected pressure failure")
}

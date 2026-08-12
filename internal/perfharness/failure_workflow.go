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
	Scenario  string `json:"scenario"`
	Observed  bool   `json:"observed"`
	ErrorText string `json:"error_text"`
}

// RunFailureSmoke executes one expected production failure.
func (Suite) RunFailureSmoke(ctx context.Context, spec FailureSpec) (FailureResult, error) {
	if spec.Items == 0 {
		return FailureResult{}, fmt.Errorf("failure smoke items must be positive")
	}
	if err := os.MkdirAll(spec.OutputDir, 0o755); err != nil {
		return FailureResult{}, fmt.Errorf("create failure smoke directory: %w", err)
	}
	manifest, err := New().Generate(ctx, FixtureSpec{
		Family: FamilyCandidateHeavy,
		Scale:  Scale{InputRecords: spec.Items, CandidateAddresses: spec.Items},
		Seed:   DefaultGeneratorSeed,
	}, filepath.Join(spec.OutputDir, "fixture"))
	if err != nil {
		return FailureResult{}, err
	}
	portPath := filepath.Join(spec.OutputDir, "ports.csv")
	if err := os.WriteFile(portPath, []byte("443/tcp\n"), 0o644); err != nil {
		return FailureResult{}, fmt.Errorf("write failure smoke ports: %w", err)
	}
	snapshotPath := filepath.Join(spec.OutputDir, "buckets.json")
	if spec.Scenario == "snapshot-save-failure" {
		snapshotPath = filepath.Join(spec.OutputDir, "missing", "buckets.json")
	}
	bucketConfig, err := config.NewGenerateBuckets(config.GenerateBucketsValues{
		CIDRFile: manifest.ArtifactPath, CIDRIPCol: "ip", CIDRIPCidrCol: "ip_cidr",
		PortFile: portPath, SnapshotOutput: snapshotPath, Workers: spec.Workers,
		ProgressInterval: int(spec.Items) + 1,
	})
	if err != nil {
		return FailureResult{}, err
	}
	if spec.Scenario == "snapshot-save-failure" {
		runErr := scanapp.GenerateBuckets(ctx, bucketConfig, io.Discard, scanapp.GenerateBucketsOptions{})
		return expectedFailure(spec.Scenario, runErr, "write snapshot")
	}
	if err := scanapp.GenerateBuckets(ctx, bucketConfig, io.Discard, scanapp.GenerateBucketsOptions{}); err != nil {
		return FailureResult{}, err
	}
	if spec.Scenario == "output-failure" {
		blocker := filepath.Join(spec.OutputDir, "output-parent-is-a-file")
		if err := os.WriteFile(blocker, []byte("block output directory creation"), 0o644); err != nil {
			return FailureResult{}, fmt.Errorf("write output blocker: %w", err)
		}
		scanConfig, err := config.NewScan(config.ScanValues{
			CIDRFile: manifest.ArtifactPath, CIDRIPCol: "ip", CIDRIPCidrCol: "ip_cidr",
			PortFile: portPath, ResumeInput: snapshotPath, Output: filepath.Join(blocker, "results.csv"),
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
		return expectedFailure(spec.Scenario, runErr, blocker)
	}
	if spec.Scenario != "pressure-fatal-error" {
		return FailureResult{}, fmt.Errorf("unsupported failure scenario %q", spec.Scenario)
	}
	pressurePolicy, err := config.SimplePressure("http://performance.invalid", time.Millisecond)
	if err != nil {
		return FailureResult{}, err
	}
	scanConfig, err := config.NewScan(config.ScanValues{
		CIDRFile: manifest.ArtifactPath, CIDRIPCol: "ip", CIDRIPCidrCol: "ip_cidr",
		PortFile: portPath, ResumeInput: snapshotPath, Output: filepath.Join(spec.OutputDir, "results.csv"),
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
	return expectedFailure(spec.Scenario, runErr, "pressure api failed 3 times")
}

func expectedFailure(scenario string, err error, expected string) (FailureResult, error) {
	if err == nil || !strings.Contains(err.Error(), expected) {
		return FailureResult{}, fmt.Errorf("scenario %s error = %v, want text %q", scenario, err, expected)
	}
	return FailureResult{Scenario: scenario, Observed: true, ErrorText: err.Error()}, nil
}

type failingPressureSource struct{}

func (failingPressureSource) Sample(context.Context) (pressure.Sample, error) {
	return pressure.Sample{}, errors.New("injected pressure failure")
}

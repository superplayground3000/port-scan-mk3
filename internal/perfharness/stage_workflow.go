package perfharness

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
	"github.com/xuxiping/port-scan-mk3/pkg/input"
	"github.com/xuxiping/port-scan-mk3/pkg/scanapp"
	"github.com/xuxiping/port-scan-mk3/pkg/state"
)

// ProductionStageSpec selects one production stage and its exact logical size.
type ProductionStageSpec struct {
	OutputDir string `json:"output_dir"`
	Items     uint64 `json:"items"`
	Workers   int    `json:"workers"`
}

// RunPrePingCase measures the production pre-ping path six times.
func (suite Suite) RunPrePingCase(ctx context.Context, spec ProductionStageSpec) (CaseResult, error) {
	return runProductionStageCase(ctx, spec, "candidate-heavy/pre-ping", suite.runPrePingObservation)
}

// RunBucketCase measures production bucket generation six times.
func (suite Suite) RunBucketCase(ctx context.Context, spec ProductionStageSpec) (CaseResult, error) {
	return runProductionStageCase(ctx, spec, "task-heavy/bucket-generation", suite.runBucketObservation)
}

type productionStageObservation struct {
	Stage      Observation
	Fixture    Observation
	Manifest   Manifest
	Counter    uint64
	OutputRows uint64
}

type productionStageObserver func(context.Context, ProductionStageSpec, int) (productionStageObservation, error)

func runProductionStageCase(ctx context.Context, spec ProductionStageSpec, name string, observe productionStageObserver) (CaseResult, error) {
	if err := validateProductionStageSpec(spec); err != nil {
		return CaseResult{}, err
	}
	if err := os.MkdirAll(spec.OutputDir, 0o755); err != nil {
		return CaseResult{}, fmt.Errorf("create production stage case directory: %w", err)
	}
	observations := make([]Observation, 0, 6)
	fixtureObservations := make([]Observation, 0, 6)
	var retained Manifest
	for run := 0; run < 6; run++ {
		result, err := observe(ctx, spec, run)
		if err != nil {
			return CaseResult{}, fmt.Errorf("run %s observation %d: %w", name, run+1, err)
		}
		observations = append(observations, result.Stage)
		fixtureObservations = append(fixtureObservations, result.Fixture)
		if run == 0 {
			retained = result.Manifest
		} else if err := removeCompletedObservation(filepath.Join(spec.OutputDir, fmt.Sprintf("run-%d", run))); err != nil {
			return CaseResult{}, err
		}
	}
	return summarizeProductionStage(name, spec.Items, observations, fixtureObservations, retained)
}

func (suite Suite) runPrePingObservation(ctx context.Context, spec ProductionStageSpec, run int) (productionStageObservation, error) {
	runDir, manifest, fixtureObservation, err := suite.generateStageFixture(ctx, spec, run)
	if err != nil {
		return productionStageObservation{}, fmt.Errorf("generate pre-ping fixture: %w", err)
	}
	cfg, err := config.NewPrePingWithResourceLimits(config.PrePingValues{
		CIDRFile: manifest.ArtifactPath, CIDRIPCol: "ip", CIDRIPCidrCol: "ip_cidr",
		Output: filepath.Join(runDir, "results.csv"), Workers: spec.Workers,
		PingTimeout: time.Second, ProgressInterval: int(spec.Items) + 1,
	}, config.PrePingResourceLimits{CIDR: input.CIDRLimits{}})
	if err != nil {
		return productionStageObservation{}, fmt.Errorf("create pre-ping configuration: %w", err)
	}
	var checks atomic.Uint64
	var outputPath string
	observation, err := suite.Measure(ctx, manifest.ActualBytes, spec.Items, func(runCtx context.Context) (uint64, error) {
		var stdout bytes.Buffer
		if runErr := scanapp.RunPrePing(runCtx, cfg, &stdout, io.Discard, scanapp.RunOptions{
			ReachabilityChecker: unreachableCountingReachability{count: &checks},
		}); runErr != nil {
			return 0, runErr
		}
		outputPath = strings.TrimSpace(stdout.String())
		return fileSize(outputPath)
	})
	if err != nil {
		return productionStageObservation{}, err
	}
	rows, err := countCSVRows(outputPath)
	if err != nil {
		return productionStageObservation{}, err
	}
	if checks.Load() != spec.Items || rows != spec.Items {
		return productionStageObservation{}, fmt.Errorf("pre-ping checks=%d rows=%d, want %d", checks.Load(), rows, spec.Items)
	}
	return productionStageObservation{Stage: observation, Fixture: fixtureObservation, Manifest: manifest, Counter: checks.Load(), OutputRows: rows}, nil
}

func (suite Suite) runBucketObservation(ctx context.Context, spec ProductionStageSpec, run int) (productionStageObservation, error) {
	runDir, manifest, fixtureObservation, err := suite.generateStageFixture(ctx, spec, run)
	if err != nil {
		return productionStageObservation{}, fmt.Errorf("generate bucket fixture: %w", err)
	}
	portPath := filepath.Join(runDir, "ports.csv")
	if err := os.WriteFile(portPath, []byte("443/tcp\n"), 0o644); err != nil {
		return productionStageObservation{}, fmt.Errorf("write bucket port fixture: %w", err)
	}
	snapshotPath := filepath.Join(runDir, "buckets.json")
	cfg, err := config.NewGenerateBucketsWithResourceLimits(config.GenerateBucketsValues{
		CIDRFile: manifest.ArtifactPath, CIDRIPCol: "ip", CIDRIPCidrCol: "ip_cidr",
		PortFile: portPath, SnapshotOutput: snapshotPath, Workers: spec.Workers,
		ProgressInterval: int(spec.Items) + 1,
	}, config.GenerateBucketsResourceLimits{CIDR: input.CIDRLimits{}, Port: input.PortLimits{}, Snapshot: state.SnapshotLimits{}})
	if err != nil {
		return productionStageObservation{}, fmt.Errorf("create bucket configuration: %w", err)
	}
	observation, err := suite.Measure(ctx, manifest.ActualBytes, spec.Items, func(runCtx context.Context) (uint64, error) {
		if runErr := scanapp.GenerateBuckets(runCtx, cfg, io.Discard, scanapp.GenerateBucketsOptions{}); runErr != nil {
			return 0, runErr
		}
		return fileSize(snapshotPath)
	})
	if err != nil {
		return productionStageObservation{}, err
	}
	snapshot, err := state.LoadSnapshotWithLimits(snapshotPath, state.SnapshotLimits{})
	if err != nil {
		return productionStageObservation{}, fmt.Errorf("load bucket snapshot: %w", err)
	}
	var tasks uint64
	for _, chunk := range snapshot.Chunks {
		tasks += uint64(chunk.TotalCount)
	}
	if tasks != spec.Items {
		return productionStageObservation{}, fmt.Errorf("bucket tasks=%d, want %d", tasks, spec.Items)
	}
	outputManifest, err := snapshotOutputManifest(snapshotPath, Manifest{SchemaVersion: SchemaVersion, Family: FamilyTaskHeavy, Seed: DefaultGeneratorSeed, ProbeTasks: spec.Items})
	if err != nil {
		return productionStageObservation{}, fmt.Errorf("record task-heavy output manifest: %w", err)
	}
	return productionStageObservation{Stage: observation, Fixture: fixtureObservation, Manifest: outputManifest, Counter: tasks, OutputRows: uint64(len(snapshot.Chunks))}, nil
}

func (suite Suite) generateStageFixture(ctx context.Context, spec ProductionStageSpec, run int) (string, Manifest, Observation, error) {
	runDir := filepath.Join(spec.OutputDir, fmt.Sprintf("run-%d", run))
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return "", Manifest{}, Observation{}, fmt.Errorf("create stage observation directory: %w", err)
	}
	var manifest Manifest
	fixtureObservation, err := suite.Measure(ctx, 0, spec.Items, func(runCtx context.Context) (uint64, error) {
		generated, generateErr := suite.Generate(runCtx, FixtureSpec{Family: FamilyCandidateHeavy, Scale: Scale{InputRecords: spec.Items, CandidateAddresses: spec.Items}, Seed: DefaultGeneratorSeed}, filepath.Join(runDir, "fixture"))
		manifest = generated
		return generated.ActualBytes, generateErr
	})
	return runDir, manifest, fixtureObservation, err
}

func validateProductionStageSpec(spec ProductionStageSpec) error {
	if spec.OutputDir == "" || spec.Items == 0 || spec.Workers < 1 {
		return fmt.Errorf("production stage requires output directory, items, and workers")
	}
	return nil
}

func summarizeProductionStage(name string, items uint64, observations, fixtureObservations []Observation, manifest Manifest) (CaseResult, error) {
	result, err := SummarizeCase(name, observations)
	if err != nil {
		return CaseResult{}, err
	}
	fixture, err := SummarizePhase(name+" fixture generation", fixtureObservations)
	if err != nil {
		return CaseResult{}, err
	}
	result.Manifest = &manifest
	result.LogicalItems = items
	result.FixtureGeneration = &fixture
	result.Correctness = Correctness{Headers: true, RowCounts: true, SnapshotProgress: true, ExpectedValues: true, Digests: true}
	result.Verdict = Verdict{Passed: true}
	return result, nil
}

func removeCompletedObservation(path string) error {
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("remove completed observation %s: %w", path, err)
	}
	return nil
}

type unreachableCountingReachability struct {
	count *atomic.Uint64
}

func (checker unreachableCountingReachability) Check(_ context.Context, ip string, _ time.Duration) scanapp.ReachabilityResult {
	checker.count.Add(1)
	return scanapp.ReachabilityResult{IP: ip, FailureText: "synthetic unreachable"}
}

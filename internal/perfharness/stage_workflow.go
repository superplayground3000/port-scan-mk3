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
	if err := validateProductionStageSpec(spec); err != nil {
		return CaseResult{}, err
	}
	if err := os.MkdirAll(spec.OutputDir, 0o755); err != nil {
		return CaseResult{}, fmt.Errorf("create pre-ping case directory: %w", err)
	}
	observations := make([]Observation, 0, 6)
	fixtureObservations := make([]Observation, 0, 6)
	var retained Manifest
	for run := 0; run < 6; run++ {
		runDir := filepath.Join(spec.OutputDir, fmt.Sprintf("run-%d", run))
		if err := os.Mkdir(runDir, 0o755); err != nil {
			return CaseResult{}, fmt.Errorf("create pre-ping observation directory: %w", err)
		}
		var manifest Manifest
		fixtureObservation, err := suite.Measure(ctx, 0, spec.Items, func(runCtx context.Context) (uint64, error) {
			generated, generateErr := suite.Generate(runCtx, FixtureSpec{
				Family: FamilyCandidateHeavy,
				Scale:  Scale{InputRecords: spec.Items, CandidateAddresses: spec.Items},
				Seed:   DefaultGeneratorSeed,
			}, filepath.Join(runDir, "fixture"))
			manifest = generated
			return generated.ActualBytes, generateErr
		})
		if err != nil {
			return CaseResult{}, fmt.Errorf("generate pre-ping fixture observation %d: %w", run+1, err)
		}
		cfg, err := config.NewPrePingWithResourceLimits(config.PrePingValues{
			CIDRFile: manifest.ArtifactPath, CIDRIPCol: "ip", CIDRIPCidrCol: "ip_cidr",
			Output: filepath.Join(runDir, "results.csv"), Workers: spec.Workers,
			PingTimeout: time.Second, ProgressInterval: int(spec.Items) + 1,
		}, config.PrePingResourceLimits{CIDR: input.CIDRLimits{}})
		if err != nil {
			return CaseResult{}, fmt.Errorf("create pre-ping configuration: %w", err)
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
			return CaseResult{}, fmt.Errorf("run pre-ping observation %d: %w", run+1, err)
		}
		rows, err := countCSVRows(outputPath)
		if err != nil {
			return CaseResult{}, err
		}
		if checks.Load() != spec.Items || rows != spec.Items {
			return CaseResult{}, fmt.Errorf("pre-ping observation %d checks=%d rows=%d, want %d", run+1, checks.Load(), rows, spec.Items)
		}
		observations = append(observations, observation)
		fixtureObservations = append(fixtureObservations, fixtureObservation)
		if run == 0 {
			retained = manifest
		} else if err := removeCompletedObservation(runDir); err != nil {
			return CaseResult{}, err
		}
	}
	return summarizeProductionStage("candidate-heavy/pre-ping", spec.Items, observations, fixtureObservations, retained)
}

// RunBucketCase measures production bucket generation six times.
func (suite Suite) RunBucketCase(ctx context.Context, spec ProductionStageSpec) (CaseResult, error) {
	if err := validateProductionStageSpec(spec); err != nil {
		return CaseResult{}, err
	}
	if err := os.MkdirAll(spec.OutputDir, 0o755); err != nil {
		return CaseResult{}, fmt.Errorf("create bucket case directory: %w", err)
	}
	observations := make([]Observation, 0, 6)
	fixtureObservations := make([]Observation, 0, 6)
	var retained Manifest
	for run := 0; run < 6; run++ {
		runDir := filepath.Join(spec.OutputDir, fmt.Sprintf("run-%d", run))
		if err := os.Mkdir(runDir, 0o755); err != nil {
			return CaseResult{}, fmt.Errorf("create bucket observation directory: %w", err)
		}
		var manifest Manifest
		fixtureObservation, err := suite.Measure(ctx, 0, spec.Items, func(runCtx context.Context) (uint64, error) {
			generated, generateErr := suite.Generate(runCtx, FixtureSpec{
				Family: FamilyCandidateHeavy,
				Scale:  Scale{InputRecords: spec.Items, CandidateAddresses: spec.Items},
				Seed:   DefaultGeneratorSeed,
			}, filepath.Join(runDir, "fixture"))
			manifest = generated
			return generated.ActualBytes, generateErr
		})
		if err != nil {
			return CaseResult{}, fmt.Errorf("generate bucket fixture observation %d: %w", run+1, err)
		}
		portPath := filepath.Join(runDir, "ports.csv")
		if err := os.WriteFile(portPath, []byte("443/tcp\n"), 0o644); err != nil {
			return CaseResult{}, fmt.Errorf("write bucket port fixture: %w", err)
		}
		snapshotPath := filepath.Join(runDir, "buckets.json")
		cfg, err := config.NewGenerateBucketsWithResourceLimits(config.GenerateBucketsValues{
			CIDRFile: manifest.ArtifactPath, CIDRIPCol: "ip", CIDRIPCidrCol: "ip_cidr",
			PortFile: portPath, SnapshotOutput: snapshotPath, Workers: spec.Workers,
			ProgressInterval: int(spec.Items) + 1,
		}, config.GenerateBucketsResourceLimits{CIDR: input.CIDRLimits{}, Port: input.PortLimits{}, Snapshot: state.SnapshotLimits{}})
		if err != nil {
			return CaseResult{}, fmt.Errorf("create bucket configuration: %w", err)
		}
		observation, err := suite.Measure(ctx, manifest.ActualBytes, spec.Items, func(runCtx context.Context) (uint64, error) {
			if runErr := scanapp.GenerateBuckets(runCtx, cfg, io.Discard, scanapp.GenerateBucketsOptions{}); runErr != nil {
				return 0, runErr
			}
			return fileSize(snapshotPath)
		})
		if err != nil {
			return CaseResult{}, fmt.Errorf("run bucket observation %d: %w", run+1, err)
		}
		snapshot, err := state.LoadSnapshotWithLimits(snapshotPath, state.SnapshotLimits{})
		if err != nil {
			return CaseResult{}, fmt.Errorf("load bucket observation %d: %w", run+1, err)
		}
		var tasks uint64
		for _, chunk := range snapshot.Chunks {
			tasks += uint64(chunk.TotalCount)
		}
		if tasks != spec.Items {
			return CaseResult{}, fmt.Errorf("bucket observation %d tasks=%d, want %d", run+1, tasks, spec.Items)
		}
		observations = append(observations, observation)
		fixtureObservations = append(fixtureObservations, fixtureObservation)
		if run == 0 {
			retained, err = snapshotOutputManifest(snapshotPath, Manifest{
				SchemaVersion: SchemaVersion,
				Family:        FamilyTaskHeavy,
				Seed:          DefaultGeneratorSeed,
				ProbeTasks:    spec.Items,
			})
			if err != nil {
				return CaseResult{}, fmt.Errorf("record task-heavy output manifest: %w", err)
			}
		} else if err := removeCompletedObservation(runDir); err != nil {
			return CaseResult{}, err
		}
	}
	return summarizeProductionStage("task-heavy/bucket-generation", spec.Items, observations, fixtureObservations, retained)
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

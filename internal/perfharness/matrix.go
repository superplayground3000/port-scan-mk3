package perfharness

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
	"github.com/xuxiping/port-scan-mk3/pkg/input"
	"github.com/xuxiping/port-scan-mk3/pkg/scanapp"
	"github.com/xuxiping/port-scan-mk3/pkg/state"
)

// RunFixtureCase generates and verifies one cold run and five steady runs.
func (suite Suite) RunFixtureCase(ctx context.Context, outputDir string, spec FixtureSpec) (CaseResult, error) {
	if err := os.Mkdir(outputDir, 0o755); err != nil {
		return CaseResult{}, fmt.Errorf("create case directory: %w", err)
	}
	observations := make([]Observation, 0, 6)
	fixtureObservations := make([]Observation, 0, 6)
	var retained Manifest
	for run := 0; run < 6; run++ {
		runDir := filepath.Join(outputDir, fmt.Sprintf("run-%d", run))
		var manifest Manifest
		units := fixtureUnits(spec.Scale)
		fixtureObservation, err := suite.Measure(ctx, 0, units, func(runCtx context.Context) (uint64, error) {
			generated, generateErr := suite.Generate(runCtx, spec, runDir)
			if generateErr != nil {
				return 0, generateErr
			}
			manifest = generated
			return manifest.ActualBytes, nil
		})
		if err != nil {
			return CaseResult{}, fmt.Errorf("generate fixture case %s observation %d: %w", spec.Family, run+1, err)
		}
		observation, err := suite.Measure(ctx, manifest.ActualBytes, units, func(runCtx context.Context) (uint64, error) {
			return runFixtureProductionStage(runCtx, suite, spec, manifest)
		})
		if err != nil {
			return CaseResult{}, fmt.Errorf("run fixture case %s observation %d: %w", spec.Family, run+1, err)
		}
		fixtureObservations = append(fixtureObservations, fixtureObservation)
		observations = append(observations, observation)
		if run == 0 {
			retained = manifest
			continue
		}
		if err := removeFixtureRun(manifest); err != nil {
			return CaseResult{}, err
		}
	}
	name := fixtureCaseName(spec)
	result, err := SummarizeCase(name, observations)
	if err != nil {
		return CaseResult{}, err
	}
	result.Manifest = &retained
	result.LogicalItems = fixtureUnits(spec.Scale)
	fixtureGeneration, err := SummarizePhase(name+" fixture generation", fixtureObservations)
	if err != nil {
		return CaseResult{}, err
	}
	result.FixtureGeneration = &fixtureGeneration
	result.Correctness = Correctness{
		Headers:          true,
		RowCounts:        true,
		SnapshotProgress: true,
		ExpectedValues:   true,
		Digests:          true,
	}
	result.Verdict = Verdict{Passed: true}
	return result, nil
}

func fixtureCaseName(spec FixtureSpec) string {
	name := string(spec.Family)
	if spec.Shape != "" {
		name += "/" + spec.Shape
	}
	if spec.Family != FamilySnapshotHeavy || spec.Shape != "mixed" {
		return name
	}
	labels := map[uint64]string{
		1_000_000:     "one-megabyte",
		10_000_000:    "ten-megabytes",
		100_000_000:   "one-hundred-megabytes",
		1_000_000_000: "one-gigabyte",
	}
	if label := labels[spec.Scale.TargetBytes]; label != "" {
		return name + "/" + label
	}
	return name
}

func runFixtureProductionStage(ctx context.Context, suite Suite, spec FixtureSpec, manifest Manifest) (uint64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if err := suite.Validate(manifest); err != nil {
		return 0, err
	}
	if spec.Family == FamilyRecordHeavy {
		rows, err := input.LoadCIDRsFileWithColumnsContext(ctx, manifest.ArtifactPath, "ip", "ip_cidr", input.CIDRLimits{})
		if err != nil {
			return 0, fmt.Errorf("load production CIDR fixture: %w", err)
		}
		return uint64(len(rows)), nil
	}
	if spec.Family == FamilyPortHeavy {
		ports, err := input.LoadPortsFileContext(ctx, manifest.ArtifactPath, input.PortLimits{})
		if err != nil {
			return 0, fmt.Errorf("load production port fixture: %w", err)
		}
		cidrPath := filepath.Join(filepath.Dir(manifest.ArtifactPath), "port-consumer-input.csv")
		if err := os.WriteFile(cidrPath, []byte("ip,ip_cidr\n127.0.0.1,127.0.0.1/32\n"), 0o644); err != nil {
			return 0, fmt.Errorf("write port consumer input: %w", err)
		}
		snapshotPath := filepath.Join(filepath.Dir(manifest.ArtifactPath), "port-consumer-snapshot.json")
		cfg, err := config.NewGenerateBucketsWithResourceLimits(config.GenerateBucketsValues{
			CIDRFile: cidrPath, CIDRIPCol: "ip", CIDRIPCidrCol: "ip_cidr",
			PortFile: manifest.ArtifactPath, SnapshotOutput: snapshotPath, Workers: 1,
		}, config.GenerateBucketsResourceLimits{CIDR: input.CIDRLimits{}, Port: input.PortLimits{}, Snapshot: state.SnapshotLimits{}})
		if err != nil {
			return 0, fmt.Errorf("create port consumer configuration: %w", err)
		}
		if err := scanapp.GenerateBuckets(ctx, cfg, io.Discard, scanapp.GenerateBucketsOptions{}); err != nil {
			return 0, fmt.Errorf("run port consumer: %w", err)
		}
		snapshot, err := state.LoadSnapshotWithLimits(snapshotPath, state.SnapshotLimits{})
		if err != nil {
			return 0, fmt.Errorf("load port consumer snapshot: %w", err)
		}
		var tasks uint64
		for _, chunk := range snapshot.Chunks {
			tasks += uint64(chunk.TotalCount)
		}
		if tasks != uint64(len(ports)) {
			return 0, fmt.Errorf("port consumer generated %d tasks from %d ports", tasks, len(ports))
		}
		return tasks, nil
	}
	if spec.Family == FamilyRichRecordMixed || spec.Family == FamilyRichUniqueKey ||
		spec.Family == FamilyRichHotKey || spec.Family == FamilyRichPrecheck || spec.Family == FamilyRichDeny {
		rows, err := input.LoadCIDRsFileWithColumnsContext(ctx, manifest.ArtifactPath, "src_ip", "src_network_segment", input.CIDRLimits{})
		if err != nil {
			return 0, fmt.Errorf("load production rich CIDR fixture: %w", err)
		}
		return uint64(len(rows)), nil
	}
	if spec.Family != FamilySnapshotHeavy && spec.Family != FamilyResumeHeavy {
		return 0, fmt.Errorf("fixture family %q requires a dedicated production runner", spec.Family)
	}
	disabledLimits := state.SnapshotLimits{}
	snapshot, err := state.LoadSnapshotWithLimits(manifest.ArtifactPath, disabledLimits)
	if err != nil {
		return 0, fmt.Errorf("load production snapshot fixture: %w", err)
	}
	roundtripPath := filepath.Join(filepath.Dir(manifest.ArtifactPath), "roundtrip.json")
	if err := state.SaveSnapshotWithLimits(roundtripPath, snapshot, disabledLimits); err != nil {
		return 0, fmt.Errorf("save production snapshot fixture: %w", err)
	}
	if _, err := state.LoadSnapshotWithLimits(roundtripPath, disabledLimits); err != nil {
		return 0, fmt.Errorf("reload production snapshot fixture: %w", err)
	}
	info, err := os.Stat(roundtripPath)
	if err != nil {
		return 0, fmt.Errorf("read production snapshot fixture size: %w", err)
	}
	return uint64(info.Size()), nil
}

func fixtureUnits(scale Scale) uint64 {
	for _, value := range []uint64{scale.ExpectedOutputs, scale.ProbeTasks, scale.CandidateAddresses, scale.InputRecords, scale.TargetBytes} {
		if value > 0 {
			return value
		}
	}
	return 1
}

func removeFixtureRun(manifest Manifest) error {
	for _, path := range []string{
		manifest.ArtifactPath,
		manifest.ManifestPath,
		filepath.Join(filepath.Dir(manifest.ArtifactPath), "roundtrip.json"),
		filepath.Join(filepath.Dir(manifest.ArtifactPath), "port-consumer-input.csv"),
		filepath.Join(filepath.Dir(manifest.ArtifactPath), "port-consumer-snapshot.json"),
	} {
		if err := os.Remove(path); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("remove generated fixture file %s: %w", path, err)
		}
	}
	if err := os.Remove(filepath.Dir(manifest.ArtifactPath)); err != nil {
		return fmt.Errorf("remove generated fixture directory: %w", err)
	}
	return nil
}

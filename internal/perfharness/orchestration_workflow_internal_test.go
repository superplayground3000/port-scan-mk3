package perfharness

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
	"github.com/xuxiping/port-scan-mk3/pkg/input"
	"github.com/xuxiping/port-scan-mk3/pkg/scanapp"
	"github.com/xuxiping/port-scan-mk3/pkg/state"
	"github.com/xuxiping/port-scan-mk3/pkg/task"
)

func TestRunOrchestrationSmokePreparesExactTasksBeforeMeasurement(t *testing.T) {
	t.Parallel()

	outputDir := filepath.Join(t.TempDir(), "orchestration")
	measureCalls := 0
	result, err := runOrchestrationWorkflow(context.Background(), WorkflowSpec{
		OutputDir: outputDir,
		Items:     10,
		Workers:   4,
	}, func(ctx context.Context, inputBytes, units uint64, action MeasuredAction) (Observation, error) {
		measureCalls++
		if measureCalls == 2 {
			if inputBytes == 0 || units != 10 {
				t.Fatalf("stage measurement input=%d units=%d", inputBytes, units)
			}
			if _, err := os.Stat(filepath.Join(outputDir, "buckets.json")); err != nil {
				t.Fatalf("snapshot was not prepared before stage Measure: %v", err)
			}
		}
		return New().Measure(ctx, inputBytes, units, action)
	})
	if err != nil {
		t.Fatalf("runOrchestrationWorkflow: %v", err)
	}
	if measureCalls != 2 || result.ProbeCount != 10 || result.ScanRows != 10 || result.OpenRows != 10 || result.Semantic.TaskCount != 10 || result.Semantic.SnapshotDigest == "" {
		t.Fatalf("orchestration result = %+v", result)
	}
	if result.ExpansionOverride == nil || result.ExpansionOverride.CandidateLimit != 11 || result.ExpansionOverride.MemoryLimitGB != 2 || result.ExpansionOverride.ScannableTasks != 10 {
		t.Fatalf("orchestration expansion override = %+v", result.ExpansionOverride)
	}
}

func TestCompactTaskBlocksRepresentExactlyTenMillionTasks(t *testing.T) {
	t.Parallel()

	blocks, singles, err := compactTaskBlocks(FullItemCount)
	if err != nil {
		t.Fatal(err)
	}
	total := uint64(len(singles))
	var previousEnd uint32
	for index, block := range blocks {
		_, network, parseErr := net.ParseCIDR(block)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		ones, bits := network.Mask.Size()
		size := uint64(1) << uint(bits-ones)
		if ones < 31 {
			total += size - 1
		} else {
			total += size
		}
		start := binary.BigEndian.Uint32(network.IP.To4())
		if index > 0 && start <= previousEnd {
			t.Fatalf("overlapping compact blocks at %s", block)
		}
		previousEnd = start + uint32(size) - 1
	}
	if total != FullItemCount {
		t.Fatalf("compact task count = %d, want %d", total, FullItemCount)
	}
	if len(blocks)+len(singles) > 64 {
		t.Fatalf("compact input has %d rows", len(blocks)+len(singles))
	}
}

func TestCompactTaskExpansionUsesTheSmallestExplicitProductionLimits(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	inputPath := filepath.Join(dir, "compact.csv")
	if err := writeCompactTaskInput(context.Background(), inputPath, FullItemCount, "LF"); err != nil {
		t.Fatal(err)
	}
	records, err := input.LoadCIDRsFileWithColumnsContext(context.Background(), inputPath, "ip", "ip_cidr", input.CIDRLimits{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := task.EstimateAuthorizedCIDRRecords(records, task.DefaultExpansionLimits(), nil); err == nil {
		t.Fatal("default target limits accepted the compact 10 million task fixture")
	}
	override, err := estimateOrchestrationExpansion(records, FullItemCount)
	if err != nil {
		t.Fatal(err)
	}
	if override.CandidateLimit != 10_000_008 || override.EstimatedBytes != 16_000_012_000 || override.MemoryLimitGB != 17 {
		t.Fatalf("override = %+v, want exact production estimate", override)
	}

	portPath := filepath.Join(dir, "ports.csv")
	if err := os.WriteFile(portPath, []byte("443/tcp\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	values := config.GenerateBucketsValues{
		CIDRFile: inputPath, CIDRIPCol: "ip", CIDRIPCidrCol: "ip_cidr", PortFile: portPath,
		SnapshotOutput: filepath.Join(dir, "buckets.json"), Workers: 1, ProgressInterval: 100,
	}
	base, err := config.NewGenerateBucketsWithResourceLimits(values, config.GenerateBucketsResourceLimits{
		CIDR: input.CIDRLimits{}, Port: input.PortLimits{}, Snapshot: state.SnapshotLimits{},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name       string
		candidate  uint64
		memoryGB   uint64
		wantDetail string
	}{
		{name: "candidate one less", candidate: override.CandidateLimit - 1, memoryGB: override.MemoryLimitGB, wantDetail: "candidate count"},
		{name: "memory one less", candidate: override.CandidateLimit, memoryGB: override.MemoryLimitGB - 1, wantDetail: "memory limit"},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg, configErr := withBucketExpansion(base, test.candidate, test.memoryGB)
			if configErr != nil {
				t.Fatal(configErr)
			}
			err := scanapp.GenerateBuckets(context.Background(), cfg, io.Discard, scanapp.GenerateBucketsOptions{})
			if err == nil || !strings.Contains(err.Error(), test.wantDetail) {
				t.Fatalf("GenerateBuckets() error = %v, want %q", err, test.wantDetail)
			}
		})
	}
}

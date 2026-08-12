package perfharness

import (
	"context"
	"encoding/binary"
	"net"
	"os"
	"path/filepath"
	"testing"
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
	if measureCalls != 2 || result.ProbeCount != 10 || result.ScanRows != 10 || result.OpenRows != 10 || result.Semantic.TaskCount != 10 {
		t.Fatalf("orchestration result = %+v", result)
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

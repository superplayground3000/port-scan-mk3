package perfharness_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/xuxiping/port-scan-mk3/internal/perfharness"
)

func TestRunOutputCaseMeasuresBothWritersAndRemovesTemporaryOutputs(t *testing.T) {
	for _, interval := range []int{0, 1, 1000} {
		outputDir := filepath.Join(t.TempDir(), "output case")
		result, err := perfharness.New().RunOutputCase(context.Background(), perfharness.OutputSpec{
			OutputDir:    outputDir,
			Results:      100,
			FlushResults: interval,
		})
		if err != nil {
			t.Fatalf("interval=%d RunOutputCase() error = %v", interval, err)
		}
		if len(result.Runs) != 5 || !result.Correctness.Headers || !result.Correctness.RowCounts || !result.Correctness.ExpectedValues {
			t.Fatalf("interval=%d result = %+v", interval, result)
		}
		if result.SteadyMedian.OutputBytes == 0 || result.SteadyMedian.ThroughputPerSecond <= 0 || result.SteadyMedian.MegabytesPerSecond <= 0 {
			t.Fatalf("interval=%d metrics = %+v", interval, result.SteadyMedian)
		}
		entries, err := os.ReadDir(outputDir)
		if err != nil {
			t.Fatalf("interval=%d ReadDir() error = %v", interval, err)
		}
		if len(entries) != 0 {
			t.Fatalf("interval=%d retained temporary outputs: %v", interval, entries)
		}
	}
}

func TestRunOutputCaseRejectsInvalidValuesBeforeIO(t *testing.T) {
	outputDir := filepath.Join(t.TempDir(), "must not exist")
	_, err := perfharness.New().RunOutputCase(context.Background(), perfharness.OutputSpec{
		OutputDir:    outputDir,
		Results:      1,
		FlushResults: -1,
	})
	if err == nil {
		t.Fatal("RunOutputCase() accepted a negative flush interval")
	}
	if _, statErr := os.Stat(outputDir); !os.IsNotExist(statErr) {
		t.Fatalf("RunOutputCase() performed I/O before validation: %v", statErr)
	}
}

func TestRunOutputCaseRejectsMissingInputsAndCancellation(t *testing.T) {
	tests := []struct {
		name string
		ctx  context.Context
		spec perfharness.OutputSpec
	}{
		{name: "empty output directory", ctx: context.Background(), spec: perfharness.OutputSpec{Results: 1}},
		{name: "zero results", ctx: context.Background(), spec: perfharness.OutputSpec{OutputDir: t.TempDir()}},
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	tests = append(tests, struct {
		name string
		ctx  context.Context
		spec perfharness.OutputSpec
	}{
		name: "canceled context",
		ctx:  canceled,
		spec: perfharness.OutputSpec{OutputDir: t.TempDir(), Results: 10, FlushResults: 1},
	})

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := perfharness.New().RunOutputCase(tc.ctx, tc.spec); err == nil {
				t.Fatal("RunOutputCase() error = nil")
			}
		})
	}
}

func TestRunOutputCaseRejectsAnExistingOutputPath(t *testing.T) {
	outputPath := filepath.Join(t.TempDir(), "output")
	if err := os.WriteFile(outputPath, []byte("occupied"), 0o644); err != nil {
		t.Fatalf("seed output path: %v", err)
	}
	if _, err := perfharness.New().RunOutputCase(context.Background(), perfharness.OutputSpec{
		OutputDir:    outputPath,
		Results:      10,
		FlushResults: 1,
	}); err == nil {
		t.Fatal("RunOutputCase() accepted an existing output path")
	}
}

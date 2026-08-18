package perfharness

import (
	"fmt"
	"time"
)

const (
	MaxTimeGrowth       = 12.5
	MaxAllocationGrowth = 11.0
	MaxRegression       = 0.10
	MaxWorkerGrowth     = 1.25
)

// Observation records portable and OS-specific process metrics.
type Observation struct {
	InputBytes             uint64        `json:"input_bytes"`
	OutputBytes            uint64        `json:"output_bytes"`
	WallTime               time.Duration `json:"wall_time_ns"`
	ThroughputPerSecond    float64       `json:"throughput_per_second"`
	MegabytesPerSecond     float64       `json:"megabytes_per_second"`
	GoAllocatedBytes       uint64        `json:"go_allocated_bytes"`
	GoAllocationCount      uint64        `json:"go_allocation_count"`
	GoPeakHeapBytes        uint64        `json:"go_peak_heap_bytes"`
	LinuxPeakRSSBytes      uint64        `json:"linux_peak_rss_bytes,omitempty"`
	WindowsWorkingSetBytes uint64        `json:"windows_peak_working_set_bytes,omitempty"`
	PeakCommittedBytes     uint64        `json:"peak_committed_bytes"`
	SwapOrPagefileBytes    uint64        `json:"swap_or_pagefile_bytes"`
	PagingReadBytes        uint64        `json:"paging_read_bytes"`
	PagingWriteBytes       uint64        `json:"paging_write_bytes"`
}

// AbsoluteBudget defines one case runtime and memory limit.
type AbsoluteBudget struct {
	MaxWallTime       time.Duration `json:"max_wall_time_ns"`
	MaxCommittedBytes uint64        `json:"max_committed_bytes"`
}

// GrowthComparison compares a ten-fold scale pair.
type GrowthComparison struct {
	Small Observation `json:"small"`
	Large Observation `json:"large"`
}

// RegressionComparison compares six-run benchmark medians.
type RegressionComparison struct {
	BeforeNSPerOp float64 `json:"before_ns_per_op"`
	AfterNSPerOp  float64 `json:"after_ns_per_op"`
	BeforeBPerOp  float64 `json:"before_bytes_per_op"`
	AfterBPerOp   float64 `json:"after_bytes_per_op"`
	Iterations    uint64  `json:"iterations,omitempty"`
}

// WorkerComparison compares the committed memory of two worker profiles.
type WorkerComparison struct {
	Workers16Bytes  uint64 `json:"workers_16_bytes"`
	Workers256Bytes uint64 `json:"workers_256_bytes"`
}

// EvaluationInput contains all applicable threshold types for one case.
type EvaluationInput struct {
	Observation Observation           `json:"observation"`
	Absolute    AbsoluteBudget        `json:"absolute"`
	Growth      *GrowthComparison     `json:"growth,omitempty"`
	Regression  *RegressionComparison `json:"regression,omitempty"`
	Workers     *WorkerComparison     `json:"workers,omitempty"`
	FatalState  string                `json:"fatal_state,omitempty"`
}

// Failure identifies one failed threshold rule.
type Failure struct {
	Rule   string `json:"rule"`
	Detail string `json:"detail"`
}

// Verdict records all threshold failures without short-circuiting.
type Verdict struct {
	Passed   bool      `json:"passed"`
	Failures []Failure `json:"failures,omitempty"`
}

// HasFailure reports whether a rule failed.
func (v Verdict) HasFailure(rule string) bool {
	for _, failure := range v.Failures {
		if failure.Rule == rule {
			return true
		}
	}
	return false
}

// Evaluate applies the approved absolute, growth, regression, and worker budgets.
func (Suite) Evaluate(input EvaluationInput) Verdict {
	verdict := Verdict{Passed: true}
	fail := func(rule, detail string) {
		verdict.Passed = false
		verdict.Failures = append(verdict.Failures, Failure{Rule: rule, Detail: detail})
	}
	if input.FatalState != "" {
		fail("fatal-state", input.FatalState)
	}
	if input.Absolute.MaxWallTime > 0 && input.Observation.WallTime > input.Absolute.MaxWallTime {
		fail("absolute-wall-time", fmt.Sprintf("wall time %s exceeds %s", input.Observation.WallTime, input.Absolute.MaxWallTime))
	}
	if input.Absolute.MaxCommittedBytes > 0 && input.Observation.PeakCommittedBytes > input.Absolute.MaxCommittedBytes {
		fail("absolute-committed-memory", fmt.Sprintf("committed memory %d exceeds %d", input.Observation.PeakCommittedBytes, input.Absolute.MaxCommittedBytes))
	}
	if input.Growth != nil {
		if ratioDuration(input.Growth.Large.WallTime, input.Growth.Small.WallTime) > MaxTimeGrowth {
			fail("growth-wall-time", "ten-fold wall-time growth exceeds 12.5-fold")
		}
		if ratioUint64(input.Growth.Large.GoAllocatedBytes, input.Growth.Small.GoAllocatedBytes) > MaxAllocationGrowth {
			fail("growth-allocated-bytes", "ten-fold allocated-byte growth exceeds 11-fold")
		}
	}
	if input.Regression != nil {
		// A baseline above zero with an after value at zero is an absent
		// measurement, not a result of zero. Report it as a failure, because a
		// ratio of zero would pass every regression rule in silence.
		switch {
		case input.Regression.BeforeNSPerOp > 0 && input.Regression.AfterNSPerOp <= 0:
			fail("regression-unmeasured", "ns/op after value is not above zero, so the benchmark did not measure one operation")
		case ratioFloat(input.Regression.AfterNSPerOp, input.Regression.BeforeNSPerOp) > 1+MaxRegression:
			fail("regression-ns-per-op", "ns/op regression exceeds 10 percent")
		}
		switch {
		case input.Regression.BeforeBPerOp > 0 && input.Regression.AfterBPerOp <= 0:
			fail("regression-unmeasured", "B/op after value is not above zero, so the benchmark did not measure one operation")
		case ratioFloat(input.Regression.AfterBPerOp, input.Regression.BeforeBPerOp) > 1+MaxRegression:
			fail("regression-bytes-per-op", "B/op regression exceeds 10 percent")
		}
	}
	if input.Workers != nil && ratioUint64(input.Workers.Workers256Bytes, input.Workers.Workers16Bytes) > MaxWorkerGrowth {
		fail("worker-memory", "256-worker memory exceeds the 16-worker result by more than 25 percent")
	}
	return verdict
}

func ratioDuration(numerator, denominator time.Duration) float64 {
	if denominator <= 0 {
		if numerator > 0 {
			return 1e300
		}
		return 1
	}
	return float64(numerator) / float64(denominator)
}

func ratioUint64(numerator, denominator uint64) float64 {
	if denominator == 0 {
		if numerator > 0 {
			return 1e300
		}
		return 1
	}
	return float64(numerator) / float64(denominator)
}

func ratioFloat(numerator, denominator float64) float64 {
	if denominator <= 0 {
		if numerator > 0 {
			return 1e300
		}
		return 1
	}
	return numerator / denominator
}

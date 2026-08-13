package perfharness

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
	"github.com/xuxiping/port-scan-mk3/pkg/input"
	"github.com/xuxiping/port-scan-mk3/pkg/pressure"
	"github.com/xuxiping/port-scan-mk3/pkg/ratelimit"
	"github.com/xuxiping/port-scan-mk3/pkg/scanapp"
	"github.com/xuxiping/port-scan-mk3/pkg/state"
)

// WorkflowSpec defines one bounded production workflow run.
type WorkflowSpec struct {
	OutputDir  string `json:"output_dir"`
	Items      uint64 `json:"items"`
	Workers    int    `json:"workers"`
	LineEnding string `json:"line_ending,omitempty"`
}

// WorkflowResult records correctness data from the production workflow.
type WorkflowResult struct {
	ProbeCount        uint64 `json:"probe_count"`
	ReachabilityCount uint64 `json:"reachability_count"`
	PrePingCompleted  bool   `json:"pre_ping_completed"`
	ScanRows          uint64 `json:"scan_rows"`
	OpenRows          uint64 `json:"open_rows"`
	// SnapshotCompleted means the production run completed all remaining work.
	// It does not mean that the successful run rewrote the input snapshot file.
	SnapshotCompleted bool               `json:"snapshot_completed"`
	ScanDigest        string             `json:"scan_digest"`
	OpenDigest        string             `json:"open_digest"`
	FixtureGeneration Observation        `json:"fixture_generation"`
	Stage             Observation        `json:"stage"`
	Semantic          SemanticArtifact   `json:"semantic"`
	ExpansionOverride *ExpansionOverride `json:"expansion_override,omitempty"`
}

// ExpansionOverride records the minimum limits for one compact task fixture.
type ExpansionOverride struct {
	CandidateLimit uint64 `json:"candidate_limit"`
	MemoryLimitGB  uint64 `json:"memory_limit_gb"`
	EstimatedBytes uint64 `json:"estimated_bytes"`
	ScannableTasks uint64 `json:"scannable_tasks"`
	Reason         string `json:"reason"`
}

// RichDenySpec defines one bounded denied-work production run.
type RichDenySpec struct {
	OutputDir string `json:"output_dir"`
	Items     uint64 `json:"items"`
	Workers   int    `json:"workers"`
	Shape     string `json:"shape"`
}

// RichSpec defines one bounded accepted rich-input production run.
type RichSpec struct {
	OutputDir string `json:"output_dir"`
	Items     uint64 `json:"items"`
	Workers   int    `json:"workers"`
	Family    Family `json:"family"`
}

// RichOversizeSpec defines one oversized rich-input correctness case.
type RichOversizeSpec struct {
	OutputDir   string `json:"output_dir"`
	Items       uint64 `json:"items"`
	Workers     int    `json:"workers"`
	TargetBytes uint64 `json:"target_bytes"`
	LimitBytes  uint64 `json:"limit_bytes"`
	Case        string `json:"case"`
}

// ResumeSpec defines one bounded production resume-rebuild run.
type ResumeSpec struct {
	OutputDir        string `json:"output_dir"`
	Items            uint64 `json:"items"`
	Workers          int    `json:"workers"`
	CompletedPercent int    `json:"completed_percent"`
}

// RunResumeSmoke rebuilds and scans the incomplete part of a production snapshot.
func (Suite) RunResumeSmoke(ctx context.Context, spec ResumeSpec) (WorkflowResult, error) {
	if spec.Items == 0 || spec.CompletedPercent < 0 || spec.CompletedPercent >= 100 {
		return WorkflowResult{}, fmt.Errorf("resume smoke requires items and a completion percentage from 0 through 99")
	}
	if err := os.MkdirAll(spec.OutputDir, 0o755); err != nil {
		return WorkflowResult{}, fmt.Errorf("create resume smoke directory: %w", err)
	}
	manifest, err := New().Generate(ctx, FixtureSpec{
		Family: FamilyCandidateHeavy,
		Scale:  Scale{InputRecords: spec.Items, CandidateAddresses: spec.Items},
		Seed:   DefaultGeneratorSeed,
	}, filepath.Join(spec.OutputDir, "fixture"))
	if err != nil {
		return WorkflowResult{}, err
	}
	portPath := filepath.Join(spec.OutputDir, "ports.csv")
	if err := os.WriteFile(portPath, []byte("443/tcp\n"), 0o644); err != nil {
		return WorkflowResult{}, fmt.Errorf("write resume smoke ports: %w", err)
	}
	snapshotPath := filepath.Join(spec.OutputDir, "buckets.json")
	bucketConfig, err := config.NewGenerateBuckets(config.GenerateBucketsValues{
		CIDRFile: manifest.ArtifactPath, CIDRIPCol: "ip", CIDRIPCidrCol: "ip_cidr",
		PortFile: portPath, SnapshotOutput: snapshotPath, Workers: spec.Workers,
		ProgressInterval: int(spec.Items) + 1,
	})
	if err != nil {
		return WorkflowResult{}, err
	}
	if err := scanapp.GenerateBuckets(ctx, bucketConfig, io.Discard, scanapp.GenerateBucketsOptions{}); err != nil {
		return WorkflowResult{}, err
	}
	snapshot, err := state.LoadSnapshot(snapshotPath)
	if err != nil {
		return WorkflowResult{}, fmt.Errorf("load resume smoke snapshot: %w", err)
	}
	completed := int(spec.Items) * spec.CompletedPercent / 100
	remainingCompletion := completed
	for index := range snapshot.Chunks {
		chunkCompletion := min(remainingCompletion, snapshot.Chunks[index].TotalCount)
		snapshot.Chunks[index].NextIndex = chunkCompletion
		snapshot.Chunks[index].ScannedCount = chunkCompletion
		if chunkCompletion == snapshot.Chunks[index].TotalCount {
			snapshot.Chunks[index].Status = "completed"
		} else if chunkCompletion > 0 {
			snapshot.Chunks[index].Status = "scanning"
		}
		remainingCompletion -= chunkCompletion
	}
	if err := state.SaveSnapshot(snapshotPath, snapshot); err != nil {
		return WorkflowResult{}, fmt.Errorf("save resume smoke snapshot: %w", err)
	}
	scanConfig, err := config.NewScan(config.ScanValues{
		CIDRFile: manifest.ArtifactPath, CIDRIPCol: "ip", CIDRIPCidrCol: "ip_cidr",
		PortFile: portPath, ResumeInput: snapshotPath, Output: filepath.Join(spec.OutputDir, "results.csv"),
		Workers: spec.Workers, DialTimeout: time.Second, BucketRate: ratelimit.MaxRate,
		BucketCapacity: ratelimit.MaxCapacity, OutputFlushResults: 1000, LogLevel: "error", Format: "json", Quiet: true,
		Pressure: config.PressureDisabled(),
	})
	if err != nil {
		return WorkflowResult{}, err
	}
	var probes atomic.Uint64
	taskEvidence := newOrderedTaskEvidence()
	stage, err := New().Measure(ctx, manifest.ActualBytes, spec.Items-uint64(completed), func(runCtx context.Context) (uint64, error) {
		runErr := scanapp.Run(runCtx, scanConfig, io.Discard, io.Discard, scanapp.RunOptions{
			DisableKeyboard: true,
			Dial: func(context.Context, string, string) (net.Conn, error) {
				probes.Add(1)
				return fakeOpenConn{}, nil
			},
			TaskObserver: func(ip string, taskPort int) {
				taskEvidence.Observe(net.JoinHostPort(ip, strconv.Itoa(taskPort)) + "/tcp")
			},
		})
		return 0, runErr
	})
	if err != nil {
		return WorkflowResult{}, err
	}
	scanPath, openPath, err := workflowOutputPaths(spec.OutputDir)
	if err != nil {
		return WorkflowResult{}, err
	}
	result, err := workflowResultFromFiles(probes.Load(), 0, true, Observation{}, stage, scanPath, openPath, taskEvidence)
	if err != nil {
		return WorkflowResult{}, err
	}
	result.Semantic.Cursor = uint64(completed) + result.ScanRows
	return result, nil
}

// RunProductionSmoke runs production parsing, bucket, resume, scan, and writer paths.
func (Suite) RunProductionSmoke(ctx context.Context, spec WorkflowSpec) (WorkflowResult, error) {
	var probes atomic.Uint64
	dial := func(context.Context, string, string) (net.Conn, error) {
		probes.Add(1)
		return fakeOpenConn{}, nil
	}
	return runProductionWorkflow(ctx, spec, 443, dial, &probes)
}

// RunRichDenySmoke runs rich parsing, bucket generation, resume, and writers.
func (Suite) RunRichDenySmoke(ctx context.Context, spec RichDenySpec) (WorkflowResult, error) {
	if spec.Items == 0 {
		return WorkflowResult{}, fmt.Errorf("rich-deny items must be positive")
	}
	if spec.Shape != "deny-only" && spec.Shape != "accept-deny-conflict" {
		return WorkflowResult{}, fmt.Errorf("unsupported rich-deny shape %q", spec.Shape)
	}
	return runRichProduction(ctx, spec.OutputDir, spec.Items, spec.Workers, FixtureSpec{
		Family: FamilyRichDeny,
		Shape:  spec.Shape,
		Scale:  Scale{InputRecords: spec.Items},
		Seed:   DefaultGeneratorSeed,
	})
}

// RunRichSmoke runs one accepted rich family through the production workflow.
func (Suite) RunRichSmoke(ctx context.Context, spec RichSpec) (WorkflowResult, error) {
	if spec.Items == 0 {
		return WorkflowResult{}, fmt.Errorf("rich smoke items must be positive")
	}
	supported := spec.Family == FamilyRichRecordMixed || spec.Family == FamilyRichUniqueKey ||
		spec.Family == FamilyRichHotKey || spec.Family == FamilyRichPrecheck
	if !supported {
		return WorkflowResult{}, fmt.Errorf("unsupported rich smoke family %q", spec.Family)
	}
	return runRichProduction(ctx, spec.OutputDir, spec.Items, spec.Workers, FixtureSpec{
		Family: spec.Family,
		Scale:  Scale{InputRecords: spec.Items},
		Seed:   DefaultGeneratorSeed,
	})
}

// RunRichOversizeCase proves default rejection and positive-override completion.
func (suite Suite) RunRichOversizeCase(ctx context.Context, spec RichOversizeSpec) (CaseResult, error) {
	if spec.Items == 0 || spec.TargetBytes <= spec.LimitBytes || spec.LimitBytes == 0 {
		return CaseResult{}, fmt.Errorf("rich oversize case requires items and target bytes above a positive limit")
	}
	if spec.Case != "default-reject" && spec.Case != "override-complete" {
		return CaseResult{}, fmt.Errorf("unsupported rich oversize case %q", spec.Case)
	}
	observations := make([]Observation, 0, 6)
	for run := 0; run < 6; run++ {
		runDir := filepath.Join(spec.OutputDir, fmt.Sprintf("run-%d", run))
		observation, err := suite.Measure(ctx, spec.TargetBytes, spec.Items, func(runCtx context.Context) (uint64, error) {
			limits := config.ScanResourceLimits{CIDR: input.DefaultCIDRLimits(""), Port: input.DefaultPortLimits(""), Snapshot: state.DefaultSnapshotLimits(), Pressure: pressure.DefaultResponseLimits()}
			limits.CIDR.MaxBytes = spec.LimitBytes
			if spec.Case == "override-complete" {
				limits.CIDR.MaxBytes = spec.TargetBytes * 2
			}
			result, runErr := runRichProductionWithLimits(runCtx, runDir, spec.Items, spec.Workers, FixtureSpec{
				Family: FamilyRichHotKey,
				Scale:  Scale{InputRecords: spec.Items, TargetBytes: spec.TargetBytes},
				Seed:   DefaultGeneratorSeed,
			}, &limits)
			if spec.Case == "default-reject" {
				if runErr == nil || !strings.Contains(runErr.Error(), "-cidr-input-size-limit-gb") {
					return 0, fmt.Errorf("default rich input limit did not reject the oversized fixture: %v", runErr)
				}
				return 0, nil
			}
			if runErr != nil {
				return 0, runErr
			}
			if !result.SnapshotCompleted || result.ProbeCount == 0 || result.ScanRows != result.ProbeCount {
				return 0, fmt.Errorf("override workflow result is incomplete: %+v", result)
			}
			return result.Stage.OutputBytes, nil
		})
		if err != nil {
			return CaseResult{}, fmt.Errorf("run rich oversize %s observation %d: %w", spec.Case, run+1, err)
		}
		observations = append(observations, observation)
	}
	result, err := SummarizeCase("rich-oversize/"+spec.Case, observations)
	if err != nil {
		return CaseResult{}, err
	}
	result.Correctness = Correctness{ExpectedValues: true, Detail: "oversized rich input matched the configured limit policy"}
	result.Verdict = Verdict{Passed: true}
	result.Semantic = &SemanticArtifact{Status: "passed"}
	return result, nil
}

func runRichProduction(ctx context.Context, outputDir string, items uint64, workers int, fixture FixtureSpec) (WorkflowResult, error) {
	return runRichProductionWithLimits(ctx, outputDir, items, workers, fixture, nil)
}

func acceptedRichResourceLimits(actualBytes uint64) (config.ScanResourceLimits, error) {
	if actualBytes == 0 {
		return config.ScanResourceLimits{}, fmt.Errorf("accepted rich input size must be positive")
	}
	const decimalGB = uint64(1_000_000_000)
	limitGB := actualBytes / decimalGB
	if actualBytes%decimalGB != 0 {
		limitGB++
	}
	if limitGB > ^uint64(0)/decimalGB {
		return config.ScanResourceLimits{}, fmt.Errorf("accepted rich input size %d overflows the size override", actualBytes)
	}
	limits := config.ScanResourceLimits{
		CIDR:     input.DefaultCIDRLimits(""),
		Port:     input.DefaultPortLimits(""),
		Snapshot: state.DefaultSnapshotLimits(),
		Pressure: pressure.DefaultResponseLimits(),
	}
	limits.CIDR.MaxBytes = limitGB * decimalGB
	return limits, nil
}

func runRichProductionWithLimits(ctx context.Context, outputDir string, items uint64, workers int, fixture FixtureSpec, resourceLimits *config.ScanResourceLimits) (WorkflowResult, error) {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return WorkflowResult{}, fmt.Errorf("create rich workflow directory: %w", err)
	}
	suite := Suite{}
	var probes atomic.Uint64
	var reachability atomic.Uint64
	var manifest Manifest
	fixtureGeneration, err := suite.Measure(ctx, 0, items, func(runCtx context.Context) (uint64, error) {
		generated, generateErr := suite.Generate(runCtx, fixture, filepath.Join(outputDir, "fixture"))
		if generateErr != nil {
			return 0, fmt.Errorf("generate rich input: %w", generateErr)
		}
		manifest = generated
		return manifest.ActualBytes, nil
	})
	if err != nil {
		return WorkflowResult{}, err
	}
	if resourceLimits == nil {
		acceptedLimits, limitErr := acceptedRichResourceLimits(manifest.ActualBytes)
		if limitErr != nil {
			return WorkflowResult{}, fmt.Errorf("set accepted rich input limits: %w", limitErr)
		}
		resourceLimits = &acceptedLimits
	}
	snapshotPath := filepath.Join(outputDir, "buckets.json")
	prePingDir := filepath.Join(outputDir, "pre-ping")
	if err := os.Mkdir(prePingDir, 0o755); err != nil {
		return WorkflowResult{}, fmt.Errorf("create rich-deny pre-ping directory: %w", err)
	}
	prePingConfig, err := config.NewPrePing(config.PrePingValues{
		CIDRFile:         manifest.ArtifactPath,
		CIDRIPCol:        "src_ip",
		CIDRIPCidrCol:    "src_network_segment",
		Output:           filepath.Join(prePingDir, "results.csv"),
		Workers:          workers,
		PingTimeout:      time.Second,
		ProgressInterval: int(items) + 1,
	})
	if err == nil && resourceLimits != nil {
		prePingConfig, err = config.NewPrePingWithResourceLimits(config.PrePingValues{
			CIDRFile: manifest.ArtifactPath, CIDRIPCol: "src_ip", CIDRIPCidrCol: "src_network_segment",
			Output: filepath.Join(prePingDir, "results.csv"), Workers: workers, PingTimeout: time.Second,
			ProgressInterval: int(items) + 1,
		}, config.PrePingResourceLimits{CIDR: resourceLimits.CIDR})
	}
	if err != nil {
		return WorkflowResult{}, fmt.Errorf("create rich-deny pre-ping configuration: %w", err)
	}
	bucketConfig, err := config.NewGenerateBuckets(config.GenerateBucketsValues{
		CIDRFile:         manifest.ArtifactPath,
		CIDRIPCol:        "src_ip",
		CIDRIPCidrCol:    "src_network_segment",
		SnapshotOutput:   snapshotPath,
		Workers:          workers,
		ProgressInterval: int(items) + 1,
	})
	if err == nil && resourceLimits != nil {
		bucketConfig, err = config.NewGenerateBucketsWithResourceLimits(config.GenerateBucketsValues{
			CIDRFile: manifest.ArtifactPath, CIDRIPCol: "src_ip", CIDRIPCidrCol: "src_network_segment",
			SnapshotOutput: snapshotPath, Workers: workers, ProgressInterval: int(items) + 1,
		}, config.GenerateBucketsResourceLimits{CIDR: resourceLimits.CIDR, Port: resourceLimits.Port, Snapshot: resourceLimits.Snapshot})
	}
	if err != nil {
		return WorkflowResult{}, fmt.Errorf("create rich-deny bucket configuration: %w", err)
	}
	scanConfig, err := config.NewScan(config.ScanValues{
		CIDRFile:           manifest.ArtifactPath,
		CIDRIPCol:          "src_ip",
		CIDRIPCidrCol:      "src_network_segment",
		ResumeInput:        snapshotPath,
		Output:             filepath.Join(outputDir, "results.csv"),
		OutputFlushResults: 1000,
		Workers:            workers,
		DialTimeout:        time.Second,
		BucketRate:         ratelimit.MaxRate,
		BucketCapacity:     ratelimit.MaxCapacity,
		LogLevel:           "error",
		Format:             "json",
		Quiet:              true,
		Pressure:           config.PressureDisabled(),
	})
	if err == nil && resourceLimits != nil {
		scanConfig, err = config.NewScanWithResourceLimits(config.ScanValues{
			CIDRFile: manifest.ArtifactPath, CIDRIPCol: "src_ip", CIDRIPCidrCol: "src_network_segment",
			ResumeInput: snapshotPath, Output: filepath.Join(outputDir, "results.csv"), OutputFlushResults: 1000,
			Workers: workers, DialTimeout: time.Second, BucketRate: ratelimit.MaxRate, BucketCapacity: ratelimit.MaxCapacity,
			LogLevel: "error", Format: "json", Quiet: true, Pressure: config.PressureDisabled(),
		}, *resourceLimits)
	}
	if err != nil {
		return WorkflowResult{}, fmt.Errorf("create rich-deny scan configuration: %w", err)
	}
	var scanPath string
	var openPath string
	taskEvidence := newOrderedTaskEvidence()
	prePingCompleted := false
	stage, err := suite.Measure(ctx, manifest.ActualBytes, items, func(runCtx context.Context) (uint64, error) {
		if runErr := scanapp.RunPrePing(runCtx, prePingConfig, io.Discard, io.Discard, scanapp.RunOptions{
			ReachabilityChecker: countingReachability{count: &reachability},
		}); runErr != nil {
			return 0, fmt.Errorf("run rich-deny pre-ping workflow: %w", runErr)
		}
		prePingCompleted = true
		if runErr := scanapp.GenerateBuckets(runCtx, bucketConfig, io.Discard, scanapp.GenerateBucketsOptions{}); runErr != nil {
			return 0, fmt.Errorf("run rich-deny bucket workflow: %w", runErr)
		}
		dial := func(context.Context, string, string) (net.Conn, error) {
			probes.Add(1)
			return fakeOpenConn{}, nil
		}
		if runErr := scanapp.Run(runCtx, scanConfig, io.Discard, io.Discard, scanapp.RunOptions{
			Dial:                dial,
			DisableKeyboard:     true,
			ReachabilityChecker: countingReachability{count: &reachability},
			TaskObserver: func(ip string, taskPort int) {
				taskEvidence.Observe(net.JoinHostPort(ip, strconv.Itoa(taskPort)) + "/tcp")
			},
		}); runErr != nil {
			return 0, fmt.Errorf("run rich-deny scan workflow: %w", runErr)
		}
		var pathErr error
		scanPath, openPath, pathErr = workflowOutputPaths(outputDir)
		if pathErr != nil {
			return 0, pathErr
		}
		scanBytes, sizeErr := fileSize(scanPath)
		if sizeErr != nil {
			return 0, sizeErr
		}
		openBytes, sizeErr := fileSize(openPath)
		if sizeErr != nil {
			return 0, sizeErr
		}
		return scanBytes + openBytes, nil
	})
	if err != nil {
		return WorkflowResult{}, err
	}
	result, err := workflowResultFromFiles(probes.Load(), reachability.Load(), true, fixtureGeneration, stage, scanPath, openPath, taskEvidence)
	result.PrePingCompleted = prePingCompleted
	return result, err
}

// RunNativeLoopbackSmoke runs the production workflow against one local listener.
func (Suite) RunNativeLoopbackSmoke(ctx context.Context, spec WorkflowSpec) (WorkflowResult, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return WorkflowResult{}, fmt.Errorf("start loopback listener: %w", err)
	}
	defer func() { _ = listener.Close() }()
	port := listener.Addr().(*net.TCPAddr).Port
	accepted := make(chan error, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr == nil {
			acceptErr = connection.Close()
		}
		accepted <- acceptErr
	}()
	var probes atomic.Uint64
	dialer := &net.Dialer{}
	dial := func(dialCtx context.Context, network, address string) (net.Conn, error) {
		probes.Add(1)
		return dialer.DialContext(dialCtx, network, address)
	}
	result, err := runProductionWorkflow(ctx, spec, port, dial, &probes)
	if err != nil {
		return WorkflowResult{}, err
	}
	if acceptErr := <-accepted; acceptErr != nil {
		return WorkflowResult{}, fmt.Errorf("accept loopback probe: %w", acceptErr)
	}
	return result, nil
}

func runProductionWorkflow(ctx context.Context, spec WorkflowSpec, port int, dial scanapp.DialFunc, probes *atomic.Uint64) (WorkflowResult, error) {
	if spec.Items == 0 {
		return WorkflowResult{}, fmt.Errorf("workflow items must be positive")
	}
	if err := os.MkdirAll(spec.OutputDir, 0o755); err != nil {
		return WorkflowResult{}, fmt.Errorf("create workflow directory: %w", err)
	}
	suite := Suite{}
	fixtureDir := filepath.Join(spec.OutputDir, "fixture")
	var manifest Manifest
	portPath := filepath.Join(spec.OutputDir, "ports.csv")
	portData := []byte(fmt.Sprintf("%d/tcp\n", port))
	fixtureGeneration, err := suite.Measure(ctx, 0, spec.Items, func(runCtx context.Context) (uint64, error) {
		generated, generateErr := suite.Generate(runCtx, FixtureSpec{
			Family:     FamilyCandidateHeavy,
			LineEnding: spec.LineEnding,
			Scale:      Scale{InputRecords: spec.Items, CandidateAddresses: spec.Items},
			Seed:       DefaultGeneratorSeed,
		}, fixtureDir)
		if generateErr != nil {
			return 0, fmt.Errorf("generate workflow input: %w", generateErr)
		}
		manifest = generated
		if writeErr := os.WriteFile(portPath, portData, 0o644); writeErr != nil {
			return 0, fmt.Errorf("write workflow ports: %w", writeErr)
		}
		return manifest.ActualBytes + uint64(len(portData)), nil
	})
	if err != nil {
		return WorkflowResult{}, err
	}
	snapshotPath := filepath.Join(spec.OutputDir, "buckets.json")
	bucketConfig, err := config.NewGenerateBuckets(config.GenerateBucketsValues{
		CIDRFile:         manifest.ArtifactPath,
		CIDRIPCol:        "ip",
		CIDRIPCidrCol:    "ip_cidr",
		PortFile:         portPath,
		SnapshotOutput:   snapshotPath,
		Workers:          spec.Workers,
		ProgressInterval: int(spec.Items) + 1,
	})
	if err != nil {
		return WorkflowResult{}, fmt.Errorf("create bucket configuration: %w", err)
	}
	scanConfig, err := config.NewScan(config.ScanValues{
		CIDRFile:           manifest.ArtifactPath,
		CIDRIPCol:          "ip",
		CIDRIPCidrCol:      "ip_cidr",
		PortFile:           portPath,
		ResumeInput:        snapshotPath,
		Output:             filepath.Join(spec.OutputDir, "results.csv"),
		OutputFlushResults: 1000,
		Workers:            spec.Workers,
		DialTimeout:        time.Second,
		DispatchDelay:      0,
		BucketRate:         ratelimit.MaxRate,
		BucketCapacity:     ratelimit.MaxCapacity,
		LogLevel:           "error",
		Format:             "json",
		Quiet:              true,
		Pressure:           config.PressureDisabled(),
	})
	if err != nil {
		return WorkflowResult{}, fmt.Errorf("create scan configuration: %w", err)
	}
	var scanPath string
	var openPath string
	taskEvidence := newOrderedTaskEvidence()
	stage, err := suite.Measure(ctx, fixtureGeneration.OutputBytes, spec.Items, func(runCtx context.Context) (uint64, error) {
		if runErr := scanapp.GenerateBuckets(runCtx, bucketConfig, io.Discard, scanapp.GenerateBucketsOptions{}); runErr != nil {
			return 0, fmt.Errorf("run production bucket workflow: %w", runErr)
		}
		if runErr := scanapp.Run(runCtx, scanConfig, io.Discard, io.Discard, scanapp.RunOptions{
			Dial:            dial,
			DisableKeyboard: true,
			TaskObserver: func(ip string, taskPort int) {
				taskEvidence.Observe(net.JoinHostPort(ip, strconv.Itoa(taskPort)) + "/tcp")
			},
		}); runErr != nil {
			return 0, fmt.Errorf("run production scan workflow: %w", runErr)
		}
		var pathErr error
		scanPath, openPath, pathErr = workflowOutputPaths(spec.OutputDir)
		if pathErr != nil {
			return 0, pathErr
		}
		scanBytes, sizeErr := fileSize(scanPath)
		if sizeErr != nil {
			return 0, sizeErr
		}
		openBytes, sizeErr := fileSize(openPath)
		if sizeErr != nil {
			return 0, sizeErr
		}
		return scanBytes + openBytes, nil
	})
	if err != nil {
		return WorkflowResult{}, err
	}
	return workflowResultFromFiles(probes.Load(), 0, true, fixtureGeneration, stage, scanPath, openPath, taskEvidence)
}

func fileSize(path string) (uint64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, fmt.Errorf("read artifact size: %w", err)
	}
	return uint64(info.Size()), nil
}

func workflowOutputPaths(root string) (string, string, error) {
	scanPaths, err := filepath.Glob(filepath.Join(root, "scan_results-*.csv"))
	if err != nil {
		return "", "", fmt.Errorf("find scan results: %w", err)
	}
	openPaths, err := filepath.Glob(filepath.Join(root, "opened_results-*.csv"))
	if err != nil {
		return "", "", fmt.Errorf("find open results: %w", err)
	}
	sort.Strings(scanPaths)
	sort.Strings(openPaths)
	if len(scanPaths) != 1 || len(openPaths) != 1 {
		return "", "", fmt.Errorf("workflow produced %d scan files and %d open files", len(scanPaths), len(openPaths))
	}
	return scanPaths[0], openPaths[0], nil
}

func countCSVRows(path string) (uint64, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("open workflow CSV: %w", err)
	}
	defer func() { _ = file.Close() }()
	reader := csv.NewReader(file)
	if _, err := reader.Read(); err != nil {
		return 0, fmt.Errorf("read workflow CSV header: %w", err)
	}
	var rows uint64
	for {
		if _, err := reader.Read(); err != nil {
			if err == io.EOF {
				return rows, nil
			}
			return 0, fmt.Errorf("read workflow CSV row: %w", err)
		}
		rows++
	}
}

func fileDigest(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open artifact for digest: %w", err)
	}
	defer func() { _ = file.Close() }()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("hash artifact: %w", err)
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func workflowResultFromFiles(probes, reachability uint64, completed bool, fixtureGeneration, stage Observation, scanPath, openPath string, taskEvidence *orderedTaskEvidence) (WorkflowResult, error) {
	scanRows, err := countCSVRows(scanPath)
	if err != nil {
		return WorkflowResult{}, err
	}
	openRows, err := countCSVRows(openPath)
	if err != nil {
		return WorkflowResult{}, err
	}
	scanDigest, err := fileDigest(scanPath)
	if err != nil {
		return WorkflowResult{}, err
	}
	openDigest, err := fileDigest(openPath)
	if err != nil {
		return WorkflowResult{}, err
	}
	normalizedDigest, err := normalizedCSVDigest(scanPath)
	if err != nil {
		return WorkflowResult{}, err
	}
	tasks := taskEvidence.Snapshot()
	return WorkflowResult{
		ProbeCount:        probes,
		ReachabilityCount: reachability,
		ScanRows:          scanRows,
		OpenRows:          openRows,
		SnapshotCompleted: completed,
		ScanDigest:        scanDigest,
		OpenDigest:        openDigest,
		FixtureGeneration: fixtureGeneration,
		Stage:             stage,
		Semantic: SemanticArtifact{
			Root:         filepath.Dir(scanPath),
			Path:         filepath.Join(filepath.Dir(scanPath), "scan-results.csv"),
			TaskOrder:    tasks.Full,
			TaskCount:    tasks.Count,
			TaskDigest:   tasks.Digest,
			TaskPrefix:   tasks.Prefix,
			TaskSuffix:   tasks.Suffix,
			RowCount:     scanRows,
			Status:       "completed",
			Cursor:       scanRows,
			OutputDigest: normalizedDigest,
		},
	}, nil
}

type countingReachability struct {
	count *atomic.Uint64
}

func (checker countingReachability) Check(context.Context, string, time.Duration) scanapp.ReachabilityResult {
	checker.count.Add(1)
	return scanapp.ReachabilityResult{}
}

type fakeOpenConn struct{}

func (fakeOpenConn) Read([]byte) (int, error)         { return 0, io.EOF }
func (fakeOpenConn) Write(data []byte) (int, error)   { return len(data), nil }
func (fakeOpenConn) Close() error                     { return nil }
func (fakeOpenConn) LocalAddr() net.Addr              { return fakeAddr("local") }
func (fakeOpenConn) RemoteAddr() net.Addr             { return fakeAddr("remote") }
func (fakeOpenConn) SetDeadline(time.Time) error      { return nil }
func (fakeOpenConn) SetReadDeadline(time.Time) error  { return nil }
func (fakeOpenConn) SetWriteDeadline(time.Time) error { return nil }

type fakeAddr string

func (fakeAddr) Network() string        { return "fake" }
func (address fakeAddr) String() string { return string(address) }

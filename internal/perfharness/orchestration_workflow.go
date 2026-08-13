package perfharness

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
	"github.com/xuxiping/port-scan-mk3/pkg/input"
	"github.com/xuxiping/port-scan-mk3/pkg/ratelimit"
	"github.com/xuxiping/port-scan-mk3/pkg/scanapp"
	"github.com/xuxiping/port-scan-mk3/pkg/state"
	"github.com/xuxiping/port-scan-mk3/pkg/task"
)

type measureAction func(context.Context, uint64, uint64, MeasuredAction) (Observation, error)

type orchestrationBucketConfig struct {
	config.GenerateBucketsConfig
	expansion config.TargetExpansionValues
}

func (cfg orchestrationBucketConfig) ResolveTargetExpansion() (config.TargetExpansionValues, error) {
	return cfg.expansion, nil
}

type orchestrationScanConfig struct {
	config.ScanConfig
	expansion config.TargetExpansionValues
}

func (cfg orchestrationScanConfig) ResolveTargetExpansion() (config.TargetExpansionValues, error) {
	return cfg.expansion, nil
}

func withBucketExpansion(base config.GenerateBucketsConfig, candidates, memoryGB uint64) (orchestrationBucketConfig, error) {
	expansion, err := explicitOrchestrationExpansion(candidates, memoryGB)
	return orchestrationBucketConfig{GenerateBucketsConfig: base, expansion: expansion}, err
}

func withScanExpansion(base config.ScanConfig, candidates, memoryGB uint64) (orchestrationScanConfig, error) {
	expansion, err := explicitOrchestrationExpansion(candidates, memoryGB)
	return orchestrationScanConfig{ScanConfig: base, expansion: expansion}, err
}

func explicitOrchestrationExpansion(candidates, memoryGB uint64) (config.TargetExpansionValues, error) {
	if candidates > math.MaxInt64 || memoryGB > math.MaxInt64 {
		return config.TargetExpansionValues{}, fmt.Errorf("orchestration expansion limit exceeds int64")
	}
	limits, err := task.NewExpansionLimits(int64(candidates), int64(memoryGB))
	if err != nil {
		return config.TargetExpansionValues{}, err
	}
	return config.TargetExpansionValues{Limits: limits, CountSet: true, MemorySet: true}, nil
}

func estimateOrchestrationExpansion(records []input.CIDRRecord, scannableTasks uint64) (ExpansionOverride, error) {
	estimate, err := task.EstimateAuthorizedCIDRRecords(records, task.ExpansionLimits{}, nil)
	if err != nil {
		return ExpansionOverride{}, fmt.Errorf("estimate compact task expansion: %w", err)
	}
	const decimalGB = uint64(1_000_000_000)
	memoryGB := estimate.EstimatedBytes / decimalGB
	if estimate.EstimatedBytes%decimalGB != 0 {
		memoryGB++
	}
	if memoryGB == 0 {
		memoryGB = 1
	}
	return ExpansionOverride{
		CandidateLimit: estimate.CandidateCount,
		MemoryLimitGB:  memoryGB,
		EstimatedBytes: estimate.EstimatedBytes,
		ScannableTasks: scannableTasks,
		Reason:         "CIDR broadcast filtering needs raw candidates above the scannable task count.",
	}, nil
}

// RunOrchestrationSmoke measures scan orchestration from a prepared compact snapshot.
func (suite Suite) RunOrchestrationSmoke(ctx context.Context, spec WorkflowSpec) (WorkflowResult, error) {
	return runOrchestrationWorkflow(ctx, spec, suite.Measure)
}

func runOrchestrationWorkflow(ctx context.Context, spec WorkflowSpec, measure measureAction) (WorkflowResult, error) {
	if spec.Items == 0 {
		return WorkflowResult{}, fmt.Errorf("orchestration items must be positive")
	}
	if err := os.MkdirAll(spec.OutputDir, 0o755); err != nil {
		return WorkflowResult{}, fmt.Errorf("create orchestration directory: %w", err)
	}
	inputPath := filepath.Join(spec.OutputDir, "compact-input.csv")
	portPath := filepath.Join(spec.OutputDir, "ports.csv")
	snapshotPath := filepath.Join(spec.OutputDir, "buckets.json")
	var fixtureBytes uint64
	var expansionOverride ExpansionOverride
	fixtureGeneration, err := measure(ctx, 0, spec.Items, func(runCtx context.Context) (uint64, error) {
		if writeErr := writeCompactTaskInput(runCtx, inputPath, spec.Items, spec.LineEnding); writeErr != nil {
			return 0, writeErr
		}
		if writeErr := os.WriteFile(portPath, []byte("443/tcp\n"), 0o644); writeErr != nil {
			return 0, fmt.Errorf("write orchestration ports: %w", writeErr)
		}
		records, loadErr := input.LoadCIDRsFileWithColumnsContext(runCtx, inputPath, "ip", "ip_cidr", input.CIDRLimits{})
		if loadErr != nil {
			return 0, fmt.Errorf("load compact task input for expansion estimate: %w", loadErr)
		}
		expansionOverride, loadErr = estimateOrchestrationExpansion(records, spec.Items)
		if loadErr != nil {
			return 0, loadErr
		}
		cfg, configErr := config.NewGenerateBucketsWithResourceLimits(config.GenerateBucketsValues{
			CIDRFile: inputPath, CIDRIPCol: "ip", CIDRIPCidrCol: "ip_cidr", PortFile: portPath,
			SnapshotOutput: snapshotPath, Workers: spec.Workers, ProgressInterval: int(spec.Items) + 1,
		}, config.GenerateBucketsResourceLimits{CIDR: input.CIDRLimits{}, Port: input.PortLimits{}, Snapshot: state.SnapshotLimits{}})
		if configErr != nil {
			return 0, configErr
		}
		boundedConfig, configErr := withBucketExpansion(cfg, expansionOverride.CandidateLimit, expansionOverride.MemoryLimitGB)
		if configErr != nil {
			return 0, configErr
		}
		if runErr := scanapp.GenerateBuckets(runCtx, boundedConfig, io.Discard, scanapp.GenerateBucketsOptions{}); runErr != nil {
			return 0, fmt.Errorf("prepare orchestration snapshot: %w", runErr)
		}
		inputBytes, sizeErr := fileSize(inputPath)
		if sizeErr != nil {
			return 0, sizeErr
		}
		snapshotBytes, sizeErr := fileSize(snapshotPath)
		if sizeErr != nil {
			return 0, sizeErr
		}
		fixtureBytes = inputBytes + snapshotBytes
		return fixtureBytes, nil
	})
	if err != nil {
		return WorkflowResult{}, err
	}
	snapshot, err := state.LoadSnapshotWithLimits(snapshotPath, state.SnapshotLimits{})
	if err != nil {
		return WorkflowResult{}, fmt.Errorf("verify prepared orchestration snapshot: %w", err)
	}
	var preparedTasks uint64
	for _, chunk := range snapshot.Chunks {
		preparedTasks += uint64(chunk.TotalCount)
	}
	if preparedTasks != spec.Items {
		return WorkflowResult{}, fmt.Errorf("prepared orchestration tasks=%d, want %d", preparedTasks, spec.Items)
	}
	scanConfig, err := config.NewScanWithResourceLimits(config.ScanValues{
		CIDRFile: inputPath, CIDRIPCol: "ip", CIDRIPCidrCol: "ip_cidr", PortFile: portPath,
		ResumeInput: snapshotPath, Output: filepath.Join(spec.OutputDir, "results.csv"), OutputFlushResults: 1000,
		Workers: spec.Workers, DialTimeout: time.Second, DispatchDelay: 0, BucketRate: ratelimit.MaxRate,
		BucketCapacity: ratelimit.MaxCapacity, LogLevel: "error", Format: "json", Quiet: true,
		Pressure: config.PressureDisabled(),
	}, config.ScanResourceLimits{CIDR: input.CIDRLimits{}, Port: input.PortLimits{}, Snapshot: state.SnapshotLimits{}})
	if err != nil {
		return WorkflowResult{}, fmt.Errorf("create orchestration scan configuration: %w", err)
	}
	boundedScanConfig, err := withScanExpansion(scanConfig, expansionOverride.CandidateLimit, expansionOverride.MemoryLimitGB)
	if err != nil {
		return WorkflowResult{}, err
	}
	var probes atomic.Uint64
	tasks := newOrderedTaskEvidence()
	var scanPath, openPath string
	stage, err := measure(ctx, fixtureBytes, spec.Items, func(runCtx context.Context) (uint64, error) {
		if runErr := scanapp.Run(runCtx, boundedScanConfig, io.Discard, io.Discard, scanapp.RunOptions{
			DisableKeyboard: true,
			Dial: func(context.Context, string, string) (net.Conn, error) {
				probes.Add(1)
				return fakeOpenConn{}, nil
			},
			TaskObserver: func(ip string, port int) {
				tasks.Observe(net.JoinHostPort(ip, strconv.Itoa(port)) + "/tcp")
			},
		}); runErr != nil {
			return 0, fmt.Errorf("run production scan orchestration: %w", runErr)
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
		return scanBytes + openBytes, sizeErr
	})
	if err != nil {
		return WorkflowResult{}, err
	}
	result, err := workflowResultFromFiles(probes.Load(), 0, true, fixtureGeneration, stage, scanPath, openPath, tasks)
	if err != nil {
		return WorkflowResult{}, err
	}
	result.Semantic.SnapshotDigest, err = normalizedSnapshotDigest(snapshotPath)
	if err != nil {
		return WorkflowResult{}, err
	}
	result.ExpansionOverride = &expansionOverride
	return result, nil
}

func normalizedSnapshotDigest(path string) (string, error) {
	snapshot, err := state.LoadSnapshotWithLimits(path, state.SnapshotLimits{})
	if err != nil {
		return "", fmt.Errorf("load orchestration snapshot for digest: %w", err)
	}
	if snapshot.Output != nil {
		snapshot.Output.ScanPath = "scan-results.csv"
		snapshot.Output.OpenPath = "open-results.csv"
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		return "", fmt.Errorf("encode orchestration snapshot for digest: %w", err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(data)), nil
}

func writeCompactTaskInput(ctx context.Context, path string, items uint64, lineEnding string) (resultErr error) {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create compact task input: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil && resultErr == nil {
			resultErr = fmt.Errorf("close compact task input: %w", closeErr)
		}
	}()
	buffered := bufio.NewWriter(file)
	writer := csv.NewWriter(buffered)
	writer.UseCRLF = lineEnding == "CRLF"
	if err := writer.Write([]string{"ip", "ip_cidr"}); err != nil {
		return fmt.Errorf("write compact task input header: %w", err)
	}
	blocks, singles, err := compactTaskBlocks(items)
	if err != nil {
		return err
	}
	for index, block := range blocks {
		if index%4_096 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		if err := writer.Write([]string{block, block}); err != nil {
			return fmt.Errorf("write compact task CIDR: %w", err)
		}
	}
	for _, ip := range singles {
		if err := writer.Write([]string{ip, ip + "/32"}); err != nil {
			return fmt.Errorf("write compact task IP: %w", err)
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return fmt.Errorf("flush compact task CSV: %w", err)
	}
	if err := buffered.Flush(); err != nil {
		return fmt.Errorf("flush compact task input: %w", err)
	}
	return nil
}

func compactTaskBlocks(items uint64) ([]string, []string, error) {
	const base = uint32(10) << 24
	const capacity = uint64(1) << 24
	if items == 0 || items+64 > capacity {
		return nil, nil, fmt.Errorf("compact task count %d exceeds private IPv4 fixture capacity", items)
	}
	blocks := make([]string, 0, 32)
	var singles uint64
	cursor := uint64(base)
	for bit := 63; bit >= 0; bit-- {
		size := uint64(1) << uint(bit)
		if items&size == 0 {
			continue
		}
		switch bit {
		case 0:
			singles++
		case 1:
			blocks = append(blocks, fmt.Sprintf("%s/31", uint32IPv4(uint32(cursor))))
			cursor += size
		default:
			prefix := 32 - bit
			blocks = append(blocks, fmt.Sprintf("%s/%d", uint32IPv4(uint32(cursor)), prefix))
			cursor += size
			singles++
		}
	}
	singleIPs := make([]string, singles)
	for index := range singleIPs {
		singleIPs[index] = uint32IPv4(uint32(cursor + uint64(index)))
	}
	return blocks, singleIPs, nil
}

func uint32IPv4(value uint32) string {
	var raw [4]byte
	binary.BigEndian.PutUint32(raw[:], value)
	return net.IP(raw[:]).String()
}

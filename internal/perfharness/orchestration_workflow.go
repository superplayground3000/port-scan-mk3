package perfharness

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/csv"
	"fmt"
	"io"
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
)

type measureAction func(context.Context, uint64, uint64, MeasuredAction) (Observation, error)

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
	fixtureGeneration, err := measure(ctx, 0, spec.Items, func(runCtx context.Context) (uint64, error) {
		if writeErr := writeCompactTaskInput(runCtx, inputPath, spec.Items, spec.LineEnding); writeErr != nil {
			return 0, writeErr
		}
		if writeErr := os.WriteFile(portPath, []byte("443/tcp\n"), 0o644); writeErr != nil {
			return 0, fmt.Errorf("write orchestration ports: %w", writeErr)
		}
		cfg, configErr := config.NewGenerateBucketsWithResourceLimits(config.GenerateBucketsValues{
			CIDRFile: inputPath, CIDRIPCol: "ip", CIDRIPCidrCol: "ip_cidr", PortFile: portPath,
			SnapshotOutput: snapshotPath, Workers: spec.Workers, ProgressInterval: int(spec.Items) + 1,
		}, config.GenerateBucketsResourceLimits{CIDR: input.CIDRLimits{}, Port: input.PortLimits{}, Snapshot: state.SnapshotLimits{}})
		if configErr != nil {
			return 0, configErr
		}
		if runErr := scanapp.GenerateBuckets(runCtx, cfg, io.Discard, scanapp.GenerateBucketsOptions{}); runErr != nil {
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
	var probes atomic.Uint64
	tasks := newOrderedTaskEvidence()
	var scanPath, openPath string
	stage, err := measure(ctx, fixtureBytes, spec.Items, func(runCtx context.Context) (uint64, error) {
		if runErr := scanapp.Run(runCtx, scanConfig, io.Discard, io.Discard, scanapp.RunOptions{
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
	return workflowResultFromFiles(probes.Load(), 0, true, fixtureGeneration, stage, scanPath, openPath, tasks)
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

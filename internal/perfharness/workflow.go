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
	"sync/atomic"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/config"
	"github.com/xuxiping/port-scan-mk3/pkg/ratelimit"
	"github.com/xuxiping/port-scan-mk3/pkg/scanapp"
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
	ProbeCount        uint64      `json:"probe_count"`
	ScanRows          uint64      `json:"scan_rows"`
	OpenRows          uint64      `json:"open_rows"`
	SnapshotCompleted bool        `json:"snapshot_completed"`
	ScanDigest        string      `json:"scan_digest"`
	OpenDigest        string      `json:"open_digest"`
	FixtureGeneration Observation `json:"fixture_generation"`
	Stage             Observation `json:"stage"`
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
		CIDRFile:       manifest.ArtifactPath,
		CIDRIPCol:      "ip",
		CIDRIPCidrCol:  "ip_cidr",
		PortFile:       portPath,
		ResumeInput:    snapshotPath,
		Output:         filepath.Join(spec.OutputDir, "results.csv"),
		Workers:        spec.Workers,
		DialTimeout:    time.Second,
		DispatchDelay:  0,
		BucketRate:     ratelimit.MaxRate,
		BucketCapacity: ratelimit.MaxCapacity,
		LogLevel:       "error",
		Format:         "json",
		Quiet:          true,
		Pressure:       config.PressureDisabled(),
	})
	if err != nil {
		return WorkflowResult{}, fmt.Errorf("create scan configuration: %w", err)
	}
	var scanPath string
	var openPath string
	stage, err := suite.Measure(ctx, fixtureGeneration.OutputBytes, spec.Items, func(runCtx context.Context) (uint64, error) {
		if runErr := scanapp.GenerateBuckets(runCtx, bucketConfig, io.Discard, scanapp.GenerateBucketsOptions{}); runErr != nil {
			return 0, fmt.Errorf("run production bucket workflow: %w", runErr)
		}
		if runErr := scanapp.Run(runCtx, scanConfig, io.Discard, io.Discard, scanapp.RunOptions{
			Dial:            dial,
			DisableKeyboard: true,
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
	return WorkflowResult{
		ProbeCount:        probes.Load(),
		ScanRows:          scanRows,
		OpenRows:          openRows,
		SnapshotCompleted: scanRows == spec.Items,
		ScanDigest:        scanDigest,
		OpenDigest:        openDigest,
		FixtureGeneration: fixtureGeneration,
		Stage:             stage,
	}, nil
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

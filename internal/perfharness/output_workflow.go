package perfharness

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/xuxiping/port-scan-mk3/pkg/writer"
)

// OutputSpec defines one all-open output performance case.
type OutputSpec struct {
	OutputDir    string `json:"output_dir"`
	Results      uint64 `json:"results"`
	FlushResults int    `json:"flush_results"`
}

// RunOutputCase writes all-open results through both production CSV writers.
// It runs the case six times and removes each output after measurement.
func (suite Suite) RunOutputCase(ctx context.Context, spec OutputSpec) (CaseResult, error) {
	if spec.Results == 0 {
		return CaseResult{}, fmt.Errorf("output results must be positive")
	}
	if spec.FlushResults < 0 {
		return CaseResult{}, fmt.Errorf("output flush results must be zero or positive")
	}
	if err := os.Mkdir(spec.OutputDir, 0o755); err != nil {
		return CaseResult{}, fmt.Errorf("create output case directory: %w", err)
	}

	observations := make([]Observation, 0, 6)
	for run := 0; run < 6; run++ {
		scanPath := filepath.Join(spec.OutputDir, fmt.Sprintf("scan-%d.csv", run))
		openPath := filepath.Join(spec.OutputDir, fmt.Sprintf("opened-%d.csv", run))
		observation, err := suite.Measure(ctx, 0, spec.Results, func(runCtx context.Context) (uint64, error) {
			return writeOutputCase(runCtx, scanPath, openPath, spec)
		})
		if err != nil {
			_ = removeExistingOutputPair(scanPath, openPath)
			return CaseResult{}, fmt.Errorf("write output observation %d: %w", run+1, err)
		}
		validationErr := validateOutputPair(scanPath, openPath, spec.Results)
		removeErr := removeOutputPair(scanPath, openPath)
		if validationErr != nil {
			return CaseResult{}, fmt.Errorf("validate output observation %d: %w", run+1, validationErr)
		}
		if removeErr != nil {
			return CaseResult{}, removeErr
		}
		observations = append(observations, observation)
	}

	result, err := SummarizeCase(fmt.Sprintf("output-heavy/results-%d/flush-%d", spec.Results, spec.FlushResults), observations)
	if err != nil {
		return CaseResult{}, err
	}
	result.Correctness = Correctness{Headers: true, RowCounts: true, ExpectedValues: true, Digests: true}
	result.LogicalItems = spec.Results
	result.Verdict = Verdict{Passed: true}
	return result, nil
}

func writeOutputCase(ctx context.Context, scanPath, openPath string, spec OutputSpec) (outputBytes uint64, resultErr error) {
	scanFile, err := os.Create(scanPath)
	if err != nil {
		return 0, fmt.Errorf("create scan output: %w", err)
	}
	openFile, err := os.Create(openPath)
	if err != nil {
		_ = scanFile.Close()
		return 0, fmt.Errorf("create open output: %w", err)
	}
	defer func() {
		if err := openFile.Close(); err != nil && resultErr == nil {
			resultErr = fmt.Errorf("close open output: %w", err)
		}
		if err := scanFile.Close(); err != nil && resultErr == nil {
			resultErr = fmt.Errorf("close scan output: %w", err)
		}
	}()

	scanWriter := writer.NewBufferedCSVWriter(scanFile)
	openInner := writer.NewBufferedCSVWriter(openFile)
	openWriter := writer.NewOpenOnlyWriter(openInner)
	record := writer.Record{
		IP: "192.0.2.1", IPCidr: "192.0.2.1/32", Port: 443, Status: "open",
		ResponseMS: 1, FabName: "fab-a", CIDRName: "segment-a", ServiceLabel: "https",
		Decision: "accept", PolicyID: "policy-a", Reason: "MATCH_POLICY_ACCEPT",
		ExecutionKey: "192.0.2.1:443/tcp", SrcIP: "198.51.100.1", SrcNetworkSegment: "198.51.100.0/24",
	}
	pending := 0
	flush := func() error {
		var firstErr error
		if err := scanWriter.Flush(); err != nil {
			firstErr = fmt.Errorf("flush scan output: %w", err)
		}
		if err := openWriter.Flush(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("flush open output: %w", err)
		}
		pending = 0
		return firstErr
	}
	for index := uint64(0); index < spec.Results; index++ {
		if index%4_096 == 0 {
			if err := ctx.Err(); err != nil {
				return 0, err
			}
		}
		if err := scanWriter.Write(record); err != nil {
			return 0, fmt.Errorf("write scan output: %w", err)
		}
		if err := openWriter.Write(record); err != nil {
			return 0, fmt.Errorf("write open output: %w", err)
		}
		pending++
		if spec.FlushResults > 0 && pending >= spec.FlushResults {
			if err := flush(); err != nil {
				return 0, err
			}
		}
	}
	if pending > 0 {
		if err := flush(); err != nil {
			return 0, err
		}
	}
	scanInfo, err := scanFile.Stat()
	if err != nil {
		return 0, fmt.Errorf("stat scan output: %w", err)
	}
	openInfo, err := openFile.Stat()
	if err != nil {
		return 0, fmt.Errorf("stat open output: %w", err)
	}
	return uint64(scanInfo.Size() + openInfo.Size()), nil
}

func validateOutputPair(scanPath, openPath string, results uint64) error {
	scanRows, scanDigest, err := inspectOutput(scanPath)
	if err != nil {
		return err
	}
	openRows, openDigest, err := inspectOutput(openPath)
	if err != nil {
		return err
	}
	if scanRows != results || openRows != results {
		return fmt.Errorf("output row counts are scan=%d open=%d, want %d", scanRows, openRows, results)
	}
	if scanDigest != openDigest {
		return fmt.Errorf("all-open output digests differ")
	}
	return nil
}

func inspectOutput(path string) (uint64, [sha256.Size]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, [sha256.Size]byte{}, fmt.Errorf("open output %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()
	hash := sha256.New()
	reader := csv.NewReader(io.TeeReader(file, hash))
	header, err := reader.Read()
	if err != nil {
		return 0, [sha256.Size]byte{}, fmt.Errorf("read output header %s: %w", path, err)
	}
	if len(header) != len(writer.Columns) {
		return 0, [sha256.Size]byte{}, fmt.Errorf("output header %s has %d columns", path, len(header))
	}
	for index, column := range writer.Columns {
		if header[index] != column.Name {
			return 0, [sha256.Size]byte{}, fmt.Errorf("output header %s column %d is %q, want %q", path, index, header[index], column.Name)
		}
	}
	var rows uint64
	for {
		_, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, [sha256.Size]byte{}, fmt.Errorf("read output row %s: %w", path, err)
		}
		rows++
	}
	var digest [sha256.Size]byte
	copy(digest[:], hash.Sum(nil))
	return rows, digest, nil
}

func removeOutputPair(paths ...string) error {
	for _, path := range paths {
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("remove output %s: %w", path, err)
		}
	}
	return nil
}

func removeExistingOutputPair(paths ...string) error {
	for _, path := range paths {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove output %s: %w", path, err)
		}
	}
	return nil
}

func summarizeFiveRunCase(name string, runs []Observation) CaseResult {
	return CaseResult{
		Name:         name,
		Runs:         append([]Observation(nil), runs...),
		ColdStart:    runs[0],
		SteadyMedian: medianObservation(runs),
	}
}

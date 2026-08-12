package input

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"os"
)

const (
	// DefaultCIDRSizeLimitBytes is the default complete CIDR input size.
	DefaultCIDRSizeLimitBytes uint64 = 1_000_000_000
	// DefaultCIDRRecordLimit is the default CIDR data-record count.
	DefaultCIDRRecordLimit uint64 = 10_000_000
	// DefaultPortSizeLimitBytes is the default complete port input size.
	DefaultPortSizeLimitBytes uint64 = 1_000_000
	// DefaultPortRecordLimit is the default nonblank port-record count.
	DefaultPortRecordLimit uint64 = 65_535
)

// CIDRLimits controls the path, byte count, and data-record count for one CIDR input.
// A zero maximum disables only that limit.
type CIDRLimits struct {
	Path       string
	MaxBytes   uint64
	MaxRecords uint64
	capacity   uint64
}

// LoadCIDRsFileWithColumnsContext reads the file at path and returns its CIDR records.
// The column names select basic input fields. The limits apply to file bytes and data records.
// It returns a path, parse, context, or limit error without retaining a second input buffer.
func LoadCIDRsFileWithColumnsContext(ctx context.Context, path, ipCol, ipCidrCol string, limits CIDRLimits) ([]CIDRRecord, error) {
	limits.Path = path
	if err := checkFileSize(path, "CIDR", "-cidr-input-size-limit-gb", limits.MaxBytes); err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open CIDR input %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	records, err := loadCIDRsSeekable(ctx, path, f, ipCol, ipCidrCol, limits)
	if err != nil {
		return nil, fmt.Errorf("load CIDR input %s: %w", path, err)
	}
	return records, nil
}

func loadCIDRsSeekable(ctx context.Context, path string, file io.ReadSeeker, ipCol, ipCidrCol string, limits CIDRLimits) ([]CIDRRecord, error) {
	recordCount, err := countCIDRFileRecords(ctx, file, limits)
	if err != nil {
		return nil, err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("rewind CIDR input %s: %w", path, err)
	}
	limits.capacity = recordCount
	records, err := LoadCIDRsWithColumnsContextAndLimits(ctx, file, ipCol, ipCidrCol, limits)
	if err != nil {
		return nil, err
	}
	if uint64(len(records)) != recordCount {
		return nil, fmt.Errorf("CIDR input %s changed during load: counted %d records, parsed %d", displayPath(path), recordCount, len(records))
	}
	return records, nil
}

func countCIDRFileRecords(ctx context.Context, inputReader io.Reader, limits CIDRLimits) (uint64, error) {
	reader := csv.NewReader(limitInputReader(inputReader, limits.Path, "CIDR", "-cidr-input-size-limit-gb", limits.MaxBytes))
	reader.ReuseRecord = true
	if _, err := readCSVRecordContext(ctx, reader); err != nil {
		if err == io.EOF {
			return 0, fmt.Errorf("cidr csv must include header and at least one row")
		}
		return 0, err
	}
	var count uint64
	for {
		_, err := readCIDRDataRecord(ctx, reader, limits, count)
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, err
		}
		count++
	}
	if count == 0 {
		return 0, fmt.Errorf("cidr csv must include header and at least one row")
	}
	return count, nil
}

// LoadPortsFileContext reads the file at path and returns normalized port records.
// The limits apply to file bytes and nonblank records.
// It returns a path, parse, context, or limit error.
func LoadPortsFileContext(ctx context.Context, path string, limits PortLimits) ([]PortSpec, error) {
	limits.Path = path
	if err := checkFileSize(path, "port", "-port-input-size-limit-mb", limits.MaxBytes); err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open port input %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	records, err := LoadPortsContextWithLimits(ctx, f, limits)
	if err != nil {
		return nil, fmt.Errorf("load port input %s: %w", path, err)
	}
	return records, nil
}

func checkFileSize(path, kind, flagName string, maxBytes uint64) error {
	if maxBytes == 0 {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat %s input %s: %w", kind, path, err)
	}
	if info.Size() >= 0 && uint64(info.Size()) > maxBytes {
		return fmt.Errorf("%s input %s size %d bytes exceeds limit %d bytes; use %s to override it", kind, path, info.Size(), maxBytes, flagName)
	}
	return nil
}

// DefaultCIDRLimits returns the default byte and record limits for path.
// This function does not open the path and cannot return an error.
func DefaultCIDRLimits(path string) CIDRLimits {
	return CIDRLimits{Path: path, MaxBytes: DefaultCIDRSizeLimitBytes, MaxRecords: DefaultCIDRRecordLimit}
}

// PortLimits controls the path, byte count, and nonblank-record count for one port input.
// A zero maximum disables only that limit.
type PortLimits struct {
	Path       string
	MaxBytes   uint64
	MaxRecords uint64
}

// DefaultPortLimits returns the default byte and record limits for path.
// This function does not open the path and cannot return an error.
func DefaultPortLimits(path string) PortLimits {
	return PortLimits{Path: path, MaxBytes: DefaultPortSizeLimitBytes, MaxRecords: DefaultPortRecordLimit}
}

type boundedInputReader struct {
	reader   io.Reader
	path     string
	kind     string
	flagName string
	maxBytes uint64
	consumed uint64
}

func limitInputReader(r io.Reader, path, kind, flagName string, maxBytes uint64) io.Reader {
	if maxBytes == 0 {
		return r
	}
	return &boundedInputReader{
		reader:   r,
		path:     path,
		kind:     kind,
		flagName: flagName,
		maxBytes: maxBytes,
	}
}

func (r *boundedInputReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	remaining := r.maxBytes - min(r.consumed, r.maxBytes)
	if remaining == 0 {
		var next [1]byte
		n, err := r.reader.Read(next[:])
		if n > 0 {
			actual := r.maxBytes
			if actual < ^uint64(0) {
				actual++
			}
			return 0, r.limitError(actual)
		}
		return 0, err
	}
	readSize := uint64(len(p))
	if remaining < readSize {
		readSize = remaining + 1
	}
	n, err := r.reader.Read(p[:int(readSize)])
	if uint64(n) > math.MaxUint64-r.consumed {
		return n, fmt.Errorf("%s input %s byte count overflows the supported range", r.kind, displayPath(r.path))
	}
	r.consumed += uint64(n)
	if r.consumed > r.maxBytes {
		return n, r.limitError(r.consumed)
	}
	return n, err
}

func incrementInputCount(count *uint64, kind string) error {
	if *count == math.MaxUint64 {
		return fmt.Errorf("%s count overflows the supported range", kind)
	}
	*count++
	return nil
}

func (r *boundedInputReader) limitError(actual uint64) error {
	return fmt.Errorf("%s input %s size %d bytes exceeds limit %d bytes; use %s to override it", r.kind, displayPath(r.path), actual, r.maxBytes, r.flagName)
}

func displayPath(path string) string {
	if path == "" {
		return "<reader>"
	}
	return path
}

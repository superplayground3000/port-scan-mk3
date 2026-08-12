package input

import (
	"context"
	"fmt"
	"io"
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

// CIDRLimits controls one CIDR input. A zero value disables that limit.
type CIDRLimits struct {
	Path       string
	MaxBytes   uint64
	MaxRecords uint64
}

// LoadCIDRsFileWithColumnsContext reads one CIDR file with metadata and stream limits.
func LoadCIDRsFileWithColumnsContext(ctx context.Context, path, ipCol, ipCidrCol string, limits CIDRLimits) ([]CIDRRecord, error) {
	limits.Path = path
	if err := checkFileSize(path, "CIDR", "-cidr-input-size-limit-gb", limits.MaxBytes); err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return LoadCIDRsWithColumnsContextAndLimits(ctx, f, ipCol, ipCidrCol, limits)
}

// LoadPortsFileContext reads one port file with metadata and stream limits.
func LoadPortsFileContext(ctx context.Context, path string, limits PortLimits) ([]PortSpec, error) {
	limits.Path = path
	if err := checkFileSize(path, "port", "-port-input-size-limit-mb", limits.MaxBytes); err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return LoadPortsContextWithLimits(ctx, f, limits)
}

func checkFileSize(path, kind, flagName string, maxBytes uint64) error {
	if maxBytes == 0 {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Size() >= 0 && uint64(info.Size()) > maxBytes {
		return fmt.Errorf("%s input %s size %d bytes exceeds limit %d bytes; use %s to override it", kind, path, info.Size(), maxBytes, flagName)
	}
	return nil
}

// DefaultCIDRLimits returns the default limits for a CIDR input path.
func DefaultCIDRLimits(path string) CIDRLimits {
	return CIDRLimits{Path: path, MaxBytes: DefaultCIDRSizeLimitBytes, MaxRecords: DefaultCIDRRecordLimit}
}

// PortLimits controls one port input. A zero value disables that limit.
type PortLimits struct {
	Path       string
	MaxBytes   uint64
	MaxRecords uint64
}

// DefaultPortLimits returns the default limits for a port input path.
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
	r.consumed += uint64(n)
	if r.consumed > r.maxBytes {
		return n, r.limitError(r.consumed)
	}
	return n, err
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

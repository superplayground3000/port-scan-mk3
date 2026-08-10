package config

import (
	"errors"
	"flag"
	"fmt"
	"strings"
)

// GenerateBucketsValues contains the values for the bucket workflow.
type GenerateBucketsValues struct {
	CIDRFile         string
	CIDRIPCol        string
	CIDRIPCidrCol    string
	PortFile         string
	BlocklistFile    string
	SnapshotOutput   string
	Workers          int
	ProgressInterval int
}

type generateBucketsState struct {
	values GenerateBucketsValues
}

// GenerateBucketsConfig is an opaque configuration for the bucket workflow.
type GenerateBucketsConfig struct {
	state *generateBucketsState
}

// NewGenerateBuckets verifies the values and returns a bucket configuration.
func NewGenerateBuckets(values GenerateBucketsValues) (GenerateBucketsConfig, error) {
	if values.CIDRFile == "" {
		return GenerateBucketsConfig{}, errors.New("-cidr-file is required")
	}
	if strings.TrimSpace(values.CIDRIPCol) == "" || strings.TrimSpace(values.CIDRIPCidrCol) == "" {
		return GenerateBucketsConfig{}, errors.New("-cidr-ip-col and -cidr-ip-cidr-col must be non-empty")
	}
	if values.SnapshotOutput == "" {
		return GenerateBucketsConfig{}, errors.New("-buckets-out is required")
	}
	if err := validateWorkers(values.Workers); err != nil {
		return GenerateBucketsConfig{}, fmt.Errorf("validate workers: %w", err)
	}
	return GenerateBucketsConfig{state: &generateBucketsState{values: values}}, nil
}

// ParseGenerateBuckets parses and verifies the bucket command arguments.
func ParseGenerateBuckets(args []string) (GenerateBucketsConfig, error) {
	fs := flag.NewFlagSet("port-scan generate-buckets", flag.ContinueOnError)
	common := commonCLIValues{}
	values := GenerateBucketsValues{}
	registerCommonFlags(fs, &common)
	fs.IntVar(&values.Workers, "workers", 10, fmt.Sprintf("worker count (1-%d)", MaxWorkers))
	fs.IntVar(&values.ProgressInterval, "progress-interval", defaultProgressInterval, "progress line cadence (count of processed units)")
	fs.StringVar(&values.PortFile, "port-file", "", "Port CSV path")
	fs.StringVar(&values.BlocklistFile, "unreachable-file", "", "unreachable blocklist CSV path (optional)")
	fs.StringVar(&values.SnapshotOutput, "buckets-out", "", "bucket snapshot output path (required)")

	if err := fs.Parse(args); err != nil {
		return GenerateBucketsConfig{}, fmt.Errorf("parse generate-buckets flags: %w", err)
	}
	if err := common.validate(); err != nil {
		return GenerateBucketsConfig{}, fmt.Errorf("validate generate-buckets flags: %w", err)
	}
	values.CIDRFile = common.cidrFile
	values.CIDRIPCol = common.cidrIPCol
	values.CIDRIPCidrCol = common.cidrIPCidrCol
	cfg, err := NewGenerateBuckets(values)
	if err != nil {
		return GenerateBucketsConfig{}, fmt.Errorf("validate generate-buckets arguments: %w", err)
	}
	return cfg, nil
}

// Resolve returns the values for the bucket workflow.
func (c GenerateBucketsConfig) Resolve() (GenerateBucketsValues, error) {
	if c.state == nil {
		return GenerateBucketsValues{}, ErrUninitializedConfiguration
	}
	return c.state.values, nil
}

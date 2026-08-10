package config

import (
	"errors"
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
		return GenerateBucketsConfig{}, err
	}
	return GenerateBucketsConfig{state: &generateBucketsState{values: values}}, nil
}

// ParseGenerateBuckets parses and verifies the bucket command arguments.
func ParseGenerateBuckets(args []string) (GenerateBucketsConfig, error) {
	cfg, err := ParseFor("generate-buckets", args)
	if err != nil {
		return GenerateBucketsConfig{}, err
	}

	return NewGenerateBuckets(GenerateBucketsValues{
		CIDRFile:         cfg.CIDRFile,
		CIDRIPCol:        cfg.CIDRIPCol,
		CIDRIPCidrCol:    cfg.CIDRIPCidrCol,
		PortFile:         cfg.PortFile,
		BlocklistFile:    cfg.UnreachableFile,
		SnapshotOutput:   cfg.BucketsOut,
		Workers:          cfg.Workers,
		ProgressInterval: cfg.ProgressInterval,
	})
}

// Resolve returns the values for the bucket workflow.
func (c GenerateBucketsConfig) Resolve() (GenerateBucketsValues, error) {
	if c.state == nil {
		return GenerateBucketsValues{}, ErrUninitializedConfiguration
	}
	return c.state.values, nil
}

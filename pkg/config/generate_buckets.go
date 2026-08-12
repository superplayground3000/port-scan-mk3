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
	values    GenerateBucketsValues
	expansion TargetExpansionValues
	resources ResourceLimitValues
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
	return GenerateBucketsConfig{state: &generateBucketsState{values: values, expansion: defaultTargetExpansionValues(), resources: defaultResourceLimitValues()}}, nil
}

// NewGenerateBucketsWithResourceLimits verifies values and keeps explicit limits.
func NewGenerateBucketsWithResourceLimits(values GenerateBucketsValues, limits ResourceLimitValues) (GenerateBucketsConfig, error) {
	cfg, err := NewGenerateBuckets(values)
	if err != nil {
		return GenerateBucketsConfig{}, err
	}
	cfg.state.resources = limits
	return cfg, nil
}

// ParseGenerateBuckets parses and verifies the bucket command arguments.
func ParseGenerateBuckets(args []string) (GenerateBucketsConfig, error) {
	fs := flag.NewFlagSet("port-scan generate-buckets", flag.ContinueOnError)
	common := commonCLIValues{}
	values := GenerateBucketsValues{}
	expansionFlags := targetExpansionFlagValues{}
	resourceFlags := defaultResourceLimitFlagValues()
	registerCommonFlags(fs, &common)
	registerTargetExpansionFlags(fs, &expansionFlags)
	registerCIDRLimitFlags(fs, &resourceFlags)
	registerPortLimitFlags(fs, &resourceFlags)
	registerSnapshotLimitFlags(fs, &resourceFlags)
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
	expansion, err := resolveTargetExpansionFlags(fs, expansionFlags)
	if err != nil {
		return GenerateBucketsConfig{}, err
	}
	resources, err := resourceFlags.resolve()
	if err != nil {
		return GenerateBucketsConfig{}, fmt.Errorf("validate resource limits: %w", err)
	}
	values.CIDRFile = common.cidrFile
	values.CIDRIPCol = common.cidrIPCol
	values.CIDRIPCidrCol = common.cidrIPCidrCol
	cfg, err := NewGenerateBuckets(values)
	if err != nil {
		return GenerateBucketsConfig{}, fmt.Errorf("validate generate-buckets arguments: %w", err)
	}
	cfg.state.expansion = expansion
	cfg.state.resources = resources
	return cfg, nil
}

// ResolveResourceLimits returns the verified input and snapshot limits.
func (c GenerateBucketsConfig) ResolveResourceLimits() (ResourceLimitValues, error) {
	if c.state == nil {
		return ResourceLimitValues{}, ErrUninitializedConfiguration
	}
	return c.state.resources, nil
}

// Resolve returns the values for the bucket workflow.
func (c GenerateBucketsConfig) Resolve() (GenerateBucketsValues, error) {
	if c.state == nil {
		return GenerateBucketsValues{}, ErrUninitializedConfiguration
	}
	return c.state.values, nil
}

// ResolveTargetExpansion returns the verified target expansion values.
func (c GenerateBucketsConfig) ResolveTargetExpansion() (TargetExpansionValues, error) {
	if c.state == nil {
		return TargetExpansionValues{}, ErrUninitializedConfiguration
	}
	return c.state.expansion, nil
}

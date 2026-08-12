package config

import (
	"errors"
	"flag"
	"fmt"
	"strings"
	"time"
)

// ErrUninitializedConfiguration reports an opaque configuration zero value.
var ErrUninitializedConfiguration = errors.New("configuration is not initialized")

// PrePingValues contains the validated values for the pre-ping workflow.
type PrePingValues struct {
	CIDRFile         string
	CIDRIPCol        string
	CIDRIPCidrCol    string
	Output           string
	Workers          int
	PingTimeout      time.Duration
	ProgressInterval int
}

type prePingState struct {
	values    PrePingValues
	expansion TargetExpansionValues
}

// PrePingConfig is an opaque configuration value for the pre-ping workflow.
type PrePingConfig struct {
	state *prePingState
}

// NewPrePing verifies the values and returns a pre-ping configuration.
func NewPrePing(values PrePingValues) (PrePingConfig, error) {
	if values.CIDRFile == "" {
		return PrePingConfig{}, errors.New("-cidr-file is required")
	}
	if strings.TrimSpace(values.CIDRIPCol) == "" || strings.TrimSpace(values.CIDRIPCidrCol) == "" {
		return PrePingConfig{}, errors.New("-cidr-ip-col and -cidr-ip-cidr-col must be non-empty")
	}
	if err := validateWorkers(values.Workers); err != nil {
		return PrePingConfig{}, fmt.Errorf("validate workers: %w", err)
	}
	if values.PingTimeout <= 0 {
		return PrePingConfig{}, errors.New("-pre-scan-ping-timeout must be > 0")
	}
	return PrePingConfig{state: &prePingState{values: values, expansion: defaultTargetExpansionValues()}}, nil
}

// ParsePrePing parses and verifies the arguments for the pre-ping command.
func ParsePrePing(args []string) (PrePingConfig, error) {
	fs := flag.NewFlagSet("port-scan pre-ping", flag.ContinueOnError)
	common := commonCLIValues{}
	values := PrePingValues{}
	expansionFlags := targetExpansionFlagValues{}
	registerCommonFlags(fs, &common)
	registerTargetExpansionFlags(fs, &expansionFlags)
	fs.IntVar(&values.Workers, "workers", 10, fmt.Sprintf("worker count (1-%d)", MaxWorkers))
	fs.IntVar(&values.ProgressInterval, "progress-interval", defaultProgressInterval, "progress line cadence (count of processed units)")
	fs.StringVar(&values.Output, "output", "scan_results.csv", "output csv")
	fs.DurationVar(&values.PingTimeout, "pre-scan-ping-timeout", 100*time.Millisecond, "pre-scan ping timeout (duration like 100ms or 2s)")

	if err := fs.Parse(args); err != nil {
		return PrePingConfig{}, fmt.Errorf("parse pre-ping flags: %w", err)
	}
	if err := common.validate(); err != nil {
		return PrePingConfig{}, fmt.Errorf("validate pre-ping flags: %w", err)
	}
	expansion, err := resolveTargetExpansionFlags(fs, expansionFlags)
	if err != nil {
		return PrePingConfig{}, err
	}
	values.CIDRFile = common.cidrFile
	values.CIDRIPCol = common.cidrIPCol
	values.CIDRIPCidrCol = common.cidrIPCidrCol
	cfg, err := NewPrePing(values)
	if err != nil {
		return PrePingConfig{}, fmt.Errorf("validate pre-ping arguments: %w", err)
	}
	cfg.state.expansion = expansion
	return cfg, nil
}

// Resolve returns the validated values for the pre-ping workflow.
func (c PrePingConfig) Resolve() (PrePingValues, error) {
	if c.state == nil {
		return PrePingValues{}, ErrUninitializedConfiguration
	}
	return c.state.values, nil
}

// ResolveTargetExpansion returns the verified target expansion values.
func (c PrePingConfig) ResolveTargetExpansion() (TargetExpansionValues, error) {
	if c.state == nil {
		return TargetExpansionValues{}, ErrUninitializedConfiguration
	}
	return c.state.expansion, nil
}

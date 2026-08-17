package config

import (
	"errors"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/input"
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
	resources PrePingResourceLimits
}

// PrePingResourceLimits contains the CIDR limits used by pre-ping.
// The workflow returns an input error when these limits reject its CIDR file.
type PrePingResourceLimits struct {
	CIDR input.CIDRLimits
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
	return PrePingConfig{state: &prePingState{values: values, expansion: defaultTargetExpansionValues(), resources: PrePingResourceLimits{CIDR: input.DefaultCIDRLimits("")}}}, nil
}

// NewPrePingWithResourceLimits verifies values and returns a configuration with explicit CIDR limits.
// It returns the same validation errors as NewPrePing. It does not open files or run ping work.
func NewPrePingWithResourceLimits(values PrePingValues, limits PrePingResourceLimits) (PrePingConfig, error) {
	cfg, err := NewPrePing(values)
	if err != nil {
		return PrePingConfig{}, err
	}
	cfg.state.resources = limits
	return cfg, nil
}

// ParsePrePing parses and verifies the arguments for the pre-ping command.
func ParsePrePing(args []string) (PrePingConfig, error) {
	fs := flag.NewFlagSet("port-scan pre-ping", flag.ContinueOnError)
	common := commonCLIValues{}
	values := PrePingValues{}
	expansionFlags := targetExpansionFlagValues{}
	cidrLimitFlags := defaultCIDRLimitFlags()
	registerCommonFlags(fs, &common)
	registerTargetExpansionFlags(fs, &expansionFlags)
	registerCIDRLimitFlags(fs, &cidrLimitFlags)
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
	cidrLimits, err := cidrLimitFlags.resolve()
	if err != nil {
		return PrePingConfig{}, fmt.Errorf("validate resource limits: %w", err)
	}
	values.CIDRFile = common.cidrFile
	values.CIDRIPCol = common.cidrIPCol
	values.CIDRIPCidrCol = common.cidrIPCidrCol
	cfg, err := NewPrePing(values)
	if err != nil {
		return PrePingConfig{}, fmt.Errorf("validate pre-ping arguments: %w", err)
	}
	cfg.state.expansion = expansion
	cfg.state.resources = PrePingResourceLimits{CIDR: cidrLimits}
	return cfg, nil
}

// ResolveResourceLimits returns the verified CIDR limits.
func (c PrePingConfig) ResolveResourceLimits() (PrePingResourceLimits, error) {
	if c.state == nil {
		return PrePingResourceLimits{}, ErrUninitializedConfiguration
	}
	return c.state.resources, nil
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

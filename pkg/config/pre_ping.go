package config

import (
	"errors"
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
	values PrePingValues
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
		return PrePingConfig{}, err
	}
	if values.PingTimeout <= 0 {
		return PrePingConfig{}, errors.New("-pre-scan-ping-timeout must be > 0")
	}
	return PrePingConfig{state: &prePingState{values: values}}, nil
}

// ParsePrePing parses and verifies the arguments for the pre-ping command.
func ParsePrePing(args []string) (PrePingConfig, error) {
	cfg, err := ParseFor("pre-ping", args)
	if err != nil {
		return PrePingConfig{}, err
	}

	return NewPrePing(PrePingValues{
		CIDRFile:         cfg.CIDRFile,
		CIDRIPCol:        cfg.CIDRIPCol,
		CIDRIPCidrCol:    cfg.CIDRIPCidrCol,
		Output:           cfg.Output,
		Workers:          cfg.Workers,
		PingTimeout:      cfg.PreScanPingTimeout,
		ProgressInterval: cfg.ProgressInterval,
	})
}

// Resolve returns the validated values for the pre-ping workflow.
func (c PrePingConfig) Resolve() (PrePingValues, error) {
	if c.state == nil {
		return PrePingValues{}, ErrUninitializedConfiguration
	}
	return c.state.values, nil
}

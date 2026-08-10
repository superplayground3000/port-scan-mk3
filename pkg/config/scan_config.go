package config

import (
	"errors"
	"strings"
	"time"
)

// PressureKind identifies one validated pressure policy variant.
type PressureKind uint8

const (
	PressureKindDisabled PressureKind = iota + 1
	PressureKindSimple
	PressureKindAuthenticated
)

// PressureValues contains the values for one pressure adapter.
type PressureValues struct {
	Kind          PressureKind
	Endpoint      string
	AuthEndpoint  string
	DataEndpoints []string
	ClientID      string
	ClientSecret  string
	Interval      time.Duration
}

type pressurePolicyState struct {
	values PressureValues
}

// PressurePolicy is an opaque validated pressure policy.
type PressurePolicy struct {
	state *pressurePolicyState
}

// PressureDisabled returns a policy that disables pressure polling.
func PressureDisabled() PressurePolicy {
	return PressurePolicy{state: &pressurePolicyState{values: PressureValues{
		Kind: PressureKindDisabled,
	}}}
}

// SimplePressure returns a policy for one unauthenticated endpoint.
// It returns an error for a non-positive interval.
func SimplePressure(endpoint string, interval time.Duration) (PressurePolicy, error) {
	if interval <= 0 {
		return PressurePolicy{}, errors.New("-pressure-interval must be > 0")
	}
	return PressurePolicy{state: &pressurePolicyState{values: PressureValues{
		Kind:     PressureKindSimple,
		Endpoint: endpoint,
		Interval: interval,
	}}}, nil
}

// AuthenticatedPressure returns a policy for OAuth pressure endpoints.
// It returns an error for missing values or a non-positive interval.
func AuthenticatedPressure(authURL string, dataURLs []string, clientID, clientSecret string, interval time.Duration) (PressurePolicy, error) {
	if authURL == "" {
		return PressurePolicy{}, errors.New("-pressure-auth-url is required when -pressure-use-auth is set")
	}
	if len(dataURLs) == 0 {
		return PressurePolicy{}, errors.New("-pressure-data-url is required when -pressure-use-auth is set")
	}
	for _, endpoint := range dataURLs {
		if strings.TrimSpace(endpoint) == "" {
			return PressurePolicy{}, errors.New("-pressure-data-url contains an empty value")
		}
	}
	if clientID == "" {
		return PressurePolicy{}, errors.New("-pressure-client-id is required when -pressure-use-auth is set")
	}
	if clientSecret == "" {
		return PressurePolicy{}, errors.New("-pressure-client-secret is required when -pressure-use-auth is set")
	}
	if interval <= 0 {
		return PressurePolicy{}, errors.New("-pressure-interval must be > 0")
	}
	return PressurePolicy{state: &pressurePolicyState{values: PressureValues{
		Kind:          PressureKindAuthenticated,
		AuthEndpoint:  authURL,
		DataEndpoints: append([]string(nil), dataURLs...),
		ClientID:      clientID,
		ClientSecret:  clientSecret,
		Interval:      interval,
	}}}, nil
}

// Resolve returns the validated pressure values.
// It returns ErrUninitializedConfiguration for a zero PressurePolicy.
func (p PressurePolicy) Resolve() (PressureValues, error) {
	if p.state == nil {
		return PressureValues{}, ErrUninitializedConfiguration
	}
	values := p.state.values
	values.DataEndpoints = append([]string(nil), values.DataEndpoints...)
	return values, nil
}

// ScanValues contains the validated values for the scan workflow.
type ScanValues struct {
	CIDRFile       string
	CIDRIPCol      string
	CIDRIPCidrCol  string
	PortFile       string
	ResumeInput    string
	Output         string
	Workers        int
	DialTimeout    time.Duration
	DispatchDelay  time.Duration
	BucketRate     int
	BucketCapacity int
	LogLevel       string
	Format         string
	Quiet          bool
	Pressure       PressurePolicy
}

type scanState struct {
	values ScanValues
}

// ScanConfig is an opaque configuration for the scan workflow.
type ScanConfig struct {
	state *scanState
}

// NewScan verifies the values and returns a scan configuration.
// It returns an error when a required value or pressure policy is invalid.
func NewScan(values ScanValues) (ScanConfig, error) {
	if values.CIDRFile == "" {
		return ScanConfig{}, errors.New("-cidr-file is required")
	}
	if strings.TrimSpace(values.CIDRIPCol) == "" || strings.TrimSpace(values.CIDRIPCidrCol) == "" {
		return ScanConfig{}, errors.New("-cidr-ip-col and -cidr-ip-cidr-col must be non-empty")
	}
	if values.ResumeInput == "" {
		return ScanConfig{}, errors.New("-resume is required")
	}
	if err := validateWorkers(values.Workers); err != nil {
		return ScanConfig{}, err
	}
	if err := validateBucketBounds(values.BucketRate, values.BucketCapacity); err != nil {
		return ScanConfig{}, err
	}
	if values.Format != "human" && values.Format != "json" {
		return ScanConfig{}, errors.New("-format must be human or json")
	}
	if _, err := values.Pressure.Resolve(); err != nil {
		return ScanConfig{}, err
	}
	return ScanConfig{state: &scanState{values: values}}, nil
}

// ParseScan parses and verifies the arguments for the scan command.
// It returns an error for an invalid flag or value.
func ParseScan(args []string) (ScanConfig, error) {
	cfg, err := ParseFor("scan", args)
	if err != nil {
		return ScanConfig{}, err
	}
	var policy PressurePolicy
	switch {
	case cfg.DisableAPI:
		policy = PressureDisabled()
	case cfg.PressureUseAuth:
		policy, err = AuthenticatedPressure(
			cfg.PressureAuthURL,
			cfg.PressureDataURLs,
			cfg.PressureClientID,
			cfg.PressureClientSecret,
			cfg.PressureInterval,
		)
	default:
		policy, err = SimplePressure(cfg.PressureAPI, cfg.PressureInterval)
	}
	if err != nil {
		return ScanConfig{}, err
	}
	return NewScan(ScanValues{
		CIDRFile:       cfg.CIDRFile,
		CIDRIPCol:      cfg.CIDRIPCol,
		CIDRIPCidrCol:  cfg.CIDRIPCidrCol,
		PortFile:       cfg.PortFile,
		ResumeInput:    cfg.Resume,
		Output:         cfg.Output,
		Workers:        cfg.Workers,
		DialTimeout:    cfg.Timeout,
		DispatchDelay:  cfg.Delay,
		BucketRate:     cfg.BucketRate,
		BucketCapacity: cfg.BucketCapacity,
		LogLevel:       cfg.LogLevel,
		Format:         cfg.Format,
		Quiet:          cfg.Quiet,
		Pressure:       policy,
	})
}

// Resolve returns the validated values for the scan workflow.
// It returns ErrUninitializedConfiguration for a zero ScanConfig.
func (c ScanConfig) Resolve() (ScanValues, error) {
	if c.state == nil {
		return ScanValues{}, ErrUninitializedConfiguration
	}
	return c.state.values, nil
}

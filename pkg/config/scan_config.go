package config

import (
	"errors"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/ratelimit"
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
	CIDRFile           string
	CIDRIPCol          string
	CIDRIPCidrCol      string
	PortFile           string
	ResumeInput        string
	Output             string
	OutputFlushResults int
	Workers            int
	DialTimeout        time.Duration
	DispatchDelay      time.Duration
	BucketRate         int
	BucketCapacity     int
	LogLevel           string
	Format             string
	Quiet              bool
	Pressure           PressurePolicy
}

type scanState struct {
	values    ScanValues
	expansion TargetExpansionValues
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
	if values.OutputFlushResults < 0 {
		return ScanConfig{}, errors.New("-output-flush-results must be >= 0")
	}
	if err := validateWorkers(values.Workers); err != nil {
		return ScanConfig{}, fmt.Errorf("validate workers: %w", err)
	}
	if err := validateBucketBounds(values.BucketRate, values.BucketCapacity); err != nil {
		return ScanConfig{}, fmt.Errorf("validate bucket bounds: %w", err)
	}
	if values.Format != "human" && values.Format != "json" {
		return ScanConfig{}, errors.New("-format must be human or json")
	}
	if _, err := values.Pressure.Resolve(); err != nil {
		return ScanConfig{}, fmt.Errorf("resolve pressure policy: %w", err)
	}
	return ScanConfig{state: &scanState{values: values, expansion: defaultTargetExpansionValues()}}, nil
}

// ParseScan parses and verifies the arguments for the scan command.
// It returns an error for an invalid flag or value.
func ParseScan(args []string) (ScanConfig, error) {
	fs := flag.NewFlagSet("port-scan scan", flag.ContinueOnError)
	common := commonCLIValues{}
	values := ScanValues{}
	expansionFlags := targetExpansionFlagValues{}
	var (
		pressureAPI          string
		pressureIntervalRaw  string
		disableAPI           bool
		pressureAuthURL      string
		pressureDataURLRaw   string
		pressureClientID     string
		pressureClientSecret string
		pressureUseAuth      bool
	)
	registerCommonFlags(fs, &common)
	registerTargetExpansionFlags(fs, &expansionFlags)
	fs.IntVar(&values.Workers, "workers", 10, fmt.Sprintf("worker count (1-%d)", MaxWorkers))
	fs.Int("progress-interval", defaultProgressInterval, "progress line cadence (count of processed units)")
	fs.StringVar(&values.PortFile, "port-file", "", "Port CSV path (optional fallback; chunks carry ports)")
	fs.StringVar(&values.Output, "output", "scan_results.csv", "output csv")
	fs.IntVar(&values.OutputFlushResults, "output-flush-results", 1000, "result count between output flushes (0 disables periodic flushes)")
	fs.StringVar(&values.ResumeInput, "resume", "", "resume/bucket snapshot file (required)")
	fs.DurationVar(&values.DialTimeout, "timeout", 100*time.Millisecond, "dial timeout")
	fs.DurationVar(&values.DispatchDelay, "delay", 10*time.Millisecond, "dispatch delay")
	fs.IntVar(&values.BucketRate, "bucket-rate", 100, fmt.Sprintf("bucket rate (1-%d)", ratelimit.MaxRate))
	fs.IntVar(&values.BucketCapacity, "bucket-capacity", 100, fmt.Sprintf("bucket capacity (1-%d)", ratelimit.MaxCapacity))
	fs.StringVar(&pressureAPI, "pressure-api", "http://localhost:8080/api/pressure", "pressure api")
	fs.StringVar(&pressureIntervalRaw, "pressure-interval", "5s", "pressure poll interval (duration or seconds)")
	fs.BoolVar(&disableAPI, "disable-api", false, "disable pressure api")
	fs.StringVar(&pressureAuthURL, "pressure-auth-url", "", "pressure auth endpoint URL")
	fs.StringVar(&pressureDataURLRaw, "pressure-data-url", "", "pressure data endpoint URLs (comma-separated)")
	fs.StringVar(&pressureClientID, "pressure-client-id", "", "pressure API client ID")
	fs.StringVar(&pressureClientSecret, "pressure-client-secret", "", "pressure API client secret")
	fs.BoolVar(&pressureUseAuth, "pressure-use-auth", false, "use authenticated pressure fetcher")

	if err := fs.Parse(args); err != nil {
		return ScanConfig{}, fmt.Errorf("parse scan flags: %w", err)
	}
	if err := common.validate(); err != nil {
		return ScanConfig{}, fmt.Errorf("validate scan flags: %w", err)
	}
	expansion, err := resolveTargetExpansionFlags(fs, expansionFlags)
	if err != nil {
		return ScanConfig{}, err
	}
	interval, err := parsePressureInterval(pressureIntervalRaw)
	if err != nil {
		return ScanConfig{}, fmt.Errorf("parse scan pressure interval: %w", err)
	}
	dataURLs, err := parsePressureDataURLs(pressureDataURLRaw)
	if err != nil {
		return ScanConfig{}, fmt.Errorf("parse scan pressure data URLs: %w", err)
	}
	if interval <= 0 {
		return ScanConfig{}, errors.New("-pressure-interval must be > 0")
	}
	if pressureUseAuth {
		if pressureAuthURL == "" {
			return ScanConfig{}, errors.New("-pressure-auth-url is required when -pressure-use-auth is set")
		}
		if len(dataURLs) == 0 {
			return ScanConfig{}, errors.New("-pressure-data-url is required when -pressure-use-auth is set")
		}
		if pressureClientID == "" {
			return ScanConfig{}, errors.New("-pressure-client-id is required when -pressure-use-auth is set")
		}
		if pressureClientSecret == "" {
			return ScanConfig{}, errors.New("-pressure-client-secret is required when -pressure-use-auth is set")
		}
	}

	var policy PressurePolicy
	switch {
	case disableAPI:
		policy = PressureDisabled()
	case pressureUseAuth:
		policy, err = AuthenticatedPressure(
			pressureAuthURL,
			dataURLs,
			pressureClientID,
			pressureClientSecret,
			interval,
		)
	default:
		policy, err = SimplePressure(pressureAPI, interval)
	}
	if err != nil {
		return ScanConfig{}, fmt.Errorf("construct scan pressure policy: %w", err)
	}

	values.CIDRFile = common.cidrFile
	values.CIDRIPCol = common.cidrIPCol
	values.CIDRIPCidrCol = common.cidrIPCidrCol
	values.LogLevel = common.logLevel
	values.Format = common.format
	values.Quiet = common.quiet
	values.Pressure = policy
	cfg, err := NewScan(values)
	if err != nil {
		return ScanConfig{}, fmt.Errorf("validate scan arguments: %w", err)
	}
	cfg.state.expansion = expansion
	return cfg, nil
}

// Resolve returns the validated values for the scan workflow.
// It returns ErrUninitializedConfiguration for a zero ScanConfig.
func (c ScanConfig) Resolve() (ScanValues, error) {
	if c.state == nil {
		return ScanValues{}, ErrUninitializedConfiguration
	}
	return c.state.values, nil
}

// ResolveTargetExpansion returns the verified target expansion values.
func (c ScanConfig) ResolveTargetExpansion() (TargetExpansionValues, error) {
	if c.state == nil {
		return TargetExpansionValues{}, ErrUninitializedConfiguration
	}
	return c.state.expansion, nil
}

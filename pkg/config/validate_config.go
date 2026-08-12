package config

import (
	"errors"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/ratelimit"
)

// ValidateValues contains input values for the validate workflow.
type ValidateValues struct {
	// CIDRFile is the path of the required CIDR or rich CSV file.
	CIDRFile string
	// CIDRIPCol is the name of the IP selector column.
	CIDRIPCol string
	// CIDRIPCidrCol is the name of the boundary CIDR column.
	CIDRIPCidrCol string
	// PortFile is the optional path of the port file.
	// Basic input requires this value after the workflow reads the CIDR file.
	PortFile string
	// Format is the output format. Its value is human or json.
	Format string
}

type validateState struct {
	values    ValidateValues
	expansion TargetExpansionValues
}

type validateCompatibilityValues struct {
	workers              int
	bucketRate           int
	bucketCapacity       int
	pressureIntervalRaw  string
	pressureDataURLRaw   string
	pressureAuthURL      string
	pressureClientID     string
	pressureClientSecret string
	pressureUseAuth      bool
	preScanPingTimeout   time.Duration
}

// ValidateConfig is an opaque configuration for the validate workflow.
type ValidateConfig struct {
	state *validateState
}

// NewValidate verifies values and returns an opaque validate configuration.
// It returns an error for a missing CIDR path, an empty column name, or an
// unsupported output format. An empty port path is valid for rich input.
func NewValidate(values ValidateValues) (ValidateConfig, error) {
	if values.CIDRFile == "" {
		return ValidateConfig{}, errors.New("-cidr-file is required")
	}
	if strings.TrimSpace(values.CIDRIPCol) == "" || strings.TrimSpace(values.CIDRIPCidrCol) == "" {
		return ValidateConfig{}, errors.New("-cidr-ip-col and -cidr-ip-cidr-col must be non-empty")
	}
	if values.Format != "human" && values.Format != "json" {
		return ValidateConfig{}, errors.New("-format must be human or json")
	}
	return ValidateConfig{state: &validateState{values: values, expansion: defaultTargetExpansionValues()}}, nil
}

// ParseValidate parses arguments and returns an opaque validate configuration.
// It accepts the complete legacy flag surface for CLI compatibility. It
// returns an error for an invalid flag, a missing value, or an invalid legacy
// compatibility value.
func ParseValidate(args []string) (ValidateConfig, error) {
	fs := flag.NewFlagSet("port-scan", flag.ContinueOnError)
	common := commonCLIValues{}
	values := ValidateValues{}
	compatibility := validateCompatibilityValues{}
	expansionFlags := targetExpansionFlagValues{}
	registerCommonFlags(fs, &common)
	registerTargetExpansionFlags(fs, &expansionFlags)
	fs.IntVar(&compatibility.workers, "workers", 10, fmt.Sprintf("worker count (1-%d)", MaxWorkers))
	fs.IntVar(&compatibility.bucketRate, "bucket-rate", 100, fmt.Sprintf("bucket rate (1-%d)", ratelimit.MaxRate))
	fs.IntVar(&compatibility.bucketCapacity, "bucket-capacity", 100, fmt.Sprintf("bucket capacity (1-%d)", ratelimit.MaxCapacity))
	fs.StringVar(&compatibility.pressureIntervalRaw, "pressure-interval", "5s", "pressure poll interval (duration or seconds)")
	fs.StringVar(&compatibility.pressureDataURLRaw, "pressure-data-url", "", "pressure data endpoint URLs (comma-separated)")
	fs.StringVar(&compatibility.pressureAuthURL, "pressure-auth-url", "", "pressure auth endpoint URL")
	fs.StringVar(&compatibility.pressureClientID, "pressure-client-id", "", "pressure API client ID")
	fs.StringVar(&compatibility.pressureClientSecret, "pressure-client-secret", "", "pressure API client secret")
	fs.BoolVar(&compatibility.pressureUseAuth, "pressure-use-auth", false, "use authenticated pressure fetcher")
	fs.DurationVar(&compatibility.preScanPingTimeout, "pre-scan-ping-timeout", 100*time.Millisecond, "pre-scan ping timeout")

	fs.StringVar(&values.PortFile, "port-file", "", "Port CSV path")

	// Validate keeps these flags for CLI compatibility. The workflow does not
	// use their values.
	fs.String("output", "scan_results.csv", "output csv")
	fs.Duration("timeout", 100*time.Millisecond, "dial timeout")
	fs.Duration("delay", 10*time.Millisecond, "dispatch delay")
	fs.String("pressure-api", "http://localhost:8080/api/pressure", "pressure api")
	fs.Bool("disable-api", false, "disable pressure api")
	fs.Bool("disable-pre-scan-ping", false, "disable pre-scan ping")
	fs.String("resume", "", "resume state file")

	if err := fs.Parse(args); err != nil {
		return ValidateConfig{}, fmt.Errorf("parse validate flags: %w", err)
	}
	if err := common.validate(); err != nil {
		return ValidateConfig{}, fmt.Errorf("validate common flags: %w", err)
	}
	expansion, err := resolveTargetExpansionFlags(fs, expansionFlags)
	if err != nil {
		return ValidateConfig{}, err
	}
	if err := compatibility.validate(); err != nil {
		return ValidateConfig{}, fmt.Errorf("validate compatibility flags: %w", err)
	}
	values.CIDRFile = common.cidrFile
	values.CIDRIPCol = common.cidrIPCol
	values.CIDRIPCidrCol = common.cidrIPCidrCol
	values.Format = common.format
	cfg, err := NewValidate(values)
	if err != nil {
		return ValidateConfig{}, fmt.Errorf("validate arguments: %w", err)
	}
	cfg.state.expansion = expansion
	return cfg, nil
}

func (v validateCompatibilityValues) validate() error {
	if err := validateWorkers(v.workers); err != nil {
		return fmt.Errorf("validate workers: %w", err)
	}
	if err := validateBucketBounds(v.bucketRate, v.bucketCapacity); err != nil {
		return fmt.Errorf("validate bucket bounds: %w", err)
	}
	if v.preScanPingTimeout <= 0 {
		return errors.New("-pre-scan-ping-timeout must be > 0")
	}

	pressureInterval, err := parsePressureInterval(v.pressureIntervalRaw)
	if err != nil {
		return fmt.Errorf("parse pressure interval: %w", err)
	}
	if pressureInterval <= 0 {
		return errors.New("-pressure-interval must be > 0")
	}

	dataURLs, err := parsePressureDataURLs(v.pressureDataURLRaw)
	if err != nil {
		return fmt.Errorf("parse pressure data URLs: %w", err)
	}
	if !v.pressureUseAuth {
		return nil
	}
	if v.pressureAuthURL == "" {
		return errors.New("-pressure-auth-url is required when -pressure-use-auth is set")
	}
	if len(dataURLs) == 0 {
		return errors.New("-pressure-data-url is required when -pressure-use-auth is set")
	}
	if v.pressureClientID == "" {
		return errors.New("-pressure-client-id is required when -pressure-use-auth is set")
	}
	if v.pressureClientSecret == "" {
		return errors.New("-pressure-client-secret is required when -pressure-use-auth is set")
	}
	return nil
}

// Resolve returns the verified values for the validate workflow.
// It returns ErrUninitializedConfiguration for a zero ValidateConfig.
func (c ValidateConfig) Resolve() (ValidateValues, error) {
	if c.state == nil {
		return ValidateValues{}, ErrUninitializedConfiguration
	}
	return c.state.values, nil
}

// ResolveTargetExpansion returns the verified target expansion values.
func (c ValidateConfig) ResolveTargetExpansion() (TargetExpansionValues, error) {
	if c.state == nil {
		return TargetExpansionValues{}, ErrUninitializedConfiguration
	}
	return c.state.expansion, nil
}

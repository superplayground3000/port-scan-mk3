// Package config parses CLI arguments and holds configuration for the port scanner.
//
// Command-line flags supply all configuration. The Parse function documents each
// flag and its default value.
//
// # Example
//
//	cfg, err := config.Parse(os.Args[1:])
//	if err != nil {
//	    log.Fatalf("config error: %v", err)
//	}
package config

import (
	"errors"
	"flag"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/ratelimit"
)

// Config holds all CLI configuration for the port scanner. An optional field
// accepts the zero value. Parse enforces the required fields.
type Config struct {
	// CIDRFile is the path to the CIDR input CSV (required).
	CIDRFile string
	// PortFile is the path to the port input file (required in basic mode).
	PortFile string
	// Output is the path for the main scan results CSV.
	Output string
	// Timeout is the per-scan TCP connection timeout.
	Timeout time.Duration
	// Delay is the pause between two consecutive tasks that the scanner
	// dispatches.
	Delay time.Duration
	// BucketRate is the leaky-bucket token refill rate (tokens per second).
	BucketRate int
	// BucketCapacity is the maximum burst size for the leaky bucket.
	BucketCapacity int
	// Workers is the number of concurrent scan goroutines.
	Workers int
	// PressureAPI is the URL for the pressure API endpoint.
	PressureAPI string
	// PressureInterval is the time between two polls of the pressure API.
	PressureInterval time.Duration
	// DisableAPI disables pressure-based throttling when it is true.
	DisableAPI bool
	// PressureAuthURL is the OAuth token endpoint URL.
	PressureAuthURL string
	// PressureDataURLs are the URLs of the authenticated endpoints for pressure
	// data. The input flag takes them as one comma-separated value.
	PressureDataURLs []string
	// PressureClientID is the OAuth client ID for the authenticated pressure
	// fetcher.
	PressureClientID string
	// PressureClientSecret is the OAuth client secret.
	PressureClientSecret string
	// PressureUseAuth enables the OAuth-authenticated pressure fetcher.
	PressureUseAuth bool
	// DisablePreScanPing disables the ping reachability filter of the pre-scan
	// step when it is true.
	DisablePreScanPing bool
	// PreScanPingTimeout is the limit for each ping reachability check in the
	// pre-scan step.
	PreScanPingTimeout time.Duration
	// Resume is the path to the resume state file.
	Resume string
	// LogLevel is the log verbosity: debug, info, or error.
	LogLevel string
	// Format is the output format: human or json.
	Format string
	// Quiet suppresses console logs but keeps pressure API logs.
	Quiet bool
	// CIDRIPCol is the column name for the IP selector in the CIDR CSV.
	CIDRIPCol string
	// CIDRIPCidrCol is the column name for the boundary CIDR in the CIDR CSV.
	CIDRIPCidrCol string
	// BucketsOut is the output path for the generated bucket snapshot. The
	// generate-buckets command requires this value.
	BucketsOut string
	// UnreachableFile is an optional blocklist CSV. The generate-buckets command
	// reads it and subtracts the unreachable IPs from the target set.
	UnreachableFile string
	// ProgressInterval is the count of processed units between two progress lines
	// on stderr. It matches the progressStep semantics of the scanner. The
	// default value is 100.
	ProgressInterval int
}

// defaultProgressInterval is the count-based progress cadence carried from the
// scanner's progressStep fallback (pkg/scanapp/scan.go). Kept in one place so
// preping, generate-buckets, and scan share the same default.
const defaultProgressInterval = 100

// Parse processes CLI arguments and returns a validated Config. A non-nil error
// means that the caller must exit and must not run the scanner. For example, the
// caller requested -help, or a required flag was missing.
//
// # Parameters
//
//	args: CLI arguments, usually os.Args[1:].
//
// # Returns
//
//	A validated Config on success. An error if the flag parsing fails, or if a
//	required flag is missing.
//
// # Required Flags
//
//	-cidr-file: Path to the CIDR input CSV.
//
// # Format Values
//
//	"human" (default): Human-readable console output.
//	"json": Structured JSON output.
//
// # Example
//
//	cfg, err := config.Parse(os.Args[1:])
//	if errors.Is(err, flag.ErrHelp) {
//	    return
//	}
//	if err != nil {
//	    log.Fatal(err)
//	}
func Parse(args []string) (Config, error) {
	fs := flag.NewFlagSet("port-scan", flag.ContinueOnError)
	cfg := Config{}
	var (
		pressureIntervalRaw string
		pressureDataURLRaw  string
	)

	fs.StringVar(&cfg.CIDRFile, "cidr-file", "", "CIDR CSV path")
	fs.StringVar(&cfg.PortFile, "port-file", "", "Port CSV path")
	fs.StringVar(&cfg.Output, "output", "scan_results.csv", "output csv")
	fs.DurationVar(&cfg.Timeout, "timeout", 100*time.Millisecond, "dial timeout")
	fs.DurationVar(&cfg.Delay, "delay", 10*time.Millisecond, "dispatch delay")
	fs.IntVar(&cfg.BucketRate, "bucket-rate", 100, fmt.Sprintf("bucket rate (1-%d)", ratelimit.MaxRate))
	fs.IntVar(&cfg.BucketCapacity, "bucket-capacity", 100, fmt.Sprintf("bucket capacity (1-%d)", ratelimit.MaxCapacity))
	fs.IntVar(&cfg.Workers, "workers", 10, fmt.Sprintf("worker count (1-%d)", MaxWorkers))
	fs.StringVar(&cfg.PressureAPI, "pressure-api", "http://localhost:8080/api/pressure", "pressure api")
	fs.StringVar(&pressureIntervalRaw, "pressure-interval", "5s", "pressure poll interval (duration or seconds)")
	fs.BoolVar(&cfg.DisableAPI, "disable-api", false, "disable pressure api")
	fs.StringVar(&cfg.PressureAuthURL, "pressure-auth-url", "", "pressure auth endpoint URL")
	fs.StringVar(&pressureDataURLRaw, "pressure-data-url", "", "pressure data endpoint URLs (comma-separated)")
	fs.StringVar(&cfg.PressureClientID, "pressure-client-id", "", "pressure API client ID")
	fs.StringVar(&cfg.PressureClientSecret, "pressure-client-secret", "", "pressure API client secret")
	fs.BoolVar(&cfg.PressureUseAuth, "pressure-use-auth", false, "use authenticated pressure fetcher")
	fs.BoolVar(&cfg.DisablePreScanPing, "disable-pre-scan-ping", false, "disable pre-scan ping")
	fs.DurationVar(&cfg.PreScanPingTimeout, "pre-scan-ping-timeout", 100*time.Millisecond, "pre-scan ping timeout (duration like 100ms or 2s)")
	fs.StringVar(&cfg.Resume, "resume", "", "resume state file")
	fs.StringVar(&cfg.LogLevel, "log-level", "info", "debug|info|error")
	fs.StringVar(&cfg.Format, "format", "human", "human|json")
	fs.BoolVar(&cfg.Quiet, "quiet", false, "suppress console logs, keep pressure API logs")
	fs.StringVar(&cfg.CIDRIPCol, "cidr-ip-col", "ip", "cidr csv ip column name")
	fs.StringVar(&cfg.CIDRIPCidrCol, "cidr-ip-cidr-col", "ip_cidr", "cidr csv ip_cidr column name")

	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	if pressureDataURLRaw != "" {
		for _, u := range strings.Split(pressureDataURLRaw, ",") {
			if trimmed := strings.TrimSpace(u); trimmed != "" {
				cfg.PressureDataURLs = append(cfg.PressureDataURLs, trimmed)
			}
		}
		if len(cfg.PressureDataURLs) == 0 {
			return Config{}, errors.New("-pressure-data-url contains only empty values after trimming")
		}
	}
	if cfg.CIDRFile == "" {
		return Config{}, errors.New("-cidr-file is required")
	}
	if strings.TrimSpace(cfg.CIDRIPCol) == "" || strings.TrimSpace(cfg.CIDRIPCidrCol) == "" {
		return Config{}, errors.New("-cidr-ip-col and -cidr-ip-cidr-col must be non-empty")
	}
	if cfg.Format != "human" && cfg.Format != "json" {
		return Config{}, errors.New("-format must be human or json")
	}
	if seconds, err := strconv.Atoi(pressureIntervalRaw); err == nil {
		cfg.PressureInterval = time.Duration(seconds) * time.Second
	} else {
		interval, parseErr := time.ParseDuration(pressureIntervalRaw)
		if parseErr != nil {
			return Config{}, errors.New("-pressure-interval must be duration like 5s or integer seconds")
		}
		cfg.PressureInterval = interval
	}
	if cfg.PressureInterval <= 0 {
		return Config{}, errors.New("-pressure-interval must be > 0")
	}
	if cfg.PreScanPingTimeout <= 0 {
		return Config{}, errors.New("-pre-scan-ping-timeout must be > 0")
	}
	// This legacy surface registers the worker and bucket flags too, so it
	// enforces the same ranges as ParseFor rather than leaving a second door in.
	if err := validateWorkers(cfg.Workers); err != nil {
		return Config{}, err
	}
	if err := validateBucketBounds(cfg.BucketRate, cfg.BucketCapacity); err != nil {
		return Config{}, err
	}
	if cfg.PressureUseAuth {
		if cfg.PressureAuthURL == "" {
			return Config{}, errors.New("-pressure-auth-url is required when -pressure-use-auth is set")
		}
		if len(cfg.PressureDataURLs) == 0 {
			return Config{}, errors.New("-pressure-data-url is required when -pressure-use-auth is set")
		}
		if cfg.PressureClientID == "" {
			return Config{}, errors.New("-pressure-client-id is required when -pressure-use-auth is set")
		}
		if cfg.PressureClientSecret == "" {
			return Config{}, errors.New("-pressure-client-secret is required when -pressure-use-auth is set")
		}
	}

	return cfg, nil
}

// Package config parses CLI arguments and holds configuration for the port scanner.
//
// All configuration is provided via command-line flags. Flags and their defaults
// are documented in the Parse function.
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
	"strconv"
	"strings"
	"time"
)

// Config holds all CLI configuration for the port scanner. Zero values are
// valid for optional fields; required fields are enforced by Parse.
type Config struct {
	// CIDRFile is the path to the CIDR input CSV (required).
	CIDRFile string
	// PortFile is the path to the port input file (required in basic mode).
	PortFile string
	// Output is the path for the main scan results CSV.
	Output string
	// Timeout is the per-scan TCP connection timeout.
	Timeout time.Duration
	// Delay is the pause between dispatching consecutive tasks.
	Delay time.Duration
	// BucketRate is the leaky-bucket token refill rate (tokens per second).
	BucketRate int
	// BucketCapacity is the maximum burst size for the leaky bucket.
	BucketCapacity int
	// Workers is the number of concurrent scan goroutines.
	Workers int
	// PressureAPI is the URL for the pressure API endpoint.
	PressureAPI string
	// PressureInterval is how often to poll the pressure API.
	PressureInterval time.Duration
	// DisableAPI disables pressure-based throttling when true.
	DisableAPI bool
	// PressureAuthURL is the OAuth token endpoint URL.
	PressureAuthURL string
	// PressureDataURLs are the authenticated pressure data endpoint URLs (comma-separated on input).
	PressureDataURLs []string
	// PressureClientID is the OAuth client ID for authenticated pressure fetching.
	PressureClientID string
	// PressureClientSecret is the OAuth client secret.
	PressureClientSecret string
	// PressureUseAuth enables OAuth-authenticated pressure fetching.
	PressureUseAuth bool
	// DisablePreScanPing disables the pre-scan ping reachability filter when true.
	DisablePreScanPing bool
	// PreScanPingTimeout bounds each pre-scan ping reachability check.
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
	// BucketsOut is the output path for the generated bucket snapshot
	// (generate-buckets command; required there).
	BucketsOut string
	// UnreachableFile is the optional blocklist CSV consumed by generate-buckets
	// to subtract unreachable IPs from the target set.
	UnreachableFile string
	// ProgressInterval is the count-based cadence for stderr progress lines.
	// It matches the scanner's progressStep semantics and defaults to 100.
	ProgressInterval int
}

// defaultProgressInterval is the count-based progress cadence carried from the
// scanner's progressStep fallback (pkg/scanapp/scan.go). Kept in one place so
// preping, generate-buckets, and scan share the same default.
const defaultProgressInterval = 100

// Parse processes CLI arguments and returns a validated Config. It follows the
// convention that a non-nil error means the caller should exit without running
// the scanner (e.g., -help was requested or a required flag was missing).
//
// # Parameters
//
//	args: CLI arguments (typically os.Args[1:]).
//
// # Returns
//
//	A validated Config on success; an error if flag parsing fails or required
//	flags are missing.
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
	fs.IntVar(&cfg.BucketRate, "bucket-rate", 100, "bucket rate")
	fs.IntVar(&cfg.BucketCapacity, "bucket-capacity", 100, "bucket capacity")
	fs.IntVar(&cfg.Workers, "workers", 10, "worker count")
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

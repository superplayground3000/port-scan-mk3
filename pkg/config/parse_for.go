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

// ParseFor processes CLI arguments for one subcommand. It registers only the
// flags that the subcommand owns, and it enforces only the required flags of
// that subcommand.
//
// A per-command flag surface makes the "scan never pings" guarantee structural,
// not cosmetic. The "scan" command does not register -disable-pre-scan-ping or
// -pre-scan-ping-timeout. Therefore either flag causes an unknown-flag error,
// and the program does not ignore it in silence.
//
// # Parameters
//
//	command: one of "pre-ping", "generate-buckets", "scan", or "validate".
//	args:    CLI arguments for the subcommand, without the subcommand token.
//
// # Returns
//
//	A validated Config on success. An error if the command is unknown, if the
//	flag parsing fails, or if a required flag for the command is missing. Flag
//	parsing failures include an unknown flag and -help.
//
// # Required Flags (per command)
//
//	all:              -cidr-file
//	generate-buckets: -buckets-out
//	scan:             -resume
func ParseFor(command string, args []string) (Config, error) {
	fs := flag.NewFlagSet("port-scan "+command, flag.ContinueOnError)
	cfg := Config{}
	var (
		pressureIntervalRaw string
		pressureDataURLRaw  string
	)

	// Flags shared by every subcommand.
	fs.StringVar(&cfg.CIDRFile, "cidr-file", "", "CIDR CSV path")
	fs.StringVar(&cfg.CIDRIPCol, "cidr-ip-col", "ip", "cidr csv ip column name")
	fs.StringVar(&cfg.CIDRIPCidrCol, "cidr-ip-cidr-col", "ip_cidr", "cidr csv ip_cidr column name")
	fs.StringVar(&cfg.LogLevel, "log-level", "info", "debug|info|error")
	fs.StringVar(&cfg.Format, "format", "human", "human|json")
	fs.BoolVar(&cfg.Quiet, "quiet", false, "suppress console logs, keep pressure API logs")

	// Workers and progress cadence are shared by the three pipeline steps.
	registerWorkers := func() {
		fs.IntVar(&cfg.Workers, "workers", 10, fmt.Sprintf("worker count (1-%d)", MaxWorkers))
	}
	registerProgress := func() {
		fs.IntVar(&cfg.ProgressInterval, "progress-interval", defaultProgressInterval,
			"progress line cadence (count of processed units)")
	}

	switch command {
	case "pre-ping":
		registerWorkers()
		registerProgress()
		fs.StringVar(&cfg.Output, "output", "scan_results.csv", "output csv")
		fs.DurationVar(&cfg.PreScanPingTimeout, "pre-scan-ping-timeout", 100*time.Millisecond,
			"pre-scan ping timeout (duration like 100ms or 2s)")
	case "generate-buckets":
		registerWorkers()
		registerProgress()
		fs.StringVar(&cfg.PortFile, "port-file", "", "Port CSV path")
		fs.StringVar(&cfg.UnreachableFile, "unreachable-file", "", "unreachable blocklist CSV path (optional)")
		fs.StringVar(&cfg.BucketsOut, "buckets-out", "", "bucket snapshot output path (required)")
	case "scan":
		registerWorkers()
		registerProgress()
		fs.StringVar(&cfg.PortFile, "port-file", "", "Port CSV path (optional fallback; chunks carry ports)")
		fs.StringVar(&cfg.Output, "output", "scan_results.csv", "output csv")
		fs.StringVar(&cfg.Resume, "resume", "", "resume/bucket snapshot file (required)")
		fs.DurationVar(&cfg.Timeout, "timeout", 100*time.Millisecond, "dial timeout")
		fs.DurationVar(&cfg.Delay, "delay", 10*time.Millisecond, "dispatch delay")
		fs.IntVar(&cfg.BucketRate, "bucket-rate", 100, fmt.Sprintf("bucket rate (1-%d)", ratelimit.MaxRate))
		fs.IntVar(&cfg.BucketCapacity, "bucket-capacity", 100, fmt.Sprintf("bucket capacity (1-%d)", ratelimit.MaxCapacity))
		fs.StringVar(&cfg.PressureAPI, "pressure-api", "http://localhost:8080/api/pressure", "pressure api")
		fs.StringVar(&pressureIntervalRaw, "pressure-interval", "5s", "pressure poll interval (duration or seconds)")
		fs.BoolVar(&cfg.DisableAPI, "disable-api", false, "disable pressure api")
		fs.StringVar(&cfg.PressureAuthURL, "pressure-auth-url", "", "pressure auth endpoint URL")
		fs.StringVar(&pressureDataURLRaw, "pressure-data-url", "", "pressure data endpoint URLs (comma-separated)")
		fs.StringVar(&cfg.PressureClientID, "pressure-client-id", "", "pressure API client ID")
		fs.StringVar(&cfg.PressureClientSecret, "pressure-client-secret", "", "pressure API client secret")
		fs.BoolVar(&cfg.PressureUseAuth, "pressure-use-auth", false, "use authenticated pressure fetcher")
	case "validate":
		// validate keeps a minimal input surface for compatibility with the
		// existing handler; it neither scans nor pings.
		fs.StringVar(&cfg.PortFile, "port-file", "", "Port CSV path")
	default:
		return Config{}, fmt.Errorf("unknown command: %s", command)
	}

	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}

	// Validation shared by every command.
	if cfg.CIDRFile == "" {
		return Config{}, errors.New("-cidr-file is required")
	}
	if strings.TrimSpace(cfg.CIDRIPCol) == "" || strings.TrimSpace(cfg.CIDRIPCidrCol) == "" {
		return Config{}, errors.New("-cidr-ip-col and -cidr-ip-cidr-col must be non-empty")
	}
	if cfg.Format != "human" && cfg.Format != "json" {
		return Config{}, errors.New("-format must be human or json")
	}

	// Command-specific required-flag and value validation.
	switch command {
	case "pre-ping":
		if err := validateWorkers(cfg.Workers); err != nil {
			return Config{}, err
		}
		if cfg.PreScanPingTimeout <= 0 {
			return Config{}, errors.New("-pre-scan-ping-timeout must be > 0")
		}
	case "generate-buckets":
		if err := validateWorkers(cfg.Workers); err != nil {
			return Config{}, err
		}
		if cfg.BucketsOut == "" {
			return Config{}, errors.New("-buckets-out is required")
		}
	case "scan":
		if err := validateWorkers(cfg.Workers); err != nil {
			return Config{}, err
		}
		if err := validateBucketBounds(cfg.BucketRate, cfg.BucketCapacity); err != nil {
			return Config{}, err
		}
		if cfg.Resume == "" {
			return Config{}, errors.New("-resume is required")
		}
		if err := applyPressureConfig(&cfg, pressureIntervalRaw, pressureDataURLRaw); err != nil {
			return Config{}, err
		}
	}

	return cfg, nil
}

// applyPressureConfig parses and validates the pressure-API flags shared only by
// the scan command. It splits the comma-separated data URLs, interprets the
// pressure interval (integer seconds or a duration string), and enforces the
// authenticated-fetcher dependencies.
func applyPressureConfig(cfg *Config, pressureIntervalRaw, pressureDataURLRaw string) error {
	if pressureDataURLRaw != "" {
		for _, u := range strings.Split(pressureDataURLRaw, ",") {
			if trimmed := strings.TrimSpace(u); trimmed != "" {
				cfg.PressureDataURLs = append(cfg.PressureDataURLs, trimmed)
			}
		}
		if len(cfg.PressureDataURLs) == 0 {
			return errors.New("-pressure-data-url contains only empty values after trimming")
		}
	}
	if seconds, err := strconv.Atoi(pressureIntervalRaw); err == nil {
		cfg.PressureInterval = time.Duration(seconds) * time.Second
	} else {
		interval, parseErr := time.ParseDuration(pressureIntervalRaw)
		if parseErr != nil {
			return errors.New("-pressure-interval must be duration like 5s or integer seconds")
		}
		cfg.PressureInterval = interval
	}
	if cfg.PressureInterval <= 0 {
		return errors.New("-pressure-interval must be > 0")
	}
	if cfg.PressureUseAuth {
		if cfg.PressureAuthURL == "" {
			return errors.New("-pressure-auth-url is required when -pressure-use-auth is set")
		}
		if len(cfg.PressureDataURLs) == 0 {
			return errors.New("-pressure-data-url is required when -pressure-use-auth is set")
		}
		if cfg.PressureClientID == "" {
			return errors.New("-pressure-client-id is required when -pressure-use-auth is set")
		}
		if cfg.PressureClientSecret == "" {
			return errors.New("-pressure-client-secret is required when -pressure-use-auth is set")
		}
	}
	return nil
}

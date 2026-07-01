package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/xuxiping/port-scan-mk3/pkg/csvtransform"
)

// TransformConfig holds all CLI configuration for the transform tool.
type TransformConfig struct {
	Input     string // Path to input CSV (required)
	Output    string // Path to output CSV (required)
	SheetName string // Worksheet name (default: all-runs, ignored for CSV)
	HostCol   string // Host column name (default: Host)
	PortCol   string // Port column name (default: Port)
	PassCol   string // Pass/fail column name (default: Pass the test)
}

// runMain is the entry point for testing. It accepts explicit stdout/stderr
// to avoid os.Exit in tests. args is the full argv slice (as returned by
// os.Args), including the program name at index 0; runMain strips it before
// delegating to ParseConfigFromArgs so that flag.FlagSet.Parse sees only the
// actual flag tokens.
func runMain(args []string, stdout io.Writer, stderrOut io.Writer) int {
	flagArgs := args
	if len(flagArgs) > 0 {
		flagArgs = flagArgs[1:]
	}
	cfg, err := ParseConfigFromArgs(flagArgs)
	if err != nil {
		fmt.Fprintf(stderrOut, "config error: %v\n", err)
		if cfgErr, ok := err.(*ConfigError); ok {
			return cfgErr.Code
		}
		return 2
	}
	tcfg := csvtransform.Config{
		Input:     cfg.Input,
		Output:    cfg.Output,
		SheetName: cfg.SheetName,
		HostCol:   cfg.HostCol,
		PortCol:   cfg.PortCol,
		PassCol:   cfg.PassCol,
	}
	if err := csvtransform.Run(tcfg, stderrOut); err != nil {
		fmt.Fprintf(stderrOut, "transform failed: %v\n", err)
		return 1
	}
	return 0
}

func main() { os.Exit(runMain(os.Args, os.Stdout, os.Stderr)) }

// ParseConfig parses CLI flags and environment variables into a TransformConfig.
// It returns ErrMissingRequired if --input or --output are not provided.
// Exit code 2 on missing required flags.
func ParseConfig() (*TransformConfig, error) {
	return ParseConfigFromArgs(os.Args[1:])
}

func ParseConfigFromArgs(args []string) (*TransformConfig, error) {
	// Create a fresh FlagSet so tests can call ParseConfig without flag
	// redefinition panics.
	fs := flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	cfg := &TransformConfig{
		SheetName: "all-runs",
		HostCol:   "Host",
		PortCol:   "Port",
		PassCol:   "Pass the test",
	}

	fs.StringVar(&cfg.Input, "input", envOrDefault("TRANSFORM_INPUT", ""), "path to input CSV file (required)")
	fs.StringVar(&cfg.Output, "output", envOrDefault("TRANSFORM_OUTPUT", ""), "path to output CSV file (required)")
	fs.StringVar(&cfg.SheetName, "sheet", envOrDefault("TRANSFORM_SHEET_NAME", "all-runs"), "worksheet name")
	fs.StringVar(&cfg.HostCol, "host-col", envOrDefault("TRANSFORM_HOST_COL", "Host"), "host column name")
	fs.StringVar(&cfg.PortCol, "port-col", envOrDefault("TRANSFORM_PORT_COL", "Port"), "port column name")
	fs.StringVar(&cfg.PassCol, "pass-col", envOrDefault("TRANSFORM_PASS_COL", "Pass the test"), "pass/fail column name")

	// Suppress default flag usage output on error.
	fs.Usage = func() {}

	fs.Parse(args)

	if cfg.Input == "" {
		return nil, &ConfigError{Msg: "input is required (--input or TRANSFORM_INPUT)", Code: 2}
	}
	if cfg.Output == "" {
		return nil, &ConfigError{Msg: "output is required (--output or TRANSFORM_OUTPUT)", Code: 2}
	}
	return cfg, nil
}

// envOrDefault returns the environment variable value if non-empty, otherwise the default.
func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// ConfigError represents a CLI flag validation error.
type ConfigError struct {
	Msg  string
	Code int
}

func (e *ConfigError) Error() string { return e.Msg }

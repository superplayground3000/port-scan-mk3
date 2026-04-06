package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/xuxiping/port-scan-mk3/pkg/xlsx"
)

// TransformConfig holds all CLI configuration for the transform tool.
type TransformConfig struct {
	Input     string // Path to input xlsx (required)
	Output    string // Path to output CSV (required)
	SheetName string // Worksheet name (default: all-runs)
	HostCol   string // Host column name (default: Host)
	PortCol   string // Port column name (default: Port)
	PassCol   string // Pass/fail column name (default: Pass the test)
}

// CSV header for Rich output format.
const csvHeader = "src_ip,src_network_segment,dst_ip,dst_network_segment,service_label,protocol,port,decision,matched_policy_id,reason"

// Default field values per spec.
const (
	defaultSrcIP           = "10.0.0.1"
	defaultSrcNetwork      = "10.0.0.0/24"
	defaultDstNetwork      = "10.0.0.0/24"
	defaultServiceLabel    = "unknown"
	defaultProtocol        = "tcp"
	defaultDecision        = "accept"
	defaultMatchedPolicyID = "transformed"
	defaultReason          = "MATCH_POLICY_ACCEPT"
)

// runMain is the entry point for testing. It accepts explicit stdout/stderr
// to avoid os.Exit in tests.
func runMain(args []string, stdout io.Writer, stderrOut io.Writer) int {
	cfg, err := ParseConfigFromArgs(args)
	if err != nil {
		fmt.Fprintf(stderrOut, "config error: %v\n", err)
		if cfgErr, ok := err.(*ConfigError); ok {
			return cfgErr.Code
		}
		return 2
	}
	if err := runTransform(cfg); err != nil {
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

	fs.StringVar(&cfg.Input, "input", envOrDefault("TRANSFORM_INPUT", ""), "path to input xlsx file (required)")
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

// runTransform wires together xlsx reading, column indexing, filtering,
// host resolution, port expansion, and CSV output.
func runTransform(cfg *TransformConfig) error {
	reader := xlsx.NewReader(cfg.Input)
	rows, err := reader.OpenSheet(cfg.SheetName)
	if err != nil {
		return fmt.Errorf("failed to open xlsx: %w", err)
	}

	if len(rows) < 2 {
		// No data rows; nothing to do.
		return nil
	}

	// Build column index from header row.
	header := rows[0]
	colIndex := make(map[string]int, len(header))
	for i, col := range header {
		colIndex[strings.TrimSpace(col)] = i
	}

	hostIdx, ok := colIndex[cfg.HostCol]
	if !ok {
		return fmt.Errorf("required column not found: %s", cfg.HostCol)
	}
	portIdx, ok := colIndex[cfg.PortCol]
	if !ok {
		return fmt.Errorf("required column not found: %s", cfg.PortCol)
	}
	passIdx, ok := colIndex[cfg.PassCol]
	if !ok {
		return fmt.Errorf("required column not found: %s", cfg.PassCol)
	}

	// Open output CSV.
	out, err := os.Create(cfg.Output)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer out.Close()

	writer := csv.NewWriter(out)
	defer writer.Flush()

	// Write header.
	if err := writer.Write(strings.Split(csvHeader, ",")); err != nil {
		return fmt.Errorf("failed to write CSV header: %w", err)
	}

	// Process data rows (skip header).
	for _, row := range rows[1:] {
		if len(row) <= passIdx || len(row) <= hostIdx || len(row) <= portIdx {
			continue
		}

		passVal := row[passIdx]
		if !ShouldIncludeRow(passVal) {
			continue
		}

		host := strings.TrimSpace(row[hostIdx])
		portStr := strings.TrimSpace(row[portIdx])

		if host == "" || portStr == "" {
			continue
		}

		dstIP, err := ResolveHost(host)
		if err != nil {
			// ResolveHost already uses original on failure; continue.
		}

		ports, err := SplitPorts(portStr)
		if ports == nil {
			continue // invalid port, skip row silently.
		}

		for _, port := range ports {
			rec := []string{
				defaultSrcIP,
				defaultSrcNetwork,
				dstIP,
				defaultDstNetwork,
				defaultServiceLabel,
				defaultProtocol,
				fmt.Sprintf("%d", port),
				defaultDecision,
				defaultMatchedPolicyID,
				defaultReason,
			}
			if err := writer.Write(rec); err != nil {
				return fmt.Errorf("failed to write CSV row: %w", err)
			}
		}
	}
	return nil
}
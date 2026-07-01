package csvtransform

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/xuxiping/port-scan-mk3/pkg/spreadsheet"
)

// Config is the transform input; mirrors the CLI's TransformConfig fields.
type Config struct {
	Input     string // Path to input CSV (required)
	Output    string // Path to output CSV (required)
	SheetName string // Worksheet name (default: all-runs, ignored for CSV)
	HostCol   string // Host column name (default: Host)
	PortCol   string // Port column name (default: Port)
	PassCol   string // Pass/fail column name (default: Pass the test)
}

// Run wires CSV reading, column indexing, filtering, host resolution, port
// expansion, and CSV output. Problematic rows are logged to warn and skipped.
func Run(cfg Config, warn io.Writer) error {
	reader := spreadsheet.NewReader(cfg.Input)
	rows, err := reader.OpenSheet(cfg.SheetName)
	if err != nil {
		return fmt.Errorf("failed to open CSV: %w", err)
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
	for rowNum, row := range rows[1:] {
		rowNum++ // 1-indexed for human readability
		rowLen := len(row)

		// Check row length
		if rowLen <= passIdx || rowLen <= hostIdx || rowLen <= portIdx {
			fmt.Fprintf(warn, "skipping row %d: insufficient columns (got %d, need at least %d)\n", rowNum, rowLen, max(passIdx, max(hostIdx, portIdx))+1)
			continue
		}

		passVal := row[passIdx]
		if !ShouldIncludeRow(passVal) {
			fmt.Fprintf(warn, "skipping row %d: pass column is not FALSE (value: %q)\n", rowNum, passVal)
			continue
		}

		host := strings.TrimSpace(row[hostIdx])
		portStr := strings.TrimSpace(row[portIdx])

		if host == "" {
			fmt.Fprintf(warn, "skipping row %d: empty host value\n", rowNum)
			continue
		}
		if portStr == "" {
			fmt.Fprintf(warn, "skipping row %d: empty port value\n", rowNum)
			continue
		}

		dstIP, _ := ResolveHost(host)

		ports, _ := SplitPorts(portStr, warn)
		if ports == nil {
			fmt.Fprintf(warn, "skipping row %d: invalid port value %q\n", rowNum, portStr)
			continue
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

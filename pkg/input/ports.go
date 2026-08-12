package input

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// LoadPorts reads line-based port specifications in `<port>/tcp` format. It
// returns a slice of normalized PortSpec values.
//
// Each non-empty line must have the format `<number>/tcp`. The <number> must be
// a TCP port in the range 1–65535. LoadPorts skips empty lines.
//
// # Parameters
//
//	r: io.Reader positioned at the port file content.
//
// # Returns
//
//	[]PortSpec on success. An error if a line is malformed, or if a port is
//	outside the range.
//
// # Example
//
//	f, _ := os.Open("ports.csv")
//	defer f.Close()
//	specs, err := input.LoadPorts(f)
//	fmt.Println("Loaded", len(specs), "port specs")
func LoadPorts(r io.Reader) ([]PortSpec, error) {
	return LoadPortsContextWithLimits(context.Background(), r, DefaultPortLimits(""))
}

// LoadPortsContext loads port rows and stops at a row transition when ctx is
// canceled.
func LoadPortsContext(ctx context.Context, r io.Reader) ([]PortSpec, error) {
	return LoadPortsContextWithLimits(ctx, r, DefaultPortLimits(""))
}

// LoadPortsContextWithLimits loads port rows with byte and nonblank-record limits.
// A zero limit disables only that limit.
func LoadPortsContextWithLimits(ctx context.Context, r io.Reader, limits PortLimits) ([]PortSpec, error) {
	limited := limitInputReader(r, limits.Path, "port", "-port-input-size-limit-mb", limits.MaxBytes)
	scanner := bufio.NewScanner(limited)
	out := make([]PortSpec, 0)
	lineNumber := 0
	recordCount := uint64(0)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !scanner.Scan() {
			break
		}
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		recordCount++
		if limits.MaxRecords > 0 && recordCount > limits.MaxRecords {
			return nil, fmt.Errorf("port input %s record %d makes count %d exceed limit %d; use -port-input-record-limit to override it", displayPath(limits.Path), lineNumber, recordCount, limits.MaxRecords)
		}
		parts := strings.Split(line, "/")
		if len(parts) != 2 || strings.ToLower(parts[1]) != "tcp" {
			return nil, fmt.Errorf("invalid port row: %s", line)
		}
		n, err := strconv.Atoi(parts[0])
		if err != nil || n < 1 || n > 65535 {
			return nil, fmt.Errorf("invalid port number: %s", line)
		}
		out = append(out, PortSpec{Number: n, Proto: "tcp", Raw: line})
	}
	return out, scanner.Err()
}

package input

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// LoadPorts reads line-based port specifications in `<port>/tcp` format and
// returns a slice of normalized PortSpec values.
//
// Each non-empty line must be in the format `<number>/tcp` where <number> is
// a TCP port in the range 1–65535. Empty lines are skipped.
//
// # Parameters
//
//	r: io.Reader positioned at the port file content.
//
// # Returns
//
//	[]PortSpec on success; error if any line is malformed or port is out of range.
//
// # Example
//
//	f, _ := os.Open("ports.csv")
//	defer f.Close()
//	specs, err := input.LoadPorts(f)
//	fmt.Println("Loaded", len(specs), "port specs")
func LoadPorts(r io.Reader) ([]PortSpec, error) {
	scanner := bufio.NewScanner(r)
	out := make([]PortSpec, 0)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
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

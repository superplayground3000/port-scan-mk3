package main

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
)

// SplitPorts splits a "/"-separated port string into individual port integers.
// Empty string returns nil (caller skips row).
// Invalid ports are skipped silently (logged to stderr).
func SplitPorts(portStr string) ([]int, error) {
	if strings.TrimSpace(portStr) == "" {
		return nil, nil
	}

	parts := strings.Split(portStr, "/")
	ports := make([]int, 0, len(parts))

	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		port, err := strconv.Atoi(p)
		if err != nil {
			fmt.Fprintf(stderr, "invalid port %q: %v\n", p, err)
			return nil, nil // skip entire row on any invalid port
		}
		ports = append(ports, port)
	}

	if len(ports) == 0 {
		return nil, nil
	}
	return ports, nil
}

// stderr is used for logging invalid port warnings.
var stderr = os.Stderr

// ResolveHost resolves a host (IP or hostname) to an IPv4 string.
// IPv4 addresses are returned as-is.
// Hostnames are resolved via net.LookupIP; on failure the original
// hostname string is returned (downstream validation will catch it).
func ResolveHost(host string) (string, error) {
	// IPv4 passthrough
	if ip := net.ParseIP(host); ip != nil && ip.To4() != nil {
		return host, nil
	}

	// Try to resolve hostname
	ips, err := net.LookupIP(host)
	if err != nil {
		// Lookup failed, return original hostname for downstream validation
		return host, nil
	}

	// Return first IPv4 address
	for _, ip := range ips {
		if ip.To4() != nil {
			return ip.String(), nil
		}
	}

	// No IPv4 found, return original hostname
	return host, nil
}

// ShouldIncludeRow returns true only if passVal is "FALSE" (case-insensitive, trimmed).
func ShouldIncludeRow(passVal string) bool {
	return strings.EqualFold(strings.TrimSpace(passVal), "FALSE")
}

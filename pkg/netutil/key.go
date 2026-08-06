// Package netutil provides IPv4 networking utilities for the port scanner.
// It computes IP ranges, builds execution keys, and converts IPv4 addresses to
// uint32 values for internal data structures.
//
// # Example
//
//	start, end, ok := netutil.IPRange(cidr)
//	key, err := netutil.BuildExecutionKey("192.168.1.1", 80, "tcp")
package netutil

import (
	"fmt"
	"net"
	"strings"
)

// BuildExecutionKey creates a standardized execution key in the format
// `dst_ip:port/protocol`. The execution layer uses this key to remove duplicate
// targets, so the key must be unique for each scan target.
//
// # Parameters
//
//	dstIP:    Destination IPv4 address string.
//	port:     TCP port number (1–65535).
//	protocol: Protocol string. BuildExecutionKey accepts only "tcp".
//
// # Returns
//
//	A canonical key string on success. An error when dstIP is not an IPv4
//	address, when port is out of range, or when protocol is not "tcp".
//
// # Example
//
//	key, err := netutil.BuildExecutionKey("192.168.1.1", 80, "tcp")
//	// key == "192.168.1.1:80/tcp"
func BuildExecutionKey(dstIP string, port int, protocol string) (string, error) {
	ip := net.ParseIP(strings.TrimSpace(dstIP)).To4()
	if ip == nil {
		return "", fmt.Errorf("invalid dst_ip: %q", dstIP)
	}
	if port < 1 || port > 65535 {
		return "", fmt.Errorf("invalid port: %d", port)
	}
	proto := strings.ToLower(strings.TrimSpace(protocol))
	if proto != "tcp" {
		return "", fmt.Errorf("invalid protocol: %q", proto)
	}
	return fmt.Sprintf("%s:%d/%s", ip.String(), port, proto), nil
}

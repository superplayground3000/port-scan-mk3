// Package netutil provides IPv4 networking utilities used across the port scanner.
// It handles IP range computation, execution key generation, and IPv4-to-uint32
// conversion for internal data structures.
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
// `dst_ip:port/protocol`. The key is used for deduplication at the execution
// layer and must be unique per scan target.
//
// # Parameters
//
//	dstIP:    Destination IPv4 address string.
//	port:     TCP port number (1–65535).
//	protocol: Protocol string (only "tcp" is accepted).
//
// # Returns
//
//	A canonical key string on success; an error if dstIP is not an IPv4 address,
//	port is out of range, or protocol is not "tcp".
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

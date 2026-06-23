// Package netutil provides IPv4 networking utilities used across the port scanner.
// It handles IP range computation, execution key generation, and IPv4-to-uint32
// conversion for internal data structures.
//
// # Example
//
//	start, end, ok := netutil.IPRange(cidr)
//	key, err := netutil.BuildExecutionKey("192.168.1.1", 80, "tcp")
package netutil

import "net"

// IPRange computes the first and last IPv4 address in a CIDR network.
//
// It applies the CIDR mask to the network IP for the start address, and
// sets host bits to 1 for the end address. Only IPv4 networks are supported.
//
// # Parameters
//
//	n: The network to expand. Must not be nil.
//
// # Returns
//
//	start: First assignable IP in the network.
//	end:   Last assignable IP in the network.
//	ok:    true on success; false if n is nil, has a non-IPv4 IP, or has a non-4-byte mask.
//
// # Example
//
//	_, n, _ := net.ParseCIDR("10.0.0.0/8")
//	start, end, ok := netutil.IPRange(n)
//	// start = "10.0.0.0", end = "10.255.255.255"
func IPRange(n *net.IPNet) (start net.IP, end net.IP, ok bool) {
	if n == nil {
		return nil, nil, false
	}
	base := n.IP.To4()
	if base == nil {
		return nil, nil, false
	}
	mask := n.Mask
	if len(mask) != net.IPv4len {
		return nil, nil, false
	}
	start = make(net.IP, net.IPv4len)
	for i := 0; i < net.IPv4len; i++ {
		start[i] = base[i] & mask[i]
	}
	end = make(net.IP, net.IPv4len)
	for i := 0; i < net.IPv4len; i++ {
		end[i] = start[i] | ^mask[i]
	}
	return start, end, true
}
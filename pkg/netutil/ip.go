// Package netutil provides IPv4 networking utilities for the port scanner.
// It computes IP ranges, builds execution keys, and converts IPv4 addresses to
// uint32 values for internal data structures.
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
// sets host bits to 1 for the end address. IPRange supports only IPv4 networks.
//
// # Parameters
//
//	n: The network to expand. It must not be nil.
//
// # Returns
//
//	start: First assignable IP in the network.
//	end:   Last assignable IP in the network.
//	ok:    true on success. It is false when n is nil, when the IP of n is not
//	       IPv4, or when the mask of n is not 4 bytes.
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

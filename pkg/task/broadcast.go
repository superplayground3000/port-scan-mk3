package task

import (
	"net"

	"github.com/xuxiping/port-scan-mk3/pkg/netutil"
)

// FilterBoundaryBroadcast removes the broadcast address of the given boundary
// subnet from ips. It keeps the order and every other address, including the
// network address.
//
// The broadcast address is a property of the network segment. The boundary CIDR
// therefore defines it, and no selector sub-range defines it.
// FilterBoundaryBroadcast removes an address only when that address equals the
// broadcast of the boundary. The rule is the same for an address from a CIDR
// expansion and for an address that the input lists as a single IP.
//
// A subnet with prefix /31 (RFC 3021 point-to-point) or /32 (single host) has no
// broadcast address, so FilterBoundaryBroadcast returns ips unchanged. A nil
// boundary means "no subnet context", so FilterBoundaryBroadcast also returns
// ips unchanged.
//
// # Example
//
//	_, n, _ := net.ParseCIDR("10.0.0.0/24")
//	kept := task.FilterBoundaryBroadcast([]string{"10.0.0.0", "10.0.0.255"}, n)
//	// kept == ["10.0.0.0"]  // .255 broadcast dropped, .0 network kept
func FilterBoundaryBroadcast(ips []string, boundary *net.IPNet) []string {
	if boundary == nil {
		return ips
	}
	ones, bits := boundary.Mask.Size()
	if bits != 32 || ones >= 31 {
		// Non-IPv4 mask, or /31 and /32 which have no broadcast address.
		return ips
	}
	_, end, ok := netutil.IPRange(boundary)
	if !ok {
		return ips
	}
	broadcast := end.String()

	out := make([]string, 0, len(ips))
	for _, ip := range ips {
		if ip == broadcast {
			continue
		}
		out = append(out, ip)
	}
	return out
}

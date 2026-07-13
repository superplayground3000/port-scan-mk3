package task

import (
	"net"

	"github.com/xuxiping/port-scan-mk3/pkg/netutil"
)

// FilterBoundaryBroadcast removes the broadcast address of the given boundary
// subnet from ips, preserving order and all other addresses (including the
// network address). The broadcast is a property of the network segment, so it
// is defined by the boundary CIDR rather than by any selector sub-range: an
// address is dropped only when it equals the boundary's broadcast, whether it
// arrived via CIDR expansion or as an explicitly listed single IP.
//
// Subnets with prefix /31 (RFC 3021 point-to-point) and /32 (single host) have
// no broadcast address, so ips is returned unchanged. A nil boundary is treated
// as "no subnet context" and ips is returned unchanged.
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

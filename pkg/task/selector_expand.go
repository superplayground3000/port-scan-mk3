package task

import (
	"encoding/binary"
	"fmt"
	"net"
	"sort"
	"strings"

	"github.com/xuxiping/port-scan-mk3/pkg/netutil"
)

// ExpandIPSelectors expands a list of IP selectors (individual IPs or CIDR ranges)
// into a deduplicated, sorted slice of individual IPv4 address strings.
//
// Each selector may be a single IPv4 address (e.g., "192.168.1.1") or a CIDR
// range (e.g., "10.0.0.0/8"). IPv6 inputs are rejected. The output is sorted
// in ascending numeric order and contains no duplicates.
//
// Expansion is inclusive of every address in the range. Broadcast-address
// exclusion is applied separately against the boundary subnet by the caller
// (see FilterBoundaryBroadcast), because the broadcast is a property of the
// network segment, not of an arbitrary selector sub-range.
//
// # Parameters
//
//	selectors: Slice of IP selector strings (IP or CIDR notation).
//
// # Returns
//
//	Sorted slice of individual IPv4 strings on success; error if any selector
//	is invalid or IPv6.
//
// # Example
//
//	ips, err := task.ExpandIPSelectors([]string{"192.168.1.0/30", "192.168.1.1"})
//	// ips == ["192.168.1.0", "192.168.1.1", "192.168.1.2", "192.168.1.3"]
func ExpandIPSelectors(selectors []string) ([]string, error) {
	uniq := make(map[uint32]struct{})
	for _, raw := range selectors {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return nil, fmt.Errorf("empty selector")
		}
		if ip := net.ParseIP(raw); ip != nil {
			v4 := ip.To4()
			if v4 == nil {
				return nil, fmt.Errorf("only ipv4 is supported: %s", raw)
			}
			uniq[binary.BigEndian.Uint32(v4)] = struct{}{}
			continue
		}
		_, n, err := net.ParseCIDR(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid selector %q: %w", raw, err)
		}
		start, end, ok := netutil.IPRange(n)
		if !ok {
			return nil, fmt.Errorf("only ipv4 is supported: %s", raw)
		}
		startN := binary.BigEndian.Uint32(start.To4())
		endN := binary.BigEndian.Uint32(end.To4())
		for curr := startN; curr <= endN; curr++ {
			uniq[curr] = struct{}{}
			if curr == ^uint32(0) {
				break
			}
		}
	}

	keys := make([]uint32, 0, len(uniq))
	for n := range uniq {
		keys = append(keys, n)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })

	out := make([]string, 0, len(keys))
	for _, n := range keys {
		ip := make(net.IP, 4)
		binary.BigEndian.PutUint32(ip, n)
		out = append(out, ip.String())
	}
	return out, nil
}

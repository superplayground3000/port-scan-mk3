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
// For a CIDR range with prefix /30 or larger, the broadcast address (all host
// bits set, e.g. .255 in a /24) is excluded because it is not a scannable host;
// the network address (all host bits clear) is retained. /31 and /32 have no
// broadcast and are expanded in full. An address supplied as an explicit single
// IP (or /32) is always kept, even if it happens to be a broadcast-looking .255.
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
//	// ips == ["192.168.1.0", "192.168.1.1", "192.168.1.2"]  // .3 broadcast excluded
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
		// A CIDR block reserves its last address (all host bits set) as the
		// broadcast address, which must not be scanned. The network address
		// (all host bits clear) is retained. /31 and /32 have no broadcast
		// (RFC 3021 point-to-point and single host), so nothing is excluded.
		ones, _ := n.Mask.Size()
		excludeBroadcast := ones <= 30
		for curr := startN; ; curr++ {
			if !(excludeBroadcast && curr == endN) {
				uniq[curr] = struct{}{}
			}
			if curr == endN {
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

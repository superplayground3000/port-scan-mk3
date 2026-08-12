package task

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"sort"
	"strings"

	"github.com/xuxiping/port-scan-mk3/pkg/netutil"
)

// ExpandIPSelectors expands a list of IP selectors into a sorted slice of
// individual IPv4 address strings. A selector is an individual IP or a CIDR
// range. The slice contains no duplicates.
//
// Each selector can be a single IPv4 address, for example "192.168.1.1", or a
// CIDR range, for example "10.0.0.0/8". ExpandIPSelectors returns an error for an
// IPv6 input. The output is in ascending numeric order.
//
// ExpandIPSelectors includes every address in the range. The caller removes the
// broadcast address separately, against the boundary subnet, with
// FilterBoundaryBroadcast. The broadcast address is a property of the network
// segment, and not of an arbitrary selector sub-range.
//
// # Parameters
//
//	selectors: Slice of IP selector strings, in IP or CIDR notation.
//
// # Returns
//
//	A sorted slice of individual IPv4 strings on success. An error if a selector
//	is invalid or is IPv6.
//
// # Example
//
//	ips, err := task.ExpandIPSelectors([]string{"192.168.1.0/30", "192.168.1.1"})
//	// ips == ["192.168.1.0", "192.168.1.1", "192.168.1.2", "192.168.1.3"]
func ExpandIPSelectors(selectors []string) ([]string, error) {
	return ExpandIPSelectorsContext(context.Background(), selectors)
}

// ExpandIPSelectorsContext expands IPv4 selectors and reads ctx at intervals
// of no more than 4,096 candidate addresses.
func ExpandIPSelectorsContext(ctx context.Context, selectors []string) ([]string, error) {
	uniq := make(map[uint32]struct{})
	for _, raw := range selectors {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
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
			if (curr-startN)%4096 == 0 {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
			}
			uniq[curr] = struct{}{}
			if curr == ^uint32(0) {
				break
			}
		}
	}

	keys := make([]uint32, 0, len(uniq))
	for n := range uniq {
		if len(keys)%4096 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		keys = append(keys, n)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })

	out := make([]string, 0, len(keys))
	for i, n := range keys {
		if i%4096 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		ip := make(net.IP, 4)
		binary.BigEndian.PutUint32(ip, n)
		out = append(out, ip.String())
	}
	return out, nil
}

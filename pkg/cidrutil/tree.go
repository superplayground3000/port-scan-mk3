package cidrutil

import (
	"fmt"
	"net"
)

// IntervalTree holds deny CIDR entries for containment queries.
type IntervalTree struct {
	entries []CIDREntry
}

// Insert adds a deny CIDR entry to the tree.
func (t *IntervalTree) Insert(e CIDREntry) {
	t.entries = append(t.entries, e)
}

// Query finds all deny CIDRs that contain the given open CIDR.
func (t *IntervalTree) Query(e CIDREntry) []CIDREntry {
	var result []CIDREntry
	for _, entry := range t.entries {
		if contains(entry, e) {
			result = append(result, entry)
		}
	}
	return result
}

// contains returns true if deny contains open.
func contains(deny, open CIDREntry) bool {
	return deny.StartIP <= open.StartIP && deny.EndIP >= open.EndIP
}

// ParseCIDR parses a CIDR string and returns a CIDREntry with numeric bounds.
func ParseCIDR(cidr string) (CIDREntry, error) {
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return CIDREntry{}, err
	}
	ip := ipNet.IP.To4()
	if ip == nil {
		return CIDREntry{}, fmt.Errorf("CIDR %q is IPv6, IPv4 required", cidr)
	}
	start := uint32(ip[0])<<24 | uint32(ip[1])<<16 | uint32(ip[2])<<8 | uint32(ip[3])
	// Calculate last IP: network address OR NOT mask
	mask := ipNet.Mask
	end := start | ^(uint32(mask[0])<<24 | uint32(mask[1])<<16 | uint32(mask[2])<<8 | uint32(mask[3]))
	return CIDREntry{
		Network: cidr,
		StartIP: start,
		EndIP:   end,
	}, nil
}

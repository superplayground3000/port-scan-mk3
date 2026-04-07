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

// ipToUint32 parses a dotted-decimal IP string to uint32.
func ipToUint32(s string) uint32 {
	ip := net.ParseIP(s)
	if ip == nil {
		return 0
	}
	ip4 := ip.To4()
	if ip4 == nil {
		return 0 // IPv6 addresses are not supported, return 0
	}
	return uint32(ip4[0])<<24 | uint32(ip4[1])<<16 | uint32(ip4[2])<<8 | uint32(ip4[3])
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

// Package cidrutil provides CIDR interval tree operations for containment queries.
// It is used by the cidr-compare command to find deny CIDRs that cover given open CIDRs.
//
// # Function Flow
//
//	Deny CIDRs ── ParseCIDR ── IntervalTree.Insert
//	  |
//	  v
//	Open CIDR ── ParseCIDR ── IntervalTree.Query
//	  |
//	  v
//	[]MatchResult (deny CIDRs covering the open CIDR)
package cidrutil

import (
	"fmt"
	"net"
)

// IntervalTree holds a list of CIDREntry values for containment queries.
// It is not a balanced tree — Query performs a linear scan over all entries.
// It is suitable for small-to-medium sets of deny rules.
type IntervalTree struct {
	entries []CIDREntry
}

// Insert adds a CIDREntry to the tree. Entries are not automatically sorted.
func (t *IntervalTree) Insert(e CIDREntry) {
	t.entries = append(t.entries, e)
}

// Query returns all entries in the tree that contain the given CIDREntry.
// An entry A contains entry B when A.StartIP <= B.StartIP and A.EndIP >= B.EndIP.
//
// # Parameters
//
//	e: The CIDR entry to test.
//
// # Returns
//
//	All tree entries that contain e; empty slice if none match.
//
// # Example
//
//	tree.Insert(cidrutil CIDREntry{Network: "10.0.0.0/8", StartIP: 167772160, EndIP: 184549375})
//	results := tree.Query(cidrutil CIDREntry{Network: "10.1.0.0/16", ...})
func (t *IntervalTree) Query(e CIDREntry) []CIDREntry {
	var result []CIDREntry
	for _, entry := range t.entries {
		if contains(entry, e) {
			result = append(result, entry)
		}
	}
	return result
}

// contains returns true if deny CIDR contains the open CIDR.
func contains(deny, open CIDREntry) bool {
	return deny.StartIP <= open.StartIP && deny.EndIP >= open.EndIP
}

// ParseCIDR parses a CIDR string and returns a CIDREntry with numeric StartIP
// and EndIP bounds computed from the network address and mask.
//
// # Parameters
//
//	cidr: A valid CIDR notation string (e.g., "10.0.0.0/8").
//
// # Returns
//
//	CIDREntry on success; error if cidr is malformed or IPv6.
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

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

// CIDREntry represents a parsed CIDR network with its numeric range bounds,
// stored as uint32 for efficient interval comparisons.
type CIDREntry struct {
	// Network is the original CIDR string (e.g., "10.0.0.0/8").
	Network string
	// StartIP is the uint32 of the first IP in the range (network-order big-endian).
	StartIP uint32
	// EndIP is the uint32 of the last IP in the range (network-order big-endian).
	EndIP uint32
}

// MatchResult represents a matching deny/open CIDR pair found by the
// interval tree containment query in the cidr-compare tool.
type MatchResult struct {
	// DenyCIDR is the deny CIDR that contains the open CIDR.
	DenyCIDR string
	// OpenCIDR is the open CIDR that was queried.
	OpenCIDR string
}

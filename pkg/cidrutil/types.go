// Package cidrutil answers CIDR containment queries with an interval tree.
// The cidr-compare command uses this package to find the deny CIDRs that cover
// a given open CIDR.
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

// CIDREntry holds a parsed CIDR network and its numeric range bounds.
// The bounds are uint32 values, which makes interval comparisons fast.
type CIDREntry struct {
	// Network is the original CIDR string, for example "10.0.0.0/8".
	Network string
	// StartIP is the uint32 of the first IP in the range (network-order big-endian).
	StartIP uint32
	// EndIP is the uint32 of the last IP in the range (network-order big-endian).
	EndIP uint32
}

// MatchResult holds a deny CIDR and the open CIDR that it contains.
// The cidr-compare tool creates these results from the interval tree
// containment query.
type MatchResult struct {
	// DenyCIDR is the deny CIDR that contains the open CIDR.
	DenyCIDR string
	// OpenCIDR is the open CIDR of the query.
	OpenCIDR string
}

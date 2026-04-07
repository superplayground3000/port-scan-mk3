package cidrutil

// CIDREntry represents a parsed CIDR with its numeric range bounds.
type CIDREntry struct {
	Network string // CIDR string, e.g., "10.0.0.0/8"
	StartIP uint32 // Network order uint32 of first IP in range
	EndIP   uint32 // Network order uint32 of last IP in range
}

// MatchResult represents a matching deny/open CIDR pair.
type MatchResult struct {
	DenyCIDR string
	OpenCIDR string
}

// Package preprocesscfg defines column names, status values, and placeholder
// defaults shared across the enrich and preprocess packages.
package preprocesscfg

// Rich CSV output columns.
var (
	ColSrcIP             = "src_ip"
	ColSrcNetworkSegment = "src_network_segment"
	ColDstIP             = "dst_ip"
	ColDstNetworkSegment = "dst_network_segment"
	ColServiceLabel      = "service_label"
	ColProtocol          = "protocol"
	ColPort              = "port"
	ColDecision          = "decision"
	ColMatchedPolicyID   = "matched_policy_id"
	ColReason            = "reason"
)

// RichHeader returns the canonical rich CSV header row in column order.
func RichHeader() []string {
	return []string{
		ColSrcIP, ColSrcNetworkSegment,
		ColDstIP, ColDstNetworkSegment,
		ColServiceLabel, ColProtocol, ColPort,
		ColDecision, ColMatchedPolicyID, ColReason,
	}
}

// Opened targets input columns.
var (
	ColHost      = "host"
	ColPortInput = "port"
)

// Cleaned CIDRs columns.
var (
	ColFab    = "fab"
	ColCIDR   = "segment"
	ColStatus = "status"
)

// Service map columns.
var (
	ColServicePort = "port"
	ColServiceName = "service_label"
)

// CIDR status values.
var (
	StatusOpen  = "open"
	StatusClose = "close"
)

// Placeholder and default values for enrichment.
var (
	DefaultSrcIP             = "10.59.42.39"
	DefaultSrcNetworkSegment = "10.59.42.39/32"
	DefaultProtocol          = "tcp"
	DefaultDecision          = "accept"
	DefaultPolicyID          = "enriched"
	DefaultReason            = "MATCH_POLICY_ACCEPT"
	FallbackServiceLabel     = "unknown"
)

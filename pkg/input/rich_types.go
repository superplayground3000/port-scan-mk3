package input

// Rich-mode column names, as they are in CSV headers. The header match is
// case-insensitive.
const (
	RichFieldSrcIP             = "src_ip"
	RichFieldSrcNetworkSegment = "src_network_segment"
	RichFieldDstIP             = "dst_ip"
	RichFieldDstNetworkSegment = "dst_network_segment"
	RichFieldServiceLabel      = "service_label"
	RichFieldProtocol          = "protocol"
	RichFieldPort              = "port"
	RichFieldDecision          = "decision"
	RichFieldPolicyID          = "matched_policy_id"
	RichFieldReason            = "reason"
)

var requiredRichFields = []string{
	RichFieldSrcIP,
	RichFieldSrcNetworkSegment,
	RichFieldDstIP,
	RichFieldDstNetworkSegment,
	RichFieldServiceLabel,
	RichFieldProtocol,
	RichFieldPort,
	RichFieldDecision,
	RichFieldPolicyID,
	RichFieldReason,
}

// RichParseSummary aggregates row-level parse outcomes for rich input mode.
// ParseRichRows returns this summary, and the caller uses it for diagnostics.
type RichParseSummary struct {
	// TotalRows is the number of data rows processed (header row excluded).
	TotalRows int
	// ValidRows is the count of rows that parsed successfully.
	ValidRows int
	// InvalidRows is the count of rows that failed validation.
	InvalidRows int
	// FailureByReason maps a validation code to the number of rows that failed with that code.
	FailureByReason map[string]int
}

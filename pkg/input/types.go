package input

import "net"

// CIDRRecord represents one parsed row from a CIDR CSV input file.
//
// In basic mode, it holds the raw selector string and the raw boundary CIDR
// string, with their parsed net.IPNet forms. In rich mode, it also holds the full
// firewall-policy fields, for example src_ip, dst_ip, port, and decision.
//
// The caller must call Parse to fill the Net and Selector fields before use.
type CIDRRecord struct {
	// FabName is the name of the fabric or mesh. It is optional and applies to
	// basic mode.
	FabName string
	// CIDR is the normalized string form of the boundary CIDR.
	CIDR string
	// CIDRName is a human-readable name for the CIDR (optional).
	CIDRName string
	// Net is the parsed boundary CIDR as *net.IPNet.
	Net *net.IPNet
	// IPRaw is the raw IP selector string, before Parse normalizes it.
	IPRaw string
	// IPCidrRaw is the raw boundary CIDR string, before Parse normalizes it.
	IPCidrRaw string
	// Selector is the parsed IP selector as *net.IPNet (a single IP or CIDR range).
	Selector *net.IPNet
	// RowNumber is the 1-indexed row number in the source CSV.
	RowNumber int
	// IPColName is the column name used for the IP selector.
	IPColName string
	// IPCidrCol is the column name used for the boundary CIDR.
	IPCidrCol string

	// The fields below hold rich input. The parser fills them when IsRich is true.
	// IsRich is true when the parser read this record in rich mode.
	IsRich bool
	// IsValid is true when the rich row passed all validation checks.
	IsValid bool
	// ValidationCode is the machine-readable failure code when IsValid is false.
	ValidationCode string
	// ValidationError is the human-readable error message when IsValid is false.
	ValidationError string
	// SrcIP is the source IP address in rich mode.
	SrcIP string
	// SrcNetworkSegment is the source network boundary in rich mode.
	SrcNetworkSegment string
	// DstIP is the destination IP address in rich mode.
	DstIP string
	// DstNetworkSegment is the destination network boundary in rich mode.
	DstNetworkSegment string
	// ServiceLabel is the service identifier from rich input.
	ServiceLabel string
	// Protocol is the protocol name. The parser accepts "tcp" only.
	Protocol string
	// Port is the row TCP port. It is required in rich mode and optional in basic mode.
	Port int
	// Decision is "accept" or "deny" from the policy row.
	Decision string
	// PolicyID is the matched policy identifier.
	PolicyID string
	// Reason is the policy match reason string.
	Reason string
	// ExecutionKey is the canonical dedup key, formatted as dst_ip:port/protocol.
	ExecutionKey string
	// RichInputIdentifier is a debug string that identifies the source row.
	RichInputIdentifier string
}

// PortSpec represents one normalized TCP port specification from a port input
// file. LoadPorts reads these specifications in `<port>/tcp` format.
type PortSpec struct {
	// Number is the TCP port number (1–65535).
	Number int
	// Proto is always "tcp".
	Proto string
	// Raw is the original line as read from the input file.
	Raw string
}

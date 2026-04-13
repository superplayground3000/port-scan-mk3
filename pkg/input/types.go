package input

import "net"

// CIDRRecord represents one parsed row from a CIDR CSV input file.
//
// In basic mode, it holds the raw selector and boundary CIDR strings alongside
// their parsed net.IPNet forms. In rich mode, it additionally carries the full
// firewall-policy fields (src_ip, dst_ip, port, decision, etc.).
//
// Callers must call Parse() to populate the Net and Selector fields before use.
type CIDRRecord struct {
	// FabName is the fabric/mesh name (optional, basic mode).
	FabName string
	// CIDR is the normalized string form of the boundary CIDR.
	CIDR string
	// CIDRName is a human-readable name for the CIDR (optional).
	CIDRName string
	// Net is the parsed boundary CIDR as *net.IPNet.
	Net *net.IPNet
	// IPRaw is the raw IP selector string (before parsing).
	IPRaw string
	// IPCidrRaw is the raw boundary CIDR string (before parsing).
	IPCidrRaw string
	// Selector is the parsed IP selector as *net.IPNet (a single IP or CIDR range).
	Selector *net.IPNet
	// RowNumber is the 1-indexed row number in the source CSV.
	RowNumber int
	// IPColName is the column name used for the IP selector.
	IPColName string
	// IPCidrCol is the column name used for the boundary CIDR.
	IPCidrCol string

	// Rich input fields (populated when IsRich is true).
	// IsRich indicates whether this record was parsed in rich mode.
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
	// Protocol is the protocol (only "tcp" is accepted).
	Protocol string
	// Port is the target TCP port number (rich mode only).
	Port int
	// Decision is "accept" or "deny" from the policy row.
	Decision string
	// PolicyID is the matched policy identifier.
	PolicyID string
	// Reason is the policy match reason string.
	Reason string
	// ExecutionKey is the canonical dedup key, formatted as dst_ip:port/protocol.
	ExecutionKey string
	// RichInputIdentifier is a debug string identifying the source row.
	RichInputIdentifier string
}

// PortSpec represents one normalized TCP port specification from a port input file.
// Port specs are in `<port>/tcp` format, as read by LoadPorts.
type PortSpec struct {
	// Number is the TCP port number (1–65535).
	Number int
	// Proto is always "tcp".
	Proto string
	// Raw is the original line as read from the input file.
	Raw string
}

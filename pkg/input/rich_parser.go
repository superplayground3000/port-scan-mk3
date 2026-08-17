// Package input parses and validates CSV inputs for the port scanner.
//
// The package supports two input modes:
//
//   - Basic CIDR mode: a CSV with ip and ip_cidr columns. LoadCIDRs and
//     LoadCIDRsWithColumns read this mode.
//   - Rich mode: a CSV with structured firewall-policy columns, for example
//     src_ip, dst_ip, port, and decision. The detectRichHeaderIndices function
//     detects this mode from the header, and ParseRichRows parses it.
//
// # Function Flow
//
//	CSV File
//	  |
//	  v
//	LoadCIDRs / LoadCIDRsWithColumns
//	  |
//	  v
//	detectRichHeaderIndices  ── rich ──> ParseRichRows
//	  |
//	  | basic
//	  v
//	Parse CIDRRecord fields
//	  |
//	  v
//	ValidateIPRows (duplicate check + containment)
//	  |
//	  v
//	[]CIDRRecord
//
// # Example
//
//	records, err := input.LoadCIDRsWithColumns(os.Stdin, "ip", "ip_cidr")
//	if err != nil {
//	    log.Fatalf("load failed: %v", err)
//	}
//	if err := input.ValidateIPRows(records); err != nil {
//	    log.Fatalf("validation failed: %v", err)
//	}
package input

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
)

// ParseRichRows parses and validates rich-mode CIDR input rows.
//
// It processes raw CSV rows with the column index map that detectRichHeaderIndices
// returns. Each input row produces exactly one CIDRRecord in the output slice. A
// valid row carries full parsed data. An invalid row carries a ValidationCode and
// a ValidationError for diagnostics.
//
// # Parameters
//
//	rows: Raw CSV rows with the header at index 0. ParseRichRows skips the header.
//	idx:  Map of canonicalized header names to column indices, from detectRichHeaderIndices.
//
// # Returns
//
//	[]CIDRRecord on success. The slice includes invalid rows with IsValid=false.
//	RichParseSummary with row-level statistics.
//	An error only when all rows are invalid, or when a structural failure occurs.
//
// # Required Header Fields
//
//	RichFieldSrcIP, RichFieldSrcNetworkSegment, RichFieldDstIP, RichFieldDstNetworkSegment,
//	RichFieldServiceLabel, RichFieldProtocol, RichFieldPort, RichFieldDecision,
//	RichFieldPolicyID, RichFieldReason
//
// # Example
//
//	headerIdx, ok := detectRichHeaderIndices(headerRow)
//	if ok {
//	    records, summary, err := input.ParseRichRows(allRows, headerIdx)
//	}
func ParseRichRows(rows [][]string, idx map[string]int) ([]CIDRRecord, RichParseSummary, error) {
	return ParseRichRowsContext(context.Background(), rows, idx)
}

// ParseRichRowsContext parses rich rows and stops at a row transition when ctx
// is canceled.
func ParseRichRowsContext(ctx context.Context, rows [][]string, idx map[string]int) ([]CIDRRecord, RichParseSummary, error) {
	summary := RichParseSummary{
		TotalRows:       max(0, len(rows)-1),
		FailureByReason: map[string]int{},
	}
	out := make([]CIDRRecord, 0, summary.TotalRows)
	pool := newRichStringPool()
	for i := 1; i < len(rows); i++ {
		if err := ctx.Err(); err != nil {
			return out, summary, err
		}
		rec, code, err := parseRichRow(rows[i], i+1, idx, pool)
		if err != nil {
			summary.InvalidRows++
			summary.FailureByReason[code]++
			out = append(out, CIDRRecord{
				RowNumber:       i + 1,
				IsRich:          true,
				IsValid:         false,
				ValidationCode:  code,
				ValidationError: err.Error(),
			})
			continue
		}
		summary.ValidRows++
		out = append(out, rec)
	}
	if summary.ValidRows == 0 {
		return out, summary, fmt.Errorf("no usable input rows")
	}
	return out, summary, nil
}

func parseRichRow(row []string, rowNumber int, idx map[string]int, pool *richStringPool) (CIDRRecord, string, error) {
	get := func(field string) string {
		i := idx[field]
		if i < 0 || i >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[i])
	}

	srcIPRaw := get(RichFieldSrcIP)
	srcSegRaw := get(RichFieldSrcNetworkSegment)
	dstIPRaw := get(RichFieldDstIP)
	dstSegRaw := get(RichFieldDstNetworkSegment)
	serviceLabel := get(RichFieldServiceLabel)
	protocolRaw := get(RichFieldProtocol)
	portRaw := get(RichFieldPort)
	decisionRaw := get(RichFieldDecision)
	policyID := get(RichFieldPolicyID)
	reason := get(RichFieldReason)

	required := [...]struct {
		field string
		value string
	}{
		{RichFieldSrcIP, srcIPRaw},
		{RichFieldSrcNetworkSegment, srcSegRaw},
		{RichFieldDstIP, dstIPRaw},
		{RichFieldDstNetworkSegment, dstSegRaw},
		{RichFieldServiceLabel, serviceLabel},
		{RichFieldProtocol, protocolRaw},
		{RichFieldPort, portRaw},
		{RichFieldDecision, decisionRaw},
		{RichFieldPolicyID, policyID},
		{RichFieldReason, reason},
	}
	for _, item := range required {
		field, value := item.field, item.value
		if value == "" {
			return CIDRRecord{}, ValidationMissingField, fmt.Errorf("missing required field %s", field)
		}
	}

	srcAddr, err := netip.ParseAddr(srcIPRaw)
	if err != nil {
		return CIDRRecord{}, ValidationInvalidSrcIP, fmt.Errorf("invalid src_ip %q", srcIPRaw)
	}
	srcAddr = srcAddr.Unmap()
	if !srcAddr.Is4() {
		return CIDRRecord{}, ValidationInvalidSrcIP, fmt.Errorf("src_ip %q is not an IPv4 address", srcIPRaw)
	}
	dstAddr, err := netip.ParseAddr(dstIPRaw)
	if err != nil {
		return CIDRRecord{}, ValidationInvalidDstIP, fmt.Errorf("invalid dst_ip %q", dstIPRaw)
	}
	dstAddr = dstAddr.Unmap()
	if !dstAddr.Is4() {
		return CIDRRecord{}, ValidationInvalidDstIP, fmt.Errorf("dst_ip %q is not an IPv4 address", dstIPRaw)
	}

	srcPrefix, err := netip.ParsePrefix(srcSegRaw)
	if err != nil {
		return CIDRRecord{}, ValidationInvalidSrcSegment, fmt.Errorf("invalid src_network_segment %q", srcSegRaw)
	}
	if !srcPrefix.Addr().Unmap().Is4() {
		return CIDRRecord{}, ValidationInvalidSrcSegment, fmt.Errorf("src_network_segment %q is not an IPv4 address", srcSegRaw)
	}
	srcPrefix = netip.PrefixFrom(srcPrefix.Addr().Unmap(), srcPrefix.Bits()).Masked()
	dstPrefix, err := netip.ParsePrefix(dstSegRaw)
	if err != nil {
		return CIDRRecord{}, ValidationInvalidDstSegment, fmt.Errorf("invalid dst_network_segment %q", dstSegRaw)
	}
	if !dstPrefix.Addr().Unmap().Is4() {
		return CIDRRecord{}, ValidationInvalidDstSegment, fmt.Errorf("dst_network_segment %q is not an IPv4 address", dstSegRaw)
	}
	dstPrefix = netip.PrefixFrom(dstPrefix.Addr().Unmap(), dstPrefix.Bits()).Masked()
	if !srcPrefix.Contains(srcAddr) {
		return CIDRRecord{}, ValidationSrcContainmentFail, fmt.Errorf("src_ip %s not in src_network_segment %s", srcAddr, srcPrefix)
	}
	if !dstPrefix.Contains(dstAddr) {
		return CIDRRecord{}, ValidationDstContainmentFail, fmt.Errorf("dst_ip %s not in dst_network_segment %s", dstAddr, dstPrefix)
	}

	if !strings.EqualFold(protocolRaw, "tcp") {
		return CIDRRecord{}, ValidationInvalidProtocol, fmt.Errorf("invalid protocol %q", protocolRaw)
	}
	decision := "accept"
	if strings.EqualFold(decisionRaw, "deny") {
		decision = "deny"
	} else if !strings.EqualFold(decisionRaw, decision) {
		return CIDRRecord{}, ValidationInvalidDecision, fmt.Errorf("invalid decision %q", decisionRaw)
	}
	port, err := strconv.Atoi(portRaw)
	if err != nil || port < 1 || port > 65535 {
		return CIDRRecord{}, ValidationInvalidPort, fmt.Errorf("invalid port %q", portRaw)
	}
	srcIP := pool.internOwned(srcAddr.String())
	dstIP := pool.internOwned(dstAddr.String())
	srcSegment := pool.internOwned(srcPrefix.String())
	dstSegment := pool.internOwned(dstPrefix.String())
	serviceLabel = pool.internBorrowed(serviceLabel)
	policyID = pool.internBorrowed(policyID)
	reason = pool.internBorrowed(reason)
	key := dstIP + ":" + strconv.Itoa(port) + "/tcp"
	dstNet := prefixIPNet(dstPrefix)
	selector := addressIPNet(dstAddr)
	if dstPrefix.Bits() == 32 && dstPrefix.Addr() == dstAddr {
		selector = dstNet
	}

	rec := CIDRRecord{
		FabName:             srcIP,
		CIDR:                dstSegment,
		CIDRName:            serviceLabel,
		Net:                 dstNet,
		IPRaw:               dstIP,
		IPCidrRaw:           dstSegment,
		Selector:            selector,
		RowNumber:           rowNumber,
		IPColName:           RichFieldDstIP,
		IPCidrCol:           RichFieldDstNetworkSegment,
		IsRich:              true,
		IsValid:             true,
		SrcIP:               srcIP,
		SrcNetworkSegment:   srcSegment,
		DstIP:               dstIP,
		DstNetworkSegment:   dstSegment,
		ServiceLabel:        serviceLabel,
		Protocol:            "tcp",
		Port:                port,
		Decision:            decision,
		PolicyID:            policyID,
		Reason:              reason,
		ExecutionKey:        key,
		RichInputIdentifier: "row:" + strconv.Itoa(rowNumber),
	}
	return rec, "", nil
}

const richStringPoolCapacity = 4_096

type richStringPool struct {
	values map[string]string
}

func newRichStringPool() *richStringPool {
	return &richStringPool{values: make(map[string]string, richStringPoolCapacity)}
}

func (pool *richStringPool) internOwned(value string) string {
	if interned, ok := pool.values[value]; ok {
		return interned
	}
	if len(pool.values) < richStringPoolCapacity {
		pool.values[value] = value
	}
	return value
}

func (pool *richStringPool) internBorrowed(value string) string {
	if interned, ok := pool.values[value]; ok {
		return interned
	}
	owned := strings.Clone(value)
	if len(pool.values) < richStringPoolCapacity {
		pool.values[owned] = owned
	}
	return owned
}

func prefixIPNet(prefix netip.Prefix) *net.IPNet {
	address := prefix.Addr().As4()
	return &net.IPNet{
		IP:   net.IP{address[0], address[1], address[2], address[3]},
		Mask: net.CIDRMask(prefix.Bits(), 32),
	}
}

func addressIPNet(address netip.Addr) *net.IPNet {
	value := address.As4()
	return &net.IPNet{
		IP:   net.IP{value[0], value[1], value[2], value[3]},
		Mask: net.IPMask{0xff, 0xff, 0xff, 0xff},
	}
}

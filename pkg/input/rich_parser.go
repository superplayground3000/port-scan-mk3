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
	"strconv"
	"strings"

	"github.com/xuxiping/port-scan-mk3/pkg/netutil"
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
	for i := 1; i < len(rows); i++ {
		if err := ctx.Err(); err != nil {
			return out, summary, err
		}
		rec, code, err := parseRichRow(rows[i], i+1, idx)
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

func parseRichRow(row []string, rowNumber int, idx map[string]int) (CIDRRecord, string, error) {
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

	required := map[string]string{
		RichFieldSrcIP:             srcIPRaw,
		RichFieldSrcNetworkSegment: srcSegRaw,
		RichFieldDstIP:             dstIPRaw,
		RichFieldDstNetworkSegment: dstSegRaw,
		RichFieldServiceLabel:      serviceLabel,
		RichFieldProtocol:          protocolRaw,
		RichFieldPort:              portRaw,
		RichFieldDecision:          decisionRaw,
		RichFieldPolicyID:          policyID,
		RichFieldReason:            reason,
	}
	for field, value := range required {
		if value == "" {
			return CIDRRecord{}, ValidationMissingField, fmt.Errorf("missing required field %s", field)
		}
	}

	srcIP := net.ParseIP(srcIPRaw)
	if srcIP == nil {
		return CIDRRecord{}, ValidationInvalidSrcIP, fmt.Errorf("invalid src_ip %q", srcIPRaw)
	}
	srcIP = srcIP.To4()
	if srcIP == nil {
		return CIDRRecord{}, ValidationInvalidSrcIP, fmt.Errorf("src_ip %q is not an IPv4 address", srcIPRaw)
	}
	dstIP := net.ParseIP(dstIPRaw)
	if dstIP == nil {
		return CIDRRecord{}, ValidationInvalidDstIP, fmt.Errorf("invalid dst_ip %q", dstIPRaw)
	}
	dstIP = dstIP.To4()
	if dstIP == nil {
		return CIDRRecord{}, ValidationInvalidDstIP, fmt.Errorf("dst_ip %q is not an IPv4 address", dstIPRaw)
	}

	_, srcSeg, err := net.ParseCIDR(srcSegRaw)
	if err != nil {
		return CIDRRecord{}, ValidationInvalidSrcSegment, fmt.Errorf("invalid src_network_segment %q", srcSegRaw)
	}
	if srcSeg.IP.To4() == nil {
		return CIDRRecord{}, ValidationInvalidSrcSegment, fmt.Errorf("src_network_segment %q is not an IPv4 address", srcSegRaw)
	}
	_, dstSeg, err := net.ParseCIDR(dstSegRaw)
	if err != nil {
		return CIDRRecord{}, ValidationInvalidDstSegment, fmt.Errorf("invalid dst_network_segment %q", dstSegRaw)
	}
	if dstSeg.IP.To4() == nil {
		return CIDRRecord{}, ValidationInvalidDstSegment, fmt.Errorf("dst_network_segment %q is not an IPv4 address", dstSegRaw)
	}
	if !srcSeg.Contains(srcIP) {
		return CIDRRecord{}, ValidationSrcContainmentFail, fmt.Errorf("src_ip %s not in src_network_segment %s", srcIP.String(), srcSeg.String())
	}
	if !dstSeg.Contains(dstIP) {
		return CIDRRecord{}, ValidationDstContainmentFail, fmt.Errorf("dst_ip %s not in dst_network_segment %s", dstIP.String(), dstSeg.String())
	}

	protocol := strings.ToLower(protocolRaw)
	if protocol != "tcp" {
		return CIDRRecord{}, ValidationInvalidProtocol, fmt.Errorf("invalid protocol %q", protocolRaw)
	}
	decision := strings.ToLower(decisionRaw)
	if decision != "accept" && decision != "deny" {
		return CIDRRecord{}, ValidationInvalidDecision, fmt.Errorf("invalid decision %q", decisionRaw)
	}
	port, err := strconv.Atoi(portRaw)
	if err != nil || port < 1 || port > 65535 {
		return CIDRRecord{}, ValidationInvalidPort, fmt.Errorf("invalid port %q", portRaw)
	}
	key, err := netutil.BuildExecutionKey(dstIP.String(), port, protocol)
	if err != nil {
		return CIDRRecord{}, ValidationInvalidPort, err
	}

	rec := CIDRRecord{
		FabName:             srcIP.String(),
		CIDRName:            serviceLabel,
		IPRaw:               dstIP.String(),
		IPCidrRaw:           dstSeg.String(),
		RowNumber:           rowNumber,
		IPColName:           RichFieldDstIP,
		IPCidrCol:           RichFieldDstNetworkSegment,
		IsRich:              true,
		IsValid:             true,
		SrcIP:               srcIP.String(),
		SrcNetworkSegment:   srcSeg.String(),
		DstIP:               dstIP.String(),
		DstNetworkSegment:   dstSeg.String(),
		ServiceLabel:        serviceLabel,
		Protocol:            protocol,
		Port:                port,
		Decision:            decision,
		PolicyID:            policyID,
		Reason:              reason,
		ExecutionKey:        key,
		RichInputIdentifier: fmt.Sprintf("row:%d", rowNumber),
	}
	if err := rec.Parse(); err != nil {
		return CIDRRecord{}, ValidationInvalidDstIP, err
	}
	return rec, "", nil
}

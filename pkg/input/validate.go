package input

import (
	"fmt"
	"net"

	"github.com/xuxiping/port-scan-mk3/pkg/netutil"
)

// ValidateIPRows enforces fail-fast input rules on a slice of CIDR records:
//
//  1. Each IP selector is contained within its ip_cidr boundary.
//  2. Duplicate (src, dst, ip_cidr, port) tuples are rejected.
//
// Callers should apply this after loading records via LoadCIDRs or
// LoadCIDRsWithColumns. It does not mutate the records.
//
// # Parameters
//
//	rows: Parsed CIDR records to validate.
//
// # Returns
//
//	nil on success; an error describing the first violation found.
//	Errors include row numbers (1-indexed) for duplicate or containment failures.
//
// # Example
//
//	records, _ := input.LoadCIDRs(os.Stdin)
//	if err := input.ValidateIPRows(records); err != nil {
//	    log.Fatalf("validation failed: %v", err)
//	}
func ValidateIPRows(rows []CIDRRecord) error {
	for i := range rows {
		if rows[i].Net == nil || rows[i].Selector == nil {
			return fmt.Errorf("row %d is not parsed", i+1)
		}
	}

	seenPair := make(map[string]int, len(rows))
	for i, row := range rows {
		key := duplicateRowKey(row)
		if prev, ok := seenPair[key]; ok {
			src, dst := duplicateTupleSrcDst(row)
			return fmt.Errorf(
				"duplicate (src,dst,ip_cidr,port) found at rows %d and %d: (%s,%s,%s,%d)",
				prev, i+1, src, dst, row.Net.String(), row.Port,
			)
		}
		seenPair[key] = i + 1
	}

	for i := 0; i < len(rows); i++ {
		if !networkContains(rows[i].Net, rows[i].Selector) {
			return fmt.Errorf("ip selector %s is outside ip_cidr %s (row %d)", rows[i].Selector.String(), rows[i].Net.String(), i+1)
		}
	}

	return nil
}

func duplicateRowKey(row CIDRRecord) string {
	src, dst := duplicateTupleSrcDst(row)
	return fmt.Sprintf("%s|%s|%s|%d", row.Net.String(), src, dst, row.Port)
}

func networkContains(outer, inner *net.IPNet) bool {
	if outer == nil || inner == nil {
		return false
	}
	innerStart, innerEnd, ok := netutil.IPRange(inner)
	if !ok {
		return false
	}
	return outer.Contains(innerStart) && outer.Contains(innerEnd)
}

func duplicateTupleSrcDst(row CIDRRecord) (src string, dst string) {
	src = row.SrcIP
	dst = row.DstIP
	if src == "" && row.Selector != nil {
		src = row.Selector.String()
	}
	if dst == "" && row.Selector != nil {
		dst = row.Selector.String()
	}
	return src, dst
}

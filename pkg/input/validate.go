package input

import (
	"fmt"
	"net"

	"github.com/xuxiping/port-scan-mk3/pkg/netutil"
)

// ValidateNoOverlap validates CIDR and IP selector rows, and rejects conflicting
// ranges. It is an alias for ValidateIPRows. The two functions do the same
// validation.
func ValidateNoOverlap(networks []CIDRRecord) error {
	return ValidateIPRows(networks)
}

// ValidateIPRows enforces fail-fast input rules on a slice of CIDR records:
//
//  1. Each IP selector must be inside its ip_cidr boundary.
//  2. A duplicate (src, dst, ip_cidr, port) tuple is an error.
//
// The caller must call ValidateIPRows after LoadCIDRs or LoadCIDRsWithColumns
// loads the records. ValidateIPRows does not mutate the records.
//
// # Parameters
//
//	rows: Parsed CIDR records to validate.
//
// # Returns
//
//	nil on success. An error that describes the first violation.
//	An error for a duplicate or for a containment failure includes the row
//	numbers, which are 1-indexed.
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

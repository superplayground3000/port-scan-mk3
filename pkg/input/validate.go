package input

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
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
	return ValidateIPRowsContext(context.Background(), rows)
}

// ValidateIPRowsContext validates CIDR records and stops within one row when
// ctx is canceled.
func ValidateIPRowsContext(ctx context.Context, rows []CIDRRecord) error {
	var basicCount, richCount int
	for i := range rows {
		if err := ctx.Err(); err != nil {
			return err
		}
		if rows[i].Net == nil || rows[i].Selector == nil {
			return fmt.Errorf("row %d is not parsed", i+1)
		}
		if rows[i].SrcIP == "" && rows[i].DstIP == "" {
			basicCount++
		} else {
			richCount++
		}
	}

	seenBasic, err := newBasicDuplicateTable(basicCount)
	if err != nil {
		return err
	}
	seenRich := make(map[richDuplicateKey]int, richCount)
	for i, row := range rows {
		if err := ctx.Err(); err != nil {
			return err
		}
		if row.SrcIP == "" && row.DstIP == "" {
			if prev, duplicate := seenBasic.add(rows, basicDuplicateRowKey(row), i+1); duplicate {
				return duplicateRowError(row, prev, i+1)
			}
		} else {
			key := richDuplicateRowKey(row)
			if prev, duplicate := seenRich[key]; duplicate {
				return duplicateRowError(row, prev, i+1)
			}
			seenRich[key] = i + 1
		}
	}

	for i := 0; i < len(rows); i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !networkContains(rows[i].Net, rows[i].Selector) {
			return fmt.Errorf("ip selector %s is outside ip_cidr %s (row %d)", rows[i].Selector.String(), rows[i].Net.String(), i+1)
		}
	}

	return nil
}

type basicDuplicateKey struct {
	boundary       uint32
	selector       uint32
	port           uint16
	prefix         uint8
	selectorPrefix uint8
}

type basicDuplicateSlot struct {
	hash uint32
	row  uint32
}

type basicDuplicateTable struct {
	slots    []basicDuplicateSlot
	fallback map[basicDuplicateKey]int
	mask     uint64
}

func newBasicDuplicateTable(count int) (basicDuplicateTable, error) {
	if count == 0 {
		return basicDuplicateTable{}, nil
	}
	if uint64(count) > math.MaxUint32 {
		return basicDuplicateTable{fallback: make(map[basicDuplicateKey]int, count)}, nil
	}
	capacity := 1
	for capacity < count+count/4 {
		if capacity > int(^uint(0)>>1)/2 {
			return basicDuplicateTable{}, fmt.Errorf("CIDR duplicate table size exceeds the addressable range")
		}
		capacity *= 2
	}
	return basicDuplicateTable{slots: make([]basicDuplicateSlot, capacity), mask: uint64(capacity - 1)}, nil
}

func (table basicDuplicateTable) add(rows []CIDRRecord, key basicDuplicateKey, row int) (int, bool) {
	if table.fallback != nil {
		previous, duplicate := table.fallback[key]
		table.fallback[key] = row
		return previous, duplicate
	}
	hash := basicDuplicateHash(key)
	index := uint64(hash) & table.mask
	for {
		slot := &table.slots[index]
		if slot.row == 0 {
			slot.hash = hash
			slot.row = uint32(row)
			return 0, false
		}
		if slot.hash == hash && basicDuplicateRowKey(rows[slot.row-1]) == key {
			return int(slot.row), true
		}
		index = (index + 1) & table.mask
	}
}

func basicDuplicateHash(key basicDuplicateKey) uint32 {
	value := uint64(key.boundary)<<32 | uint64(key.selector)
	value ^= uint64(key.port)<<16 | uint64(key.prefix)<<8 | uint64(key.selectorPrefix)
	value ^= value >> 33
	value *= 0xff51afd7ed558ccd
	value ^= value >> 33
	return uint32(value ^ value>>32)
}

func basicDuplicateRowKey(row CIDRRecord) basicDuplicateKey {
	key := basicDuplicateKey{
		boundary: binary.BigEndian.Uint32(row.Net.IP.To4()),
		selector: binary.BigEndian.Uint32(row.Selector.IP.To4()),
		port:     uint16(row.Port),
	}
	ones, _ := row.Net.Mask.Size()
	key.prefix = uint8(ones)
	ones, _ = row.Selector.Mask.Size()
	key.selectorPrefix = uint8(ones)
	return key
}

type richDuplicateKey struct {
	boundary uint32
	src      string
	dst      string
	port     int
	prefix   uint8
}

func richDuplicateRowKey(row CIDRRecord) richDuplicateKey {
	src, dst := duplicateTupleSrcDst(row)
	ones, _ := row.Net.Mask.Size()
	return richDuplicateKey{
		boundary: binary.BigEndian.Uint32(row.Net.IP.To4()),
		src:      src,
		dst:      dst,
		port:     row.Port,
		prefix:   uint8(ones),
	}
}

func duplicateRowError(row CIDRRecord, firstRow, secondRow int) error {
	src, dst := duplicateTupleSrcDst(row)
	return fmt.Errorf(
		"duplicate (src,dst,ip_cidr,port) found at rows %d and %d: (%s,%s,%s,%d)",
		firstRow, secondRow, src, dst, row.Net.String(), row.Port,
	)
}

func networkContains(outer, inner *net.IPNet) bool {
	if outer == nil || inner == nil {
		return false
	}
	if outerStart := outer.IP.To4(); outerStart != nil {
		innerStart := inner.IP.To4()
		outerPrefix, outerBits := outer.Mask.Size()
		innerPrefix, innerBits := inner.Mask.Size()
		if innerStart == nil || outerBits != 32 || innerBits != 32 || innerPrefix < outerPrefix {
			return false
		}
		mask := binary.BigEndian.Uint32(outer.Mask)
		boundary := binary.BigEndian.Uint32(outerStart) & mask
		return binary.BigEndian.Uint32(innerStart)&mask == boundary
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

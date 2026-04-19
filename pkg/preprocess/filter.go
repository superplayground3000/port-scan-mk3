// Package preprocess filters rich CSV rows by removing targets whose
// destination network segment falls within a closed CIDR.
package preprocess

import (
	"fmt"

	"github.com/xuxiping/port-scan-mk3/pkg/cidrutil"
)

// Filter checks whether targets should be kept or dropped based on a tree
// of closed CIDRs.
type Filter struct {
	closedTree *cidrutil.IntervalTree
}

// NewFilter creates a Filter from an IntervalTree of closed CIDRs.
// If closedTree is nil, an empty tree is substituted to avoid nil-pointer panics.
func NewFilter(closedTree *cidrutil.IntervalTree) *Filter {
	if closedTree == nil {
		closedTree = &cidrutil.IntervalTree{}
	}
	return &Filter{closedTree: closedTree}
}

// Keep returns true if dstNetworkSegment is NOT contained within any closed CIDR.
// Returns an error if the segment string cannot be parsed.
func (f *Filter) Keep(dstNetworkSegment string) (bool, error) {
	entry, err := cidrutil.ParseCIDR(dstNetworkSegment)
	if err != nil {
		return false, fmt.Errorf("parsing dst_network_segment %q: %w", dstNetworkSegment, err)
	}
	matches := f.closedTree.Query(entry)
	return len(matches) == 0, nil
}

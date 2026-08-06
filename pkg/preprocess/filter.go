// Package preprocess filters rich CSV rows. It removes each target whose
// destination network segment is inside a closed CIDR.
package preprocess

import (
	"fmt"

	"github.com/xuxiping/port-scan-mk3/pkg/cidrutil"
)

// Filter uses a tree of closed CIDRs to decide which targets to keep and which
// targets to drop.
type Filter struct {
	closedTree *cidrutil.IntervalTree
}

// NewFilter creates a Filter from an IntervalTree of closed CIDRs.
// If closedTree is nil, NewFilter substitutes an empty tree, which prevents a
// nil-pointer panic.
func NewFilter(closedTree *cidrutil.IntervalTree) *Filter {
	if closedTree == nil {
		closedTree = &cidrutil.IntervalTree{}
	}
	return &Filter{closedTree: closedTree}
}

// Keep returns true when NO closed CIDR contains dstNetworkSegment.
// If Keep cannot parse the segment string, it returns an error.
func (f *Filter) Keep(dstNetworkSegment string) (bool, error) {
	entry, err := cidrutil.ParseCIDR(dstNetworkSegment)
	if err != nil {
		return false, fmt.Errorf("parsing dst_network_segment %q: %w", dstNetworkSegment, err)
	}
	matches := f.closedTree.Query(entry)
	return len(matches) == 0, nil
}

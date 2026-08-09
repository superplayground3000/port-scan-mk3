package scanapp

import (
	"github.com/xuxiping/port-scan-mk3/pkg/ratelimit"
	"github.com/xuxiping/port-scan-mk3/pkg/task"
)

type targetMeta struct {
	fabName           string
	cidrName          string
	serviceLabel      string
	decision          string
	policyID          string
	reason            string
	executionKey      string
	srcIP             string
	srcNetworkSegment string
}

type chunkRuntime struct {
	ipCidr  string
	ports   []int
	targets []scanTarget
	state   *task.Chunk
	tracker *chunkStateTracker
	bkt     *ratelimit.LeakyBucket
}

type scanTarget struct {
	ip     string
	ipCidr string
	// ipU32 is the target IP parsed once at creation time (big-endian uint32 of
	// the IPv4 bytes, 0 for non-IPv4) so group sorts never re-parse per
	// comparison. See design.md §3.3.
	ipU32 uint32
	port  int
	meta  targetMeta
}

type scanTask struct {
	chunkIdx int
	taskIdx  int
	ipCidr   string
	ip       string
	port     int
	meta     targetMeta
}

type scanResult struct {
	chunkIdx int
	taskIdx  int
	record   ScanRecord
}

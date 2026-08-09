// Package task indexes tasks, expands IP selectors, and generates execution keys
// for the port scanner.
//
// # Function Flow
//
//	[]input.CIDRRecord
//	  |
//	  v
//	buildCIDRGroups / buildRichGroups  (group_builder.go)
//	  |
//	  v
//	ExpandIPSelectors  (selector_expand.go)  ── expands CIDR → []IP
//	  |
//	  v
//	[]scanTarget with execution keys
//	  |
//	  v
//	IndexToTarget / indexToRuntimeTarget  (index.go)
//	  |
//	  v
//	Dispatched scan tasks
//
// # Example
//
//	ips, _ := task.ExpandIPSelectors([]string{"192.168.1.0/30"})
//	key, _ := task.BuildExecutionKey("192.168.1.1", 80, "tcp")
package task

// Chunk represents a resumable scan unit for one CIDR and its port list. The
// scanner serializes a Chunk to JSON to persist the resume state.
type Chunk struct {
	// CIDR is the boundary network, for example "10.0.0.0/8".
	CIDR string `json:"cidr"`
	// CIDRName is a human-readable name for this CIDR.
	CIDRName string `json:"cidr_name"`
	// Ports is the list of port specs in `<port>/tcp` format.
	Ports []string `json:"ports"`
	// NextIndex is the index of the next task to dispatch. The resume logic uses
	// it.
	NextIndex int `json:"next_index"`
	// ScannedCount is the number of persisted task results credited to progress.
	// A write-failure rewind sets it to the safe dispatch cursor.
	ScannedCount int `json:"scanned_count"`
	// TotalCount is the total number of scan tasks for this chunk (targets × ports).
	TotalCount int `json:"total_count"`
	// Status is "pending", "scanning", or "completed".
	Status string `json:"status"`
}

// Task represents a single scan task for one combination of an IP and a port.
type Task struct {
	ChunkCIDR string
	IP        string
	Port      int
	Index     int
}

// Remaining returns the number of scan tasks not yet dispatched for this chunk.
func (c Chunk) Remaining() int {
	if c.TotalCount <= c.NextIndex {
		return 0
	}
	return c.TotalCount - c.NextIndex
}

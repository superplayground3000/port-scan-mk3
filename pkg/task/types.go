// Package task provides task indexing, IP selector expansion, and execution key
// generation for the port scanner.
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

// Chunk represents a resumable scan unit scoped to one CIDR and its port list.
// It is serialized to JSON for resume state persistence.
type Chunk struct {
	// CIDR is the boundary network (e.g., "10.0.0.0/8").
	CIDR string `json:"cidr"`
	// CIDRName is a human-readable name for this CIDR.
	CIDRName string `json:"cidr_name"`
	// Ports is the list of port specs in `<port>/tcp` format.
	Ports []string `json:"ports"`
	// NextIndex is the next task index to dispatch (used for resume).
	NextIndex int `json:"next_index"`
	// ScannedCount is the number of tasks that have produced a result.
	ScannedCount int `json:"scanned_count"`
	// TotalCount is the total number of scan tasks for this chunk (targets × ports).
	TotalCount int `json:"total_count"`
	// Status is "pending", "scanning", or "completed".
	Status string `json:"status"`
}

// Task represents a single scan task targeting one IP and port combination.
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

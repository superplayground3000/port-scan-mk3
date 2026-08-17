package scanapp

import (
	"sync"

	"github.com/xuxiping/port-scan-mk3/pkg/task"
)

type chunkStateTracker struct {
	mu             sync.Mutex
	chunk          *task.Chunk
	firstUnwritten int
}

func newChunkStateTracker(ch *task.Chunk) *chunkStateTracker {
	return &chunkStateTracker{chunk: ch, firstUnwritten: -1}
}

func (t *chunkStateTracker) AdvanceNextIndex(i int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.chunk.NextIndex = i
	t.updateStatus()
}

func (t *chunkStateTracker) IncrementScanned() {
	t.IncrementScannedBy(1)
}

func (t *chunkStateTracker) IncrementScannedBy(count int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.chunk.ScannedCount += count
	t.updateStatus()
}

func (t *chunkStateTracker) MarkUnwritten(taskIdx int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.firstUnwritten < 0 || taskIdx < t.firstUnwritten {
		t.firstUnwritten = taskIdx
	}
}

func (t *chunkStateTracker) RewindUnwritten() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.firstUnwritten < 0 {
		return false
	}
	t.chunk.NextIndex = t.firstUnwritten
	t.chunk.ScannedCount = t.chunk.NextIndex
	t.firstUnwritten = -1
	if t.chunk.NextIndex == 0 {
		t.chunk.Status = "pending"
	} else {
		t.updateStatus()
	}
	return true
}

func (t *chunkStateTracker) Snapshot() task.Chunk {
	t.mu.Lock()
	defer t.mu.Unlock()
	return *t.chunk
}

func (t *chunkStateTracker) ScannedCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.chunk.ScannedCount
}

func (t *chunkStateTracker) TotalCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.chunk.TotalCount
}

func (t *chunkStateTracker) CIDR() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.chunk.CIDR
}

func (t *chunkStateTracker) Status() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.chunk.Status
}

func (t *chunkStateTracker) updateStatus() {
	if t.chunk.ScannedCount >= t.chunk.TotalCount {
		t.chunk.Status = "completed"
	} else if t.chunk.ScannedCount > 0 || t.chunk.NextIndex > 0 {
		t.chunk.Status = "scanning"
	}
}

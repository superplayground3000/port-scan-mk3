package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/state"
	"github.com/xuxiping/port-scan-mk3/pkg/task"
)

func partialChunk() task.Chunk {
	return task.Chunk{
		CIDR:         "172.28.0.0/24",
		CIDRName:     "mock-target-fail",
		Ports:        []string{"8080/tcp"},
		NextIndex:    4,
		ScannedCount: 3,
		TotalCount:   254,
		Status:       "scanning",
	}
}

func writeSnapshot(t *testing.T, snap state.Snapshot) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "resume_state.json")
	if err := state.SaveSnapshot(path, snap); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}
	return path
}

func TestAssertSnapshot_AcceptsAPartiallyDispatchedSnapshot(t *testing.T) {
	snap := state.Snapshot{Chunks: []task.Chunk{partialChunk()}}

	if err := assertSnapshot(snap, checks{requireRemaining: true}); err != nil {
		t.Fatalf("assertSnapshot: unexpected error: %v", err)
	}
}

func TestAssertSnapshot_RejectsMalformedSnapshots(t *testing.T) {
	tests := []struct {
		name  string
		snap  state.Snapshot
		want  string
		match string
	}{
		{
			name: "no chunks at all",
			snap: state.Snapshot{},
			want: "chunk",
		},
		{
			name: "chunk with no work in it",
			snap: state.Snapshot{Chunks: []task.Chunk{func() task.Chunk {
				c := partialChunk()
				c.TotalCount = 0
				c.NextIndex = 0
				c.ScannedCount = 0
				return c
			}()}},
			want: "total_count",
		},
		{
			name: "next_index past total_count",
			snap: state.Snapshot{Chunks: []task.Chunk{func() task.Chunk {
				c := partialChunk()
				c.NextIndex = c.TotalCount + 1
				return c
			}()}},
			want: "next_index",
		},
		{
			name: "negative next_index",
			snap: state.Snapshot{Chunks: []task.Chunk{func() task.Chunk {
				c := partialChunk()
				c.NextIndex = -1
				return c
			}()}},
			want: "next_index",
		},
		{
			name: "scanned_count past total_count",
			snap: state.Snapshot{Chunks: []task.Chunk{func() task.Chunk {
				c := partialChunk()
				c.ScannedCount = c.TotalCount + 1
				return c
			}()}},
			want: "scanned_count",
		},
		{
			name: "unknown status",
			snap: state.Snapshot{Chunks: []task.Chunk{func() task.Chunk {
				c := partialChunk()
				c.Status = "halfway"
				return c
			}()}},
			want: "status",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := assertSnapshot(tc.snap, checks{})
			if err == nil {
				t.Fatalf("assertSnapshot(%s): got nil error, want a rejection", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("assertSnapshot(%s): error %q does not mention %q", tc.name, err, tc.want)
			}
		})
	}
}

// The point of the e2e assertion is that pressure control aborted a scan with
// work still pending. A snapshot whose chunks are all fully dispatched proves
// the scan actually FINISHED, which is the issue-#71 failure mode.
func TestAssertSnapshot_RequireRemaining_RejectsAFullyDispatchedSnapshot(t *testing.T) {
	chunk := partialChunk()
	chunk.NextIndex = chunk.TotalCount
	chunk.ScannedCount = chunk.TotalCount
	chunk.Status = "completed"
	snap := state.Snapshot{Chunks: []task.Chunk{chunk}}

	if err := assertSnapshot(snap, checks{}); err != nil {
		t.Fatalf("assertSnapshot without -require-remaining: unexpected error: %v", err)
	}

	err := assertSnapshot(snap, checks{requireRemaining: true})
	if err == nil {
		t.Fatal("assertSnapshot with -require-remaining: got nil error, want a rejection")
	}
	if !strings.Contains(err.Error(), "remaining") {
		t.Errorf("error %q does not mention remaining work", err)
	}
}

// generate-buckets writes next_index=0 and the aborted scan writes its progress
// back to the SAME path, so "the file exists" proves nothing on its own — the
// input snapshot is always there. Requiring next_index>0 is what actually
// proves the aborted run persisted its cursor.
func TestAssertSnapshot_RequireProgress_RejectsAnUntouchedSnapshot(t *testing.T) {
	chunk := partialChunk()
	chunk.NextIndex = 0
	chunk.ScannedCount = 0
	chunk.Status = "pending"
	snap := state.Snapshot{Chunks: []task.Chunk{chunk}}

	if err := assertSnapshot(snap, checks{requireRemaining: true}); err != nil {
		t.Fatalf("assertSnapshot without -require-progress: unexpected error: %v", err)
	}

	err := assertSnapshot(snap, checks{requireProgress: true})
	if err == nil {
		t.Fatal("assertSnapshot with -require-progress: got nil error, want a rejection")
	}
	if !strings.Contains(err.Error(), "progress") {
		t.Errorf("error %q does not mention persisted progress", err)
	}
}

func TestAssertSnapshot_RequireProgress_AcceptsAdvancedCursor(t *testing.T) {
	snap := state.Snapshot{Chunks: []task.Chunk{partialChunk()}}

	if err := assertSnapshot(snap, checks{requireProgress: true, requireRemaining: true}); err != nil {
		t.Fatalf("assertSnapshot: unexpected error: %v", err)
	}
}

func TestRun_AcceptsASnapshotWrittenByTheStatePackage(t *testing.T) {
	path := writeSnapshot(t, state.Snapshot{Chunks: []task.Chunk{partialChunk()}})

	if err := run(path, checks{requireProgress: true, requireRemaining: true}); err != nil {
		t.Fatalf("run(%s): unexpected error: %v", path, err)
	}
}

func TestRun_RejectsAMissingOrUnreadableFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist.json")

	err := run(missing, checks{})
	if err == nil {
		t.Fatal("run: got nil error for a missing snapshot, want a rejection")
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("error %q does not name the offending path", err)
	}
}

func TestRun_RejectsAnEmptyFileFlag(t *testing.T) {
	err := run("  ", checks{})
	if err == nil {
		t.Fatal("run: got nil error for an empty -file, want a rejection")
	}
	if !strings.Contains(err.Error(), "-file") {
		t.Errorf("error %q does not mention the -file flag", err)
	}
}

func TestRun_RejectsAFileThatIsNotASnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "garbage.json")
	if err := os.WriteFile(path, []byte("not json at all"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	if err := run(path, checks{}); err == nil {
		t.Fatal("run: got nil error for a non-snapshot file, want a rejection")
	}
}

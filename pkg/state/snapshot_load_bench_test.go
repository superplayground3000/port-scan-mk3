package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/task"
)

const (
	benchmarkSnapshotChunks     = 4000
	benchmarkUnreachableIPv4U32 = 42587
)

func benchmarkChunks() []task.Chunk {
	chunks := make([]task.Chunk, benchmarkSnapshotChunks)
	for i := range chunks {
		chunks[i] = task.Chunk{
			CIDR:         fmt.Sprintf("10.%d.%d.0/24", i/256, i%256),
			CIDRName:     "benchmark",
			Ports:        []string{"80/tcp"},
			NextIndex:    1,
			ScannedCount: 1,
			TotalCount:   254,
			Status:       "in_progress",
		}
	}
	return chunks
}

func benchmarkUnreachableIPv4() []uint32 {
	values := make([]uint32, benchmarkUnreachableIPv4U32)
	for i := range values {
		values[i] = uint32(i + 1)
	}
	return values
}

func BenchmarkLoadSnapshot(b *testing.B) {
	chunks := benchmarkChunks()
	dir := b.TempDir()

	currentPath := filepath.Join(dir, "current.json")
	if err := SaveSnapshot(currentPath, Snapshot{
		Chunks: chunks,
		PreScanPing: PreScanPingState{
			Enabled:            true,
			TimeoutMS:          100,
			UnreachableIPv4U32: benchmarkUnreachableIPv4(),
		},
	}); err != nil {
		b.Fatalf("SaveSnapshot() error = %v", err)
	}

	legacyPath := filepath.Join(dir, "legacy.json")
	legacyJSON, err := json.Marshal(chunks)
	if err != nil {
		b.Fatalf("json.Marshal() error = %v", err)
	}
	if err := os.WriteFile(legacyPath, legacyJSON, 0o644); err != nil {
		b.Fatalf("os.WriteFile() error = %v", err)
	}

	for _, tc := range []struct {
		name string
		path string
	}{
		{name: "current_4000_chunks_42587_unreachable", path: currentPath},
		{name: "legacy_4000_chunks", path: legacyPath},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := LoadSnapshot(tc.path); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

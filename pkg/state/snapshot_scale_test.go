//go:build linux && !race

package state

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/task"
)

func TestSnapshotLoadAndSaveAllocationGrowthIsLinear(t *testing.T) {
	small := scaleSnapshot(158_698)
	large := scaleSnapshot(1_388_861)
	dir := t.TempDir()
	smallPath := filepath.Join(dir, "small.json")
	largePath := filepath.Join(dir, "large.json")
	if err := SaveSnapshotWithLimits(smallPath, small, SnapshotLimits{}); err != nil {
		t.Fatal(err)
	}
	if err := SaveSnapshotWithLimits(largePath, large, SnapshotLimits{}); err != nil {
		t.Fatal(err)
	}

	for _, operation := range []struct {
		name  string
		small func()
		large func()
	}{
		{
			name: "load",
			small: func() {
				loaded, err := LoadSnapshotWithLimits(smallPath, SnapshotLimits{})
				if err != nil {
					t.Fatal(err)
				}
				runtime.KeepAlive(loaded)
			},
			large: func() {
				loaded, err := LoadSnapshotWithLimits(largePath, SnapshotLimits{})
				if err != nil {
					t.Fatal(err)
				}
				runtime.KeepAlive(loaded)
			},
		},
		{
			name:  "save",
			small: func() { saveScaleSnapshot(t, filepath.Join(dir, "small-save.json"), small) },
			large: func() { saveScaleSnapshot(t, filepath.Join(dir, "large-save.json"), large) },
		},
	} {
		t.Run(operation.name, func(t *testing.T) {
			smallBytes := allocatedDuring(operation.small)
			largeBytes := allocatedDuring(operation.large)
			t.Logf("allocated bytes: small=%d large=%d ratio=%.3f", smallBytes, largeBytes, float64(largeBytes)/float64(smallBytes))
			if largeBytes > smallBytes*11 {
				t.Fatalf("ten-fold data allocated %d bytes after %d bytes at the small scale", largeBytes, smallBytes)
			}
		})
	}
}

func scaleSnapshot(entries int) Snapshot {
	unreachable := make([]uint32, entries)
	for index := range unreachable {
		unreachable[index] = uint32(index)
	}
	return Snapshot{
		Chunks:      []task.Chunk{{CIDR: "127.0.0.1/32", Ports: []string{"80/tcp"}, TotalCount: 1, Status: "pending"}},
		PreScanPing: PreScanPingState{Enabled: true, UnreachableIPv4U32: unreachable},
	}
}

func saveScaleSnapshot(t *testing.T, path string, snapshot Snapshot) {
	t.Helper()
	if err := SaveSnapshotWithLimits(path, snapshot, SnapshotLimits{}); err != nil {
		t.Fatal(err)
	}
}

func allocatedDuring(action func()) uint64 {
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	action()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

func TestSaveSnapshotBytesRemainStable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snapshot.json")
	snapshot := Snapshot{Chunks: []task.Chunk{{
		CIDR: "10.0.0.0/30", CIDRName: "edge", Ports: []string{"80/tcp"}, TotalCount: 2, Status: "pending",
	}}}
	if err := SaveSnapshotWithLimits(path, snapshot, SnapshotLimits{}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"chunks\": [\n    {\n      \"cidr\": \"10.0.0.0/30\",\n      \"cidr_name\": \"edge\",\n      \"ports\": [\n        \"80/tcp\"\n      ],\n      \"next_index\": 0,\n      \"scanned_count\": 0,\n      \"total_count\": 2,\n      \"status\": \"pending\"\n    }\n  ]\n}"
	if string(got) != want {
		t.Fatalf("snapshot bytes changed:\n%s", got)
	}
}

func TestChunkHeavySnapshotLoadAllocationGrowthIsLinear(t *testing.T) {
	dir := t.TempDir()
	smallPath := filepath.Join(dir, "small-chunks.json")
	largePath := filepath.Join(dir, "large-chunks.json")
	writeChunkHeavySnapshot(t, smallPath, 10_000)
	writeChunkHeavySnapshot(t, largePath, 100_000)

	smallBytes := allocatedDuring(func() { loadScaleSnapshot(t, smallPath) })
	largeBytes := allocatedDuring(func() { loadScaleSnapshot(t, largePath) })
	t.Logf("allocated bytes: small=%d large=%d ratio=%.3f", smallBytes, largeBytes, float64(largeBytes)/float64(smallBytes))
	if largeBytes > smallBytes*11 {
		t.Fatalf("ten-fold chunk data allocated %d bytes after %d bytes at the small scale", largeBytes, smallBytes)
	}
}

func writeChunkHeavySnapshot(t *testing.T, path string, count int) {
	t.Helper()
	chunks := make([]task.Chunk, count)
	for index := range chunks {
		chunks[index] = task.Chunk{CIDR: "127.0.0.1/32", Ports: []string{"80/tcp"}, TotalCount: 1, Status: "pending"}
	}
	if err := SaveSnapshotWithLimits(path, Snapshot{Chunks: chunks}, SnapshotLimits{}); err != nil {
		t.Fatal(err)
	}
}

func loadScaleSnapshot(t *testing.T, path string) {
	t.Helper()
	loaded, err := LoadSnapshotWithLimits(path, SnapshotLimits{})
	if err != nil {
		t.Fatal(err)
	}
	runtime.KeepAlive(loaded)
}

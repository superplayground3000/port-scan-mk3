//go:build linux && !race

package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
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

func TestSaveSnapshotBytesMatchMarshalIndentForAllFields(t *testing.T) {
	tests := []Snapshot{
		{},
		{Chunks: []task.Chunk{}},
		{
			Chunks: []task.Chunk{{
				CIDR: "10.0.0.0/30", CIDRName: "edge\nname", Ports: []string{"80/tcp", "443/tcp"},
				NextIndex: 1, ScannedCount: 2, TotalCount: 3, Status: "scan\"ning",
			}},
			PreScanPing:            PreScanPingState{Enabled: true, TimeoutMS: 42, UnreachableIPv4U32: []uint32{0, 4_294_967_295}},
			Output:                 &OutputState{ScanPath: "scan.csv", OpenPath: "open.csv"},
			RichDenyExcluded:       true,
			TargetExpansion:        &TargetExpansionState{CandidateCount: 9, CandidateLimit: -1, MemoryLimitGB: 16},
			TargetSemanticsVersion: 1,
			BasicPortFallback:      []string{"22/tcp", "8080/tcp"},
		},
	}
	for index, snapshot := range tests {
		path := filepath.Join(t.TempDir(), "snapshot.json")
		if err := SaveSnapshotWithLimits(path, snapshot, SnapshotLimits{}); err != nil {
			t.Fatalf("case %d: SaveSnapshotWithLimits() error = %v", index, err)
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		want, err := json.MarshalIndent(snapshotEnvelopeFromSnapshot(snapshot), "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("case %d bytes changed:\n got:\n%s\nwant:\n%s", index, got, want)
		}
	}
}

func TestSnapshotEncoderFieldListsMatchPersistedStructs(t *testing.T) {
	tests := []struct {
		name   string
		typeOf reflect.Type
		want   []string
	}{
		{name: "envelope", typeOf: reflect.TypeOf(snapshotEnvelope{}), want: []string{"chunks", "pre_scan_ping", "output", "rich_deny_excluded", "target_expansion", "target_semantics_version", "basic_port_fallback"}},
		{name: "pre scan ping", typeOf: reflect.TypeOf(preScanPingEnvelope{}), want: []string{"enabled", "timeout_ms", "unreachable_ipv4_u32"}},
		{name: "chunk", typeOf: reflect.TypeOf(task.Chunk{}), want: []string{"cidr", "cidr_name", "ports", "next_index", "scanned_count", "total_count", "status"}},
		{name: "output", typeOf: reflect.TypeOf(OutputState{}), want: []string{"scan_path", "open_path"}},
		{name: "target expansion", typeOf: reflect.TypeOf(TargetExpansionState{}), want: []string{"candidate_count", "candidate_limit", "memory_limit_gb"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var got []string
			for index := 0; index < test.typeOf.NumField(); index++ {
				name := strings.Split(test.typeOf.Field(index).Tag.Get("json"), ",")[0]
				got = append(got, name)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("persisted fields = %v, encoder fields = %v", got, test.want)
			}
		})
	}
}

func TestSaveSnapshotByteLimitStopsAtLimitPlusOne(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snapshot.json")
	var written int
	withFileOps(t, func(ops *snapshotFileOps) {
		realWrite := ops.write
		ops.write = func(file *os.File, data []byte) (int, error) {
			count, err := realWrite(file, data)
			written += count
			return count, err
		}
	})

	err := SaveSnapshotWithLimits(path, scaleSnapshot(100_000), SnapshotLimits{MaxBytes: 1000})
	if err == nil {
		t.Fatal("SaveSnapshotWithLimits() accepted oversized output")
	}
	if written != 1001 {
		t.Fatalf("serialized bytes written = %d, want limit+1", written)
	}
	assertNoTempFilesBesideSnapshot(t, path)
}

func TestSaveSnapshotZeroByteLimitWritesCompleteSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snapshot.json")
	want := scaleSnapshot(10_000)
	if err := SaveSnapshotWithLimits(path, want, SnapshotLimits{}); err != nil {
		t.Fatalf("SaveSnapshotWithLimits() error = %v", err)
	}
	got, err := LoadSnapshotWithLimits(path, SnapshotLimits{})
	if err != nil {
		t.Fatalf("LoadSnapshotWithLimits() error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatal("snapshot changed after byte-limit bypass")
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

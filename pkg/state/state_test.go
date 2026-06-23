package state

import (
	"os"
	"reflect"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/task"
)

func TestSaveAndLoad_WhenStatePersisted_RestoresChunkFields(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "resume_state.json")

	chunks := []task.Chunk{{CIDR: "10.0.0.0/30", NextIndex: 2, TotalCount: 8, Status: "scanning"}}
	if err := Save(file, chunks); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	got, err := Load(file)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if got[0].NextIndex != 2 {
		t.Fatalf("next index mismatch: %d", got[0].NextIndex)
	}
}

func TestSave_WhenPreScanPingAbsent_OmitsFieldFromJSON(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "resume_state.json")

	chunks := []task.Chunk{{CIDR: "10.0.0.0/30", NextIndex: 2, TotalCount: 8, Status: "scanning"}}
	if err := Save(file, chunks); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if strings.Contains(string(data), `"pre_scan_ping"`) {
		t.Fatalf("unexpected pre_scan_ping in JSON: %s", data)
	}
}

func TestSaveAndLoadSnapshot_WhenPreScanPingStatePresent_RoundTrips(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "resume_snapshot.json")

	want := Snapshot{
		Chunks: []task.Chunk{{CIDR: "10.0.0.0/24", TotalCount: 2}},
		PreScanPing: PreScanPingState{
			Enabled:            true,
			TimeoutMS:          100,
			UnreachableIPv4U32: []uint32{167772167, 167772168},
		},
	}

	if err := SaveSnapshot(file, want); err != nil {
		t.Fatalf("save snapshot failed: %v", err)
	}

	got, err := LoadSnapshot(file)
	if err != nil {
		t.Fatalf("load snapshot failed: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("snapshot mismatch: got %+v want %+v", got, want)
	}
}

package state_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/state"
	"github.com/xuxiping/port-scan-mk3/pkg/task"
)

func TestSnapshotLimitsApplyToLoadAndAtomicSave(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "resume.json")
	previous := []byte(`{"chunks":[]}`)
	if err := os.WriteFile(path, previous, 0o644); err != nil {
		t.Fatal(err)
	}
	snapshot := state.Snapshot{
		Chunks: []task.Chunk{
			{CIDR: "192.0.2.0/24", Ports: []string{"80/tcp", "443/tcp"}},
			{CIDR: "198.51.100.0/24", Ports: []string{"8080/tcp"}},
		},
		PreScanPing: state.PreScanPingState{UnreachableIPv4U32: []uint32{1, 2}},
	}
	exact := state.SnapshotLimits{MaxBytes: 0, MaxChunks: 2, MaxPortEntries: 3, MaxUnreachableIPs: 2}
	if err := state.SaveSnapshotWithLimits(path, snapshot, exact); err != nil {
		t.Fatalf("exact save limits rejected: %v", err)
	}
	if _, err := state.LoadSnapshotWithLimits(path, exact); err != nil {
		t.Fatalf("exact load limits rejected: %v", err)
	}

	for name, limits := range map[string]state.SnapshotLimits{
		"chunks":      {MaxChunks: 1},
		"ports":       {MaxPortEntries: 2},
		"unreachable": {MaxUnreachableIPs: 1},
	} {
		t.Run(name, func(t *testing.T) {
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			err = state.SaveSnapshotWithLimits(path, snapshot, limits)
			if err == nil || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "override") {
				t.Fatalf("SaveSnapshotWithLimits() error = %v, want path and override flag", err)
			}
			after, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if string(after) != string(before) {
				t.Fatal("failed save replaced the previous snapshot")
			}
		})
	}
}

func TestSnapshotByteLimitRejectsInputAndOutput(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "resume.json")
	if err := os.WriteFile(path, []byte(`{"chunks":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := state.LoadSnapshotWithLimits(path, state.SnapshotLimits{MaxBytes: 12}); err == nil || !strings.Contains(err.Error(), "-snapshot-size-limit-gb") {
		t.Fatalf("LoadSnapshotWithLimits() error = %v, want byte-limit error", err)
	}
	before, _ := os.ReadFile(path)
	if err := state.SaveSnapshotWithLimits(path, state.Snapshot{Chunks: []task.Chunk{}}, state.SnapshotLimits{MaxBytes: 1}); err == nil {
		t.Fatal("SaveSnapshotWithLimits() accepted oversized serialized output")
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(before) {
		t.Fatal("byte-limit failure replaced the previous snapshot")
	}
}

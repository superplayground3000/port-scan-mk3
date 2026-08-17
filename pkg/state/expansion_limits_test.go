package state

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/task"
)

func TestSnapshot_TargetExpansionRoundTripAndLegacyAbsence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snapshot.json")
	want := &TargetExpansionState{
		CandidateCount: 8_388_608,
		CandidateLimit: 10_000_000,
		MemoryLimitGB:  16,
	}
	if err := SaveSnapshot(path, Snapshot{Chunks: []task.Chunk{}, TargetExpansion: want}); err != nil {
		t.Fatalf("SaveSnapshot() error = %v", err)
	}
	got, err := LoadSnapshot(path)
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	if got.TargetExpansion == nil || *got.TargetExpansion != *want {
		t.Fatalf("target expansion = %#v, want %#v", got.TargetExpansion, want)
	}

	legacyPath := filepath.Join(t.TempDir(), "legacy.json")
	if err := os.WriteFile(legacyPath, []byte(`{"chunks":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	legacy, err := LoadSnapshot(legacyPath)
	if err != nil {
		t.Fatalf("LoadSnapshot(legacy) error = %v", err)
	}
	if legacy.TargetExpansion != nil {
		t.Fatalf("legacy target expansion = %#v, want nil", legacy.TargetExpansion)
	}
}

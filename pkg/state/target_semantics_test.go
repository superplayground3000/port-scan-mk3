package state

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/task"
)

func TestSnapshot_TargetSemanticsRoundTripAndLegacyAbsence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snapshot.json")
	want := Snapshot{
		Chunks:                 []task.Chunk{},
		TargetSemanticsVersion: CurrentTargetSemanticsVersion,
		BasicPortFallback:      []string{"80/tcp", "443/tcp"},
	}
	if err := SaveSnapshot(path, want); err != nil {
		t.Fatalf("SaveSnapshot() error = %v", err)
	}
	got, err := LoadSnapshot(path)
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	if got.TargetSemanticsVersion != CurrentTargetSemanticsVersion {
		t.Fatalf("target semantics version = %d, want %d", got.TargetSemanticsVersion, CurrentTargetSemanticsVersion)
	}
	if !slices.Equal(got.BasicPortFallback, want.BasicPortFallback) {
		t.Fatalf("basic port fallback = %v, want %v", got.BasicPortFallback, want.BasicPortFallback)
	}
}

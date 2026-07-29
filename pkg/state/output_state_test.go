package state

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/task"
)

// TestSaveAndLoadSnapshot_WhenOutputStatePresent_RoundTrips proves the resume
// snapshot carries the output paths (§3.7) so a subsequent -resume appends to
// the same files rather than minting new ones.
func TestSaveAndLoadSnapshot_WhenOutputStatePresent_RoundTrips(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "resume_snapshot.json")

	want := Snapshot{
		Chunks: []task.Chunk{{CIDR: "10.0.0.0/24", TotalCount: 2}},
		Output: &OutputState{
			ScanPath: "/out/scan_results-20260101T000000Z.csv",
			OpenPath: "/out/opened_results-20260101T000000Z.csv",
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

// TestLoadSnapshot_WhenLegacyWithoutOutput_LoadsWithNilOutput proves old
// snapshots that predate the output field still load, leaving Output nil.
func TestLoadSnapshot_WhenLegacyWithoutOutput_LoadsWithNilOutput(t *testing.T) {
	file := filepath.Join(t.TempDir(), "no_output.json")
	if err := os.WriteFile(file, []byte(`{"chunks":[{"cidr":"10.0.0.0/30","total_count":4}]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := LoadSnapshot(file)
	if err != nil {
		t.Fatalf("load snapshot failed: %v", err)
	}
	if got.Output != nil {
		t.Fatalf("expected nil Output for legacy snapshot, got %+v", got.Output)
	}
	if len(got.Chunks) != 1 || got.Chunks[0].CIDR != "10.0.0.0/30" {
		t.Fatalf("unexpected chunks: %+v", got.Chunks)
	}
}

// TestSaveSnapshot_WhenOutputNil_OmitsFieldFromJSON keeps the envelope clean
// for runs that never recorded an output path.
func TestSaveSnapshot_WhenOutputNil_OmitsFieldFromJSON(t *testing.T) {
	file := filepath.Join(t.TempDir(), "omit_output.json")
	if err := SaveSnapshot(file, Snapshot{Chunks: []task.Chunk{{CIDR: "10.0.0.0/24"}}}); err != nil {
		t.Fatalf("save snapshot failed: %v", err)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); contains(got, "\"output\"") {
		t.Fatalf("expected no output field in JSON, got %s", got)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

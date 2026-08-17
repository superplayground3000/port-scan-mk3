package state

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/task"
)

func writeSnapshotFile(t *testing.T, name, data string) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(file, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	return file
}

func TestLoadSnapshot_WhenLegacyArrayHasTrailingContent_ReturnsError(t *testing.T) {
	file := writeSnapshotFile(t, "legacy_trailing.json",
		`[{"cidr":"10.0.0.0/30","next_index":1,"total_count":4,"status":"paused"}] {"chunks":[]}`)

	if _, err := LoadSnapshot(file); err == nil {
		t.Fatal("LoadSnapshot() error = nil, want an error for trailing content after the legacy array")
	}
}

// Trailing content that is not JSON takes the other branch of the check. Valid
// trailing JSON makes the second decode return nil. Invalid trailing content
// makes it return a syntax error. Both must reject the file.
func TestLoadSnapshot_WhenLegacyArrayHasTrailingGarbage_ReturnsError(t *testing.T) {
	file := writeSnapshotFile(t, "legacy_garbage.json",
		`[{"cidr":"10.0.0.0/30","next_index":1,"total_count":4,"status":"paused"}] xyz`)

	if _, err := LoadSnapshot(file); err == nil {
		t.Fatal("LoadSnapshot() error = nil, want an error for trailing garbage after the legacy array")
	}
}

func TestLoadSnapshot_WhenLegacyArrayIsValid_LoadsChunks(t *testing.T) {
	file := writeSnapshotFile(t, "legacy_valid.json",
		`[{"cidr":"10.0.0.0/30","next_index":1,"total_count":4,"status":"paused"}]`)
	want := []task.Chunk{{CIDR: "10.0.0.0/30", NextIndex: 1, TotalCount: 4, Status: "paused"}}

	got, err := LoadSnapshot(file)
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	if !reflect.DeepEqual(got.Chunks, want) {
		t.Fatalf("LoadSnapshot() chunks = %+v, want %+v", got.Chunks, want)
	}
}

func TestLoadSnapshot_WhenObjectEnvelopeHasTrailingContent_ReturnsError(t *testing.T) {
	file := writeSnapshotFile(t, "envelope_trailing.json",
		`{"chunks":[{"cidr":"10.0.0.0/30","next_index":1,"total_count":4,"status":"paused"}]} []`)

	if _, err := LoadSnapshot(file); err == nil {
		t.Fatal("LoadSnapshot() error = nil, want an error for trailing content after the object envelope")
	}
}

func TestLoadSnapshot_WhenObjectEnvelopeIsValid_LoadsChunks(t *testing.T) {
	file := writeSnapshotFile(t, "envelope_valid.json",
		`{"chunks":[{"cidr":"10.0.0.0/30","next_index":1,"total_count":4,"status":"paused"}]}`)
	want := []task.Chunk{{CIDR: "10.0.0.0/30", NextIndex: 1, TotalCount: 4, Status: "paused"}}

	got, err := LoadSnapshot(file)
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	if !reflect.DeepEqual(got.Chunks, want) {
		t.Fatalf("LoadSnapshot() chunks = %+v, want %+v", got.Chunks, want)
	}
}

package preprocess

import (
	"path/filepath"
	"testing"
	"time"
)

func TestOutputPathForFabName_ParsedNameStaysInsideBaseDir(t *testing.T) {
	baseDir := t.TempDir()
	fabName, err := ParseFabName("dc-east")
	if err != nil {
		t.Fatalf("ParseFabName returned error: %v", err)
	}
	ts := time.Date(2026, 4, 16, 15, 30, 0, 0, time.UTC)

	got, err := OutputPathForFabName(baseDir, fabName, ts)
	if err != nil {
		t.Fatalf("OutputPathForFabName returned error: %v", err)
	}
	rel, err := filepath.Rel(baseDir, got)
	if err != nil {
		t.Fatalf("filepath.Rel returned error: %v", err)
	}
	const want = "dc-east/20260416T153000Z/input.csv"
	if filepath.ToSlash(rel) != want {
		t.Errorf("relative output path = %q, want %q", filepath.ToSlash(rel), want)
	}
}

package preprocess

import (
	"testing"
	"time"
)

func TestOutputPath(t *testing.T) {
	ts := time.Date(2026, 4, 16, 15, 30, 0, 0, time.UTC)
	got := OutputPath("/data/out", "dc-east", ts)
	expected := "/data/out/dc-east/20260416T153000Z/input.csv"
	if got != expected {
		t.Errorf("expected %s, got %s", expected, got)
	}
}

func TestOutputPath_SpecialCharsInFab(t *testing.T) {
	ts := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	got := OutputPath("/out", "fab-1", ts)
	expected := "/out/fab-1/20260102T030405Z/input.csv"
	if got != expected {
		t.Errorf("expected %s, got %s", expected, got)
	}
}

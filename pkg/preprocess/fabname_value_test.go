package preprocess

import (
	"errors"
	"testing"
)

func TestParseFabName_AcceptsValidName(t *testing.T) {
	const raw = "fab 12 東京"

	got, err := ParseFabName(raw)
	if err != nil {
		t.Fatalf("ParseFabName(%q) returned error: %v", raw, err)
	}
	if got.String() != raw {
		t.Errorf("ParseFabName(%q).String() = %q, want the original name", raw, got.String())
	}
}

func TestParseFabName_RejectsInvalidName(t *testing.T) {
	const raw = "../escape"

	_, err := ParseFabName(raw)
	if !errors.Is(err, ErrInvalidFabName) {
		t.Fatalf("ParseFabName(%q) error = %v, want ErrInvalidFabName", raw, err)
	}
}

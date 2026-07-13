package task

import "testing"

func TestExpandIPSelectors_WhenSelectorsProvided_ReturnsExpandedListedTargets(t *testing.T) {
	got, err := ExpandIPSelectors([]string{"10.0.0.1", "10.0.0.8/30"})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("expected 5 targets, got %d", len(got))
	}
	if got[0] != "10.0.0.1" || got[4] != "10.0.0.11" {
		t.Fatalf("unexpected targets: %#v", got)
	}
}

func TestExpandIPSelectors_WhenSelectorInvalid_ReturnsError(t *testing.T) {
	if _, err := ExpandIPSelectors([]string{"bad"}); err == nil {
		t.Fatal("expected error")
	}
}

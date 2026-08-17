package task

import (
	"strings"
	"testing"
)

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

func TestExpandIPSelectors_UsesDefaultExpansionLimits(t *testing.T) {
	_, err := ExpandIPSelectors([]string{"10.0.0.0/8"})
	if err == nil || !strings.Contains(err.Error(), "count limit 10000000") {
		t.Fatalf("ExpandIPSelectors(/8) error = %v, want default count limit", err)
	}
}

func TestExpandIPSelectorsWithLimits_AcceptsOverridesAndZeroBypass(t *testing.T) {
	strict, err := NewExpansionLimits(3, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ExpandIPSelectorsWithLimits([]string{"192.0.2.0/30"}, strict); err == nil {
		t.Fatal("ExpandIPSelectorsWithLimits(strict) error = nil, want count limit error")
	}

	disabled, err := NewExpansionLimits(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ExpandIPSelectorsWithLimits([]string{"192.0.2.0/30"}, disabled)
	if err != nil {
		t.Fatalf("ExpandIPSelectorsWithLimits(disabled) error = %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("disabled limit expansion size = %d, want 4", len(got))
	}
}

func TestExpandIPSelectorsWithLimits_MemoryLimitIsIndependent(t *testing.T) {
	limits, err := NewExpansionLimits(0, 2)
	if err != nil {
		t.Fatal(err)
	}
	_, err = ExpandIPSelectorsWithLimits([]string{"198.16.0.0/12"}, limits)
	if err == nil || !strings.Contains(err.Error(), "memory limit 2 GB") {
		t.Fatalf("memory-only limit error = %v, want 2 GB limit", err)
	}
}

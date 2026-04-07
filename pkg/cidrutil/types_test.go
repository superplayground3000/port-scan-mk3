package cidrutil

import "testing"

func TestCIDREntry(t *testing.T) {
	entry := CIDREntry{
		Network: "10.0.0.0/8",
	}
	if entry.Network != "10.0.0.0/8" {
		t.Errorf("expected 10.0.0.0/8, got %s", entry.Network)
	}
}

func TestMatchResult(t *testing.T) {
	result := MatchResult{
		DenyCIDR: "10.0.0.0/8",
		OpenCIDR: "10.1.2.3/32",
	}
	if result.DenyCIDR != "10.0.0.0/8" {
		t.Errorf("expected deny 10.0.0.0/8, got %s", result.DenyCIDR)
	}
}

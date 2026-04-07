package cidrutil

import "testing"

func TestCIDREntry(t *testing.T) {
	entry := CIDREntry{
		Network: "10.0.0.0/8",
		StartIP: 0x0A000000, // 10.0.0.0 in uint32
		EndIP:   0x0AFFFFFF, // 10.255.255.255 in uint32
	}
	if entry.Network != "10.0.0.0/8" {
		t.Errorf("expected 10.0.0.0/8, got %s", entry.Network)
	}
	if entry.StartIP != 0x0A000000 {
		t.Errorf("expected StartIP 0x0A000000, got 0x%08X", entry.StartIP)
	}
	if entry.EndIP != 0x0AFFFFFF {
		t.Errorf("expected EndIP 0x0AFFFFFF, got 0x%08X", entry.EndIP)
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
	if result.OpenCIDR != "10.1.2.3/32" {
		t.Errorf("expected open 10.1.2.3/32, got %s", result.OpenCIDR)
	}
}

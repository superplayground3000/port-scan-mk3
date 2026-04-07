package cidrutil

import "testing"

func TestCIDRContains(t *testing.T) {
	deny := CIDREntry{
		Network: "10.0.0.0/8",
		StartIP: ipToUint32("10.0.0.0"),
		EndIP:   ipToUint32("10.255.255.255"),
	}
	open := CIDREntry{
		Network: "10.1.2.3/32",
		StartIP: ipToUint32("10.1.2.3"),
		EndIP:   ipToUint32("10.1.2.3"),
	}
	if !contains(deny, open) {
		t.Error("10.0.0.0/8 should contain 10.1.2.3/32")
	}
}

func TestCIDRDoesNotContain(t *testing.T) {
	deny := CIDREntry{
		Network: "10.0.0.0/8",
		StartIP: ipToUint32("10.0.0.0"),
		EndIP:   ipToUint32("10.255.255.255"),
	}
	open := CIDREntry{
		Network: "192.168.1.1/32",
		StartIP: ipToUint32("192.168.1.1"),
		EndIP:   ipToUint32("192.168.1.1"),
	}
	if contains(deny, open) {
		t.Error("10.0.0.0/8 should not contain 192.168.1.1/32")
	}
}

func TestIntervalTreeQueryEmpty(t *testing.T) {
	tree := &IntervalTree{}
	matches := tree.Query(CIDREntry{
		Network: "10.1.2.3/32",
		StartIP: ipToUint32("10.1.2.3"),
		EndIP:   ipToUint32("10.1.2.3"),
	})
	if len(matches) != 0 {
		t.Errorf("expected 0 matches for empty tree, got %d", len(matches))
	}
}

func TestIntervalTreeInsertAndQuery(t *testing.T) {
	tree := &IntervalTree{}
	tree.Insert(CIDREntry{
		Network: "10.0.0.0/8",
		StartIP: ipToUint32("10.0.0.0"),
		EndIP:   ipToUint32("10.255.255.255"),
	})

	matches := tree.Query(CIDREntry{
		Network: "10.1.2.3/32",
		StartIP: ipToUint32("10.1.2.3"),
		EndIP:   ipToUint32("10.1.2.3"),
	})

	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
	if matches[0].Network != "10.0.0.0/8" {
		t.Errorf("expected 10.0.0.0/8, got %s", matches[0].Network)
	}
}

func TestParseCIDRInvalid(t *testing.T) {
	_, err := ParseCIDR("invalid")
	if err == nil {
		t.Error("expected error for invalid CIDR")
	}
}

func TestParseCIDRSlash16(t *testing.T) {
	entry, err := ParseCIDR("10.0.0.0/16")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 10.0.0.0 = 0x0A000000, 10.0.255.255 = 0x0A00FFFF
	if entry.StartIP != 0x0A000000 {
		t.Errorf("expected StartIP 0x0A000000, got 0x%08X", entry.StartIP)
	}
	if entry.EndIP != 0x0A00FFFF {
		t.Errorf("expected EndIP 0x0A00FFFF, got 0x%08X", entry.EndIP)
	}
}

func TestParseCIDRIPv6(t *testing.T) {
	_, err := ParseCIDR("2001:db8::/32")
	if err == nil {
		t.Error("expected error for IPv6 CIDR")
	}
}

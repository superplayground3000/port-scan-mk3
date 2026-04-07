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

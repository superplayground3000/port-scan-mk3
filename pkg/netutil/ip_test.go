package netutil

import (
	"net"
	"testing"
)

func TestIPRange_WhenIPv4CIDR_ReturnsRange(t *testing.T) {
	_, n, err := net.ParseCIDR("10.0.0.4/30")
	if err != nil {
		t.Fatalf("parse cidr failed: %v", err)
	}

	start, end, ok := IPRange(n)
	if !ok {
		t.Fatal("expected ok for ipv4 cidr")
	}
	if got, want := start.String(), "10.0.0.4"; got != want {
		t.Fatalf("unexpected start ip: got %s want %s", got, want)
	}
	if got, want := end.String(), "10.0.0.7"; got != want {
		t.Fatalf("unexpected end ip: got %s want %s", got, want)
	}
}

func TestIPRange_WhenInputInvalid_ReturnsNotOK(t *testing.T) {
	tests := []struct {
		name string
		net  *net.IPNet
	}{
		{name: "nil net", net: nil},
		{name: "ipv6 net", net: mustCIDR(t, "2001:db8::/64")},
		{
			name: "invalid mask length",
			net: &net.IPNet{
				IP:   net.IPv4(10, 0, 0, 1),
				Mask: net.CIDRMask(32, 128),
			},
		},
	}

	for _, tt := range tests {
		start, end, ok := IPRange(tt.net)
		if ok {
			t.Fatalf("%s: expected ok=false, got true", tt.name)
		}
		if start != nil || end != nil {
			t.Fatalf("%s: expected nil start/end, got start=%v end=%v", tt.name, start, end)
		}
	}
}

func mustCIDR(t *testing.T, raw string) *net.IPNet {
	t.Helper()
	_, n, err := net.ParseCIDR(raw)
	if err != nil {
		t.Fatalf("parse cidr %q failed: %v", raw, err)
	}
	return n
}

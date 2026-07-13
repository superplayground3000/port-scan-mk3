package task

import (
	"net"
	"slices"
	"testing"
)

func mustCIDR(t *testing.T, s string) *net.IPNet {
	t.Helper()
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		t.Fatalf("bad cidr %q: %v", s, err)
	}
	return n
}

func TestFilterBoundaryBroadcast_WhenBoundary24_DropsBroadcastKeepsNetwork(t *testing.T) {
	ips := []string{"10.0.0.0", "10.0.0.1", "10.0.0.254", "10.0.0.255"}
	got := FilterBoundaryBroadcast(ips, mustCIDR(t, "10.0.0.0/24"))
	want := []string{"10.0.0.0", "10.0.0.1", "10.0.0.254"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
}

func TestFilterBoundaryBroadcast_WhenBoundary23_DropsOnlyTrueBroadcast(t *testing.T) {
	ips := []string{"10.0.0.0", "10.0.0.255", "10.0.1.0", "10.0.1.255"}
	got := FilterBoundaryBroadcast(ips, mustCIDR(t, "10.0.0.0/23"))
	// Only 10.0.1.255 is the /23 broadcast; 10.0.0.255 is a valid host.
	want := []string{"10.0.0.0", "10.0.0.255", "10.0.1.0"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
}

func TestFilterBoundaryBroadcast_WhenExplicitBroadcastIP_IsDropped(t *testing.T) {
	// A single explicitly listed IP that is the boundary broadcast is excluded.
	got := FilterBoundaryBroadcast([]string{"10.0.0.255"}, mustCIDR(t, "10.0.0.0/24"))
	if len(got) != 0 {
		t.Fatalf("expected explicit broadcast to be dropped, got %#v", got)
	}
}

func TestFilterBoundaryBroadcast_WhenSmallPrefixes_KeepAll(t *testing.T) {
	ips := []string{"10.0.0.0", "10.0.0.1"}
	if got := FilterBoundaryBroadcast(ips, mustCIDR(t, "10.0.0.0/31")); !slices.Equal(got, ips) {
		t.Fatalf("/31 must keep all, got %#v", got)
	}
	single := []string{"10.0.0.255"}
	if got := FilterBoundaryBroadcast(single, mustCIDR(t, "10.0.0.255/32")); !slices.Equal(got, single) {
		t.Fatalf("/32 must keep the host, got %#v", got)
	}
}

func TestFilterBoundaryBroadcast_WhenNilBoundary_KeepAll(t *testing.T) {
	ips := []string{"10.0.0.255"}
	if got := FilterBoundaryBroadcast(ips, nil); !slices.Equal(got, ips) {
		t.Fatalf("nil boundary must keep all, got %#v", got)
	}
}

func TestFilterBoundaryBroadcast_WhenTopOfIPv4Range_DropsMaxBroadcast(t *testing.T) {
	// Max-uint32 boundary condition: 255.255.255.0/24 broadcast is 255.255.255.255.
	ips := []string{"255.255.255.0", "255.255.255.254", "255.255.255.255"}
	got := FilterBoundaryBroadcast(ips, mustCIDR(t, "255.255.255.0/24"))
	want := []string{"255.255.255.0", "255.255.255.254"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
}

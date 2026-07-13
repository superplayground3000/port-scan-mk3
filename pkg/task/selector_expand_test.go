package task

import (
	"slices"
	"testing"
)

func TestExpandIPSelectors_WhenSelectorsProvided_ReturnsExpandedListedTargets(t *testing.T) {
	got, err := ExpandIPSelectors([]string{"10.0.0.1", "10.0.0.8/30"})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	// 10.0.0.8/30 => .8 (network, kept), .9, .10; .11 (broadcast) excluded.
	want := []string{"10.0.0.1", "10.0.0.8", "10.0.0.9", "10.0.0.10"}
	if !slices.Equal(got, want) {
		t.Fatalf("unexpected targets: got %#v want %#v", got, want)
	}
}

func TestExpandIPSelectors_WhenSelectorInvalid_ReturnsError(t *testing.T) {
	if _, err := ExpandIPSelectors([]string{"bad"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestExpandIPSelectors_WhenCIDR24_ExcludesBroadcastKeepsNetwork(t *testing.T) {
	got, err := ExpandIPSelectors([]string{"10.0.0.0/24"})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(got) != 255 {
		t.Fatalf("expected 255 targets (256 minus broadcast), got %d", len(got))
	}
	if got[0] != "10.0.0.0" {
		t.Fatalf("expected network address .0 kept, got first %s", got[0])
	}
	if slices.Contains(got, "10.0.0.255") {
		t.Fatalf("broadcast 10.0.0.255 must not be a scan target")
	}
	if !slices.Contains(got, "10.0.0.254") {
		t.Fatalf("last usable host 10.0.0.254 must be a scan target")
	}
}

func TestExpandIPSelectors_WhenCIDR23_ExcludesOnlyTrueBroadcast(t *testing.T) {
	// 10.0.0.0/23 spans 10.0.0.0 .. 10.0.1.255; only 10.0.1.255 is the broadcast.
	got, err := ExpandIPSelectors([]string{"10.0.0.0/23"})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if slices.Contains(got, "10.0.1.255") {
		t.Fatalf("true broadcast 10.0.1.255 must be excluded")
	}
	if !slices.Contains(got, "10.0.0.255") {
		t.Fatalf("10.0.0.255 is a valid host in a /23 and must be scanned")
	}
	if !slices.Contains(got, "10.0.0.0") {
		t.Fatalf("network address 10.0.0.0 must be kept")
	}
}

func TestExpandIPSelectors_WhenSmallPrefixes_KeepsAllAddresses(t *testing.T) {
	// /31 (RFC 3021 point-to-point) has no broadcast: keep both.
	got31, err := ExpandIPSelectors([]string{"10.0.0.0/31"})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !slices.Equal(got31, []string{"10.0.0.0", "10.0.0.1"}) {
		t.Fatalf("/31 must keep both addresses, got %#v", got31)
	}

	// /32 single host must be kept, even when it looks like a broadcast (.255).
	got32, err := ExpandIPSelectors([]string{"10.0.0.255/32"})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !slices.Equal(got32, []string{"10.0.0.255"}) {
		t.Fatalf("/32 host must be kept, got %#v", got32)
	}
}

func TestExpandIPSelectors_WhenExplicitSingleIP_IsHonored(t *testing.T) {
	// A directly-listed single IP (bare, no CIDR) is honored even if it ends in .255.
	got, err := ExpandIPSelectors([]string{"10.0.0.255"})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !slices.Equal(got, []string{"10.0.0.255"}) {
		t.Fatalf("explicit single IP must be scanned, got %#v", got)
	}
}

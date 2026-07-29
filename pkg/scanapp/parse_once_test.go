package scanapp

import (
	"net"
	"strings"
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/input"
)

// referenceReachable reproduces the pre-optimization reachablePredicate behavior
// (TrimSpace, parse, non-IPv4 -> reachable, otherwise binary-search the blocked
// set) so the optimized predicate can be proven byte-for-byte equivalent. The
// TrimSpace mirrors production (pre_scan_ping.go); without it this oracle would
// disagree with production on padded input that resolves to a blocked IP.
func referenceReachable(blocked []uint32, ip string) bool {
	parsed := net.ParseIP(strings.TrimSpace(ip))
	if parsed == nil || parsed.To4() == nil {
		return true
	}
	want := ipv4ToUint32(parsed.String())
	for _, b := range blocked {
		if b == want {
			return false
		}
	}
	return true
}

func TestReachablePredicate_MatchesReferenceBehavior(t *testing.T) {
	blockedIPs := []string{"10.0.0.5", "192.168.1.1", "0.0.0.0"}
	blocked := make([]uint32, 0, len(blockedIPs))
	for _, ip := range blockedIPs {
		blocked = append(blocked, ipv4ToUint32(ip))
	}
	predicate := reachablePredicate(blocked)

	cases := []string{
		"10.0.0.5",        // blocked -> unreachable
		"10.0.0.6",        // not blocked -> reachable
		"192.168.1.1",     // blocked -> unreachable
		"192.168.1.2",     // not blocked -> reachable
		"0.0.0.0",         // blocked boundary -> unreachable
		"255.255.255.255", // boundary max -> reachable
		"  10.0.0.6  ",    // whitespace padded, not blocked -> reachable
		"  10.0.0.5  ",    // whitespace padded, resolves to a blocked IP -> unreachable (exercises TrimSpace)
		"::ffff:10.0.0.5", // IPv4-mapped IPv6, To4()!=nil, resolves to blocked -> unreachable
		"not-an-ip",       // non-IPv4 -> reachable
		"::1",             // IPv6 (To4()==nil) -> reachable
		"",                // empty -> reachable
	}
	for _, ip := range cases {
		got := predicate(ip)
		want := referenceReachable(blocked, ip)
		if got != want {
			t.Errorf("reachablePredicate(%q) = %v, want %v", ip, got, want)
		}
	}
}

func TestScanTarget_IPU32_PopulatedAtCreationBasic(t *testing.T) {
	_, ipNet, _ := net.ParseCIDR("1.2.3.0/30")
	records := []input.CIDRRecord{
		{FabName: "fab1", IPRaw: "1.2.3.1", CIDR: "1.2.3.0/30", CIDRName: "net-a", Net: ipNet},
	}
	groups, err := buildCIDRGroups(records)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	g := groups["1.2.3.0/30"]
	if len(g.targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(g.targets))
	}
	// 1.2.3.1 = 0x01020301 = 16909057
	const want uint32 = 16909057
	if g.targets[0].ipU32 != want {
		t.Fatalf("ipU32 = %d, want %d", g.targets[0].ipU32, want)
	}
	if g.targets[0].ipU32 != ipv4ToUint32(g.targets[0].ip) {
		t.Fatalf("ipU32 %d does not match ipv4ToUint32(%q)=%d",
			g.targets[0].ipU32, g.targets[0].ip, ipv4ToUint32(g.targets[0].ip))
	}
}

func TestScanTarget_IPU32_PopulatedAtCreationRich(t *testing.T) {
	records := []input.CIDRRecord{
		{
			IsRich: true, IsValid: true, ExecutionKey: "10.89.52.36:80/tcp",
			DstIP: "10.89.52.36", DstNetworkSegment: "10.89.52.0/24", Port: 80,
			FabName: "fab1", CIDRName: "net-a",
		},
	}
	groups, err := buildRichGroups(records)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	g := groups["10.89.52.0/24"]
	if len(g.targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(g.targets))
	}
	if g.targets[0].ipU32 != ipv4ToUint32("10.89.52.36") {
		t.Fatalf("ipU32 = %d, want %d", g.targets[0].ipU32, ipv4ToUint32("10.89.52.36"))
	}
}

func TestGroupOrdering_UnchangedForMixedSet(t *testing.T) {
	_, ipNet, _ := net.ParseCIDR("10.0.0.0/24")
	records := []input.CIDRRecord{
		{IPRaw: "10.0.0.3", CIDR: "10.0.0.0/24", Net: ipNet},
		{IPRaw: "10.0.0.1", CIDR: "10.0.0.0/24", Net: ipNet},
		{IPRaw: "10.0.0.10", CIDR: "10.0.0.0/24", Net: ipNet},
		{IPRaw: "10.0.0.2", CIDR: "10.0.0.0/24", Net: ipNet},
	}
	groups, err := buildCIDRGroups(records)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	g := groups["10.0.0.0/24"]
	want := []string{"10.0.0.1", "10.0.0.2", "10.0.0.3", "10.0.0.10"}
	if len(g.targets) != len(want) {
		t.Fatalf("expected %d targets, got %d", len(want), len(g.targets))
	}
	for i, ip := range want {
		if g.targets[i].ip != ip {
			t.Fatalf("target[%d].ip = %q, want %q", i, g.targets[i].ip, ip)
		}
	}
}

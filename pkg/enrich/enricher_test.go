package enrich

import (
	"strings"
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/cidrutil"
	"github.com/xuxiping/port-scan-mk3/pkg/preprocesscfg"
)

func buildTree(cidrs ...string) *cidrutil.IntervalTree {
	tree := &cidrutil.IntervalTree{}
	for _, c := range cidrs {
		entry, _ := cidrutil.ParseCIDR(c)
		tree.Insert(entry)
	}
	return tree
}

func TestEnrich_FullMatch(t *testing.T) {
	tree := buildTree("10.0.0.0/8", "10.1.0.0/16")
	svc := map[int]string{22: "SSH", 80: "HTTP"}
	e := NewEnricher(tree, svc)

	row, err := e.Enrich("10.1.2.3", 22)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if row.DstIP != "10.1.2.3" {
		t.Errorf("DstIP: expected 10.1.2.3, got %s", row.DstIP)
	}
	// Should pick most specific CIDR: 10.1.0.0/16
	if row.DstNetworkSegment != "10.1.0.0/16" {
		t.Errorf("DstNetworkSegment: expected 10.1.0.0/16, got %s", row.DstNetworkSegment)
	}
	if row.ServiceLabel != "SSH" {
		t.Errorf("ServiceLabel: expected SSH, got %s", row.ServiceLabel)
	}
	if row.Port != "22" {
		t.Errorf("Port: expected 22, got %s", row.Port)
	}
	if row.SrcIP != preprocesscfg.DefaultSrcIP {
		t.Errorf("SrcIP: expected %s, got %s", preprocesscfg.DefaultSrcIP, row.SrcIP)
	}
	if row.SrcNetworkSegment != preprocesscfg.DefaultSrcNetworkSegment {
		t.Errorf("SrcNetworkSegment: expected %s, got %s", preprocesscfg.DefaultSrcNetworkSegment, row.SrcNetworkSegment)
	}
	if row.Protocol != preprocesscfg.DefaultProtocol {
		t.Errorf("Protocol: expected %s, got %s", preprocesscfg.DefaultProtocol, row.Protocol)
	}
	if row.Decision != preprocesscfg.DefaultDecision {
		t.Errorf("Decision: expected %s, got %s", preprocesscfg.DefaultDecision, row.Decision)
	}
	if row.MatchedPolicyID != preprocesscfg.DefaultPolicyID {
		t.Errorf("MatchedPolicyID: expected %s, got %s", preprocesscfg.DefaultPolicyID, row.MatchedPolicyID)
	}
	if row.Reason != preprocesscfg.DefaultReason {
		t.Errorf("Reason: expected %s, got %s", preprocesscfg.DefaultReason, row.Reason)
	}
}

func TestEnrich_NoCIDRMatch_FallbackSlash32(t *testing.T) {
	tree := buildTree("192.168.0.0/16")
	svc := map[int]string{80: "HTTP"}
	e := NewEnricher(tree, svc)

	row, err := e.Enrich("10.5.6.7", 80)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if row.DstNetworkSegment != "10.5.6.7/32" {
		t.Errorf("expected fallback 10.5.6.7/32, got %s", row.DstNetworkSegment)
	}
}

func TestEnrich_NoServiceMatch_FallbackUnknown(t *testing.T) {
	tree := buildTree("10.0.0.0/8")
	svc := map[int]string{22: "SSH"}
	e := NewEnricher(tree, svc)

	row, err := e.Enrich("10.1.2.3", 9999)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if row.ServiceLabel != preprocesscfg.FallbackServiceLabel {
		t.Errorf("expected %s, got %s", preprocesscfg.FallbackServiceLabel, row.ServiceLabel)
	}
}

func TestEnrich_InvalidHost(t *testing.T) {
	tree := buildTree("10.0.0.0/8")
	svc := map[int]string{}
	e := NewEnricher(tree, svc)

	_, err := e.Enrich("not-an-ip", 80)
	if err == nil {
		t.Fatal("expected error for invalid host IP")
	}
}

func TestEnrich_IPv6Rejected(t *testing.T) {
	tree := buildTree("10.0.0.0/8")
	svc := map[int]string{}
	e := NewEnricher(tree, svc)

	_, err := e.Enrich("::1", 80)
	if err == nil {
		t.Fatal("expected error for IPv6 address")
	}
	if !strings.Contains(err.Error(), "not IPv4") {
		t.Errorf("expected 'not IPv4' in error, got: %v", err)
	}
}

func TestEnrich_NilTree(t *testing.T) {
	svc := map[int]string{22: "SSH"}
	e := NewEnricher(nil, svc)

	row, err := e.Enrich("10.1.2.3", 22)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// With no tree entries, should fall back to /32
	if row.DstNetworkSegment != "10.1.2.3/32" {
		t.Errorf("expected fallback 10.1.2.3/32, got %s", row.DstNetworkSegment)
	}
}

func TestEnrich_PortZero(t *testing.T) {
	tree := buildTree("10.0.0.0/8")
	svc := map[int]string{}
	e := NewEnricher(tree, svc)

	_, err := e.Enrich("10.1.2.3", 0)
	if err == nil {
		t.Fatal("expected error for port 0")
	}
}

func TestEnrich_PortNegative(t *testing.T) {
	tree := buildTree("10.0.0.0/8")
	svc := map[int]string{}
	e := NewEnricher(tree, svc)

	_, err := e.Enrich("10.1.2.3", -1)
	if err == nil {
		t.Fatal("expected error for negative port")
	}
}

func TestEnrich_PortTooHigh(t *testing.T) {
	tree := buildTree("10.0.0.0/8")
	svc := map[int]string{}
	e := NewEnricher(tree, svc)

	_, err := e.Enrich("10.1.2.3", 99999)
	if err == nil {
		t.Fatal("expected error for port > 65535")
	}
}

func TestEnrich_PortBoundary(t *testing.T) {
	tree := buildTree("10.0.0.0/8")
	svc := map[int]string{65535: "MAX"}
	e := NewEnricher(tree, svc)

	row, err := e.Enrich("10.1.2.3", 65535)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if row.Port != "65535" {
		t.Errorf("expected port 65535, got %s", row.Port)
	}
	if row.ServiceLabel != "MAX" {
		t.Errorf("expected service label MAX, got %s", row.ServiceLabel)
	}
}

package input

import (
	"strings"
	"testing"
)

func TestLoadCIDRs_WhenCIDRsOverlap_ReturnsNil(t *testing.T) {
	rows := "fab_name,ip,ip_cidr,cidr_name\n" +
		"fab1,10.0.0.1,10.0.0.0/8,a\n" +
		"fab2,10.1.0.1,10.1.0.0/16,b\n"
	_, err := LoadCIDRs(strings.NewReader(rows))
	if err != nil {
		t.Fatalf("expected overlap to be allowed, got %v", err)
	}
}

func TestParseBasicCIDRRows_ParsesFabPortAndSelector(t *testing.T) {
	rows := [][]string{
		{"ip", "ip_cidr", "fab_name", "cidr_name", "port"},
		{"192.168.1.10", "192.168.1.0/24", "fab-1", "web", "443"},
	}
	got, err := parseBasicCIDRRows(rows, "ip", "ip_cidr")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d records, want 1", len(got))
	}
	r := got[0]
	if r.IPRaw != "192.168.1.10" || r.CIDR != "192.168.1.0/24" || r.FabName != "fab-1" || r.CIDRName != "web" || r.Port != 443 {
		t.Errorf("unexpected record: %+v", r)
	}
	if r.Net == nil || r.Selector == nil {
		t.Error("expected Parse() to populate Net and Selector")
	}
}

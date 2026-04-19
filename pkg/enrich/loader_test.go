package enrich

import (
	"strings"
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/cidrutil"
)

func TestLoadServiceMap_ValidCSV(t *testing.T) {
	csv := "port,service_label\n22,SSH\n80,HTTP\n443,HTTPS\n"
	m, err := LoadServiceMap(strings.NewReader(csv))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(m))
	}
	if m[22] != "SSH" {
		t.Errorf("port 22: expected SSH, got %s", m[22])
	}
	if m[80] != "HTTP" {
		t.Errorf("port 80: expected HTTP, got %s", m[80])
	}
	if m[443] != "HTTPS" {
		t.Errorf("port 443: expected HTTPS, got %s", m[443])
	}
}

func TestLoadServiceMap_MissingHeader(t *testing.T) {
	csv := "a,b\n22,SSH\n"
	_, err := LoadServiceMap(strings.NewReader(csv))
	if err == nil {
		t.Fatal("expected error for missing header columns")
	}
}

func TestLoadServiceMap_InvalidPort(t *testing.T) {
	csv := "port,service_label\nabc,SSH\n80,HTTP\n"
	m, err := LoadServiceMap(strings.NewReader(csv))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m) != 1 {
		t.Fatalf("expected 1 entry (skipping invalid), got %d", len(m))
	}
	if m[80] != "HTTP" {
		t.Errorf("port 80: expected HTTP, got %s", m[80])
	}
}

func TestLoadServiceMap_Empty(t *testing.T) {
	csv := "port,service_label\n"
	m, err := LoadServiceMap(strings.NewReader(csv))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(m))
	}
}

func TestLoadCIDRList_ValidCSV(t *testing.T) {
	csv := "cidr\n10.0.0.0/8\n192.168.1.0/24\n"
	tree, err := LoadCIDRList(strings.NewReader(csv))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	q, _ := cidrutil.ParseCIDR("10.1.2.3/32")
	matches := tree.Query(q)
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
	if matches[0].Network != "10.0.0.0/8" {
		t.Errorf("expected 10.0.0.0/8, got %s", matches[0].Network)
	}
}

func TestLoadCIDRList_NoHeader(t *testing.T) {
	csv := "10.0.0.0/8\n192.168.1.0/24\n"
	tree, err := LoadCIDRList(strings.NewReader(csv))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	q, _ := cidrutil.ParseCIDR("10.1.2.3/32")
	matches := tree.Query(q)
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
}

func TestLoadCIDRList_MalformedSkipped(t *testing.T) {
	csv := "cidr\n10.0.0.0/8\nnot-a-cidr\n192.168.1.0/24\n"
	tree, err := LoadCIDRList(strings.NewReader(csv))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	q1, _ := cidrutil.ParseCIDR("10.1.2.3/32")
	q2, _ := cidrutil.ParseCIDR("192.168.1.1/32")
	if len(tree.Query(q1)) != 1 {
		t.Error("expected match for 10.1.2.3 in 10.0.0.0/8")
	}
	if len(tree.Query(q2)) != 1 {
		t.Error("expected match for 192.168.1.1 in 192.168.1.0/24")
	}
}

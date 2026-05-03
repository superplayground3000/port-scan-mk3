package enrich

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/cidrutil"
)

func TestLoadServiceMap_ValidCSV(t *testing.T) {
	csv := "port,service_label\n22,SSH\n80,HTTP\n443,HTTPS\n"
	m, err := LoadServiceMap(strings.NewReader(csv), io.Discard)
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
	_, err := LoadServiceMap(strings.NewReader(csv), io.Discard)
	if err == nil {
		t.Fatal("expected error for missing header columns")
	}
}

func TestLoadServiceMap_InvalidPort(t *testing.T) {
	csv := "port,service_label\nabc,SSH\n80,HTTP\n"
	m, err := LoadServiceMap(strings.NewReader(csv), io.Discard)
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
	m, err := LoadServiceMap(strings.NewReader(csv), io.Discard)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(m))
	}
}

func TestLoadCIDRList_ValidCSV(t *testing.T) {
	csv := "cidr\n10.0.0.0/8\n192.168.1.0/24\n"
	tree, err := LoadCIDRList(strings.NewReader(csv), io.Discard)
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
	tree, err := LoadCIDRList(strings.NewReader(csv), io.Discard)
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
	tree, err := LoadCIDRList(strings.NewReader(csv), io.Discard)
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

func TestLoadServiceMap_EmptyCSV(t *testing.T) {
	_, err := LoadServiceMap(strings.NewReader(""), io.Discard)
	if err == nil {
		t.Fatal("expected error for completely empty CSV")
	}
}

func TestLoadServiceMap_WarnsOnInvalidPort(t *testing.T) {
	csv := "port,service_label\nabc,SSH\n80,HTTP\n"
	var warn bytes.Buffer
	m, err := LoadServiceMap(strings.NewReader(csv), &warn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(m))
	}
	if !strings.Contains(warn.String(), "invalid port") {
		t.Errorf("expected warning about invalid port, got: %s", warn.String())
	}
}

func TestLoadCIDRList_WarnsOnMalformed(t *testing.T) {
	csv := "cidr\n10.0.0.0/8\nnot-a-cidr\n"
	var warn bytes.Buffer
	_, err := LoadCIDRList(strings.NewReader(csv), &warn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(warn.String(), "invalid CIDR") {
		t.Errorf("expected warning about invalid CIDR, got: %s", warn.String())
	}
}

func TestLoadCIDRList_EmptyCSV(t *testing.T) {
	tree, err := LoadCIDRList(strings.NewReader(""), io.Discard)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	q, _ := cidrutil.ParseCIDR("10.0.0.0/8")
	if len(tree.Query(q)) != 0 {
		t.Error("expected no matches from empty CIDR list")
	}
}

func TestLoadServiceMap_NilWarn(t *testing.T) {
	csv := "port,service_label\n22,SSH\n"
	m, err := LoadServiceMap(strings.NewReader(csv), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(m))
	}
}

func TestLoadCIDRList_NilWarn(t *testing.T) {
	csv := "cidr\n10.0.0.0/8\n"
	tree, err := LoadCIDRList(strings.NewReader(csv), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	q, _ := cidrutil.ParseCIDR("10.1.2.3/32")
	if len(tree.Query(q)) != 1 {
		t.Fatalf("expected 1 match, got %d", len(tree.Query(q)))
	}
}

package preprocess

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/cidrutil"
)

func TestLoadCleanedCIDRs_FiltersByFabAndClose(t *testing.T) {
	csv := "fab,segment,status\ndc-east,10.0.0.0/16,close\ndc-east,10.1.0.0/16,open\ndc-west,192.168.0.0/24,close\n"
	tree, err := LoadCleanedCIDRs(strings.NewReader(csv), "dc-east", io.Discard)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	q1, _ := cidrutil.ParseCIDR("10.0.1.0/24")
	if len(tree.Query(q1)) != 1 {
		t.Error("expected 10.0.1.0/24 to match closed 10.0.0.0/16")
	}
	q2, _ := cidrutil.ParseCIDR("10.1.1.0/24")
	if len(tree.Query(q2)) != 0 {
		t.Error("expected no match for open CIDR 10.1.0.0/16")
	}
	q3, _ := cidrutil.ParseCIDR("192.168.0.1/32")
	if len(tree.Query(q3)) != 0 {
		t.Error("expected no match for dc-west CIDR when filtering dc-east")
	}
}

func TestLoadCleanedCIDRs_MissingColumns(t *testing.T) {
	csv := "a,b,c\n1,2,3\n"
	_, err := LoadCleanedCIDRs(strings.NewReader(csv), "dc-east", io.Discard)
	if err == nil {
		t.Fatal("expected error for missing required columns")
	}
}

func TestLoadCleanedCIDRs_EmptyResult(t *testing.T) {
	csv := "fab,segment,status\ndc-east,10.0.0.0/16,open\n"
	tree, err := LoadCleanedCIDRs(strings.NewReader(csv), "dc-east", io.Discard)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	q, _ := cidrutil.ParseCIDR("10.0.1.0/24")
	if len(tree.Query(q)) != 0 {
		t.Error("expected no matches when all CIDRs are open")
	}
}

func TestLoadCleanedCIDRs_CaseInsensitiveStatus(t *testing.T) {
	csv := "fab,segment,status\ndc-east,10.0.0.0/16,CLOSE\n"
	tree, err := LoadCleanedCIDRs(strings.NewReader(csv), "dc-east", io.Discard)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	q, _ := cidrutil.ParseCIDR("10.0.1.0/24")
	if len(tree.Query(q)) != 1 {
		t.Error("expected match for case-insensitive CLOSE status")
	}
}

func TestLoadCleanedCIDRs_EmptyCSV(t *testing.T) {
	_, err := LoadCleanedCIDRs(strings.NewReader(""), "dc-east", io.Discard)
	if err == nil {
		t.Fatal("expected error for completely empty CSV")
	}
}

func TestLoadCleanedCIDRs_WarnsOnInvalidCIDR(t *testing.T) {
	csv := "fab,segment,status\ndc-east,not-a-cidr,close\ndc-east,10.0.0.0/16,close\n"
	var warn bytes.Buffer
	tree, err := LoadCleanedCIDRs(strings.NewReader(csv), "dc-east", &warn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(warn.String(), "invalid CIDR") {
		t.Errorf("expected warning about invalid CIDR, got: %s", warn.String())
	}
	q, _ := cidrutil.ParseCIDR("10.0.1.0/24")
	if len(tree.Query(q)) != 1 {
		t.Error("expected valid CIDR to still be loaded")
	}
}

func TestLoadCleanedCIDRs_NilWarn(t *testing.T) {
	csv := "fab,segment,status\ndc-east,10.0.0.0/16,close\n"
	tree, err := LoadCleanedCIDRs(strings.NewReader(csv), "dc-east", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	q, _ := cidrutil.ParseCIDR("10.0.1.0/24")
	if len(tree.Query(q)) != 1 {
		t.Error("expected closed CIDR to be loaded")
	}
}

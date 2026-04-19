package preprocess

import (
	"strings"
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/cidrutil"
)

func TestLoadCleanedCIDRs_FiltersByFabAndClose(t *testing.T) {
	csv := "fab,segment,status\ndc-east,10.0.0.0/16,close\ndc-east,10.1.0.0/16,open\ndc-west,192.168.0.0/24,close\n"
	tree, err := LoadCleanedCIDRs(strings.NewReader(csv), "dc-east")
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
	_, err := LoadCleanedCIDRs(strings.NewReader(csv), "dc-east")
	if err == nil {
		t.Fatal("expected error for missing required columns")
	}
}

func TestLoadCleanedCIDRs_EmptyResult(t *testing.T) {
	csv := "fab,segment,status\ndc-east,10.0.0.0/16,open\n"
	tree, err := LoadCleanedCIDRs(strings.NewReader(csv), "dc-east")
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
	tree, err := LoadCleanedCIDRs(strings.NewReader(csv), "dc-east")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	q, _ := cidrutil.ParseCIDR("10.0.1.0/24")
	if len(tree.Query(q)) != 1 {
		t.Error("expected match for case-insensitive CLOSE status")
	}
}

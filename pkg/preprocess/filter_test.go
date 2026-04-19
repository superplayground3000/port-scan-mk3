package preprocess

import (
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/cidrutil"
)

func buildClosedTree(cidrs ...string) *cidrutil.IntervalTree {
	tree := &cidrutil.IntervalTree{}
	for _, c := range cidrs {
		entry, _ := cidrutil.ParseCIDR(c)
		tree.Insert(entry)
	}
	return tree
}

func TestFilter_KeepWhenNotInClosedCIDR(t *testing.T) {
	tree := buildClosedTree("10.0.0.0/8")
	f := NewFilter(tree)

	keep, err := f.Keep("192.168.1.0/24")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !keep {
		t.Error("expected keep=true for CIDR not in closed tree")
	}
}

func TestFilter_DropWhenContainedInClosedCIDR(t *testing.T) {
	tree := buildClosedTree("10.0.0.0/8")
	f := NewFilter(tree)

	keep, err := f.Keep("10.1.0.0/16")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if keep {
		t.Error("expected keep=false for CIDR contained in closed 10.0.0.0/8")
	}
}

func TestFilter_DropOnExactMatch(t *testing.T) {
	tree := buildClosedTree("10.0.0.0/16")
	f := NewFilter(tree)

	keep, err := f.Keep("10.0.0.0/16")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if keep {
		t.Error("expected keep=false for exact CIDR match")
	}
}

func TestFilter_KeepAllWhenNoClosedCIDRs(t *testing.T) {
	tree := &cidrutil.IntervalTree{}
	f := NewFilter(tree)

	keep, err := f.Keep("10.0.0.0/16")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !keep {
		t.Error("expected keep=true when no closed CIDRs exist")
	}
}

func TestFilter_InvalidCIDR(t *testing.T) {
	tree := buildClosedTree("10.0.0.0/8")
	f := NewFilter(tree)

	_, err := f.Keep("not-a-cidr")
	if err == nil {
		t.Fatal("expected error for invalid CIDR")
	}
}

func TestFilter_NilTree(t *testing.T) {
	f := NewFilter(nil)

	keep, err := f.Keep("10.0.0.0/16")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !keep {
		t.Error("expected keep=true when tree is nil (nothing closed)")
	}
}

package input

import (
	"strings"
	"testing"
)

func mustLoadRows(t *testing.T, rows []CIDRRecord) []CIDRRecord {
	t.Helper()
	for i := range rows {
		if err := rows[i].Parse(); err != nil {
			t.Fatalf("parse row[%d] failed: %v", i, err)
		}
	}
	return rows
}

func TestValidateIPRows_WhenDuplicateSrcDstIPCidrAndPortExists_ReturnsError(t *testing.T) {
	rows := mustLoadRows(t, []CIDRRecord{
		{IPRaw: "10.0.0.1", IPCidrRaw: "10.0.0.0/24", Port: 443},
		{IPRaw: "10.0.0.1", IPCidrRaw: "10.0.0.0/24", Port: 443},
	})
	err := ValidateIPRows(rows)
	if err == nil {
		t.Fatal("expected duplicate tuple error")
	}
	if !strings.Contains(err.Error(), "duplicate (src,dst,ip_cidr,port)") {
		t.Fatalf("expected duplicate tuple error, got %v", err)
	}
}

func TestValidateIPRows_WhenBasicSelectorsHaveEquivalentForms_ReturnsError(t *testing.T) {
	rows := mustLoadRows(t, []CIDRRecord{
		{IPRaw: "10.0.0.1", IPCidrRaw: "10.0.0.42/24", Port: 443},
		{IPRaw: "10.0.0.1/32", IPCidrRaw: "10.0.0.0/24", Port: 443},
	})
	if err := ValidateIPRows(rows); err == nil || !strings.Contains(err.Error(), "rows 1 and 2") {
		t.Fatalf("equivalent selector duplicate error = %v", err)
	}
}

func TestValidateIPRows_ReportsTheEarliestDuplicateInInputOrder(t *testing.T) {
	rows := mustLoadRows(t, []CIDRRecord{
		{IPRaw: "10.0.0.9", IPCidrRaw: "10.0.0.0/24"},
		{IPRaw: "10.0.0.1", IPCidrRaw: "10.0.0.0/24"},
		{IPRaw: "10.0.0.9", IPCidrRaw: "10.0.0.0/24"},
		{IPRaw: "10.0.0.1", IPCidrRaw: "10.0.0.0/24"},
	})
	err := ValidateIPRows(rows)
	if err == nil || !strings.Contains(err.Error(), "rows 1 and 3") {
		t.Fatalf("duplicate error = %v, want earliest duplicate in input order", err)
	}
}

func TestValidateIPRows_ReportsInvalidRowBeforeDuplicate(t *testing.T) {
	duplicate := mustLoadRows(t, []CIDRRecord{
		{IPRaw: "10.0.0.1", IPCidrRaw: "10.0.0.0/24"},
		{IPRaw: "10.0.0.1", IPCidrRaw: "10.0.0.0/24"},
	})
	rows := append([]CIDRRecord{{}}, duplicate...)
	err := ValidateIPRows(rows)
	if err == nil || err.Error() != "row 1 is not parsed" {
		t.Fatalf("validation error = %v, want invalid-row precedence", err)
	}
}

func TestValidateIPRows_WhenRichRowsShareSameIPCidrWithDifferentSrcDstOrPort_ReturnsNil(t *testing.T) {
	rows := mustLoadRows(t, []CIDRRecord{
		{
			IsRich:            true,
			IPRaw:             "10.0.0.10",
			IPCidrRaw:         "10.0.0.0/24",
			Port:              443,
			SrcIP:             "192.168.10.1",
			SrcNetworkSegment: "192.168.10.0/24",
			DstIP:             "10.0.0.10",
			DstNetworkSegment: "10.0.0.0/24",
		},
		{
			IsRich:            true,
			IPRaw:             "10.0.0.20",
			IPCidrRaw:         "10.0.0.0/24",
			Port:              8443,
			SrcIP:             "192.168.20.1",
			SrcNetworkSegment: "192.168.20.0/24",
			DstIP:             "10.0.0.20",
			DstNetworkSegment: "10.0.0.0/24",
		},
	})
	if err := ValidateIPRows(rows); err != nil {
		t.Fatalf("expected rich rows with same ip_cidr but different src/dst/port to be allowed, got %v", err)
	}
}

func TestValidateIPRows_WhenRichRowsDifferOnlyBySrcIP_ReturnsNil(t *testing.T) {
	rows := mustLoadRows(t, []CIDRRecord{
		{
			IsRich:            true,
			IPRaw:             "10.0.0.10",
			IPCidrRaw:         "10.0.0.0/24",
			Port:              443,
			SrcIP:             "192.168.10.1",
			SrcNetworkSegment: "192.168.10.0/24",
			DstIP:             "10.0.0.10",
			DstNetworkSegment: "10.0.0.0/24",
		},
		{
			IsRich:            true,
			IPRaw:             "10.0.0.10",
			IPCidrRaw:         "10.0.0.0/24",
			Port:              443,
			SrcIP:             "192.168.20.1",
			SrcNetworkSegment: "192.168.20.0/24",
			DstIP:             "10.0.0.10",
			DstNetworkSegment: "10.0.0.0/24",
		},
	})
	if err := ValidateIPRows(rows); err != nil {
		t.Fatalf("expected rich rows differing only by src_ip to be allowed, got %v", err)
	}
}

func TestValidateIPRows_WhenRichRowsHaveExactSameSrcDstIPCidrAndPort_ReturnsError(t *testing.T) {
	rows := mustLoadRows(t, []CIDRRecord{
		{
			IsRich:            true,
			IPRaw:             "10.0.0.10",
			IPCidrRaw:         "10.0.0.0/24",
			Port:              443,
			SrcIP:             "192.168.10.1",
			SrcNetworkSegment: "192.168.10.0/24",
			DstIP:             "10.0.0.10",
			DstNetworkSegment: "10.0.0.0/24",
		},
		{
			IsRich:            true,
			IPRaw:             "10.0.0.10",
			IPCidrRaw:         "10.0.0.0/24",
			Port:              443,
			SrcIP:             "192.168.10.1",
			SrcNetworkSegment: "192.168.10.0/24",
			DstIP:             "10.0.0.10",
			DstNetworkSegment: "10.0.0.0/24",
		},
	})
	err := ValidateIPRows(rows)
	if err == nil {
		t.Fatal("expected duplicate tuple error")
	}
	if !strings.Contains(err.Error(), "duplicate (src,dst,ip_cidr,port)") {
		t.Fatalf("expected duplicate tuple error, got %v", err)
	}
}

func TestBasicDuplicateRowKey_WhenOnlyPortDiffers_ReturnsDifferentKeys(t *testing.T) {
	rows := mustLoadRows(t, []CIDRRecord{
		{IPRaw: "10.0.0.1", IPCidrRaw: "10.0.0.0/24", Port: 80},
		{IPRaw: "10.0.0.1", IPCidrRaw: "10.0.0.0/24", Port: 443},
	})

	if basicDuplicateRowKey(rows[0]) == basicDuplicateRowKey(rows[1]) {
		t.Fatal("expected duplicate key to include port")
	}
}

func TestValidateIPRows_WhenIPIsOutsideIPCidr_ReturnsError(t *testing.T) {
	rows := mustLoadRows(t, []CIDRRecord{
		{IPRaw: "10.1.0.1", IPCidrRaw: "10.0.0.0/24"},
	})
	if err := ValidateIPRows(rows); err == nil {
		t.Fatal("expected containment error")
	}
}

func TestValidateIPRows_WhenSelectorCIDRIsOutsideIPCidr_ReturnsError(t *testing.T) {
	rows := mustLoadRows(t, []CIDRRecord{
		{IPRaw: "10.0.1.0/24", IPCidrRaw: "10.0.0.0/24"},
	})
	if err := ValidateIPRows(rows); err == nil {
		t.Fatal("expected selector containment error")
	}
}

func TestValidateIPRows_WhenDifferentIPCidrsOverlap_ReturnsNil(t *testing.T) {
	rows := mustLoadRows(t, []CIDRRecord{
		{IPRaw: "10.0.0.1", IPCidrRaw: "10.0.0.0/24"},
		{IPRaw: "10.0.1.1", IPCidrRaw: "10.0.0.0/16"},
	})
	if err := ValidateIPRows(rows); err != nil {
		t.Fatalf("expected overlapping ip_cidr rows to be allowed, got %v", err)
	}
}

func TestValidateIPRows_WhenSelectorRangesOverlapWithinSameIPCidrButDstDiffers_ReturnsNil(t *testing.T) {
	rows := mustLoadRows(t, []CIDRRecord{
		{IPRaw: "10.0.0.1", IPCidrRaw: "10.0.0.0/24"},
		{IPRaw: "10.0.0.0/31", IPCidrRaw: "10.0.0.0/24"},
	})
	if err := ValidateIPRows(rows); err != nil {
		t.Fatalf("expected selector overlap within same ip_cidr to be allowed, got %v", err)
	}
}

func TestValidateIPRows_WhenSelectorsAreDistinctWithinSameIPCidr_ReturnsNil(t *testing.T) {
	rows := mustLoadRows(t, []CIDRRecord{
		{IPRaw: "10.0.0.1", IPCidrRaw: "10.0.0.0/24"},
		{IPRaw: "10.0.0.2", IPCidrRaw: "10.0.0.0/24"},
		{IPRaw: "10.0.0.8/30", IPCidrRaw: "10.0.0.0/24"},
	})
	if err := ValidateIPRows(rows); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
}

func TestValidateNoOverlap_WhenNetworksDoNotOverlap_ReturnsNil(t *testing.T) {
	rows := mustLoadRows(t, []CIDRRecord{
		{IPRaw: "10.0.0.1", IPCidrRaw: "10.0.0.0/24"},
		{IPRaw: "10.0.1.1", IPCidrRaw: "10.0.1.0/24"},
	})
	if err := ValidateNoOverlap(rows); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
}

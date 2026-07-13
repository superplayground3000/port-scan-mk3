package input

import (
	"strings"
	"testing"
)

func TestLoadCIDRsWithColumns_WhenRichHeaderCaseAndTrimVary_ParsesAndKeepsRowResults(t *testing.T) {
	csv := strings.Join([]string{
		" SRC_IP , SRC_NETWORK_SEGMENT , DST_IP , DST_NETWORK_SEGMENT , SERVICE_LABEL , PROTOCOL , PORT , DECISION , MATCHED_POLICY_ID , REASON ",
		"10.0.0.10,10.0.0.0/24,192.168.1.10,192.168.1.0/24,web,tcp,443,accept,P-1,baseline",
		"10.0.0.11,10.0.0.0/24,192.168.1.11,192.168.1.0/24,web,udp,443,accept,P-2,bad-proto",
	}, "\n")

	rows, err := LoadCIDRsWithColumns(strings.NewReader(csv), "ip", "ip_cidr")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 row results, got %d", len(rows))
	}

	if !rows[0].IsRich || !rows[0].IsValid {
		t.Fatalf("expected first row valid rich row: %+v", rows[0])
	}
	if rows[0].ExecutionKey != "192.168.1.10:443/tcp" {
		t.Fatalf("unexpected execution key: %s", rows[0].ExecutionKey)
	}
	if rows[0].IPRaw != "192.168.1.10" || rows[0].IPCidrRaw != "192.168.1.0/24" {
		t.Fatalf("unexpected mapped fields: %+v", rows[0])
	}

	if rows[1].IsValid {
		t.Fatalf("expected second row invalid: %+v", rows[1])
	}
	if rows[1].ValidationCode != ValidationInvalidProtocol {
		t.Fatalf("unexpected validation code: %s", rows[1].ValidationCode)
	}
}

func TestLoadCIDRsWithColumns_WhenRichHeaderHasExtraColumns_IgnoresThemAndMapsByName(t *testing.T) {
	// The extra columns are interspersed among (not just appended after) the
	// required rich fields, so a positional parser would read the wrong cells.
	// Correct name-based mapping must ignore them and still resolve the schema.
	csv := strings.Join([]string{
		"extra_a,src_ip,src_network_segment,extra_b,dst_ip,dst_network_segment,service_label,protocol,port,decision,matched_policy_id,reason,extra_c",
		"junk-a,10.0.0.10,10.0.0.0/24,junk-b,192.168.1.10,192.168.1.0/24,web,tcp,443,accept,P-1,baseline,junk-c",
	}, "\n")

	rows, err := LoadCIDRsWithColumns(strings.NewReader(csv), "ip", "ip_cidr")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row result, got %d", len(rows))
	}

	rec := rows[0]
	if !rec.IsRich || !rec.IsValid {
		t.Fatalf("expected valid rich row with extra columns ignored: %+v", rec)
	}
	if rec.ExecutionKey != "192.168.1.10:443/tcp" {
		t.Fatalf("extra columns shifted mapping: got execution key %q", rec.ExecutionKey)
	}
	if rec.SrcIP != "10.0.0.10" || rec.DstIP != "192.168.1.10" {
		t.Fatalf("unexpected src/dst mapping: %+v", rec)
	}
	if rec.Port != 443 || rec.Decision != "accept" || rec.ServiceLabel != "web" {
		t.Fatalf("unexpected mapped rich fields: %+v", rec)
	}
	if rec.PolicyID != "P-1" || rec.Reason != "baseline" {
		t.Fatalf("unexpected policy/reason mapping: %+v", rec)
	}
}

func TestLoadCIDRsWithColumns_WhenAllRichRowsInvalid_ReturnsNoUsableError(t *testing.T) {
	csv := strings.Join([]string{
		"src_ip,src_network_segment,dst_ip,dst_network_segment,service_label,protocol,port,decision,matched_policy_id,reason",
		"10.0.0.10,10.0.0.0/24,192.168.1.10,192.168.1.0/24,web,udp,443,accept,P-1,bad-proto",
	}, "\n")

	_, err := LoadCIDRsWithColumns(strings.NewReader(csv), "ip", "ip_cidr")
	if err == nil || !strings.Contains(err.Error(), "no usable input rows") {
		t.Fatalf("expected no usable rows error, got %v", err)
	}
}

func TestLoadCIDRsWithColumns_WhenAliasHeaderUsed_DoesNotTreatAsCanonicalRichField(t *testing.T) {
	csv := strings.Join([]string{
		"source_ip,src_network_segment,dst_ip,dst_network_segment,service_label,protocol,port,decision,matched_policy_id,reason",
		"10.0.0.10,10.0.0.0/24,192.168.1.10,192.168.1.0/24,web,tcp,443,accept,P-1,baseline",
	}, "\n")

	_, err := LoadCIDRsWithColumns(strings.NewReader(csv), "ip", "ip_cidr")
	if err == nil || !strings.Contains(err.Error(), "missing required ip column") {
		t.Fatalf("expected legacy missing-column error, got %v", err)
	}
}

func TestLoadCIDRsWithColumns_WhenSrcOrDstOutOfSegment_MarksRowInvalid(t *testing.T) {
	csv := strings.Join([]string{
		"src_ip,src_network_segment,dst_ip,dst_network_segment,service_label,protocol,port,decision,matched_policy_id,reason",
		"10.0.1.10,10.0.0.0/24,192.168.1.10,192.168.1.0/24,web,tcp,443,accept,P-1,bad-src",
		"10.0.0.10,10.0.0.0/24,192.168.2.10,192.168.1.0/24,web,tcp,443,accept,P-2,bad-dst",
		"10.0.0.10,10.0.0.0/24,192.168.1.10,192.168.1.0/24,web,tcp,443,accept,P-3,ok",
	}, "\n")

	rows, err := LoadCIDRsWithColumns(strings.NewReader(csv), "ip", "ip_cidr")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("unexpected rows len: %d", len(rows))
	}
	if rows[0].ValidationCode != ValidationSrcContainmentFail {
		t.Fatalf("unexpected code for row1: %s", rows[0].ValidationCode)
	}
	if rows[1].ValidationCode != ValidationDstContainmentFail {
		t.Fatalf("unexpected code for row2: %s", rows[1].ValidationCode)
	}
	if !rows[2].IsValid {
		t.Fatalf("expected final row valid: %+v", rows[2])
	}
}

func TestLoadCIDRsWithColumns_WhenDecisionOrPortInvalid_MarksRowInvalidWithReasonCode(t *testing.T) {
	csv := strings.Join([]string{
		"src_ip,src_network_segment,dst_ip,dst_network_segment,service_label,protocol,port,decision,matched_policy_id,reason",
		"10.0.0.10,10.0.0.0/24,192.168.1.10,192.168.1.0/24,web,tcp,abc,accept,P-1,bad-port",
		"10.0.0.10,10.0.0.0/24,192.168.1.11,192.168.1.0/24,web,tcp,443,hold,P-2,bad-decision",
		"10.0.0.10,10.0.0.0/24,192.168.1.12,192.168.1.0/24,web,tcp,443,accept,P-3,ok",
	}, "\n")

	rows, err := LoadCIDRsWithColumns(strings.NewReader(csv), "ip", "ip_cidr")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if rows[0].ValidationCode != ValidationInvalidPort {
		t.Fatalf("unexpected code for invalid port row: %s", rows[0].ValidationCode)
	}
	if rows[1].ValidationCode != ValidationInvalidDecision {
		t.Fatalf("unexpected code for invalid decision row: %s", rows[1].ValidationCode)
	}
	if !rows[2].IsValid {
		t.Fatalf("expected last row valid")
	}
}

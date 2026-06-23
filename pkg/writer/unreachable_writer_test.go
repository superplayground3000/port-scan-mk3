package writer

import (
	"bytes"
	"encoding/csv"
	"strings"
	"testing"
)

func TestUnreachableWriter_WriteHeader_WritesFixedHeaderOnce(t *testing.T) {
	buf := &bytes.Buffer{}
	w := NewUnreachableWriter(buf)

	if err := w.WriteHeader(); err != nil {
		t.Fatalf("first write header failed: %v", err)
	}
	if err := w.WriteHeader(); err != nil {
		t.Fatalf("second write header failed: %v", err)
	}

	rows, err := csv.NewReader(strings.NewReader(buf.String())).ReadAll()
	if err != nil {
		t.Fatalf("csv parse failed: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}

	want := []string{
		"ip",
		"ip_cidr",
		"status",
		"reason",
		"fab_name",
		"cidr_name",
		"service_label",
		"decision",
		"matched_policy_id",
		"execution_key",
		"src_ip",
		"src_network_segment",
	}
	for i, col := range want {
		if rows[0][i] != col {
			t.Fatalf("header[%d] mismatch: got=%s want=%s", i, rows[0][i], col)
		}
	}
}

func TestUnreachableWriter_Write_AutoWritesHeaderAndRowInContractOrder(t *testing.T) {
	buf := &bytes.Buffer{}
	w := NewUnreachableWriter(buf)

	record := UnreachableRecord{
		IP:                "10.0.0.1",
		IPCidr:            "10.0.0.0/24",
		Status:            "unreachable",
		Reason:            "icmp timeout",
		FabName:           "fab-a",
		CIDRName:          "corp",
		ServiceLabel:      "https",
		Decision:          "deny",
		PolicyID:          "policy-1",
		ExecutionKey:      "exec-123",
		SrcIP:             "192.168.1.10",
		SrcNetworkSegment: "segment-a",
	}

	if err := w.Write(record); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	rows, err := csv.NewReader(strings.NewReader(buf.String())).ReadAll()
	if err != nil {
		t.Fatalf("csv parse failed: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}

	wantRow := []string{
		"10.0.0.1",
		"10.0.0.0/24",
		"unreachable",
		"icmp timeout",
		"fab-a",
		"corp",
		"https",
		"deny",
		"policy-1",
		"exec-123",
		"192.168.1.10",
		"segment-a",
	}
	for i, col := range wantRow {
		if rows[1][i] != col {
			t.Fatalf("row[%d] mismatch: got=%s want=%s", i, rows[1][i], col)
		}
	}
}

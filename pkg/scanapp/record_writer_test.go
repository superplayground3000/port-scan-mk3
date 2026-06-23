package scanapp

import (
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/writer"
)

func TestWriterRecordAdapter_ImplementsScanRecord(t *testing.T) {
	rec := writer.Record{
		IP:                "192.168.1.1",
		IPCidr:            "192.168.1.0/24",
		Port:              8080,
		Status:            "open",
		ResponseMS:        42,
		FabName:           "test-fab",
		CIDRName:          "test-cidr",
		ServiceLabel:      "test-service",
		Decision:          "accept",
		PolicyID:          "P-42",
		Reason:            "test-reason",
		ExecutionKey:      "test-key",
		SrcIP:             "10.0.0.1",
		SrcNetworkSegment: "10.0.0.0/8",
	}

	scanRec := AsScanRecord(rec)

	if scanRec.IP() != rec.IP {
		t.Errorf("IP() = %q, want %q", scanRec.IP(), rec.IP)
	}
	if scanRec.IPCidr() != rec.IPCidr {
		t.Errorf("IPCidr() = %q, want %q", scanRec.IPCidr(), rec.IPCidr)
	}
	if scanRec.Port() != rec.Port {
		t.Errorf("Port() = %d, want %d", scanRec.Port(), rec.Port)
	}
	if scanRec.Status() != rec.Status {
		t.Errorf("Status() = %q, want %q", scanRec.Status(), rec.Status)
	}
	if scanRec.ResponseMS() != rec.ResponseMS {
		t.Errorf("ResponseMS() = %d, want %d", scanRec.ResponseMS(), rec.ResponseMS)
	}
	if scanRec.FabName() != rec.FabName {
		t.Errorf("FabName() = %q, want %q", scanRec.FabName(), rec.FabName)
	}
	if scanRec.CIDRName() != rec.CIDRName {
		t.Errorf("CIDRName() = %q, want %q", scanRec.CIDRName(), rec.CIDRName)
	}
	if scanRec.ServiceLabel() != rec.ServiceLabel {
		t.Errorf("ServiceLabel() = %q, want %q", scanRec.ServiceLabel(), rec.ServiceLabel)
	}
	if scanRec.Decision() != rec.Decision {
		t.Errorf("Decision() = %q, want %q", scanRec.Decision(), rec.Decision)
	}
	if scanRec.PolicyID() != rec.PolicyID {
		t.Errorf("PolicyID() = %q, want %q", scanRec.PolicyID(), rec.PolicyID)
	}
	if scanRec.Reason() != rec.Reason {
		t.Errorf("Reason() = %q, want %q", scanRec.Reason(), rec.Reason)
	}
	if scanRec.ExecutionKey() != rec.ExecutionKey {
		t.Errorf("ExecutionKey() = %q, want %q", scanRec.ExecutionKey(), rec.ExecutionKey)
	}
	if scanRec.SrcIP() != rec.SrcIP {
		t.Errorf("SrcIP() = %q, want %q", scanRec.SrcIP(), rec.SrcIP)
	}
	if scanRec.SrcNetworkSegment() != rec.SrcNetworkSegment {
		t.Errorf("SrcNetworkSegment() = %q, want %q", scanRec.SrcNetworkSegment(), rec.SrcNetworkSegment)
	}
}

func TestWriterRecordAdapter_AsWriterRecord(t *testing.T) {
	rec := writer.Record{
		IP:   "192.168.1.1",
		Port: 8080,
	}

	scanRec := AsScanRecord(rec)
	got := scanRec.AsWriterRecord()

	if got.IP != rec.IP {
		t.Errorf("AsWriterRecord().IP = %q, want %q", got.IP, rec.IP)
	}
	if got.Port != rec.Port {
		t.Errorf("AsWriterRecord().Port = %d, want %d", got.Port, rec.Port)
	}
}

func TestRecordWriter_InterfaceSatisfiedByCSVWriter(t *testing.T) {
	// Verify that *writer.CSVWriter satisfies the RecordWriter interface.
	// This test documents the interface contract.
	var _ RecordWriter = (*writer.CSVWriter)(nil)
}

func TestRecordWriter_InterfaceSatisfiedByOpenOnlyWriter(t *testing.T) {
	// Verify that *writer.OpenOnlyWriter satisfies the RecordWriter interface.
	// This test documents the interface contract.
	var _ RecordWriter = (*writer.OpenOnlyWriter)(nil)
}

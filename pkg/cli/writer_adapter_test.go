package cli

import (
	"bytes"
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/writer"
)

func TestRecordWriterAdapter_Write_DelegatesToCSVWriter(t *testing.T) {
	buf := &bytes.Buffer{}
	csv := writer.NewCSVWriter(buf)
	adapter := NewRecordWriterAdapter(csv)

	record := writer.Record{
		IP:     "192.168.1.1",
		Port:   8080,
		Status: "open",
	}

	if err := adapter.Write(record); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	out := buf.String()
	if out == "" {
		t.Fatal("expected output, got empty")
	}
	// Header should be written first, then the record
	if !bytes.Contains(buf.Bytes(), []byte("192.168.1.1")) {
		t.Fatalf("expected IP in output: %s", out)
	}
}

func TestRecordWriterAdapter_WriteHeader_DelegatesToCSVWriter(t *testing.T) {
	buf := &bytes.Buffer{}
	csv := writer.NewCSVWriter(buf)
	adapter := NewRecordWriterAdapter(csv)

	if err := adapter.WriteHeader(); err != nil {
		t.Fatalf("WriteHeader failed: %v", err)
	}

	out := buf.String()
	if !bytes.Contains(buf.Bytes(), []byte("ip")) {
		t.Fatalf("expected header 'ip' in output: %s", out)
	}
}

func TestRecordWriterAdapter_WriteHeader_IsIdempotent(t *testing.T) {
	buf := &bytes.Buffer{}
	csv := writer.NewCSVWriter(buf)
	adapter := NewRecordWriterAdapter(csv)

	// Write header twice
	if err := adapter.WriteHeader(); err != nil {
		t.Fatalf("first WriteHeader failed: %v", err)
	}
	if err := adapter.WriteHeader(); err != nil {
		t.Fatalf("second WriteHeader failed: %v", err)
	}

	// Should only have one header line
	lines := bytes.Count(buf.Bytes(), []byte("\n"))
	if lines != 1 {
		t.Fatalf("expected exactly 1 line (header), got %d lines: %s", lines, buf.String())
	}
}

func TestOpenOnlyRecordWriterAdapter_Write_FiltersNonOpenRecords(t *testing.T) {
	buf := &bytes.Buffer{}
	csv := writer.NewCSVWriter(buf)
	openOnly := writer.NewOpenOnlyWriter(csv)
	adapter := NewOpenOnlyRecordWriterAdapter(openOnly)

	// Write a closed record - should be filtered
	closedRecord := writer.Record{
		IP:     "192.168.1.1",
		Port:   8080,
		Status: "closed",
	}

	if err := adapter.Write(closedRecord); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Should not have written anything since status is not "open"
	if buf.Len() != 0 {
		t.Fatalf("expected empty buffer for non-open record, got: %s", buf.String())
	}
}

func TestOpenOnlyRecordWriterAdapter_Write_ForwardsOpenRecords(t *testing.T) {
	buf := &bytes.Buffer{}
	csv := writer.NewCSVWriter(buf)
	openOnly := writer.NewOpenOnlyWriter(csv)
	adapter := NewOpenOnlyRecordWriterAdapter(openOnly)

	openRecord := writer.Record{
		IP:     "192.168.1.1",
		Port:   8080,
		Status: "open",
	}

	if err := adapter.Write(openRecord); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	if buf.Len() == 0 {
		t.Fatal("expected output for open record, got empty")
	}
	if !bytes.Contains(buf.Bytes(), []byte("192.168.1.1")) {
		t.Fatalf("expected IP in output: %s", buf.String())
	}
}

func TestOpenOnlyRecordWriterAdapter_WriteHeader_DelegatesToInnerWriter(t *testing.T) {
	buf := &bytes.Buffer{}
	csv := writer.NewCSVWriter(buf)
	openOnly := writer.NewOpenOnlyWriter(csv)
	adapter := NewOpenOnlyRecordWriterAdapter(openOnly)

	if err := adapter.WriteHeader(); err != nil {
		t.Fatalf("WriteHeader failed: %v", err)
	}

	if !bytes.Contains(buf.Bytes(), []byte("ip")) {
		t.Fatalf("expected header 'ip' in output: %s", buf.String())
	}
}

func TestRecordWriterAdapter_ImplementsRecordWriterInterface(t *testing.T) {
	// Compile-time interface check is already in writer_adapter.go
	// This test verifies the interface is satisfied at runtime
	var _ interface {
		Write(record writer.Record) error
		WriteHeader() error
	} = (*RecordWriterAdapter)(nil)
}

func TestOpenOnlyRecordWriterAdapter_ImplementsRecordWriterInterface(t *testing.T) {
	// Compile-time interface check is already in writer_adapter.go
	// This test verifies the interface is satisfied at runtime
	var _ interface {
		Write(record writer.Record) error
		WriteHeader() error
	} = (*OpenOnlyRecordWriterAdapter)(nil)
}
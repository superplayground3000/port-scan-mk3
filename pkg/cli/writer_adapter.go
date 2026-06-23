package cli

import (
	"github.com/xuxiping/port-scan-mk3/pkg/scanapp"
	"github.com/xuxiping/port-scan-mk3/pkg/writer"
)

// RecordWriterAdapter wraps a writer.CSVWriter to implement scanapp.RecordWriter.
// It lives in the CLI layer per SOLID: it bridges the domain's RecordWriter
// interface to the concrete writer implementation without polluting domain code
// with transport details.
type RecordWriterAdapter struct {
	w *writer.CSVWriter
}

// NewRecordWriterAdapter creates a RecordWriterAdapter from a CSVWriter.
// The resulting adapter implements scanapp.RecordWriter.
func NewRecordWriterAdapter(csv *writer.CSVWriter) *RecordWriterAdapter {
	return &RecordWriterAdapter{w: csv}
}

// Write forwards the record to the underlying CSVWriter.
func (a *RecordWriterAdapter) Write(record writer.Record) error {
	return a.w.Write(record)
}

// WriteHeader forwards the header write to the underlying CSVWriter.
func (a *RecordWriterAdapter) WriteHeader() error {
	return a.w.WriteHeader()
}

// Compile-time interface check
var _ scanapp.RecordWriter = (*RecordWriterAdapter)(nil)

// OpenOnlyRecordWriterAdapter wraps a writer.OpenOnlyWriter to implement
// scanapp.RecordWriter. The OpenOnlyWriter filters records to include only those
// with Status equal to "open", enabling separate all-results and open-only outputs.
type OpenOnlyRecordWriterAdapter struct {
	w *writer.OpenOnlyWriter
}

// NewOpenOnlyRecordWriterAdapter creates an OpenOnlyRecordWriterAdapter from
// an OpenOnlyWriter. The resulting adapter implements scanapp.RecordWriter.
func NewOpenOnlyRecordWriterAdapter(open *writer.OpenOnlyWriter) *OpenOnlyRecordWriterAdapter {
	return &OpenOnlyRecordWriterAdapter{w: open}
}

// Write forwards only "open" status records to the underlying writer.
func (a *OpenOnlyRecordWriterAdapter) Write(record writer.Record) error {
	return a.w.Write(record)
}

// WriteHeader forwards the header write to the underlying writer.
func (a *OpenOnlyRecordWriterAdapter) WriteHeader() error {
	return a.w.WriteHeader()
}

// Compile-time interface check
var _ scanapp.RecordWriter = (*OpenOnlyRecordWriterAdapter)(nil)
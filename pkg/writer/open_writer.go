// Package writer provides CSV output writers for scan results. The package
// defines the fixed output contract (Columns) that the scanner pipeline uses.
// OpenOnlyWriter filters the records and writes only the open-port results.
//
// # Output Schema
//
//	ip, ip_cidr, port, status, response_time_ms, fab_name, cidr_name,
//	service_label, decision, matched_policy_id, reason, execution_key,
//	src_ip, src_network_segment
//
// # Example
//
//	w := writer.NewCSVWriter(os.Stdout)
//	_ = w.WriteHeader()
//	_ = w.Write(writer.Record{IP: "192.168.1.1", Port: 80, Status: "open"})
package writer

// OpenOnlyWriter is a filter writer that forwards only the records with Status
// equal to "open". OpenOnlyWriter drops all other records without a message.
// OpenOnlyWriter wraps an inner CSVWriter and delegates the header write to it.
type OpenOnlyWriter struct {
	inner *CSVWriter
}

// NewOpenOnlyWriter creates an OpenOnlyWriter that wraps the inner CSVWriter.
// The inner writer must not be nil.
func NewOpenOnlyWriter(inner *CSVWriter) *OpenOnlyWriter {
	return &OpenOnlyWriter{inner: inner}
}

// Write forwards the record to the inner writer only when r.Status == "open".
// Write discards all other records without a message.
func (w *OpenOnlyWriter) Write(r Record) error {
	if w == nil || w.inner == nil {
		return nil
	}
	if r.Status != "open" {
		return nil
	}
	return w.inner.Write(r)
}

// WriteHeader delegates to the WriteHeader method of the inner CSVWriter.
func (w *OpenOnlyWriter) WriteHeader() error {
	if w == nil || w.inner == nil {
		return nil
	}
	return w.inner.WriteHeader()
}

// Flush writes the buffered open-only CSV data to the underlying writer.
func (w *OpenOnlyWriter) Flush() error {
	if w == nil || w.inner == nil {
		return nil
	}
	return w.inner.Flush()
}

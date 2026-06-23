// Package writer provides CSV output writers for scan results.
// It defines the fixed output contract (Columns) used across the scanner pipeline
// and supports filtering via OpenOnlyWriter to emit only open-port results.
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

// OpenOnlyWriter is a filter writer that forwards only records with Status
// equal to "open". All other records are silently dropped. It wraps an inner
// CSVWriter and delegates header writing to it.
type OpenOnlyWriter struct {
	inner *CSVWriter
}

// NewOpenOnlyWriter creates an OpenOnlyWriter that wraps the provided inner
// CSVWriter. The inner writer must not be nil.
func NewOpenOnlyWriter(inner *CSVWriter) *OpenOnlyWriter {
	return &OpenOnlyWriter{inner: inner}
}

// Write forwards the record to the inner writer only if r.Status == "open".
// Non-open records are silently discarded.
func (w *OpenOnlyWriter) Write(r Record) error {
	if w == nil || w.inner == nil {
		return nil
	}
	if r.Status != "open" {
		return nil
	}
	return w.inner.Write(r)
}

// WriteHeader delegates to the inner CSVWriter's WriteHeader.
func (w *OpenOnlyWriter) WriteHeader() error {
	if w == nil || w.inner == nil {
		return nil
	}
	return w.inner.WriteHeader()
}

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

import (
	"bytes"
	"encoding/csv"
	"io"
	"strconv"
	"strings"
)

// Record is one scan result row in the output CSV. The scan pipeline fills all
// fields. An empty field produces an empty CSV cell.
type Record struct {
	IP                string
	IPCidr            string
	Port              int
	Status            string
	ResponseMS        int64
	FabName           string
	CIDR              string
	CIDRName          string
	ServiceLabel      string
	Decision          string
	PolicyID          string
	Reason            string
	ExecutionKey      string
	SrcIP             string
	SrcNetworkSegment string
}

// ColumnDef maps a header name to a function that extracts one field from a
// Record.
type ColumnDef struct {
	// Name is the CSV column header string.
	Name string
	// Extract returns the string value of a Record field for this column.
	Extract func(Record) string
}

// Columns defines the canonical CSV output schema as a single source of truth.
// The schema is: ip, ip_cidr, port, status, response_time_ms, fab_name,
// cidr_name, service_label, decision, matched_policy_id, reason, execution_key,
// src_ip, src_network_segment.
//
// If you change this slice, the output contract changes. Such a change requires
// a MAJOR version bump.
var Columns = []ColumnDef{
	{"ip", func(r Record) string { return r.IP }},
	{"ip_cidr", func(r Record) string {
		if r.IPCidr != "" {
			return r.IPCidr
		}
		return r.CIDR
	}},
	{"port", func(r Record) string { return strconv.Itoa(r.Port) }},
	{"status", func(r Record) string { return r.Status }},
	{"response_time_ms", func(r Record) string { return strconv.FormatInt(r.ResponseMS, 10) }},
	{"fab_name", func(r Record) string { return r.FabName }},
	{"cidr_name", func(r Record) string { return r.CIDRName }},
	{"service_label", func(r Record) string { return r.ServiceLabel }},
	{"decision", func(r Record) string { return r.Decision }},
	{"matched_policy_id", func(r Record) string { return r.PolicyID }},
	{"reason", func(r Record) string { return r.Reason }},
	{"execution_key", func(r Record) string { return r.ExecutionKey }},
	{"src_ip", func(r Record) string { return r.SrcIP }},
	{"src_network_segment", func(r Record) string { return r.SrcNetworkSegment }},
}

// CanonicalHeader returns the exact header line that the CSV writers write for
// the Columns schema. The line holds the column names with the same encoding
// that csv.Writer produces: comma-separated, and quoted only where necessary.
// The line has no trailing newline.
//
// A caller that reopens a result file in append mode compares the existing
// first line of the file against this value. The comparison proves that the
// file uses the current schema before the caller appends to it (design §3.7).
// WriteHeader uses the same csv encoder, so the value matches byte-for-byte,
// minus the line terminator.
func CanonicalHeader() string {
	names := make([]string, len(Columns))
	for i, col := range Columns {
		names[i] = col.Name
	}
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	// Writing a fixed, comma-safe header cannot fail; ignore the error and rely
	// on Flush's error, which is likewise nil for an in-memory buffer.
	_ = w.Write(names)
	w.Flush()
	return strings.TrimRight(buf.String(), "\r\n")
}

// CSVWriter writes scan result rows to a CSV output with the fixed Columns
// header. CSVWriter is safe for concurrent use only when the caller serializes
// each Write call. The scan pipeline serializes the writes at the dispatcher
// level.
type CSVWriter struct {
	w           *csv.Writer
	wroteHeader bool
}

// NewCSVWriter creates a CSVWriter that writes to the io.Writer out. The
// CSVWriter does not take ownership of out. If the underlying writer needs a
// close, the caller must close it.
func NewCSVWriter(out io.Writer) *CSVWriter {
	return &CSVWriter{w: csv.NewWriter(out)}
}

// NewCSVWriterAppending creates a CSVWriter that appends to a destination that
// already holds the canonical header. The CSVWriter starts with
// wroteHeader=true. Therefore the first Write does NOT write a header again,
// and WriteHeader does nothing. Use this constructor when you reopen an
// existing result file in append mode (design §3.7).
func NewCSVWriterAppending(out io.Writer) *CSVWriter {
	return &CSVWriter{w: csv.NewWriter(out), wroteHeader: true}
}

// Write appends one record to the CSV. On the first call, Write also writes the
// header. Write flushes after each write to make sure that the data is visible.
//
// # Parameters
//
//	r: The scan result record to write.
//
// # Returns
//
//	nil on success. An error if the header write or the CSV write fails.
func (cw *CSVWriter) Write(r Record) error {
	if err := cw.WriteHeader(); err != nil {
		return err
	}
	row := make([]string, len(Columns))
	for i, col := range Columns {
		row[i] = col.Extract(r)
	}
	if err := cw.w.Write(row); err != nil {
		return err
	}
	cw.w.Flush()
	return cw.w.Error()
}

// WriteHeader writes the fixed result header one time. Later calls do nothing.
func (cw *CSVWriter) WriteHeader() error {
	if !cw.wroteHeader {
		header := make([]string, len(Columns))
		for i, col := range Columns {
			header[i] = col.Name
		}
		if err := cw.w.Write(header); err != nil {
			return err
		}
		cw.wroteHeader = true
		cw.w.Flush()
		return cw.w.Error()
	}
	return nil
}

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

import (
	"bytes"
	"encoding/csv"
	"io"
	"strconv"
	"strings"
)

// Record is one scan result row written to the output CSV. All fields are
// populated by the scan pipeline; empty fields produce empty CSV cells.
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

// ColumnDef maps a header name to a Record field extractor function.
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
// Changing this slice changes the output contract and requires a MAJOR version bump.
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

// CanonicalHeader returns the exact header line the CSV writers emit for the
// Columns schema: the column names encoded exactly as csv.Writer would encode
// them (comma-separated, quoted only where necessary), with no trailing
// newline. Callers reopening a result file in append mode compare the file's
// existing first line against this value to prove the file was written with the
// current schema before appending to it (design §3.7). Because the value is
// produced by the same csv encoder WriteHeader uses, it matches byte-for-byte
// (minus the line terminator).
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
// header. It is safe for concurrent use only if each Write call is externally
// serialized (the scan pipeline serializes writes at the dispatcher level).
type CSVWriter struct {
	w           *csv.Writer
	wroteHeader bool
}

// NewCSVWriter creates a CSVWriter that writes to the provided io.Writer.
// The writer does not take ownership; callers are responsible for closing
// the underlying writer if needed.
func NewCSVWriter(out io.Writer) *CSVWriter {
	return &CSVWriter{w: csv.NewWriter(out)}
}

// NewCSVWriterAppending creates a CSVWriter for appending to a destination that
// already contains the canonical header. It starts with wroteHeader=true so the
// first Write does NOT re-emit a header and WriteHeader is a no-op. Use this when
// reopening an existing result file in append mode (design §3.7).
func NewCSVWriterAppending(out io.Writer) *CSVWriter {
	return &CSVWriter{w: csv.NewWriter(out), wroteHeader: true}
}

// Write appends a single record to the CSV, writing the header on the first
// call. It flushes after each write to ensure data is visible.
//
// # Parameters
//
//	r: The scan result record to write.
//
// # Returns
//
//	nil on success; error if header write or CSV write fails.
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

// WriteHeader writes the fixed result header once. Subsequent calls are no-ops.
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

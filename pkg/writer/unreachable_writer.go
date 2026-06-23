package writer

import (
	"encoding/csv"
	"io"
)

// UnreachableRecord is one pre-scan unreachable output row written to CSV.
type UnreachableRecord struct {
	IP                string
	IPCidr            string
	Status            string
	Reason            string
	FabName           string
	CIDRName          string
	ServiceLabel      string
	Decision          string
	PolicyID          string
	ExecutionKey      string
	SrcIP             string
	SrcNetworkSegment string
}

type unreachableColumnDef struct {
	name    string
	extract func(UnreachableRecord) string
}

var unreachableColumns = []unreachableColumnDef{
	{name: "ip", extract: func(r UnreachableRecord) string { return r.IP }},
	{name: "ip_cidr", extract: func(r UnreachableRecord) string { return r.IPCidr }},
	{name: "status", extract: func(r UnreachableRecord) string { return r.Status }},
	{name: "reason", extract: func(r UnreachableRecord) string { return r.Reason }},
	{name: "fab_name", extract: func(r UnreachableRecord) string { return r.FabName }},
	{name: "cidr_name", extract: func(r UnreachableRecord) string { return r.CIDRName }},
	{name: "service_label", extract: func(r UnreachableRecord) string { return r.ServiceLabel }},
	{name: "decision", extract: func(r UnreachableRecord) string { return r.Decision }},
	{name: "matched_policy_id", extract: func(r UnreachableRecord) string { return r.PolicyID }},
	{name: "execution_key", extract: func(r UnreachableRecord) string { return r.ExecutionKey }},
	{name: "src_ip", extract: func(r UnreachableRecord) string { return r.SrcIP }},
	{name: "src_network_segment", extract: func(r UnreachableRecord) string { return r.SrcNetworkSegment }},
}

// UnreachableWriter writes unreachable result rows with the fixed contract header.
type UnreachableWriter struct {
	w           *csv.Writer
	wroteHeader bool
}

// NewUnreachableWriter creates a CSV writer for unreachable result output.
func NewUnreachableWriter(out io.Writer) *UnreachableWriter {
	return &UnreachableWriter{w: csv.NewWriter(out)}
}

// Write appends a single unreachable record and writes header first if needed.
func (uw *UnreachableWriter) Write(r UnreachableRecord) error {
	if err := uw.WriteHeader(); err != nil {
		return err
	}
	row := make([]string, len(unreachableColumns))
	for i, col := range unreachableColumns {
		row[i] = col.extract(r)
	}
	if err := uw.w.Write(row); err != nil {
		return err
	}
	uw.w.Flush()
	return uw.w.Error()
}

// WriteHeader writes the fixed unreachable header once.
func (uw *UnreachableWriter) WriteHeader() error {
	if !uw.wroteHeader {
		header := make([]string, len(unreachableColumns))
		for i, col := range unreachableColumns {
			header[i] = col.name
		}
		if err := uw.w.Write(header); err != nil {
			return err
		}
		uw.wroteHeader = true
		uw.w.Flush()
		return uw.w.Error()
	}
	return nil
}

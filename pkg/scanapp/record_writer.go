package scanapp

import "github.com/xuxiping/port-scan-mk3/pkg/writer"

// RecordWriter abstracts writing scan records to output destinations.
// This interface allows the scanapp domain to remain independent of
// concrete writer implementations (e.g., CSVWriter, OpenOnlyWriter).
type RecordWriter interface {
	// Write appends a single record to the output.
	Write(record writer.Record) error
	// WriteHeader writes the output header if not already written.
	WriteHeader() error
}

// ScanRecord represents the data fields of a single scan result record.
// This abstraction allows scanapp domain types to work with scan records
// without depending on the concrete writer.Record type.
type ScanRecord interface {
	// AsWriterRecord returns the underlying writer.Record.
	// This allows interoperability when concrete writer types are required.
	AsWriterRecord() writer.Record

	IP() string
	IPCidr() string
	Port() int
	Status() string
	ResponseMS() int64
	FabName() string
	CIDRName() string
	ServiceLabel() string
	Decision() string
	PolicyID() string
	Reason() string
	ExecutionKey() string
	SrcIP() string
	SrcNetworkSegment() string
}

// writerRecordAdapter adapts writer.Record to satisfy the ScanRecord interface.
type writerRecordAdapter struct {
	record writer.Record
}

func (a *writerRecordAdapter) AsWriterRecord() writer.Record { return a.record }
func (a *writerRecordAdapter) IP() string                    { return a.record.IP }
func (a *writerRecordAdapter) IPCidr() string               { return a.record.IPCidr }
func (a *writerRecordAdapter) Port() int                    { return a.record.Port }
func (a *writerRecordAdapter) Status() string               { return a.record.Status }
func (a *writerRecordAdapter) ResponseMS() int64             { return a.record.ResponseMS }
func (a *writerRecordAdapter) FabName() string               { return a.record.FabName }
func (a *writerRecordAdapter) CIDRName() string              { return a.record.CIDRName }
func (a *writerRecordAdapter) ServiceLabel() string          { return a.record.ServiceLabel }
func (a *writerRecordAdapter) Decision() string              { return a.record.Decision }
func (a *writerRecordAdapter) PolicyID() string              { return a.record.PolicyID }
func (a *writerRecordAdapter) Reason() string                { return a.record.Reason }
func (a *writerRecordAdapter) ExecutionKey() string         { return a.record.ExecutionKey }
func (a *writerRecordAdapter) SrcIP() string                 { return a.record.SrcIP }
func (a *writerRecordAdapter) SrcNetworkSegment() string    { return a.record.SrcNetworkSegment }

// AsScanRecord adapts a writer.Record to the ScanRecord interface.
// This is used by the scan pipeline to bridge concrete writer types to
// the domain's record abstraction.
//
// # Example
//
//	var record writer.Record = ...
//	sr := scanapp.AsScanRecord(record)
//	fmt.Println(sr.Status())
func AsScanRecord(r writer.Record) ScanRecord {
	return &writerRecordAdapter{record: r}
}

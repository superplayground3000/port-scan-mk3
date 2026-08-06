package scanapp

import "github.com/xuxiping/port-scan-mk3/pkg/writer"

// RecordWriter is the interface that writes scan records to output
// destinations. With this interface, the scanapp domain stays independent of
// the concrete writer implementations (for example, CSVWriter and
// OpenOnlyWriter).
type RecordWriter interface {
	// Write appends one record to the output.
	Write(record writer.Record) error
	// WriteHeader writes the output header. If the header is already
	// written, WriteHeader writes nothing.
	WriteHeader() error
}

// ScanRecord represents the data fields of one scan result record. With this
// abstraction, scanapp domain types can use scan records and do not depend on
// the concrete writer.Record type.
type ScanRecord interface {
	// AsWriterRecord returns the underlying writer.Record. It gives
	// interoperability when the caller needs a concrete writer type.
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
func (a *writerRecordAdapter) IPCidr() string                { return a.record.IPCidr }
func (a *writerRecordAdapter) Port() int                     { return a.record.Port }
func (a *writerRecordAdapter) Status() string                { return a.record.Status }
func (a *writerRecordAdapter) ResponseMS() int64             { return a.record.ResponseMS }
func (a *writerRecordAdapter) FabName() string               { return a.record.FabName }
func (a *writerRecordAdapter) CIDRName() string              { return a.record.CIDRName }
func (a *writerRecordAdapter) ServiceLabel() string          { return a.record.ServiceLabel }
func (a *writerRecordAdapter) Decision() string              { return a.record.Decision }
func (a *writerRecordAdapter) PolicyID() string              { return a.record.PolicyID }
func (a *writerRecordAdapter) Reason() string                { return a.record.Reason }
func (a *writerRecordAdapter) ExecutionKey() string          { return a.record.ExecutionKey }
func (a *writerRecordAdapter) SrcIP() string                 { return a.record.SrcIP }
func (a *writerRecordAdapter) SrcNetworkSegment() string     { return a.record.SrcNetworkSegment }

// AsScanRecord adapts a writer.Record to the ScanRecord interface. The scan
// pipeline uses AsScanRecord to bridge concrete writer types to the record
// abstraction of the domain.
//
// # Example
//
//	var record writer.Record = ...
//	sr := scanapp.AsScanRecord(record)
//	fmt.Println(sr.Status())
func AsScanRecord(r writer.Record) ScanRecord {
	return &writerRecordAdapter{record: r}
}

package scanapp

import (
	"github.com/xuxiping/port-scan-mk3/pkg/scanner"
	"github.com/xuxiping/port-scan-mk3/pkg/writer"
)

func recordFromScanTask(task scanTask, res scanner.Result) ScanRecord {
	return AsScanRecord(recordToWriterRecord(task, res))
}

// recordToWriterRecord creates a writer.Record from a scan task and result.
// This helper exists to build the record data before converting to ScanRecord.
func recordToWriterRecord(task scanTask, res scanner.Result) writer.Record {
	return writer.Record{
		IP:                res.IP,
		IPCidr:            task.ipCidr,
		Port:              res.Port,
		Status:            res.Status,
		ResponseMS:        res.ResponseTimeMS,
		FabName:           task.meta.fabName,
		CIDRName:          task.meta.cidrName,
		ServiceLabel:      task.meta.serviceLabel,
		Decision:          task.meta.decision,
		PolicyID:          task.meta.policyID,
		Reason:            task.meta.reason,
		ExecutionKey:      task.meta.executionKey,
		SrcIP:             task.meta.srcIP,
		SrcNetworkSegment: task.meta.srcNetworkSegment,
	}
}

package scanapp

import (
	"github.com/xuxiping/port-scan-mk3/pkg/scanner"
	"github.com/xuxiping/port-scan-mk3/pkg/writer"
)

func recordFromScanTask(task scanTask, res scanner.Result) writer.Record {
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

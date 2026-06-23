package scanapp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/speedctrl"
	"github.com/xuxiping/port-scan-mk3/pkg/writer"
)

type resultSummary struct {
	written      int
	openCount    int
	closeCount   int
	timeoutCount int
}

// writeScanRecord writes a scan record to both the full-results writer and
// the open-only writer. Both writers must implement the RecordWriter interface.
func writeScanRecord(csvWriter, openOnlyWriter RecordWriter, record writer.Record) error {
	if err := csvWriter.Write(record); err != nil {
		return err
	}
	return openOnlyWriter.Write(record)
}

func applyScanResult(runtimes []*chunkRuntime, res scanResult, summary *resultSummary, observer resultTelemetryObserver) *resultSummary {
	if summary == nil {
		summary = &resultSummary{}
	}
	runtimes[res.chunkIdx].tracker.IncrementScanned()
	if observer != nil {
		observer.OnResult()
	}

	summary.written++
	switch {
	case strings.EqualFold(res.record.Status(), "open"):
		summary.openCount++
	case strings.Contains(strings.ToLower(res.record.Status()), "timeout"):
		summary.timeoutCount++
	default:
		summary.closeCount++
	}
	return summary
}

func emitScanResultEvents(stdout io.Writer, logger *scanLogger, ctrl *speedctrl.Controller, progressStep int, runtimes []*chunkRuntime, res scanResult, summary *resultSummary, quiet bool) {
	logger.eventf("scan_result", res.record.IP(), res.record.Port(), "scanned", statusErrorCause(res.record.Status()), map[string]any{
		"status":           res.record.Status(),
		"response_time_ms": res.record.ResponseMS(),
		"cidr":             res.record.IPCidr(),
	})
	if summary == nil || progressStep <= 0 || summary.written%progressStep != 0 || quiet {
		return
	}

	rt := runtimes[res.chunkIdx]
	cidr := rt.tracker.CIDR()
	scanned := rt.tracker.ScannedCount()
	total := rt.tracker.TotalCount()
	_, _ = fmt.Fprintf(stdout, "progress cidr=%s scanned=%d/%d paused=%t\n", cidr, scanned, total, ctrl.IsPaused())
	completionRate := 0.0
	if total > 0 {
		completionRate = float64(scanned) / float64(total)
	}
	logger.eventf("scan_progress", "", 0, "progress", "none", map[string]any{
		"cidr":            cidr,
		"scanned_count":   scanned,
		"total_count":     total,
		"completion_rate": completionRate,
		"paused":          ctrl.IsPaused(),
	})
}

func emitCompletionSummary(logger *scanLogger, summary resultSummary, startedAt time.Time, err error) {
	success := err == nil
	cause := "none"
	if err != nil {
		cause = errorCause(err)
	}
	logger.eventf("scan_completion", "", 0, "completion_summary", cause, map[string]any{
		"total_tasks":   summary.written,
		"open_count":    summary.openCount,
		"close_count":   summary.closeCount,
		"timeout_count": summary.timeoutCount,
		"duration_ms":   time.Since(startedAt).Milliseconds(),
		"success":       success,
	})
}

func statusErrorCause(status string) string {
	s := strings.ToLower(status)
	switch {
	case strings.Contains(s, "timeout"):
		return "timeout"
	case s == "close":
		return "closed"
	default:
		return "none"
	}
}

func errorCause(err error) string {
	if err == nil {
		return "none"
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline_exceeded"
	}
	return "runtime_error"
}

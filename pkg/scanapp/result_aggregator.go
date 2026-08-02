package scanapp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/scanner"
	"github.com/xuxiping/port-scan-mk3/pkg/speedctrl"
	"github.com/xuxiping/port-scan-mk3/pkg/writer"
)

// resultSummary counts written rows by the outcome the row records.
//
// closeCount is deliberately narrow: it counts ONLY rows the remote end
// characterized as shut (scanner.StatusClose). Rows that the scanning host
// failed to send (scanner.StatusLocalError) and rows carrying an unrecognized
// transport error (scanner.StatusUnknown) get their own counters, because
// folding them into closeCount would report the scanner's own socket exhaustion
// to the operator as "the target is closed" — exactly the claim issue #62
// forbids.
type resultSummary struct {
	written         int
	openCount       int
	closeCount      int
	timeoutCount    int
	localErrorCount int
	unknownCount    int
}

// errScanOutputWrite marks every error that originates from persisting a scan
// result row. It is wrapped in at the single point such an error is produced
// (writeScanRecord) so downstream code can classify the failure with errors.Is
// instead of inspecting message text.
//
// The classification matters because the dispatch cursor (task.Chunk.NextIndex)
// advances at enqueue time, not at write time: once a row fails to be written,
// the cursor no longer describes what is on disk, so a resume snapshot saved
// afterwards would make the next -resume skip the rows that were in flight
// (issue #51). persistResumeSnapshot therefore refuses to save for this error
// class and for no other.
var errScanOutputWrite = errors.New("scan output write failed")

// writeScanRecord writes a scan record to both the full-results writer and
// the open-only writer. Both writers must implement the RecordWriter interface.
//
// A failure from either writer is returned wrapped in errScanOutputWrite. The
// caller must treat the record as NOT fully persisted: writes are attempted in
// order, so an open-only failure can leave the row already present in the
// full-results CSV. Counting such a record as scanned is still wrong — the
// dispatch cursor must not be saved either way — so the asymmetry is safe here,
// but it does mean the two output files can differ by that one row.
func writeScanRecord(csvWriter, openOnlyWriter RecordWriter, record writer.Record) error {
	if err := csvWriter.Write(record); err != nil {
		return fmt.Errorf("%w: %w", errScanOutputWrite, err)
	}
	if err := openOnlyWriter.Write(record); err != nil {
		return fmt.Errorf("%w: %w", errScanOutputWrite, err)
	}
	return nil
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
	// The switch is over the exact status enum produced by
	// scanner.statusForOutcome, not over substrings of it: a substring match
	// ("contains timeout") silently reclassifies any status added later, and a
	// default: that means "closed" turns every unforeseen status into a claim
	// about the remote port. Anything this switch does not recognize is counted
	// as unknown, which is the honest answer.
	switch res.record.Status() {
	case scanner.StatusOpen:
		summary.openCount++
	case scanner.StatusCloseTimeout:
		summary.timeoutCount++
	case scanner.StatusClose:
		summary.closeCount++
	case scanner.StatusLocalError:
		summary.localErrorCount++
	default:
		summary.unknownCount++
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
		"total_tasks":       summary.written,
		"open_count":        summary.openCount,
		"close_count":       summary.closeCount,
		"timeout_count":     summary.timeoutCount,
		"local_error_count": summary.localErrorCount,
		"unknown_count":     summary.unknownCount,
		"duration_ms":       time.Since(startedAt).Milliseconds(),
		"success":           success,
	})
}

// statusErrorCause maps a written row's status to the error_cause field of its
// scan_result event. Constitution VI requires the cause to be visible in the
// structured log, so the two non-probing outcomes name themselves rather than
// reporting "none" as if nothing had gone wrong.
func statusErrorCause(status string) string {
	switch status {
	case scanner.StatusCloseTimeout:
		return "timeout"
	case scanner.StatusClose:
		return "closed"
	case scanner.StatusLocalError:
		return "local_resource"
	case scanner.StatusOpen:
		return LogEventNone
	default:
		return "indeterminate"
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

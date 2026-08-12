package scanapp

import (
	"fmt"
	"io"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/scanner"
	"github.com/xuxiping/port-scan-mk3/pkg/speedctrl"
)

type outputCommitterConfig struct {
	outputs                *batchOutputs
	flushInterval          int
	runtimes               []*chunkRuntime
	resultObserver         resultTelemetryObserver
	resultObserverCallback func(completed uint64)
	stdout                 io.Writer
	logger                 *scanLogger
	ctrl                   *speedctrl.Controller
	progressStep           int
	quiet                  bool
}

type pendingChunkOutput struct {
	earliestTask int
	count        int
	statuses     resultSummary
}

type outputCommitter struct {
	config         outputCommitterConfig
	batchID        uint64
	pendingCount   int
	pending        map[int]*pendingChunkOutput
	summary        resultSummary
	committedCount int
	failedCount    int
	maxBatchSize   int
	totalFlushTime time.Duration
	failed         bool
	lastChunkIndex int
}

func newOutputCommitter(config outputCommitterConfig) *outputCommitter {
	return &outputCommitter{
		config:  config,
		batchID: 1,
		pending: make(map[int]*pendingChunkOutput),
	}
}

func (c *outputCommitter) Accept(result scanResult) error {
	if c.failed {
		c.config.runtimes[result.chunkIdx].tracker.MarkUnwritten(result.taskIdx)
		return nil
	}
	c.addPending(result)
	c.lastChunkIndex = result.chunkIdx
	emitPendingScanResult(c.config.logger, result, c.batchID)
	if err := c.config.outputs.write(result.record); err != nil {
		return c.fail(err)
	}
	if c.config.flushInterval > 0 && c.pendingCount >= c.config.flushInterval {
		return c.commit()
	}
	return nil
}

func (c *outputCommitter) Finish() error {
	if c.failed {
		c.emitSummary()
		return nil
	}
	if c.pendingCount > 0 {
		if err := c.commit(); err != nil {
			return err
		}
	}
	c.emitSummary()
	return nil
}

func (c *outputCommitter) emitSummary() {
	c.config.logger.eventf("output_batch_summary", "", 0, "output_batch_summary", LogEventNone, map[string]any{
		"effective_interval": c.config.flushInterval,
		"committed_batches":  c.committedCount,
		"failed_batches":     c.failedCount,
		"maximum_batch_size": c.maxBatchSize,
		"total_flush_ms":     c.totalFlushTime.Milliseconds(),
	})
}

func (c *outputCommitter) Summary() resultSummary {
	return c.summary
}

func (c *outputCommitter) addPending(result scanResult) {
	chunk := c.pending[result.chunkIdx]
	if chunk == nil {
		chunk = &pendingChunkOutput{earliestTask: result.taskIdx}
		c.pending[result.chunkIdx] = chunk
	}
	if result.taskIdx < chunk.earliestTask {
		chunk.earliestTask = result.taskIdx
	}
	chunk.count++
	addStatus(&chunk.statuses, result.record.Status)
	c.pendingCount++
	if c.pendingCount > c.maxBatchSize {
		c.maxBatchSize = c.pendingCount
	}
}

func (c *outputCommitter) commit() error {
	started := time.Now()
	err := c.config.outputs.flush()
	duration := time.Since(started)
	c.totalFlushTime += duration
	if err != nil {
		return c.fail(err)
	}
	batchSize := c.pendingCount
	previousWritten := c.summary.written
	for chunkIndex, pending := range c.pending {
		c.config.runtimes[chunkIndex].tracker.IncrementScannedBy(pending.count)
		mergeSummary(&c.summary, pending.statuses)
		if c.config.resultObserver != nil {
			for range pending.count {
				c.config.resultObserver.OnResult()
			}
		}
	}
	if c.config.resultObserverCallback != nil {
		for completed := previousWritten + 1; completed <= c.summary.written; completed++ {
			c.config.resultObserverCallback(uint64(completed))
		}
	}
	emitCommittedProgress(
		c.config.stdout,
		c.config.logger,
		c.config.ctrl,
		c.config.progressStep,
		c.config.runtimes,
		c.lastChunkIndex,
		previousWritten,
		&c.summary,
		c.config.quiet,
	)
	c.committedCount++
	c.config.logger.eventf("output_batch_committed", "", 0, "output_batch_committed", LogEventNone, map[string]any{
		"batch_id":     c.batchID,
		"result_count": batchSize,
		"flush_ms":     duration.Milliseconds(),
	})
	c.batchID++
	c.pendingCount = 0
	clear(c.pending)
	return nil
}

func (c *outputCommitter) fail(err error) error {
	for chunkIndex, pending := range c.pending {
		c.config.runtimes[chunkIndex].tracker.MarkUnwritten(pending.earliestTask)
	}
	c.failed = true
	c.failedCount++
	c.config.logger.eventf("output_batch_failed", "", 0, "output_batch_failed", errorCause(err), map[string]any{
		"batch_id":                 c.batchID,
		"uncommitted_result_count": c.pendingCount,
	})
	return fmt.Errorf("%w: uncommitted result count %d", err, c.pendingCount)
}

func emitPendingScanResult(logger *scanLogger, result scanResult, batchID uint64) {
	logger.eventf("scan_result", result.record.IP, result.record.Port, "scanned", statusErrorCause(result.record.Status), map[string]any{
		"status":           result.record.Status,
		"response_time_ms": result.record.ResponseMS,
		"cidr":             result.record.IPCidr,
		"output_state":     "pending",
		"batch_id":         batchID,
	})
}

func addStatus(summary *resultSummary, status string) {
	summary.written++
	switch status {
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
}

func mergeSummary(destination *resultSummary, source resultSummary) {
	destination.written += source.written
	destination.openCount += source.openCount
	destination.closeCount += source.closeCount
	destination.timeoutCount += source.timeoutCount
	destination.localErrorCount += source.localErrorCount
	destination.unknownCount += source.unknownCount
}

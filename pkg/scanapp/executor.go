package scanapp

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/scanner"
)

type scanExecutorTelemetry struct {
	mu             sync.Mutex
	stopping       bool
	inFlight       int
	inFlightAtStop int
	abandoned      int
	stopStartedAt  time.Time
}

func (t *scanExecutorTelemetry) markStopping() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.stopping {
		return
	}
	t.stopping = true
	t.inFlightAtStop = t.inFlight
	t.stopStartedAt = time.Now()
}

func (t *scanExecutorTelemetry) startProbe(ctx context.Context) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.stopping || ctx.Err() != nil {
		return false
	}
	t.inFlight++
	return true
}

func (t *scanExecutorTelemetry) finishProbe(ctx context.Context) {
	t.mu.Lock()
	if !t.stopping && ctx.Err() != nil {
		t.stopping = true
		t.inFlightAtStop = t.inFlight
		t.stopStartedAt = time.Now()
	}
	t.inFlight--
	t.mu.Unlock()
}

func (t *scanExecutorTelemetry) abandon() {
	t.mu.Lock()
	t.abandoned++
	t.mu.Unlock()
}

func (t *scanExecutorTelemetry) snapshot() (int, int, time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.inFlightAtStop, t.abandoned, t.stopStartedAt
}

// startScanExecutor launches worker goroutines that consume scanTask items from taskCh,
// execute TCP scans, and emit structured log events for each result.
//
// Parameters:
//   - workers: number of concurrent scan workers; values outside
//     1..config.MaxWorkers are clamped into range, so no caller value can
//     overflow the result-queue sizing or start an unbounded pool
//   - timeout: per-scan connection timeout
//   - dial: TCP dial function (allows injection for testing)
//   - logger: structured logger for scan events
//   - taskCh: channel delivering scan tasks
//
// Returns:
//   - resultCh: closed when all workers finish scanning.
//   - errCh: receives a fatal executor error (for example, recovered worker panic).
func startScanExecutor(workers int, timeout time.Duration, dial DialFunc, logger *scanLogger, taskCh <-chan scanTask) (<-chan scanResult, <-chan error) {
	resultCh, errCh, _, _ := startCancellableScanExecutor(context.Background(), workers, timeout, dial, logger, taskCh)
	return resultCh, errCh
}

func startCancellableScanExecutor(ctx context.Context, workers int, timeout time.Duration, dial DialFunc, logger *scanLogger, taskCh <-chan scanTask) (<-chan scanResult, <-chan error, <-chan scanTask, *scanExecutorTelemetry) {
	executorCtx, stopExecutor := context.WithCancel(ctx)
	resultCh := make(chan scanResult, queueCapacityFor(workers))
	abandonedCh := make(chan scanTask, queueCapacityFor(workers))
	telemetry := &scanExecutorTelemetry{}
	if ctx.Done() != nil {
		go func() {
			<-ctx.Done()
			telemetry.markStopping()
		}()
	}
	workers = effectiveWorkerCount(workers)
	errCh := make(chan error, 1)

	var workerWG sync.WaitGroup
	var errOnce sync.Once
	reportFatal := func(err error) {
		if err == nil {
			return
		}
		errOnce.Do(func() {
			logger.errorf("%v", err)
			telemetry.markStopping()
			stopExecutor()
			errCh <- err
			close(errCh)
		})
	}

	// A local resource failure (Winsock WSAEADDRNOTAVAIL/WSAENOBUFS/WSAEACCES and
	// their POSIX equivalents) means the scanning host failed, not that the target
	// refused: those rows are written as scanner.StatusLocalError, never as a
	// confirmed "close". They are deliberately NOT fatal — see the failure-policy
	// note on scanner.ScanTCP — so every dispatched target still produces exactly
	// one persisted row and the resume cursor stays truthful. One error-level line
	// per run (not per target, which would be unbounded under exhaustion) makes the
	// condition impossible to miss; the per-target detail is in the
	// scan_probe_result events and in the status column of the output CSV.
	var localResourceOnce sync.Once
	reportLocalResource := func(res scanner.Result) {
		if res.Outcome != scanner.OutcomeLocalResource {
			return
		}
		localResourceOnce.Do(func() {
			logger.errorf("local resource failure while dialing %s:%d (%v): the scanning host, not the target, failed; affected rows are recorded as %q and are NOT confirmed closed",
				res.IP, res.Port, res.Error, scanner.StatusLocalError)
		})
	}

	for i := 0; i < workers; i++ {
		workerWG.Add(1)
		go func() {
			defer workerWG.Done()
			var activeTask scanTask
			active := false
			defer func() {
				if r := recover(); r != nil {
					if active {
						telemetry.finishProbe(executorCtx)
						telemetry.abandon()
						abandonedCh <- activeTask
					}
					reportFatal(fmt.Errorf("executor worker panic: %v", r))
					for abandoned := range taskCh {
						telemetry.abandon()
						abandonedCh <- abandoned
					}
				}
			}()
			for {
				var (
					t  scanTask
					ok bool
				)
				select {
				case <-executorCtx.Done():
					telemetry.markStopping()
					for abandoned := range taskCh {
						telemetry.abandon()
						abandonedCh <- abandoned
					}
					return
				case t, ok = <-taskCh:
					if !ok {
						return
					}
				}
				if !telemetry.startProbe(executorCtx) {
					telemetry.abandon()
					abandonedCh <- t
					continue
				}
				activeTask = t
				active = true
				res := scanner.ScanTCP(dial, t.ip, t.port, timeout)
				telemetry.finishProbe(executorCtx)
				active = false
				state := LogEventScanned
				errCause := LogEventNone
				if res.Error != "" {
					errCause = LogEventRuntimeErr
					state = LogEventError
				}
				reportLocalResource(res)
				if logger.enabledEvent(LogEventScanProbeResult) {
					logger.eventf(LogEventScanProbeResult, t.ip, t.port, state, errCause, map[string]any{
						"status":  res.Status,
						"outcome": string(res.Outcome),
						"error":   res.Error,
					})
				}
				resultCh <- scanResult{
					chunkIdx: t.chunkIdx,
					taskIdx:  t.taskIdx,
					record:   recordFromScanTask(t, res),
				}
			}
		}()
	}

	go func() {
		workerWG.Wait()
		stopExecutor()
		close(resultCh)
		close(abandonedCh)
		errOnce.Do(func() {
			close(errCh)
		})
	}()

	return resultCh, errCh, abandonedCh, telemetry
}

package scanapp

import (
	"fmt"
	"sync"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/scanner"
)

// startScanExecutor launches worker goroutines that consume scanTask items from taskCh,
// execute TCP scans, and emit structured log events for each result.
//
// Parameters:
//   - workers: number of concurrent scan workers; must be > 0
//   - timeout: per-scan connection timeout
//   - dial: TCP dial function (allows injection for testing)
//   - logger: structured logger for scan events
//   - taskCh: channel delivering scan tasks
//
// Returns:
//   - resultCh: closed when all workers finish scanning.
//   - errCh: receives a fatal executor error (for example, recovered worker panic).
func startScanExecutor(workers int, timeout time.Duration, dial DialFunc, logger *scanLogger, taskCh <-chan scanTask) (<-chan scanResult, <-chan error) {
	if workers <= 0 {
		workers = 1
	}

	resultCh := make(chan scanResult, workers*2)
	errCh := make(chan error, 1)

	var workerWG sync.WaitGroup
	var errOnce sync.Once
	reportFatal := func(err error) {
		if err == nil {
			return
		}
		errOnce.Do(func() {
			logger.errorf("%v", err)
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
			defer func() {
				if r := recover(); r != nil {
					reportFatal(fmt.Errorf("executor worker panic: %v", r))
				}
			}()
			for t := range taskCh {
				res := scanner.ScanTCP(dial, t.ip, t.port, timeout)
				state := LogEventScanned
				errCause := LogEventNone
				if res.Error != "" {
					errCause = LogEventRuntimeErr
					state = LogEventError
				}
				reportLocalResource(res)
				logger.eventf(LogEventScanProbeResult, t.ip, t.port, state, errCause, map[string]any{
					"status":  res.Status,
					"outcome": string(res.Outcome),
					"error":   res.Error,
				})
				resultCh <- scanResult{
					chunkIdx: t.chunkIdx,
					record:   recordFromScanTask(t, res),
				}
			}
		}()
	}

	go func() {
		workerWG.Wait()
		close(resultCh)
		errOnce.Do(func() {
			close(errCh)
		})
	}()

	return resultCh, errCh
}

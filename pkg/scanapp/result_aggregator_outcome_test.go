package scanapp

import (
	"strings"
	"testing"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/scanner"
	"github.com/xuxiping/port-scan-mk3/pkg/task"
)

// outcomeRuntimes builds the single-chunk runtime slice applyScanResult needs.
func outcomeRuntimes() []*chunkRuntime {
	ch := &task.Chunk{CIDR: "10.0.0.0/24", TotalCount: 8}
	return []*chunkRuntime{{state: ch, tracker: newChunkStateTracker(ch)}}
}

// scanResultWithStatus builds the exact scanResult the executor emits for a
// scanner.Result carrying the given status, so the aggregator is exercised on
// real records rather than hand-written ones.
func scanResultWithStatus(status string) scanResult {
	return scanResult{
		chunkIdx: 0,
		record: recordFromScanTask(
			scanTask{chunkIdx: 0, ipCidr: "10.0.0.0/24", ip: "10.0.0.8", port: 443},
			scanner.Result{IP: "10.0.0.8", Port: 443, Status: status},
		),
	}
}

// Issue #62: a local resource failure is the *scanning host* failing, and an
// unclassifiable transport error characterizes nothing. Writing them to the CSV
// under their own status is only half the fix — the run summary must not turn
// around and report them as confirmed closed ports.
func TestApplyScanResult_WhenNotConfirmedClosed_IsNotCountedAsClose(t *testing.T) {
	for _, status := range []string{scanner.StatusLocalError, scanner.StatusUnknown} {
		t.Run(status, func(t *testing.T) {
			summary := applyScanResult(outcomeRuntimes(), scanResultWithStatus(status), &resultSummary{}, nil)
			if summary.closeCount != 0 {
				t.Fatalf("a %q row was counted as a confirmed closed port: close_count=%d", status, summary.closeCount)
			}
			if summary.openCount != 0 || summary.timeoutCount != 0 {
				t.Fatalf("a %q row was miscounted: open=%d timeout=%d", status, summary.openCount, summary.timeoutCount)
			}
			if summary.written != 1 {
				t.Fatalf("every dispatched target must still be counted as written, got %d", summary.written)
			}
		})
	}
}

// The statuses that DO characterize the target must keep their counters, so the
// fix cannot be satisfied by simply dropping rows out of the summary.
func TestApplyScanResult_WhenTargetAnswered_KeepsExistingCounters(t *testing.T) {
	cases := map[string]func(resultSummary) int{
		scanner.StatusOpen:         func(s resultSummary) int { return s.openCount },
		scanner.StatusClose:        func(s resultSummary) int { return s.closeCount },
		scanner.StatusCloseTimeout: func(s resultSummary) int { return s.timeoutCount },
	}
	for status, get := range cases {
		t.Run(status, func(t *testing.T) {
			summary := applyScanResult(outcomeRuntimes(), scanResultWithStatus(status), &resultSummary{}, nil)
			if got := get(*summary); got != 1 {
				t.Fatalf("status %q: expected its own counter to be 1, got %d", status, got)
			}
		})
	}
}

// The completion summary is the operator's end-of-run report (constitution VI).
// It must name the two non-probing outcomes explicitly instead of hiding them
// inside another counter, otherwise a run that scanned nothing because the host
// ran out of sockets is indistinguishable from a run that found everything shut.
func TestEmitCompletionSummary_ReportsNonProbingOutcomesSeparately(t *testing.T) {
	summary := &resultSummary{}
	summary = applyScanResult(outcomeRuntimes(), scanResultWithStatus(scanner.StatusLocalError), summary, nil)
	summary = applyScanResult(outcomeRuntimes(), scanResultWithStatus(scanner.StatusUnknown), summary, nil)

	logOut := &lockedBuffer{}
	emitCompletionSummary(newLogger("info", true, logOut), *summary, time.Now().Add(-5*time.Millisecond), nil)

	logs := logOut.String()
	for _, required := range []string{
		`"local_error_count":1`,
		`"unknown_count":1`,
		`"close_count":0`,
	} {
		if !strings.Contains(logs, required) {
			t.Fatalf("missing %s in completion summary: %s", required, logs)
		}
	}
}

// A scan_result event for a local failure must not claim "none" as its error
// cause: constitution VI requires the cause to be visible in the structured log.
func TestStatusErrorCause_NamesTheNonProbingOutcomes(t *testing.T) {
	cases := map[string]string{
		scanner.StatusOpen:         "none",
		scanner.StatusClose:        "closed",
		scanner.StatusCloseTimeout: "timeout",
		scanner.StatusLocalError:   "local_resource",
		scanner.StatusUnknown:      "indeterminate",
	}
	for status, want := range cases {
		t.Run(status, func(t *testing.T) {
			if got := statusErrorCause(status); got != want {
				t.Fatalf("statusErrorCause(%q) = %q, want %q", status, got, want)
			}
		})
	}
}

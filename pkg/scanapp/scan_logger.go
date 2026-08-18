package scanapp

import (
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/xuxiping/port-scan-mk3/pkg/logx"
)

// Log event constants for structured logging. They keep the event names
// consistent across the scanning pipeline. They are also the single source of
// truth for log consumers.
const (
	// Scan result states
	LogEventScanned    = "scanned"
	LogEventError      = "error"
	LogEventRuntimeErr = "runtime_error"
	LogEventNone       = "none"

	// Bucket events
	LogEventBucketWaitStart    = "bucket_wait_start"
	LogEventBucketAcquired     = "bucket_acquired"
	LogEventBucketAcquireError = "bucket_acquire_error"

	// Gate events
	LogEventGateWaitStart = "gate_wait_start"
	LogEventGateReleased  = "gate_released"

	// Task events
	LogEventScanResult      = "scan_result"
	LogEventScanProbeResult = "scan_probe_result"
)

type scanLogger struct {
	level  int
	asJSON bool
	out    io.Writer
	// mu serializes writes to out. The logger is shared across scan worker
	// goroutines, and the underlying io.Writer (e.g. bytes.Buffer, os.Stderr)
	// is not safe for concurrent writes.
	mu sync.Mutex
}

func newLogger(level string, asJSON bool, out io.Writer) *scanLogger {
	parsed := 1
	switch strings.ToLower(level) {
	case "debug":
		parsed = 0
	case "info":
		parsed = 1
	case "error":
		parsed = 2
	}
	return &scanLogger{level: parsed, asJSON: asJSON, out: out}
}

func (l *scanLogger) debugf(format string, args ...any) {
	l.logWithFields(0, "debug", fmt.Sprintf(format, args...), nil)
}

func (l *scanLogger) infof(format string, args ...any) {
	l.logWithFields(1, "info", fmt.Sprintf(format, args...), nil)
}

func (l *scanLogger) errorf(format string, args ...any) {
	l.logWithFields(2, "error", fmt.Sprintf(format, args...), nil)
}

func (l *scanLogger) eventf(msg, target string, port int, transition, errCause string, extra map[string]any) {
	if !l.enabledEvent() {
		return
	}
	fields := map[string]any{
		"target":           target,
		"port":             port,
		"state_transition": transition,
		"error_cause":      errCause,
	}
	for k, v := range extra {
		fields[k] = v
	}
	l.logWithFields(1, "info", msg, fields)
}

func (l *scanLogger) logWithFields(level int, levelName, msg string, fields map[string]any) {
	if !l.enabled(level) {
		return
	}
	if fields == nil {
		fields = map[string]any{}
	}
	// Serialize writes: the logger is shared across worker goroutines and the
	// underlying io.Writer is not safe for concurrent use.
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.asJSON {
		logx.LogJSON(l.out, levelName, msg, fields)
		return
	}
	if len(fields) > 0 {
		_, _ = fmt.Fprintf(l.out, "[%s] %s fields=%v\n", strings.ToUpper(levelName), msg, fields)
		return
	}
	_, _ = fmt.Fprintf(l.out, "[%s] %s\n", strings.ToUpper(levelName), msg)
}

func (l *scanLogger) enabledEvent() bool {
	return l.enabled(1)
}

// enabled reports whether a message at level survives the configured verbosity.
// -log-level is the only owner of log verbosity: -quiet suppresses progress and
// per-result console output (see result_aggregator.go) and never filters the
// logger, so an error-level line always reaches stderr under -quiet. A user who
// wants a fully silent run pairs -quiet with -log-level error. See issue #157.
func (l *scanLogger) enabled(level int) bool {
	return l != nil && level >= l.level
}

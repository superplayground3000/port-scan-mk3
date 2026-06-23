package scanapp

import (
	"fmt"
	"io"
	"strings"

	"github.com/xuxiping/port-scan-mk3/pkg/logx"
)

// Log event constants for structured logging. These ensure consistent event names
// across the scanning pipeline and provide a single source of truth for log consumers.
const (
	// Scan result states
	LogEventScanned     = "scanned"
	LogEventError       = "error"
	LogEventRuntimeErr  = "runtime_error"
	LogEventNone        = "none"

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
	quiet  bool
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

func newLoggerWithQuiet(level string, asJSON bool, out io.Writer, quiet bool) *scanLogger {
	l := newLogger(level, asJSON, out)
	l.quiet = quiet
	return l
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
	if l == nil || level < l.level {
		return
	}
	if l.quiet && !isPressureLog(msg) {
		return
	}
	if fields == nil {
		fields = map[string]any{}
	}
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

func isPressureLog(msg string) bool {
	return strings.Contains(msg, "[API]") || strings.Contains(msg, "pressure")
}

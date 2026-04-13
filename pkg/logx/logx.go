// Package logx provides structured JSON logging for the port scanner.
//
// All log output is emitted as newline-delimited JSON (NDJSON) when JSON mode is
// enabled, with a consistent schema: {level, msg, fields, ts}.
//
// # Log Schema
//
//	{
//	  "level":  "info" | "debug" | "error",
//	  "msg":    "...",       // human-readable message
//	  "fields": {...},       // structured key-value pairs
//	  "ts":     "2006-01-02T15:04:05Z07:00"
//	}
//
// # Example
//
//	logx.LogJSON(os.Stdout, "info", "scan_complete", map[string]any{
//	    "total": 100, "open": 5,
//	})
package logx

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"
)

// LogJSON writes a structured log entry as newline-delimited JSON to the
// specified writer. The entry always includes: level, msg, fields, and ts (RFC3339).
//
// # Parameters
//
//	out:     Output writer (e.g., os.Stdout or a file).
//	level:   Log level string: "debug", "info", or "error".
//	msg:     Human-readable message describing the event.
//	fields:  Structured key-value pairs with event-specific data.
//
// # Example
//
//	logx.LogJSON(os.Stdout, "info", "port_open", map[string]any{
//	    "ip": "192.168.1.1", "port": 443, "response_ms": 12,
//	})
func LogJSON(out io.Writer, level, msg string, fields map[string]any) {
	payload := map[string]any{
		"level":  level,
		"msg":    msg,
		"fields": fields,
		"ts":     time.Now().UTC().Format(time.RFC3339),
	}
	if err := json.NewEncoder(out).Encode(payload); err != nil {
		fmt.Fprintf(os.Stderr, "logx: json encode failed: %v\n", err)
	}
}

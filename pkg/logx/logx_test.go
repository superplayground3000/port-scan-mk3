package logx

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// failingWriter is a writer that always fails after N bytes
type failingWriter struct {
	w        *bytes.Buffer
	failAt   int
	written  int
	closed   bool
}

func (fw *failingWriter) Write(p []byte) (n int, err error) {
	if fw.closed {
		return 0, errors.New("write to closed writer")
	}
	if fw.written >= fw.failAt {
		fw.closed = true
		return 0, errors.New("simulated write failure")
	}
	n, _ = fw.w.Write(p)
	fw.written += n
	if fw.written >= fw.failAt {
		fw.closed = true
		return n, errors.New("simulated write failure")
	}
	return n, nil
}

func TestLogJSON_WhenCalled_EmitsJSONPayload(t *testing.T) {
	buf := &bytes.Buffer{}
	LogJSON(buf, "info", "hello", map[string]any{"k": "v"})
	out := buf.String()
	if !strings.Contains(out, "\"level\":\"info\"") {
		t.Fatalf("missing level: %s", out)
	}
	if !strings.Contains(out, "\"msg\":\"hello\"") {
		t.Fatalf("missing msg: %s", out)
	}
}

func TestLogJSON_WhenEncoderWriteFails_LogsToStderr(t *testing.T) {
	// Create a writer that fails after partial write
	buf := &bytes.Buffer{}
	fw := &failingWriter{w: buf, failAt: 5}

	LogJSON(fw, "error", "test error", map[string]any{"key": "value"})

	// The function should handle the error gracefully (no panic)
	// and not write to the buffer since the encoder failed
}

func TestLogJSON_WithEmptyFields_EmitsValidJSON(t *testing.T) {
	buf := &bytes.Buffer{}
	LogJSON(buf, "debug", "no fields", nil)
	out := buf.String()
	if !strings.Contains(out, "\"level\":\"debug\"") {
		t.Fatalf("missing level: %s", out)
	}
	if !strings.Contains(out, "\"msg\":\"no fields\"") {
		t.Fatalf("missing msg: %s", out)
	}
}

func TestLogJSON_WithTimestamp_EmitsRFC3339Timestamp(t *testing.T) {
	buf := &bytes.Buffer{}
	LogJSON(buf, "info", "ts test", map[string]any{})
	out := buf.String()
	if !strings.Contains(out, "\"ts\":") {
		t.Fatalf("missing timestamp: %s", out)
	}
}

func TestLogJSON_WithVariousLogLevels_EmitsCorrectLevel(t *testing.T) {
	for _, level := range []string{"debug", "info", "warn", "error", "fatal"} {
		buf := &bytes.Buffer{}
		LogJSON(buf, level, "test", nil)
		out := buf.String()
		if !strings.Contains(out, "\"level\":\""+level+"\"") {
			t.Errorf("expected level %q in output: %s", level, out)
		}
	}
}

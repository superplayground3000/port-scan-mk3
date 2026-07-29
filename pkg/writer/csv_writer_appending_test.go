package writer

import (
	"bytes"
	"strings"
	"testing"
)

// TestNewCSVWriterAppending_WhenFirstWrite_DoesNotEmitHeader proves the
// appending constructor treats the header as already present, so writing to a
// file that already has a header does not duplicate it (design §3.7).
func TestNewCSVWriterAppending_WhenFirstWrite_DoesNotEmitHeader(t *testing.T) {
	var buf bytes.Buffer
	w := NewCSVWriterAppending(&buf)

	if err := w.Write(Record{IP: "1.2.3.4", IPCidr: "1.2.3.0/24", Port: 80, Status: "open"}); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	out := buf.String()
	if strings.Contains(out, "ip,ip_cidr,port") {
		t.Fatalf("appending writer must not emit header, got: %q", out)
	}
	if !strings.Contains(out, "1.2.3.4") {
		t.Fatalf("expected data row, got: %q", out)
	}
	// Exactly one line (the data row) — no header line.
	if lines := strings.Count(strings.TrimRight(out, "\n"), "\n"); lines != 0 {
		t.Fatalf("expected a single data line, got %d extra newlines: %q", lines, out)
	}
}

// TestNewCSVWriterAppending_WriteHeaderIsNoOp confirms an explicit WriteHeader
// call is suppressed as well (idempotent "already wrote header" state).
func TestNewCSVWriterAppending_WriteHeaderIsNoOp(t *testing.T) {
	var buf bytes.Buffer
	w := NewCSVWriterAppending(&buf)
	if err := w.WriteHeader(); err != nil {
		t.Fatalf("write header failed: %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("expected WriteHeader to be a no-op, got: %q", buf.String())
	}
}

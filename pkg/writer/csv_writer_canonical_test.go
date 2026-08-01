package writer

import (
	"bytes"
	"strings"
	"testing"
)

// TestCanonicalHeader_MatchesWrittenHeaderLine proves CanonicalHeader returns
// exactly the first line WriteHeader emits (minus the trailing newline), so the
// append-mode header validation in scanapp compares against the true on-disk
// header.
func TestCanonicalHeader_MatchesWrittenHeaderLine(t *testing.T) {
	var buf bytes.Buffer
	w := NewCSVWriter(&buf)
	if err := w.WriteHeader(); err != nil {
		t.Fatalf("write header: %v", err)
	}
	firstLine := strings.SplitN(strings.TrimRight(buf.String(), "\r\n"), "\n", 2)[0]
	if got := CanonicalHeader(); got != firstLine {
		t.Fatalf("CanonicalHeader()=%q, written header line=%q", got, firstLine)
	}
	// Sanity: it lists all Columns in order.
	if !strings.HasPrefix(CanonicalHeader(), "ip,ip_cidr,port,status") {
		t.Fatalf("unexpected canonical header: %q", CanonicalHeader())
	}
}

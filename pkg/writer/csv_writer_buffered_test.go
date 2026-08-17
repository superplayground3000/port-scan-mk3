package writer

import (
	"bytes"
	"strings"
	"testing"
)

type countingOutput struct {
	bytes.Buffer
	writes int
}

func (o *countingOutput) Write(data []byte) (int, error) {
	o.writes++
	return o.Buffer.Write(data)
}

func TestBufferedCSVWriterPublishesCompleteRecordsOnlyAfterFlush(t *testing.T) {
	var output bytes.Buffer
	w := NewBufferedCSVWriter(&output)

	if err := w.Write(Record{IP: "192.0.2.1", IPCidr: "192.0.2.1/32", Port: 443, Status: "open"}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if strings.Contains(output.String(), "192.0.2.1") {
		t.Fatalf("record became visible before Flush(): %q", output.String())
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	if !strings.Contains(output.String(), "192.0.2.1,192.0.2.1/32,443,open") {
		t.Fatalf("record is absent after Flush(): %q", output.String())
	}
}

func TestExistingCSVWriterStillFlushesEachRecord(t *testing.T) {
	var output bytes.Buffer
	w := NewCSVWriter(&output)

	if err := w.Write(Record{IP: "192.0.2.2", IPCidr: "192.0.2.2/32", Port: 80, Status: "close"}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if !strings.Contains(output.String(), "192.0.2.2,192.0.2.2/32,80,close") {
		t.Fatalf("existing writer did not publish the record: %q", output.String())
	}
}

func TestBufferedCSVWriterCoalescesOneThousandSmallResults(t *testing.T) {
	var output countingOutput
	w := NewBufferedCSVWriter(&output)
	record := Record{IP: "192.0.2.1", IPCidr: "192.0.2.1/32", Port: 443, Status: "open"}
	for index := 0; index < 1000; index++ {
		if err := w.Write(record); err != nil {
			t.Fatalf("Write(%d) error = %v", index, err)
		}
	}
	if output.writes != 0 {
		t.Fatalf("underlying writes before Flush() = %d, want 0", output.writes)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	if output.writes != 1 {
		t.Fatalf("underlying writes after Flush() = %d, want 1", output.writes)
	}
}

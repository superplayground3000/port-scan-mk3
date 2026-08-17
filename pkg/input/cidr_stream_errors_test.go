package input

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestCountCIDRFileRecordsRejectsIncompleteInputs(t *testing.T) {
	t.Parallel()

	for _, data := range []string{"", "ip,ip_cidr\n"} {
		if _, err := countCIDRFileRecords(context.Background(), strings.NewReader(data), CIDRLimits{}); err == nil {
			t.Fatalf("countCIDRFileRecords accepted %q", data)
		}
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := countCIDRFileRecords(canceled, strings.NewReader("ip,ip_cidr\n127.0.0.1,127.0.0.0/8\n"), CIDRLimits{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("count error = %v, want context cancellation", err)
	}
}

func TestLoadCIDRsSeekableReportsSeekAndSecondPassErrors(t *testing.T) {
	t.Parallel()

	seekFailure := &failingCIDRSeeker{Reader: bytes.NewReader([]byte("ip,ip_cidr\n127.0.0.1,127.0.0.0/8\n"))}
	if _, err := loadCIDRsSeekable(context.Background(), "input.csv", seekFailure, "ip", "ip_cidr", CIDRLimits{}); err == nil || !strings.Contains(err.Error(), "rewind") {
		t.Fatalf("seek error = %v", err)
	}
	malformed := &changingCIDRReadSeeker{
		first:  []byte("ip,ip_cidr\n127.0.0.1,127.0.0.0/8\n"),
		second: []byte("ip,ip_cidr\n\"unterminated\n"),
	}
	if _, err := loadCIDRsSeekable(context.Background(), "input.csv", malformed, "ip", "ip_cidr", CIDRLimits{}); err == nil {
		t.Fatal("loadCIDRsSeekable accepted malformed second-pass CSV")
	}
}

type failingCIDRSeeker struct {
	*bytes.Reader
}

func (reader *failingCIDRSeeker) Seek(int64, int) (int64, error) {
	return 0, io.ErrUnexpectedEOF
}

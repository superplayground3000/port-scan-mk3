package input

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
)

func TestLoadCountedCIDRFileRejectsRecordCountChange(t *testing.T) {
	before := "ip,ip_cidr\n127.0.0.1,127.0.0.0/8\n127.0.0.2,127.0.0.0/8\n"
	after := "ip,ip_cidr\n127.0.0.1,127.0.0.0/8\n"
	file := &changingCIDRReadSeeker{first: []byte(before), second: []byte(after)}

	_, err := loadCIDRsSeekable(context.Background(), "changing.csv", file, "ip", "ip_cidr", CIDRLimits{})
	if err == nil || !strings.Contains(err.Error(), "changed during load: counted 2 records, parsed 1") {
		t.Fatalf("load error = %v", err)
	}
}

type changingCIDRReadSeeker struct {
	first  []byte
	second []byte
	reader *bytes.Reader
}

func (reader *changingCIDRReadSeeker) Read(data []byte) (int, error) {
	if reader.reader == nil {
		reader.reader = bytes.NewReader(reader.first)
	}
	return reader.reader.Read(data)
}

func (reader *changingCIDRReadSeeker) Seek(int64, int) (int64, error) {
	reader.reader = bytes.NewReader(reader.second)
	return 0, nil
}

var _ io.ReadSeeker = (*changingCIDRReadSeeker)(nil)

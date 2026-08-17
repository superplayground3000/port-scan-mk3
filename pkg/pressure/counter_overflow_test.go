package pressure

import (
	"math"
	"testing"
)

func TestIncrementResponseCountRejectsOverflow(t *testing.T) {
	count := uint64(math.MaxUint64)
	if err := incrementResponseCount(&count, "OAuth data entries"); err == nil {
		t.Fatal("incrementResponseCount() accepted an entry counter overflow")
	}
}

func TestCountingReaderRejectsByteCounterOverflow(t *testing.T) {
	reader := &countingReader{reader: fixedReadCount{count: 2}, count: math.MaxUint64 - 1}
	if _, err := reader.Read(make([]byte, 2)); err == nil {
		t.Fatal("Read() accepted a response-byte counter overflow")
	}
}

type fixedReadCount struct {
	count int
}

func (r fixedReadCount) Read([]byte) (int, error) {
	return r.count, nil
}

package input

import (
	"io"
	"math"
	"strings"
	"testing"
)

func TestBoundedInputReaderRejectsConsumedByteOverflow(t *testing.T) {
	reader := &boundedInputReader{
		reader:   strings.NewReader("ab"),
		maxBytes: math.MaxUint64,
		consumed: math.MaxUint64 - 1,
	}

	if _, err := reader.Read(make([]byte, 2)); err == nil {
		t.Fatal("Read() accepted a consumed-byte counter overflow")
	}
}

func TestIncrementInputCountRejectsOverflow(t *testing.T) {
	count := uint64(math.MaxUint64)
	if err := incrementInputCount(&count, "port records"); err == nil {
		t.Fatal("incrementInputCount() accepted a record counter overflow")
	}
}

var _ io.Reader = (*boundedInputReader)(nil)

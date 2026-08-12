package input

import (
	"bytes"
	"testing"
)

func BenchmarkLoadPortsOneMB(b *testing.B) {
	data := bytes.Repeat([]byte("65535/tcp     \n"), 65_535)
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	for b.Loop() {
		if _, err := LoadPorts(bytes.NewReader(data)); err != nil {
			b.Fatal(err)
		}
	}
}

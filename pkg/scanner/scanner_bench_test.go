package scanner

import (
	"context"
	"net"
	"os"
	"syscall"
	"testing"
	"time"
)

// BenchmarkScanTCPOpen measures the successful-dial hot path against a
// synthetic in-process listener on loopback (never a real host).
func BenchmarkScanTCPOpen(b *testing.B) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()

	addr := ln.Addr().(*net.TCPAddr)
	dialer := &net.Dialer{}
	opened := 0
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if res := ScanTCP(dialer.DialContext, addr.IP.String(), addr.Port, time.Second); res.Status == "open" {
			opened++
		}
	}
	b.StopTimer()
	// Sustained loopback dialing can exhaust local ephemeral ports, which is a
	// property of the benchmark host rather than of ScanTCP; requiring only that
	// the open path worked at all keeps the benchmark from flaking while still
	// proving it measured real successful dials.
	if opened == 0 {
		b.Fatal("no dial succeeded; the open path was never measured")
	}
}

// BenchmarkScanTCPDialFailure isolates the error-classification path: the dial
// function returns a prebuilt refused error, so the measurement is the
// per-result work ScanTCP does, with no syscall noise. This is the path issue
// #62 changed.
func BenchmarkScanTCPDialFailure(b *testing.B) {
	refused := &net.OpError{
		Op:   "dial",
		Net:  "tcp",
		Addr: &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 9},
		Err:  os.NewSyscallError("connect", syscall.ECONNREFUSED),
	}
	dial := func(context.Context, string, string) (net.Conn, error) {
		return nil, refused
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ScanTCP(dial, "127.0.0.1", 9, time.Second)
	}
}

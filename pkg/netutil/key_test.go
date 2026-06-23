package netutil

import "testing"

func TestBuildExecutionKey_WhenInputsValid_ReturnsCanonicalKey(t *testing.T) {
	got, err := BuildExecutionKey(" 10.0.0.8 ", 443, "TCP")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "10.0.0.8:443/tcp" {
		t.Fatalf("unexpected key: %s", got)
	}
}

func TestBuildExecutionKey_WhenInputsInvalid_ReturnsError(t *testing.T) {
	tests := []struct {
		name     string
		dstIP    string
		port     int
		protocol string
	}{
		{name: "invalid ip", dstIP: "bad-ip", port: 80, protocol: "tcp"},
		{name: "invalid low port", dstIP: "10.0.0.1", port: 0, protocol: "tcp"},
		{name: "invalid high port", dstIP: "10.0.0.1", port: 65536, protocol: "tcp"},
		{name: "invalid protocol", dstIP: "10.0.0.1", port: 80, protocol: "udp"},
	}

	for _, tt := range tests {
		if _, err := BuildExecutionKey(tt.dstIP, tt.port, tt.protocol); err == nil {
			t.Fatalf("%s: expected error, got nil", tt.name)
		}
	}
}

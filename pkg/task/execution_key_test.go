package task

import (
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/netutil"
)

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
	if _, err := BuildExecutionKey("bad-ip", 80, "tcp"); err == nil {
		t.Fatal("expected invalid ip error")
	}
	if _, err := BuildExecutionKey("10.0.0.1", 0, "tcp"); err == nil {
		t.Fatal("expected invalid port error")
	}
	if _, err := BuildExecutionKey("10.0.0.1", 80, "udp"); err == nil {
		t.Fatal("expected invalid protocol error")
	}
}

func TestBuildExecutionKey_WhenComparedWithNetutil_StaysBehaviorallyConsistent(t *testing.T) {
	tests := []struct {
		dstIP    string
		port     int
		protocol string
	}{
		{dstIP: " 10.0.0.8 ", port: 443, protocol: "TCP"},
		{dstIP: "bad-ip", port: 80, protocol: "tcp"},
		{dstIP: "10.0.0.1", port: 0, protocol: "tcp"},
		{dstIP: "10.0.0.1", port: 80, protocol: "udp"},
		{dstIP: "10.0.0.1", port: 80, protocol: " UDP "},
	}

	for _, tt := range tests {
		taskKey, taskErr := BuildExecutionKey(tt.dstIP, tt.port, tt.protocol)
		netutilKey, netutilErr := netutil.BuildExecutionKey(tt.dstIP, tt.port, tt.protocol)
		if taskKey != netutilKey {
			t.Fatalf("key mismatch for %+v: task=%q netutil=%q", tt, taskKey, netutilKey)
		}
		if (taskErr == nil) != (netutilErr == nil) {
			t.Fatalf("error presence mismatch for %+v: task=%v netutil=%v", tt, taskErr, netutilErr)
		}
		if taskErr != nil && taskErr.Error() != netutilErr.Error() {
			t.Fatalf("error mismatch for %+v: task=%q netutil=%q", tt, taskErr.Error(), netutilErr.Error())
		}
	}
}

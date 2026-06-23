package task

import (
	"net"
	"testing"
)

func TestIndexToIPv4Target_WhenIndexInRange_ReturnsTargetAndPort(t *testing.T) {
	_, n, _ := net.ParseCIDR("10.0.0.0/30")
	ip, port, err := IndexToIPv4Target(n, []int{80, 443}, 3)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if ip != "10.0.0.1" || port != 443 {
		t.Fatalf("unexpected target: %s:%d", ip, port)
	}
}

func TestCountIPv4Hosts_WhenIPv4CIDRProvided_ReturnsHostCount(t *testing.T) {
	_, n, _ := net.ParseCIDR("10.0.0.0/30")
	count, err := CountIPv4Hosts(n)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if count != 4 {
		t.Fatalf("unexpected host count: %d", count)
	}
}

func TestCountIPv4Hosts_WhenNilNetwork_ReturnsError(t *testing.T) {
	_, err := CountIPv4Hosts(nil)
	if err == nil {
		t.Fatal("expected error for nil network")
	}
}

func TestCountIPv4Hosts_WhenIPv6Network_ReturnsError(t *testing.T) {
	_, n, _ := net.ParseCIDR("2001:db8::/32")
	_, err := CountIPv4Hosts(n)
	if err == nil {
		t.Fatal("expected error for IPv6 network")
	}
}

func TestIndexToIPv4Target_WhenNilNetwork_ReturnsError(t *testing.T) {
	_, _, err := IndexToIPv4Target(nil, []int{80}, 0)
	if err == nil {
		t.Fatal("expected error for nil network")
	}
}

func TestIndexToIPv4Target_WhenEmptyPorts_ReturnsError(t *testing.T) {
	_, n, _ := net.ParseCIDR("10.0.0.0/30")
	_, _, err := IndexToIPv4Target(n, []int{}, 0)
	if err == nil {
		t.Fatal("expected error for empty ports")
	}
}

func TestIndexToIPv4Target_WhenNegativeIndex_ReturnsError(t *testing.T) {
	_, n, _ := net.ParseCIDR("10.0.0.0/30")
	_, _, err := IndexToIPv4Target(n, []int{80}, -1)
	if err == nil {
		t.Fatal("expected error for negative index")
	}
}

func TestIndexToIPv4Target_WhenIndexOutOfRange_ReturnsError(t *testing.T) {
	_, n, _ := net.ParseCIDR("10.0.0.0/30")
	// /30 has 4 hosts, with 2 ports that means indices 0-7
	// index 10 is out of range
	_, _, err := IndexToIPv4Target(n, []int{80, 443}, 10)
	if err == nil {
		t.Fatal("expected error for out of range index")
	}
}

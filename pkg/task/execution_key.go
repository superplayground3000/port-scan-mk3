package task

import "github.com/xuxiping/port-scan-mk3/pkg/netutil"

// BuildExecutionKey returns the canonical dedup key `dst_ip:port/protocol`.
// It validates the IPv4 target, the TCP protocol, and the port range before it
// builds the key. The dedup logic of the execution layer uses this key.
func BuildExecutionKey(dstIP string, port int, protocol string) (string, error) {
	return netutil.BuildExecutionKey(dstIP, port, protocol)
}

package task

import "github.com/xuxiping/port-scan-mk3/pkg/netutil"

// BuildExecutionKey returns the canonical dedup key `dst_ip:port/protocol`.
// It validates IPv4 target, TCP protocol, and port range before generating
// the key used by execution-layer dedup logic.
func BuildExecutionKey(dstIP string, port int, protocol string) (string, error) {
	return netutil.BuildExecutionKey(dstIP, port, protocol)
}

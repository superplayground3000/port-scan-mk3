package scanapp

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"strconv"
	"strings"
)

func indexToRuntimeTarget(targets []scanTarget, ports []int, idx int) (scanTarget, int, error) {
	if len(targets) == 0 {
		return scanTarget{}, 0, fmt.Errorf("empty targets")
	}
	if len(ports) == 0 {
		return scanTarget{}, 0, fmt.Errorf("empty ports")
	}
	if idx < 0 {
		return scanTarget{}, 0, fmt.Errorf("negative index")
	}
	if len(ports) == 1 {
		dedicatedPortPerTarget := true
		for i := range targets {
			if targets[i].port <= 0 {
				dedicatedPortPerTarget = false
				break
			}
		}
		if dedicatedPortPerTarget {
			if idx >= len(targets) {
				return scanTarget{}, 0, fmt.Errorf("index out of range")
			}
			return targets[idx], targets[idx].port, nil
		}
	}
	targetIdx := idx / len(ports)
	portIdx := idx % len(ports)
	if targetIdx >= len(targets) {
		return scanTarget{}, 0, fmt.Errorf("index out of range")
	}
	return targets[targetIdx], ports[portIdx], nil
}

func indexToChunkRuntimeTarget(runtime *chunkRuntime, idx int) (scanTarget, int, error) {
	if runtime == nil {
		return scanTarget{}, 0, fmt.Errorf("nil chunk runtime")
	}
	if len(runtime.targets) > 0 && len(runtime.basicTargets) > 0 {
		return scanTarget{}, 0, fmt.Errorf("chunk runtime has mixed target storage")
	}
	if len(runtime.basicTargets) == 0 {
		return indexToRuntimeTarget(runtime.targets, runtime.ports, idx)
	}
	if len(runtime.ports) == 0 {
		return scanTarget{}, 0, fmt.Errorf("empty ports")
	}
	if idx < 0 {
		return scanTarget{}, 0, fmt.Errorf("negative index")
	}
	targetIndex := idx / len(runtime.ports)
	portIndex := idx % len(runtime.ports)
	if targetIndex >= len(runtime.basicTargets) {
		return scanTarget{}, 0, fmt.Errorf("index out of range")
	}
	compact := runtime.basicTargets[targetIndex]
	meta := targetMeta{}
	if compact.meta != nil {
		meta = *compact.meta
	}
	return scanTarget{ip: compact.ip, ipCidr: runtime.ipCidr, ipU32: compact.ipU32, meta: meta}, runtime.ports[portIndex], nil
}

func ipv4ToUint32(ip string) uint32 {
	parsed := net.ParseIP(ip).To4()
	if parsed == nil {
		return 0
	}
	return binary.BigEndian.Uint32(parsed)
}

func parsePortRows(rows []string) ([]int, error) {
	return parsePortRowsContext(context.Background(), rows)
}

func parsePortRowsContext(ctx context.Context, rows []string) ([]int, error) {
	ports := make([]int, 0, len(rows))
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		parts := strings.Split(strings.TrimSpace(row), "/")
		if len(parts) != 2 || strings.ToLower(parts[1]) != "tcp" {
			return nil, fmt.Errorf("invalid chunk port row: %s", row)
		}
		n, err := strconv.Atoi(parts[0])
		if err != nil || n < 1 || n > 65535 {
			return nil, fmt.Errorf("invalid chunk port number: %s", row)
		}
		ports = append(ports, n)
	}
	return ports, nil
}

func defaultString(primary, fallback string) string {
	if strings.TrimSpace(primary) != "" {
		return primary
	}
	return fallback
}

package task

// IndexToTarget maps a flat task index to an IP address and a port number.
//
// It treats the IP list and the port list as a Cartesian product. The flat index
// i maps to IP i/len(ports) and port i%len(ports). This mapping is the inverse of
// the index that the task dispatcher builds when it expands groups.
//
// # Parameters
//
//	idx:   Flat 0-based task index.
//	ips:   Sorted slice of IP address strings.
//	ports: Slice of port numbers.
//
// # Returns
//
//	The IP address and the port at the given index. The values ("", 0) if the
//	index is outside the range, or if an input is empty.
//
// # Example
//
//	ip, port := task.IndexToTarget(5, []string{"192.168.1.1", "192.168.1.2"}, []int{80, 443})
//	// With 2 ports: idx=5 → ipIdx=5/2=2 → out of range → returns ("", 0)
func IndexToTarget(idx int, ips []string, ports []int) (string, int) {
	if idx < 0 || len(ips) == 0 || len(ports) == 0 {
		return "", 0
	}
	ipIdx := idx / len(ports)
	portIdx := idx % len(ports)
	if ipIdx >= len(ips) {
		return "", 0
	}
	return ips[ipIdx], ports[portIdx]
}

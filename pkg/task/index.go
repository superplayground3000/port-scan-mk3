package task

// IndexToTarget maps a flat task index to an IP address and port number.
//
// It treats the IP list and port list as a Cartesian product where the flat
// index i corresponds to IP i/len(ports) and port i%len(ports). This is the
// inverse of the indexing used by the task dispatcher when expanding groups.
//
// # Parameters
//
//	idx:   Flat 0-based task index.
//	ips:   Sorted slice of IP address strings.
//	ports: Slice of port numbers.
//
// # Returns
//
//	The IP address and port at the given index; ("", 0) if the index is
//	out of range or inputs are empty.
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

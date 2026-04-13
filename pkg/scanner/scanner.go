// Package scanner provides TCP port scanning functionality for network diagnostics.
//
// # Function Overview
//
//	ScanTCP(dial, ip, port, timeout) -> Result
//
// # Flow Diagram
//
//	Start
//	  |
//	  v
//	Create context with timeout
//	  |
//	  v
//	Attempt TCP dial through dial function
//	  |
//	  +---> Success -----> Close connection -----> Return Result{Status:"open"}
//	  |
//	  +---> Failure (net.Error timeout)
//	  |       |
//	  |       v
//	  |     Return Result{Status:"close(timeout)"}
//	  |
//	  +---> Failure (context.DeadlineExceeded)
//	  |       |
//	  |       v
//	  |     Return Result{Status:"close(timeout)"}
//	  |
//	  +---> Failure (other error)
//	        |
//	        v
//	      Return Result{Status:"close"}
//
// # Usage Example
//
//	result := ScanTCP((&net.Dialer{}).DialContext, "192.168.1.1", 80, 500*time.Millisecond)
//	if result.Status == "open" {
//	    fmt.Printf("Port %d is open (response time: %dms)\n", result.Port, result.ResponseTimeMS)
//	}
package scanner

import (
	"context"
	"errors"
	"net"
	"strconv"
	"time"
)

// Result holds the outcome of a TCP port scan operation.
type Result struct {
	IP             string
	Port           int
	Status         string
	ResponseTimeMS int64
	Error          string
}

// ScanTCP attempts to establish a TCP connection to the specified IP and port.
//
// It uses a dial function to perform the connection, allowing callers to provide
// custom dialers (e.g., with specific local addresses or network interfaces).
// A timeout is applied via context to prevent indefinite waiting.
//
// # Parameters
//
//   - dial: A function that performs the actual TCP connection (e.g., net.Dialer.DialContext).
//     The function must accept a context, network type ("tcp"), and target address.
//   - ip: The target IP address (IPv4 or IPv6) to scan.
//   - port: The TCP port number to attempt connection on (1-65535).
//   - timeout: The maximum duration to wait for a connection. If zero, no timeout is applied.
//
// # Returns
//
// A Result containing:
//   - IP: The scanned IP address
//   - Port: The scanned port number
//   - Status: "open" when connection succeeds, "close(timeout)" on timeout, "close" on other errors
//   - ResponseTimeMS: Milliseconds for successful connections, 0 otherwise
//   - Error: Error message string when status is not "open", empty otherwise
//
// # Status Values
//
//   - "open": Connection established successfully within the timeout.
//   - "close(timeout)": Connection attempt timed out or context deadline exceeded.
//   - "close": Connection was refused or another non-timeout error occurred.
//
// # Example
//
//	result := ScanTCP((&net.Dialer{}).DialContext, "192.168.1.1", 80, 500*time.Millisecond)
//	if result.Status == "open" {
//	    fmt.Printf("Port %d open, response time: %dms\n", result.Port, result.ResponseTimeMS)
//	} else {
//	    fmt.Printf("Port %d closed: %s\n", result.Port, result.Error)
//	}
func ScanTCP(dial func(context.Context, string, string) (net.Conn, error), ip string, port int, timeout time.Duration) Result {
	target := net.JoinHostPort(ip, strconv.Itoa(port))
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	start := time.Now()
	conn, err := dial(ctx, "tcp", target)
	if err == nil {
		closeErr := conn.Close()
		res := Result{
			IP:             ip,
			Port:           port,
			Status:         "open",
			ResponseTimeMS: time.Since(start).Milliseconds(),
		}
		if closeErr != nil {
			res.Error = "close failed: " + closeErr.Error()
		}
		return res
	}

	if ne, ok := err.(net.Error); ok && ne.Timeout() {
		return Result{
			IP:             ip,
			Port:           port,
			Status:         "close(timeout)",
			ResponseTimeMS: 0,
			Error:          err.Error(),
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return Result{
			IP:             ip,
			Port:           port,
			Status:         "close(timeout)",
			ResponseTimeMS: 0,
			Error:          err.Error(),
		}
	}

	return Result{
		IP:             ip,
		Port:           port,
		Status:         "close",
		ResponseTimeMS: 0,
		Error:          err.Error(),
	}
}

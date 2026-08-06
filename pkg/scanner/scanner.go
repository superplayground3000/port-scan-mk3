// Package scanner provides TCP port scanning for network diagnostics.
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
//	  +---> Failure -----> classifyDialError(runtime.GOOS, err)
//	          |
//	          +---> OutcomeTimeout ---------> Result{Status:"close(timeout)"}
//	          +---> OutcomeRefused ---------> Result{Status:"close"}
//	          +---> OutcomeLocalResource ---> Result{Status:"error(local)"}
//	          +---> OutcomeIndeterminate ---> Result{Status:"unknown"}
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
	"net"
	"runtime"
	"strconv"
	"time"
)

// Result holds the outcome of one TCP port scan operation.
//
// Outcome is the machine-readable classification. Status is the string that the
// scan writes to the scan CSV, and it comes from Outcome. A caller that must
// know *why* a target was not reported open must switch on Outcome. Such a
// caller must not parse Status.
type Result struct {
	IP             string
	Port           int
	Status         string
	Outcome        Outcome
	ResponseTimeMS int64
	Error          string
}

// ScanTCP tries to establish a TCP connection to the given IP and port.
//
// ScanTCP uses a dial function to make the connection. A caller can therefore
// supply a custom dialer, for example one with a specific local address or
// network interface. A context applies the timeout and prevents an unlimited
// wait.
//
// # Parameters
//
//   - dial: A function that makes the TCP connection (for example, net.Dialer.DialContext).
//     The function must accept a context, a network type ("tcp"), and a target address.
//   - ip: The target IP address (IPv4 or IPv6) to scan.
//   - port: The TCP port number to connect to (1-65535).
//   - timeout: The maximum time to wait for a connection. ScanTCP always applies
//     it with context.WithTimeout, so a zero timeout expires immediately and the
//     dial fails at once.
//
// # Returns
//
// A Result containing:
//   - IP: The scanned IP address
//   - Port: The scanned port number
//   - Status: the CSV status string derived from Outcome (see below)
//   - Outcome: the explicit classification of the attempt
//   - ResponseTimeMS: Milliseconds for successful connections, 0 otherwise
//   - Error: Error message string when status is not "open", empty otherwise
//
// # Status Values
//
//   - "open" (OutcomeOpen): the connection was established within the timeout.
//   - "close(timeout)" (OutcomeTimeout): the connection attempt timed out, or the
//     context deadline expired.
//   - "close" (OutcomeRefused): the remote end actively refused or reset the
//     connection. This is the ONLY status that asserts the port is closed.
//   - "error(local)" (OutcomeLocalResource): the dial failed on the scanning host
//     (address or buffer exhaustion, handle limits, permission denied — on Windows
//     WSAEADDRNOTAVAIL, WSAENOBUFS, WSAEACCES). The target was never characterized.
//   - "unknown" (OutcomeIndeterminate): any other transport error. Port state unknown.
//
// # Failure policy for "error(local)" and "unknown"
//
// Both outcomes are INDETERMINATE and NON-FATAL. ScanTCP returns a normal
// Result, the scan writes the row with its own status, and the scan continues.
// This choice keeps the dispatch cursor honest. Every dispatched target still
// produces exactly one persisted row, and resume durability depends on that
// invariant (see the issue #51 entry in .claude/rules/50-lessons.md: a cursor
// that advances at dispatch time is only trustworthy while "dispatched =>
// persisted" holds).
//
// The alternative was to treat a local resource failure as fatal for the whole
// run, and this project rejected it. Winsock ephemeral-port and buffer
// exhaustion is transient and load-dependent. An abort therefore throws away a
// long scan for a condition that the operator can scan again. Under the #51
// rule, the abort also throws away the resume snapshot. A retry inside the dial
// path is also out of scope, because it adds unbounded latency to a hot path. The
// operator sees the affected targets as "error(local)" rows and can scan
// exactly those targets again.
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
			Status:         StatusOpen,
			Outcome:        OutcomeOpen,
			ResponseTimeMS: time.Since(start).Milliseconds(),
		}
		if closeErr != nil {
			res.Error = "close failed: " + closeErr.Error()
		}
		return res
	}

	outcome := classifyDialError(runtime.GOOS, err)
	return Result{
		IP:             ip,
		Port:           port,
		Status:         statusForOutcome(outcome),
		Outcome:        outcome,
		ResponseTimeMS: 0,
		Error:          err.Error(),
	}
}

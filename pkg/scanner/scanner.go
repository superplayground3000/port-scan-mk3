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

// Result holds the outcome of a TCP port scan operation.
//
// Outcome is the machine-readable classification; Status is the string written
// to the scan CSV and is derived from Outcome. Callers that need to reason
// about *why* a target was not reported open must switch on Outcome rather than
// parse Status.
type Result struct {
	IP             string
	Port           int
	Status         string
	Outcome        Outcome
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
//   - Status: the CSV status string derived from Outcome (see below)
//   - Outcome: the explicit classification of the attempt
//   - ResponseTimeMS: Milliseconds for successful connections, 0 otherwise
//   - Error: Error message string when status is not "open", empty otherwise
//
// # Status Values
//
//   - "open" (OutcomeOpen): Connection established successfully within the timeout.
//   - "close(timeout)" (OutcomeTimeout): Connection attempt timed out or context
//     deadline exceeded.
//   - "close" (OutcomeRefused): the remote end actively refused or reset the
//     connection. This is the ONLY status that asserts the port is closed.
//   - "error(local)" (OutcomeLocalResource): the dial failed on the scanning host
//     (address/buffer exhaustion, handle limits, permission denied — on Windows
//     WSAEADDRNOTAVAIL, WSAENOBUFS, WSAEACCES). The target was never characterized.
//   - "unknown" (OutcomeIndeterminate): any other transport error. Port state unknown.
//
// # Failure policy for "error(local)" and "unknown"
//
// Both are INDETERMINATE and NON-FATAL: ScanTCP returns a normal Result, the row
// is written with its own status, and the scan continues. That choice keeps the
// dispatch cursor honest — every dispatched target still produces exactly one
// persisted row, which is the invariant resume durability depends on (see the
// issue #51 entry in .claude/rules/50-lessons.md; a cursor that advances at
// dispatch time is only trustworthy while "dispatched => persisted" holds).
//
// The alternative — treating a local resource failure as fatal for the whole run
// — was rejected: Winsock ephemeral-port/buffer exhaustion is transient and
// load-dependent, so aborting would throw away a long scan (and, under the #51
// rule, the resume snapshot with it) for a condition the operator can simply
// re-scan. Retrying inside the dial path was also rejected as out of scope: it
// would add unbounded latency to a hot path. Operators see the affected targets
// as "error(local)" rows and can re-scan exactly those.
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

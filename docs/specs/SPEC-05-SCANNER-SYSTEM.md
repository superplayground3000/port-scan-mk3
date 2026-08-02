# SPEC-05: Scanner System Specification

## Overview

```
pkg/scanner/
├── scanner.go         # Core TCP probe implementation
├── scanner_test.go   # Unit tests
└── scanner_extra_test.go  # Edge case tests
```

## 1. Core Function

### ScanTCP

```go
func ScanTCP(
    dial func(context.Context, string, string) (net.Conn, error),
    ip string,
    port int,
    timeout time.Duration,
) Result
```

**Parameters:**
| Parameter | Type | Description |
|------------|------|-------------|
| `dial` | `func(context.Context, string, string) (net.Conn, error)` | Dial function (typically `(&net.Dialer{}).DialContext`) |
| `ip` | `string` | Target IP address |
| `port` | `int` | Target port |
| `timeout` | `time.Duration` | Connection timeout |

**Returns:** `Result` struct

## 2. Result Structure

```go
type Result struct {
    IP             string  // Input IP
    Port           int     // Input port
    Status         string  // CSV status string, derived from Outcome
    Outcome        Outcome // Explicit classification of the attempt
    ResponseTimeMS int64   // Elapsed time in milliseconds (only for "open")
    Error          string  // Error message (only for non-"open")
}
```

### Status Values

| Status | Outcome | Description |
|--------|---------|-------------|
| `"open"` | `OutcomeOpen` | TCP connection successful |
| `"close"` | `OutcomeRefused` | Remote end refused or reset the connection (`ECONNREFUSED`/`ECONNRESET`, `WSAECONNREFUSED`/`WSAECONNRESET`). The **only** status that asserts the port is closed |
| `"close(timeout)"` | `OutcomeTimeout` | Connection timed out (`net.Error.Timeout`, context deadline, `ETIMEDOUT`/`WSAETIMEDOUT`) |
| `"error(local)"` | `OutcomeLocalResource` | The dial failed on the **scanning host**: address/buffer exhaustion, handle limits or permission denied (`EADDRNOTAVAIL`/`ENOBUFS`/`EACCES`/`EMFILE`/`EADDRINUSE`/`ENOMEM`, Winsock `WSAEADDRNOTAVAIL`/`WSAENOBUFS`/`WSAEACCES`/`WSAEMFILE`/`WSAEADDRINUSE`/`WSAEPROCLIM`). The target was never characterized |
| `"unknown"` | `OutcomeIndeterminate` | Any other transport error (host/network unreachable, an error carrying no recognizable errno). Port state unknown |

### Failure policy (issue #62)

`error(local)` and `unknown` are **indeterminate and non-fatal**. The scan
continues and each affected target still produces exactly one persisted row, so
the dispatch cursor stays truthful and `-resume` remains consistent (the
invariant established by issue #51). They are never reported as `close`, so no
downstream consumer that filters on `close` — for example
`pkg/preprocess.LoadCleanedCIDRs` — can mistake a local Winsock failure for a
confirmed closed port. The first local resource failure of a run also emits an
`error`-level log line; every occurrence carries `outcome` in its
`scan_probe_result` event.

Rejected alternatives: aborting the run (a transient Winsock exhaustion would
throw away a long scan and, under the issue #51 rule, its resume snapshot), and
retrying inside the dial path (unbounded added latency on a hot path).

## 3. DialFunc Interface

### Definition (from pkg/scanapp/scan.go)

```go
type DialFunc func(context.Context, string, string) (net.Conn, error)
```

### Signature Breakdown

```
(context, network, address)
   ↓        ↓         ↓
 ctx   "tcp"    "10.0.0.1:443"
```

### Common Implementations

**Standard dialer:**
```go
dial := (&net.Dialer{
    Timeout:   timeout,
    LocalAddr: localAddr,  // Optional: bind to specific local port
}).DialContext
```

**Custom dialer (for testing):**
```go
mockDial := func(ctx context.Context, network, address string) (net.Conn, error) {
    // Return mock connection or error
}
```

## 4. Implementation Details

### Connection Flow

```
1. Construct address: net.JoinHostPort(ip, port)
2. Create timeout context: context.WithTimeout(ctx, timeout)
3. Call dial function: dial(ctx, "tcp", address)
4. On success:
   - Close connection immediately
   - Record response time
   - Return Status="open"
5. On error:
   - Classify error type
   - Return appropriate status
```

### Error Classification

```go
// Structural classification only - never error text, which Windows localizes.
// classifyDialError(runtime.GOOS, err):
//   1. errno lookup: *net.OpError -> *os.SyscallError -> syscall.Errno
//      (errors.As fallback for any other wrapping), matched against the
//      Winsock table when goos == "windows" and the portable table otherwise
//   2. net.Error.Timeout() / context.DeadlineExceeded
//   3. otherwise OutcomeIndeterminate
outcome := classifyDialError(runtime.GOOS, err)
return Result{Status: statusForOutcome(outcome), Outcome: outcome, Error: err.Error()}
```

### Response Time Measurement

```go
start := time.Now()
conn, err := dial(ctx, "tcp", address)
elapsed := time.Since(start)

if err != nil {
    // Handle error
}

conn.Close()
return Result{
    Status:         "open",
    ResponseTimeMS: elapsed.Milliseconds(),
}
```

## 5. Usage Patterns

### Basic Usage

```go
result := scanner.ScanTCP(
    (&net.Dialer{}).DialContext,
    "10.0.0.1",
    443,
    100*time.Millisecond,
)

if result.Status == "open" {
    fmt.Printf("Port %d is open (response time: %dms)\n", 
        result.Port, result.ResponseTimeMS)
}
```

### With Custom Timeout

```go
cfg.Timeout = 500 * time.Millisecond
result := scanner.ScanTCP(dial, ip, port, cfg.Timeout)
```

### With Local Address Binding

```go
dialer := &net.Dialer{
    Timeout: timeout,
    LocalAddr: &net.TCPAddr{
        IP:   net.ParseIP("0.0.0.0"),
        Port: 0, // Let OS choose port
    },
}
result := scanner.ScanTCP(dialer.DialContext, ip, port, timeout)
```

### Testing with Mock

```go
mockConn := &mockConn{...}
mockDial := func(ctx context.Context, network, address string) (net.Conn, error) {
    return mockConn, nil
}

result := scanner.ScanTCP(mockDial, "10.0.0.1", 443, timeout)
// result.Status will be "open"
```

## 6. Design Decisions

| Decision | Rationale |
|----------|-----------|
| Injectable dial function | Enables unit testing with mock dialers |
| Context-based timeout | Standard Go pattern for cancellation/timeout |
| Immediate connection close | Stateless port scanning - no connection reuse |
| No connection pooling | Designed for high-volume scanning |
| Minimal footprint | Single probe = single function call |

## 7. Configuration

### Timeout Recommendations

| Network Type | Recommended Timeout |
|--------------|-------------------|
| Local LAN | 100ms - 500ms |
| Data center | 100ms - 200ms |
| Internet | 1s - 5s |

### Worker Pool Configuration

Workers consume `ScanTCP` results. Configuration in `config.Config`:
- `Workers`: Number of concurrent workers
- `Timeout`: Per-probe timeout
- `BucketRate`: Rate limit tokens/second
- `BucketCapacity`: Burst allowance

## 8. Extending the Scanner

### Adding New Protocols

1. Create new function (e.g., `ScanUDP`)
2. Follow same signature pattern
3. Add to `DialFunc` interface if needed

### Adding TLS Support

```go
tlsDial := func(ctx context.Context, network, address string) (net.Conn, error) {
    return tls.Dial("tcp", address, &tls.Config{...})
}
```

### Adding Custom Error Classification

Add the case to `pkg/scanner/dial_error.go` — a new `Outcome` constant, its
errno(s) in `classifyErrno` (Winsock table and/or portable table) and its status
string in `statusForOutcome`. Never classify by error text: Windows localizes it.

```go
const OutcomeSpecial Outcome = "special"

// in classifyErrno
case syscall.ESOMETHING:
    return OutcomeSpecial, true
```

## 9. Implementation Files Reference

| File | Responsibility |
|------|----------------|
| `pkg/scanner/scanner.go` | Core TCP probe implementation |
| `pkg/scanner/dial_error.go` | Dial-error classification (`Outcome`, errno tables, status mapping) |
| `pkg/scanner/scanner_test.go` | Basic unit tests |
| `pkg/scanner/scanner_extra_test.go` | Edge case tests (timeout, refused, local resource, indeterminate) |
| `pkg/scanner/dial_error_test.go` | Winsock/POSIX errno classification tables |
| `pkg/scanner/scanner_bench_test.go` | Dial hot-path benchmarks |
| `pkg/scanapp/scan.go` | DialFunc interface definition |
| `pkg/scanapp/executor.go` | Worker pool consuming ScanTCP |

## 10. Integration Points

- **Executor**: `startScanExecutor()` in `executor.go` calls `ScanTCP`
- **Config**: Timeout from `config.Config.Timeout`
- **Runtime**: Dial function passed via `RunOptions.Dial`

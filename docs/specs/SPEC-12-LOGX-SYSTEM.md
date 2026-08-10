# SPEC-12: Logx System Specification

## Overview

```
pkg/logx/
├── logx.go                   # Logger implementation
├── logx_test.go             # Unit tests
└── logx_contract_test.go   # Contract tests
```

## 1. Logger Structure

### Logger Struct

```go
type Logger struct {
    out io.Writer
    mu  sync.Mutex
}
```

### Constructor

```go
func New(out io.Writer) *Logger
```

Default output: `os.Stdout`

## 2. Log Levels

### Level Constants

```go
const (
    LevelDebug = "debug"
    LevelInfo  = "info"
    LevelWarn  = "warn"
    LevelError = "error"
)
```

### Configuration

The scan workflow gets the level from `config.ScanValues.LogLevel`:
| Value | Behavior |
|-------|----------|
| `"debug"` | Debug, Info, Warn, Error |
| `"info"` | Info, Warn, Error |
| `"warn"` | Warn, Error |
| `"error"` | Error only |

Default: `"info"`

## 3. Logging Methods

### Debug

```go
func (l *Logger) Debug(format string, args ...interface{})
```

### Info

```go
func (l *Logger) Info(format string, args ...interface{})
```

### Warn

```go
func (l *Logger) Warn(format string, args ...interface{})
```

### Error

```go
func (l *Logger) Error(format string, args ...interface{})
```

## 4. Output Format

### Human Format (default)

```
2024/03/18 12:34:56 [INFO] Scanning 10.0.0.0/24:443
2024/03/18 12:34:56 [DEBUG] Dispatching task 1/256
2024/03/18 12:34:57 [INFO] Result: 10.0.0.1:443 open (12ms)
2024/03/18 12:34:57 [WARN] Pressure threshold exceeded, pausing
2024/03/18 12:35:00 [ERROR] Failed to connect: connection refused
```

Format: `YYYY/MM/DD HH:MM:SS [LEVEL] message`

### JSON Format

```json
{"time":"2024-03-18T12:34:56Z","level":"info","message":"Scanning 10.0.0.0/24:443"}
{"time":"2024-03-18T12:34:57Z","level":"debug","message":"Dispatching task 1/256"}
{"time":"2024-03-18T12:34:57Z","level":"info","message":"Result: 10.0.0.1:443 open (12ms)"}
{"time":"2024-03-18T12:34:57Z","level":"warn","message":"Pressure threshold exceeded, pausing"}
{"time":"2024-03-18T12:35:00Z","level":"error","message":"Failed to connect: connection refused"}
```

## 5. Usage in Scan

### Initialization

```go
var logger *logx.Logger
if cfg.LogLevel != "" {
    logger = logx.New(os.Stderr)
} else {
    logger = logx.New(io.Discard)  // No logging
}
```

### Progress Events

In `result_aggregator.go`:
```go
logger.Info("Result: %s:%d %s (%dms)", 
    record.IP, record.Port, record.Status, record.ResponseMS)
```

### Summary Events

```go
logger.Info("Scan complete: written=%d, open=%d, closed=%d, timeout=%d",
    summary.written, summary.openCount, summary.closeCount, summary.timeoutCount)
```

## 6. Thread Safety

The `Logger` uses mutex for thread-safe writing:

```go
func (l *Logger) log(level, format string, args ...interface{}) {
    l.mu.Lock()
    defer l.mu.Unlock()
    // ... write
}
```

## 7. Configuration

### CLI Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-log-level` | "info" | debug/info/warn/error |
| `-format` | "human" | human/json |

### Example

```bash
# Verbose logging
go run ./cmd/port-scan scan -cidr-file ips.csv -log-level debug

# JSON output (for log aggregation)
go run ./cmd/port-scan scan -cidr-file ips.csv -format json

# Quiet (no logging)
go run ./cmd/port-scan scan -cidr-file ips.csv -log-level error
```

## 8. Design Decisions

| Decision | Rationale |
|----------|-----------|
| Simple interface | Just Debug/Info/Warn/Error |
| Configurable level | Filter unwanted noise |
| Thread-safe | Concurrent goroutines |
| Two formats | Human for terminal, JSON for tools |
| Default to stderr | Keep stdout for actual output |

## 9. Adding New Log Levels

### Step 1: Add constant

```go
const (
    // ... existing
    LevelFatal = "fatal"
)
```

### Step 2: Add method

```go
func (l *Logger) Fatal(format string, args ...interface{}) {
    l.log(LevelFatal, format, args)
    os.Exit(1)
}
```

## 10. Implementation Files Reference

| File | Responsibility |
|------|----------------|
| `pkg/logx/logx.go` | Logger implementation |
| `pkg/logx/logx_test.go` | Unit tests |
| `pkg/logx/logx_contract_test.go` | Contract tests |
| `pkg/scanapp/scan_logger.go` | Scan-specific logging |

## 11. Integration Points

- **Config**: Log level from `config.ScanValues.LogLevel`
- **Scan**: `scanapp.Run` creates its private scan logger after it resolves the
  configuration
- **Result Aggregator**: Progress and summary logging
- **Pressure Monitor**: State change logging
- **Dispatcher**: Debug logging for task dispatch

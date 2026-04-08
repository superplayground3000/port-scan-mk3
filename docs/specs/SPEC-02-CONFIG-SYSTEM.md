# SPEC-02: Configuration System Specification

## Overview

```
pkg/config/
├── config.go        # Config struct and Parse function
└── config_test.go  # Unit tests
```

## 1. Config Struct

```go
type Config struct {
    // Input files
    CIDRFile string  // Required: CIDR CSV path
    PortFile string  // Optional: Port list path (required in default mode, optional in rich mode)

    // Output
    Output string    // Default: "scan_results.csv"

    // Scanning
    Timeout        time.Duration // Default: 100ms
    Delay          time.Duration // Default: 10ms
    BucketRate     int           // Default: 100 (tokens per second)
    BucketCapacity int           // Default: 100 (burst capacity)
    Workers        int           // Default: 10

    // Pressure control
    PressureAPI      string        // Default: "http://localhost:8080/api/pressure"
    PressureInterval time.Duration // Default: 5s
    DisableAPI       bool          // Default: false

    // Resume
    Resume string // Optional: resume state file path

    // Logging/Output
    LogLevel string // Default: "info"
    Format   string // Default: "human" (or "json")

    // Column mapping
    CIDRIPCol     string // Default: "ip"
    CIDRIPCidrCol string // Default: "ip_cidr"
}
```

## 2. Parse Function

### Signature

```go
func Parse(args []string) (Config, error)
```

### Flag Definitions

| Flag | Type | Default | Required | Notes |
|------|------|---------|----------|-------|
| `-cidr-file` | string | "" | YES | Path to CIDR CSV |
| `-port-file` | string | "" | NO* | *Required in default mode |
| `-output` | string | "scan_results.csv" | NO | Output directory anchor |
| `-timeout` | duration | 100ms | NO | TCP connect timeout |
| `-delay` | duration | 10ms | NO | Inter-task delay |
| `-bucket-rate` | int | 100 | NO | Tokens per second |
| `-bucket-capacity` | int | 100 | NO | Burst allowance |
| `-workers` | int | 10 | NO | Worker pool size |
| `-pressure-api` | string | "http://localhost:8080/api/pressure" | NO | Pressure API URL |
| `-pressure-interval` | duration/int | 5s | NO | Supports "5" or "5s" |
| `-disable-api` | bool | false | NO | Disable pressure API |
| `-resume` | string | "" | NO | Resume state path |
| `-log-level` | string | "info" | NO | debug/info/error |
| `-format` | string | "human" | NO | human/json |
| `-cidr-ip-col` | string | "ip" | NO | Custom IP column name |
| `-cidr-ip-cidr-col` | string | "ip_cidr" | NO | Custom CIDR column name |

### Validation Rules

| Rule | Error Message | Exit Code |
|------|---------------|-----------|
| `-cidr-file` must be non-empty | "-cidr-file is required" | 2 |
| `-cidr-ip-col` must be non-empty | "-cidr-ip-col is required" | 2 |
| `-cidr-ip-cidr-col` must be non-empty | "-cidr-ip-cidr-col is required" | 2 |
| `-format` must be "human" or "json" | "-format must be 'human' or 'json'" | 2 |
| `-pressure-interval` must be > 0 | "-pressure-interval must be positive" | 2 |

### Special Parsing

#### Pressure Interval

Accepts two formats:
- Duration string: `5s`, `100ms`, `1m30s`
- Plain integer (seconds): `5`, `10`

Implementation in `config.go`:
```go
pressureIntervalRaw := fs.String("pressure-interval", "5s", "...")
var PressureInterval time.Duration
if i, err := strconv.Atoi(pressureIntervalRaw); err == nil {
    PressureInterval = time.Duration(i) * time.Second
} else {
    PressureInterval, _ = time.ParseDuration(pressureIntervalRaw)
}
```

## 3. Default Values

| Field | Default Value | Go Type |
|-------|--------------|---------|
| `Output` | "scan_results.csv" | string |
| `Timeout` | 100ms | time.Duration |
| `Delay` | 10ms | time.Duration |
| `BucketRate` | 100 | int |
| `BucketCapacity` | 100 | int |
| `Workers` | 10 | int |
| `PressureAPI` | "http://localhost:8080/api/pressure" | string |
| `PressureInterval` | 5s | time.Duration |
| `DisableAPI` | false | bool |
| `Resume` | "" | string |
| `LogLevel` | "info" | string |
| `Format` | "human" | string |
| `CIDRIPCol` | "ip" | string |
| `CIDRIPCidrCol` | "ip_cidr" | string |

## 4. Error Handling

### InvalidFlagError

```go
var InvalidFlagError = errors.New("invalid flag")

func (e *invalidFlagError) Error() string {
    return e.message
}
```

All validation errors wrap with `InvalidFlagError` and return exit code 2.

## 5. Usage Pattern

### Basic Usage

```go
cfg, err := config.Parse(os.Args[1:])
if err != nil {
    if errors.Is(err, config.InvalidFlagError) {
        return 2
    }
    return 1
}
```

### With Custom Flags

```go
fs := flag.NewFlagSet("custom", flag.ContinueOnError)
fs.String("cidr-file", "", "...")
fs.String("output", "results.csv", "...")

// Parse and override defaults
cfg, err := config.ParseWithFlagSet(fs, os.Args[1:])
```

## 6. Adding New Configuration Options

### Step 1: Add field to Config struct

```go
type Config struct {
    // ... existing fields
    NewOption string  // Add here
}
```

### Step 2: Add flag definition in Parse()

```go
fs.StringVar(&cfg.NewOption, "new-option", "default_value", "description")
```

### Step 3: Add validation (if needed)

```go
if cfg.NewOption != "valid1" && cfg.NewOption != "valid2" {
    return Config{}, &invalidFlagError{
        message: "-new-option must be 'valid1' or 'valid2'",
    }
}
```

### Step 4: Add test case in config_test.go

```go
func TestParse_WithInvalidNewOption_ReturnsError(t *testing.T) {
    _, err := config.Parse([]string{"-cidr-file", "test.csv", "-new-option", "invalid"})
    require.Error(t, err)
    assert.Contains(t, err.Error(), "-new-option must be")
}
```

## 7. Implementation Files Reference

| File | Responsibility |
|------|----------------|
| `pkg/config/config.go` | Config struct, Parse function, flag definitions |
| `pkg/config/config_test.go` | Unit tests for parsing and validation |

## 8. Integration Points

- **CLI**: `config.Parse(args)` consumed by `cmd/port-scan`
- **Validation**: `config.Config` passed to `validate.Inputs(cfg)`
- **Scan**: `config.Config` passed to `scanapp.Run(ctx, cfg, ...)`
- **Input**: Column names from `cfg.CIDRIPCol`, `cfg.CIDRIPCidrCol`

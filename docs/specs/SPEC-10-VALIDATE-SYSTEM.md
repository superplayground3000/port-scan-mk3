# SPEC-10: Validate System Specification

## Overview

```
pkg/validate/
├── service.go        # Main validation service
└── service_test.go   # Unit tests
```

## 1. Main Entry Point

### Inputs Function

```go
type Configuration interface {
    Resolve() (config.ValidateValues, error)
}

func Inputs(cfg Configuration) Result
```

**Parameters:**
- `cfg` - Consumer-owned configuration that resolves file paths and column names.

**Returns:**
```go
type Result struct {
    Valid  bool   // true = validation passed
    Detail string // "ok" on success, error message on failure
}
```

## 2. Validation Rules

### 2.1 CIDR File Existence

```
Rule: CIDR file must exist and be readable
Error: "open cidr-file: <os error>"
```

### 2.2 CIDR Records Loading

```
Rule: CIDR CSV must be parseable
Error: "load cidr: <parse error>"
```

Uses: `input.LoadCIDRsWithColumns(values.CIDRFile, values.CIDRIPCol, values.CIDRIPCidrCol)`

### 2.3 Port File Requirement

**In Default Mode (not rich CSV):**
```
Rule: Port file is REQUIRED
Error: "port-file is required in default mode"
```

**In Rich CSV Mode (all 10 rich fields present):**
```
Rule: Port file is OPTIONAL
Validation: PASSES even if port file not provided
```

### 2.4 Port File Parsing

```
Rule: Port file must be parseable
Format: <port>/tcp (one per line)
Error: "load ports: <parse error>"
```

Uses: `input.LoadPorts(values.PortFile)`

## 3. Validation Flow

```
validate.Inputs(cfg)
     │
     ├─► Resolve configuration
     │      │
     │      └─► Error: invalid Result before file I/O
     │
     ├─► Open CIDR file
     │      │
     │      └─► Error: "open cidr-file: ..."
     │
     ├─► LoadCIDRsWithColumns()
     │      │
     │      ├─► Auto-detect: Rich CSV?
     │      │      │
     │      │      └─► Yes: Port file optional
     │      │
     │      └─► Error: "load cidr: ..."
     │
     ├─► Mode: Rich CSV?
     │      │
     │      ├─► YES: Return Valid (port file optional)
     │      │
     │      └─► NO: Port file required
     │             │
     │             ├─► Port file provided → LoadPorts()
     │             │        │
     │             │        └─► Error: "load ports: ..."
     │             │
     │             └─► No port file → Error: "port-file is required"
     │
     └─► Return Result{Valid: true, Detail: "ok"}
```

## 4. Error Messages

| Error | Cause |
|-------|-------|
| `open cidr-file: ...` | File doesn't exist or not readable |
| `load cidr: ...` | CSV parsing error |
| `port-file is required in default mode` | Missing port file in non-rich mode |
| `load ports: ...` | Port file parsing error |

## 5. Row-Level Validation

Performed by `input.LoadCIDRsWithColumns()`:

| Rule | Error |
|------|-------|
| IP must be valid IPv4 | Part of load error |
| CIDR must be valid IPv4 | Part of load error |
| IP must be within CIDR | Part of load error |

## 6. Test Cases

### TestInputs_WhenFilesAreValid_ReturnsValidResult

```go
func TestInputs_WhenFilesAreValid_ReturnsValidResult(t *testing.T) {
    // Setup valid CIDR and port files
    // Call validate.Inputs(cfg)
    // Assert: Result.Valid == true
}
```

### TestInputs_WhenCIDRRowsInvalid_ReturnsInvalidDetail

```go
func TestInputs_WhenCIDRRowsInvalid_ReturnsInvalidDetail(t *testing.T) {
    // Setup: IP outside CIDR range
    // Call validate.Inputs(cfg)
    // Assert: Result.Valid == false
    // Assert: strings.Contains(Result.Detail, "load cidr")
}
```

### TestInputs_WhenRichCSVAndPortFileMissing_ReturnsValidResult

```go
func TestInputs_WhenRichCSVAndPortFileMissing_ReturnsValidResult(t *testing.T) {
    // Setup: Rich CSV (all 10 fields), no port file
    // Call validate.Inputs(cfg)
    // Assert: Result.Valid == true
}
```

### TestInputs_WhenDefaultCSVAndPortFileMissing_ReturnsInvalidDetail

```go
func TestInputs_WhenDefaultCSVAndPortFileMissing_ReturnsInvalidDetail(t *testing.T) {
    // Setup: Default CSV, no port file
    // Call validate.Inputs(cfg)
    // Assert: Result.Valid == false
    // Assert: strings.Contains(Result.Detail, "port-file is required")
}
```

## 7. Integration with CLI

### validate Command Handler

```go
func handleValidateCommand(args []string, stdout, stderr io.Writer) int {
    cfg, err := config.ParseValidate(args)
    if err != nil {
        return 2  // CLI error
    }

    values, err := cfg.Resolve()
    if err != nil {
        return 2
    }

    result := validate.Inputs(cfg)

    cli.WriteValidation(stdout, values.Format, result.Valid, result.Detail)
    
    if result.Valid {
        return 0
    }
    return 1  // Validation failed
}
```

## 8. Design Decisions

| Decision | Rationale |
|----------|-----------|
| Fail-fast | Stops at first error |
| Auto-detect rich mode | No explicit flag needed |
| Port file optional in rich mode | Rich CSV already contains ports |
| Clear error messages | Easy to diagnose issues |

## 9. Adding New Validation Rules

### Step 1: Add validation in Inputs()

```go
func Inputs(cfg Configuration) Result {
    // ... existing validation
    
    // Add new validation
    if err := validateNewThing(cfg); err != nil {
        return Result{Valid: false, Detail: "new thing: " + err.Error()}
    }
    
    return Result{Valid: true, Detail: "ok"}
}
```

### Step 2: Add test case

```go
func TestInputs_WhenNewThingInvalid_ReturnsInvalidDetail(t *testing.T) {
    // Setup invalid new thing
    // Call validate.Inputs(cfg)
    // Assert: Result.Valid == false
}
```

## 10. Implementation Files Reference

| File | Responsibility |
|------|----------------|
| `pkg/validate/service.go` | Main validation service |
| `pkg/validate/service_test.go` | Unit tests |
| `pkg/input/cidr.go` | Row-level validation |
| `pkg/input/ports.go` | Port file validation |
| `pkg/cli/output.go` | Validation output formatting |

## 11. Integration Points

- **CLI**: `validate.Inputs(cfg)` called from `handleValidateCommand()`
- **Config**: File paths from `config.ValidateValues` through `Configuration`
- **Input**: Uses `input.LoadCIDRsWithColumns()` and `input.LoadPorts()`
- **Output**: Uses `cli.WriteValidation()` for formatting

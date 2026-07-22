# SPEC-01: CLI Layer Specification

## Overview

```
cmd/port-scan/
├── main.go                 # Entry point, command routing
└── command_handlers.go     # Command implementations
```

## 1. Entry Point (main.go)

### Function Signature

```go
func main() {
    os.Exit(runMain(os.Args, os.Stdout, os.Stderr))
}

func runMain(args []string, stdout, stderr io.Writer) int {
    // Pure function for testable exit codes
}
```

### Command Routing

The CLI uses simple switch-based dispatch. `port-scan` is a three-step pipeline
(`preping` -> `generate-buckets` -> `scan`) plus a `validate` helper:

| Input | Command | Handler |
|-------|---------|---------|
| `args[0] == "validate"` | Input validation only | `handleValidateCommand()` |
| `args[0] == "preping"` | Ping unique IPs, write unreachable CSV | `handlePrepingCommand()` |
| `args[0] == "generate-buckets"` | Build resume bucket snapshot | `handleGenerateBucketsCommand()` |
| `args[0] == "scan"` | Pure scan of a bucket snapshot (requires `-resume`) | `handleScanCommand()` |
| `args[0] == "--help"` or `-h` | Help | `handleHelpCommand()` |
| otherwise | Error | Exit 2 |

Each pipeline subcommand parses its own flag surface via
`config.ParseFor(command, args)` — only the flags that subcommand owns are
registered, so a foreign flag (e.g. a ping flag on `scan`) is an unknown-flag
error. `validate` still uses `config.Parse(args)`.

### Exit Code Conventions

| Code | Meaning | Trigger |
|------|---------|---------|
| **0** | Success | Valid input, scan completed |
| **1** | Validation/Runtime error | Invalid input (`validate`), scan runtime error (`scan`) |
| **2** | CLI parsing error | Invalid flags, missing required args, flag validation fail |
| **130** | Scan canceled | SIGINT (Ctrl+C) - detected via `context.Canceled` |

## 2. Command Handlers (command_handlers.go)

### handleHelpCommand()

```go
func handleHelpCommand(stdout io.Writer) int
```

- Displays usage text to stdout
- Returns exit code 0

### handleValidateCommand()

```go
func handleValidateCommand(args []string, stdout, stderr io.Writer) int
```

**Flow:**
1. Parse flags via `config.Parse(args)`
2. Call `validate.Inputs(cfg)` 
3. Output result via `cli.WriteValidation(stdout, cfg.Format, result.Valid, result.Detail)`
4. Return 0 if valid, 1 if invalid

### handlePrepingCommand()

```go
func handlePrepingCommand(args []string, stdout, stderr io.Writer) int
```

**Flow:**
1. Parse via `config.ParseFor("preping", args)` (rejects scan/bucket flags)
2. Wrap context with `state.WithSIGINTCancel(ctx)`
3. Call `scanapp.RunPreping(ctx, cfg, stdout, stderr, scanapp.RunOptions{})`,
   which writes `unreachable_results-<ts>.csv` and prints its resolved path to
   stdout for chaining
4. Map errors to exit codes (parse → 2, `context.Canceled` → 130, else → 1)

### handleGenerateBucketsCommand()

```go
func handleGenerateBucketsCommand(args []string, stdout, stderr io.Writer) int
```

**Flow:**
1. Parse via `config.ParseFor("generate-buckets", args)` (requires `-buckets-out`)
2. Wrap context with `state.WithSIGINTCancel(ctx)`
3. Call `scanapp.GenerateBuckets(ctx, cfg, stderr, scanapp.GenerateBucketsOptions{})`;
   performs no network I/O
4. Map errors to exit codes (parse / missing `-buckets-out` → 2, cancel → 130,
   else → 1)

### handleScanCommand()

```go
func handleScanCommand(args []string, stdout, stderr io.Writer) int
```

**Flow:**
1. Delegate to `runScan(args, stdout, stderr)`

### runScan() - Internal Implementation

```go
func runScan(args []string, stdout, stderr io.Writer) int
```

**Flow:**
1. Parse config via `config.ParseFor("scan", args)` — **`-resume` is required**
   and no ping flags are registered
2. Wrap context with SIGINT handling via `state.WithSIGINTCancel(ctx)`
3. Build `RunOptions` (pressure fetcher wiring lives only here)
4. Call `scanapp.Run(ctx, cfg, stdout, stderr, opts)`
5. Map errors to exit codes

## 3. Package Dependencies

```
cmd/port-scan (CLI layer)
    │
    ├── pkg/config       # Parse CLI args → Config struct
    ├── pkg/validate    # validate.Inputs(cfg) for validation command
    ├── pkg/cli          # cli.WriteValidation() for output formatting
    ├── pkg/scanapp     # scanapp.Run() for scan command
    └── pkg/state       # state.WithSIGINTCancel() for SIGINT handling
```

## 4. Key Design Patterns

### Thin CLI Pattern

- `cmd/port-scan` ONLY handles:
  - Argument parsing routing
  - Exit code mapping
  - User I/O delegation
- ALL reusable logic lives in `pkg/`

### Testable Entry Point

- `runMain()` is pure (no `os.Exit` internally)
- Enables unit testing of exit codes without process termination

### Dependency Injection

- Handlers accept `io.Writer` for stdout/stderr
- Enables capture in tests

## 5. Adding New Commands

### Step 1: Add routing case in main.go

```go
switch args[0] {
case "validate":
    return handleValidateCommand(args[1:], stdout, stderr)
case "scan":
    return handleScanCommand(args[1:], stdout, stderr)
case "newcommand":           // ← ADD HERE
    return handleNewCommand(args[1:], stdout, stderr)
}
```

### Step 2: Implement handler in command_handlers.go

```go
func handleNewCommand(args []string, stdout, stderr io.Writer) int {
    // Implementation
    if err != nil {
        return 1  // or 2 for CLI error
    }
    return 0
}
```

## 6. Error Mapping

Errors from scanapp.Run() are mapped to exit codes:

| Error Type | Exit Code |
|------------|-----------|
| `context.Canceled` | 130 |
| `config.ErrInvalidFlag` | 2 |
| Any other error | 1 |

## 7. Implementation Files Reference

| File | Responsibility |
|------|----------------|
| `cmd/port-scan/main.go` | Entry point, command dispatch |
| `cmd/port-scan/command_handlers.go` | Command implementations, exit code mapping |
| `cmd/port-scan/main_test.go` | CLI unit tests |
| `cmd/port-scan/main_scan_test.go` | Scan command tests |
| `cmd/port-scan/test_helpers_test.go` | Test utilities |

## 8. Integration Points

- **Config**: `config.Parse(args)` → `Config`
- **Validation**: `validate.Inputs(cfg)` → `Result{Valid, Detail}`
- **Scan**: `scanapp.Run(ctx, cfg, stdout, stderr, opts)` → error
- **Signal**: `state.WithSIGINTCancel(ctx)` → context
- **Output**: `cli.WriteValidation(out, format, valid, detail)` → void

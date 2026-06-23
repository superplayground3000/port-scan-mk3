---
name: codebase-docs
description: |
  Use when the user asks to document, write docs, or generate documentation for the port-scan-mk3 codebase.
  Also use when updating existing documentation, adding README sections, writing Go doc comments,
  or creating architecture diagrams. This skill is specific to the port-scan-mk3 project at
  /media/hp/secondary/projects/port-scan-mk3. Triggers on: "write documentation", "add docs",
  "document this", "update README", "add comments to code", "generate docs", "document this function",
  "write a README for", "add usage examples"
---

# Codebase Documentation Skill for port-scan-mk3

This skill ensures all documentation for the port-scan-mk3 project remains accurate, consistent,
and synchronized with the codebase. It provides a structured approach to documenting Go code,
README files, and architecture diagrams.

## Workflow

### Step 1: Explore and Understand

Before writing any documentation:

1. **Identify the scope**: Determine if documenting the entire project, a specific tool (cmd/), package (pkg/), or function.
2. **Read the existing codebase**: Use Glob and Read to understand the actual implementation.
3. **Check existing docs**: Look in `docs/` directory for any existing related documentation.
4. **Identify all cmd/ tools**: The project has multiple tools:
   - `cmd/port-scan`: Main TCP port scanner CLI
   - `cmd/cidr-compare`: CIDR comparison tool
   - `cmd/csv-transform`: CSV transformation tool

### Step 2: Document Structure

All documentation MUST follow this structure:

#### For README.md (Project-Level)

```
# [Project Name]

## Overview
Brief description of what the project does and why it exists.

## Architecture
High-level architecture diagram and explanation of main components.

## Tools
Separate sections for each tool in cmd/:
- [Tool Name] (cmd/[tool-name])
  - Purpose and use case
  - Architecture diagram showing function flow
  - Usage examples with real commands
  - Exit codes and error handling

## Features
List of main features with explanations.

## Quick Start
Installation and basic usage commands.

## Input/Output Contracts
File formats, flags, and data contracts.

## Testing
How to run tests and verification commands.

---
**Revised**: YYYY-MM-DD | **Author**: [Name]
```

#### For Package-Level Documentation (pkg/)

Follow [Go Doc Comments](https://go.dev/doc/comment) conventions:

```go
// Package pkgname provides [what the package does].
//
// The package implements [main concept] by [how it works].
// It is used by [consumer packages] and depends on [dependencies].
//
// # Function Flow
//
//	Start → Step1 → Step2 → Step3 → End
//
// # Example
//
//	result := PackageFunction(input)
//	if result.Err != nil {
//	    // handle error
//	}
package pkgname
```

#### For Function Documentation

Every exported function MUST have a Go doc comment that includes:

1. **What** the function does (one sentence)
2. **Inputs** - parameters with their types and meaning
3. **Outputs** - return values and their meanings
4. **Behavior** - side effects, error conditions, thread safety
5. **Example** - real usage snippet if complex

```go
// FunctionName performs [what] on [input] and returns [output].
//
// It [describes behavior including error handling].
//
// # Parameters
//
//	conf: Configuration object containing [settings]
//	ctx:  Context for cancellation and timeouts
//
// # Returns
//
//	Result object on success, or error if [failure condition].
//
// # Example
//
//	result, err := FunctionName(ctx, config)
//	if err != nil {
//	    log.Fatalf("operation failed: %v", err)
//	}
func FunctionName(ctx context.Context, conf *Config) (*Result, error)
```

### Step 3: Architecture Diagrams

For function flow diagrams, use ASCII art in markdown:

```markdown
## Function Flow

```
┌─────────────┐
│   Input     │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  Validate   │───── invalid ───▶ Return Error
└──────┬──────┘
       │ valid
       ▼
┌─────────────┐
│   Process   │───── error ────▶ Return Error
└──────┬──────┘
       │ ok
       ▼
┌─────────────┐
│   Output    │
└─────────────┘
```
```

For component architecture, use Mermaid if supported or ASCII diagrams.

### Step 4: Usage Examples

Every tool MUST have real, executable command examples:

```markdown
## Usage Examples

### Example 1: Basic Scan

Scan a network for open ports:

```bash
go run ./cmd/port-scan scan \
  -cidr-file e2e/inputs/cidr_normal.csv \
  -port-file e2e/inputs/ports.csv \
  -output ./results
```

Output:
```
2026-04-13T10:00:00Z Starting scan
2026-04-13T10:00:01Z Found open port 443 on 192.168.1.1
...
```

### Example 2: Validate Input Only

Validate without scanning:

```bash
go run ./cmd/port-scan validate \
  -cidr-file e2e/inputs/cidr_normal.csv \
  -port-file e2e/inputs/ports.csv \
  -format human
```
```

### Step 5: Drift Prevention

After writing documentation, ALWAYS verify against the codebase:

1. **Check flag names**: Ensure all flags in docs match actual flag definitions in code
2. **Check function names**: Verify function signatures match documentation
3. **Check output formats**: Run commands and verify actual output matches documented output
4. **Check paths**: Ensure all file paths in docs exist and are accurate

Verification commands:
```bash
# Verify flags match
go run ./cmd/port-scan scan -help 2>&1 | grep -E "^-"

# Verify packages exist
ls pkg/*/

# Verify tests pass
go test ./...
```

### Step 6: Document Metadata

Every document MUST include revision tracking:

```markdown
---
**Revised**: YYYY-MM-DD
**Author**: [GitHub username or name]
**Changes**: [Brief description of what changed]
---
```

For Go files, track changes via git blame rather than inline metadata.

## Tools Per Command

### cmd/port-scan

Main TCP port scanner. Document:
- CLI flags and configuration
- Input CSV format (CIDR and Rich modes)
- Scan pipeline architecture
- Output file formats
- Resume behavior
- Pressure control mechanism
- Dashboard and logging

### cmd/cidr-compare

CIDR comparison tool. Document:
- Purpose: find overlapping CIDR ranges between deny and open lists
- Interval tree algorithm explanation
- Input/output CSV formats
- Usage examples

### cmd/csv-transform

CSV transformation tool. Document:
- Purpose: transform spreadsheet data to port-scan input format
- Column mapping and filtering
- Host resolution and port expansion
- Usage examples

## Quality Checklist

Before completing documentation:

- [ ] Go doc comments follow https://go.dev/doc/comment conventions
- [ ] README.md has architecture diagram with function flow
- [ ] All tools (cmd/*) have separate documentation sections
- [ ] Real, executable command examples are provided
- [ ] All documented flags, paths, and outputs match the codebase
- [ ] Document includes revision date and author
- [ ] `go test ./...` passes after any changes
- [ ] No placeholder text or TODO comments in final docs

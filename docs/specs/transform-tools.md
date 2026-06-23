# Transform Tools Specification

## Overview

`xlsx-transform` converts an xlsx workbook containing port scan results into a Rich CSV input file consumable by the `port-scan` pipeline.

**Relationship to port-scan pipeline:** Pre-scan input preparation. This tool is a standalone binary that feeds into the `validate` and `scan` commands — it does not replace or modify any existing `cmd/port-scan` functionality.

**Boundaries:** This tool is isolated. It has no dependency on `pkg/scanapp`, `pkg/scanner`, or any other scan pipeline package.

---

## 1. Integration Architecture

```
cmd/xlsx-transform/
├── main.go              # Entry point, CLI flag parsing
├── transform.go         # Core transformation logic (library-isolated)
└── main_test.go         # Unit tests

pkg/xlsx/
└── reader.go            # xlsx parsing wrapper (github.com/xlsxio)
```

**Package responsibilities:**

| Package | Responsibility |
|---------|----------------|
| `cmd/xlsx-transform` | CLI composition, flag handling, file I/O |
| `pkg/xlsx` | xlsx worksheet reading, cell access |
| `pkg/input` | (shared) Rich CSV field constants via `pkg/input/rich_types.go` |

**Dependency graph:**

```
cmd/xlsx-transform
    ├── pkg/xlsx         # xlsx reading
    ├── pkg/input        # Rich field constants (read-only import)
    └── stdlib: net, os, encoding/csv, flag, fmt
```

---

## 2. Input Specification

### 2.1 xlsx Workbook

**Default worksheet name:** `all-runs`

**Column mapping (configurable via flags/env):**

| Config Key | Default | Description |
|------------|---------|-------------|
| `sheet-name` | `all-runs` | Worksheet name to read |
| `host-col` | `Host` | Column containing IP or hostname |
| `port-col` | `Port` | Column containing port(s), "/"-separated for multiple |
| `pass-col` | `Pass the test` | Column indicating pass/fail |

**Filtering rule:** Only rows where `Pass the test` is **not** `"TRUE"` (case-insensitive) are included in output. All other rows are skipped silently.

**Row expansion rule:** If `Port` contains multiple ports separated by `/`, the row is expanded into one output row per port. Example: `80/443` → two rows.

### 2.2 Host Resolution

For each `Host` value:

1. If the value is a valid IPv4 address → use as-is as `dst_ip`
2. If it is a hostname → call `net.LookupIP(host)`
   - If resolution succeeds → use the first returned IPv4 as `dst_ip`
   - If resolution fails → use the original hostname string as `dst_ip`; let downstream validation report the issue

**No DNS failures are fatal** during transform. The tool continues and emits all resolvable rows.

---

## 3. Output Specification

### 3.1 Rich CSV Format

The tool outputs a CSV in Rich mode with these columns:

```
src_ip,src_network_segment,dst_ip,dst_network_segment,service_label,protocol,port,decision,matched_policy_id,reason
```

### 3.2 Field Derivation

| Output Column | Value |
|---------------|-------|
| `src_ip` | Placeholder: `10.0.0.1` |
| `src_network_segment` | Placeholder: `10.0.0.0/24` |
| `dst_ip` | Resolved IP or hostname (see Section 2.2) |
| `dst_network_segment` | Placeholder: `10.0.0.0/24` |
| `service_label` | Placeholder: `unknown` |
| `protocol` | Fixed: `tcp` |
| `port` | Expanded port from input row |
| `decision` | Fixed: `accept` |
| `matched_policy_id` | Placeholder: `transformed` |
| `reason` | Fixed: `MATCH_POLICY_ACCEPT` |

### 3.3 Output File

- Format: CSV with header row
- Path: Specified via `--output` flag (required)
- If file exists: **overwrite** without warning

---

## 4. CLI Interface

### 4.1 Binary Name

`xlsx-transform` (not `port-scan transform`)

### 4.2 Flags

| Flag | Env Var | Default | Required | Description |
|------|---------|---------|----------|-------------|
| `--input` | `TRANSFORM_INPUT` | - | Yes | Path to input xlsx file |
| `--output` | `TRANSFORM_OUTPUT` | - | Yes | Path to output CSV file |
| `--sheet` | `TRANSFORM_SHEET_NAME` | `all-runs` | No | Worksheet name |
| `--host-col` | `TRANSFORM_HOST_COL` | `Host` | No | Host column name |
| `--port-col` | `TRANSFORM_PORT_COL` | `Port` | No | Port column name |
| `--pass-col` | `TRANSFORM_PASS_COL` | `Pass the test` | No | Pass/fail column name |
| `--help` | - | - | - | Show help |

### 4.3 Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Transform succeeded, output file written |
| 1 | Runtime error (file not found, xlsx parse error, sheet not found) |
| 2 | CLI flag validation error (missing required flags, invalid values) |

---

## 5. Error Handling

| Error Condition | Behavior |
|-----------------|----------|
| Input file not found | Exit 1, message: `input file not found: <path>` |
| Input file is not valid xlsx | Exit 1, message: `failed to open xlsx: <detail>` |
| Sheet name not found | Exit 1, message: `sheet not found: <name>` |
| Host column missing | Exit 1, message: `required column not found: <col>` |
| Port column missing | Exit 1, message: `required column not found: <col>` |
| Pass column missing | Exit 1, message: `required column not found: <col>` |
| Port value empty | Skip row silently |
| Host value empty | Skip row silently |
| Port value invalid (non-numeric) | Skip row silently, log to stderr |
| DNS lookup timeout | Use hostname as-is, continue |

---

## 6. Data Flow

```
xlsx file
    │
    ▼
pkg/xlsx.Reader
    │ reads sheet, returns rows []
    ▼
Transform(filters: Pass the test != TRUE, expands ports, resolves host)
    │
    ▼
Rich CSV rows (10 fields each, defaults filled)
    │
    ▼
Write CSV via encoding/csv → output file
```

---

## 7. Testing Strategy

### 7.1 Unit Tests

| Test | Coverage |
|------|----------|
| Port string splitting: `"80"` → `[80]` | Single port |
| Port string splitting: `"80/443"` → `[80, 443]` | Multiple ports |
| Port string splitting: `""` → skip | Empty port |
| Host: valid IPv4 passed through | `"192.168.1.1"` → `"192.168.1.1"` |
| Host: hostname resolved | `"example.com"` → `net.LookupIP` result or hostname |
| Pass filter: `"TRUE"` → skip | Case variations |
| Pass filter: `"FALSE"` → include | |
| Row expansion: 2 ports → 2 rows | |
| Row expansion: 1 port → 1 row | |

### 7.2 Test Fixtures

- Use a minimal xlsx generated in-memory or a small test file committed under `cmd/xlsx-transform/testdata/`

---

## 8. Implementation Notes

- **Library isolation**: `pkg/xlsx` must be a standalone package with no dependency on `pkg/input`, `pkg/scanapp`, or any pipeline package.
- **No global state**: All config passed via `TransformConfig` struct.
- **Go doc required** on all public types and functions.
- Third-party dependency for xlsx reading: `github.com/xlsxio` (or equivalent, minimal dependency policy applies per constitution).

---

## 9. Files to Create

```
cmd/xlsx-transform/
├── main.go
├── main_test.go
└── transform.go

pkg/xlsx/
├── reader.go
└── reader_test.go
```

---

## 10. Constitution Alignment Check

| Principle | Alignment |
|-----------|-----------|
| Library-First Design | Transform logic in `pkg/xlsx`, CLI-only wiring in `cmd/xlsx-transform` |
| CLI Contract-First | Flags stable; exit codes documented |
| Test-First Delivery | Unit tests for all transformation rules |
| Isolated End-to-End | Separate binary, no shared state with port-scan |
| SOLID Boundaries | Narrow pkg/xlsx, no cyclic deps |
| Dependency Minimalism | Single xlsx library dependency |

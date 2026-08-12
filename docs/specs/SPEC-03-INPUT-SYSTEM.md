# SPEC-03: Input System Specification

## Overview

```
pkg/input/
├── types.go              # CIDRRecord, PortSpec data structures
├── cidr.go               # CIDR CSV parsing with custom column mapping
├── rich_parser.go       # Rich CSV parsing
├── rich_types.go        # Rich input field constants
├── header_match.go      # Rich header detection
├── validate.go          # Cross-row validation (overlap, containment, duplicates)
├── validation_errors.go # Validation error codes
└── ports.go             # Port file parsing
```

## 1. Input Formats

### 1.1 CIDR Mode (Default)

**Required columns:**
- `ip` - IP selector (single IP like `10.0.0.1` or CIDR like `10.0.0.8/30`)
- `ip_cidr` - CIDR boundary (e.g., `10.0.0.0/24`)

**Optional columns:**
- `fab_name` - Fabric name
- `cidr_name` - CIDR name
- `port` - Specific port (overrides port-file)

**Example:**
```csv
ip,ip_cidr,fab_name,cidr_name
10.0.0.1,10.0.0.0/24,fab1,backend
10.0.0.8/30,10.0.0.0/24,fab1,backend
```

### 1.2 Rich CSV Mode (Auto-detected)

**Required columns (ALL must be present for auto-detection):**
- `src_ip` - Source IP
- `src_network_segment` - Source network (CIDR)
- `dst_ip` - Destination IP
- `dst_network_segment` - Destination network (CIDR)
- `service_label` - Service identifier
- `protocol` - Protocol (must be "tcp")
- `port` - Port number (1-65535)
- `decision` - "accept" or "deny"
- `matched_policy_id` - Policy identifier
- `reason` - Decision reason

**Detection:** Auto-detected when ALL 10 columns exist in header.

**Example:**
```csv
src_ip,src_network_segment,dst_ip,dst_network_segment,service_label,protocol,port,decision,matched_policy_id,reason
10.0.0.1,10.0.0.0/24,192.168.1.1,192.168.1.0/24,web,tcp,443,accept,POLICY-001,allowed
```

### 1.3 Port File Format

One port per line in `<port>/tcp` format.

**Example:**
```
443/tcp
80/tcp
8080/tcp
```

## 2. Core Data Structures

### 2.1 CIDRRecord

```go
type CIDRRecord struct {
    IP              string     // IP selector
    IPCidr          *net.IPNet // CIDR boundary (parsed)
    Selector        *net.IPNet // Expanded IP selector (may be /32 for single IP)
    FabName         string     // Optional: fabric name
    CidrName        string     // Optional: CIDR name
    Port            int        // Optional: port override (0 = use global ports)
    RowNumber       int        // Source row number (for error reporting)
    IsRich          bool       // True if parsed from rich CSV
    RichSrcIP       string     // Rich: source IP
    RichSrcSegment  string     // Rich: source segment
    RichDstIP       string     // Rich: destination IP
    RichDstSegment  string     // Rich: destination segment
    RichLabel       string     // Rich: service label
    RichProtocol    string     // Rich: protocol
    RichDecision    string     // Rich: decision (accept/deny)
    RichPolicyID    string     // Rich: policy ID
    RichReason      string     // Rich: reason
}
```

### 2.2 PortSpec

```go
type PortSpec struct {
    Port     int    // Port number (1-65535)
    Protocol string // Protocol (currently only "tcp" supported)
    Line     int    // Source line number
}
```

## 3. Key Functions

### 3.1 CIDR CSV Loading

```go
func LoadCIDRsWithColumns(r io.Reader, ipCol, ipCidrCol string) ([]CIDRRecord, error)
```

**Parameters:**
- `r` - CSV reader
- `ipCol` - Column name for IP selector (default: "ip")
- `ipCidrCol` - Column name for CIDR boundary (default: "ip_cidr")

**Process:**
1. Read CSV header
2. Auto-detect rich mode via `detectRichHeaderIndices()`
3. If rich: delegate to `ParseRichRows()`
4. If CIDR: parse rows with custom column mapping
5. Call `ValidateIPRows()` for cross-row validation

### 3.2 Rich CSV Detection

```go
func detectRichHeaderIndices(header []string) (map[string]int, bool)
```

- Canonicalizes header names (lowercase, trimmed)
- Checks if ALL 10 required rich fields exist
- Returns column index map if rich, `false` otherwise

**Required Rich Fields:**
```go
var requiredRichFields = []string{
    "src_ip", "src_network_segment", "dst_ip", "dst_network_segment",
    "service_label", "protocol", "port", "decision",
    "matched_policy_id", "reason",
}
```

### 3.3 Rich CSV Parsing

```go
func ParseRichRows(rows []string, idx map[string]int) ([]CIDRRecord, RichParseSummary, error)
```

**Returns:**
- `[]CIDRRecord` - All records (valid and invalid with `IsValid=false`)
- `RichParseSummary` - Counts and failure reasons
- `error` - Only if NO valid rows

### 3.4 Port File Loading

```go
func LoadPorts(r io.Reader) ([]PortSpec, error)
```

**Format:** One per line in `<port>/tcp`

**Validation:**
- Port must be 1-65535
- Protocol must be "tcp"
- Returns error on first invalid port

## 4. Validation Rules

### 4.1 Per-Row Validation (During Parsing)

| Rule | CIDR Mode | Rich Mode |
|------|-----------|-----------|
| IP must be valid IPv4 | ✅ | ✅ |
| CIDR must be valid IPv4 | ✅ | ✅ |
| IP must be within CIDR | ✅ | ✅ |
| Protocol must be "tcp" | N/A | ✅ |
| Decision must be "accept"/"deny" | N/A | ✅ |
| Port must be 1-65535 | ✅ | ✅ |

### 4.2 Cross-Row Validation (validate.go)

```go
func ValidateIPRows(rows []CIDRRecord) error
```

| Rule | Description | Action on Violation |
|------|-------------|---------------------|
| **Nil Check** | All records must have parsed Net and Selector | Return error |
| **Duplicate** | Reject duplicate `(ip_cidr, src, dst, port)` | Return error |
| **Containment** | Each selector must be inside its ip_cidr boundary | Return error |

### 4.3 Validation Error Codes

```go
const (
    ValidationOK               = ""
    ValidationMissingField     = "missing_field"
    ValidationInvalidSrcIP     = "invalid_src_ip"
    ValidationInvalidSrcCIDR   = "invalid_src_cidr"
    ValidationInvalidDstIP     = "invalid_dst_ip"
    ValidationInvalidDstCIDR   = "invalid_dst_cidr"
    ValidationIPNotInCIDR      = "ip_not_in_cidr"
    ValidationInvalidProtocol  = "invalid_protocol"
    ValidationInvalidDecision  = "invalid_decision"
    ValidationInvalidPort      = "invalid_port"
)
```

## 5. Custom Column Mapping

### CLI Flags

```bash
-cidr-ip-col "SourceIP"         # Default: "ip"
-cidr-ip-cidr-col "Network"     # Default: "ip_cidr"
```

### Usage

```go
records, err := input.LoadCIDRsWithColumns(file, "SourceIP", "Network")
```

CLI workflows get these column names from their workflow-specific
configuration values.

## 6. Resource Limits

`CIDRLimits` controls complete input bytes and data-record counts.
The defaults are `1000000000` bytes and `10000000` records.

`PortLimits` controls complete input bytes and nonblank-record counts.
The defaults are `1000000` bytes and `65535` records.

The file adapters reject known oversized files before parsing.
The reader interfaces also stop after `limit + 1` bytes.
A zero field disables only that limit.

## 7. Adding New Input Formats

### Step 1: Add detection logic in cidr.go

```go
func detectFormat(header []string) InputFormat {
    if detectRichHeaderIndices(header) {
        return FormatRich
    }
    if detectBasicHeaderIndices(header) {
        return FormatBasic
    }
    return FormatUnknown
}
```

### Step 2: Add parser for new format

```go
func ParseNewFormat(rows []string, idx map[string]int) ([]CIDRRecord, error) {
    // Implementation
}
```

### Step 3: Update LoadCIDRsWithColumns

```go
switch detectFormat(header) {
case FormatRich:
    return ParseRichRows(rows, idx)
case FormatNew:
    return ParseNewFormat(rows, idx)
default:
    return nil, errors.New("unsupported format")
}
```

## 8. Implementation Files Reference

| File | Responsibility |
|------|----------------|
| `pkg/input/types.go` | CIDRRecord, PortSpec data structures |
| `pkg/input/cidr.go` | CIDR CSV parsing, auto-detection |
| `pkg/input/rich_parser.go` | Rich CSV parsing |
| `pkg/input/rich_types.go` | Rich field constants, RichParseSummary |
| `pkg/input/header_match.go` | Rich header detection |
| `pkg/input/validate.go` | Cross-row validation |
| `pkg/input/validation_errors.go` | Error codes |
| `pkg/input/ports.go` | Port file parsing |

## 9. Integration Points

- **Config**: Column names from `PrePingValues`, `GenerateBucketsValues`,
  `ScanValues`, or `ValidateValues`
- **Validation**: `validate.Inputs(cfg)` calls `LoadCIDRsWithColumns()`
- **Scan**: `input_loader.go` calls `LoadCIDRsWithColumns()` and `LoadPorts()`
- **Writer**: Rich fields flow through to output CSV

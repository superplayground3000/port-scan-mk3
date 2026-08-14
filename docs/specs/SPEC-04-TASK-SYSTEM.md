# SPEC-04: Task System Specification

## Overview

```
pkg/task/
├── types.go            # Chunk and Task data structures
├── selector_expand.go  # IP selector expansion logic
├── execution_key.go    # Execution key builder
├── index.go           # Index-to-target conversion
└── ipv4.go            # IPv4 utilities
```

## 1. Core Data Structures

### 1.1 Chunk

```go
type Chunk struct {
    CIDR     string     // CIDR boundary, for example "10.0.0.0/24"
    CIDRName string     // Human-readable CIDR name
    Ports    []string   // Port specifications in <port>/tcp format
    
    // Progress tracking
    NextIndex   int     // Next task index to dispatch (0-based)
    ScannedCount int   // Number of committed results credited to progress
    TotalCount  int     // Number of ordered probe tasks in this chunk
    Status      string  // "pending", "scanning", "completed"
}
```

The snapshot does not store an expanded target list. The runtime rebuilds incomplete chunk targets from the current input.

Port overrides, authorization, exclusions, and deduplication determine `TotalCount`. One universal multiplication formula does not apply.

### 1.2 Task

```go
type Task struct {
    ChunkCIDR string    // Parent CIDR
    IP        string    // Target IP
    Port      int       // Target port
    Index     int       // Flat index within chunk
}
```

## 2. Selector Expansion

### ExpandIPSelectors

```go
func ExpandIPSelectors(selectors []string) ([]string, error)
```

**Input:** Raw selectors (single IPs or CIDR blocks)
- Single IP: `10.0.0.1`
- CIDR block: `10.0.0.8/30`

**Process:**
1. Parse each selector - distinguish IP vs CIDR via `net.ParseIP()` vs `net.ParseCIDR()`
2. For IPs: convert to uint32 via BigEndian binary
3. For CIDRs: compute start/end range via `ipv4Range()`, enumerate all hosts
4. Deduplicate via `map[uint32]struct{}`
5. Sort ascending, convert back to strings

**Output:** Sorted, deduplicated list of IPv4 strings

**Validation:** Rejects non-IPv4 (IPv6) with error

**Example:**
```go
selectors := []string{"10.0.0.1", "10.0.0.8/30"}
expanded, err := ExpandIPSelectors(selectors)
// Returns: ["10.0.0.1", "10.0.0.8", "10.0.0.9", "10.0.0.10", "10.0.0.11"]
```

## 3. Execution Key

### BuildExecutionKey

```go
func BuildExecutionKey(dstIP string, port int, protocol string) (string, error)
```

**Format:** `dst_ip:port/protocol`

**Example:**
```go
key, _ := BuildExecutionKey("10.0.0.1", 443, "tcp")
// Returns: "10.0.0.1:443/tcp"
```

**Validation:**
| Rule | Error |
|------|-------|
| IPv4 address required | "invalid IP address" |
| Port 1-65535 | "invalid port" |
| Protocol must be "tcp" | "protocol must be tcp" |

## 4. Index-to-Target Mapping

### IndexToTarget

```go
func IndexToTarget(idx int, ips []string, ports []int) (string, int)
```

**Formula:**
```go
ipIdx := idx / len(ports)
portIdx := idx % len(ports)
return ips[ipIdx], ports[portIdx]
```

**Example:**
```go
ips := []string{"10.0.0.1", "10.0.0.2"}
ports := []int{80, 443}

IndexToTarget(0, ips, ports) // "10.0.0.1", 80
IndexToTarget(1, ips, ports) // "10.0.0.1", 443
IndexToTarget(2, ips, ports) // "10.0.0.2", 80
IndexToTarget(3, ips, ports) // "10.0.0.2", 443
```

### IndexToIPv4Target

```go
func IndexToIPv4Target(ipNet *net.IPNet, ports []int, idx int) (string, int, error)
```

Direct index-to-target without pre-expanded IP array.

### CountIPv4Hosts

```go
func CountIPv4Hosts(ipNet *net.IPNet) (int, error)
```

Counts hosts in CIDR (excludes network and broadcast for /24 and smaller).

## 5. Task Lifecycle

```
Input Load (pkg/input)
       ↓
Chunk Build (pkg/scanapp/chunk_lifecycle.go)
       ↓
Runtime Rebuild (prepareRuntimePlan)
       ↓
Dispatch (pkg/scanapp/task_dispatcher.go)
       ↓
Execute (pkg/scanapp/executor.go)
       ↓
Resume (persistResumeSnapshot)
```

### Status Flow

```
pending → scanning → completed
  ↑          ↓
  └──────────┘ (if NextIndex >= TotalCount)
```

## 6. Runtime Types (pkg/scanapp/runtime_types.go)

These types extend the core task types for orchestration:

### targetMeta

```go
type targetMeta struct {
    fabName           string  // Fabric name
    cidrName          string  // CIDR name
    serviceLabel      string  // Rich: service label
    decision          string  // Rich: accept/deny
    policyID          string  // Rich: policy ID
    reason            string  // Rich: decision reason (PRECHECK_ALLOW_ALL, MATCH_POLICY_ACCEPT, etc.)
    executionKey      string  // Rich: dst_ip:port/protocol
    srcIP             string  // Rich: source IP
    srcNetworkSegment string  // Rich: source network segment
}
```

### scanTarget

```go
type scanTarget struct {
    ip     string
    ipCidr string
    port   int
    meta   targetMeta
}
```

### scanTask

```go
type scanTask struct {
    chunkIdx int
    ipCidr   string
    ip       string
    port     int
    meta     targetMeta
}
```

### chunkRuntime

```go
type chunkRuntime struct {
    ipCidr  string
    ports   []int
    targets []scanTarget
    state   *task.Chunk
    tracker *chunkStateTracker
    bkt     *ratelimit.LeakyBucket
}
```

## 7. Reason-Aware Rich Target Expansion

The group builder implements intelligent target expansion based on the `reason` field in rich CSV records:

### Reason Constants

```go
const (
    reasonPrecheckAllowAll  = "PRECHECK_ALLOW_ALL"
    reasonMatchPolicyAccept = "MATCH_POLICY_ACCEPT"
)
```

### Expansion Logic

| Reason | Target IPs | Description |
|--------|------------|-------------|
| `PRECHECK_ALLOW_ALL` | All IPs in `dst_network_segment` | Expand entire CIDR |
| `MATCH_POLICY_ACCEPT` | Single `dst_ip` | Only the specific destination IP |
| (other/unknown) | Single `dst_ip` | Default to specific destination IP |

### Example

```go
// PRECHECK_ALLOW_ALL: expand entire CIDR
record := input.CIDRRecord{
    DstNetworkSegment: "10.0.0.0/24",
    Port:              443,
    Reason:           "PRECHECK_ALLOW_ALL",
}
// Results in 254 target IPs (10.0.0.1 - 10.0.0.254)

// MATCH_POLICY_ACCEPT: single IP
record := input.CIDRRecord{
    DstIP:   "10.0.0.1",
    Port:    443,
    Reason:  "MATCH_POLICY_ACCEPT",
}
// Results in 1 target IP (10.0.0.1)
```

## 8. Execution Key Ownership (CIDR-Scoped Rate Control)

Rich mode implements global deduplication with CIDR-scoped rate control:

### Concept

- **Global deduplication**: Same `(dst_ip, port)` across multiple CIDR records → scanned once
- **CIDR-scoped rate control**: Each CIDR has its own rate limit bucket
- **Owner tracking**: The CIDR that first "claims" an execution key becomes its owner

### Implementation

```go
// Build owner map during group construction
ownerByExecutionKey := make(map[string]string)
for _, rec := range cidrRecords {
    // ... process records
    ownerByExecutionKey[executionKey] = cidr  // First owner wins
}

// Reassign non-owner targets to their owner CIDR
for cidr, group := range groups {
    for _, target := range group.targets {
        ownerCIDR := ownerByExecutionKey[target.meta.executionKey]
        if ownerCIDR != cidr {
            // Move target to owner CIDR's group
            groups[ownerCIDR].targets = append(groups[ownerCIDR].targets, target)
        }
    }
}
```

### Benefit

- Prevents duplicate scans of the same target
- Maintains per-CIDR rate limiting
- Ensures consistent ordering within each CIDR

## 9. Adding New Task Features

### Adding New Task Metadata

1. Add field to `scanTarget` in `runtime_types.go`
2. Update `indexToRuntimeTarget()` in `task_dispatcher.go`
3. Pass through to output in `result_aggregator.go`

### Adding New Selector Syntax

1. Modify `ExpandIPSelectors()` in `selector_expand.go`
2. Add parser for new syntax
3. Update validation rules
4. Add tests

### Adding New Grouping Strategy

1. Create new strategy interface in `group_builder.go`
2. Implement grouping logic
3. Register in `groupBuildStrategy` interface

## 10. Implementation Files Reference

| File | Responsibility |
|------|----------------|
| `pkg/task/types.go` | Chunk, Task data structures |
| `pkg/task/selector_expand.go` | IP selector expansion |
| `pkg/task/execution_key.go` | Execution key building |
| `pkg/task/index.go` | Index-to-target conversion |
| `pkg/task/ipv4.go` | IPv4 utilities |
| `pkg/scanapp/runtime_types.go` | Runtime extensions (scanTarget, scanTask, chunkRuntime) |
| `pkg/scanapp/group_builder.go` | CIDR grouping |
| `pkg/scanapp/chunk_lifecycle.go` | Chunk lifecycle management |

## 11. Integration Points

- **Input**: `input.CIDRRecord` provides IP/CIDR for expansion
- **Rate Limit**: Each chunk gets its own `LeakyBucket`
- **Dispatcher**: Uses `IndexToTarget()` to resolve flat index to (IP, port)
- **Resume**: `Chunk` state (NextIndex, ScannedCount, Status) persisted
- **Writer**: Execution key included in output CSV

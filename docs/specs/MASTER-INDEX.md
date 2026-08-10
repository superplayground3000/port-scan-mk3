# Port Scan MK3 - Implementation Specification Index

## Document Overview

This index provides a comprehensive map of the port-scan-mk3 implementation specifications. Each specification document defines a specific subsystem with complete detail, enabling:
- Multi-agent collaboration on implementation
- Single-agent multi-session development
- Clear ownership boundaries
- No ambiguity for new developers

---

## Specification Index

| ID | Document | Responsibility | Key Files |
|----|----------|----------------|-----------|
| **SPEC-01** | [CLI Layer](SPEC-01-CLI-LAYER.md) | CLI entry point, command routing, exit codes | `cmd/port-scan/main.go`, `cmd/port-scan/command_handlers.go` |
| **SPEC-02** | [Config System](SPEC-02-CONFIG-SYSTEM.md) | Flag parsing, configuration | `pkg/config/*_config.go`, `pkg/config/pre_ping.go`, `pkg/config/generate_buckets.go` |
| **SPEC-03** | [Input System](SPEC-03-INPUT-SYSTEM.md) | CIDR/Rich CSV parsing | `pkg/input/*.go` |
| **SPEC-04** | [Task System](SPEC-04-TASK-SYSTEM.md) | Task modeling, selector expansion | `pkg/task/*.go` |
| **SPEC-05** | [Scanner System](SPEC-05-SCANNER-SYSTEM.md) | TCP probe primitive | `pkg/scanner/scanner.go` |
| **SPEC-06** | [Scan Orchestration](SPEC-06-SCAN-ORCHESTRATION.md) | Full scan pipeline | `pkg/scanapp/*.go` |
| **SPEC-07** | [Rate Limit System](SPEC-07-RATE-LIMIT-SYSTEM.md) | Leaky bucket rate limiting | `pkg/ratelimit/leaky_bucket.go` |
| **SPEC-08** | [Speed Control](SPEC-08-SPEED-CONTROL.md) | Pressure-based pause | `pkg/speedctrl/*.go`, `pkg/scanapp/pressure*.go` |
| **SPEC-09** | [Writer System](SPEC-09-WRITER-SYSTEM.md) | CSV output | `pkg/writer/*.go` |
| **SPEC-10** | [Validate System](SPEC-10-VALIDATE-SYSTEM.md) | Input validation | `pkg/validate/service.go` |
| **SPEC-11** | [State System](SPEC-11-STATE-SYSTEM.md) | Resume & signal handling | `pkg/state/*.go` |
| **SPEC-12** | [Logx System](SPEC-12-LOGX-SYSTEM.md) | Logging utilities | `pkg/logx/*.go` |
| **SPEC-13** | [Rich Dashboard](SPEC-13-RICH-DASHBOARD.md) | Real-time terminal UI (planned) | `pkg/scanapp/dashboard_*.go` (planned) |

---

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────────────────┐
│                              CLI Layer                                   │
│                         (SPEC-01: cmd/port-scan)                         │
└─────────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                          Config System                                   │
│                         (SPEC-02: pkg/config)                            │
└─────────────────────────────────────────────────────────────────────────┘
                                    │
                    ┌───────────────┴───────────────┐
                    ▼                               ▼
┌─────────────────────────────────┐   ┌───────────────────────────────────┐
│       Validate System           │   │        Input System               │
│    (SPEC-10: pkg/validate)      │   │     (SPEC-03: pkg/input)          │
└─────────────────────────────────┘   └───────────────────────────────────┘
                    │                               │
                    └───────────────┬───────────────┘
                                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                           Task System                                     │
│                        (SPEC-04: pkg/task)                               │
└─────────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                      Scan Orchestration                                   │
│                     (SPEC-06: pkg/scanapp)                               │
│  ┌─────────────┬──────────────┬─────────────┬─────────────┐             │
│  │ Rate Limit  │ Speed Control│  Scanner    │   Writer    │             │
│  │ (SPEC-07)   │  (SPEC-08)   │  (SPEC-05)  │  (SPEC-09)  │             │
│  └─────────────┴──────────────┴─────────────┴─────────────┘             │
│                              │                                           │
│                              ▼                                           │
│  ┌─────────────────────────────────────────────────────┐               │
│  │            Rich Dashboard (SPEC-13) - planned        │               │
│  └─────────────────────────────────────────────────────┘               │
└─────────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                          State System                                     │
│                       (SPEC-11: pkg/state)                               │
└─────────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                          Logx System                                     │
│                        (SPEC-12: pkg/logx)                              │
└─────────────────────────────────────────────────────────────────────────┘
```

---

## Dependency Graph

```
CLI Layer (SPEC-01)
    │
    ├──► Config System (SPEC-02)
    │        │
    │        ├──► Validate System (SPEC-10)
    │        │        │
    │        │        └──► Input System (SPEC-03)
    │        │
    │        └──► Scan Orchestration (SPEC-06)
    │                 │
    │                 ├──► Task System (SPEC-04)
    │                 │        │
    │                 │        └──► Input System (SPEC-03)
    │                 │
    │                 ├──► Scanner System (SPEC-05)
    │                 │
    │                 ├──► Rate Limit System (SPEC-07)
    │                 │
    │                 ├──► Speed Control (SPEC-08)
    │                 │
    │                 ├──► Writer System (SPEC-09)
    │                 │
    │                 └──► State System (SPEC-11)
    │                          │
    │                          └──► Logx System (SPEC-12)
    │
    └──► Logx System (SPEC-12)
```

---

## Package Map

| Package | Spec | Description |
|---------|------|-------------|
| `cmd/port-scan` | SPEC-01 | CLI composition root |
| `pkg/config` | SPEC-02 | Flag parsing |
| `pkg/input` | SPEC-03 | CIDR/Rich CSV parsing |
| `pkg/task` | SPEC-04 | Task modeling |
| `pkg/scanner` | SPEC-05 | TCP probe |
| `pkg/scanapp` | SPEC-06 | Scan orchestration |
| `pkg/ratelimit` | SPEC-07 | Rate limiting |
| `pkg/speedctrl` | SPEC-08 | Pause control |
| `pkg/writer` | SPEC-09 | CSV output |
| `pkg/validate` | SPEC-10 | Validation |
| `pkg/state` | SPEC-11 | Resume/signal |
| `pkg/logx` | SPEC-12 | Logging |
| `pkg/cli` | - | CLI utilities |
| `pkg/scanapp/dashboard_*` | SPEC-13 | Rich dashboard (planned) |

---

## Implementation Guide

### Starting a New Feature

1. **Identify the subsystem** from the architecture
2. **Read the relevant SPEC** document
3. **Check integration points** in this index
4. **Implement** according to the specification
5. **Verify** against success criteria in the spec

### Cross-Subsystem Changes

For changes that affect multiple specs:

1. **Identify all affected specs**
2. **Review dependency graph** above
3. **Implement in dependency order** (bottom-up)
4. **Test integration** between subsystems

### Adding New Subsystems

1. Create new SPEC document in `docs/specs/`
2. Add to this index
3. Define package in `pkg/`
4. Add integration points
5. Update architecture diagram

---

## Quick Reference

### CLI Commands

| Command | Spec | Description |
|---------|------|-------------|
| `validate` | SPEC-01 + SPEC-10 | Validate input files |
| `scan` | SPEC-01 + SPEC-06 | Run full scan |

### Key Functions

| Function | Spec | Package |
|----------|------|---------|
| `Run()` | SPEC-06 | `scanapp` |
| `ScanTCP()` | SPEC-05 | `scanner` |
| `Parse()` | SPEC-02 | `config` |
| `Inputs()` | SPEC-10 | `validate` |
| `LoadCIDRsWithColumns()` | SPEC-03 | `input` |
| `ExpandIPSelectors()` | SPEC-04 | `task` |
| `BuildExecutionKey()` | SPEC-04 | `task` |
| `NewLeakyBucket()` | SPEC-07 | `ratelimit` |
| `NewController()` | SPEC-08 | `speedctrl` |
| `Controller.APIPaused()` | SPEC-08 | `speedctrl` |
| `Save()` | SPEC-11 | `state` |
| `New()` | SPEC-12 | `logx` |
| `buildCIDRGroups()` | SPEC-06 | `scanapp` |
| `buildRichGroups()` | SPEC-06 | `scanapp` |

### Exit Codes

| Code | Meaning | Spec |
|------|---------|------|
| 0 | Success | SPEC-01 |
| 1 | Validation/Runtime error | SPEC-01 |
| 2 | CLI parsing error | SPEC-01 |
| 130 | Scan canceled (SIGINT) | SPEC-01 |

---

## Version

- Created: 2024-03-18
- Last Updated: 2024-03-18
- Total Specifications: 13

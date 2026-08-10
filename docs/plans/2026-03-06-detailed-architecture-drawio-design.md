# 2026-03-06 Detailed Architecture Drawings Design (Draw.io Compatible)

**Status:** Historical

**Current architecture:** [port-scan design](../apps/port-scan/DESIGN.md)

## 1. Objective

Create a detailed architecture diagram set for `port-scan-mk3` in draw.io-compatible format that clearly shows:
- component interactions
- input/output structures per component
- component responsibilities
- happy path and sad path behaviors

The deliverables must serve both implementation engineers and architecture reviewers.

## 2. Scope and Audience

### Scope
- Two-layer diagram set:
  - 1 system overview page
  - 3 detailed pages
- Dual output format:
  - `.drawio` as source of truth (SSOT)
  - `.html` as readable artifact with embedded mxGraph XML blocks

### Audience
- Engineering implementation audience (module boundaries, contracts, recovery)
- Architecture review audience (separation of concerns, risk controls, extensibility)

## 3. Chosen Approach

### Option selected
**Draw.io SSOT + HTML presentation layer**

### Why selected
- best editing workflow in draw.io
- single source minimizes drift
- still provides searchable/reviewable HTML representation

### Rejected options
- HTML-first with later `.drawio` backfill (drift risk)
- dual independent maintenance (`.drawio` and `.html`) (high sync cost)

## 4. Artifact Plan

## 4.1 SSOT diagram file
- `docs/architecture/port-scan-mk3-architecture.drawio`

## 4.2 Presentation file
- `docs/architecture/port-scan-mk3-architecture.html`

Each HTML section maps 1:1 with one draw.io page and includes:
- diagram preview
- raw mxGraph XML block
- mapping notes
- validation checklist

## 5. Page Information Architecture (4 pages)

### P01-System-Overview
Purpose:
- visualize full system context and major component boundaries

Includes:
- CLI/config
- input
- task
- scanapp
- scanner
- speedctrl
- writer
- state
- logx
- external systems (CIDR CSV, ports input, pressure API, targets)
- runtime artifacts (scan results, open-only results, resume state)

### P02-Happy-Path-Dataflow
Purpose:
- show successful end-to-end runtime behavior

Includes:
- `validate` preflight success route
- `scan` runtime route
- worker dispatch and probing
- output generation and completion summary

### P03-Sad-Path-Error-Recovery
Purpose:
- show failure and recovery behavior

Required sad paths:
1. input validation failure -> early stop
2. pressure API repeated failure -> escalation -> save resume state -> non-zero exit
3. cancellation/interruption -> save resume state -> rerun with `-resume`

### P04-Component-Contracts
Purpose:
- show what each component does and I/O contract shape

Each component entry includes:
- input schema/structure summary
- processing responsibility
- output schema/structure summary
- important status transitions/errors

## 6. Interaction and Contract Model

## 6.1 Core component interactions
1. `cmd/config -> input`
- input: CLI flags and runtime options
- output: normalized config and loaded records

2. `input -> task`
- input: validated CIDR records and ports
- output: expanded targets and task descriptors

3. `task -> scanapp -> scanner`
- input: task queue (`target x port`, execution key)
- output: probe result status (`open`, `close`, `close(timeout)`) and response timing

4. `scanapp -> writer/state/logx`
- writer output: `scan_results-*`, `opened_results-*`
- state output: `resume_state.json` or explicit `-resume` path
- log output: structured events and progress

5. `pressure API -> speedctrl -> scanapp` (control flow)
- input: polled pressure signals
- output: pause/resume gate state and escalation decision

## 6.2 TCP probe boundary requirement
Must be explicit on P02 and P03:
- use `net.DialTimeout("tcp", target, timeout)`
- success closure via `conn.Close()`
- not a raw SYN/RST packet scanner
- not active RST-forced teardown strategy

## 7. Visual Grammar

## 7.1 Node types
- Process
- Data
- Control
- Decision
- Artifact
- Boundary

## 7.2 Edge types
- solid arrow: main data/control progression
- dashed arrow: external control signal (pressure/signal)
- dotted arrow: derived artifact flow/reporting

## 7.3 Stable IDs
- node: `Pxx-Nyy`
- edge: `Pxx-Eyy`

## 8. Happy Path and Sad Path Granularity

### Happy path target granularity
- 12 to 16 nodes
- explicit input/output tags on key transitions

### Sad path target granularity
- 14 to 18 nodes
- each error branch must include:
  - trigger condition
  - effect/result status
  - recovery action

## 9. Error Handling and Fallback Strategy

- if a page becomes too dense, split subflow in place while preserving page intent
- if XML snippet readability degrades, keep compact and add mapping notes
- maintain complete semantic coverage even if visual complexity is reduced

## 10. Validation Criteria

## 10.1 Structural checks
- `.drawio` has exactly 4 pages (P01-P04)
- HTML has exactly 4 corresponding sections (`#p01` to `#p04`)

## 10.2 Semantic checks
- P02 contains full happy path route
- P03 contains all three required sad paths
- P02/P03 include TCP dial boundary statements
- P04 includes responsibilities and I/O structures for all core components

## 10.3 Import compatibility checks
- extract XML for any 2 pages and import successfully in draw.io

## 11. Non-Goals

- no replacement of the 19-slide drawio assets set
- no automated converter pipeline between arbitrary HTML and draw.io
- no expansion into speculative features beyond current architecture

## 12. Maintenance Strategy

- update `.drawio` first (SSOT)
- then update HTML mapped section
- run structural and semantic checks before merge

## 13. Approved Design Summary

Approved by user:
- diagram set style: two-level set (1 overview + 3 detailed pages)
- output format: both `.drawio` and `.html`
- target: both implementation and architecture review use cases
- approach: draw.io SSOT + HTML presentation mapping

## 14. Next Step

After this design doc, invoke `writing-plans` to produce executable implementation plan.

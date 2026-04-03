# Phase 03: Multi-Source Pressure Inputs and Cross-Platform Run Scripts - Context

**Gathered:** 2026-04-03
**Status:** Ready for planning
**Source:** User request via `$gsd-plan-phase`

<domain>
## Phase Boundary

This phase expands pressure control from one source to multiple sources in a single scan run, while preserving existing single-source behavior. It also delivers runnable example scripts for Linux and Windows to demonstrate multi-source usage.

In scope:
- CLI/config contract for multiple pressure sources.
- Runtime aggregation path for multiple pressure fetchers.
- Defined policy to convert multiple pressure readings into pause/resume decisions.
- Example execution scripts (`.sh` and `.bat`) for Linux/Windows.

Out of scope:
- New pressure provider protocols beyond current HTTP-based source model.
- Full deployment automation or installer packaging.
</domain>

<decisions>
## Implementation Decisions

### Locked by user
- Support multiple pressure sources in one run; current single-source limitation must be removed.
- Provide sample run scripts for Linux and Windows.
- Script formats are explicitly:
  - Linux: Bash (`.sh`)
  - Windows: Batch (`.bat`)

### Locked by project guardrails
- Follow SOLID and keep `cmd/port-scan` focused on CLI assembly/argument parsing/I/O only.
- Reusable logic must remain in `pkg/` (no business logic in CLI command handlers).
- Keep interfaces minimal and owned by consumers; avoid god interfaces and cyclic dependencies.

### the agent's Discretion
- Exact CLI syntax for multiple pressure sources (repeatable flags vs CSV-encoded list vs config file reference).
- Pressure aggregation policy defaults (e.g., max/min/average/any-fail) and user override strategy.
- Concrete script filenames and directory layout, as long as one `.sh` and one `.bat` sample are provided and documented.
- Backward-compatibility migration notes for existing users.
</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Current roadmap and requirement baseline
- `.planning/ROADMAP.md` — Phase sequencing and success criteria framing.
- `.planning/REQUIREMENTS.md` — Requirement inventory and traceability.
- `.planning/STATE.md` — Current project focus and phase state.

### Existing pressure and scan runtime flow
- `pkg/config/config.go` — current pressure-related flags/config parsing.
- `cmd/port-scan/command_handlers.go` — CLI-to-RunOptions wiring.
- `pkg/scanapp/pressure.go` — pressure fetcher implementations and contracts.
- `pkg/scanapp/pressure_monitor.go` — pressure polling / failure behavior.
- `pkg/scanapp/scan.go` — orchestration and pause-control integration.

### Existing architecture and quality references
- `.planning/codebase/ARCHITECTURE.md` — layered orchestration and boundaries.
- `.planning/codebase/CONCERNS.md` — known risks and fragile areas.
- `.planning/codebase/CONVENTIONS.md` — coding/testing conventions.
- `.planning/codebase/TESTING.md` — current testing patterns and quality gates.
</canonical_refs>

<specifics>
## Specific Ideas

- Prefer a config shape that allows adding pressure sources incrementally without requiring mutually exclusive old/new modes.
- Ensure scripts include placeholders/comments for endpoint URL, auth credentials, interval, delay, and output paths.
- Include one sample that demonstrates mixed source types (e.g., simple + authenticated).
</specifics>

<deferred>
## Deferred Ideas

- Dynamic plugin-based pressure source loading.
- Non-HTTP pressure adapters (Kafka, file watcher, metrics backends).
- Centralized secret-management integration for sample scripts.
</deferred>

---

*Phase: 03-support-multiple-pressure-sources-and-provide-cross-platform-run-scripts*
*Context gathered: 2026-04-03 from user request*

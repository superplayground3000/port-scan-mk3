# README / Docs Enhancement Design

**Status:** Historical

**Current architecture:** [port-scan design](../apps/port-scan/DESIGN.md)

- Date: 2026-03-02
- Owner: Codex + xuxiping
- Scope: Documentation only (no runtime behavior changes)

## 1. Goals

- Extend project documentation for developer/contributor audience.
- Introduce how `port-scan-mk3` works end-to-end.
- Provide complete, maintainable flag documentation.
- Add scenario-based command examples (8-10 scenarios).
- Explain e2e architecture and what e2e validates.
- Provide a static architecture diagram rendered with HTML + CSS.

## 2. Non-Goals

- No changes to scanning logic, CLI behavior, or API contracts.
- No new runtime dependencies.
- No interactive diagram behavior (static diagram only).

## 3. Audience

Primary audience: developers and contributors who need to understand architecture, contracts, and operational scenarios.

## 4. Information Architecture

## 4.1 README (entry point)

README will be a concise navigation layer with:

1. Project positioning (what it does)
2. `How It Works` (high-level flow)
3. Command overview (`validate` vs `scan`)
4. Flags quick reference (high-impact subset)
5. Scenario cookbook index (links)
6. E2E overview (what is tested + outputs)
7. Architecture diagram entry (link to HTML diagram)

README should avoid duplicating exhaustive details that belong in dedicated docs.

## 4.2 Dedicated docs

Add:

- `docs/cli/flags.md`
- `docs/cli/scenarios.md`
- `docs/e2e/overview.md`
- `docs/architecture/diagram.html`
- `docs/architecture/diagram.css`

## 5. Content Design

## 5.1 `docs/cli/flags.md`

Structure:

- Full flags table sourced from current code (`pkg/config/config.go`, `cmd/port-scan/main.go`).
- For each flag: type, default, applicable command(s), behavior notes.
- Interaction/dependency notes (for example resume path and output directory relation).
- Common mistakes and corrective examples.

## 5.2 `docs/cli/scenarios.md`

Target scenarios (8-10):

1. Basic scan
2. Column mapping customization
3. Validate in human format
4. Validate in JSON format
5. Pressure control enabled
6. Pressure API failure behavior
7. Resume with explicit state path
8. Cancel/SIGINT and resume workflow
9. Same-second output collision naming
10. E2E parity check commands

Each scenario will include:

- Goal
- Command
- Expected outputs/behavior
- Failure notes (if applicable)

## 5.3 `docs/e2e/overview.md`

Describe:

- e2e objective and boundaries
- Docker Compose topology (`scanner`, target mocks, pressure API mocks)
- What is tested:
  - normal open/closed behavior
  - pressure API 5xx handling
  - pressure API timeout handling
  - pressure API connection-fail handling
  - output artifact generation and report checks
- How to run and how to interpret outputs under `e2e/out/`

## 5.4 Static architecture diagram (`diagram.html` + `diagram.css`)

Diagram style: static block flowchart.

Layers:

- CLI layer (`port-scan validate|scan`)
- Core packages (`config`, `input`, `task`, `scanapp`, `writer`, `state`, `speedctrl`, `logx`)
- External/artifact layer (CIDR/Port CSV input, pressure API, scan targets, output CSVs, resume state, e2e report)

Data flow arrows:

- Input → parser/validator → task expansion → scanner orchestration → writer outputs
- Pressure API → speed control gate → scan dispatch
- Runtime interruption/failure → resume state persistence
- e2e execution → artifact/report verification

## 6. Approach Options and Trade-offs

### Option A (Recommended): README as navigation + deep docs

- Pros: maintainable, readable, avoids duplication, contributor-friendly.
- Cons: requires cross-link hygiene.

### Option B: README monolith

- Pros: single file access.
- Cons: long and harder to maintain.

### Option C: docs-first minimal README

- Pros: very clean separation.
- Cons: weak first-contact experience.

Recommendation: Option A.

## 7. Risks and Mitigations

- Risk: documentation drift from code flags
  - Mitigation: use code-backed source list from `pkg/config/config.go` and command usage in `cmd/port-scan/main.go`.
- Risk: examples become stale
  - Mitigation: keep scenario docs compact and aligned with current tests/e2e scripts.
- Risk: architecture diagram mismatch
  - Mitigation: tie diagram blocks directly to existing package/module names.

## 8. Validation Checklist

- README links resolve to all new docs.
- Full flag coverage matches current CLI parser code.
- Scenario commands are copy-paste runnable.
- e2e doc accurately reflects `e2e/run_e2e.sh` behavior.
- Diagram renders correctly in common markdown/HTML viewers.

## 9. Implementation Handoff

Next step is to produce a concrete implementation plan via `writing-plans` skill, then execute in a separate implementation phase.

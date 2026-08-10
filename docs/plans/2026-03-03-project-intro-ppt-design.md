# 2026-03-03 Project Intro PPT Design (port-scan-mk3)

**Status:** Historical

**Current architecture:** [port-scan design](../apps/port-scan/DESIGN.md)

## 1. Context

- Project: `port-scan-mk3`
- Audience: technical team
- Deck duration: ~30 minutes
- Slide count target: 15-18 slides (selected: 17)
- Script style: full speaker script (can be read directly)

Key source materials reviewed:
- `README.md`
- `docs/cli/flags.md`
- `docs/cli/scenarios.md`
- `docs/architecture/diagram.html`
- `docs/e2e/overview.md`
- `docs/release-notes/1.2.0.md`
- runtime implementation check in `pkg/scanner/scanner.go` and `pkg/scanapp/scan.go`

## 2. Clarified Requirements

User-approved requirements:
- Use architecture-first story flow with a short pain-point opening.
- Must explicitly teach that scanning uses Go `net` TCP dial (regular connection lifecycle).
- Must explicitly state this is not RST-forced teardown behavior.

## 3. Approaches Considered

1. Architecture-first (recommended and approved)
- Start with goals and pipeline, then module boundaries, then features and usage.
- Best for technical alignment and onboarding.

2. Problem-first
- Start from pain points and map each to implementation.
- Strong persuasion, weaker architecture depth.

3. Deep implementation-first
- Start from parser/task/runtime internals.
- Most technical depth, potentially too dense.

## 4. Approved Deck Design

### Section A: Positioning and Success Criteria
- Goal: Align team on why and how `port-scan-mk3` works.
- Success criteria:
  - Audience can explain pipeline `input -> task -> scanapp -> writer/state`.
  - Audience can run `validate` and `scan` correctly.
  - Audience understands differentiation: fail-fast validation, pressure-aware pacing, resume support, e2e verifiability.
  - Audience understands TCP probe model via Go `net.DialTimeout` + normal close, not active RST-forced teardown.

### Section B: Architecture (6 slides)
- End-to-end flow
- CLI/config layer
- Input/task modeling
- Runtime orchestration
- Output/resume
- TCP probe and connection lifecycle model (explicitly non-RST strategy)

### Section C: Feature Implementation (5 slides)
- Fail-fast validation
- Rich input parser and canonical field contract
- Execution-key dedup
- Pressure-aware control and escalation
- Observability and output contracts

### Section D: Usage (3 slides)
- Minimum workflow
- Common flag combinations
- Troubleshooting and recovery patterns

### Section E: Quality and Wrap-up (3 slides in final deck framing)
- Quality gates and e2e evidence
- Known boundaries and non-goals
- Next-step roadmap

## 5. Data Flow and Technical Accuracy Notes

- Probe behavior (from code):
  - `pkg/scanapp/scan.go` defaults dial function to `net.DialTimeout`.
  - `pkg/scanner/scanner.go` calls dial with `("tcp", target, timeout)`.
  - On successful connect, code executes `conn.Close()` and records status `open`.
  - On timeout/error, status is mapped to `close(timeout)` or `close`.
- Therefore deck wording should avoid packet-crafting terminology and emphasize socket connect semantics.

## 6. Risks and Mitigations in Presentation

- Risk: audience confuses with SYN/RST scan model.
  - Mitigation: dedicate one full slide to connection lifecycle and non-goals.
- Risk: too feature-heavy without operational guidance.
  - Mitigation: preserve 3 usage slides with copy-paste workflow.
- Risk: unclear production confidence.
  - Mitigation: include quality gates (`go test`, coverage gate, e2e matrix) and artifact examples.

## 7. Deliverable Format

Primary deliverable in this session:
- 17-slide full speaker script in Traditional Chinese, suitable for direct presentation.
- Each slide contains:
  - Slide title
  - On-slide bullets
  - Full narration script

## 8. Transition

Brainstorming design is validated by user. Next step is invoking writing-plans skill to produce the implementation-style writing plan before generating or refining final artifacts.

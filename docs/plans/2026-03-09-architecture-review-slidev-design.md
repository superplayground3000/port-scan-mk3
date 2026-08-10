# 2026-03-09 Architecture Review Slidev Design

**Status:** Historical

**Current architecture:** [port-scan design](../apps/port-scan/DESIGN.md)

## 1. Context

- Project: `port-scan-mk3`
- Deliverable: Slidev-based architecture review deck with draw.io-backed diagrams
- Primary audience: architecture reviewers evaluating design rationality
- Secondary audience: implementation engineers who need to understand why the boundaries exist
- Preferred language: Traditional Chinese

The deck should not behave like an onboarding walkthrough. The main job is to justify why the current design is reasonable under the project's operational constraints.

## 2. Review Objective

The deck must help reviewers answer four questions quickly:

1. What requirements and constraints shaped the design?
2. Why are the package boundaries and runtime control points arranged this way?
3. Where are the current bottlenecks and operational risks?
4. What trade-offs were intentionally accepted, and why?

## 3. Key Design Direction

Approved direction:
- Use a **design-rationale-first** narrative.
- Use diagrams heavily; each diagram must explain a decision, not just decorate a slide.
- Focus on `requirements`, `architecture`, `bottlenecks`, and `trade-offs`.

This means the deck should lead with requirement pressure and decision rationale, then use architecture/data-flow/failure-path diagrams to show how those decisions materialize in the codebase.

## 4. Options Considered

### Option A: Design-rationale review deck (approved)

- Structure: `requirements pressure -> design decisions -> architecture -> bottlenecks -> trade-offs -> verdict`
- Strength:
  - best matches architecture review
  - directly answers "why this design"
  - creates natural connection between requirements and boundaries
- Weakness:
  - less operational tutorial content

### Option B: Architecture walkthrough deck

- Structure: `system overview -> packages -> flows -> quality gates`
- Strength:
  - easy to follow visually
  - good for onboarding
- Weakness:
  - can become descriptive instead of evaluative

### Option C: Problem-solution deck

- Structure: `pain points -> solution mapping -> outcomes`
- Strength:
  - persuasive
  - simple story arc
- Weakness:
  - may under-explain actual architecture boundaries and control surfaces

## 5. Chosen Approach

Use Option A as the primary narrative and borrow the strongest visual patterns from Option B.

In practice:
- the story is review-first
- the visuals are architecture-first
- every major slide must state both `what the design is` and `why it is that way`

## 6. Deck Structure

Target size:
- 14 to 16 slides
- approximately 30 minutes

Planned sections:

### A. Review Context

Purpose:
- define what is being reviewed
- clarify success criteria and non-goals

### B. Requirement Pressure to Design Decision

Purpose:
- show the operational constraints driving the design
- map each pressure to a specific architectural response

### C. Architecture and Runtime Control

Purpose:
- show package boundaries
- explain data flow and control flow
- justify pause/resume, pressure-aware gating, and artifact persistence

### D. Bottlenecks and Trade-offs

Purpose:
- expose the main throughput and complexity constraints
- explain why current choices were favored over more complex alternatives

### E. Review Verdict

Purpose:
- summarize why the design is acceptable now
- make known limitations explicit
- frame future evolution without overcommitting

## 7. Visual Asset Strategy

The deck should use as many purposeful diagrams as practical. Expected asset set:

1. Requirement-to-design mapping diagram
2. System context and responsibility boundary diagram
3. End-to-end happy path data-flow diagram
4. Failure and recovery diagram
5. Bottleneck and trade-off comparison diagram

Asset rules:
- keep `.drawio` sources in the repo
- export SVG for Slidev consumption
- avoid decorative screenshots unless they convey review evidence

## 8. Content Principles

- Prefer one message per slide.
- Keep bullets short; rely on diagrams for detail.
- Each architecture diagram must have an explicit rationale subtitle or callout.
- Review language should emphasize:
  - design intent
  - protected contracts
  - boundary ownership
  - operational risk control

## 9. Slide-Level Emphasis

The final deck should explicitly cover:

- why fail-fast validation is preferable to permissive runtime failure
- why `cmd/port-scan` stays as composition root only
- why `pkg/scanapp` orchestration is decomposed into smaller collaborators
- why pressure-aware control is in-process rather than delegated to an external scheduler
- why resumable state and deterministic artifacts are part of the architecture rather than tooling extras
- why the scanner uses standard Go TCP dial/close behavior instead of raw packet strategies

## 10. Expected Deliverables

Primary deliverables:
- Slidev entry deck for architecture review
- draw.io source file for review-specific diagrams
- exported SVG assets used by the deck

Suggested locations:
- `docs/slides/architecture-review/slides.md`
- `docs/slides/architecture-review/slides/*.md`
- `docs/slides/architecture-review/public/diagrams/*.svg`
- `docs/slides/architecture-review/public/diagrams-src/*.drawio`

## 11. Approval Summary

Validated with user:
- audience is architecture review rather than onboarding
- the deck should emphasize why the design is reasonable
- the deck should use diagrams heavily
- the key review topics are requirements, architecture, bottlenecks, and trade-offs

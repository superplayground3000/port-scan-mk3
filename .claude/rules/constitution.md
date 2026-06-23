<!--
Sync Impact Report
- Version change: 1.1.0 -> 1.2.0
- Modified principles:
  - None
- Added sections:
  - VIII. SOLID Structural Boundaries
- Removed sections:
  - None
- Templates requiring updates:
  - ✅ .specify/templates/plan-template.md
  - ✅ .specify/templates/spec-template.md
  - ✅ .specify/templates/tasks-template.md
  - ✅ .specify/templates/commands/*.md (directory not present, no update required)
- Follow-up TODOs:
  - None
-->
# Port Scan MKIII Constitution

## Core Principles

### I. Library-First Design
- New scanner behavior MUST be implemented in reusable packages under `pkg/` before CLI wiring.
- Packages MUST expose deterministic APIs and include unit tests for success and failure paths.
- Public package behavior MUST be documented with Go doc comments that describe inputs, outputs,
  and failure modes.

Rationale: Isolating domain logic from CLI wiring keeps features reusable, testable, and easier
to review.

### II. CLI Contract-First
- User-facing workflows MUST be accessible through `cmd/port-scan`.
- CLI commands MUST support both `human` and `json` formats when output is user-consumable.
- Any CLI contract change (flags, output schema, defaults) MUST include compatibility notes in
  release notes.

Rationale: The CLI is the product boundary; stable contracts protect automation and operators.

### III. Test-First Delivery (NON-NEGOTIABLE)
- Production behavior changes MUST start with failing tests before implementation code is added.
- Every feature or bug fix MUST include unit tests and update integration or e2e tests when
  interfaces or runtime behavior change.
- `go test ./...` MUST pass before merge; skipping or quarantining failing tests is prohibited.

Rationale: Red-green-refactor catches regressions early and keeps behavior changes intentional.

### IV. Integration Coverage for Contract Boundaries
- Integration tests MUST cover parser, task expansion, pipeline orchestration, and writer
  boundaries when their contracts change.
- Schema or protocol updates MUST include compatibility regression cases.
- Integration tests MUST run in pull request validation before merge.

Rationale: Boundary failures are high-risk in scanning pipelines and need contract-level evidence.

### V. Isolated End-to-End Verification
- e2e tests MUST run in Docker Compose with isolated networks and mock services only.
- e2e scenarios MUST cover open-port detection, closed or timeout behavior, and pressure-control
  failure handling when the feature touches those paths.
- e2e runs MUST produce report artifacts in `e2e/out/` for human and automated review.

Rationale: Isolated e2e prevents accidental external scanning and validates production-like flows.

### VI. Observability by Default
- Runtime logs MUST be structured and include target, port, state transition, and error cause.
- Retry, throttling, and fatal-exit events MUST be visible in stderr or structured logs.
- Long-running scans MUST emit progress and completion summaries.

Rationale: Port scanning requires reliable diagnostics to debug network variance and failures.

### VII. Versioning and Release Evidence
- Product releases MUST use semantic versioning `MAJOR.MINOR.PATCH`.
- Every release MUST include `docs/release-notes/<version>.md` with features, fixes, breaking
  changes, and migration guidance.
- Breaking changes MUST increment `MAJOR` and document rollback or mitigation steps.

Rationale: Explicit version semantics and release evidence reduce upgrade risk for users.

### VIII. SOLID Structural Boundaries
- Code structure MUST comply with SOLID: each package, type, and function MUST have one
  clear responsibility and one primary reason to change.
- High-level workflows MUST depend on narrow abstractions owned by the consuming package;
  domain packages MUST NOT depend on CLI glue, concrete writers, or transport details.
- Interfaces MUST be minimal and purpose-specific; "god" structs, "god" interfaces, and
  cyclic dependencies are prohibited.
- Feature growth MUST prefer composition or new implementations over modifying stable
  contracts unless a breaking change is intentionally approved and documented.

Rationale: SOLID boundaries keep scanner behavior modular, testable, and safe to extend
without coupling orchestration, I/O, and domain logic together.

## Technology Stack Requirements
- Implementation MUST use Go 1.24.x as the primary language runtime.
- TCP scanning MUST use Go standard library `net` primitives (for example, `net.DialTimeout`)
  unless a deviation is approved in a complexity exception.
- New third-party dependencies SHOULD be minimal and MUST include justification in the PR.
- Code organization MUST keep reusable domain logic in `pkg/` and limit `cmd/port-scan` to
  CLI composition, argument handling, and user-facing I/O.

## Quality Gates
- `go test ./...` MUST pass.
- `bash scripts/coverage_gate.sh` MUST pass with total coverage >= 85%.
- `bash e2e/run_e2e.sh` MUST pass when a change affects scan pipeline, writers, or pressure
  control behavior.
- Pull requests MUST include command output or CI links proving gate execution.
- Changes that add or reshape packages, interfaces, or adapters MUST document SOLID boundary
  decisions in the relevant spec, plan, or pull request.

## Governance
- This constitution supersedes conflicting local practices for this repository.
- Amendment procedure MUST follow all steps:
  1. Submit a PR that explains changed principles, migration impact, and synchronized templates.
  2. Obtain approval from at least one maintainer.
  3. Update constitution version and `Last Amended` date in this file.
- Constitution semantic versioning policy:
  - MAJOR: remove or redefine a principle in a backward-incompatible way.
  - MINOR: add a principle/section or materially expand mandatory guidance.
  - PATCH: wording clarifications with no policy behavior change.
- Compliance review expectations:
  - Every plan, spec, and task artifact MUST include a constitution alignment check.
  - Architecture reviews MUST confirm responsibility boundaries, interface ownership, and
    dependency direction for new or changed packages.
  - Reviewers MUST block merges when MUST-level rules are unmet unless a dated exception is
    recorded in the feature's complexity tracking.
  - Template alignment under `.specify/templates/` MUST be reviewed on every constitution
    amendment.

**Version**: 1.2.0 | **Ratified**: 2026-03-01 | **Last Amended**: 2026-03-07

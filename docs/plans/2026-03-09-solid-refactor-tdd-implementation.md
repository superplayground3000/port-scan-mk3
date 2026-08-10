# SOLID Refactor + TDD Enforcement Implementation Plan

**Status:** Historical

**Current architecture:** [port-scan design](../apps/port-scan/DESIGN.md)

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Deliver the SOLID refactor incrementally by completing `T001-T006` first as hard constraints, then using those artifacts to drive baseline tests and code changes.

**Architecture:** The implementation starts with feature-local design artifacts that lock down CLI stability, runtime boundaries, and TDD evidence rules. Only after those constraints exist do we move into baseline regression tests and incremental extraction of collaborators from `cmd/port-scan/main.go` and `pkg/scanapp/scan.go`.

**Tech Stack:** Go 1.24.x, existing `cmd/port-scan`, `pkg/scanapp`, `tests/integration`, and feature artifacts under `specs/001-solid-refactor-tdd/`

---

## Execution Order

### Task Group 1: Constraint Artifacts First

**Files:**
- Create: `specs/001-solid-refactor-tdd/research.md`
- Create: `specs/001-solid-refactor-tdd/contracts/cli-stability-contract.md`
- Create: `specs/001-solid-refactor-tdd/contracts/tdd-evidence-contract.md`
- Create: `specs/001-solid-refactor-tdd/contracts/runtime-boundaries.md`
- Create: `specs/001-solid-refactor-tdd/data-model.md`
- Create: `specs/001-solid-refactor-tdd/quickstart.md`

**Steps:**
1. Complete `T001-T006` in order from `specs/001-solid-refactor-tdd/tasks.md`.
2. Make each document concrete enough that `T007+` can reference it directly.
3. Do not change production code in this group.

### Task Group 2: Baseline Contract Protection

**Files:**
- Modify: `cmd/port-scan/main_test.go`
- Modify: `cmd/port-scan/main_scan_test.go`
- Modify: `pkg/scanapp/scan_helpers_test.go`
- Modify: `pkg/scanapp/scan_test.go`
- Modify: `pkg/scanapp/scan_observability_test.go`
- Modify: `tests/integration/resume_flow_test.go`
- Modify: `tests/integration/scan_pipeline_test.go`

**Steps:**
1. Use the contract documents from Task Group 1 as the baseline source of truth.
2. Add failing or strengthening regression coverage before any structural change.
3. Only after baseline protection exists, start `US1`.

### Task Group 3: Incremental Refactor Stories

**Files:**
- Modify: `cmd/port-scan/main.go`
- Create: `cmd/port-scan/command_handlers.go`
- Modify: `pkg/scanapp/scan.go`
- Create: `pkg/scanapp/input_loader.go`
- Create: `pkg/scanapp/runtime_builder.go`
- Create: `pkg/scanapp/task_dispatcher.go`
- Create: `pkg/scanapp/pressure_monitor.go`
- Create: `pkg/scanapp/result_aggregator.go`
- Create: `pkg/scanapp/resume_manager.go`

**Steps:**
1. Execute User Story 1 fully before moving to User Story 2.
2. Use `quickstart.md` to record red/green/refactor evidence for each story.
3. Preserve CLI stability and runtime visibility throughout.

### Task Group 4: Final Verification

**Files:**
- Modify: `README.md`
- Modify: `docs/release-notes/1.2.0.md`
- Modify: `specs/001-solid-refactor-tdd/quickstart.md`

**Steps:**
1. Run `go test ./...`.
2. Run `bash scripts/coverage_gate.sh`.
3. Run `bash e2e/run_e2e.sh` when the affected stories touch scan flow, writer flow, or resume behavior.
4. Record all verification evidence before claiming completion.

## Handoff

- Primary execution source: [/Users/xuxiping/tsmc/port-scan-mk3/specs/001-solid-refactor-tdd/tasks.md](/Users/xuxiping/tsmc/port-scan-mk3/specs/001-solid-refactor-tdd/tasks.md)
- Design constraints: [/Users/xuxiping/tsmc/port-scan-mk3/docs/plans/2026-03-09-solid-refactor-tdd-design.md](/Users/xuxiping/tsmc/port-scan-mk3/docs/plans/2026-03-09-solid-refactor-tdd-design.md)
- Spec: [/Users/xuxiping/tsmc/port-scan-mk3/specs/001-solid-refactor-tdd/spec.md](/Users/xuxiping/tsmc/port-scan-mk3/specs/001-solid-refactor-tdd/spec.md)

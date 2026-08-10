# Scanapp Simplification Implementation Plan

**Status:** Historical

**Current architecture:** [port-scan design](../apps/port-scan/DESIGN.md)

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Reduce orchestration complexity in `pkg/scanapp` and `cmd/port-scan` without changing CLI contracts, while aligning the runtime structure with the constitution's SOLID and TDD requirements.

**Architecture:** Keep `pkg/scanapp` as the facade entrypoint and extract narrow collaborators for planning, execution, sink/output, pause control, and resume persistence. Move CLI validation logic into `pkg/` so `cmd/port-scan` remains a composition root with no parsing-rule ownership.

**Tech Stack:** Go 1.24.x, Go standard library, existing internal packages under `pkg/`, existing integration/unit test suites.

---

### Task 1: Freeze Current Contracts With Baseline Tests

**Files:**
- Modify: `cmd/port-scan/main_test.go`
- Modify: `cmd/port-scan/main_scan_test.go`
- Modify: `pkg/scanapp/scan_test.go`
- Modify: `pkg/scanapp/scan_observability_test.go`

**Step 1: Write the failing test**

Add or tighten tests that explicitly lock these behaviors:

- `validate` exit codes and output formats
- `scan` cancel/error exit mapping
- opened-results CSV side effects
- progress and completion event semantics
- resume-save behavior on cancel/API failure

Use focused test names that mention the protected contract.

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/port-scan ./pkg/scanapp -run 'Test(Main|Run|Emit|Persist)' -v`

Expected: FAIL because at least one tightened assertion should reveal current ambiguity or missing coverage.

**Step 3: Write minimal implementation**

Only adjust tests and any supporting test helpers needed to express the contract. Do not change production logic yet.

**Step 4: Run test to verify it passes**

Run: `go test ./cmd/port-scan ./pkg/scanapp -run 'Test(Main|Run|Emit|Persist)' -v`

Expected: PASS with stronger baseline coverage.

**Step 5: Commit**

```bash
git add cmd/port-scan/main_test.go cmd/port-scan/main_scan_test.go pkg/scanapp/scan_test.go pkg/scanapp/scan_observability_test.go
git commit -m "test: lock scanapp and cli contracts"
```

### Task 2: Extract CLI Validation Service Out Of `cmd/port-scan`

**Files:**
- Create: `pkg/validate/service.go`
- Create: `pkg/validate/service_test.go`
- Modify: `cmd/port-scan/command_handlers.go`
- Modify: `cmd/port-scan/main_test.go`

**Step 1: Write the failing test**

Add tests that prove:

- `cmd/port-scan` still returns the same exit codes/output
- validation behavior can be exercised through a package-level service without CLI-owned parsing logic

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/port-scan ./pkg/validate -v`

Expected: FAIL because `pkg/validate` does not exist yet and `command_handlers.go` still owns `validateInputs`.

**Step 3: Write minimal implementation**

Implement a small concrete validation service in `pkg/validate`:

- open CIDR and port files
- delegate to `pkg/input`
- return `(bool, string)` or a similarly narrow result

Update `cmd/port-scan/command_handlers.go` to call the package service and delete local validation details.

**Step 4: Run test to verify it passes**

Run: `go test ./cmd/port-scan ./pkg/validate -v`

Expected: PASS with unchanged CLI behavior.

**Step 5: Commit**

```bash
git add pkg/validate/service.go pkg/validate/service_test.go cmd/port-scan/command_handlers.go cmd/port-scan/main_test.go
git commit -m "refactor: move cli validation into pkg service"
```

### Task 3: Extract Scan Output Setup And Record Mapping

**Files:**
- Create: `pkg/scanapp/output_files.go`
- Create: `pkg/scanapp/record_mapper.go`
- Modify: `pkg/scanapp/scan.go`
- Modify: `pkg/scanapp/result_aggregator.go`
- Modify: `pkg/scanapp/scan_helpers_test.go`

**Step 1: Write the failing test**

Add tests that isolate:

- output file creation and header writing
- scan-task-to-writer-record mapping
- fallback behavior for `ip_cidr`

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/scanapp -run 'Test(Output|Record|Write)' -v`

Expected: FAIL because those seams are still embedded in `scan.go`.

**Step 3: Write minimal implementation**

Create small concrete helpers:

- one for opening scan/opened CSV writers
- one for converting `scanTask + scanner.Result` into `writer.Record`

Update `scan.go` to call them and remove inline mapping/setup logic.

**Step 4: Run test to verify it passes**

Run: `go test ./pkg/scanapp -run 'Test(Output|Record|Write)' -v`

Expected: PASS with slimmer orchestration code.

**Step 5: Commit**

```bash
git add pkg/scanapp/output_files.go pkg/scanapp/record_mapper.go pkg/scanapp/scan.go pkg/scanapp/result_aggregator.go pkg/scanapp/scan_helpers_test.go
git commit -m "refactor: extract scan output setup and record mapping"
```

### Task 4: Extract Worker Pool Execution From `scan.go`

**Files:**
- Create: `pkg/scanapp/executor.go`
- Create: `pkg/scanapp/executor_test.go`
- Modify: `pkg/scanapp/scan.go`
- Modify: `pkg/scanapp/task_dispatcher.go`

**Step 1: Write the failing test**

Add tests that prove:

- worker pool closes result channels correctly
- cancellation stops execution cleanly
- dispatch and scan execution remain behaviorally identical

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/scanapp -run 'TestExecutor|TestDispatch' -v`

Expected: FAIL because execution lifecycle is still embedded in `scan.go`.

**Step 3: Write minimal implementation**

Implement `executor.go` as a concrete collaborator that owns:

- worker startup
- task consumption
- result publication
- worker shutdown and channel close

Keep `dispatchTasks` focused on pacing/gating only.

**Step 4: Run test to verify it passes**

Run: `go test ./pkg/scanapp -run 'TestExecutor|TestDispatch' -v`

Expected: PASS with reduced responsibility in `scan.go`.

**Step 5: Commit**

```bash
git add pkg/scanapp/executor.go pkg/scanapp/executor_test.go pkg/scanapp/scan.go pkg/scanapp/task_dispatcher.go
git commit -m "refactor: extract scan executor"
```

### Task 5: Narrow Runtime Policies And Internal Types

**Files:**
- Create: `pkg/scanapp/runtime_types.go`
- Modify: `pkg/scanapp/scan.go`
- Modify: `pkg/scanapp/runtime_builder.go`
- Modify: `pkg/scanapp/task_dispatcher.go`
- Modify: `pkg/scanapp/result_aggregator.go`
- Modify: `pkg/scanapp/scan_helpers_test.go`

**Step 1: Write the failing test**

Add tests that reveal unwanted coupling:

- dispatch only needs delay/gate policy, not full config
- runtime metadata mapping remains intact after type reshaping

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/scanapp -run 'Test(Build|Dispatch|Runtime)' -v`

Expected: FAIL because helpers still depend on broader state than necessary.

**Step 3: Write minimal implementation**

Introduce narrower internal shapes such as:

- `targetMeta`
- `dispatchPolicy`
- `recordEnvelope`

Remove whole-config dependencies where only a subset is needed.

**Step 4: Run test to verify it passes**

Run: `go test ./pkg/scanapp -run 'Test(Build|Dispatch|Runtime)' -v`

Expected: PASS with simpler parameter lists and reduced duplication.

**Step 5: Commit**

```bash
git add pkg/scanapp/runtime_types.go pkg/scanapp/scan.go pkg/scanapp/runtime_builder.go pkg/scanapp/task_dispatcher.go pkg/scanapp/result_aggregator.go pkg/scanapp/scan_helpers_test.go
git commit -m "refactor: narrow scanapp runtime policies"
```

### Task 6: Evaluate And Remove Or Integrate Dead Pipeline Abstractions

**Files:**
- Modify: `pkg/pipeline/runner.go`
- Modify: `pkg/pipeline/runner_test.go`
- Modify: `pkg/pipeline/extra_test.go`
- Modify: `docs/plans/2026-03-17-scanapp-simplification-design.md`

**Step 1: Write the failing test**

Write a test or decision check that proves whether `pkg/pipeline.Runner` is:

- required by production flow, or
- dead/test-only abstraction that should be removed or explicitly scoped

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/pipeline -v`

Expected: FAIL or produce evidence that the abstraction has no production consumer.

**Step 3: Write minimal implementation**

Choose one:

- remove dead abstraction if it has no justified consumer, or
- explicitly integrate it into `scanapp` only if it simplifies the production design without widening responsibilities

Update the design doc decision note accordingly.

**Step 4: Run test to verify it passes**

Run: `go test ./pkg/pipeline -v`

Expected: PASS with a documented reason for keeping or removing the abstraction.

**Step 5: Commit**

```bash
git add pkg/pipeline/runner.go pkg/pipeline/runner_test.go pkg/pipeline/extra_test.go docs/plans/2026-03-17-scanapp-simplification-design.md
git commit -m "refactor: resolve unused pipeline abstraction"
```

### Task 7: Run Full Verification Gates

**Files:**
- Modify: `docs/plans/2026-03-17-scanapp-simplification-implementation.md`

**Step 1: Write the failing test**

No new test code. This task is verification-only.

**Step 2: Run test to verify current state**

Run: `go test ./...`
Expected: PASS

Run: `bash scripts/coverage_gate.sh`
Expected: PASS with total coverage >= 85%

Run: `bash e2e/run_e2e.sh`
Expected: PASS only if the final implementation changes scan pipeline, writers, or pressure-control behavior

**Step 3: Write minimal implementation**

Document actual verification evidence and any justified omission for e2e in this plan file or the final verification summary.

**Step 4: Run test to verify it passes**

Re-run any gate that failed after fixes until all required gates pass.

**Step 5: Commit**

```bash
git add docs/plans/2026-03-17-scanapp-simplification-implementation.md
git commit -m "docs: record scanapp simplification verification"
```

## Verification Evidence

- `go test ./...`
  - Result: PASS on 2026-03-17 in `/Users/xuxiping/tsmc/port-scan-mk3/.worktrees/scanapp-simplification`

- `bash scripts/coverage_gate.sh`
  - Result: PASS
  - Total coverage: `86.2%`

- `bash e2e/run_e2e.sh`
  - Result: PASS
  - Report artifacts: `e2e/out/`

## Final Notes

- The simplification work changed `pkg/scanapp` orchestration and removed the unused `pkg/pipeline`
  abstraction, so running the e2e gate was required and completed.

# High-Severity Structural Refactor — Design Spec

**Date:** 2026-07-01
**Branch/Worktree:** `worktree-refactor-high-severity-structure`
**Source of findings:** Codex `codex:rescue` code-structure review (2026-07-01)
**Status:** Approved design — pending spec review

## Goal

Reduce maintenance effort and improve readability in the three `[high]`-severity areas
Codex flagged, **without changing any observable behavior**. This is a structural /
clarity refactor, not a behavior or feature change.

## Non-Negotiable Constraints

- **Behavior-preserving.** No change to CLI output, exit codes, file/CSV schemas, or
  observable logs. Existing tests are the safety net.
- **Constitution gates must stay green throughout:**
  - `go test ./...` passes.
  - `bash scripts/coverage_gate.sh` ≥ 85% total coverage.
  - Test-First: when a new package/function boundary is introduced, its unit tests are
    written/relocated before callers are rewired.
  - SOLID boundary decisions documented here and in the plan/PR.
- **Isolated worktree.** All work in `worktree-refactor-high-severity-structure`; `master`
  checkout untouched.
- **Continuous Codex collaboration.** Each phase: confirm approach with Codex *before*
  implementing, hand the resulting diff to Codex for a cross-model review *after*. This is
  both the user's explicit request and the cross-model review rule.

## Out of Scope (explicitly deferred)

These are real Codex findings intentionally deferred because they require behavior changes
or larger architectural moves; captured here for a future phase-2 pass:

- Unifying `pkg/input` rich vs. basic parse contracts (fail-fast vs. return-invalid-as-data)
  — a behavior change.
- `cmd/csv-transform` `ResolveHost` returning structured resolution failure — a behavior change.
- `pkg/scanapp` concurrency-policy extraction from `runReachabilityChecks` /
  `pollPressureAPI` / `dispatchTasks`, and a dedicated planner package — larger architectural
  moves, higher risk than a behavior-preserving pass warrants.
- All `[medium]` / `[low]` findings across `pkg/cidrutil`, `pkg/config`, `pkg/cli`,
  `pkg/enrich`, `e2e/`, `labs/`, `scripts/`, etc.

**Deferred during execution (evaluated + skipped on cross-model review):**
- **C2 — `output_files.go` `os.Stderr` routing:** Codex verdict LEAVE-IT. The direct
  `os.Stderr` close-error writes on the double-error path are intentional; routing them
  via `errors.Join` would change observable stderr, so it is not behavior-preserving.
- **C3 — remove the `ScanRecord` interface:** Codex verdict SKIP. The interface is used
  in production (`result_aggregator.go` getters) and is exported package API; removal is
  not behavior/API-preserving in this pass.
- **Pre-existing (out of scope):** `go test -race` flags a data race in
  `TestRun_WhenExecutorWorkerPanics` present on the pre-refactor baseline; not introduced
  here, left for a dedicated follow-up.

## Phase A — `pkg/input`

**Findings addressed:** god-struct `CIDRRecord`; overloaded `LoadCIDRsWithColumns`;
misleading `ValidateNoOverlap`; duplicated package docs.

**Grounded facts:**
- `ValidateNoOverlap` is a pure alias of `ValidateIPRows` (verified in source). It has
  **zero production callers**; the only reference is `validate_ip_rules_test.go`.

**Changes:**
1. Remove the misleading `ValidateNoOverlap` alias; update the single test to call
   `ValidateIPRows`. (Exported-symbol removal is acceptable: unpublished CLI product, no
   external consumers. Confirm with Codex.)
2. Refactor `LoadCIDRsWithColumns` into named, independently-testable helper steps
   (basic-row parse → rich-row detect → validate). Same inputs/outputs.
3. Improve `CIDRRecord` internal cohesion by grouping its concerns (raw-CSV fields vs.
   parsed-net fields vs. rich-mode metadata) **without** altering the exported field set
   unless a grep proves a change is safe. Exact treatment confirmed with Codex — this is the
   riskiest item in Phase A.
4. Trim duplicated package-level docs between `cidr.go` and `rich_parser.go`.

**Behavior contract:** identical parse results, identical validation errors (same messages,
same row numbers), identical public load API.

## Phase B — `cmd/csv-transform`

**Findings addressed:** application logic living in `cmd/`; package-global `stderr`.

**Changes:**
1. Extract the `runTransform` pipeline (CSV read → column index → filter → resolve →
   port-expand → CSV write) into a new package `pkg/csvtransform`.
2. Replace the package-global `stderr` with an injected `io.Writer` warning sink so the
   package chooses no process-global I/O.
3. `cmd/csv-transform/main.go` shrinks to flag parsing + wiring; relocate transform unit
   tests to `pkg/csvtransform`, keep a thin black-box CLI test.

**Behavior contract:** identical output CSV, identical skip-and-warn messages (now routed to
the injected writer, which `main` wires to `os.Stderr`), identical exit codes.

## Phase C — `pkg/scanapp` (staged, safest subset first)

**Findings addressed:** ~200-line `Run` owning too many policies; direct `os.Stderr` writes
in `output_files.go`; duplicate record abstraction; monolithic test files.

**Grounded facts:**
- `ScanRecord` is **not** dead: it is carried through the pipeline (`runtime_types.go:46`,
  `record_mapper.go`, `scan.go:211`). Its 14 getter methods are, however, consumed almost
  exclusively by tests; production uses only `AsWriterRecord()`. So C3 is a *simplification*
  candidate, not dead-code removal.

**Changes:**
- **C1:** Split `Run` into internal staged units (`prepare` / `precheck` / `execute` /
  `finalize`) — pure extraction preserving order and behavior.
- **C2:** Route `output_files.go` diagnostics through `scanLogger` / return values instead of
  direct `os.Stderr` writes, keeping emitted diagnostics equivalent (verified against tests).
- **C3 (conditional):** Evaluate removing the `ScanRecord` interface and carrying
  `writer.Record` directly through the pipeline. Implement **only if** Codex agrees the
  simplification is net-positive and it stays behavior-preserving; otherwise leave it and
  document the decision.
- **C4:** Mechanical split of oversized test files (`scan_test.go` 1599 lines,
  `scan_observability_test.go`, `scan_helpers_test.go`) by behavior. No test logic changes.

**Behavior contract:** identical scan orchestration, identical outputs and logs, identical
public `scanapp` API.

## Sequencing & Workflow

`A → B → C`. Each phase:

1. Confirm concrete approach with Codex (`codex:rescue`).
2. Implement TDD-style; keep commits small and per-phase.
3. Run `go test ./...` + `scripts/coverage_gate.sh`.
4. Hand the diff to Codex for a cross-model review pass; address feedback.
5. Commit.

## Risks & Mitigations

- **Hidden behavior coupling in `scanapp`** → rely on the existing broad test suite; C1 is
  pure extraction; verify no log/output diffs.
- **Coverage dips from moving code between packages** → relocate tests with the code; run the
  coverage gate each phase.
- **Over-reaching on `CIDRRecord`/`ScanRecord`** → both gated on grep evidence + Codex
  agreement; default to "leave and document" if not clearly net-positive.

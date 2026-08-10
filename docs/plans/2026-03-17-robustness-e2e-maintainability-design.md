# Robustness, E2E Enhancement, and Maintainability Design

**Status:** Historical

**Current architecture:** [port-scan design](../apps/port-scan/DESIGN.md)

**Date:** 2026-03-17
**Approach:** Incremental Fix-First (Approach A)
**Constitution:** v1.2.0 compliance required

## Overview

Three-phase enhancement plan:
1. **Robustness fixes** — concurrency safety, atomic writes, graceful drain
2. **Maintainability refactoring** — decompose scan.go, consolidate group builders, decouple CSV contract
3. **E2E report enhancement** — full test matrix with checklists, timing, artifact inventory, HTML report

## Phase 1: Robustness Fixes

### 1.1 Chunk State Race Condition

**Problem:** `task_dispatcher.go:45` sets `ch.NextIndex = i + 1` in the dispatch goroutine,
while `result_aggregator.go:32` sets `ch.ScannedCount++` in the main goroutine's select loop.
Both mutate `*task.Chunk` concurrently without synchronization.

**Fix:** Introduce a `chunkStateTracker` type wrapping `*task.Chunk` with `sync.Mutex`:

```go
type chunkStateTracker struct {
    mu    sync.Mutex
    chunk *task.Chunk
}
func (t *chunkStateTracker) AdvanceNextIndex(i int)
func (t *chunkStateTracker) IncrementScanned()
func (t *chunkStateTracker) Snapshot() task.Chunk
```

Replace direct `ch.NextIndex` / `ch.ScannedCount` mutations in `task_dispatcher.go` and
`result_aggregator.go` with tracker method calls.

**Files:** `pkg/scanapp/chunk_lifecycle.go` (new), `pkg/scanapp/task_dispatcher.go`,
`pkg/scanapp/result_aggregator.go`

### 1.2 Atomic CSV Writes

**Problem:** `output_files.go` writes directly to the final CSV path. If the scan fails
mid-write, the output is a partial, potentially corrupt CSV.

**Fix:** Write to a temporary file (same directory, `.tmp` suffix), then `os.Rename()` to the
final path on successful completion. On failure, leave the `.tmp` file for debugging and save
resume state. Modify `batchOutputs.Close()` to accept a `success bool` parameter to control
rename-or-keep behavior.

**Files:** `pkg/scanapp/output_files.go`

### 1.3 Output Collision Upper Bound

**Problem:** `batch_output.go:20` loops up to 1,000,000 sequences for collision avoidance.

**Fix:** Cap at 100. If 100 collisions exist within the same second, return an error indicating
misconfiguration.

**Files:** `pkg/scanapp/batch_output.go`

### 1.4 Rate-Limit Token Leak During Pause

**Problem:** In `task_dispatcher.go:19-28`, the pause gate is checked first, then
`bkt.Acquire()` is called. If the scan pauses immediately after token acquisition, the token is
consumed but no task dispatched. Over many pause cycles, tokens leak.

**Fix:** Reorder: acquire token first, then check pause gate before sending task:

```go
if err := rt.bkt.Acquire(ctx); err != nil { return err }
select {
case <-ctx.Done(): return ctx.Err()
case <-ctrl.Gate():
}
// now send task
```

**Files:** `pkg/scanapp/task_dispatcher.go`

### 1.5 Graceful Worker Drain

**Problem:** After context cancellation and a write error, the main event loop cancels further
processing. But results from in-flight workers may fill the `resultCh` buffer, and the loop
stops draining them. This makes the resume state inaccurate because `ScannedCount` lags behind
actual completed scans.

**Fix:** After `runErr` is set, continue draining `resultCh` but skip `writeScanRecord`. Only
call `applyScanResult` so the resume state reflects actual completion. Add a `drainOnly` flag
after `runErr` is set:

```go
if err := writeScanRecord(...); err != nil && runErr == nil {
    runErr = err
    cancel()
}
if runErr == nil {
    writeScanRecord(...)
}
applyScanResult(...)  // always called for accurate resume state
```

**Files:** `pkg/scanapp/scan.go`

## Phase 2: Maintainability Refactoring

### 2.1 Extract Chunk State Manager

Move chunk state lifecycle functions from `scan.go` into `chunk_lifecycle.go`:
- `loadOrBuildChunks()` (scan.go:197-237)
- `buildRuntime()` (scan.go:239-299)
- `hasIncomplete()` (scan.go:180-187)
- `collectChunkStates()` (scan.go:189-195)
- `chunkStateTracker` type from Phase 1

**Files:** `pkg/scanapp/chunk_lifecycle.go`

### 2.2 Consolidate Group Builders

**Problem:** `buildCIDRGroups()` (scan.go:307-354) and `buildRichGroups()` (scan.go:396-444)
share iteration, map building, and sorting. Differences: key derivation, target construction,
port handling, metadata merge.

**Fix:** Extract `group_builder.go` with a strategy interface:

```go
type groupBuildStrategy interface {
    Key(rec input.CIDRRecord) (string, error)
    Targets(rec input.CIDRRecord) ([]scanTarget, int, error)
    MergeTarget(existing *scanTarget, rec input.CIDRRecord)
}
```

Two implementations: `basicGroupStrategy` and `richGroupStrategy`. One shared
`buildGroups(records, strategy)` function. Removes ~100 LOC of duplication.

Also move into this file: `cidrGroup`, `scanTarget`, `targetMeta`, `mergeFieldValue`,
`ipv4ToUint32`.

**Files:** `pkg/scanapp/group_builder.go` (new)

### 2.3 Extract Policy Conversion

Move `runtimePolicy` application into `chunk_lifecycle.go` (where `buildRuntime` lives).
Move `dispatchPolicy` and its constructor into `task_dispatcher.go` (where it's consumed).
`runtime_types.go` keeps only data types: `chunkRuntime`, `scanTask`, `scanResult`.

**Files:** `pkg/scanapp/runtime_types.go`, `pkg/scanapp/chunk_lifecycle.go`,
`pkg/scanapp/task_dispatcher.go`

### 2.4 Decouple CSV Header/Record Mapping

**Problem:** CSV column order hardcoded in two places in `writer/csv_writer.go` (header and
row). Adding a column requires synchronized changes.

**Fix:** Single source of truth via `ColumnDef` slice:

```go
type ColumnDef struct {
    Name    string
    Extract func(Record) string
}

var Columns = []ColumnDef{
    {"ip", func(r Record) string { return r.IP }},
    {"ip_cidr", func(r Record) string { ... }},
    // ...
}
```

`WriteHeader()` and `Write()` both iterate `Columns`.

**Files:** `pkg/writer/csv_writer.go`

### 2.5 Extract Logger and Helpers from scan.go

- Move `scanLogger` type and methods to `scan_logger.go`
- Move `parsePortRows`, `indexToRuntimeTarget`, `ensureFDLimit`, `defaultString` to
  `scan_helpers.go`
- Move `statusErrorCause`, `errorCause` to `result_aggregator.go` (where consumed)

**Result:** `scan.go` shrinks from 618 lines to ~170 lines.

**Files:** `pkg/scanapp/scan_logger.go` (new), `pkg/scanapp/scan_helpers.go` (new),
`pkg/scanapp/result_aggregator.go`, `pkg/scanapp/scan.go`

## Phase 3: E2E Report Enhancement

### 3.1 Report Data Model

Extend `e2e/report/` with rich types:

```go
type ScenarioResult struct {
    Name       string
    Status     string        // "pass" | "fail"
    Duration   time.Duration
    ExitCode   int
    Checklist  []CheckItem
    Artifacts  []Artifact
    LogSnippet string
}

type CheckItem struct {
    Description string
    Passed      bool
    Detail      string
}

type Artifact struct {
    Name     string
    Path     string
    Size     int64
    RowCount int
}

type FullReport struct {
    Timestamp     time.Time
    TotalDuration time.Duration
    Scenarios     []ScenarioResult
    Summary       Summary
    PassCount     int
    FailCount     int
}
```

**Files:** `e2e/report/types.go` (new)

### 3.2 Per-Scenario Checklists

Normal scan checklist:
- `scan_results-*.csv` exists
- `opened_results-*.csv` exists
- `opened_results` contains only "open" rows
- At least 1 open result
- At least 1 non-open result
- CSV header matches 14-column contract
- `report.html` generated
- `report.txt` generated
- Exit code is 0

Failure scenario checklist (api_5xx, api_timeout, api_conn_fail):
- Exit code is non-zero
- `resume_state.json` created
- Resume state JSON is valid and parseable
- Resume state contains expected chunk CIDRs
- Scenario log captured

**Files:** `e2e/report/checklist.go` (new)

### 3.3 Shell Script Changes

`run_e2e.sh` captures timing and writes a JSON manifest:
- Capture `start_time` / `end_time` per scenario via `date +%s%N`
- Write `manifest.json` with per-scenario timing, exit codes, artifact paths
- Checklist evaluation moves into the Go report tool
- Shell script retains only: Docker orchestration, input creation, scan execution, manifest writing

**Files:** `e2e/run_e2e.sh`, `e2e/report/manifest.go` (new)

### 3.4 HTML Report

Full HTML report with CSS styling:
- Summary bar (pass/fail counts, total duration)
- Per-scenario collapsible sections with checklist, artifacts, timing
- Green/red check marks for pass/fail items
- Plain text `report.txt` with equivalent content

**Files:** `e2e/report/generate_report.go` (rewrite), `e2e/report/html_template.go` (new)

### 3.5 Expected Output Diffing

For normal scan scenario, validate CSV structure:
- Header columns match 14-column contract exactly
- Each row has correct field count
- Status values within allowed set (`open`, `close`, `close(timeout)`)
- Port values match input port file
- IP values fall within input CIDR ranges

Mismatches appear as checklist failures with expected-vs-actual detail.

**Files:** `e2e/report/checklist.go`

### 3.6 README Documentation

Add e2e section to `README.md`:
- How to run: `bash e2e/run_e2e.sh`
- Covered scenarios and how to read the report
- Excluded scenarios with rationale:
  - Keyboard interrupt — requires TTY, not automatable in Docker
  - Output write failure — environment-dependent, not reproducible in CI
  - Resume state corruption — unit-tested via `pkg/state`
  - Partial input files — unit-tested via `pkg/input` validation
  - FD limit exhaustion — OS-dependent, pre-scan check guards this

**Files:** `README.md`

## Constitution Alignment

| Principle | How Addressed |
|-----------|--------------|
| I. Library-First | All fixes in `pkg/` with unit tests |
| II. CLI Contract-First | No CLI contract changes |
| III. Test-First (NON-NEGOTIABLE) | Each fix starts with failing test |
| IV. Integration Coverage | Chunk state tracker and atomic writes get integration tests |
| V. Isolated E2E | Enhanced report runs in existing Docker Compose |
| VI. Observability | Logger extraction preserves structured logging |
| VII. Versioning | No version bump needed (no public API change) |
| VIII. SOLID Boundaries | Group builder strategy, chunk state tracker, CSV column contract |

## File Change Summary

**New files:**
- `pkg/scanapp/chunk_lifecycle.go`
- `pkg/scanapp/group_builder.go`
- `pkg/scanapp/scan_logger.go`
- `pkg/scanapp/scan_helpers.go`
- `e2e/report/types.go`
- `e2e/report/checklist.go`
- `e2e/report/manifest.go`
- `e2e/report/html_template.go`

**Modified files:**
- `pkg/scanapp/scan.go` (shrink to ~170 lines)
- `pkg/scanapp/task_dispatcher.go` (token leak fix, policy ownership)
- `pkg/scanapp/result_aggregator.go` (tracker methods, error helpers)
- `pkg/scanapp/output_files.go` (atomic writes)
- `pkg/scanapp/batch_output.go` (collision cap)
- `pkg/scanapp/runtime_types.go` (slim to data types only)
- `pkg/writer/csv_writer.go` (ColumnDef contract)
- `e2e/run_e2e.sh` (manifest capture, simplify assertions)
- `e2e/report/generate_report.go` (rewrite for full report)
- `e2e/report/cmd/generate/main.go` (accept manifest flag)
- `README.md` (e2e section, excluded scenarios)

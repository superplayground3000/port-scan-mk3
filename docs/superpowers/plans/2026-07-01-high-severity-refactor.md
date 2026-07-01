# High-Severity Structural Refactor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Improve readability and reduce maintenance effort in the three `[high]`-severity areas from the Codex review — `pkg/input`, `cmd/csv-transform`, `pkg/scanapp` — without changing any observable behavior.

**Architecture:** Pure structural / clarity refactor. Existing tests are the regression safety net; new tests are added only for new boundaries (e.g. an injected warning sink) or where coverage is thin. Each phase confirms its concrete approach with Codex before editing and hands the diff back to Codex after. Work happens in the `worktree-refactor-high-severity-structure` worktree.

**Tech Stack:** Go 1.24, standard library only (`net`, `encoding/csv`, `io`), existing project packages.

## Global Constraints

- Behavior-preserving: no change to CLI output, exit codes, CSV/file schemas, or observable logs — copied verbatim from the spec.
- `go test ./...` MUST pass after every task.
- `bash scripts/coverage_gate.sh` MUST pass (≥ 85% total coverage) at the end of every phase.
- Idiomatic Go: accept interfaces, return concrete types; wrap errors with `fmt.Errorf("...: %w", err)`.
- No new third-party dependencies.
- All commits use small, per-task messages ending with the `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>` trailer.
- Each phase: (1) confirm approach with Codex before editing, (2) Codex diff review after, address feedback before the phase-final commit.

---

## Phase A — `pkg/input`

### Task A0: Confirm Phase A approach with Codex

- [ ] **Step 1: Ask Codex to confirm the Phase A plan**

Dispatch the `codex:codex-rescue` subagent (cross-model review) with: the three Phase A changes below (remove `ValidateNoOverlap` alias; decompose `LoadCIDRsWithColumns`; the `CIDRRecord` cohesion question), the grounded facts (zero production callers of `ValidateNoOverlap`; `CIDRRecord` exported fields consumed across the repo), and ask specifically: "Is removing the exported `ValidateNoOverlap` alias acceptable, or should it be kept as a `Deprecated:` shim? For `CIDRRecord`, do you agree we should NOT change the exported field set and instead only improve doc/field grouping?"

- [ ] **Step 2: Record Codex's verdict inline in the plan**

Note the decision for A1 (remove vs. deprecate) and A3 (field grouping vs. leave) before proceeding.

### Task A1: Remove the misleading `ValidateNoOverlap` alias

**Files:**
- Modify: `pkg/input/validate.go` (remove lines 10-14, the alias)
- Modify: `pkg/input/validate_ip_rules_test.go:180-186` (point test at `ValidateIPRows`)

**Interfaces:**
- Consumes: existing `ValidateIPRows(rows []CIDRRecord) error`.
- Produces: `ValidateNoOverlap` no longer exists (or becomes a `Deprecated:` shim per A0 verdict).

- [ ] **Step 1: Update the test to call `ValidateIPRows`**

In `pkg/input/validate_ip_rules_test.go`, rename `TestValidateNoOverlap_WhenNetworksDoNotOverlap_ReturnsNil` to `TestValidateIPRows_WhenNetworksDoNotOverlap_ReturnsNil` and change the call `ValidateNoOverlap(rows)` to `ValidateIPRows(rows)`.

- [ ] **Step 2: Run the test to verify it still passes (proves alias unused)**

Run: `go test -run TestValidateIPRows_WhenNetworksDoNotOverlap_ReturnsNil ./pkg/input/`
Expected: PASS

- [ ] **Step 3: Remove the alias**

If A0 verdict is "remove": delete lines 10-14 (`ValidateNoOverlap` doc + func) from `pkg/input/validate.go`.
If A0 verdict is "deprecate": replace the doc comment with `// Deprecated: use ValidateIPRows. ValidateNoOverlap is a misnamed alias retained for compatibility.` and keep the body.

- [ ] **Step 4: Verify the package builds and tests pass**

Run: `go build ./... && go test ./pkg/input/`
Expected: PASS, no unresolved references.

- [ ] **Step 5: Commit**

```bash
git add pkg/input/validate.go pkg/input/validate_ip_rules_test.go
git commit -m "refactor(input): remove misleading ValidateNoOverlap alias"
```

### Task A2: Decompose `LoadCIDRsWithColumns` into named steps

**Files:**
- Modify: `pkg/input/cidr.go:93-172`
- Test: `pkg/input/cidr_test.go` (existing suite is the regression net; add unit tests for the extracted basic-row parser)

**Interfaces:**
- Consumes: existing `detectRichHeaderIndices`, `ParseRichRows`, `normalizeHeader`, `headerIndex`, `CIDRRecord.Parse`, `ValidateIPRows`.
- Produces (new unexported helpers in `cidr.go`):
  - `parseBasicCIDRRows(rows [][]string, ipCol, ipCidrCol string) ([]CIDRRecord, error)` — the basic-mode loop currently inline at lines 117-167.
  - `LoadCIDRsWithColumns` keeps its exact signature and behavior, now reading: validate args → rich-detect branch → `parseBasicCIDRRows` → `ValidateIPRows`.

- [ ] **Step 1: Add a characterization test for the basic-row parser**

Add to `pkg/input/cidr_test.go`:

```go
func TestParseBasicCIDRRows_ParsesFabPortAndSelector(t *testing.T) {
	rows := [][]string{
		{"ip", "ip_cidr", "fab_name", "cidr_name", "port"},
		{"192.168.1.10", "192.168.1.0/24", "fab-1", "web", "443"},
	}
	got, err := parseBasicCIDRRows(rows, "ip", "ip_cidr")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d records, want 1", len(got))
	}
	r := got[0]
	if r.IPRaw != "192.168.1.10" || r.CIDR != "192.168.1.0/24" || r.FabName != "fab-1" || r.CIDRName != "web" || r.Port != 443 {
		t.Errorf("unexpected record: %+v", r)
	}
	if r.Net == nil || r.Selector == nil {
		t.Error("expected Parse() to populate Net and Selector")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails (helper undefined)**

Run: `go test -run TestParseBasicCIDRRows_ParsesFabPortAndSelector ./pkg/input/`
Expected: FAIL — `undefined: parseBasicCIDRRows`.

- [ ] **Step 3: Extract `parseBasicCIDRRows`**

Move the basic-mode body (current `cidr.go` lines 117-167: header normalization, index lookups, per-row loop building `CIDRRecord`, `rec.Parse()`) into a new unexported function `parseBasicCIDRRows(rows [][]string, ipCol, ipCidrCol string) ([]CIDRRecord, error)`. It returns the built slice WITHOUT calling `ValidateIPRows` (validation stays in the caller). Preserve every error string and row-number verbatim.

Rewrite `LoadCIDRsWithColumns` body to:

```go
func LoadCIDRsWithColumns(r io.Reader, ipCol, ipCidrCol string) ([]CIDRRecord, error) {
	cr := csv.NewReader(r)
	rows, err := cr.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(rows) < 2 {
		return nil, fmt.Errorf("cidr csv must include header and at least one row")
	}

	ipCol = strings.TrimSpace(ipCol)
	ipCidrCol = strings.TrimSpace(ipCidrCol)
	if ipCol == "" || ipCidrCol == "" {
		return nil, fmt.Errorf("ip and ip_cidr column names must be non-empty")
	}

	if richIdx, ok := detectRichHeaderIndices(rows[0]); ok {
		records, _, err := ParseRichRows(rows, richIdx)
		if err != nil {
			return nil, err
		}
		return records, nil
	}

	out, err := parseBasicCIDRRows(rows, ipCol, ipCidrCol)
	if err != nil {
		return nil, err
	}
	if err := ValidateIPRows(out); err != nil {
		return nil, err
	}
	return out, nil
}
```

- [ ] **Step 4: Run the new test and the full input suite**

Run: `go test ./pkg/input/`
Expected: PASS (new test + all existing).

- [ ] **Step 5: Commit**

```bash
git add pkg/input/cidr.go pkg/input/cidr_test.go
git commit -m "refactor(input): extract parseBasicCIDRRows from LoadCIDRsWithColumns"
```

### Task A3: `CIDRRecord` cohesion + doc trim (Codex-gated)

**Files:**
- Modify: `pkg/input/types.go` (doc/field grouping only — NO exported-field changes unless A0 verdict explicitly approves)
- Modify: `pkg/input/cidr.go:1-38` and `pkg/input/rich_parser.go` package docs (remove duplication)

- [ ] **Step 1: Apply the A0 verdict for `CIDRRecord`**

Default (per spec): keep the exported field set unchanged; regroup fields under clear comment banners (`// --- Basic mode ---`, `// --- Parsed forms ---`, `// --- Rich mode ---`) and tighten field docs. Only if A0 explicitly approved a structural change, follow that instead.

- [ ] **Step 2: Deduplicate package docs**

The package-level "Function Flow" doc block lives in `cidr.go`. Ensure `rich_parser.go` does not repeat the same overview; leave a one-line pointer instead. Keep exactly one canonical package doc.

- [ ] **Step 3: Verify build + tests + go vet**

Run: `go build ./... && go vet ./pkg/input/ && go test ./pkg/input/`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add pkg/input/types.go pkg/input/cidr.go pkg/input/rich_parser.go
git commit -m "docs(input): regroup CIDRRecord fields and dedupe package docs"
```

### Task A4: Codex review of Phase A + coverage gate

- [ ] **Step 1: Run the coverage gate**

Run: `bash scripts/coverage_gate.sh`
Expected: PASS (≥ 85%).

- [ ] **Step 2: Hand the Phase A diff to Codex**

Dispatch `codex:codex-rescue` with `git diff master...HEAD -- pkg/input` and ask for a behavior-preserving-correctness review. Address any confirmed issue with a follow-up commit before starting Phase B.

---

## Phase B — `cmd/csv-transform` → `pkg/csvtransform`

### Task B0: Confirm Phase B approach with Codex

- [ ] **Step 1: Confirm package name and warning-sink signature**

Dispatch `codex:codex-rescue`: propose new package `pkg/csvtransform` exposing `Run(cfg Config, warn io.Writer) error`, `SplitPorts(portStr string, warn io.Writer) ([]int, error)`, `ResolveHost(host string) (string, error)`, `ShouldIncludeRow(passVal string) bool`; flag parsing (`ParseConfigFromArgs`, `ConfigError`) stays in `cmd/csv-transform`. Confirm the package name and that behavior stays identical (warnings routed to `warn`, which `main` wires to `os.Stderr`).

### Task B1: Create `pkg/csvtransform` with injected warning sink

**Files:**
- Create: `pkg/csvtransform/transform.go` (moved `SplitPorts`, `ResolveHost`, `ShouldIncludeRow`, defaults, `csvHeader`)
- Create: `pkg/csvtransform/pipeline.go` (moved `runTransform` body as exported `Run`)
- Create: `pkg/csvtransform/transform_test.go`, `pkg/csvtransform/pipeline_test.go` (relocate existing `cmd/csv-transform` unit tests for these functions)

**Interfaces:**
- Produces:
  - `type Config struct { Input, Output, SheetName, HostCol, PortCol, PassCol string }`
  - `func Run(cfg Config, warn io.Writer) error`
  - `func SplitPorts(portStr string, warn io.Writer) ([]int, error)`
  - `func ResolveHost(host string) (string, error)`
  - `func ShouldIncludeRow(passVal string) bool`

- [ ] **Step 1: Write the warning-sink test first**

In `pkg/csvtransform/transform_test.go`:

```go
func TestSplitPorts_InvalidPort_WritesWarningToSink(t *testing.T) {
	var buf bytes.Buffer
	got, err := SplitPorts("443/abc", &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("got %v, want nil (row skipped)", got)
	}
	if !strings.Contains(buf.String(), "invalid port") {
		t.Errorf("warning not written to sink: %q", buf.String())
	}
}
```

- [ ] **Step 2: Run to verify it fails (package missing)**

Run: `go test ./pkg/csvtransform/`
Expected: FAIL — package does not exist yet.

- [ ] **Step 3: Create the package by moving code**

Move `SplitPorts`, `ResolveHost`, `ShouldIncludeRow`, `csvHeader`, and the `default*` constants from `cmd/csv-transform/transform.go` + `main.go` into `pkg/csvtransform/`. Change `SplitPorts` to take `warn io.Writer` and replace `fmt.Fprintf(stderr, ...)` with `fmt.Fprintf(warn, ...)`. Delete the package-global `stderr` var. Move `runTransform`'s body into `func Run(cfg Config, warn io.Writer) error`, replacing every `fmt.Fprintf(stderr, ...)` with `warn` and each `SplitPorts(portStr)` call with `SplitPorts(portStr, warn)`. Keep `spreadsheet.NewReader`, column indexing, and CSV output byte-for-byte identical.

- [ ] **Step 4: Relocate existing tests**

Move the `SplitPorts`/`ResolveHost`/`ShouldIncludeRow` test functions from `cmd/csv-transform/*_test.go` into the new package's test files, updating `SplitPorts` calls to pass a `&bytes.Buffer{}` (or `io.Discard`) as the sink.

- [ ] **Step 5: Run the new package tests**

Run: `go test ./pkg/csvtransform/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add pkg/csvtransform/
git commit -m "refactor(csv-transform): extract transform pipeline into pkg/csvtransform"
```

### Task B2: Rewire `cmd/csv-transform` to thin wiring

**Files:**
- Modify: `cmd/csv-transform/main.go` (delete `runTransform`; `runMain` calls `csvtransform.Run`)
- Modify: `cmd/csv-transform/transform.go` (now empty of moved funcs — delete file if nothing remains)
- Modify: `cmd/csv-transform/*_test.go` (keep a thin black-box CLI test; remove tests moved to the package)

**Interfaces:**
- Consumes: `csvtransform.Run`, `csvtransform.Config`.

- [ ] **Step 1: Rewire `runMain`**

In `main.go`, after successful `ParseConfigFromArgs`, build `csvtransform.Config` from the `TransformConfig` fields and call `csvtransform.Run(cfgValue, stderrOut)` instead of `runTransform(cfg)`. Delete the local `runTransform`. Keep exit codes (config error path unchanged, transform error → return 1).

- [ ] **Step 2: Verify the CLI black-box test still passes**

Run: `go test ./cmd/csv-transform/`
Expected: PASS (identical output CSV, identical warnings on stderr, identical exit codes).

- [ ] **Step 3: Full build + test**

Run: `go build ./... && go test ./...`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add cmd/csv-transform/
git commit -m "refactor(csv-transform): reduce main to flag parsing and wiring"
```

### Task B3: Codex review of Phase B + coverage gate + docs

- [ ] **Step 1: Coverage gate**

Run: `bash scripts/coverage_gate.sh`
Expected: PASS.

- [ ] **Step 2: Update docs referencing csv-transform internals**

Per project `documents.md` rule, grep docs/README for `csv-transform` and update any reference to the moved logic (now `pkg/csvtransform`).

Run: `grep -rn "csv-transform\|runTransform" docs/ README.md 2>/dev/null`

- [ ] **Step 3: Codex diff review**

Dispatch `codex:codex-rescue` with `git diff` for Phase B; address confirmed issues, then commit any doc/fix follow-ups.

---

## Phase C — `pkg/scanapp`

### Task C0: Confirm Phase C approach with Codex

- [ ] **Step 1: Confirm the `Run` staging boundaries and the two gated items**

Dispatch `codex:codex-rescue` with `scan.go` and `output_files.go`. Confirm: (a) the `prepare` / `execute` / `finalize` split boundaries below; (b) **C2** — whether routing the `openBatchOutputs` close-error `os.Stderr` writes into a joined returned error is acceptable given it changes stderr on the rare double-error path (behavior-preserving question); (c) **C3** — whether removing the `ScanRecord` interface (carry `writer.Record` directly) is net-positive, given its getters are test-only while the interface is carried through the pipeline. Record verdicts for C2/C3 inline.

### Task C1: Split `Run` into `prepare` / `execute` / `finalize` stages

**Files:**
- Modify: `pkg/scanapp/scan.go:45-244`
- Test: existing `pkg/scanapp/scan_test.go`, `scan_observability_test.go` (regression net — no new behavior)

**Interfaces:**
- Produces (new unexported helpers in `scan.go`, all taking/returning existing types):
  - `prepareRun(ctx, cfg, opts, deps, stderr) (*runPlan-ish bundle, error)` — groups the current lines 46-104 (logger, col defaults, FD limit, `loadRunInputs`, `resolveRunOutputPaths`, `loadResumeSnapshot`, `runPreScanPing`, `finalizeUnreachableResults`, `prepareRuntimePlan`, `openBatchOutputs`). Returns a struct bundling `logger`, `inputs`, `plan`, `outputs`, `resumeSnapshot`, `preScan`.
  - `executeScan(...)` — the concurrency wiring + event loop (current lines 110-227), returning `(summary resultSummary, dispatchErr, runErr error)`.
  - `finalizeRun(...)` — resume persistence + completion summary + return value selection (current lines 229-243).
- `Run` becomes a short orchestrator calling the three in order. The `defer outputs.Finalize(scanSuccess)` and `scanSuccess` flag stay in `Run`.

- [ ] **Step 1: Capture a baseline of the scanapp suite (green before refactor)**

Run: `go test ./pkg/scanapp/`
Expected: PASS — this is the regression baseline.

- [ ] **Step 2: Extract `prepareRun`**

Move lines 46-104 into `prepareRun`, returning a `runContext` struct. Preserve ordering exactly (FD limit before inputs, prescan before plan, etc.). No behavior change.

- [ ] **Step 3: Run the suite after the first extraction**

Run: `go test ./pkg/scanapp/`
Expected: PASS.

- [ ] **Step 4: Extract `executeScan` and `finalizeRun`**

Move the concurrency/event-loop block and the finalization block into the two helpers. Thread `runCtx`, `cancel`, `logger`, `plan`, `outputs`, `ctrl`, observers through explicit parameters (no package globals). `Run` now reads as: `prepareRun` → set up `runCtx`/dashboard/`defer Finalize` → `executeScan` → `finalizeRun`.

- [ ] **Step 5: Run the full suite + vet**

Run: `go vet ./pkg/scanapp/ && go test ./pkg/scanapp/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add pkg/scanapp/scan.go
git commit -m "refactor(scanapp): split Run into prepare/execute/finalize stages"
```

### Task C2: Route `output_files.go` close-error diagnostics (Codex-gated)

**Files:**
- Modify: `pkg/scanapp/output_files.go:27-70`

- [ ] **Step 1: Apply the C0/C2 verdict**

If Codex approved joining: replace each `fmt.Fprintf(os.Stderr, "failed to close ...: %v", closeErr)` with `err = errors.Join(err, fmt.Errorf("failed to close scan file: %w", closeErr))` before `return nil, err`, and drop the now-unused `os` import if applicable. If Codex rejected (prefers preserving exact stderr): leave as-is and document the decision in a code comment. Do NOT change behavior beyond the agreed option.

- [ ] **Step 2: Run the suite**

Run: `go test ./pkg/scanapp/`
Expected: PASS.

- [ ] **Step 3: Commit (only if a change was made)**

```bash
git add pkg/scanapp/output_files.go
git commit -m "refactor(scanapp): join close errors instead of writing to os.Stderr"
```

### Task C3: `ScanRecord` simplification (Codex-gated, optional)

**Files:**
- Modify (only if approved): `pkg/scanapp/record_writer.go`, `runtime_types.go:46`, `record_mapper.go`, `scan.go:211`, `executor.go` + related tests.

- [ ] **Step 1: Apply the C0/C3 verdict**

If Codex agreed the simplification is net-positive: replace the `ScanRecord` interface field in the pipeline with `writer.Record` directly (`recordFromScanTask` returns `writer.Record`; `scan.go:211` uses `res.record` directly; delete the adapter + getters). Update tests that used getters to read struct fields. Keep all outputs identical.
If Codex disagreed or it risks behavior/coverage: **skip** this task and add a one-line note to the spec's deferred list. This is explicitly optional.

- [ ] **Step 2: Run the suite + coverage**

Run: `go test ./pkg/scanapp/ && bash scripts/coverage_gate.sh`
Expected: PASS.

- [ ] **Step 3: Commit (only if changed)**

```bash
git add pkg/scanapp/
git commit -m "refactor(scanapp): carry writer.Record directly, drop ScanRecord indirection"
```

### Task C4: Split oversized test files

**Files:**
- Modify: `pkg/scanapp/scan_test.go` (1599 lines) → split by behavior into e.g. `scan_lifecycle_test.go`, `scan_resume_test.go`, `scan_error_test.go` (names per actual test groupings)
- Modify: `pkg/scanapp/scan_observability_test.go`, `scan_helpers_test.go` if similarly broad

**Interfaces:** none — test-file reorganization only. No test logic changes.

- [ ] **Step 1: Group tests by theme**

Read the test function names in `scan_test.go` and cluster them (lifecycle/happy-path, resume, error/cancel, dashboard). Create one new `_test.go` file per cluster in the same package; move whole test functions verbatim (including helpers used only by that cluster). Shared helpers stay in `scan_helpers_test.go`.

- [ ] **Step 2: Verify no test was lost or duplicated**

Run: `go test -count=1 ./pkg/scanapp/ 2>&1 | tail -5` and compare the pass count / `go test -list '.*' ./pkg/scanapp/ | wc -l` before and after (same count).
Expected: identical test list, all PASS.

- [ ] **Step 3: Commit**

```bash
git add pkg/scanapp/
git commit -m "test(scanapp): split monolithic scan_test.go by behavior"
```

### Task C5: Final Codex review + gates + docs

- [ ] **Step 1: Full gates**

Run: `go build ./... && go test ./... && bash scripts/coverage_gate.sh`
Expected: all PASS.

- [ ] **Step 2: Codex whole-phase review**

Dispatch `codex:codex-rescue` with the full `pkg/scanapp` diff for a final cross-model review. Address confirmed issues.

- [ ] **Step 3: Update docs**

Per `documents.md`, update any doc/README/architecture reference affected by the scanapp restructuring.

---

## Self-Review Notes

- **Spec coverage:** Phase A ↔ input findings (god-struct, overloaded loader, ValidateNoOverlap, dup docs); Phase B ↔ csv-transform findings (logic-in-cmd, global stderr); Phase C ↔ scanapp findings (Run size, os.Stderr, ScanRecord dup, test sprawl). Deferred items match the spec's Out-of-Scope list.
- **Gated items:** A1 (remove vs deprecate), A3 (field change), C2 (stderr routing), C3 (ScanRecord removal) all pass through an explicit Codex decision gate before implementation — satisfying the "continuously confirm with Codex" requirement.
- **Regression discipline:** every refactor task runs the existing suite; coverage gate at each phase boundary.

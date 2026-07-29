# Implementation — Speed Up Scan Preparation

Status: draft · Companion to [`design.md`](design.md) · **Primary scenario:
rich-mode input.** Constitution III (test-first) is NON-NEGOTIABLE: every phase
starts with a RED test.

Definition of Done for **every** phase: `make verify` exits 0 (paste the final
line + coverage total). Phases 3–5 also require `make verify-e2e` exit 0.

Phase 0 is complete (`profile-before.txt`): the rich execution-key merge is
confirmed quadratic and IP-parsing is the top CPU frame. Remaining phases are
ordered rich-first — the biggest rich win (B) lands before the amplifier (A).

---

## Phase 0 — Measure (DONE)

`pkg/scanapp/resume_rebuild_bench_test.go` + `docs/speed-up-scan-prepare/profile-before.txt`.
Confirmed: **B (rich merge) is quadratic**, **C (double `net.ParseIP`) is the
largest CPU frame**, **A** scales cost by full CSV size (24.5× at 4000 vs 130
rows). The rich `PRECHECK_ALLOW_ALL` scaling sub-benchmarks are kept as the B
regression guard. The 42k unreachable filter is not implicated.

---

## Phase 1 — Rich merge O(N²)→O(N) + parse each IP once (the primary win)

Biggest measured win, lowest risk: no format change, no API change, targets
identical. This is the headline rich-mode fix.

### 1a. Map-indexed rich execution-key merge (fixes B)
- **RED:** in `group_builder_test.go`, a rich fixture with a large
  `PRECHECK_ALLOW_ALL` segment (many distinct execution keys) **and** a segment
  assembled from many rows sharing keys (exercises de-dup + metadata merge). Assert
  the merged target set/order/metadata is correct; keep the existing
  `rich_precheck_scaling_*` benchmark as the quadratic-vs-linear guard.
- **Fix:** add a build-time `richGroupBuilder{ targets []scanTarget; index
  map[string]int }` inside `buildRichGroupsWithPredicate`
  (`group_builder.go:243-311`); append via the map, replace the
  `richTargetIndexByExecutionKey` linear scan (`:425-432`) and
  `mergeRichTargetIntoGroup` (`:475-486`). Normalize the key once at ingest.
  Finalize builders to `cidrGroup` (drop the map) at the end — `cidrGroup` and all
  downstream code unchanged.
- **GREEN:** same targets as before; the scaling benchmark flips from ~×4 to ~×2
  per host-count doubling.

### 1b. Parse each IP once (fixes C)
- **RED:** structural test/benchmark asserting the reachable predicate takes a
  `uint32` (no per-IP `net.ParseIP`), and `sortRichGroups` no longer parses in its
  less-func.
- **Fix:** carry the parsed `uint32` from `ExpandIPSelectors` (or a wrapper);
  `reachablePredicate` (`pre_scan_ping.go:16-29`) does only `sort.Search`; remove
  `ipv4ToUint32(parsed.String())` double-parse (`scan_helpers.go:44-50`); sort rich
  and basic groups on the precomputed `uint32` (`group_builder.go:490-501, 57-59`).

**Acceptance:** `make verify` exit 0; `profile-after-phase1.txt` shows the
`richTargetIndexByExecutionKey` and duplicate-`ParseIP` frames gone; rich scaling
benchmark now linear.

**Files:** `pkg/scanapp/group_builder.go`, `pkg/scanapp/pre_scan_ping.go`,
`pkg/scanapp/scan_helpers.go`, `pkg/task/selector_expand.go` (+ tests).

---

## Phase 2 — Expand only pending segments (fixes A, rich-aware)

Removes whole-CSV re-expansion; makes resume O(remaining work). Rich groups span
multiple rows, so this is segment-scoped, not row-scoped.

- **RED (differential golden):** `chunk_lifecycle_test.go` — for the rich-shape
  matrix in `design.md §3.4` (one big `PRECHECK_ALLOW_ALL`; many small
  `MATCH_POLICY_ACCEPT`; one segment from many rows with key de-dup + metadata
  merge; mixed reachable/unreachable; mid-`next_index`; legacy `[...]` array; plus
  a basic fixture), assert the **per-segment** generator yields the exact
  `[]scanTarget` (ip, port, meta, order) of the current whole-input
  `buildRuntimeWithPredicate`. Write first; keep green through the refactor.
- **RED (guard):** a fixture where two different segments produce the same
  execution key; assert a loud "resume input is not segment-disjoint; start a fresh
  scan" error (not silent duplication / `total_count` mismatch).
- **Fix:**
  - Build `segmentRows map[segment][]input.CIDRRecord` in one O(rows) pass
    (`richCIDRKey`), **no expansion**; also detect the disjointness violation here.
  - Drive the plan from `snapshot.Chunks`: per pending segment, expand + filter +
    merge **only that segment's rows** (Phase 1's builder), attach ports, honor
    `NextIndex`. Replace the whole-slice `buildRichGroupsWithPredicate(inputs.cidrRecords,…)`
    call at `chunk_lifecycle.go:110-131`. Generate lazily (≈ one segment in memory).
  - Keep the "chunk references a segment absent from input" error
    (`chunk_lifecycle.go:129-130`), now raised from the segment index.

**Acceptance:** `make verify` exit 0; differential golden green (behavior held);
`BenchmarkResumeRebuild` large-input case drops to milliseconds;
`profile-after-phase2.txt` committed.

**Files:** `pkg/scanapp/chunk_lifecycle.go`, `pkg/scanapp/runtime_builder.go`,
`pkg/scanapp/group_builder.go` (+ tests).

---

## Phase 3 — Deliver results on Ctrl+C (requirement 3)

State-on-Ctrl+C already works; the gap is stranded `.tmp` output.

- **RED:** simulate `context.Canceled` mid-scan with rows already written; assert
  the rows land in the **final** output path (not `.tmp`) and the snapshot has
  advanced cursors.
- **Fix:** on graceful cancel, deliver the partial output to the final path (or,
  once Phase 4 lands, we already write to the final path in append mode); a
  hard-failure path still does **not** promote. Distinguish `context.Canceled`
  from other errors around `scan.go:107-110,244` and `output_files.go:92-120`.

**Acceptance:** `make verify` **and** `make verify-e2e` exit 0. e2e: interrupt a
scan, assert scanned rows present in the final file.

**Files:** `pkg/scanapp/output_files.go`, `pkg/scanapp/scan.go`,
`pkg/scanapp/resume_manager.go` (+ tests), `e2e/` interrupt scenario.

---

## Phase 4 — Resume appends to the prior output (requirement 4)

- **RED:** first run writes N rows to a path; `-resume` writes the next M to the
  **same** file (N+M, one header, continuous), not a new timestamped file.
- **Fix:** record the output path in the snapshot on first start; on `-resume`
  reopen it `O_APPEND` instead of `resolveBatchOutputPaths` minting a new name
  (`batch_output.go:16-43`); write header only on new/empty file. Edge tests:
  prior output deleted → recreate with header; schema mismatch → fail loudly;
  `-seq` collision.

**Acceptance:** `make verify` + `make verify-e2e` exit 0. e2e: rich scan →
interrupt → resume, assert one continuous output file, no duplicate header, no
lost/duplicated rows.

**Files:** `pkg/scanapp/batch_output.go`, `pkg/scanapp/runtime_builder.go`,
`pkg/state/state.go`, `pkg/writer/csv_writer.go` (+ tests), e2e.

---

## Phase 5 — Phase-progress logging (requirement 5)

- **RED:** capture the injected log writer; assert the sequence
  `segment_parse_start` → `segment_parse_progress`(×N, throttled) →
  `segment_parse_complete` → `scan_start`, and that existing
  `scan_progress` / `scan_completion` events are unchanged.
- **Fix:** thread `pkg/logx` / `pkg/progress` through the segment-parse rebuild
  (`chunk_lifecycle.go` / `runtime_builder.go`); add `scan_start` before dialing
  (`scan.go`); reuse `-progress-interval`. Do not alter `result_aggregator.go:52-94`.

**Acceptance:** `make verify` exit 0; test asserts new phase lines + preserved
scan-progress lines.

**Files:** `pkg/scanapp/chunk_lifecycle.go`, `pkg/scanapp/runtime_builder.go`,
`pkg/scanapp/scan.go` (+ tests).

---

## Cross-cutting

- **Docs (`.claude/rules/documents.md`):** update `docs/release-notes/<next>.md`
  (rich perf fix + `-resume` append semantics + new log lines); note any CLI
  contract change (constitution II). If the snapshot gains an output-path field,
  document that old snapshots without it still load.
- **Versioning (VII):** append-on-resume and phase logs are additive;
  deliver-on-cancel is a behavior change to call out. No flag removed → MINOR.
- **Review:** final whole-change review → Codex (cross-model), per repo routing.
- **Landing order:** 1 → 2 give the rich speed win (1 first: highest measured
  return, lowest risk); 3 → 4 are the data-integrity changes (gate hardest on
  e2e); 5 is observability, anytime after 2.

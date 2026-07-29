# Design — Speed Up Scan Preparation

Status: draft · Owner: hp · Related: [`requests.md`](requests.md),
[`profile-before.txt`](profile-before.txt) · Constitution: I (library-first),
III (test-first), VIII (SOLID) apply.

> **Primary design scenario: rich-mode input.** The 6-hour case is designed for
> and around **rich CSV** (execution-key grouping by `dst_network_segment`,
> `PRECHECK_ALLOW_ALL` segment expansion). Basic mode stays supported and
> benefits from the same fixes, but every decision below optimizes the rich path
> first. Phase 0 profiling (`profile-before.txt`) already **confirmed the rich
> execution-key merge is measurably quadratic** — that is the headline problem.

## TL;DR (慢的問題點)

以 rich mode 為主，前置準備慢的**核心元兇**是 **rich 的 execution-key 合併是
O(N²)**：每插入一個 target 就對整個 group 做一次線性掃描 + 兩邊 `TrimSpace` 比對
（`group_builder.go:268-296` → `mergeRichTargetIntoGroup:475-486` →
`richTargetIndexByExecutionKey:425-432`）。Phase 0 實測每次 host 翻倍時間倍率
2.46→2.92→3.14→3.72，逐步逼近 4，就是 quadratic 的指紋；用 `/20`(25ms) 外推，
單一 `/16` segment ≈ 6.4 秒、`/14` ≈ 28 分鐘，幾個大 segment 就能湊到 6 小時。

另外兩個問題會疊加放大它：

- **每次 resume 重展整份 input CSV**（`chunk_lifecycle.go:110-131`）：連不屬於這次
  pending chunk 的 segment 也全部展開再丟掉。rich 的一個 chunk = 一個
  `dst_network_segment` group，可能由多列 CSV 組成，所以要「只做當前 chunk」需要先
  用 segment 建索引、只處理貢獻該 segment 的列。
- **每個 IP 被 `net.ParseIP` 解析多次**（`pre_scan_ping.go:16-29`、
  `scan_helpers.go:44-50`、`sortRichGroups:490-501`）：profile 顯示 IP 解析是最大的
  單一 CPU 消耗。

**明確排除**：42,587 筆 `unreachable_ipv4_u32` 不是瓶頸（排序 `[]uint32` +
二分搜尋，沒出現在熱點）；JSON unmarshal 也不是（次秒級）。

## 1. Context & problem

`port-scan` runs `preping → generate-buckets → scan` (breaking change in #42).
`scan -resume <bucket.json>` must rebuild an in-memory runtime plan before dialing:
expand each segment into IPs, drop unreachable IPs, **group by
`dst_network_segment` and de-duplicate by `execution_key`**, attach ports. The
reported failure spent **6+ hours in this rebuild and never dialed a port**.

### Rich-mode grouping semantics (the thing the design must preserve)

`buildRichGroupsWithPredicate` (`group_builder.go:243-311`) is the rich path:

- **Group key = `dst_network_segment`** (`richCIDRKey`, falls back to CIDR/Net).
  One group == one segment == one resume chunk (`richChunkFromGroup:222-240`,
  `CIDR = segment`).
- **A group is assembled from potentially many CSV rows.** Each row yields targets
  via `richTargetsFromRecord`:
  - `Reason == PRECHECK_ALLOW_ALL` → expand the **whole segment** to host IPs, each
    getting `execution_key = BuildExecutionKey(ip, port, proto)`.
  - `MATCH_POLICY_ACCEPT` / default → a single `dst_ip` with the row's execution key.
- **De-dup by `execution_key` within a group**, merging metadata on collision
  (`mergeRichTargetIntoGroup` → `mergeRichTargetValues`).
- **Cross-segment first-claim ownership** (`ownerByExecutionKey`): the first
  segment to produce an execution key "owns" it; the same key produced later under
  a different segment is redirected into the owner's group. In **well-formed input
  this never fires** — `dst_network_segment`s are disjoint CIDRs, so their host
  expansions (and thus execution keys) are disjoint — but the design must not
  silently break it.

### Where the time goes (confirmed by Phase 0)

| # | Bottleneck | Location | Complexity | Rich-mode role |
|---|---|---|---|---|
| **B** | Per-group execution-key merge does a linear scan (+`TrimSpace` on both sides) for every inserted target | `mergeRichTargetIntoGroup:475-486`, `richTargetIndexByExecutionKey:425-432` | **O(N²) per group** | **primary** — only mechanism that reaches hours from one input; measured quadratic |
| A | Rebuilds groups from the **entire** input CSV every resume, keeps only the pending chunk segments | `chunk_lifecycle.go:110-131` (`buildRichGroupsWithPredicate(inputs.cidrRecords, …)`) | O(all input hosts) per resume | amplifier — pays for non-pending segments; 24.5× at 4000 vs 130 rows |
| C | Each IP parsed by `net.ParseIP` twice; `sortRichGroups` re-parses per comparison | `pre_scan_ping.go:16-29`, `scan_helpers.go:44-50`, `group_builder.go:490-501` | constant × on A/B | multiplier — largest single CPU frame in the profile |

**Explicitly ruled out** (do not "fix"): the 42,587-entry unreachable filter is a
sorted, de-duplicated `[]uint32` tested with `sort.Search` (`pre_scan_ping.go:31-45`),
and never appears as a hot frame; JSON unmarshal decodes in sub-second.

### Magnitude (Phase 0, `profile-before.txt`)

Rich `PRECHECK_ALLOW_ALL` single segment, time per host-count doubling:
`/24`→`/23` ×2.46, `/23`→`/22` ×2.92, `/22`→`/21` ×3.14, `/21`→`/20` ×3.72 → the
ratio climbs toward 4.0 == quadratic. Extrapolating from `/20` (25 ms): a `/16`
≈ 6.4 s, a `/14` ≈ 28 min for **one** segment; a handful of large segments = hours.

## 2. Goals / non-goals

**Goals**
1. Make rich-mode preparation linear: O(N²)→O(N) per-group merge, and expand only
   the **pending** segments (chunks), not the whole CSV.
2. Parse each IP once.
3. Progress accounted **per bucket/segment**, never filter × CIDR.
4. Ctrl+C: persist resume state **and** deliver every already-scanned result to the
   output file, so `-resume` continues cleanly.
5. `-resume` **appends** to the prior run's output file.
6. Phase progress logged (`start segment parse`, `segment i/N`, `parse complete`,
   `start scan`) while keeping the existing per-result scan progress.
7. Preserve rich grouping semantics exactly: segment grouping, execution-key
   de-dup + metadata merge, and cross-segment ownership behavior (or fail loudly).

**Non-goals** — scanner/dial core, rate limiting, ping logic; the #42 stage split;
distributed scanning.

## 3. Design

### 3.1 Kill the O(N²) rich merge (fixes B — the primary change)

Replace the per-group linear `richTargetIndexByExecutionKey` scan with a
**per-group `map[string]int`** (normalized execution key → index into the group's
target slice), maintained as targets are appended. Merge becomes O(1) amortized,
so a group of N targets builds in O(N) instead of O(N²).

- Introduce a build-time `richGroupBuilder{ targets []scanTarget; index map[string]int }`
  used only inside `buildRichGroupsWithPredicate`; finalize to the existing
  `cidrGroup` (drop the map) at the end. `cidrGroup`'s shape is unchanged, so
  downstream (`richChunkFromGroup`, runtime build) is untouched — SOLID: the change
  is internal to the builder.
- Normalize (`TrimSpace`) the execution key **once at ingest**, not on every compare.
- Cross-segment ownership still works: `ownerByExecutionKey` continues to pick the
  owning segment; the per-group map replaces only the intra-group *lookup*.
- Behavior-preserving: same targets, same de-dup/merge, same order (a group is
  sorted at the end regardless).

### 3.2 Expand only the pending segments (fixes A, rich-aware)

Today `buildRuntimeWithPredicate` builds groups over the *entire* `cidrRecords`,
then `chunk_lifecycle.go:126-131` keeps only `chunks[i].CIDR`. Because a rich group
spans multiple rows, "only the current CIDR" means **only the rows contributing to
each pending segment**:

- Build `segmentRows map[segment][]input.CIDRRecord` in one O(rows) pass — grouping
  rows by `richCIDRKey`, **no IP expansion**. Cheap.
- Drive the plan from the resume chunks: for each pending chunk (segment), expand +
  filter + execution-key-merge **only that segment's rows** (via §3.1's builder),
  attach ports, honor `NextIndex`. Non-pending segments are never expanded.
- Generate lazily one segment at a time so peak memory ≈ one segment's targets —
  important because rich segments can be huge.

**Cross-segment ownership under per-segment rebuild.** The global first-claim
resolution already happened at `generate-buckets` time and is baked into the chunk
boundaries; a faithful per-segment rebuild reproduces it **iff** segments are
disjoint (the well-formed case, where a key is only ever produced by one segment).
Design decision: rebuild per-segment for speed, and add a **guard** — during the
one-pass row indexing, if any execution key is produced by two different segments,
fail loudly with a "resume input is not segment-disjoint; start a fresh scan"
error rather than silently duplicating targets or mismatching `total_count`. The
differential golden test (§4) proves per-segment == whole-input build across
realistic rich shapes; the guard covers the malformed remainder.

### 3.3 Parse each IP once (fixes C)

- Carry the parsed `uint32` out of `ExpandIPSelectors` (or a thin wrapper) so the
  reachable predicate and the group sort reuse it. `reachablePredicate`
  (`pre_scan_ping.go:16-29`) takes a `uint32` and does only `sort.Search` — no
  `net.ParseIP`, no `.String()` alloc per IP.
- `sortRichGroups` (`group_builder.go:490-501`) sorts on the precomputed `uint32`
  key instead of calling `net.ParseIP` on both operands of every comparison. (Rich
  groups interleave rows, so a sort is still required — just not a re-parsing one.)

### 3.4 Resume file structure & validation (requirement 2, rich-focused)

Keep the envelope — already correct: `{ chunks:[{cidr(=segment), cidr_name, ports,
next_index, scanned_count, total_count, status}], pre_scan_ping:{…,
unreachable_ipv4_u32:[u32]} }`. `next_index` is the per-segment resume cursor the
streaming rebuild relies on; `unreachable_ipv4_u32` stays a sorted `[]uint32`
(42k ≈ 170 KB, binary-search-fast; a roaring bitmap was rejected as an unjustified
dependency).

Requirement 2 is met by a **differential + benchmark harness** (§4) over a
**rich-shape matrix**, not a format change:

- one large `PRECHECK_ALLOW_ALL` segment (drives B);
- many small segments, each one `MATCH_POLICY_ACCEPT` `dst_ip`;
- one segment assembled from **many rows** (execution-key de-dup + metadata merge);
- mixed reachable/unreachable within a segment;
- mid-`next_index` resume; legacy `[...]` chunk-array form (`state.go:82-126`);
- the segment-disjointness guard case (§3.2);
- a basic-mode fixture too, to prove the shared path didn't regress.

Each asserts (a) identical target set/order vs the current whole-input build and
(b) seconds-not-hours rebuild.

### 3.5 Progress counted per segment

`TotalCount` is already per-chunk (`richChunkFromGroup`: `len(targets)`;
basic: `len(targets)*len(ports)`), not filter × CIDR. Overall progress =
segments-completed / segment-total plus within-segment `scanned/total`, so no
global cross product is ever materialized.

### 3.6 Graceful Ctrl+C: persist state **and** deliver results (requirement 3)

- State-on-Ctrl+C — **already works**: on `context.Canceled`,
  `persistResumeSnapshot` rewrites the bucket with advanced cursors
  (`resume_manager.go:19`, `scan_helpers.go:28-33`, `scan.go:231`).
- Flush-pending — **broken**: rows go to `scan_results-<ts>.csv.tmp`, promoted to
  the final path only on `success == true` (`output_files.go:92-120`); on Ctrl+C
  `Finalize(false)` strands them. Fix: on graceful cancel, deliver already-written
  rows to the final path (rows are per-row flushed, `csv_writer.go:116`, so nothing
  buffered is lost); a genuine crash still does not promote, so a half-file is never
  mistaken for complete.

### 3.7 Resume appends to the prior output (requirement 4)

Record the run's output path in the resume snapshot on first scan start; on
`-resume`, reopen that path in `O_APPEND` instead of minting a new timestamped file
(`batch_output.go:16-43`). Write the CSV header only when creating a new/empty file;
skip on append (verify header logic in `csv_writer.go`). Composes with §3.6:
writing straight to the stable final path in append mode makes "promote on cancel"
moot. Test edge cases: prior output deleted (recreate with header), schema mismatch
(fail loudly), `-seq` collision.

### 3.8 Phase-progress logging (requirement 5)

No pre-scan phase logging exists (`input_loader.go`, `bucketgen.go`,
`runtime_builder.go` are silent). Add structured + human lines via `pkg/logx` /
`pkg/progress` (constitution VI): `segment_parse_start` (total segments),
`segment_parse_progress` (`segment i/N cidr=<segment>`, throttled by
`-progress-interval`), `segment_parse_complete` (segments, targets, elapsed),
`scan_start` (absent today). Keep `result_aggregator.go:52-94` unchanged.

## 4. Correctness & performance validation

1. **Phase 0 (done)** — `BenchmarkResumeRebuild` + CPU profile confirmed B is
   quadratic and C is the top CPU frame (`profile-before.txt`). The rich scaling
   sub-benchmarks stay as the regression guard for B.
2. **Differential correctness** — golden test asserting the new per-segment
   generator yields the exact `[]scanTarget` (ip, port, meta, order) of the current
   whole-input builder, across the §3.4 rich matrix. Safety net for the refactor
   (constitution III: behavior held, no assertion weakened).
3. **Benchmark deltas** — before/after `testing.B` for the rich scaling series
   (must go from quadratic to linear) and the basic fixtures, pasted as evidence.
4. **e2e** — touches pipeline + writers, so `make verify-e2e` must pass, including a
   rich scenario that Ctrl+C-interrupts and resumes, asserting one continuous,
   complete output file.

## 5. Risks & alignment

- **Rich semantics drift** — the highest-value risk. Mitigated by the differential
  golden test over the rich matrix *before* touching the hot path, and the
  segment-disjointness guard (§3.2).
- **Append/resume data integrity** — data-loss surface (R4/R5); gate on the e2e
  interrupt-and-resume plus missing-file / schema-mismatch tests.
- **Coverage floor (≥85%)** — ship tests in the same change; never weaken
  `scripts/coverage_gate.sh`.
- **Cross-model final review** → Codex, per repo routing.

See [`implementation.md`](implementation.md) for the phased, test-first plan.

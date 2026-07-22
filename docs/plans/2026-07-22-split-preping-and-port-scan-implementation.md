# Implementation Plan: Split Preping and Port Scan (Test-First)

- **Date:** 2026-07-22
- **Design:** `docs/plans/2026-07-22-split-preping-and-port-scan-design.md`
- **Request:** `docs/requests/split-preping-and-port-scan.md`
- **Method:** Test-first (constitution III, NON-NEGOTIABLE). Every ticket starts
  with a failing test, then minimal code to green, then `make verify`.

---

## How to use this plan

- Tickets are ordered by dependency (§ "Dependency graph"). Do them in order
  unless the graph shows they are independent.
- Each ticket has: **Goal · Files · Red test (write first) · Green (impl) ·
  Gate · Done-when**. Do not write production code before the red test exists
  and is observed failing.
- Before starting execution, honor the user's global rules:
  - **implementation-mode-selection** — pick sub-agent dispatch vs. team mode
    with the user first.
  - **subagent-provider-routing** — code-quality / final review goes to a
    cross-model reviewer (Codex); announce the switch.
- The whole feature is done only when `make verify` **and** `make verify-e2e`
  exit 0 (this touches the scan pipeline & writers) and the docs in T8 are
  updated.

## Key facts the tickets rely on (verified in code)

- **B1** — Fresh chunk building already exists: `loadOrBuildChunksWithPredicate`
  (`pkg/scanapp/chunk_lifecycle.go:57`), rich branch `buildRichChunksWithPredicate`
  (`chunk_lifecycle.go:185`), basic branch `buildCIDRGroupsWithPredicate`
  (`group_builder.go:207`). This is bucket-gen's core.
- **B2** — Scan re-derives groups + `total_count` in `buildRuntimeWithPredicate`
  (`chunk_lifecycle.go:103`) using `buildRichGroupsWithPredicate`
  (`group_builder.go:243`) / `buildCIDRGroupsWithPredicate`. **The invariant
  `bucket-gen total_count == scan expectedTotal` must hold** or scan errors at
  `chunk_lifecycle.go:151`. This is R1.
- **B3** — Snapshot IO: `state.SaveSnapshot(path, Snapshot)` (`state.go:60`),
  `state.LoadSnapshot(path)` (`state.go:82`); `Snapshot{Chunks, PreScanPing}`
  (`state.go:43`); `PreScanPingState{Enabled, TimeoutMS, UnreachableIPv4U32}`
  (`state.go:36`).
- **B4** — Preping internals: `runPreScanPing` (`pre_scan_ping.go:24`),
  `collectUniquePreScanIPs` (`pre_scan_ping.go:117`), `runReachabilityChecks`
  (`pre_scan_ping.go:142`), `finalizeUnreachableResults` (`scan.go:256`),
  `openUnreachableOutput` (`output_files.go:72`). Unreachable writer schema is
  fixed in `pkg/writer/unreachable_writer.go:29`.
- **B5** — Reachable predicate from a blocklist: `reachablePredicate(unreachable)`
  used at `pre_scan_ping.go:37`; blocklist type is `[]uint32`.
- **B6** — CLI: single `config.Parse(args)` flagset (`config.go:106`); dispatch
  in `cmd/port-scan/main.go:18`; handlers in `cmd/port-scan/command_handlers.go`.
- **B7** — `Run` (`scan.go:45`) is the monolith. Decision B removes the
  monolith, so `Run` is refactored into the **scan-only** path; preping and
  bucket-build move out into their own library entries.

## Package-home decision (pragmatic SOLID)

New orchestration entries live in **`pkg/scanapp`** as exported functions
(`RunPreping`, `GenerateBuckets`) reusing existing unexported helpers, rather
than new `pkg/preping` / `pkg/bucketgen` packages. Rationale: both reuse many
unexported scanapp helpers (group/chunk builders, unreachable writer wiring,
reachability); exporting them or duplicating into new packages costs more than
it buys today. Revisit an extraction only if `scanapp` shows god-package strain
(constitution VIII). The **progress** abstraction (T2) is small and dependency-
free, so it goes in its own `pkg/progress`.

---

## Dependency graph

```
T0 (config: per-subcommand flags)  ─┐
T1 (chunk-build invariant test)     ─┼─► T4 (generate-buckets) ─┐
T2 (progress reporter)  ─► T3 (preping) ─────────────────────── ┼─► T6 (CLI wiring) ─► T7 (integration + e2e) ─► T8 (docs)
T2 ────────────────────────────────► T4                         │
T5 (scan → scan-only) ──────────────────────────────────────────┘
```

T0, T1, T2, T5 are independent and can start in parallel. T3 needs T2. T4 needs
T1+T2. T6 needs T0+T3+T4+T5. T7 needs T6. T8 last.

---

## T0 — Per-subcommand flag parsing & validation

**Goal.** Register only the flags each subcommand owns, enforce per-command
required flags, and add the new flags. This is what makes "`scan` has no ping
flags" real (design §6) rather than cosmetic.

**Files.**
- `pkg/config/config.go` — split `Parse` into per-command registration, or add
  `ParseFor(command string, args []string)`. Add fields: `BucketsOut`,
  `UnreachableFile`, `ProgressInterval`.
- `pkg/config/config_test.go` (or new `parse_test.go`).

**Red test (write first).**
- `TestParseFor_Preping_RejectsPingScanFlags` — `-disable-pre-scan-ping` and
  `-port-file` are **not** registered for `preping` → parse error.
- `TestParseFor_GenerateBuckets_RequiresBucketsOut` — missing `-buckets-out`
  errors; `-unreachable-file` optional.
- `TestParseFor_Scan_RequiresResume` — missing `-resume` errors; ping flags not
  registered.
- `TestParseFor_ProgressInterval_Default` — default carried from the current
  `progressStep` default (100) and overridable.
- `TestParseFor_Scan_RejectsUnknownPingFlag` — passing `-pre-scan-ping-timeout`
  to `scan` is an unknown-flag error.

**Green.** Introduce command-scoped `flag.FlagSet` builders (one per command)
sharing a common core (input, workers, log/format/quiet, column names). Keep the
existing `config.Parse` for `validate` behavior or route it through `ParseFor`.

**Gate.** `make verify`. **Done-when.** New tests green; existing config tests
still green; coverage ≥85%.

---

## T1 — Lock the chunk-build ↔ scan `total_count` invariant (R1)

**Goal.** Prove that chunks produced for a bucket file carry the exact
`total_count` scan re-derives, so a generated snapshot never trips
`chunk_lifecycle.go:151`. No production change expected — this test is a
regression lock before T4 depends on it.

**Files.**
- `pkg/scanapp/chunk_lifecycle_invariant_test.go` (new).

**Red test (write first).**
- `TestChunkBuild_TotalCountMatchesRuntimeExpected_Rich` — for a rich record set
  + blocklist: build chunks via `loadOrBuildChunksWithPredicate` (fresh), then
  feed them to `buildRuntimeWithPredicate` with the **same** records+predicate;
  assert no error and each runtime's `TotalCount` equals the chunk's.
- `TestChunkBuild_TotalCountMatchesRuntimeExpected_Basic` — same for basic mode
  with `-port-file` ports.
- Include a blocklist that removes some IPs, and the broadcast-exclusion case
  (see `d03296d` / `labs/broadcast-exclusion/`) so the counts account for
  excluded addresses identically on both sides.

**Green.** If the test fails, unify both paths on one grouping function (rich:
have `buildRichChunksWithPredicate` and `buildRuntimeWithPredicate` both call
`buildRichGroupsWithPredicate`; basic likewise) so counting is single-sourced.
Otherwise, no code change — the test just locks current parity.

**Gate.** `make verify` (esp. `-race`, `-shuffle=on`). **Done-when.** Invariant
tests green; if a unification refactor was needed, no behavior-change to existing
tests.

---

## T2 — Progress reporter abstraction

**Goal.** A testable, non-hard-coded progress emitter (design §8) shared by
preping and bucket-gen.

**Files.**
- `pkg/progress/progress.go` (new) — `Reporter` interface + a stderr impl.
- `pkg/progress/progress_test.go`.

**Red test (write first).**
- `TestReporter_EmitsAtInterval` — with interval N, advancing the counter emits
  a line every N units and not between.
- `TestReporter_FinalSummaryAlwaysEmitted` — `Done()` emits a final
  `X/X (100.0%)` line even if the last tick didn't align to the interval.
- `TestReporter_FormatsPercent` — `4200/10000` → `(42.0%)`.
- `TestReporter_ThreadSafe` — concurrent `Inc()` from multiple goroutines under
  `-race` is clean (bucket-gen needs this).

**Green.** Interface:
```go
type Reporter interface { Inc(); Add(n int); Done() }
```
Constructor takes `(label string, total int, interval int, w io.Writer)`;
mutex-guarded counter; writes structured lines to stderr. Interval comes from
`-progress-interval` (T0). No cadence constant hard-coded in callers.

**Gate.** `make verify`. **Done-when.** All T2 tests green incl. the race test.

---

## T3 — `preping` library entry (reachability → unreachable CSV + progress)

**Goal.** An exported `scanapp.RunPreping` that pings unique targets, writes the
existing `unreachable_results-<ts>.csv`, reports progress, and prints the
resolved path — without any chunk/scan logic (design §5.2).

**Files.**
- `pkg/scanapp/preping.go` (new; wraps `runPreScanPing` internals from B4).
- `pkg/scanapp/preping_test.go`.

**Red test (write first).**
- `TestRunPreping_WritesUnreachableCSVWithExistingSchema` — inject a fake
  `ReachabilityChecker` marking some IPs unreachable; assert the output file has
  the exact 12-col header (`unreachable_writer.go:29`) and one row per
  unreachable IP with `status=unreachable`, `reason="ping failed within <t>"`.
- `TestRunPreping_ReportsProgressOverUniqueIPs` — a spy `progress.Reporter`
  receives increments totaling the unique-IP count and a final `Done()`.
- `TestRunPreping_PrintsResolvedPath` — resolved timestamped path written to
  stdout for chaining.
- `TestRunPreping_NoTargets_WritesEmptyValidCSV` — header-only file, no error.

**Green.** Compose existing pieces: `loadRunInputs` → `collectUniquePreScanIPs`
→ `runReachabilityChecks` (workers, timeout from cfg) with a `progress.Reporter`
tick per checked IP → `collectUnreachableRows` → `finalizeUnreachableResults`
into an `-output`-derived path (reuse `resolveRunOutputPaths` /
`batch_output.go` naming). Inject the checker for tests.

**Gate.** `make verify`. **Done-when.** Tests green; unreachable schema
byte-identical to today.

---

## T4 — `generate-buckets` library entry (subtraction, parallel, deterministic)

**Goal.** Exported `scanapp.GenerateBuckets` that reads targets + optional
blocklist, builds chunks over `targets − blocklist` in parallel, stamps
`pre_scan_ping.enabled=true`, and writes a resume `Snapshot` to `-buckets-out`
(design §5.2, §5.3, §9).

**Files.**
- `pkg/scanapp/bucketgen.go` (new).
- `pkg/scanapp/bucketgen_test.go`.
- Blocklist parse helper: `parseUnreachableBlocklist(path) ([]uint32, error)`
  reading the `ip` column (B3/B5), tolerant of a missing path (→ empty).

**Red test (write first).**
- `TestGenerateBuckets_SubtractsBlocklist` — targets minus the `ip`-column set;
  chunks cover only reachable targets; `total_count` matches.
- `TestGenerateBuckets_NoBlocklist_ScansAll` — missing `-unreachable-file` →
  all targets, empty blocklist, `pre_scan_ping.enabled=true`.
- `TestGenerateBuckets_StampsEnabledTrue` — even with zero unreachable,
  `Snapshot.PreScanPing.Enabled == true` (guarantees scan never pings, design
  Q7).
- `TestGenerateBuckets_Deterministic_AcrossWorkers` — `-workers 1` and
  `-workers 8` produce **byte-identical** serialized snapshots (CIDR-sorted).
- `TestGenerateBuckets_RaceFree` — run under `-race` with high worker count.
- `TestGenerateBuckets_ReportsProgressOverGroups` — spy reporter counts groups.
- `TestGenerateBuckets_SnapshotAcceptedByRuntime` — feed the produced
  `Snapshot.Chunks` into `buildRuntimeWithPredicate` with the same records →
  no `total_count` mismatch (ties to T1's invariant).
- `TestParseUnreachableBlocklist_IPColumnToU32` and `_MissingFileIsEmpty`.

**Green.**
1. Parse records (`loadRunInputs`) and ports; parse blocklist (helper).
2. Build the reachable predicate from the blocklist (B5).
3. Group by CIDR (existing builders), then **fan out** per-group chunk building
   over a `-workers` pool writing into a pre-sized result slice by index
   (race-free); tick the reporter per completed group.
4. Sort chunks by CIDR; assemble `Snapshot{Chunks, PreScanPing{Enabled:true,
   TimeoutMS: <from flag or 0>, UnreachableIPv4U32: blocklist}}`.
5. `state.SaveSnapshot(cfg.BucketsOut, snap)`.

Reuse the T1-locked builder so counting is single-sourced.

**Gate.** `make verify` incl. `-race`. **Done-when.** All T4 tests green;
determinism + race tests pass.

---

## T5 — Decompose `scan` into pure scanning (no checker)

**Goal.** Refactor `Run` (B7) so `scan` requires a bucket file via `-resume`,
constructs **no** reachability checker, and does not ping or build fresh chunks
(design §5.2, structural guarantee).

**Files.**
- `pkg/scanapp/scan.go` — refactor `Run`.
- `pkg/scanapp/scan_scanonly_test.go` (new); adjust existing `Run` tests that
  assumed inline preping.

**Red test (write first).**
- `TestRun_RequiresResume` — `cfg.Resume == ""` → clear error (no fresh build).
- `TestRun_NeverConstructsChecker` — with a bucket file, a checker factory spy
  is **never** invoked (guarantee-by-construction; `resolveReachabilityChecker`
  returns nil on this path).
- `TestRun_ProducesEnrichedRowsFromRichCSVAndSnapshot` — given rich.csv +
  a generated snapshot, result rows carry `service_label`, `decision`,
  `matched_policy_id`, etc. (F2), and reachable predicate comes from the
  snapshot blocklist.
- `TestRun_DoesNotWriteUnreachableCSV` — scan no longer emits
  `unreachable_results-*.csv` (that is preping's artifact).

**Green.** In `Run`:
- Require `cfg.Resume`; load snapshot.
- Build `reachable := reachablePredicate(snapshot.PreScanPing.UnreachableIPv4U32)`
  (B5); **do not** call `runPreScanPing`, `resolveReachabilityChecker`, or
  `finalizeUnreachableResults`.
- `prepareRuntimePlan(..., reachable, snapshot.Chunks, useResumeChunks=true)`.
- Keep the dispatch/executor/result/persist tail unchanged.
- Remove the now-dead monolith branches (fresh-build, preping) from `Run`.

**Gate.** `make verify` **and** `make verify-e2e` (pipeline change).
**Done-when.** Scan-only tests green; no checker path reachable; existing
dispatch/pressure tests still green.

---

## T6 — CLI wiring for the three subcommands

**Goal.** Expose `preping`, `generate-buckets`, `scan` in `cmd/port-scan`
(design §6), each parsing its own flags (T0) and calling its library entry.

**Files.**
- `cmd/port-scan/main.go` — add cases `preping`, `generate-buckets`.
- `cmd/port-scan/command_handlers.go` — `handlePrepingCommand`,
  `handleGenerateBucketsCommand`; update `runScan` to `ParseFor("scan", ...)`;
  update `usage`.
- `cmd/port-scan/*_test.go` — drive `runMain` end-to-end per command.

**Red test (write first).**
- `TestRunMain_Preping_WritesUnreachable` — `runMain(["preping", ...])` returns
  0 and writes the CSV.
- `TestRunMain_GenerateBuckets_WritesSnapshot` — returns 0 and writes
  `-buckets-out`; missing `-buckets-out` → exit 2.
- `TestRunMain_Scan_RequiresResume` — `scan` without `-resume` → exit 2.
- `TestRunMain_Scan_RejectsPingFlags` — `scan -disable-pre-scan-ping` → exit 2
  (unknown flag).
- `TestRunMain_UnknownCommand` unchanged; `usage` lists all three.

**Green.** Thin handlers: parse → build options → call `RunPreping` /
`GenerateBuckets` / `Run`; map errors to exit codes (reuse `runScan`'s
SIGINT/cancel handling). Pressure-fetcher wiring stays only in `scan`.

**Gate.** `make verify`. **Done-when.** All command tests green; `usage` updated.

---

## T7 — Integration + e2e for the three-step pipeline

**Goal.** Prove the artifact hand-offs end to end (constitution IV/V).

**Files.**
- `pkg/scanapp/pipeline_integration_test.go` (new) — in-process chain.
- `e2e/` — extend the compose scenario + `e2e/run_e2e.sh` to run the three
  steps in sequence; artifacts to `e2e/out/`.

**Red test (write first).**
- `TestPipeline_PrepingToBucketsToScan` — run `RunPreping` → feed its
  unreachable CSV to `GenerateBuckets` → feed the snapshot to `Run`; assert:
  final rows exclude unreachable IPs, carry rich metadata, and open/closed
  states are correct against a fake dialer.
- `TestPipeline_NoPreping_ScansAll` — skip preping; `GenerateBuckets` with no
  blocklist → scan covers all targets, never pings.
- `TestPipeline_TamperedTotalCount_Rejected` — mutate a chunk `total_count` →
  scan errors (F2 guard).
- e2e: `preping` → `generate-buckets` → `scan` against mock services; assert
  open-port detection, closed/timeout handling, and report artifacts exist.

**Green.** Wire the e2e script steps; add fixtures. No new production logic
expected beyond glue.

**Gate.** `make verify` **and** `make verify-e2e` exit 0.
**Done-when.** Integration + e2e green; artifacts in `e2e/out/`.

---

## T8 — Docs, release notes, version bump

**Goal.** Keep docs in lockstep (constitution II/VII, `.claude/rules/documents.md`).

**Files / actions.**
- `docs/release-notes/<new-major>.md` — new three-step flow, flag relocation
  table (design §6), and the copy-paste migration (design §10). MAJOR bump.
- `docs/cli/*` + `usage` string — three subcommands.
- `docs/specs/SPEC-01-CLI-LAYER.md`, `SPEC-06-SCAN-ORCHESTRATION.md` — reflect
  decomposition + bucket-gen boundary.
- `README` — three-step examples.
- `docs/architecture/*` — pipeline diagram update.
- `.claude/rules/50-lessons.md` — append any real failure hit during
  implementation (format per `40-maintenance-protocol.md`).

**Gate.** Re-read edited docs; verify every command shown actually runs.
**Done-when.** Docs match behavior; release notes contain gate evidence.

---

## Definition of Done (whole feature)

- [ ] `make verify` exits 0 — paste the final result line + coverage total.
- [ ] `make verify-e2e` exits 0 (pipeline/writer change).
- [ ] Every new production behavior started with a red test (T0–T7).
- [ ] No gate/threshold/test weakened; coverage ≥85% held by same-package tests.
- [ ] Cross-platform intact (`filepath`, `t.TempDir()`, no hardcoded paths);
      preping's Windows ping classification preserved (R3 / `50-lessons.md`).
- [ ] Cross-model final review completed (Codex) per subagent-provider-routing.
- [ ] Docs + release notes updated (T8); MAJOR version bumped.

## Risks carried from design (watch during execution)

- **R1 — `total_count` parity.** T1 locks it; T4/T5/T7 depend on it. Never let
  bucket-gen and scan count independently.
- **R2 — coverage floor.** New `cmd/` + code can dip total <85%; land tests with
  code, check `go tool cover -func=coverage.out | tail -1`.
- **R3 — Windows ping fragility.** Preserve `runCtx.Err()` classification and
  `pingProcessStartupAllowance` when extracting preping; keep reachability tests
  green.
- **R4 — timestamped hand-off filename.** Capture preping's printed path for
  chaining; documented in the migration snippet.

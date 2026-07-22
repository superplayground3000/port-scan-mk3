# Design: Split Preping and Port Scan into Three Independent Steps

- **Date:** 2026-07-22
- **Request:** `docs/requests/split-preping-and-port-scan.md`
- **Status:** Design (approved for implementation planning)
- **Constitution impact:** Breaking CLI change to `scan` → **MAJOR** version bump (VII); requires release notes with migration guidance (II).

---

## 1. Problem

Under large inputs, two phases dominate wall-clock time and produce long,
silent stretches where the operator cannot tell what is happening:

1. **Preping** — reachability-pinging every unique target IP.
2. **Bucket conversion** — turning ping results + input into the per-CIDR
   chunk set the scanner consumes.

Today all of this runs inline inside a single `scanapp.Run` call (the `scan`
subcommand): preping → (inline) CIDR-group→chunk build → scan + resume-persist.
There is no way to run, observe, checkpoint, or parallelize the two expensive
pre-phases independently of the scan itself.

**Goal:** split the pipeline into three independent, separately-runnable steps
— `preping`, `generate-buckets`, `scan` — each with visible progress, so a long
run can be staged, observed, and resumed at well-defined boundaries.

## 2. Goals and non-goals

### Goals
- Three independent CLI steps with durable hand-off artifacts between them.
- Each pre-phase (`preping`, `generate-buckets`) reports percentage progress.
- `generate-buckets` uses parallel processing to accelerate the conversion.
- Existing input/output CSV schemas are unchanged.
- All existing flags for the affected flow are preserved (relocated to the step
  they belong to); necessary new flags are added.
- No tunable parameter is hard-coded — timeout, worker count, and progress
  cadence are all flags.

### Non-goals
- No change to the scan/reachability *algorithms* themselves.
- No new input or output CSV schema.
- No convenience "run all three at once" wrapper — the split intentionally
  removes the monolithic path (see §4, decision Q2). Chaining is the caller's
  job (shell/pipeline).
- No change to the pressure-control, rate-limiter, or writer internals beyond
  wiring them to the `scan` subcommand.

## 3. Current architecture (verified)

| Concern | Where | Notes |
|---|---|---|
| Orchestration | `pkg/scanapp/scan.go:45` (`Run`) | Monolith: preping → chunk build → scan → resume-persist |
| Preping | `pkg/scanapp/pre_scan_ping.go:24` (`runPreScanPing`) | Shells `ping`; gated by `-disable-pre-scan-ping` |
| Reachability check | `pkg/scanapp/reachability.go:19,27` | `ReachabilityChecker` iface; `commandReachabilityChecker` default |
| Unreachable CSV | `pkg/writer/unreachable_writer.go:29` | Fixed 12-col header; file `unreachable_results-<ts>.csv` |
| CIDR grouping | `pkg/scanapp/group_builder.go` | Basic keys on `ip_cidr`; rich keys on `dst_network_segment` |
| Chunk build | `pkg/scanapp/chunk_lifecycle.go:57,103` | Inline; one `task.Chunk` per CIDR; recomputed each run |
| Resume snapshot | `pkg/state/state.go:43` (`Snapshot`) | JSON `{chunks:[]task.Chunk, pre_scan_ping}` |
| Resume flag | `pkg/config/config.go:132` (`-resume`) | Reads/writes the snapshot |
| Progress | `pkg/scanapp/dashboard_*` + `progressStep` | Full-screen TTY dashboard, or periodic log lines |

Two facts that constrain the design:

- **F1 — Resume already carries the blocklist and, when present, skips
  pinging.** `runPreScanPing` returns *before invoking the checker* when
  `hasSavedPreScanPingState(saved)` is true (`pre_scan_ping.go:35`), where that
  predicate is `saved.Enabled || saved.TimeoutMS != 0 ||
  len(saved.UnreachableIPv4U32) > 0` (`pre_scan_ping.go:97`).
- **F2 — Resume rebuilds all per-target metadata from the input CSV.**
  `buildRuntimeWithPredicate` re-derives CIDR groups from `cidrRecords`
  (rich.csv) even on resume (`chunk_lifecycle.go:105`); saved chunks supply only
  ports + progress/status. It then asserts `ch.TotalCount == len(targets) *
  len(ports)` and errors on mismatch (`chunk_lifecycle.go:151`).

## 4. Decisions

Each decision below was resolved with the requester. The label (Q#) matches the
clarification session.

| # | Decision | Choice |
|---|---|---|
| Q1 | Command surface | **Subcommands** under `cmd/port-scan`: `preping`, `generate-buckets`, `scan` |
| Q2 | Fate of `scan` | **Decompose** to pure scanning; requires a bucket file. Breaking change; no monolith retained |
| Q3 | Bucket file format | **Reuse the resume `Snapshot` JSON**; write via `-buckets-out`; read via existing `-resume` |
| Q4 | Reachable derivation | **Subtraction**: rich.csv targets − IPs in `unreachable.csv` `ip` column (as uint32) |
| Q5 | Progress mechanism | **Periodic stderr percentage lines**; cadence via `-progress-interval` |
| Q6 | Bucket-gen parallelism | **Fan-out per-CIDR-group** over a `-workers` pool; sequential parse; deterministic CIDR-sorted output |
| Q7 | Preping optionality | **Optional**; `-unreachable-file` optional to bucket-gen; bucket-gen always stamps `enabled=true` |
| Q8 | `scan` ping guarantee | **Structural**: no reachability checker wired into `scan`; "never pings" holds by construction |
| Q8 | Flag distribution | As §6; **reuse `-output`** and **`-workers`** names across steps |

## 5. Target architecture

### 5.1 Data-flow

```
             rich.csv
                │
                ▼
        ┌───────────────┐
        │    preping    │   ping every unique reachable IP
        └───────┬───────┘
                │  unreachable_results-<ts>.csv   (existing schema, unchanged)
                ▼
  rich.csv + unreachable.csv (optional)
                │
                ▼
        ┌───────────────────┐
        │  generate-buckets │   subtract blocklist, group per CIDR,
        │   (parallel)      │   compute chunks, stamp pre_scan_ping
        └─────────┬─────────┘
                  │  bucket snapshot JSON   (== resume Snapshot; -buckets-out)
                  ▼
     rich.csv + bucket snapshot
                  │
                  ▼
        ┌───────────────┐
        │     scan      │   pure TCP scan; NO checker; -resume <bucket>
        └───────┬───────┘
                │  scan_results-<ts>.csv / opened_results-<ts>.csv (unchanged)
                ▼
```

Each arrow is a **durable file** — the pipeline can stop and resume at any
boundary. rich.csv is threaded into all three steps because it is the single
source of truth for target metadata (F2).

### 5.2 Step contracts

**`preping`** — reachability probe.
- *Inputs:* rich.csv (`-cidr-file`), reachability checker (platform `ping`).
- *Output:* `unreachable_results-<ts>.csv` (existing writer/schema/naming,
  derived from `-output`); resolved path printed to stdout for chaining.
- *Progress:* percentage over unique IP count, to stderr, every
  `-progress-interval` units.
- *Does not:* build chunks or a snapshot.

**`generate-buckets`** — bucket/chunk builder.
- *Inputs:* rich.csv (`-cidr-file`), ports (`-port-file`), optional blocklist
  (`-unreachable-file`).
- *Output:* bucket snapshot JSON (`-buckets-out`) — a resume `Snapshot` with all
  chunks `status="pending"`, `next_index=0`, correct `total_count`, and
  `pre_scan_ping.enabled=true` (+ blocklist if a file was supplied).
- *Progress:* percentage over CIDR-group count, to stderr.
- *Parallelism:* per-CIDR-group fan-out over `-workers`; deterministic
  CIDR-sorted serialization.

**`scan`** — pure scanner.
- *Inputs:* rich.csv (`-cidr-file`, for metadata via F2) + bucket snapshot
  (`-resume`, now **required**).
- *Output:* `scan_results-<ts>.csv` / `opened_results-<ts>.csv` (unchanged);
  updates the resume snapshot in place on interrupt/error.
- *No reachability checker is constructed* (`resolveReachabilityChecker`
  returns nil for this path) → pinging is impossible by construction, not merely
  skipped by a flag.

### 5.3 Snapshot contract produced by `generate-buckets`

`generate-buckets` must produce a snapshot that `scan` accepts under F2:

- One `task.Chunk` per CIDR group over the **reachable** target set
  (`rich.csv − unreachable`).
- `Chunk.Ports` populated from `-port-file` (rich mode: the per-target port),
  so `scan` does not fall back to defaults.
- `Chunk.TotalCount = len(reachableTargets) * len(ports)` computed with the
  **same grouping/predicate logic** `scan` uses, so F2's equality assertion
  passes. This is the primary correctness invariant of the whole feature.
- `Chunk.NextIndex = 0`, `Chunk.Status = "pending"`.
- `pre_scan_ping.enabled = true` always (Q7); `unreachable_ipv4_u32` = parsed
  blocklist (empty when no `-unreachable-file`); `timeout_ms` carried through if
  known, else 0 (irrelevant to `scan` since it has no checker).

## 6. CLI surface

New flags marked **NEW**. All others are existing flags relocated to their step.

### `port-scan preping`
`-cidr-file`(req) · `-cidr-ip-col` · `-cidr-ip-cidr-col` ·
`-pre-scan-ping-timeout` · `-workers` · `-output` · **NEW** `-progress-interval`
· `-log-level` · `-format` · `-quiet`
- Drops `-disable-pre-scan-ping` (skip pinging by not running this step).
- No `-port-file` (ping is per-IP).

### `port-scan generate-buckets`
`-cidr-file`(req) · `-port-file` · `-cidr-ip-col` · `-cidr-ip-cidr-col` ·
`-workers` · **NEW** `-unreachable-file`(optional) · **NEW** `-buckets-out`(req)
· **NEW** `-progress-interval` · `-log-level` · `-format` · `-quiet`

### `port-scan scan`
`-cidr-file`(req) · `-cidr-ip-col` · `-cidr-ip-cidr-col` ·
`-resume`(**now required**) · `-output` · `-timeout` · `-workers` ·
`-bucket-rate` · `-bucket-capacity` · `-delay` · all pressure-API flags
(`-disable-api`, `-pressure-*`) · `-progress-interval` · `-log-level` ·
`-format` · `-quiet`
- **No ping flags.**
- `-port-file` retained as a fallback but normally ignored (chunks carry ports).

Flag-name reuse (Q8): `-output` and `-workers` keep the same names across steps;
a user rarely runs two steps concurrently, and one vocabulary is clearer.

## 7. Package / SOLID boundaries (library-first, constitution I & VIII)

Behavior lands in `pkg/` before CLI wiring; each subcommand is thin composition.

- **Preping** logic already lives in `pkg/scanapp` (`runPreScanPing`,
  `collectUniquePreScanIPs`, `runReachabilityChecks`). Extract a reusable entry
  that runs preping and writes the unreachable CSV **without** the rest of
  `Run`. Candidate home: keep in `pkg/scanapp` or a focused `pkg/preping`
  facade — decided at implementation time; must not depend on scan/dispatch.
- **Bucket generation** is new reusable logic (candidate `pkg/bucketgen`) that
  depends on `pkg/input` (parse), `pkg/scanapp` group/chunk builders (or a
  shared extraction of them), `pkg/state` (serialize). It must **not** depend on
  the scanner, writers, or pressure control. It exposes a deterministic API:
  `(records, blocklist, ports, workers, progress) -> state.Snapshot`.
  - The group→chunk builder currently lives inside `pkg/scanapp`
    (`chunk_lifecycle.go`). To satisfy the "compute `total_count` exactly as
    scan does" invariant (§5.3) without duplicating logic, extract the
    group-build + chunk-build into a shared, dependency-narrow function used by
    **both** `generate-buckets` and `scan`'s resume path. This is the key SOLID
    refactor: one owner for "records + predicate + ports → chunks."
- **Scan** reuses `scanapp.Run`'s dispatch/executor/result/persist portion, with
  the preping and inline-chunk-build phases removed from its path and the
  checker set to nil.

## 8. Progress reporting design

- A small `ProgressReporter` abstraction (interface, injected) so the emit
  cadence and sink are testable and not hard-coded. Cadence = `-progress-interval`
  (count-based, matching the existing `progressStep` semantics; default carried
  from today's `opts.ProgressInterval`).
- `preping`: denominator = unique IP count (known up front from
  `collectUniquePreScanIPs`).
- `generate-buckets`: denominator = CIDR-group count; increment as each group's
  chunk is completed by a worker (atomic counter; the reporter is the only
  shared writer and is mutex-guarded per the concurrency standard).
- Emitted to **stderr** as structured lines (constitution VI), e.g.
  `preping: 4200/10000 (42.0%)`, with a final summary line.
- Works identically under a TTY or a pipe (no full-screen dashboard for these
  steps). `scan` keeps its existing dashboard/log behavior unchanged.

## 9. Parallelism design (generate-buckets)

- Sequential single-pass parse of rich.csv (I/O-bound; sharding rejected as Q6).
- Group by CIDR (existing `buildRichGroups` / `buildCIDRGroups`), producing N
  independent groups.
- Fan out group→chunk building across a `-workers` pool. Each worker writes only
  its own result slot (no shared mutable chunk state) → race-free.
- Join, **sort chunks by CIDR**, then serialize once (single goroutine) →
  deterministic output regardless of completion order.
- Verified under `-race -shuffle=on` (constitution quality gate); a dedicated
  race test asserts concurrent group building is clean.

## 10. Backward compatibility & migration

- **Breaking:** `port-scan scan` no longer accepts raw input for an end-to-end
  run; `-resume <bucket file>` is required and it no longer pings or builds
  buckets. `-disable-pre-scan-ping` and `-pre-scan-ping-timeout` are removed from
  `scan`.
- **Version:** MAJOR bump (constitution VII).
- **Release notes:** `docs/release-notes/<new-version>.md` documenting the new
  three-step flow, the flag relocation table (§6), and a copy-paste migration:
  ```
  # before
  port-scan scan -cidr-file rich.csv -port-file ports.txt -output out/

  # after
  port-scan preping          -cidr-file rich.csv -output out/
  port-scan generate-buckets -cidr-file rich.csv -port-file ports.txt \
                             -unreachable-file out/unreachable_results-<ts>.csv \
                             -buckets-out out/buckets.json
  port-scan scan             -cidr-file rich.csv -resume out/buckets.json -output out/
  ```
- **Unchanged:** all CSV schemas (`writer.Columns`, unreachable header), file
  naming, resume JSON envelope, rich/basic auto-detection.
- **Snapshot fidelity:** a snapshot produced by `generate-buckets` is byte-shape
  identical to one produced by an interrupted legacy run, so no reader change is
  needed in `pkg/state`.

## 11. Testing strategy (test-first, constitution III/IV/V)

Each behavior change starts red.

**Unit (`pkg/`):**
- Bucket-gen subtraction: `rich.csv − unreachable` yields expected reachable set;
  IP parsed from `ip` column to uint32 correctly.
- **Invariant test:** `total_count` from `generate-buckets` equals what
  `scan`'s resume path re-derives (drives them through the shared extracted
  builder; guards F2).
- Empty / missing `-unreachable-file` → snapshot over all targets,
  `enabled=true`, empty blocklist; `scan` accepts and never pings.
- Deterministic output: same inputs → identical serialized snapshot regardless
  of `-workers`.
- Race test: concurrent group building under `-race`.
- Progress reporter: emits at the configured cadence with correct
  numerator/denominator; final summary emitted.
- `scan` with nil checker: no `ping` invocation path reachable (structural).

**Integration (contract boundaries, constitution IV):**
- preping CSV → generate-buckets → scan, asserting the artifact hand-offs and
  that enriched result rows carry rich metadata (service_label, decision, …).
- `total_count` mismatch (tampered snapshot) → scan rejects with the existing
  error.

**E2E (constitution V, Docker-isolated):**
- Extend `e2e/` to run the three steps in sequence against the mock services;
  assert open/closed/timeout detection and report artifacts in `e2e/out/`.
- Run `make verify-e2e` (this feature touches the scan pipeline & writers).

**Gates:** `make verify` (fmt, vet, build, `-race` tests, ≥85% coverage) exit 0;
`make verify-e2e` exit 0. Add tests in the same package as any new production
code to hold the coverage floor.

## 12. Documentation to update (constitution II, `.claude/rules/documents.md`)

- `docs/release-notes/<version>.md` (new; migration guide).
- `docs/cli/*` and any usage strings (`command_handlers.go:83`) for the three
  subcommands.
- `docs/specs/SPEC-01-CLI-LAYER.md` and `SPEC-06-SCAN-ORCHESTRATION.md` to
  reflect the decomposition and the new bucket-gen boundary.
- `README` examples showing the three-step flow.
- `docs/architecture/*` diagram: the three-step pipeline.

## 13. Open risks

- **R1 — the `total_count` invariant.** If `generate-buckets` and `scan`
  diverge in how they count reachable targets × ports, every scan of a generated
  bucket fails F2's assertion. Mitigation: the shared extracted builder (§7) and
  the dedicated invariant test (§11) — do not let the two paths compute it
  independently.
- **R2 — coverage floor.** New `cmd/` wiring and a new package can drag total
  coverage below 85%. Mitigation: land tests with code; check
  `go tool cover -func` before claiming done.
- **R3 — Windows ping fragility.** preping inherits the platform ping quirks
  already logged (`50-lessons.md`, 2026-07-21/22). Extracting preping must
  preserve the `runCtx.Err()`-based classification and `pingProcessStartupAllowance`;
  keep the existing reachability tests green.
- **R4 — timestamped hand-off filename.** `unreachable_results-<ts>.csv` is
  non-deterministic, so chaining requires capturing preping's printed path.
  Accepted (keeps output format unchanged per the request); documented in the
  migration snippet.

## 14. Implementation sequencing (for the follow-up plan)

1. Extract the shared "records + predicate + ports → chunks" builder from
   `chunk_lifecycle.go` (refactor, no behavior change; tests prove parity).
2. `pkg` bucket-gen API + tests (subtraction, invariant, determinism, race).
3. `preping` extraction + progress reporter + tests.
4. Wire the three subcommands in `cmd/port-scan`; remove ping/inline-build from
   `scan`'s path; nil checker.
5. Integration + e2e; docs + release notes; `make verify` / `make verify-e2e`.

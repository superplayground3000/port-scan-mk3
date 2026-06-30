# Pre-Scan Ping Design

## Summary

Add a default-on pre-scan ping phase before TCP task planning and dispatch.
If an IP is unreachable by ping, the scan must skip all TCP tasks for that IP and
record the skipped IP in a separate batch output file. Users can disable this
behavior with `-disable-pre-scan-ping`.

## Goals

- Avoid wasting TCP scan work on IPs that are already unreachable by ping.
- Preserve current CLI behavior when pre-scan ping is disabled.
- Keep scan, opened-only, and unreachable outputs as separate files.
- Keep resume behavior consistent across interrupted and resumed runs.

## Non-Goals

- No user-configurable ping timeout in this change. (Superseded 2026-06-30: the timeout is now configurable via `-pre-scan-ping-timeout`.)
- No ICMP implementation inside Go.
- No change to existing `scan_results-*.csv` or `opened_results-*.csv` schemas.
- No per-port unreachable output rows.

## User-Facing Behavior

### CLI

- Add `-disable-pre-scan-ping` to `port-scan scan`.
- Default behavior: pre-scan ping is enabled.
- When `-disable-pre-scan-ping=true`, the program behaves like the current
  implementation and does not perform pre-ping filtering.

### Ping Timeout

- Ping timeout defaulted to a fixed 100ms in v1; as of the 2026-06-30 design it is configurable
  via the `-pre-scan-ping-timeout` flag (default `100ms`). See
  [2026-06-30-configurable-pre-scan-ping-timeout-design.md](2026-06-30-configurable-pre-scan-ping-timeout-design.md).

### Output Files

- Keep the current timestamped batch output naming strategy.
- Continue writing:
  - `scan_results-<suffix>.csv`
  - `opened_results-<suffix>.csv`
- Add:
  - `unreachable_results-<suffix>.csv`
- All three files must share the same suffix so a single run can be correlated.
- `unreachable_results-<suffix>.csv` must be fully flushed and finalized before
  the TCP scanning phase starts.
- TCP task planning, dispatch, and execution must not begin until unreachable
  output is durably written for the pre-scan ping phase.

### Unreachable Output Contract

Write one row per skipped IP within its scan context, not one row per port.

Proposed columns:

- `ip`
- `ip_cidr`
- `status`
- `reason`
- `fab_name`
- `cidr_name`
- `service_label`
- `decision`
- `matched_policy_id`
- `execution_key`
- `src_ip`
- `src_network_segment`

Field rules:

- `status` is always `unreachable`.
- `reason` reflects the configured pre-scan ping timeout, e.g. `ping failed within 100ms` (or `ping failed within 300ms` when `-pre-scan-ping-timeout=300ms`). Originally fixed at `100ms` in v1; see [2026-06-30-configurable-pre-scan-ping-timeout-design.md](2026-06-30-configurable-pre-scan-ping-timeout-design.md).
- When multiple rich-input targets for the same IP are collapsed into one
  unreachable row, metadata fields use the existing merge behavior (`|`-joined
  distinct values).

## Design Options

### Option A: Pre-ping Before Runtime Planning

Run ping checks after input loading and before scan plan construction. Remove
unreachable IPs from the runtime targets, then build chunks and runtimes only
for reachable IPs.

Pros:

- Matches the meaning of "pre-scan ping"
- Keeps dispatcher and executor focused on TCP work
- Makes progress counts reflect only real TCP tasks

Cons:

- Requires resume state to remember ping decisions

### Option B: Ping During Dispatch

Check reachability when each IP is about to enqueue TCP tasks.

Pros:

- Smaller impact on stored plan data

Cons:

- Not truly pre-scan
- Makes dispatcher responsible for filtering and output side effects
- Progress totals still include work that is later skipped

### Option C: Convert Ping Failure Into Normal Scan Rows

Do not add a separate file. Emit pseudo scan rows with a special status.

Pros:

- Lowest implementation effort

Cons:

- Breaks the requested file separation
- Pollutes the meaning of scan output rows

## Chosen Approach

Choose Option A.

This keeps the pipeline boundaries clean:

- input loading
- pre-scan reachability filtering
- runtime planning
- TCP task dispatch and execution
- batch output finalization

The extra resume work is justified because it preserves deterministic behavior
across interrupted runs.

## Architecture

### New Reachability Abstraction

Add a small consumer-owned interface in `pkg/scanapp`:

```go
type ReachabilityChecker interface {
    Check(ctx context.Context, ip string, timeout time.Duration) ReachabilityResult
}
```

And a result type:

```go
type ReachabilityResult struct {
    IP          string
    Reachable   bool
    FailureText string
}
```

`RunOptions` gains an injectable checker for tests. Production uses a default
checker that shells out to the system `ping` command with platform-specific
arguments and a context deadline.

This keeps the CLI thin and allows tests to avoid invoking real ICMP tooling.

### Windows Compatibility

The feature must work on Windows as well as Unix-like environments.

Design requirements:

- Keep OS-specific `ping` command construction out of `cmd/port-scan`.
- Use a small platform adapter in `pkg/scanapp` or a closely related package.
- Build the command with OS-specific arguments:
  - Windows: one echo request with Windows-compatible timeout arguments
  - Unix-like: one echo request with Unix-compatible count arguments
- Enforce the `100ms` timeout with `context.WithTimeout` around command
  execution so the high-level contract remains consistent even when CLI flags of
  the underlying `ping` binary differ by platform.
- Tests must cover command construction or injected checker behavior for
  Windows and non-Windows code paths without requiring real ICMP access in CI.

### Pre-Scan Filtering Phase

Insert a new phase in `scanapp.Run()` after `loadRunInputs()` and before
`prepareRunPlan()`.

Responsibilities:

1. Extract unique target IPs from loaded records.
2. Run reachability checks once per unique IP using a bounded worker pool.
3. Build an in-memory ping decision set.
4. Write unreachable rows into the new batch output.
5. Flush and finalize `unreachable_results-<suffix>.csv`.
6. Filter unreachable IPs out of group/runtime construction.
7. Start TCP runtime planning and scanning only after step 5 succeeds.

The filtering applies before chunk totals are finalized so `TotalCount`,
dashboard totals, and resume state remain aligned with actual TCP work.

The worker pool is required for scale. With a fixed `100ms` timeout, sequential
ping is not acceptable when the expanded input can reach hundreds of thousands
of unique IPv4 targets.

This phase is a hard barrier. TCP scanning is a later stage, not an interleaved
continuation of pre-ping work.

### Runtime and Group Building

`buildCIDRGroups()` and `buildRichGroups()` must accept only reachable targets.
The implementation should avoid baking ping logic into group builders. Instead,
the pre-scan phase should prepare filtered inputs or a reusable predicate that
group construction consumes.

For rich mode:

- Multiple targets may share the same destination IP but differ by port or
  metadata.
- All targets for an unreachable IP are dropped from runtime planning.
- A single unreachable row is written per IP per scan context, with merged
  metadata values.

### Output Writers

Batch output path allocation must happen before pre-scan ping so all outputs can
share the same timestamp suffix.

Writer lifecycle is split into two stages:

1. Pre-scan stage:
   - open unreachable temp file
   - write unreachable rows
   - flush, close, and rename to final `unreachable_results-<suffix>.csv`
2. TCP scan stage:
   - open scan/opened temp files using the already allocated suffix
   - run TCP scanning
   - finalize the scan/opened outputs on successful completion

The new unreachable writer should be isolated from the existing writer contract
to avoid breaking callers that only understand scan result rows.

If unreachable output finalization fails, TCP scanning must not start.

### Resume Format

Current resume state only stores `[]task.Chunk`, which is insufficient once
chunks depend on pre-ping filtering.

Introduce a backward-compatible state envelope:

```json
{
  "chunks": [...],
  "pre_scan_ping": {
    "enabled": true,
    "timeout_ms": 100,
    "unreachable_ipv4_u32": [167772167]
  }
}
```

Representation rules:

- `unreachable_ipv4_u32` stores IPv4 addresses as unsigned 32-bit integers in
  ascending order.
- Resume-time membership checks should use binary search over the sorted slice.
- The implementation should avoid rebuilding a `map[string]struct{}` unless a
  later benchmark proves it is necessary.

Compatibility rules:

- Old `[]task.Chunk` resumes must still load successfully.
- New resume files persist both chunk state and pre-ping decisions.
- Resume runs must reuse the saved unreachable IP list instead of re-running
  ping and potentially changing the task graph mid-resume.
- The saved format is optimized for large IPv4 sets and is expected to remain
  practical when expanded input size approaches `300,000` unique IPs.

## Error Handling

- If pre-scan ping is enabled and the `ping` command is missing or fails at the
  tool level, abort the scan with an error.
- If a specific IP fails the reachability check, treat that IP as unreachable
  and continue.
- If writing or finalizing `unreachable_results-<suffix>.csv` fails, abort the
  run before TCP planning or dispatch starts.
- If all candidate IPs are unreachable, produce a successful run with:
  - `scan_results-*.csv` containing only the header
  - `opened_results-*.csv` containing only the header
  - `unreachable_results-*.csv` containing the skipped IP rows

## Testing Strategy

### Unit Tests

- Config parsing defaults pre-scan ping to enabled.
- `-disable-pre-scan-ping` disables the feature.
- Pre-scan filtering removes unreachable IPs before runtime totals are built.
- Rich-mode metadata is merged correctly in unreachable output.
- Resume save/load supports both legacy chunk arrays and the new envelope.
- Resume save/load preserves sorted `unreachable_ipv4_u32` data.
- Batch output finalization renames the unreachable file with the same suffix.
- Pre-scan reachability work is deduplicated per IP and executed with bounded
  concurrency.
- TCP scanning does not start until unreachable output has been flushed and
  finalized.
- Windows and non-Windows ping command paths are both covered by tests or by an
  injected checker abstraction.

### Integration-Level Tests

- A reachable IP still produces normal TCP scan rows.
- An unreachable IP produces no TCP dial attempts.
- Mixed reachable/unreachable inputs produce all three output files.
- Resume after interruption reuses saved pre-ping decisions.
- Disabled mode preserves the current behavior exactly.
- Windows runs can complete the pre-scan ping stage and transition into TCP
  scanning using the same high-level behavior contract.

## Implementation Notes

- Keep `cmd/port-scan` limited to flag parsing and `RunOptions` assembly.
- Put ping execution, result mapping, filtering, and unreachable writing in
  `pkg/scanapp` and supporting `pkg/` packages.
- Prefer focused types over adding more responsibility to existing scan result
  writers.
- Keep pre-scan output finalization as an explicit stage boundary, not an
  incidental side effect inside the TCP scan loop.

## Open Questions Resolved

- Pre-scan ping is default-on.
- Users disable it with `-disable-pre-scan-ping`.
- Ping timeout defaulted to `100ms` in v1 and is now configurable via `-pre-scan-ping-timeout` (default `100ms`); see [2026-06-30-configurable-pre-scan-ping-timeout-design.md](2026-06-30-configurable-pre-scan-ping-timeout-design.md).
- Unreachable output uses the timestamped batch naming convention instead of a
  fixed `unreachable.csv` filename.
- `unreachable_results` must be fully written before TCP scanning begins.
- The implementation must support Windows.

# Port-scan performance harness

The performance harness records repeatable evidence for large `port-scan` data.
The complete matrix records scale-conformance results for issue 151.

## Module design

The private `internal/perfharness` module owns one interface named `Harness`.
This interface covers fixture generation, metrics, evaluation, normalization, and reports.
The issue 150 contract requires this single evidence-run boundary.
Production packages do not use this interface.

The module calls the production preparation, snapshot, resume, scan, and writer paths.
The fake-probe adapter uses the existing `scanapp.RunOptions.Dial` seam.
Production packages do not import the performance harness.

The harness also observes dispatcher order, resume progress, and result progress.
`scanapp.RunOptions` owns these narrow callbacks because `scanapp` produces the events.
The callbacks do not contain harness types or threshold rules.

The Linux shell and Windows PowerShell scripts are OS adapters.
They start the process, collect OS metrics, prepare paths, and remove specific temporary files.
The Go module owns all threshold formulas.
Build-tagged process samplers record OS metrics for each case.
The scripts also keep raw metrics for the complete matrix process.

## Matrix profiles

The `full` profile defines these fixture families:

- `record-heavy`
- `candidate-heavy`
- `port-heavy`
- `task-heavy`
- `output-heavy`
- `snapshot-heavy`
- `resume-heavy`
- `rich-record-mixed`
- `rich-unique-key`
- `rich-hot-key`
- `rich-precheck`
- `rich-deny`

Snapshot fixtures use 1 MB, 10 MB, 100 MB, and 1 GB shapes.
Each snapshot fixture produces separate load and save results.
The load result measures only production decoding.
The save result measures serialization, write, sync, close, and replacement.
The harness reloads the saved snapshot after the measured operation.
This reload is a correctness check and is not part of the save result.
Resume fixtures use 0%, 50%, and 99% progress.
The contract also records LF and CRLF input variants.

The bounded `smoke` profile uses 100,000 records and a 100 MB snapshot.
Linux and Windows CI run this profile.
The worker-memory threshold applies at this scale and at larger scales.

Cancellation cases use 200 items, and failure-control cases use 100 items.
This fixed size makes the 1%, 50%, and 99% injection points deterministic.
The control size is not a production limit.

The matrix executes 15 cancellation cases.
It covers five stages at 1%, 50%, and 99% progress.
Each scan cancellation must leave resumable work.
New work must stop within one second.

The matrix executes bounded production cases for every rich-input family.
The denied cases must make zero reachability checks and zero TCP probes.
The accepted cases run pre-ping, snapshot generation, resume, scan, and both writers.

The matrix executes resume cases at 0%, 50%, and 99% completion.
It also executes output, snapshot-save, and pressure fatal failures.

The output matrix uses all-open results to write both result files. The full
profile uses 10,000, 100,000, 1,000,000, and 10,000,000 results.

Each scale uses flush intervals `1`, `1000`, and `0`. Each output case runs
five times and removes its temporary CSV files after measurement.

The matrix executes six cases for each target expansion and data resource limit.
These cases cover the exact default, default plus one, a positive override,
`0`, a negative value, and overflow. The estimator does not allocate targets.

The CIDR loader uses 1 MB, 10 MB, 100 MB, and 1 GB fixtures.
The snapshot loader and saver use the same four sizes.
Each save fixture uses its production serialized size for the selected scale.
The full matrix checks default rejection and positive-override completion for rich input larger than 1 GB.

The production fake-probe cases use 1, 16, and 256 workers.
The native loopback cases use 1 and 32 workers.
All network activity in these cases stays in `127.0.0.0/8`.

## Commands

Run the complete native Linux matrix:

```bash
make verify-performance
```

Run the bounded Linux smoke profile:

```bash
bash scripts/performance_gate.sh smoke /tmp/port-scan-performance-smoke
```

Run the bounded Windows smoke profile:

```powershell
./scripts/performance_gate.ps1 -Profile smoke -OutputDir "$env:TEMP\performance smoke"
```

Compare portable Linux and Windows reports:

```bash
go run ./internal/perfharness/cmd/perf-harness \
  -compare-left linux/performance-report.json \
  -compare-right windows/performance-report.json
```

Use a new output path for each run.
The scripts stop if the selected output path exists.

The full Linux script requires 50 GB of free space.
The smoke script requires 2 GB of free space.
The scripts make this check before fixture generation.

Set `PERF_MINIMUM_PROFILE_CERTIFIED=1` only on an equivalent constrained host.
Otherwise, the report uses the `hardware-qualified` evidence label.

## Report schema

Each run writes these files:

- `performance-report.json`
- `performance-report.md`
- A raw OS-metrics file from the native adapter.
- A raw native interrupt-test log.
- One retained fixture and manifest for each fixture case.

The OS adapters save raw logs before they return a failed matrix status.

The JSON schema version is `1`.
Each fixture manifest records the seed, counts, bytes, digest, family, and shape.
Artifact names are relative, so deterministic manifests do not contain host roots.

Each fixture or workflow case starts with one cold observation. Five more
observations produce its steady-state median.

Snapshot load and save operations have independent observations and verdicts.
Their manifests identify the artifact that each operation reads or writes.

Each output case has five observations. Its report uses the first observation
as the cold value and calculates the median from all five observations.

Fixture generation time is separate from production-stage time.

The portable observation has these metrics:

- Input and output bytes.
- Wall time and throughput.
- Output MB/s.
- Go allocated bytes and allocation count.
- Go peak heap.
- Linux peak RSS or Windows peak working set.
- Peak committed memory.
- Swap or pagefile bytes and I/O.

Linux and Windows do not expose per-process paging byte counters through these adapters.
The paging byte fields are `0` when the OS does not supply these counters.
The raw matrix evidence records available page faults and swap counts.

The report records the CPU, core counts, power mode, RAM, filesystem, and disk.
It also records free space, Go version, commit, constraints, and evidence label.

## Threshold rules

The Go module evaluates absolute runtime and memory budgets.
It also evaluates growth, regression, and worker-memory budgets.

The runner checks both the cold observation and the steady median.
The absolute values come from the approved issue 149 contract.
The report stores these values with the matrix contract.

A ten-fold input increase permits at most 12.5-fold time growth.
Allocated bytes permit at most 11-fold growth.
A benchmark change permits at most a 10% increase in `ns/op` or `B/op`.
The 256-worker case permits at most 25% more memory than the 16-worker case.

A ten-fold output increase permits at most 12.5-fold time growth. At the two
largest scales, interval `1000` must be twice as fast as interval `1`.

At those scales, interval `1000` cannot be more than 15% slower than interval `0`.

An OOM, panic, corruption, missing result, or incomplete case fails immediately.
Paging is permitted, and its time stays in the wall-time result.

## Normalization and parity

Semantic comparison normalizes only declared volatile fields.
These fields are roots, path separators, timestamps, durations, and OS error details.

The harness does not normalize task order, row count, status, cursor, or output digest.
Byte parity applies only to deterministic artifacts.

Each worker run records dispatcher task order and a normalized result digest.
The runner compares all six runs and the 1, 16, and 256 worker profiles.
The report comparison command applies the same rules across operating systems.

## Windows release checklist

Windows CI runs the 100,000-record and 100 MB smoke profile.
Cross-build results are not native runtime evidence.

Before release, record the complete Windows matrix in the Native Windows tracker.
That manual record must include the commit, environment, raw metrics, reports, and failure logs.

The Native Windows tracker must also cover these items:

- Ctrl+Break delivery.
- A second interrupt and exit code `130`.

## Accepted Linux report

The complete Linux matrix passed for commit `3ad301eacb6f574a83b6839919186dc41a81aac7`.
It ran 149 cases on the recorded hardware and used no swap.

The [issue 151 evidence](../.agent-evidence/issue-151.md#final-full-linux-matrix) records the result and the raw report paths.
The report includes all four mixed snapshot sizes and all 100 MB snapshot shapes.
- Complete large-data finalization.

Do not label an unconstrained result as `minimum-profile certified`.
Use `hardware-qualified` for results from the recorded host.

# Port-scan performance harness

The performance harness records repeatable evidence for large `port-scan` data.
The harness does not certify the scale targets of issue 151.

## Module design

The private `internal/perfharness` module owns one interface named `Harness`.
This interface covers fixture generation, metrics, evaluation, normalization, and reports.
The issue 150 contract requires this single evidence-run boundary.
Production packages do not use this interface.

The module calls the production `scanapp.GenerateBuckets` and `scanapp.Run` functions.
The fake-probe adapter uses the existing `scanapp.RunOptions.Dial` seam.
Production packages do not import the performance harness.

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
Resume fixtures use 0%, 50%, and 99% progress.
The contract also records LF and CRLF input variants.

The bounded `smoke` profile uses 100,000 records and a 100 MB snapshot.
Linux and Windows CI run this profile.
The worker-memory threshold applies at this scale and at larger scales.

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
- One retained fixture and manifest for each fixture case.

The JSON schema version is `1`.
Each fixture manifest records the seed, counts, bytes, digest, family, and shape.
Artifact names are relative, so deterministic manifests do not contain host roots.

Each case starts with one cold observation.
Five more observations produce the steady-state median.
Fixture generation time is separate from production-stage time.

The portable observation has these metrics:

- Input and output bytes.
- Wall time and throughput.
- Go allocated bytes and allocation count.
- Go peak heap.
- Linux peak RSS or Windows peak working set.
- Peak committed memory.
- Swap or pagefile bytes and I/O.

On Windows, the I/O counters include process file I/O and paging I/O.

The report records the CPU, core counts, power mode, RAM, filesystem, and disk.
It also records free space, Go version, commit, constraints, and evidence label.

## Threshold rules

The Go module evaluates absolute runtime and memory budgets.
It also evaluates growth, regression, and worker-memory budgets.

A ten-fold input increase permits at most 12.5-fold time growth.
Allocated bytes permit at most 11-fold growth.
A benchmark change permits at most a 10% increase in `ns/op` or `B/op`.
The 256-worker case permits at most 25% more memory than the 16-worker case.

An OOM, panic, corruption, missing result, or incomplete case fails immediately.
Paging is permitted, and its time stays in the wall-time result.

## Normalization and parity

Semantic comparison normalizes only declared volatile fields.
These fields are roots, path separators, timestamps, durations, and OS error details.

The harness does not normalize task order, row count, status, cursor, or output digest.
Byte parity applies only to deterministic artifacts.

## Windows release checklist

Windows CI runs the 100,000-record and 100 MB smoke profile.
Cross-build results are not native runtime evidence.

Before release, record the complete Windows matrix in the Native Windows tracker.
That manual record must include the commit, environment, raw metrics, reports, and failure logs.

The Native Windows tracker must also cover these items:

- Ctrl+Break delivery.
- A second interrupt and exit code `130`.
- Complete large-data finalization.

Do not label an unconstrained result as `minimum-profile certified`.
Use `hardware-qualified` for results from the recorded host.

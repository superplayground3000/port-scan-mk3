# Issue 151 evidence

## Baseline failures

The retained issue 146 matrix used commit `e8b9ce7`.
Its 1 GB CIDR case reached 10,345,607,168 bytes of committed memory.
The contract permits 8,000,000,000 bytes.

The same report combined snapshot load, save, and reload in one observation.
It also saved 1,777,777,758 bytes from a 1,000,000,007-byte fixture.
That observation reached 7,863,914,496 bytes of committed memory.
It did not measure the approved load and save operations separately.

## Snapshot measurement RED and GREEN

The first focused test failed to compile:

```text
perfharness.New().RunSnapshotCases undefined
```

The new harness seam emits `snapshot-load` and `snapshot-save` results.
The 100 KB focused test passed in `0.065s`.
The save output was between 100,000 and 101,000 bytes.

A dirty diagnostic smoke run completed in `1:16.88` with exit code `0`.
The report metadata names base commit `0ec8bd5`, but the worktree contained changes.
Therefore this run is diagnostic evidence only.

The first separate 100 MB diagnostic results were:

| Operation | Median time | Allocated bytes | Peak heap | Committed memory |
| --- | ---: | ---: | ---: | ---: |
| Load | `968719185 ns` | `1146767880` | `463470592` | `560291840` |
| Save | `368199674 ns` | `297138240` | `293281792` | `344334336` |

The 10 MB to 100 MB growth ratios were:

| Operation | Time | Allocated bytes |
| --- | ---: | ---: |
| Load | `10.3746x` | `9.6317x` |
| Save | `11.8922x` | `8.6114x` |

These early ratios passed. The later complete matrix found failures at other scales.
The final implementation and results are in the sections below.

## CIDR loader RED and GREEN

The public file loader test used 100,000 basic records.
The first run failed:

```text
peak heap increase = 95469568 bytes, want at most 85000000
```

The loader now counts rows with a streaming pass.
It then rewinds the file and allocates the exact domain-record capacity.
The parser does not retain a complete `[][]string` value.

Three green peak-heap results were:

```text
83812352
83812352
83795968
```

The file loader rejects a changed record count between the count and parse passes.
The focused race tests passed in `1.013s`.

The six-run before and after benchmark medians were:

| Metric | Before | After | Change |
| --- | ---: | ---: | ---: |
| Time | `91747743 ns/op` | `89666299 ns/op` | `-2.27%` |
| Allocated bytes | `105279864 B/op` | `101915400 B/op` | `-3.20%` |
| Allocations | `2300320 allocs/op` | `2400321 allocs/op` | `+4.35%` |

No benchmark metric regressed by more than 10 percent.
Raw benchmarks and memory profiles are in `/media/hp/secondary/issue151-profiles/`.

## Final CIDR results

The final CIDR loader uses a compact table for duplicate detection.
The table stores an 8-byte hash and row index in each slot.
It compares the complete semantic key after a hash match.
Therefore, a hash collision cannot hide a duplicate.

The 100,000-row heap test has a 70,000,000-byte limit.
Three final runs used `69,328,896`, `69,337,088`, and `69,337,088` bytes.
An earlier test increase to 75,000,000 bytes was invalid and was reverted.

The 1,000,000-row benchmark changed as follows:

| Metric | Before | Final | Change |
| --- | ---: | ---: | ---: |
| Median time | `1.0197s` | `0.6619s` | `-35.1%` |
| Allocated bytes | `1,043,963,648` | `792,794,000` | `-24.1%` |
| Allocations | `24,004,344` | `12,000,100` | `-50.0%` |

An index-sort prototype took `2.616s` and was rejected.
The raw rejected result is `cidr-100mb-index-sort.txt` in the profile directory.

## Final snapshot load and save results

The loader reads a regular file into one exact-size byte slice.
It rejects a file that grows or shrinks after the size check.
It also preallocates only the unreachable-IP slice from a safe size hint.

The schema scan rejects unknown object fields before `json.Unmarshal`.
The standard decoder still controls JSON syntax, numbers, null values, and duplicate fields.

The saver writes indented JSON through a 256 KB buffer.
It streams the large arrays and preserves the old `json.MarshalIndent` bytes.
The size limiter stops after `limit + 1` bytes.
A zero size limit disables this limit.

The final allocation results were:

| Operation | 1 MB | 10 MB | 100 MB | 1 GB |
| --- | ---: | ---: | ---: | ---: |
| Load | `1,681,368` | `16,672,968` | `166,684,776` | `1,666,680,936` |
| Save | `270,752` | `270,752` | `270,848` | `270,848` |

The load growth ratios were `9.916x`, `9.998x`, and `9.998x`.
The save allocation did not grow with the serialized data size.

Two snapshot prototypes were rejected:

- Per-value `json.Decoder` was too slow.
- `json.RawMessage` batching used `251.3ms` and `145.9MB` at 10 MB.

## Final Linux matrix

The final report uses commit `d800a3fdb34ee19bade8259abe2e100f2e01c456`.
It is hardware-qualified evidence, not minimum-profile certification.

The host had these properties:

- Linux AMD64 and Go `1.24.4`.
- AMD RYZEN AI MAX+ 395 CPU, with 16 physical and 32 logical cores.
- `131,891,437,568` bytes of RAM.
- WD_BLACK SN7100 2TB storage.
- The recorded power mode was `powersave`.

The matrix completed 152 cases and 900 measured runs.
All 152 verdicts passed.
The wall time was `23:12.12`.
The process maximum RSS was `18,347,828` KB.
It recorded 8 major page faults, 67,142,513 minor page faults, and no swaps.

Important results were:

| Case | Steady time | Steady committed memory | Contract |
| --- | ---: | ---: | ---: |
| CIDR 1 GB | `7.909s` | `7,222,046,720` | `< 300s`, `< 8GB` |
| Snapshot load 1 GB | `8.150s` | `1,794,101,248` | `< 120s`, `< 6GB` |
| Snapshot save 1 GB | `1.690s` | `502,669,312` | `< 120s`, `< 6GB` |

The 10-million-result output case without periodic flushes reached `555.82 MB/s`.
The slowest cancellation observation stopped in `218,736,808ns`.
The contract limit is `1,000,000,000ns`.

All 152 cases recorded expected-value correctness.
Seventy-eight cases recorded deterministic digest correctness.
Eighty-six cases included a semantic artifact for report comparison.

The final artifacts are:

- `/media/hp/secondary/issue151-performance-d800a3f/report/performance-report.json`
- `/media/hp/secondary/issue151-performance-d800a3f/report/performance-report.md`
- `/media/hp/secondary/issue151-performance-d800a3f/matrix-os-metrics.txt`

## Quality gates and Windows status

`GOTOOLCHAIN=go1.24.4 make verify` passed at the final production commit.
The coverage gate reported `85.0%`.

`COMPOSE_PROJECT_NAME=issue151_scale_d800a3f make verify-e2e` passed.
It removed all Docker resources after the test.

The Windows cross-build passed for all five commands.
All Windows test packages also compiled with `CGO_ENABLED=0 GOOS=windows GOARCH=amd64`.
A synthetic Windows copy of the Linux report passed the report comparison.
This comparison validates the comparison path only. It is not Native Windows evidence.

The complete Native Windows matrix is still pending in issue 99.

## Scale-conformance routing correction

The report at commit `d800a3f` did not execute all required production routes.
It is valid only for the production operations that it measured directly.
It is not the final full-matrix result for issue 151.

The corrected harness separates preparation from scan orchestration.
Preparation writes one compact snapshot with exactly 10,000,000 scan tasks.
The measured stage calls `scanapp.Run` with a fake dialer and both CSV writers.
The stage uses a 4,000,000,000-byte committed-memory limit.

The compact fixture has 10,000,008 raw candidate addresses.
Eight boundary broadcast addresses are removed before scan dispatch.
The harness sets the minimum explicit limits of 10,000,008 candidates and 17 GB.
The default limits reject this raw candidate count.
A candidate limit of 10,000,007 and a memory limit of 16 GB also reject it.

## Scan-orchestration diagnostic results

These one-run diagnostics used production commit `5bd943b`.
A temporary build-tag driver was present, so each result is marked as diagnostic.
The driver called the existing production measurement seam one time.
It did not duplicate the measurement formula.

| Workers | Stage time | Peak committed | Peak heap | Allocated bytes | 4 GB result |
| ---: | ---: | ---: | ---: | ---: | --- |
| 1 | `91.756s` | `1,197,219,840` | `1,161,199,616` | `6,467,695,400` | Pass |
| 16 | `125.510s` | `1,024,598,016` | `955,932,672` | `6,468,708,136` | Pass |
| 256 | `119.831s` | `1,005,658,112` | `953,401,344` | `6,469,603,968` | Pass |

The 256-worker committed-memory result was `0.9815x` the 16-worker result.
The contract limit is `1.25x`.
All runs used zero swap.

Each run produced exactly 10,000,000 probes and 10,000,000 rows in both files.
Each run recorded the same ordered task digest, `2a4ed5b629a305a020f41327cf506689fd81026e109be374f1994818258ce8fb`.
Each run recorded the same normalized output digest, `bb33ef38e0639d13db0a0b1973a36be1fee289470027e1c63fb257529ca488f7`.
Raw CSV byte digests differ because worker completion order can differ.

The retained diagnostic files are in these directories:

- `/media/hp/secondary/issue151-performance-5bd943b-diagnostics/worker-1`
- `/media/hp/secondary/issue151-performance-5bd943b-diagnostics/worker-16`
- `/media/hp/secondary/issue151-performance-5bd943b-diagnostics/worker-256`

The current implementation reduced the worker-1 stage peak from `6,529,245,184` bytes to `1,197,219,840` bytes.
It reduced the peak by 81.7 percent without changing task or normalized-result digests.

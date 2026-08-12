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

The separate 100 MB results were:

| Operation | Median time | Allocated bytes | Peak heap | Committed memory |
| --- | ---: | ---: | ---: | ---: |
| Load | `968719185 ns` | `1146767880` | `463470592` | `560291840` |
| Save | `368199674 ns` | `297138240` | `293281792` | `344334336` |

The 10 MB to 100 MB growth ratios were:

| Operation | Time | Allocated bytes |
| --- | ---: | ---: |
| Load | `10.3746x` | `9.6317x` |
| Save | `11.8922x` | `8.6114x` |

All ratios are within the `12.5x` time limit and `11x` allocation limit.
The evidence did not justify a production snapshot change before the exact 1 GB run.

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

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

## Resume diagnostic results

These one-run diagnostics used production commit `fab8b9c`.
A temporary build-tag driver was present, so each result is marked as diagnostic.
The driver called `RunResumeSmoke` one time for each completion percentage.

The measured stage included the production resume rebuild, fake probes, and both CSV writers.
Fixture preparation was outside the measured stage.
The harness did not measure fixture preparation, so these results make no preparation-performance claim.

| Completed | Remaining tasks | Stage time | Peak committed | Peak heap | Allocated bytes | 24 GB result |
| ---: | ---: | ---: | ---: | ---: | ---: | --- |
| 99% | `100,000` | `18.150s` | `17,247,334,400` | `16,949,387,264` | `18,344,933,744` | Pass |
| 50% | `5,000,000` | `65.462s` | `18,202,533,888` | `17,880,793,088` | `20,783,604,416` | Pass |
| 0% | `10,000,000` | `131.043s` | `18,093,965,312` | `17,762,443,264` | `23,265,465,128` | Pass |

All three stages used zero swap and zero paging input or output.
Each run ended with status `completed` and cursor `10,000,000`.
The probe, task, scan-row, and open-row counts matched the remaining task count.

| Completed | Task digest | Normalized output digest |
| ---: | --- | --- |
| 99% | `d05ddc124eae4fc74bff0088ec0eb81b4d084a4c3f21a8a7b41f6401b6fa0637` | `8b78f54b22e938596ca2baafd108c8241c7a0c7e82261be96065e1814e5870e7` |
| 50% | `b53711ea4e6180fc87d323c42c6abbf9ea9994546b9926f3cf179d1d98a85519` | `644aef3aa385cf2bf8f7ffb0427ed8b32dca40bd6be1d8e38a5735655f38f0d2` |
| 0% | `075122422d95cbe3683a6d55fced180fbcaada9a6a845d3139b5c83165cf894a` | `cd92280828500597c28b88c2312b2ae21c0b852d298331f1032a77baa3cf1f1c` |

The retained diagnostics are in `/media/hp/secondary/issue151-performance-fab8b9c-diagnostics/`.
The `resume-99`, `resume-50`, and `resume-0` directories contain the reports and output files.

## Cancellation evidence correction

The earlier cancellation report stored only a Boolean resumable value.
It did not record the injection threshold, progress unit, probe starts, or recovery parity.

The first focused test failed to compile with these representative errors:

```text
CancellationResult.TotalItems undefined
CancellationResult.InjectionThreshold undefined
CancellationResult.CompletedAtInjection undefined
CancellationResult.ProbeStartsAfterCancel undefined
CancellationResult.Recovery undefined
CaseResult.Cancellation undefined
```

The harness now records six runs for each cancellation case.
Each run has separate preparation and stage observations.
It also records the total items, threshold, progress unit, error class, probe starts, and recovery evidence.

The scan runtime reports probe starts under the executor lock.
This interface proves that no probe starts after the executor enters the stop state.
The result-output and resume-rebuild cases run a production recovery scan.
The recovery task digest must match the remaining snapshot task digest.

The focused race command passed:

```text
ok  github.com/xuxiping/port-scan-mk3/pkg/scanapp  2.878s
ok  github.com/xuxiping/port-scan-mk3/internal/perfharness  2.556s
ok  github.com/xuxiping/port-scan-mk3/internal/perfharness/cmd/perf-harness  8.124s
```

## Cancellation runtime scaling

One runtime exists for each incomplete chunk.
The old builder created a rate limiter for each runtime, even when its initial capacity covered all remaining work.

The regression test first failed with this output:

```text
one remaining task allocated a rate limiter even though the initial capacity already covers it
```

The builder now omits the rate limiter when the initial capacity covers the remaining tasks.
The dispatcher preserves the same wait and acquire events for this immediate path.
Chunks that need refill still use the existing rate limiter.

The first 10-million-item diagnostic stopped before the cancellation stage.
The 2 GB default snapshot limit rejected a 2,233,986,251-byte snapshot.
The harness now uses the approved zero byte-limit bypass for this matrix only.
The default production limit did not change.

The next diagnostic reached cancellation and recovery.
It failed because the old stop measurement included the durable snapshot save.
The issue contract applies one second to new CPU, queue, and wait work.
It does not apply this limit to durable finalization.

The report now records two times:

- `stop_duration_ns` ends when the executor drain is complete.
- `finalization_duration_ns` ends after snapshot persistence and the production call returns.

The exact 10-million-item `result-output` diagnostic used commit `a3d4736`.
The temporary build-tag driver called `RunCancellationSmoke` one time.

| Metric | Result | Contract |
| --- | ---: | ---: |
| Work-stop time | `14,307ns` | `< 1s` |
| Finalization time | `5,666,565,034ns` | Report only |
| Stage wall time | `110.377s` | `< 15m` |
| Stage committed memory | `20,384,903,168` | `< 24GB` |
| Stage peak heap | `20,047,527,936` | Report only |
| Process swaps | `0` | Report only |

The stage started exactly 9,900,000 probes before cancellation.
It started zero probes after cancellation.
The saved cursor was 9,900,000, with 100,000 remaining tasks.
The recovery run completed all remaining tasks.
Both result files contained exactly 10,000,000 rows.

The recovery and reference digest was:

```text
5b8f3f23c1f452f6e99bf45058826d8fd574d511f2f9fd0b50b3fdbf97a8f821
```

The retained diagnostic is in `/media/hp/secondary/issue151-performance-a3d4736-diagnostics/cancel-result-99-10m/`.

## Input cancellation memory correction

The first 10-million-item `input-parsing` diagnostic canceled at 99 percent.
It reached 16,097,673,216 bytes of committed memory and failed the 8 GB parse limit.

The cancellation harness used the generic reader interface.
That interface did not use the optimized two-pass path for a seekable reader.

The public 100,000-row seekable-reader test failed first:

```text
peak heap increase = 148815872 bytes, want at most 70000000
```

The reader interface now detects `io.ReadSeeker`.
It uses the existing count, rewind, exact-capacity, and parse implementation.
An input stream that cannot seek keeps the original one-pass behavior.

Three green peak-heap runs used these values:

```text
69337088
69304320
69304320
```

The cancellation progress reader starts injection after the rewind.
Therefore, the count pass does not trigger cancellation.
The parse pass still injects cancellation at the requested byte-based record estimate.

The exact 10-million-item `input-parsing` diagnostic used commit `5a9ef5e`.
It canceled at 99 percent with these results:

| Metric | Result | Contract |
| --- | ---: | ---: |
| Stage wall time | `4.348s` | `< 5m` |
| Work-stop time | `28,464ns` | `< 1s` |
| Stage committed memory | `6,399,045,632` | `< 8GB` |
| Stage peak heap | `6,321,266,688` | Report only |
| Process swaps | `0` | Report only |

The injection threshold was 9,900,000 records.
The observed estimate was 9,900,037 records.
The stage started no probes.

The retained diagnostic is in `/media/hp/secondary/issue151-performance-5a9ef5e-diagnostics/cancel-input-99-10m/`.

## Other 99-percent cancellation stages

The exact 10-million-item diagnostics used commit `548802b`.
Each temporary build-tag driver called the existing cancellation seam one time.

| Stage | Stage wall time | Work-stop time | Peak committed | Recovery | Swaps |
| --- | ---: | ---: | ---: | --- | ---: |
| Rich expansion | `2.722s` | `27,292ns` | `649,281,536` | Not applicable | `0` |
| Bucket generation | `49.494s` | `55,404ns` | `16,248,602,624` | Not applicable | `0` |
| Resume rebuild | `72.696s` | `92,233ns` | `20,494,516,224` | Complete | `0` |

Rich expansion and bucket generation started no probes.
Resume rebuild canceled before scan dispatch and also started no probes.

The resume-rebuild saved cursor was zero because cancellation occurred during target reconstruction.
The recovery run therefore scanned all 10,000,000 tasks.
Both result files contained exactly 10,000,000 rows.

The recovery and reference digest was:

```text
baf309bb144d15b5227315ad2913e26fdfb2a9d81343064951a5dbe8a5c05630
```

The retained diagnostics are in these directories:

- `/media/hp/secondary/issue151-performance-548802b-diagnostics/cancel-rich-99-10m/`
- `/media/hp/secondary/issue151-performance-548802b-diagnostics/cancel-bucket-99-10m/`
- `/media/hp/secondary/issue151-performance-548802b-diagnostics/cancel-resume-99-10m/`

## Failure evidence phase split

The first failure-evidence tests did not compile.
`FailureResult` had no phase, operation, error-class, or logical-item fields.
`CaseResult` also had no versioned failure evidence.

The harness now measures fixture preparation and the failure stage separately.
The JSON report retains all six `FailureResult` values.
The Markdown report shows the scenario, operation, error class, and logical item count.
Cross-platform comparison ignores error text and metrics.
It compares the stable failure fields.

The focused race command passed:

```text
ok  github.com/xuxiping/port-scan-mk3/internal/perfharness  1.032s
ok  github.com/xuxiping/port-scan-mk3/internal/perfharness/cmd/perf-harness  1.029s
```

## Output-write failure recovery

The first production-seam test did not compile.
`RunOptions` had no public output-failure injection.

The new `OutputFailureInjection` wraps the real buffered scan writer.
The wrapper fails on the selected result and preserves the normal flush path.
The runtime reports probe and snapshot telemetry after the stop.

The first harness test failed because the old case still used an invalid output path.
That path failed during open and did not start the scan.

The green 20-item case failed on result 10.
It saved cursor 4 and 16 remaining tasks.
The initial output files contained eight rows because a write failure can leave duplicate durable rows.
The recovery appended 16 rows and omitted no task.
The recovery and reference task digests were equal.
The case also reported one or more rewound chunks, zero probe starts after failure, and released output handles.

The focused race command passed:

```text
ok  github.com/xuxiping/port-scan-mk3/pkg/scanapp  2.868s
ok  github.com/xuxiping/port-scan-mk3/internal/perfharness  2.676s
```

Six writer benchmarks used Go 1.24.4.
The base median was approximately `584.85ns/op`.
The branch median was approximately `577.8ns/op`.
This change was approximately `-1.2%`.
Both sides used `454B/op` and `4 allocs/op`.

## Snapshot-save failure precedence

The first state test did not compile.
The state package had no public failure injection for an atomic save operation.

The new state injection uses the production temp-write path.
It fails during replacement after write, flush, `fsync`, and close.
The prior snapshot remains byte-identical and loadable.
The failed save leaves no temp file and releases the file handle.

The first harness test failed on the old missing-directory case.
That case failed before a previous snapshot existed.

The green harness case first saves a valid snapshot.
It then injects three pressure failures during a real scan.
The runtime tries to save corrected progress and fails during replacement.
The snapshot-save error takes precedence over the pressure error.

The focused race command passed:

```text
ok  github.com/xuxiping/port-scan-mk3/pkg/state  1.017s
ok  github.com/xuxiping/port-scan-mk3/pkg/scanapp  1.055s
ok  github.com/xuxiping/port-scan-mk3/internal/perfharness  1.026s
```

## Pressure fatal recovery

The first harness test found no pressure evidence.
The old case retained only the fatal error text.

The green case records three source failures and final probe telemetry.
It loads the saved snapshot and records its cursor, remaining tasks, and rewind count.
It also records both output row counts before recovery and the released file handles.

The small run started one probe and saved cursor 1 with 19 remaining tasks.
It started zero probes after the fatal stop.
No chunk rewind was necessary because the first result was committed before the stop.

The recovery used pressure disabled and the production scan path.
It scanned all 19 remaining tasks.
The recovery and reference task digests were equal.
Both output files ended with 20 rows and final cursor 20.

The focused race command passed:

```text
ok  github.com/xuxiping/port-scan-mk3/pkg/state  1.032s
ok  github.com/xuxiping/port-scan-mk3/pkg/scanapp  2.925s
ok  github.com/xuxiping/port-scan-mk3/internal/perfharness  2.552s
```

## Failure coverage and fail-closed validation

The first aggregate coverage check reported `84.7%`.
No test threshold changed.

New public tests cover invalid injection values, blocked preparation paths, missing snapshots, and nested report differences.
One new test found that an unknown failure scenario could pass `FailureResult.Correct` when its common fields were present.
The test failed first.
The validator now rejects every unknown scenario.

The first repeated coverage run also found a test isolation problem.
An allocation assertion ran in parallel and counted one allocation from another goroutine.
The parser used zero allocations in 20 normal runs and 20 coverage-instrumented runs.
The assertion now runs in sequence and passed 100 focused runs.
The production sampler did not change.

The focused race tests passed.
The final aggregate coverage command exited zero with this line:

```text
coverage gate passed: 85.0%
```

The first complete `make verify` still failed at the aggregate coverage stage.
All earlier gate stages passed, but that coverage run reported `84.9%`.
The earlier result had no safe headroom.

Additional tests cover report destination errors, malformed artifacts, every stable semantic difference, counter reset, and missing Linux counters.
The new standalone gate covered 7,141 of 8,381 statements.
This is `85.2046%` before the gate formats the value to one decimal place.
The command exited zero with this line:

```text
coverage gate passed: 85.1%
```

## Ten-million-item failure diagnostics

The three diagnostics used exact production commit `52d8cbecb99bf451b6cc6e747af0029a0f50322b`.
Each diagnostic ran once and used 10,000,000 logical items with 16 workers.
Fixture preparation and the injected failure stage had separate measurements.
The stage budget was 15 minutes and 24 GB committed memory.
All three diagnostics passed this budget and reported zero process swaps.

The snapshot-save failure stage took `18.371980964s` and used `17,200,660,480` committed bytes.
The previous snapshot remained loadable and byte-identical.
The failed replacement left no temp file and released the file handle.
The injected save error took precedence over the pressure error.

The pressure fatal stage took `18.576056648s` and used `17,201,692,672` committed bytes.
Three pressure samples failed.
The run started one probe before the fatal stop and zero probes after it.
The saved cursor was 1, and 9,999,999 tasks remained.
Recovery completed every remaining task.
The recovery digest matched the reference digest.
Both CSV files ended with 10,000,000 rows and cursor 10,000,000.

The output failure stage took `63.870161933s` and used `18,132,717,568` committed bytes.
The writer failed at result 5,000,000 and rewound one chunk.
The saved cursor was 4,998,995, and 5,001,005 tasks remained.
Recovery completed every remaining task, and its digest matched the reference digest.
The final cursor was 10,000,000.
Each CSV file had 10,000,005 rows.
Five durable in-flight rows were repeated after rewind, but no probe task was omitted.
The output handles were released.

Raw JSON and process logs are under this directory:

```text
/media/hp/secondary/issue151-performance-52d8cbe-diagnostics/
```

## First final full-matrix attempt

The exact commit was `6d9c6dfb2c398dfe958d23e74ebe056995f49514`.
The matrix used one process and ran for more than five hours.
It passed 140 cases and reported no case failure before the final error.

All 15 cancellation cases passed at 10,000,000 logical items.
The resume cases at 0%, 50%, and 99% passed.
The three failure cases passed in six observations each.

The next accepted rich workflow stopped before pre-ping.
Its generated CSV was 1,022,664,300 bytes.
The default CIDR input limit was 1,000,000,000 bytes.
The exact error was:

```text
run rich family=rich-record-mixed: run rich-deny pre-ping workflow: CIDR input /media/hp/secondary/issue151-performance-6d9c6df/cases/rich-rich-record-mixed/run-0/fixture/input.csv size 1022664300 bytes exceeds limit 1000000000 bytes; use -cidr-input-size-limit-gb to override it
exit status 1
```

The harness did not write a partial JSON or Markdown report.
The raw log and case artifacts remain under these paths:

```text
/media/hp/secondary/issue151-performance-6d9c6df.log
/media/hp/secondary/issue151-performance-6d9c6df/
```

The focused regression test first failed to compile because
`acceptedRichResourceLimits` did not exist.
The new helper calculates the smallest positive decimal-GB override.
For the failed fixture, the override is 2,000,000,000 bytes.
The pre-ping, bucket, and scan stages use the same resource limits.
All unrelated limits keep their default values.

The focused race command passed:

```text
ok  github.com/xuxiping/port-scan-mk3/internal/perfharness  1.142s
```

## Accepted rich workflow bottlenecks

The first corrected full matrix used commit `212050c0939906b4c537d8553deac62b2e034160`.
It passed the accepted rich fixture cases and all earlier production cases.
The later production rich case processed only about 50 rows each second.
This rate required more than 55 hours for one observation.
The process received `SIGTERM` after more than six hours.

The CPU profile found an O(N) target-port lookup for each rich scan task.
At one million items, `indexToRuntimeTarget` used 69.14 percent of CPU time.
The lookup test failed with these results:

```text
one target: 18.87ns/op
65,536 targets: 30,631ns/op
```

The homogeneous rich runtime now uses a direct indexed lookup.
The focused race test passed with 59.89ns/op for one target and 55.79ns/op for 65,536 targets.
An exact one-million-item rich workflow then completed in 25.27 seconds.

## Accepted rich memory correction

The first exact ten-million-item run after the lookup fix took about 5 minutes and 29 seconds.
It used 27,979,001,856 committed bytes and failed the 24,000,000,000-byte limit.

The pre-ping workflow built the complete authorized rich target graph twice.
The first graph produced the unique ping addresses.
The second graph produced the unreachable rows.
The workflow also retained all unreachable rows before it wrote the CSV file.

The first public allocation test used 100,000 all-unreachable rich rows.
It allocated 718,055,352 bytes.
The final target-plan and streaming-writer implementation allocated 385,498,720 bytes.
The implementation keeps one authorized plan and writes merged unreachable rows in order.

The scan resume builder also copied the complete CIDR record table when all chunks were incomplete.
The new selection helper reuses the original table for this case.
Partial resumes keep the existing key filter.
The differential rich and basic runtime tests passed.

The rich harness previously measured three separate CLI commands as one process stage.
Each command now has a separate measurement boundary.
The combined workflow adds time and allocation counters and uses the largest command memory peak.
No runtime memory limit or `GOMEMLIMIT` value is set by the fix.

The final exact diagnostic used 10,000,000 rich records and 16 workers.
It passed all counts, completion checks, and semantic digests.
The evaluator returned this result:

```text
wall_time: 4m24.403987579s
peak_committed_bytes: 22961319936
go_peak_heap_bytes: 22500483072
go_allocated_bytes: 125430936272
swap_bytes: 0
verdict: passed
```

The raw artifacts are in this directory:

```text
/media/hp/secondary/issue151-rich-10m-integrated-green-463a86e-dirty/
```

Six pre-ping benchmark runs used 10,000 rich rows.
The base median was approximately 69.4ms/op and 68.49MB/op.
The branch median was approximately 39.4ms/op and 37.76MB/op.
The branch reduced time by about 43 percent and allocated bytes by about 45 percent.

The focused race command passed:

```text
ok  github.com/xuxiping/port-scan-mk3/pkg/scanapp  5.457s
ok  github.com/xuxiping/port-scan-mk3/internal/perfharness  2.616s
ok  github.com/xuxiping/port-scan-mk3/internal/perfharness/cmd/perf-harness  8.671s
```

## Accepted rich snapshot limit correction

The full matrix at commit `f35cfa9b1b7ab217f5a7a89df3fc601f42a366c4` ran for `6:38:35`.
It reached `production-rich/rich-precheck` after every earlier production case passed.
The matrix then exited with status 1 during the first precheck observation.

The 1,052,121,388-byte CSV produced a snapshot larger than 2 GB.
The retained exact-size evidence measured that snapshot at 2,233,986,251 bytes.
The accepted rich harness raised the CIDR limit to 2 GB.
It incorrectly kept the snapshot byte limit at the 2 GB product default.
The limiting writer stopped at byte 2,000,000,001, as designed.

The failed matrix used no swap.
Its maximum RSS was 23,947,512 KiB.
The raw artifacts remain in this directory:

```text
/media/hp/secondary/issue151-performance-f35cfa9-root/run-20260813T142029Z-363412/
```

The focused test first failed with this result:

```text
snapshot byte limit = 2000000000, want 4000000000
```

The accepted rich policy now gives the snapshot twice the rounded CIDR byte allowance.
The exact fixture therefore uses a positive 4 GB snapshot limit.
The 10,000,000 chunk, port-entry, and unreachable-IP limits remain unchanged.
The product default and the 15-minute and 24 GB performance gates also remain unchanged.

The focused race test passed:

```text
ok  github.com/xuxiping/port-scan-mk3/internal/perfharness  1.039s
```

The first broad package race command used a 10-minute test timeout.
It timed out while three existing heavy tests waited for concurrent GC cycles.
It reported no data race.
The repository gate ran the same package with its supported shuffle settings and passed.

`make verify` exited 0 with 85.1 percent coverage.
Its final result was:

```text
All selected quality gates passed.
```

`make verify-e2e` also exited 0.
Its final result was:

```text
All selected quality gates passed.
```

## Full matrix result after the snapshot limit correction

The full Linux matrix tested commit `686408b5a183759985508471304985999f15cd62`.
It ran for `7:36:24` and exited with status 2.
The process used no swap and reached a maximum RSS of 33,569,424 KiB.

All functional cases printed a case-level pass result.
The accepted rich precheck wrote a 2,204,185,907-byte snapshot in each of its six observations.
This result confirms that the bounded 4 GB override removed the earlier 2 GB save failure.

The final aggregate evaluator still failed five performance cases.
Three fixture-only rich cases exceeded their 8 GB committed-memory limit:

```text
rich-record-mixed: 11,635,814,400 / 11,669,544,960 bytes
rich-unique-key: 11,796,602,880 / 11,810,217,984 bytes
rich-hot-key: 11,858,558,976 / 11,658,543,104 bytes
```

`production-workflow/complete/workers-16` failed its growth rule.
Its ten-fold wall-time growth exceeded the allowed 12.5-fold ratio.

`production-rich/rich-precheck` completed in less than five minutes per observation.
However, its committed-memory values exceeded the unchanged 24 GB limit:

```text
cold: 30,149,410,816 bytes
steady median: 30,377,390,080 bytes
```

The generated reports are here:

```text
/media/hp/secondary/issue151-performance-f35cfa9-root/run-20260813T211538Z-1692500/report/performance-report.json
/media/hp/secondary/issue151-performance-f35cfa9-root/run-20260813T211538Z-1692500/report/performance-report.md
```

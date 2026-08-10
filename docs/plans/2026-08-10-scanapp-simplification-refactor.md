# Scanapp Simplification Refactor Specification

- **Status:** Historical
- **Date:** 2026-08-10
- **Decision map:** [Define the scanapp simplification route](https://github.com/superplayground3000/port-scan-mk3/issues/104)
- **Execution ticket:** [Write the executable scanapp refactor specification](https://github.com/superplayground3000/port-scan-mk3/issues/113)
- **Current architecture:** [`docs/apps/port-scan/DESIGN.md`](../apps/port-scan/DESIGN.md)
- **Audit baseline:** `a5061dd76d5dac7fb8ff7c1f22876d13af027235`

This file is a historical planning record. It does not define the current implementation architecture.

`docs/apps/port-scan/DESIGN.md` remains authoritative. Each architecture slice must update that file in the same change.

## 1. Objective

This refactor makes `pkg/scanapp` simpler without a user-visible CLI change. It replaces shallow Go interfaces and puts coupled behavior behind deep modules.

The implementation has these outcomes:

- Four command-specific parsers replace the shared configuration structure.
- Workflow modules accept consumer-owned configuration interfaces.
- A deep `pkg/pressure` module owns pressure transport behavior.
- A private concrete `scanRuntime` owns the complete scan lifecycle.
- Approved seams replace internal helper functions as the normal test surface.
- Shallow adapters, dead paths, and test-only resume paths leave the repository.
- Each change uses the approved TDD and evidence sequence.

The refactor ships only in the next major release. A minor or patch release cannot include the approved public Go breaks.

## 2. Non-goals

The refactor does not change these items:

- Command names or accepted CLI flags.
- CLI defaults, streams, formats, and exit classes.
- The input CSV, output CSV, or snapshot schemas.
- Scan algorithms, rate limits, pressure thresholds, or retry counts.
- The three-stage `pre-ping`, `generate-buckets`, and `scan` workflow.
- Legacy snapshot decoding or missing-`total_count` recovery.
- Scheduler-dependent ordering between simultaneous runtime errors.
- The current treatment of output close errors.

The refactor does not add a general runtime package. It does not add compatibility wrappers for removed Go interfaces.

## 3. Decision sources

The following decisions define this specification:

- [Lock the public test seams and protected behavior](https://github.com/superplayground3000/port-scan-mk3/issues/105) defines the contract and test surfaces.
- [Choose the public compatibility and release policy](https://github.com/superplayground3000/port-scan-mk3/issues/106) permits the next-major Go breaks.
- [Choose the current architecture source of truth](https://github.com/superplayground3000/port-scan-mk3/issues/107) defines the document roles.
- [Decide which shallow and legacy modules to remove](https://github.com/superplayground3000/port-scan-mk3/issues/108) defines the removal list.
- [Prototype command-specific configuration interfaces](https://github.com/superplayground3000/port-scan-mk3/issues/109) defines the configuration design.
- [Prototype the pressure source seam](https://github.com/superplayground3000/port-scan-mk3/issues/110) defines the pressure module.
- [Prototype a deep scan runtime module](https://github.com/superplayground3000/port-scan-mk3/issues/111) defines the runtime ownership.
- [Define the TDD vertical slices and validation evidence](https://github.com/superplayground3000/port-scan-mk3/issues/112) defines the execution order and evidence.

The accepted prototypes are discussion evidence only. Do not merge their code into production.

## 4. Target module map

```text
cmd/port-scan
    |
    +-- config.ParsePrePing ----------> config.PrePingConfig
    |                                      |
    |                                      v
    |                                  scanapp.RunPrePing
    |
    +-- config.ParseGenerateBuckets --> config.GenerateBucketsConfig
    |                                      |
    |                                      v
    |                                  scanapp.GenerateBuckets
    |
    +-- config.ParseScan -------------> config.ScanConfig
    |                                      |
    |                                      v
    |                                  scanapp.Run
    |                                      |
    |                                      v
    |                                  private scanRuntime
    |                                      |
    |                    +-----------------+-----------------+
    |                    |                 |                 |
    |                    v                 v                 v
    |               pkg/pressure      pkg/writer        pkg/state
    |
    +-- config.ParseValidate ---------> config.ValidateConfig
                                           |
                                           v
                                       validate.Inputs
```

`cmd/port-scan` owns CLI composition and exit mapping. It does not inspect pressure variants or build pressure adapters.

`pkg/config` owns argument parsing, defaults, input rules, and command values. It does not perform file, process, or network work.

`pkg/scanapp` owns workflow orchestration and consumer interfaces. It imports `pkg/pressure` for pressure samples and production adapters.

`pkg/pressure` owns HTTP and OAuth behavior. It does not import `pkg/scanapp`.

## 5. Approved seams

### 5.1 Product seam

The compiled `cmd/port-scan` CLI is the highest product seam. CLI integration and e2e tests use this seam.

The product seam protects these items:

- Command names and accepted flags.
- Required values, defaults, and ranges.
- Exit classes and output streams.
- Human and JSON formats.
- Durable artifact names, roles, and schemas.
- The complete three-stage pipeline.

The product seam does not protect help whitespace, timestamps, goroutine order, or non-contract error text.

### 5.2 Configuration seams

Each command-specific parser is a seam:

- `config.ParsePrePing`
- `config.ParseGenerateBuckets`
- `config.ParseScan`
- `config.ParseValidate`

Parser tests protect accepted flags, required values, defaults, bounds, durations, URLs, OAuth dependencies, and error classes.

### 5.3 Workflow seams

These functions are the workflow seams:

- `scanapp.RunPrePing`
- `scanapp.GenerateBuckets`
- `scanapp.Run`
- `validate.Inputs`

Tests use these seams for ordinary workflow behavior. They do not call private orchestration helpers after replacement coverage exists.

### 5.4 External seams and adapters

| Seam owner | Interface | Production adapter | Test adapter |
| --- | --- | --- | --- |
| `scanapp` | `DialFunc` | Standard library `net.Dialer` | Scripted dial function or local `net.Listener` |
| `scanapp` | `ReachabilityChecker` | Platform ping command | Scripted reachability checker |
| `scanapp` | `PressureSource` | `pressure.SimpleHTTP` or `pressure.OAuthMulti` | Scripted pressure source |
| `scanapp` | Private keyboard interface | `speedctrl` keyboard loop | Disabled private adapter |
| `scanapp` | Private writer fault interface | CSV writers | Deterministic failing writer |
| `scanapp` | Private terminal and time interfaces | Production terminal and clock behavior | Controlled adapters or event notifications |

Normal filesystem tests use real files under `t.TempDir()`. Do not add a general filesystem interface.

HTTP adapter tests use `httptest.Server`. Rare transport failures use a custom `http.RoundTripper` in the supplied client.

Fixed sleep is not a synchronization method. Tests use contexts, channels, or event notifications.

## 6. Command-specific configuration module

### 6.1 Public constructors and parsers

`pkg/config` exposes these opaque concrete values:

```go
type PrePingConfig struct{ state *prePingState }
type GenerateBucketsConfig struct{ state *generateBucketsState }
type ScanConfig struct{ state *scanState }
type ValidateConfig struct{ state *validateState }

func ParsePrePing(args []string) (PrePingConfig, error)
func ParseGenerateBuckets(args []string) (GenerateBucketsConfig, error)
func ParseScan(args []string) (ScanConfig, error)
func ParseValidate(args []string) (ValidateConfig, error)

func NewPrePing(PrePingValues) (PrePingConfig, error)
func NewGenerateBuckets(GenerateBucketsValues) (GenerateBucketsConfig, error)
func NewScan(ScanValues) (ScanConfig, error)
func NewValidate(ValidateValues) (ValidateConfig, error)
```

The constructors support tests and non-CLI callers. Constructors and parsers use the same input rules.

Each concrete value contains private state. Callers cannot construct a partial non-zero value.

Go still permits an exported zero value. Each `Resolve` method returns `ErrUninitializedConfiguration` for that value.

Every workflow resolves its configuration before file, process, or network work. A workflow does not repair values or apply parser defaults.

### 6.2 Consumer-owned configuration interfaces

The consuming packages own these one-method interfaces:

```go
// package scanapp
type PrePingConfiguration interface {
    Resolve() (config.PrePingValues, error)
}

type GenerateBucketsConfiguration interface {
    Resolve() (config.GenerateBucketsValues, error)
}

type ScanConfiguration interface {
    Resolve() (config.ScanValues, error)
}

// package validate
type Configuration interface {
    Resolve() (config.ValidateValues, error)
}
```

Each workflow accepts its interface and returns its current result type. The `pkg/config` constructors return concrete command values.

A command value cannot satisfy another workflow interface because each `Resolve` result type is different.

### 6.3 Command values

`PrePingValues` contains these values:

- Target path and column names.
- Output path.
- Worker count.
- Ping timeout.
- Progress interval.

`GenerateBucketsValues` contains these values:

- Target path and column names.
- Optional port path.
- Optional blocklist path.
- Snapshot output path.
- Worker count.
- Progress interval.

`ScanValues` contains these values:

- Target path and column names.
- Optional port path.
- Required resume input path.
- Output path.
- Worker count and dial timeout.
- Dispatch delay and bucket bounds.
- Logging format and quiet controls.
- One validated pressure policy.

`ValidateValues` contains these values:

- Target path and column names.
- Optional port path.
- Output format.

A parser can accept a compatibility flag without storing it. Accepted but unused values do not cross a workflow seam.

### 6.4 Pressure policy

Scan pressure configuration uses one opaque variant:

```go
func PressureDisabled() PressurePolicy
func SimplePressure(
    endpoint string,
    interval time.Duration,
) (PressurePolicy, error)

func AuthenticatedPressure(
    authURL string,
    dataURLs []string,
    clientID string,
    clientSecret string,
    interval time.Duration,
) (PressurePolicy, error)
```

The variants are disabled, simple endpoint, and OAuth endpoints. Partial OAuth configuration cannot leave `pkg/config`.

The scan parser continues to accept `-progress-interval`. This refactor does not give that flag new scan behavior.

Bucket generation sets pre-ping snapshot metadata explicitly. It does not read an unrelated zero `PreScanPingTimeout` value.

## 7. Pressure module

### 7.1 Consumer interface

`pkg/scanapp` owns the pressure source interface:

```go
type PressureSource interface {
    Sample(context.Context) (pressure.Sample, error)
}
```

`RunOptions.PressureSource` is the test and override seam. It replaces `RunOptions.PressureFetcher` and `RunOptions.PressureHTTP`.

The pressure monitor calls `Sample` once for each poll. It does not use type assertions for source telemetry.

### 7.2 Module interface

`pkg/pressure` exposes these types and operations:

```go
type Sample struct {
    Maximum float64
    Sources []SourceResult
}

type SourceResult struct {
    Name     string
    Pressure float64
    Err      error
}

type OAuthConfig struct {
    AuthEndpoint  string
    DataEndpoints []string
    ClientID      string
    ClientSecret  string
}

type SimpleHTTP struct { /* private state */ }
type OAuthMulti struct { /* private state */ }

func NewSimpleHTTP(
    endpoint string,
    client *http.Client,
) (*SimpleHTTP, error)

func NewOAuthMulti(
    cfg OAuthConfig,
    client *http.Client,
) (*OAuthMulti, error)

func (s *SimpleHTTP) Sample(context.Context) (Sample, error)
func (s *OAuthMulti) Sample(context.Context) (Sample, error)
```

Constructors return concrete adapters. They verify input, reject a nil client, and copy caller-owned slices.

The module hides HTTP requests, JSON decoding, normalization, OAuth refresh, token locks, fan-out, ordering, and aggregate errors.

Do not add an `HTTPDoer`, `PressureProbe`, or `AggregationPolicy` interface. These interfaces have no second approved adapter.

### 7.3 Construction rules

A private `scanapp` factory maps the validated policy to an adapter:

```go
func newPressureSource(
    policy config.PressurePolicy,
) (PressureSource, error)
```

The factory creates one `http.Client` for each scan. The default timeout remains two seconds.

The factory passes the same client to all endpoints. Each OAuth endpoint keeps its separate token cache and mutex.

The command handler does not inspect pressure variants. It does not construct HTTP or OAuth adapters.

If `RunOptions.PressureSource` is not nil, use that source. Otherwise, create the source from the validated pressure policy.

Construct the source before runtime file or network work. Return constructor errors before the monitor starts.

### 7.4 Sample invariants

A successful `SimpleHTTP` sample puts normalized pressure in `Maximum`. Its `Sources` list stays empty.

A successful `OAuthMulti` sample returns one source result for each configured endpoint. The results stay in configuration order.

`OAuthMulti` polls all endpoints concurrently and waits for all results. The largest normalized value becomes `Maximum`.

Zero and negative pressure values remain valid. Each call returns a new result slice.

If one source fails, the sample still contains every source result. `Maximum` stays zero, and the complete poll returns an error.

The aggregate error wraps the first failed source in configuration order. Goroutine completion order does not select that error.

### 7.5 Monitor and telemetry order

The pressure monitor owns the failure streak, threshold comparison, pause state, and fatal error.

For each poll, the monitor uses this order:

1. Record every source result.
2. If `Sample` returns an error, record the aggregate error.
3. If `Sample` returns an error, increase the consecutive-failure count.
4. If the failure count reaches three, stop the run.
5. If `Sample` succeeds, reset the failure count.
6. Record aggregate pressure.
7. Pause at or above the threshold.
8. Resume below the threshold.

The third consecutive failure reaches the scan runtime. The runtime saves the resume snapshot.

Context cancellation stops active HTTP requests. It does not increase the failure streak after the scan context ends.

One complete private observer replaces optional observer assertions:

```go
type pressureTelemetryObserver interface {
    OnPressurePoll(pressurePoll)
}
```

`pressurePoll` contains the sample, aggregate error, failure count, and sample time.

## 8. Deep scan runtime

### 8.1 Module interface

Keep the runtime inside `pkg/scanapp`. Do not create `pkg/scanruntime` or a runtime interface.

There is one orchestration implementation and one public caller. A package seam or polymorphic interface is hypothetical.

The private module has this interface:

```go
type scanRuntime struct {
    // Private inputs, adapters, and owned state.
}

func newScanRuntime(
    input scanRuntimeInput,
    adapters scanRuntimeAdapters,
) *scanRuntime

func (r *scanRuntime) execute(context.Context) error
```

`scanapp.Run` resolves `ScanConfiguration` and creates approved adapters. It then creates the runtime and calls `execute`.

The facade does not coordinate channels. It does not select pressure transports or apply resume error precedence.

### 8.2 Runtime ownership

The runtime owns this lifecycle:

1. Prepare inputs, snapshot state, output paths, and chunk runtimes.
2. Open output files before workers and the dispatcher start.
3. Start control tasks, executor workers, and the dispatcher.
4. Coordinate cancellation and drain every required channel.
5. Commit output rows and rewind every unwritten task after an output error.
6. If persistence is required, save the resume snapshot.
7. Select the final error.
8. Emit one completion summary.
9. Stop background tasks and close output files.

This refactor preserves the relative order that `scanapp.Run` tests protect. It does not create a new order for unprotected internal actions.

The dispatcher closes the task channel. Executors close the result and executor-error channels after all workers stop.

The runtime drains produced results after cancellation. It also drains the executor-error channel after the result channel closes.

### 8.3 Error precedence

Preparation and output-open errors return before the running phase. Runtime completion reporting starts only after the running phase begins.

The first observed pressure, executor, or output error becomes `runErr`. The runtime cancels the child context once.

Simultaneous runtime sources keep scheduler-dependent select order. This refactor does not add a deterministic source priority.

The final error uses this order:

1. Resume snapshot save error.
2. First observed runtime error.
3. Dispatcher error.
4. `nil`.

Output close errors remain best-effort and do not replace the selected error. A separate behavior decision is required to change this rule.

### 8.4 Durability invariants

A probe result becomes committed only after both required output writes succeed. Tracker progress and summary counts advance after that commit.

Each committed probe produces one full-results row. An open probe also produces one open-only row.

An open-only write failure can leave a duplicate full-results row after resume. It cannot cause an unwritten task to be skipped.

After an output error, the runtime drains results and marks unwritten tasks. It rewinds each affected chunk before snapshot creation.

The runtime saves a snapshot for incomplete work, cancellation, or a fatal runtime error. It does not save after clean completion.

The saved snapshot contains tracker state, pre-ping state, and resolved absolute output paths. A resumed scan appends to those paths.

Completion counts include only committed rows. Local-resource errors never count as confirmed close.

### 8.5 Preserved runtime mechanics

The runtime writes directly to final output paths and flushes each row. This refactor does not add a temporary-file commit protocol.

A keyboard startup error remains nonfatal. The runtime records the error and continues without keyboard control.

The dispatch delay keeps its current non-context-aware sleep behavior. A separate behavior slice is required to change that sleep.

Snapshot persistence occurs before output close. Output close errors remain best-effort.

## 9. Protected workflow behavior

### 9.1 Pre-ping behavior

- Basic and rich inputs operate on unique IP addresses.
- Basic input does not require a port file.
- Each unique IP receives one reachability probe.
- The command configuration supplies the worker limit.
- Unreachable results and fatal checker errors remain different outcomes.
- Success writes the fixed CSV schema and writes its path to `stdout`.
- Progress uses `stderr`.
- Cancellation does not leave a misleading final output.

### 9.2 Bucket behavior

- Basic and rich inputs remain supported.
- A fresh basic build requires a port file.
- The optional blocklist excludes unreachable IPv4 targets.
- Chunk ownership and `total_count` remain correct.
- The snapshot keeps its current schema and pre-ping state.
- Different worker counts produce byte-identical snapshots.
- `scanapp.Run` accepts the snapshot without conversion.
- Cancellation and write errors do not produce a misleading complete snapshot.

### 9.3 Scan and control behavior

- `scanapp.Run` requires a resume snapshot.
- The scan does not ping or build fresh chunks.
- The snapshot blocklist selects reachable targets.
- Rate limits and dispatch delay apply to scan tasks.
- Manual pause and pressure pause can exist at the same time.
- Any active pause blocks dispatch.
- Pressure at the threshold pauses the run.
- Pressure less than the threshold resumes the run.
- Three consecutive pressure failures stop the run and save resume state.
- A successful pressure request resets the failure streak.

### 9.4 Observability behavior

- The dashboard runs only for human output on a TTY `stderr`.
- JSON and non-TTY modes do not emit an ANSI dashboard.
- Quiet mode keeps required error and completion evidence.
- Each committed result produces one `scan_result` event.
- Completion counts use committed rows.
- Open, confirmed close, timeout, local-resource error, and unknown remain separate outcomes.

## 10. Compatibility requirements

### 10.1 Public release policy

The next major release removes these Go interfaces without a deprecation period:

- `config.Config`
- `config.Parse`
- `config.ParseFor`
- The old shape of `scanapp.RunOptions`
- `scanapp.ScanRecord`
- Old pressure fetchers and constructors

Do not add compatibility wrappers only to preserve these shapes. Release notes must list every break and give migration steps.

### 10.2 CLI compatibility

Keep command names, accepted flags, required values, defaults, ranges, durations, URLs, OAuth rules, streams, formats, and exit classes unchanged.

Flag and configuration errors use exit 2 and `stderr`. Workflow and input-content failures use exit 1.

Cancellation uses exit 130. Exact non-contract error text can change.

`validate` is a compatibility exception. `ParseValidate` must accept and verify the complete flag surface that legacy `config.Parse` accepts.

`ParseValidate` can discard values that `ValidateValues` does not use. A narrower flag surface requires a separate CLI decision.

### 10.3 Snapshot and output compatibility

Keep current and legacy snapshot decoding. Keep recovery for snapshots without `total_count`.

Resume uses the original output paths and verifies both CSV headers. Relative legacy paths keep their current anchoring and upgrade rules.

Keep the current CSV schemas and roles. A write error can cause duplicates, but it cannot skip an unsaved task.

## 11. Approved removal list

Remove an item only after approved seam tests protect retained behavior.

### 11.1 Record and writer shapes

- Remove `ScanRecord`, `writerRecordAdapter`, and `AsScanRecord`.
- Use `writer.Record` directly inside the runtime.
- Remove `pkg/cli/writer_adapter.go` and its tests.
- Keep `pkg/cli/output.go` because `WriteValidation` has production callers.
- Make `scanapp.RecordWriter` private.
- Keep a private writer adapter only for deterministic output failures.

### 11.2 Run-plan shapes

- Remove `prepareRunPlan` and its direct unit test.
- Remove dead fields named `chunks`, `outputPaths`, `scanOutputPath`, and `openOnlyPath`.
- Keep `resolveRunOutputPaths` because pre-ping and scan use it.

### 11.3 Resume shapes

- Remove `RunOptions.ResumeStatePath`.
- Remove `resumePath`, `defaultResumeStateFile`, and their direct tests.
- Remove the test-only `persistResumeState` wrapper.
- Remove empty-path behavior from `loadResumeSnapshot`, or call `state.LoadSnapshot` directly.
- Keep production snapshot persistence inside the runtime.
- Make tests use the configured resume path under `t.TempDir()`.

### 11.4 Fresh-build scan paths

- Remove `useResumeChunks` and the fresh-build branch in `resolveRuntimeChunks`.
- Remove unused load-or-build and fallback dependencies.
- Remove production `loadOrBuildChunks`, `loadOrBuildChunksWithPredicate`, and `buildRuntime` wrappers.
- If benchmarks need fixture builders, keep them in `_test.go` files.
- Keep `GenerateBuckets` as the only production path that creates fresh chunks.

### 11.5 Other obsolete shapes

- Remove `openBatchOutputsAfterUnreachable` and its direct test.
- Keep `openUnreachableOutput` for pre-ping.
- Remove `PressureFetcher`, `pressureSourceStatusFetcher`, and `PressureSourceResult`.
- Remove `Fetch`, `FetchWithSourceStatuses`, and the duplicate `fetchPressure` helper.
- Remove optional pressure observer type assertions.
- Remove the pressure adapter-selection branch from `cmd/port-scan`.

## 12. TDD execution slices

A TDD cycle contains one failing test, the minimum implementation, and one focused green run. A delivery slice can contain related cycles.

Every delivery slice ends with `make verify` and independent review. Triggered e2e and performance evidence belongs to the same slice.

Pure structural changes do not use artificial red tests. They use green characterization coverage through an approved seam.

Keep the red command, expected failure, focused green output, and gate exit code for each slice.

The red output must identify the missing approved behavior or interface. An unrelated compile error is not red proof.

Run e2e tests only in the isolated Docker environment with mock services. Do not scan an external target.

### Slice 1: Pre-ping configuration

**Red proof**

- Add parser and constructor tests before the new symbols exist.
- Add one uninitialized-configuration workflow test.
- Add separate red cycles for defaults and invalid values.

**Minimal implementation**

- Add the opaque pre-ping value, parser, constructor, and consumer interface.
- Migrate `RunPrePing` and its command handler.
- Return `ErrUninitializedConfiguration` before side effects.

**Focused green**

```sh
go test -race ./pkg/config ./pkg/scanapp ./cmd/port-scan -run 'PrePing'
```

**Review stage**

- Replace pre-ping `config.Config` literals with `NewPrePing`.
- Remove only duplicate tests of the old shape.
- Update the affected sections of `docs/apps/port-scan/DESIGN.md`.
- Create or update the architecture index and mark stale diagrams `Outdated`.

**Evidence**

- Run `make verify`.
- This slice does not trigger e2e or performance evidence.
- Get an independent review.

### Slice 2: Bucket configuration

**Red proof**

- Add parser and constructor tests before the new symbols exist.
- Add separate red cycles for required output, bounds, blocklist input, and zero value.
- Add a red cycle for explicit pre-ping snapshot metadata.

**Minimal implementation**

- Add the opaque bucket value, parser, constructor, and consumer interface.
- Migrate `GenerateBuckets` and its command handler.
- Stop reading the unrelated zero `PreScanPingTimeout` field.

**Focused green**

```sh
go test -race ./pkg/config ./pkg/scanapp ./cmd/port-scan \
  -run 'GenerateBuckets|Bucket'
```

**Review stage**

- Replace bucket `config.Config` literals with `NewGenerateBuckets`.
- Remove only duplicate tests of the old shape.
- Update the affected `DESIGN.md` sections.

**Evidence**

- Run `make verify`.
- This slice does not trigger e2e or performance evidence.
- Get an independent review.

### Slice 3: Pressure module

**Red proof**

- Start with an `OAuthMulti` test for ordered partial results and a non-nil aggregate error.
- Add separate cycles for maximum selection, simple HTTP responses, constructor errors, and result-slice ownership.

**Minimal implementation**

- Add the pressure result model and both concrete adapters.
- Move transport, OAuth, fan-out, normalization, ordering, and aggregate errors into `pkg/pressure`.

**Focused green**

```sh
go test -race ./pkg/pressure
```

**Review stage**

- Remove private helper seams from the module interface.
- Verify the dependency direction between `scanapp` and `pressure`.
- Update the affected `DESIGN.md` sections.

**Evidence**

- Run `make verify` and `make verify-e2e`.
- Record equivalent old-fetcher and new-adapter benchmark results.
- Get an independent review.

### Slice 4: Scan configuration and pressure wiring

**Red proof**

- Add parser cycles for defaults, bounds, durations, URLs, OAuth dependencies, pressure variants, and zero value.
- Add a workflow cycle that injects `PressureSource`.
- Protect fatal third-failure resume persistence through `scanapp.Run`.

**Minimal implementation**

- Add the opaque scan value, parser, constructor, pressure policy, and consumer interface.
- Migrate `scanapp.Run` and its command handler.
- Move pressure adapter selection into the private `scanapp` factory.
- Replace old pressure option fields with `RunOptions.PressureSource`.

**Focused green**

```sh
go test -race ./pkg/config ./pkg/pressure ./pkg/scanapp \
  ./cmd/port-scan -run 'Scan|Pressure'
```

**Review stage**

- Remove pressure variant knowledge from the command handler.
- Keep the current accepted but unused scan progress flag behavior.
- Update the affected `DESIGN.md` sections.

**Evidence**

- Run `make verify` and `make verify-e2e`.
- Record pressure-control benchmark results.
- Get an independent review.

### Slice 5: Deep scan runtime

**Red proof**

Red proof is not applicable because this slice changes structure only.

Before the move, add or retain green `scanapp.Run` coverage for these behaviors:

- Output files open before workers and the dispatcher start.
- The dispatcher closes the task channel.
- The executor closes and drains its result and error channels.
- Cancellation does not stop required channel drain.
- A snapshot save error overrides a runtime error.
- A runtime error overrides a dispatcher error.
- The runtime emits one completion summary.
- Output close errors do not replace the selected error.

**Minimal implementation**

- Add the private concrete runtime.
- Move the existing lifecycle into `scanRuntime.execute` without an order change.
- Reduce `scanapp.Run` to configuration resolution, adapter construction, and one runtime call.

**Focused green**

```sh
go test -race ./pkg/scanapp -run 'TestRun|TestFullFlow|TestPipeline'
```

**Review stage**

- Verify output-open, startup, drain, rewind, save, summary, and close order.
- Remove direct lifecycle tests only after workflow coverage exists.
- Update the affected `DESIGN.md` sections.

**Evidence**

- Run `make verify` and `make verify-e2e`.
- Run the approved scan runtime benchmarks before and after the change.
- Get an independent review.

Use this benchmark command:

```sh
go test -run '^$' \
  -bench 'Benchmark(ResumeRebuild|StartScanExecutor|ApplyScanResult|WriteScanRecord)' \
  -benchmem -count=6 ./pkg/scanapp
```

### Slice 6: Scan internals cleanup

**Red proof**

Red proof is not applicable because this slice removes replaced structures without a behavior change.

Before each removal, protect retained behavior through `scanapp.Run` or an approved internal fault test.

**Minimal implementation**

- Use `writer.Record` directly.
- Remove the approved record, writer, run-plan, resume, fresh-build, and output shapes.

**Focused green**

```sh
go test -race ./pkg/scanapp ./pkg/state ./pkg/writer \
  -run 'TestRun|Record|Output|Resume|Snapshot'
```

**Review stage**

- Keep only race, platform, deterministic-fault, and performance tests against internal implementation.
- Keep legacy snapshot and resume-preparation coverage.
- Update the affected `DESIGN.md` sections.

**Evidence**

- Run `make verify` and `make verify-e2e`.
- Record result-write and resume-rebuild benchmark results.
- Get an independent review.

### Slice 7: Validate configuration

**Red proof**

- Add parser and constructor tests before the new symbols exist.
- Add separate cycles for zero value, exit classes, and the complete legacy flag surface.

**Minimal implementation**

- Add the opaque validate value, parser, constructor, and consumer interface.
- Migrate `validate.Inputs` and its command handler.
- Preserve input-dependent validity results.

**Focused green**

```sh
go test -race ./pkg/config ./pkg/validate ./cmd/port-scan -run 'Validate'
```

**Review stage**

- Remove only duplicate tests of the old shape.
- Verify the acceptance and input rules for every legacy flag.
- Update the affected `DESIGN.md` sections.

**Evidence**

- Run `make verify`.
- This slice does not trigger e2e or performance evidence.
- Get an independent review.

### Slice 8: Legacy removal and documentation

**Red proof**

Red proof is not applicable because this slice removes unused shapes and updates documents.

**Minimal implementation**

- Remove the old configuration and pressure interfaces after all callers migrate.
- Remove tests that protect old shapes instead of protected behavior.
- Complete the approved document migration.

**Focused green**

```sh
go test -race ./pkg/config ./pkg/pressure ./pkg/scanapp \
  ./pkg/validate ./cmd/port-scan
```

**Review stage**

- Search the repository for every removed symbol.
- Verify that no production caller uses an old shape.
- Complete the final `DESIGN.md` coherence review.
- Update the architecture index and diagram status.
- Add migration guidance to `docs/release-notes/<next-major>.md`.
- Give each existing plan its required status and current-architecture link.
- Mark this plan `Historical` after implementation ends.

**Evidence**

- Run `make verify` and `make verify-e2e`.
- Compare all hot-path benchmarks with the audit baseline.
- Get an independent review.

## 13. Performance evidence

Each hot-path slice compares its head with its direct parent. The final slice also compares the complete refactor with the audit baseline.

Run before and after benchmarks on the same machine. Use the same Go version, fixtures, benchmark body, and adapter settings.

Use `-benchmem -count=6`. If `benchstat` is available, use it.

If `benchstat` is not available, keep all raw `ns/op` and `allocs/op` results.

For `pkg/pressure`, use equivalent HTTP fixtures for old fetchers and new adapters. Compare these benchmark names:

- `BenchmarkPressureSampleSimple`
- `BenchmarkPressureSampleOAuthMulti`

Resume preparation performance includes snapshot load and runtime rebuild. Keep these representative cases:

- Current and legacy snapshot formats.
- 4,000 chunks and 42,587 unreachable IPs.
- 130 incomplete chunks from 4,000 chunks.
- 4,000 incomplete chunks from 4,000 chunks.
- The existing rich-input scaling matrix.

Target growth must remain approximately linear. A regression of more than 10% in `ns/op` or `allocs/op` blocks the slice.

A human can accept a larger regression as an explicit trade-off. Record that decision before the slice continues.

## 14. Independent review evidence

Each delivery slice requires an independent review after its applicable gates pass.

If a different provider is available, use it. The minimum reviewer is a fresh-context agent that did not see the implementation conversation.

Each review records these items:

- Slice base commit and reviewed head commit.
- Reviewer provider, model, or fresh-context identity.
- Standards review against repository rules and the constitution.
- Spec review against the approved seam, behavior, red proof, and removal scope.
- Reviewer `make verify` result.
- Reviewer decision for e2e and performance triggers.
- An `approve` or `block` verdict.
- Each blocking finding with a `file:line` location.
- Fix commit and re-review verdict for each blocking finding.

The reviewer verifies responsibility ownership, interface direction, platform behavior, and all triggered evidence.

## 15. Documentation updates

`docs/apps/port-scan/DESIGN.md` is the current architecture source. Update affected sections in every architecture slice.

`docs/apps/port-scan/SPEC.md` defines user-visible behavior. This refactor does not change that behavior.

If implementation requires a `SPEC.md` behavior change, stop the current slice. Get a separate behavior decision before the change.

`docs/architecture/` contains derived views. Its index records each diagram status, version, and source document.

If a slice does not update a derived diagram, mark that diagram `Outdated` in the same slice.

Keep historical diagram paths. Add a visible historical banner to pre-2.0.0 HTML artifacts.

Each plan that predates this active plan gets a `Status: Historical` header. It also gets a link to the current `DESIGN.md`.

New plans use `Proposed`, `Active`, or `Historical`.

The next-major release notes list these changes:

- Four direct parser functions and four constructors.
- Consumer-owned workflow configuration interfaces.
- The changed `RunOptions` fields.
- Removed `config.Config`, `Parse`, and `ParseFor`.
- Removed `ScanRecord` and writer adapters.
- Removed pressure fetchers and constructors.
- Migration examples for non-CLI Go callers.
- A statement that user-visible CLI behavior stays unchanged.

## 16. Stop conditions

If one of these conditions is true, stop the delivery slice:

- A new behavior or interface has no genuine red proof.
- A focused test or applicable gate fails.
- Coverage is less than 85%.
- A change weakens a gate, threshold, or test.
- A benchmark regression is more than 10%.
- The independent reviewer returns `block`.
- Protected CLI, CSV, resume, or error-class behavior changes without approval.
- The implementation needs an unapproved public seam or compatibility adapter.
- A pure refactor needs a production behavior change.
- Linux and Windows build results or protected behavior differ.
- A local slice spreads across approximately ten unrelated production files without a clear reason.

If a behavior correction becomes necessary, stop the current slice. Create a separate red-to-green slice for that correction.

Do not lower coverage. Do not remove assertions or skip tests.

Do not add `t.Skip` or `//nolint` to continue a blocked slice.

## 17. Completion conditions

The refactor is complete only when all conditions are true:

- All eight delivery slices are merged in order.
- Every behavior change has red and green evidence.
- Every slice has a successful `make verify` result and an independent approval.
- Every triggered e2e and performance result is present.
- The final performance comparison meets the approved limit or records an accepted trade-off.
- No production caller uses a removed interface.
- The CLI and workflow contracts remain unchanged.
- Current and legacy snapshot behavior remains protected.
- Linux and Windows behavior remains equivalent.
- `DESIGN.md`, diagram status, plan status, and next-major release notes are current.
- This active plan has changed to `Historical`.

This specification ends at the implementation handoff. It does not authorize a CLI behavior change or a quality-gate exception.

# CLI Flags Reference

This document is the complete CLI flag reference for `port-scan-mk3`. It comes
from the current parser behavior in:

- `pkg/config/pre_ping.go` (`ParsePrePing`)
- `pkg/config/generate_buckets.go` (`ParseGenerateBuckets`)
- `pkg/config/scan_config.go` (`ParseScan`)
- `pkg/config/validate_config.go` (`ParseValidate`)
- `cmd/port-scan/main.go` and `cmd/port-scan/command_handlers.go` (dispatch, usage)

## Command Scope

`port-scan` is a three-step pipeline plus a `validate` helper. The three
pipeline commands register only their workflow flags. An unregistered flag is
an unknown-flag error (exit `2`).

`validate` is a compatibility exception. It accepts and verifies all 30 flags
from the removed shared parser. It discards values that input validation does
not use.

This structure makes "`scan` never pings" a property of the code. `scan` does
not register `-disable-pre-scan-ping` or `-pre-scan-ping-timeout`.

| Subcommand | Purpose | Required flags |
|------------|---------|----------------|
| `pre-ping` | Ping unique target IPs, write `unreachable_results-<ts>.csv` | `-cidr-file` |
| `generate-buckets` | Build the resume bucket snapshot from targets − blocklist | `-cidr-file`, `-buckets-out` |
| `scan` | Pure TCP scan of a bucket snapshot | `-cidr-file`, `-resume` |
| `validate` | Validate CIDR/port inputs, no scan | `-cidr-file` |

## Per-command flag tables

Flags shared by every subcommand: `-cidr-file` (required), `-cidr-ip-col`
(default `ip`), `-cidr-ip-cidr-col` (default `ip_cidr`), `-log-level` (default
`info`), `-format` (`human`|`json`, default `human`), `-quiet`,
`-target-count-limit` (default `10000000`), and
`-target-memory-limit-gb` (default `16`).

All commands also accept `-cidr-input-size-limit-gb` (default `1`) and
`-cidr-input-record-limit` (default `10000000`).

### `port-scan pre-ping`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `-cidr-file` | string | required | Path to CIDR/rich CSV input file. |
| `-cidr-ip-col` / `-cidr-ip-cidr-col` | string | `ip` / `ip_cidr` | Case-sensitive column mapping. |
| `-target-count-limit` | int | `10000000` | Maximum candidate addresses. `0` disables this limit. |
| `-target-memory-limit-gb` | int | `16` | Target expansion budget in decimal GB. `0` disables this limit. |
| `-cidr-input-size-limit-gb` | int | `1` | Maximum CIDR input size in decimal GB. `0` disables this limit. |
| `-cidr-input-record-limit` | int | `10000000` | Maximum CIDR data records. `0` disables this limit. |
| `-pre-scan-ping-timeout` | duration | `100ms` | Reply-wait timeout for each ping reachability check. Must be > 0. On Windows, the process wall-clock ceiling adds an internal fixed startup allowance to this value. The ping launch therefore does not kill a fast reply. The reply-wait still uses this value. |
| `-workers` | int | `10` | Concurrent ping workers. Accepted range `1`-`1024`. One ping process can run for each worker. |
| `-output` | string | `scan_results.csv` | Output anchor path. It gives the directory and the shared timestamp suffix for `unreachable_results-<ts>.csv`. |
| `-progress-interval` | int | `100` | Progress line cadence (count of processed unique IPs). The command writes these lines to stderr. |
| `-log-level` / `-format` / `-quiet` | — | — | Shared observability flags. |

- No `-port-file` (a ping is per-IP, so ports have no effect).
- No `-disable-pre-scan-ping` — to skip the ping, do not run this step.
- The command prints the resolved `unreachable_results-<ts>.csv` path to
  **stdout** for chaining. Progress lines go to **stderr**.

### `port-scan generate-buckets`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `-cidr-file` | string | required | Path to CIDR/rich CSV input file. |
| `-cidr-ip-col` / `-cidr-ip-cidr-col` | string | `ip` / `ip_cidr` | Case-sensitive column mapping. |
| `-target-count-limit` | int | `10000000` | Maximum candidate addresses. `0` disables this limit. |
| `-target-memory-limit-gb` | int | `16` | Target expansion budget in decimal GB. `0` disables this limit. |
| `-cidr-input-size-limit-gb` | int | `1` | Maximum CIDR input size in decimal GB. `0` disables this limit. |
| `-cidr-input-record-limit` | int | `10000000` | Maximum CIDR data records. `0` disables this limit. |
| `-port-input-size-limit-mb` | int | `1` | Maximum port input size in decimal MB. `0` disables this limit. |
| `-port-input-record-limit` | int | `65535` | Maximum nonblank port records. `0` disables this limit. |
| `-snapshot-size-limit-gb` | int | `2` | Maximum snapshot size in decimal GB. `0` disables this limit. |
| `-snapshot-chunk-limit` | int | `10000000` | Maximum snapshot chunks. `0` disables this limit. |
| `-snapshot-port-entry-limit` | int | `10000000` | Maximum snapshot port entries. `0` disables this limit. |
| `-snapshot-unreachable-ip-limit` | int | `10000000` | Maximum snapshot unreachable IPs. `0` disables this limit. |
| `-port-file` | string | optional | Port list (`<port>/tcp` lines). Required in basic (non-rich) mode. Ignored in rich mode, where the command reads the port of each target from the CSV. |
| `-unreachable-file` | string | optional | Blocklist CSV (a `pre-ping` output). The command subtracts its `ip` column from the target set. Omit it to bucket all targets. |
| `-buckets-out` | string | **required** | Output path for the bucket snapshot (the resume `Snapshot` JSON). |
| `-workers` | int | `10` | Parallel chunk builders, one for each CIDR group. Accepted range `1`-`1024`. The output is deterministic (CIDR-sorted) at every worker count. |
| `-progress-interval` | int | `100` | Progress line cadence (count of processed CIDR groups). The command writes these lines to stderr. |
| `-log-level` / `-format` / `-quiet` | — | — | Shared observability flags. |

- The command does **no network I/O**.
- It always stamps `pre_scan_ping.enabled=true` in the snapshot, so `scan` never pings.

### `port-scan scan`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `-cidr-file` | string | required | Rich/basic CSV — the source of truth for target metadata. The command rebuilds it on every run. |
| `-cidr-ip-col` / `-cidr-ip-cidr-col` | string | `ip` / `ip_cidr` | Case-sensitive column mapping. |
| `-target-count-limit` | int | stored or `10000000` | Explicit value replaces the stored count limit. `0` disables this limit. |
| `-target-memory-limit-gb` | int | stored or `16` | Explicit value replaces the stored memory limit. `0` disables this limit. |
| `-cidr-input-size-limit-gb` | int | `1` | Maximum CIDR input size in decimal GB. `0` disables this limit. |
| `-cidr-input-record-limit` | int | `10000000` | Maximum CIDR data records. `0` disables this limit. |
| `-port-input-size-limit-mb` | int | `1` | Maximum port input size in decimal MB. `0` disables this limit. |
| `-port-input-record-limit` | int | `65535` | Maximum nonblank port records. `0` disables this limit. |
| `-snapshot-size-limit-gb` | int | `2` | Maximum snapshot load and save size in decimal GB. `0` disables this limit. |
| `-snapshot-chunk-limit` | int | `10000000` | Maximum snapshot chunks. `0` disables this limit. |
| `-snapshot-port-entry-limit` | int | `10000000` | Maximum snapshot port entries. `0` disables this limit. |
| `-snapshot-unreachable-ip-limit` | int | `10000000` | Maximum snapshot unreachable IPs. `0` disables this limit. |
| `-pressure-response-size-limit-mb` | int | `1` | Maximum size of each pressure response in decimal MB. `0` disables this limit. |
| `-pressure-response-entry-limit` | int | `10000` | Maximum entries in each OAuth data array. `0` disables this limit. |
| `-resume` | string | **required** | Bucket snapshot file to scan. The command reads it at start. On an interrupt or an error, the command **updates it in place at this path**. |
| `-output` | string | `scan_results.csv` | Output anchor. It gives the directory and the shared suffix for `scan_results-<ts>.csv` and `opened_results-<ts>.csv`. |
| `-output-flush-results` | int | `1000` | Probe results per output batch. `1` flushes each result. `0` disables periodic flushes. Negative values are errors. Positive values have no fixed maximum. |
| `-timeout` | duration | `100ms` | TCP dial timeout for each probe. |
| `-delay` | duration | `10ms` | Dispatch delay between tasks. |
| `-bucket-rate` | int | `100` | Leaky-bucket refill rate. Accepted range `1`-`1000000`. At a higher rate, the refill interval becomes shorter than the runtime timer resolution. |
| `-bucket-capacity` | int | `100` | Leaky-bucket capacity. Accepted range `1`-`1000000`. The bucket fills token by token at construction. |
| `-workers` | int | `10` | Number of scan workers. Accepted range `1`-`1024`. |
| `-disable-api` | bool | `false` | Disable pressure-API polling completely. |
| `-pressure-api` | string | `http://localhost:8080/api/pressure` | Pressure API endpoint. |
| `-pressure-interval` | duration or int seconds | `5s` | Poll interval (a duration such as `200ms`/`5s`, or an integer number of seconds). |
| `-pressure-use-auth` | bool | `false` | Use the authenticated (OAuth) pressure fetcher. |
| `-pressure-auth-url` | string | empty | OAuth auth endpoint. Required with `-pressure-use-auth`. |
| `-pressure-data-url` | string | empty | Comma-separated pressure data endpoints. Required with `-pressure-use-auth`. All sources must succeed, and the command uses the maximum value. |
| `-pressure-client-id` / `-pressure-client-secret` | string | empty | OAuth credentials. Required with `-pressure-use-auth`. |
| `-port-file` | string | optional | A fallback only. The command usually ignores it, because bucket chunks carry the ports. |
| `-progress-interval` | int | `100` | Progress line cadence (count of written scan results). The command writes these lines to stdout, and a matching structured event to stderr. A value that is not positive selects the default `100`. The count advances when results are **written**, so `-output-flush-results` bounds how often a line can appear: with its default of `1000`, `-progress-interval 1` still emits one line per flushed batch, not one per result. Set `-output-flush-results 1` to see a line per result. |
| `-log-level` / `-format` / `-quiet` | — | — | Shared observability flags. |

- **No ping flags.** `scan` does not register `-disable-pre-scan-ping` or
  `-pre-scan-ping-timeout`. Either flag is an unknown-flag error.
- The snapshot does not store `-output-flush-results`. Each resumed run uses
  its current flag value.

### `port-scan validate`

The command uses these values: `-cidr-file` (required), `-port-file` (optional),
`-cidr-ip-col`, `-cidr-ip-cidr-col`, `-format`, and both target expansion flags.
It also uses both CIDR input flags and both port input flags.

For compatibility, it also accepts and verifies these values:

- `-workers`, `-bucket-rate`, and `-bucket-capacity`
- `-pressure-interval`, `-pressure-auth-url`, and `-pressure-data-url`
- `-pressure-client-id`, `-pressure-client-secret`, and `-pressure-use-auth`
- `-pre-scan-ping-timeout`

It accepts and discards these values after flag parsing:

- `-output`, `-timeout`, `-delay`, and `-resume`
- `-pressure-api`, `-disable-api`, and `-disable-pre-scan-ping`
- `-log-level` and `-quiet`

The command validates input files only. It never scans and never pings.

## Interaction Rules and Behavior Notes

- `-cidr-file` is required for every subcommand.
- `-format` accepts `human` or `json` only.
- `-quiet` suppresses the periodic progress output only: the
  `progress cidr=...` line on standard output and the matching `scan_progress`
  event, which one emitter produces together. It filters nothing else, so
  per-result `scan_result` events and error-level lines still go to standard
  error. `-log-level` is the only control for log verbosity. To make a run
  fully silent, use `-quiet -log-level error`.
- `-cidr-ip-col` and `-cidr-ip-cidr-col` must not be empty after the command trims them.
- A target expansion flag must be zero or positive. Zero disables only that limit.
- A resource-limit flag must be zero or positive. Zero disables only that limit.
- A disabled resource limit has no hidden replacement limit.
- A disabled resource limit can exhaust memory or terminate the process.
- The memory estimate is `1000000000 + candidate count * 1500` bytes.
- The count occurs before de-duplication, broadcast removal, and blocklist filtering.
- Rich deny rows contribute zero candidates.
- `pre-ping` requires `-pre-scan-ping-timeout > 0`.
- `generate-buckets` requires `-buckets-out`. `-unreachable-file` is optional.
- `scan` requires `-resume`, and `-pressure-interval` must be more than zero. If
  you set `-pressure-use-auth`, all four auth flags are required
  (`-pressure-auth-url`, `-pressure-data-url`, `-pressure-client-id`,
  `-pressure-client-secret`).
- Batch output names use a shared timestamp suffix. A collision inside the same
  second appends `-n`. `pre-ping` writes `unreachable_results-<ts>.csv`. `scan`
  writes `scan_results-<ts>.csv` and `opened_results-<ts>.csv`.
- **Ctrl+C keeps the results, and `-resume` appends to the same files.** `scan`
  writes rows directly to the final `scan_results-<ts>.csv` /
  `opened_results-<ts>.csv` (no `.tmp` file), so an interrupt keeps every
  already-scanned row in place. The resume snapshot records the chosen output
  paths. A resume from that snapshot therefore appends the remaining rows to the
  *same* files (one continuous file, a single header). It mints no new
  timestamped pair. If you delete the earlier output file before the resume, the
  command creates that file again with a header.

  The first Ctrl+C or Windows Ctrl+Break starts graceful cancellation. Queued
  probes are abandoned. Started probes finish with their original `-timeout`.

  The snapshot rewinds to the lowest unwritten task. A resume can repeat a
  persisted row, but it cannot skip an unwritten task.

  Press Ctrl+C or Ctrl+Break again to force exit code `130`. This emergency
  exit does not promise a current snapshot or finalized output handles.
- **The snapshot records output paths as absolute paths.** A relative `-output`
  (the default `scan_results.csv` is one) resolves against the working directory
  *one time*, when the command first mints the batch paths. The snapshot stores
  the resulting absolute paths. A resume from a different directory — or, on
  Windows, from a different drive — therefore continues to append to the original
  files. It does not create a second set of files next to the new working
  directory silently.

  Compatibility: a snapshot from an older build can still hold a relative path.
  The command resolves that path against the current working directory (the same
  behavior that the older build had, so a same-directory resume does not change)
  and writes it back as an absolute path *in the snapshot that the run saves*.
  That is, the command rewrites it only when the run is interrupted or leaves
  work incomplete. A resume that completes cleanly saves no snapshot, so the
  legacy relative path stays on disk. No work is left to resume, so that path no
  longer has an effect. Path resolution cannot recover the directory that the old
  build never recorded, so a legacy snapshot resumed from a *different* directory
  still resolves against that new directory. The command uses an absolute path in
  a snapshot exactly as recorded.
- The `-unreachable-file` name has a timestamp and is not deterministic. Capture
  the stdout path that `pre-ping` prints when you chain the steps. Do not
  hard-code the name.

## Common Mistakes

1. **You run `scan` without `-resume`** — `scan` no longer builds buckets. Run
   `generate-buckets` first, then pass its `-buckets-out` file as `-resume`.
2. **You pass a ping flag to `scan`** — `-disable-pre-scan-ping` and
   `-pre-scan-ping-timeout` belong to `pre-ping` only. On `scan` they are
   unknown flags (exit `2`). To skip the ping, skip the `pre-ping` step.
3. **You hard-code the unreachable filename** — the name has a timestamp.
   Capture the path that `pre-ping` prints to stdout.
4. **You use the wrong case in a CIDR column name** — column names are
   case-sensitive. Match the CSV header exactly.
5. **You omit `-port-file` for `generate-buckets` in basic mode** — the flag is
   required, unless the input is a rich CSV.

## Examples

### Full three-step pipeline

```bash
# 1. Ping targets; capture the printed unreachable CSV path
UNREACHABLE=$(go run ./cmd/port-scan pre-ping \
  -cidr-file e2e/inputs/cidr_normal.csv \
  -cidr-ip-col source_ip -cidr-ip-cidr-col source_cidr \
  -output e2e/out/scan_results.csv)

# 2. Build the bucket snapshot (targets minus the blocklist)
go run ./cmd/port-scan generate-buckets \
  -cidr-file e2e/inputs/cidr_normal.csv \
  -cidr-ip-col source_ip -cidr-ip-cidr-col source_cidr \
  -port-file e2e/inputs/ports.csv \
  -unreachable-file "$UNREACHABLE" \
  -buckets-out e2e/out/buckets.json

# 3. Scan the buckets
go run ./cmd/port-scan scan \
  -cidr-file e2e/inputs/cidr_normal.csv \
  -cidr-ip-col source_ip -cidr-ip-cidr-col source_cidr \
  -resume e2e/out/buckets.json \
  -port-file e2e/inputs/ports.csv \
  -output e2e/out/scan_results.csv
```

### Skip pinging (bucket all targets)

```bash
go run ./cmd/port-scan generate-buckets \
  -cidr-file rich.csv -buckets-out out/buckets.json     # no -unreachable-file
go run ./cmd/port-scan scan \
  -cidr-file rich.csv -resume out/buckets.json -output out/
```

### Authenticated Pressure API (scan only)

```bash
go run ./cmd/port-scan scan \
  -cidr-file rich.csv -resume out/buckets.json \
  -pressure-use-auth \
  -pressure-auth-url "https://auth.example.com/oauth/token" \
  -pressure-data-url "https://api1.example.com/pressure,https://api2.example.com/pressure" \
  -pressure-client-id "your-client-id" \
  -pressure-client-secret "your-client-secret"
```

---
**Revised**: 2026-07-22 | **Author**: docs-team

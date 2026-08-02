# CLI Flags Reference

This is the complete CLI flag reference for `port-scan-mk3`, sourced from current
parser behavior in:

- `pkg/config/parse_for.go` (`ParseFor`, per-subcommand flag registration)
- `cmd/port-scan/main.go` and `cmd/port-scan/command_handlers.go` (dispatch, usage)

## Command Scope

`port-scan` is a three-step pipeline plus a `validate` helper. **Each subcommand
registers only the flags it owns** (`ParseFor(command, args)`); passing a flag
that a subcommand does not register is an unknown-flag error (exit `2`). This is
what makes "`scan` never pings" structural rather than cosmetic — `scan` does not
register `-disable-pre-scan-ping` or `-pre-scan-ping-timeout` at all.

| Subcommand | Purpose | Required flags |
|------------|---------|----------------|
| `preping` | Ping unique target IPs, write `unreachable_results-<ts>.csv` | `-cidr-file` |
| `generate-buckets` | Build the resume bucket snapshot from targets − blocklist | `-cidr-file`, `-buckets-out` |
| `scan` | Pure TCP scan of a bucket snapshot | `-cidr-file`, `-resume` |
| `validate` | Validate CIDR/port inputs, no scan | `-cidr-file` |

## Per-command flag tables

Flags shared by every subcommand: `-cidr-file` (required), `-cidr-ip-col`
(default `ip`), `-cidr-ip-cidr-col` (default `ip_cidr`), `-log-level` (default
`info`), `-format` (`human`|`json`, default `human`), `-quiet`.

### `port-scan preping`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `-cidr-file` | string | required | Path to CIDR/rich CSV input file. |
| `-cidr-ip-col` / `-cidr-ip-cidr-col` | string | `ip` / `ip_cidr` | Case-sensitive column mapping. |
| `-pre-scan-ping-timeout` | duration | `100ms` | Reply-wait timeout for each ping reachability check. Must be > 0. On Windows an internal fixed startup allowance is added on top of this for the process wall-clock ceiling so a fast reply is not killed during ping launch; the reply-wait itself still uses this value. |
| `-workers` | int | `10` | Concurrent ping workers. |
| `-output` | string | `scan_results.csv` | Output anchor path; the directory and shared timestamp suffix for `unreachable_results-<ts>.csv`. |
| `-progress-interval` | int | `100` | Progress line cadence (count of processed unique IPs); emitted to stderr. |
| `-log-level` / `-format` / `-quiet` | — | — | Shared observability flags. |

- No `-port-file` (ping is per-IP, ports are irrelevant).
- No `-disable-pre-scan-ping` — to skip pinging, do not run this step.
- Prints the resolved `unreachable_results-<ts>.csv` path to **stdout** for chaining;
  progress lines go to **stderr**.

### `port-scan generate-buckets`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `-cidr-file` | string | required | Path to CIDR/rich CSV input file. |
| `-cidr-ip-col` / `-cidr-ip-cidr-col` | string | `ip` / `ip_cidr` | Case-sensitive column mapping. |
| `-port-file` | string | optional | Port list (`<port>/tcp` lines). Required in basic (non-rich) mode; ignored in rich mode (the per-target port is read from the CSV). |
| `-unreachable-file` | string | optional | Blocklist CSV (a `preping` output). Its `ip` column is subtracted from the target set. Omit to bucket all targets. |
| `-buckets-out` | string | **required** | Output path for the bucket snapshot (the resume `Snapshot` JSON). |
| `-workers` | int | `10` | Parallel per-CIDR-group chunk builders. Output is deterministic (CIDR-sorted) regardless of worker count. |
| `-progress-interval` | int | `100` | Progress line cadence (count of processed CIDR groups); emitted to stderr. |
| `-log-level` / `-format` / `-quiet` | — | — | Shared observability flags. |

- Performs **no network I/O**.
- Always stamps `pre_scan_ping.enabled=true` in the snapshot, so `scan` never pings.

### `port-scan scan`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `-cidr-file` | string | required | Rich/basic CSV — the source of truth for target metadata (rebuilt on every run). |
| `-cidr-ip-col` / `-cidr-ip-cidr-col` | string | `ip` / `ip_cidr` | Case-sensitive column mapping. |
| `-resume` | string | **required** | Bucket snapshot file to scan. Read at start; **updated in place at this path** on interrupt/error. |
| `-output` | string | `scan_results.csv` | Output anchor; directory and shared suffix for `scan_results-<ts>.csv` and `opened_results-<ts>.csv`. |
| `-timeout` | duration | `100ms` | TCP dial timeout per probe. |
| `-delay` | duration | `10ms` | Dispatch delay between tasks. |
| `-bucket-rate` | int | `100` | Leaky-bucket refill rate. |
| `-bucket-capacity` | int | `100` | Leaky-bucket capacity. |
| `-workers` | int | `10` | Number of scan workers. |
| `-disable-api` | bool | `false` | Disable pressure-API polling completely. |
| `-pressure-api` | string | `http://localhost:8080/api/pressure` | Pressure API endpoint. |
| `-pressure-interval` | duration or int seconds | `5s` | Poll interval (duration like `200ms`/`5s`, or integer seconds). |
| `-pressure-use-auth` | bool | `false` | Use the authenticated (OAuth) pressure fetcher. |
| `-pressure-auth-url` | string | empty | OAuth auth endpoint; required with `-pressure-use-auth`. |
| `-pressure-data-url` | string | empty | Comma-separated pressure data endpoints; required with `-pressure-use-auth`. All sources must succeed; the max value is used. |
| `-pressure-client-id` / `-pressure-client-secret` | string | empty | OAuth credentials; required with `-pressure-use-auth`. |
| `-port-file` | string | optional | Fallback only; normally ignored because bucket chunks carry ports. |
| `-progress-interval` | int | `100` | Progress line cadence. |
| `-log-level` / `-format` / `-quiet` | — | — | Shared observability flags. |

- **No ping flags.** `-disable-pre-scan-ping` / `-pre-scan-ping-timeout` are not
  registered; passing either is an unknown-flag error.

### `port-scan validate`

`-cidr-file` (required) · `-port-file` (optional) · `-cidr-ip-col` ·
`-cidr-ip-cidr-col` · `-format` · `-log-level` · `-quiet`. Parses and validates
inputs only; never scans or pings.

## Interaction Rules and Behavior Notes

- `-cidr-file` is required for every subcommand.
- `-format` only accepts `human` or `json`.
- `-cidr-ip-col` and `-cidr-ip-cidr-col` must be non-empty after trimming.
- `preping` requires `-pre-scan-ping-timeout > 0`.
- `generate-buckets` requires `-buckets-out`; `-unreachable-file` is optional.
- `scan` requires `-resume`; `-pressure-interval` must be positive; when
  `-pressure-use-auth` is set, all four auth flags are required
  (`-pressure-auth-url`, `-pressure-data-url`, `-pressure-client-id`,
  `-pressure-client-secret`).
- Batch output naming uses a shared timestamp suffix; same-second collisions
  append `-n`. `preping` writes `unreachable_results-<ts>.csv`; `scan` writes
  `scan_results-<ts>.csv` and `opened_results-<ts>.csv`.
- **Ctrl+C keeps results; `-resume` appends to the same files.** `scan` writes
  rows straight to the final `scan_results-<ts>.csv` / `opened_results-<ts>.csv`
  (no `.tmp`), so an interrupt leaves every already-scanned row in place. The
  chosen output paths are recorded in the resume snapshot, so resuming that
  snapshot appends the remaining rows to the *same* files (one continuous file,
  a single header) instead of minting a new timestamped pair. If the prior
  output file was deleted before resuming, it is recreated with a header.
- **Output paths are recorded as absolute paths.** A relative `-output` (the
  default `scan_results.csv` is one) is resolved against the working directory
  *once*, when the batch paths are first minted, and the resulting absolute
  paths are what the snapshot stores. Resuming from a different directory — or,
  on Windows, a different drive — therefore keeps appending to the original
  files instead of silently creating a second set next to the new working
  directory. Compatibility: a snapshot written by an older build may still hold
  a relative path; it is resolved against the current working directory (what
  the older build did implicitly, so a same-directory resume is unchanged) and
  rewritten as absolute *in the snapshot that run saves* — that is, only when the
  run is interrupted or leaves work incomplete. A resume that finishes cleanly
  saves no snapshot at all, so the legacy relative path stays on disk; nothing is
  left to resume, so it no longer matters. Resolving a legacy path cannot recover
  the directory the old build never recorded, so a legacy snapshot resumed from a
  *different* directory still resolves against that new directory. Absolute paths
  already recorded in a snapshot are used exactly as recorded.
- The `-unreachable-file` name is timestamped and non-deterministic — capture
  `preping`'s printed stdout path when chaining rather than hard-coding it.

## Common Mistakes

1. **Running `scan` without `-resume`** — `scan` no longer builds buckets. Run
   `generate-buckets` first and pass its `-buckets-out` file as `-resume`.
2. **Passing a ping flag to `scan`** — `-disable-pre-scan-ping` /
   `-pre-scan-ping-timeout` are `preping`-only; on `scan` they are unknown flags
   (exit `2`). Skip pinging by skipping the `preping` step.
3. **Hard-coding the unreachable filename** — it is timestamped; capture the path
   `preping` prints to stdout.
4. **Wrong CIDR column casing** — column names are case-sensitive; match the CSV
   header exactly.
5. **Omitting `-port-file` in basic mode `generate-buckets`** — required unless
   the input is rich CSV.

## Examples

### Full three-step pipeline

```bash
# 1. Ping targets; capture the printed unreachable CSV path
UNREACHABLE=$(go run ./cmd/port-scan preping \
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

# CLI Flags Reference

This is the complete CLI flag reference for `port-scan-mk3`, sourced from current parser behavior in:

- `pkg/config/config.go`
- `cmd/port-scan/main.go`

## Command Scope

- `validate` and `scan` both parse the same flag set.
- Some flags are only operationally meaningful in `scan` (for example pressure/rate/worker controls).

## Complete Flag Table

| Flag | Type | Default | Command | Description |
|------|------|---------|---------|-------------|
| `-cidr-file` | string | none (required) | `validate`, `scan` | Path to CIDR CSV input file. |
| `-port-file` | string | optional | `validate`, `scan` | Path to port list file (`<port>/tcp` lines). Required when CIDR input is not rich mode. |
| `-output` | string | `scan_results.csv` | `validate`, `scan` | Output anchor path for scan batch files; output directory also controls default resume fallback location. |
| `-timeout` | duration | `100ms` | `validate`, `scan` | TCP dial timeout per probe. Primarily used by `scan`. |
| `-delay` | duration | `10ms` | `validate`, `scan` | Dispatch delay between tasks. Primarily used by `scan`. |
| `-bucket-rate` | int | `100` | `validate`, `scan` | Leaky bucket refill rate. Primarily used by `scan`. |
| `-bucket-capacity` | int | `100` | `validate`, `scan` | Leaky bucket capacity. Primarily used by `scan`. |
| `-workers` | int | `10` | `validate`, `scan` | Number of scan workers. Primarily used by `scan`. |
| `-pressure-api` | string | `http://localhost:8080/api/pressure` | `validate`, `scan` | Pressure API endpoint used for pause/resume control. |
| `-pressure-interval` | duration or integer seconds | `5s` | `validate`, `scan` | Poll interval for pressure API. Accepts duration (for example `200ms`, `5s`) or integer seconds (for example `7`). |
| `-pressure-use-auth` | bool | `false` | `validate`, `scan` | Use authenticated pressure fetcher with OAuth flow. |
| `-pressure-auth-url` | string | empty | `validate`, `scan` | OAuth auth endpoint URL. Required when `-pressure-use-auth` is set. |
| `-pressure-data-url` | string | empty | `validate`, `scan` | Pressure data endpoint URL. Required when `-pressure-use-auth` is set. |
| `-pressure-client-id` | string | empty | `validate`, `scan` | OAuth client ID. Required when `-pressure-use-auth` is set. |
| `-pressure-client-secret` | string | empty | `validate`, `scan` | OAuth client secret. Required when `-pressure-use-auth` is set. |
| `-disable-api` | bool | `false` | `validate`, `scan` | Disable pressure API polling completely. |
| `-quiet` | bool | `false` | `validate`, `scan` | Suppress console logs, keep only pressure API logs. |
| `-resume` | string | empty | `validate`, `scan` | Resume state file path. If set, load/save uses this exact path. |
| `-log-level` | string | `info` | `validate`, `scan` | Runtime log level: `debug`, `info`, `error`. |
| `-format` | string | `human` | `validate`, `scan` | User-facing output format: `human` or `json`. |
| `-cidr-ip-col` | string | `ip` | `validate`, `scan` | Case-sensitive CIDR CSV column name used as IP selector source. |
| `-cidr-ip-cidr-col` | string | `ip_cidr` | `validate`, `scan` | Case-sensitive CIDR CSV column name used as boundary CIDR source. |

## Interaction Rules and Behavior Notes

- `-cidr-file` is required.
- `-port-file` is required only when CIDR input is not rich mode.
- `-format` only accepts `human` or `json`.
- `-pressure-interval` must be positive; invalid format or non-positive values are rejected.
- When `-pressure-use-auth` is set, all four auth flags are required:
  - `-pressure-auth-url`
  - `-pressure-data-url`
  - `-pressure-client-id`
  - `-pressure-client-secret`
- `-cidr-ip-col` and `-cidr-ip-cidr-col` must be non-empty after trimming.
- Resume write path behavior:
  - If `-resume` is set, state is read from and written back to that same path.
  - If `-resume` is not set, fallback save path is `<output-dir>/resume_state.json`.

## Common Mistakes

1. Using wrong CIDR column casing
- Problem: CSV has `source_ip` but command uses `-cidr-ip-col Source_IP`.
- Fix: Use exact case-sensitive column names.

2. Invalid `-pressure-interval`
- Problem: passing `0`, `-1s`, or malformed durations.
- Fix: use positive values such as `200ms`, `5s`, or integer `7`.

3. Assuming fixed output filename
- Problem: expecting a single `scan_results.csv` file after each run.
- Fix: scan output is timestamped batch format `scan_results-YYYYMMDDTHHMMSSZ[-n].csv`.

4. Forgetting explicit resume pinning
- Problem: restart run but cannot find expected resume file.
- Fix: pass `-resume <path>` to keep load/save path explicit.

## Examples

### Quiet Mode

Suppress console output while keeping pressure API logs visible:

```bash
port-scan scan -cidr-file targets.csv -quiet
```

### Authenticated Pressure API

Use OAuth-authenticated pressure API:

```bash
port-scan scan -cidr-file targets.csv \
  -pressure-use-auth \
  -pressure-auth-url "https://auth.example.com/oauth/token" \
  -pressure-data-url "https://api.example.com/pressure" \
  -pressure-client-id "your-client-id" \
  -pressure-client-secret "your-client-secret"
```

### Quiet Mode with Authenticated Pressure API

Suppress console logs while using authenticated pressure API:

```bash
port-scan scan -cidr-file targets.csv \
  -quiet \
  -pressure-use-auth \
  -pressure-auth-url "https://auth.example.com/oauth/token" \
  -pressure-data-url "https://api.example.com/pressure" \
  -pressure-client-id "your-client-id" \
  -pressure-client-secret "your-client-secret"
```

---
**Revised**: 2026-04-13 | **Author**: docs-team

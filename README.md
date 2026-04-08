# Port Scan MK3

Developer-first TCP port scanner CLI in Go with fail-fast input validation, pressure-aware pacing, resumable scanning, and e2e verification.

## Prerequisites

- Go `1.24.0` (toolchain `go1.24.4`)
- Docker + `docker compose` (required for e2e only)

## Quick Start

Validate input only (no network scan):

```bash
go run ./cmd/port-scan validate \
  -cidr-file e2e/inputs/cidr_normal.csv \
  -port-file e2e/inputs/ports.csv \
  -format human
```

Run a real scan:

```bash
go run ./cmd/port-scan scan \
  -cidr-file e2e/inputs/cidr_normal.csv \
  -port-file e2e/inputs/ports.csv
```

## Input Contracts

- CIDR CSV (default mode):
  - Required columns: `ip`, `ip_cidr`
  - Optional columns: `fab_name`, `cidr_name`
  - Column mapping flags are case-sensitive: `-cidr-ip-col`, `-cidr-ip-cidr-col`
- Rich CSV mode is auto-detected when all rich fields exist:
  - `src_ip`, `src_network_segment`, `dst_ip`, `dst_network_segment`
  - `service_label`, `protocol`, `port`, `decision`, `matched_policy_id`, `reason`
- Port file format: one line per port in `<port>/tcp` (for example `443/tcp`)
  - Required in default CIDR mode
  - Optional in rich CSV mode

## Commands

- `validate`: parse and validate input files only
- `scan`: run full orchestration (dispatch, probe, output, resume persistence)

Exit code behavior:

- `0`: success
- `1`: validation failed (`validate`) or scan runtime error (`scan`)
- `2`: CLI parsing/config error
- `130`: scan canceled by `SIGINT` (`Ctrl+C`)

## How the Scan Pipeline Works

1. Parse CLI flags and validate required inputs.
2. Load CIDR CSV and port list, then apply fail-fast validation.
3. Expand selectors into concrete IPv4 targets and build scan tasks.
4. Dispatch tasks with rate control and optional pressure-based pause.
5. Run TCP probes in worker pool and stream progress events.
6. Write timestamped batch output files:
   - `scan_results-YYYYMMDDTHHMMSSZ[-n].csv`
   - `opened_results-YYYYMMDDTHHMMSSZ[-n].csv`
7. Save resume state when canceled, failed, or partially complete.

## Output and Resume Behavior

- `-output` controls output directory; result files are always timestamped batches.
- Default batch naming is collision-safe within the same second (`-1`, `-2`, ... suffix).
- Resume state save path:
  - If `-resume` is set: save and load from that exact path.
  - If `-resume` is not set: save fallback to `<output-dir>/resume_state.json`.
- Resume state auto-save does not mean auto-load:
  - Loading previous progress requires passing `-resume <path>`.

## Dashboard and Logging

- `scan` enables the rich dashboard by default when `stderr` is attached to a TTY and `-format human` is used.
- Rich dashboard output is written to `stderr`.
- If `stderr` is not a TTY, or if `-format json` is selected, `scan` falls back to non-rich output.
- No new CLI flags are added for the UI in this version.

## Flags Quick Reference

This section lists high-impact flags. Full definitions are in [All flags](docs/cli/flags.md).

| Flag | Typical Use |
|------|-------------|
| `-cidr-file` | CIDR input CSV path (required) |
| `-port-file` | Port list path (required in default mode; optional in rich mode) |
| `-cidr-ip-col` / `-cidr-ip-cidr-col` | Map custom CSV column names |
| `-output` | Choose output directory anchor |
| `-resume` | Read/write state from explicit path |
| `-disable-api` | Disable pressure API polling |
| `-pressure-api` / `-pressure-interval` | Configure pressure-based pause control |
| `-workers` / `-timeout` / `-delay` | Tune concurrency and probe pacing |
| `-log-level` / `-format` | Runtime visibility (`human` or `json`) |

## Repository Map

- `cmd/port-scan`: CLI composition root, command routing, user I/O, exit codes
- `pkg/config`: flag parsing and configuration validation
- `pkg/input`: CIDR/rich input loading and row-level validation
- `pkg/task`: selector expansion and execution-key helpers
- `pkg/scanapp`: scan orchestration (load, plan, dispatch, execute, aggregate, resume, outputs)
- `pkg/scanner`: single TCP probe primitive
- `pkg/writer`: fixed CSV output contract and open-only projection
- `pkg/speedctrl`: manual/API pause controller
- `pkg/state`: resume state persistence and signal helpers
- `tests/integration`: integration contracts
- `e2e`: dockerized end-to-end verification and artifact checks

## Operational Notes and Constraints

- IPv4 only (selectors, CIDR parsing, and expansion paths).
- Port input accepts `<port>/tcp` only.
- Pressure API polling fails hard after 3 consecutive failures.
- Pressure threshold defaults to `90` and is not exposed as CLI flag.
- Pause gate blocks new dispatch only; in-flight worker probes continue.
- Dispatch order is chunk-serial (not cross-CIDR fair round-robin).
- E2E requires Docker runtime and `docker compose`.

## Testing and Verification

- Unit + integration: `go test ./...`
- Coverage gate (85%): `bash scripts/coverage_gate.sh`
- E2E gate: `bash e2e/run_e2e.sh`
- Speed-control verification report: `bash e2e/speedcontrol/run_speedcontrol_e2e.sh`

## Secret Scanning (gitleaks)

- Install gitleaks (example on macOS): `brew install gitleaks`
- Enable pre-commit hook (one-time): `bash scripts/setup-githooks.sh`
- Manual staged scan (same as hook): `gitleaks git --staged --redact --config=.gitleaks.toml .`
- CI scan is enforced on every `push` and `pull_request` by `.github/workflows/gitleaks.yml`.

## Docs

- [All flags](docs/cli/flags.md)
- [Scenario cookbook](docs/cli/scenarios.md)
- [E2E overview](docs/e2e/overview.md)
- [Speed-control E2E](docs/e2e/speedcontrol.md)
- [Architecture diagram](docs/architecture/diagram.html)

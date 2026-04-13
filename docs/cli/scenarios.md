# CLI Scenario Cookbook

Copy-paste scenarios for developers and contributors. Run from repo root unless noted.

## Scenario 1: Basic scan with defaults

Goal: Run a baseline scan with required inputs only.

Command:
```bash
go run ./cmd/port-scan scan \
  -cidr-file e2e/inputs/cidr_normal.csv \
  -port-file e2e/inputs/ports.csv
```

Expected:
- Scan finishes with exit code `0`.
- Batch outputs appear in current directory as `scan_results-*.csv` and `opened_results-*.csv`.

Troubleshooting:
- If parser rejects inputs, run Scenario 3/4 (`validate`) first.

## Scenario 2: Scan with custom CIDR column mapping

Goal: Use non-default CIDR CSV column names.

Command:
```bash
go run ./cmd/port-scan scan \
  -cidr-file e2e/inputs/cidr_normal.csv \
  -port-file e2e/inputs/ports.csv \
  -cidr-ip-col source_ip \
  -cidr-ip-cidr-col source_cidr
```

Expected:
- Case-sensitive mapping is applied.
- Scan runs only on resolved targets from mapped columns.

Troubleshooting:
- Column names are case-sensitive; verify header spelling exactly.

## Scenario 2A: Rich mode scan without port file

Goal: Use rich CSV input (`src_ip`/`dst_ip`/.../`port`) and omit `-port-file`.

Command:
```bash
go run ./cmd/port-scan scan \
  -cidr-file tests/integration/testdata/rich_input/dedup_context.csv \
  -disable-api=true
```

Expected:
- Rich mode is auto-detected by header.
- Scan runs without requiring `-port-file`.
- Output still includes rich context columns such as `policy_id` and `execution_key`.

## Scenario 2B: Observe rich dashboard output

Goal: See the rich dashboard render during an interactive scan.

Command:
```bash
go run ./cmd/port-scan scan \
  -cidr-file tests/integration/testdata/ip_aware/cidr_normal.csv \
  -port-file tests/integration/testdata/ip_aware/ports.csv \
  -format human
```

Expected:
- When `stderr` is attached to a TTY, the rich dashboard appears by default.
- Rich dashboard output is sent to `stderr`.
- If you redirect `stderr` or switch to `-format json`, the scan falls back to non-rich output.

Troubleshooting:
- Run in an interactive terminal and avoid redirecting `stderr` (for example `2>` or `2>&1`) if you want rich output.

## Scenario 3: Validate inputs (human format)

Goal: Pre-flight check input files without scanning.

Command:
```bash
go run ./cmd/port-scan validate \
  -cidr-file e2e/inputs/cidr_normal.csv \
  -port-file e2e/inputs/ports.csv \
  -cidr-ip-col source_ip \
  -cidr-ip-cidr-col source_cidr \
  -format human
```

Expected:
- Exit code `0`.
- Human-readable success text.

Troubleshooting:
- Non-zero exit means fail-fast validation detected schema/range issues.

## Scenario 4: Validate inputs (JSON format)

Goal: Integrate validation into scripts/CI.

Command:
```bash
go run ./cmd/port-scan validate \
  -cidr-file e2e/inputs/cidr_normal.csv \
  -port-file e2e/inputs/ports.csv \
  -cidr-ip-col source_ip \
  -cidr-ip-cidr-col source_cidr \
  -format json
```

Expected:
- JSON output with validity fields.
- Exit code `0` for valid input, `1` for invalid input.

Troubleshooting:
- If JSON is malformed in scripts, confirm no extra shell output is mixed in.

## Scenario 5: Scan with pressure control enabled

Goal: Pause/resume dispatch based on pressure API.

Command:
```bash
go run ./cmd/port-scan scan \
  -cidr-file e2e/inputs/cidr_normal.csv \
  -port-file e2e/inputs/ports.csv \
  -pressure-api http://localhost:8080/api/pressure \
  -pressure-interval 500ms
```

Expected:
- Scanner polls pressure API and adjusts dispatch gate accordingly.
- Logs show pressure-triggered pause/resume transitions when threshold is crossed.

Troubleshooting:
- Use `-disable-api=true` to isolate API effects during debugging.

## Scenario 6: Pressure API failures (escalation behavior)

Goal: Confirm fatal cutoff at third consecutive API failure.

Command:
```bash
go run ./cmd/port-scan scan \
  -cidr-file e2e/inputs/cidr_fail.csv \
  -port-file e2e/inputs/ports.csv \
  -cidr-ip-col source_ip \
  -cidr-ip-cidr-col source_cidr \
  -pressure-api http://127.0.0.1:9/api/pressure \
  -pressure-interval 200ms
```

Expected:
- First and second API failures are logged.
- Third consecutive failure terminates scan with non-zero exit.
- Resume state file is written.

Troubleshooting:
- Verify network route/firewall if this fails unexpectedly on first call.

## Scenario 7: Explicit resume path workflow

Goal: Pin state file location for deterministic resume behavior.

Command:
```bash
go run ./cmd/port-scan scan \
  -cidr-file e2e/inputs/cidr_normal.csv \
  -port-file e2e/inputs/ports.csv \
  -output e2e/out/scan_results.csv \
  -resume e2e/out/resume_state_manual.json
```

Expected:
- Scan loads and saves state through `e2e/out/resume_state_manual.json`.
- Subsequent run with same `-resume` continues from saved state.

Troubleshooting:
- If no state file appears, ensure run ended in cancellation or failure path.

## Scenario 8: Cancellation with SIGINT and resume

Goal: Validate interruption handling and continuation after `SIGINT`.

Command:
```bash
go run ./cmd/port-scan scan \
  -cidr-file e2e/inputs/cidr_normal.csv \
  -port-file e2e/inputs/ports.csv \
  -output e2e/out/scan_results.csv
# Press Ctrl+C (SIGINT) during run, then restart with:
go run ./cmd/port-scan scan \
  -cidr-file e2e/inputs/cidr_normal.csv \
  -port-file e2e/inputs/ports.csv \
  -output e2e/out/scan_results.csv \
  -resume e2e/out/resume_state.json
```

Expected:
- First run exits due to cancellation and persists resume state.
- Second run resumes without duplicate or missing records.

Troubleshooting:
- Confirm resume path: default is `<output-dir>/resume_state.json` if `-resume` omitted.

## Scenario 9: Same-second output collision naming

Goal: Observe `-n` suffix allocation when runs start within same second.

Command:
```bash
go run ./cmd/port-scan scan -cidr-file e2e/inputs/cidr_normal.csv -port-file e2e/inputs/ports.csv -output e2e/out/scan_results.csv
go run ./cmd/port-scan scan -cidr-file e2e/inputs/cidr_normal.csv -port-file e2e/inputs/ports.csv -output e2e/out/scan_results.csv
```

Expected:
- Files follow batch naming:
  - `scan_results-YYYYMMDDTHHMMSSZ.csv`
  - `scan_results-YYYYMMDDTHHMMSSZ-1.csv`
- `opened_results` uses the same sequence as `scan_results` for each batch.

Troubleshooting:
- If suffix does not appear, runs may have started in different seconds.

## Scenario 10: e2e parity execution

Goal: Validate production-like e2e behavior with Docker mocks and report artifacts.

Command:
```bash
bash e2e/run_e2e.sh
```

Expected:
- Normal and failure scenarios pass (`api_5xx`, `api_timeout`, `api_conn_fail`).
- Artifacts created under `e2e/out/`: report files, batch CSVs, and `resume_state_*` files.

Troubleshooting:
- If e2e fails early, verify Docker daemon and `docker compose` availability.

---
**Revised**: 2026-04-13 | **Author**: docs-team

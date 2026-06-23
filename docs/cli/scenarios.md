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

## Scenario 2C: Pre-scan ping, unreachable output, and disabled mode

Goal: Verify the default pre-scan ping flow and the `-disable-pre-scan-ping` fallback.

Command:
```bash
go run ./cmd/port-scan scan \
  -cidr-file tests/integration/testdata/ip_aware/cidr_fail.csv \
  -port-file tests/integration/testdata/ip_aware/ports.csv \
  -output e2e/out/scan_results.csv \
  -disable-api=true
```

Expected:
- The test data uses the default `ip` / `ip_cidr` headers, so no CIDR mapping flags are needed.
- With the flag omitted, pre-scan ping runs before TCP scan planning.
- `unreachable_results-<suffix>.csv` is flushed and finalized before any TCP dial starts.
- `scan_results-<suffix>.csv`, `opened_results-<suffix>.csv`, and `unreachable_results-<suffix>.csv` share the same suffix.
- IPs that fail ping are written only to `unreachable_results-<suffix>.csv` and are skipped from TCP scan.
- Windows and Unix follow the same high-level contract even though the underlying ping command differs.

Disabled mode:
```bash
go run ./cmd/port-scan scan \
  -cidr-file tests/integration/testdata/ip_aware/cidr_normal.csv \
  -port-file tests/integration/testdata/ip_aware/ports.csv \
  -output e2e/out/scan_results.csv \
  -disable-pre-scan-ping=true \
  -disable-api=true
```

Expected:
- No pre-scan ping stage runs.
- `unreachable_results-<suffix>.csv` is still created as a final batch file, but it contains header only.
- TCP scanning behaves like the prior direct-scan flow.

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
- `opened_results` and `unreachable_results` use the same sequence as `scan_results` for each batch.

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

## Scenario 11: From-scratch pre-processing flow

Goal: Filter a firewall-policy rich CSV with `preprocess`, then hand the output directly to `port-scan`.

Commands:
```bash
# Step 1 — filter targets by closed CIDRs
go run ./cmd/preprocess \
  --input filtered-targets/dc-east/20260503T120000Z/opened_targets.csv \
  --cleaned-cidrs cleaned_cidrs.csv \
  --fab-name dc-east \
  --output-dir ./scan-input

# Step 2 — scan the filtered input
# Replace <timestamp> with the value printed by step 1 ("Output written to: ...")
go run ./cmd/port-scan scan \
  -cidr-file scan-input/dc-east/<timestamp>/input.csv \
  -disable-api=true
```

Expected:
- Step 1 prints a summary (`total / kept / dropped`) and the output path to stderr.
- Step 2 runs a full rich-mode scan; `opened_results-*.csv` contains only open ports.

Troubleshooting:
- If `preprocess` reports 0 kept rows, verify that the `--fab-name` value matches the `fab` column in `cleaned_cidrs.csv` (case-sensitive).
- If `port-scan` rejects the input, confirm the output file path from step 1 was copied exactly.

## Scenario 12: Re-scan pre-processing flow

Goal: Promote a minimal `host,port` opened-targets CSV to rich format with `enrich-targets`, filter it with `preprocess`, then re-scan.

Commands:
```bash
# Step 1 — enrich minimal CSV to rich format
go run ./cmd/enrich-targets \
  --input previous-scanned/dc-east/20260503T120000Z/opened_targets.csv \
  --cidr-list cidrs.csv \
  --service-map services.csv \
  --output enriched.csv

# Step 2 — filter enriched targets by closed CIDRs
go run ./cmd/preprocess \
  --input enriched.csv \
  --cleaned-cidrs cleaned_cidrs.csv \
  --fab-name dc-east \
  --output-dir ./scan-input

# Step 3 — re-scan
# Replace <timestamp> with the value printed by step 2
go run ./cmd/port-scan scan \
  -cidr-file scan-input/dc-east/<timestamp>/input.csv \
  -disable-api=true
```

Expected:
- Step 1 reports `Enriched N rows from M input rows` on stderr; rows with unparseable ports are skipped with a per-row warning.
- Step 2 prints total/kept/dropped summary and the output path.
- Step 3 runs a full rich-mode scan using the previously discovered hosts as targets.

Troubleshooting:
- If `enrich-targets` fails with a missing-columns error, confirm the input CSV has `host` and `port` headers (case-insensitive).
- If `dst_network_segment` mapping produces `<host>/32` for most rows, the CIDR reference list (`--cidr-list`) may not cover those hosts; verify coverage with `cidr-compare`.

---
**Revised**: 2026-05-03 | **Author**: docs-team

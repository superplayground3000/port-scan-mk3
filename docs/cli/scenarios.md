# CLI Scenario Cookbook

Copy-paste scenarios for developers and contributors. Run from repo root unless
noted.

`port-scan` is a **three-step pipeline**: `preping` -> `generate-buckets` ->
`scan`. `scan` requires a bucket snapshot from `-resume`. It never pings, and it
no longer accepts the ping flags. See [All flags](flags.md) and the
[2.0.0 release notes](../release-notes/2.0.0.md) for the migration.

## Scenario 0: Full three-step pipeline (basic mode)

Goal: Run the complete pipeline end to end with a basic CIDR CSV + port file.

Commands:
```bash
# 1. Ping unique targets; capture the printed unreachable CSV path (stdout)
UNREACHABLE=$(go run ./cmd/port-scan preping \
  -cidr-file e2e/inputs/cidr_normal.csv \
  -cidr-ip-col source_ip -cidr-ip-cidr-col source_cidr \
  -output e2e/out/scan_results.csv)

# 2. Build the bucket snapshot over targets minus the blocklist
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

Expected:
- `preping` prints its `unreachable_results-<ts>.csv` path to stdout and a
  `preping: N/N (100.0%)` summary to stderr.
- `generate-buckets` writes `e2e/out/buckets.json` and prints a
  `generate-buckets: N/N (100.0%)` summary.
- `scan` finishes with exit `0` and writes `scan_results-*.csv` /
  `opened_results-*.csv` under `e2e/out/`.

Troubleshooting:
- If `scan` reports `-resume is required`, you skipped step 2.
- If the parser rejects inputs, run Scenario 5 (`validate`) first.

## Scenario 1: Skip pinging (bucket all targets)

Goal: Reproduce the old `-disable-pre-scan-ping=true` behavior — scan every
target without a reachability filter.

Commands:
```bash
# No preping step; no -unreachable-file
go run ./cmd/port-scan generate-buckets \
  -cidr-file e2e/inputs/cidr_normal.csv \
  -cidr-ip-col source_ip -cidr-ip-cidr-col source_cidr \
  -port-file e2e/inputs/ports.csv \
  -buckets-out e2e/out/buckets.json

go run ./cmd/port-scan scan \
  -cidr-file e2e/inputs/cidr_normal.csv \
  -cidr-ip-col source_ip -cidr-ip-cidr-col source_cidr \
  -resume e2e/out/buckets.json \
  -port-file e2e/inputs/ports.csv \
  -output e2e/out/scan_results.csv -disable-api
```

Expected:
- The snapshot covers all targets and still stamps `pre_scan_ping.enabled=true`,
  so `scan` never pings.
- `scan` scans every target directly.

## Scenario 2: Rich mode (no port file)

Goal: Use rich CSV input (`src_ip`/`dst_ip`/.../`port`) — the ports come from the
CSV, so you do not need `-port-file` at any step.

Commands:
```bash
go run ./cmd/port-scan generate-buckets \
  -cidr-file tests/integration/testdata/rich_input/dedup_context.csv \
  -buckets-out e2e/out/rich_buckets.json

go run ./cmd/port-scan scan \
  -cidr-file tests/integration/testdata/rich_input/dedup_context.csv \
  -resume e2e/out/rich_buckets.json \
  -output e2e/out/scan_results.csv -disable-api
```

Expected:
- `port-scan` detects rich mode from the header. Each chunk carries its per-target port.
- Output includes rich context columns such as `policy_id` and `execution_key`.

## Scenario 3: Custom CIDR column mapping

Goal: Use non-default CIDR CSV column names (apply the same names to every step).

Command (preping shown — pass the same flags to generate-buckets and scan):
```bash
go run ./cmd/port-scan preping \
  -cidr-file e2e/inputs/cidr_normal.csv \
  -cidr-ip-col source_ip \
  -cidr-ip-cidr-col source_cidr \
  -output e2e/out/scan_results.csv
```

Troubleshooting:
- Column names are case-sensitive. Make sure that the header spelling is exact.
  Use the same mapping flags on `preping`, `generate-buckets`, and `scan`.

## Scenario 4: Observe rich dashboard output (scan)

Goal: See the rich dashboard while an interactive scan runs.

Command (build a bucket file first, as in Scenario 2):
```bash
go run ./cmd/port-scan scan \
  -cidr-file tests/integration/testdata/rich_input/dedup_context.csv \
  -resume e2e/out/rich_buckets.json \
  -format human -disable-api
```

Expected:
- When `stderr` is a TTY and you use `-format human`, the rich dashboard appears.
- If you redirect `stderr` or select `-format json`, `port-scan` shows the non-rich output.

## Scenario 5: Validate inputs (human and JSON)

Goal: Validate the input files before a scan.

Commands:
```bash
go run ./cmd/port-scan validate \
  -cidr-file e2e/inputs/cidr_normal.csv \
  -port-file e2e/inputs/ports.csv \
  -cidr-ip-col source_ip -cidr-ip-cidr-col source_cidr \
  -format human

go run ./cmd/port-scan validate \
  -cidr-file e2e/inputs/cidr_normal.csv \
  -port-file e2e/inputs/ports.csv \
  -cidr-ip-col source_ip -cidr-ip-cidr-col source_cidr \
  -format json
```

Expected:
- Exit `0` for valid input, `1` for invalid input.
- `human` prints readable text. `json` prints validity fields for scripts and CI.

## Scenario 6: Scan with pressure control

Goal: Pause and resume dispatch from the pressure API (scan-only feature).

Command (build a bucket file first):
```bash
go run ./cmd/port-scan scan \
  -cidr-file e2e/inputs/cidr_normal.csv \
  -cidr-ip-col source_ip -cidr-ip-cidr-col source_cidr \
  -resume e2e/out/buckets.json \
  -port-file e2e/inputs/ports.csv \
  -pressure-api http://localhost:8080/api/pressure \
  -pressure-interval 500ms
```

Expected:
- The scanner polls the pressure API and adjusts the dispatch gate.
- The logs show the pause and resume transitions that pressure triggers.
- The third consecutive API failure terminates the scan with a non-zero exit and
  persists the resume snapshot. To isolate the API effects, use `-disable-api`.

## Scenario 7: Resume-in-place after SIGINT

Goal: Interrupt a scan and continue it — the bucket file *is* the resume state.

Command:
```bash
go run ./cmd/port-scan scan \
  -cidr-file e2e/inputs/cidr_normal.csv \
  -cidr-ip-col source_ip -cidr-ip-cidr-col source_cidr \
  -resume e2e/out/buckets.json \
  -port-file e2e/inputs/ports.csv \
  -output e2e/out/scan_results.csv
# Press Ctrl+C (SIGINT) during the run, then re-run the EXACT same command:
go run ./cmd/port-scan scan \
  -cidr-file e2e/inputs/cidr_normal.csv \
  -cidr-ip-col source_ip -cidr-ip-cidr-col source_cidr \
  -resume e2e/out/buckets.json \
  -port-file e2e/inputs/ports.csv \
  -output e2e/out/scan_results.csv
```

Expected:
- On SIGINT (exit `130`) or on an error, `scan` writes the progress back to the
  **same** `-resume` path (`e2e/out/buckets.json`). It overwrites the bucket file
  that it read.
- The second run loads that updated snapshot and continues without duplicate or
  missing records.

Troubleshooting:
- There is no separate `resume_state.json` for the pipeline flow — the bucket
  file passed to `-resume` is both input and checkpoint.

## Scenario 8: Same-second output collision naming

Goal: When two scans start in the same second, observe the `-n` suffix allocation.

Command (build a bucket file first, then run this twice quickly):
```bash
go run ./cmd/port-scan scan -cidr-file e2e/inputs/cidr_normal.csv -cidr-ip-col source_ip -cidr-ip-cidr-col source_cidr -resume e2e/out/buckets.json -port-file e2e/inputs/ports.csv -output e2e/out/scan_results.csv
go run ./cmd/port-scan scan -cidr-file e2e/inputs/cidr_normal.csv -cidr-ip-col source_ip -cidr-ip-cidr-col source_cidr -resume e2e/out/buckets.json -port-file e2e/inputs/ports.csv -output e2e/out/scan_results.csv
```

Expected:
- `scan_results-YYYYMMDDTHHMMSSZ.csv`, then `scan_results-YYYYMMDDTHHMMSSZ-1.csv`.
  `opened_results` uses the same sequence for each batch.

## Scenario 9: e2e parity execution

Goal: Verify the production-like e2e behavior with Docker mocks and report
artifacts.

Command:
```bash
bash e2e/run_e2e.sh
```

Expected:
- The three-step pipeline (`preping` -> `generate-buckets` -> `scan`) runs in
  sequence against the mock services.
- The normal and failure scenarios pass. Artifacts appear under `e2e/out/` (report
  files, batch CSVs, bucket/resume snapshots).

Troubleshooting:
- If e2e fails early, make sure that the Docker daemon and `docker compose` are
  available.

## Scenario 10: From-scratch pre-processing into the pipeline

Goal: Filter a firewall-policy rich CSV with `preprocess`, then feed the output
through the three-step pipeline.

Commands:
```bash
# Step 1 — filter targets by closed CIDRs (prints the output path to stderr)
go run ./cmd/preprocess \
  --input filtered-targets/dc-east/20260503T120000Z/opened_targets.csv \
  --cleaned-cidrs cleaned_cidrs.csv \
  --fab-name dc-east \
  --output-dir ./scan-input

# Step 2 — run the pipeline on the filtered rich input (rich mode: no -port-file)
# Replace <timestamp> with the value printed by step 1.
IN=scan-input/dc-east/<timestamp>/input.csv
go run ./cmd/port-scan generate-buckets -cidr-file "$IN" -buckets-out out/buckets.json
go run ./cmd/port-scan scan -cidr-file "$IN" -resume out/buckets.json -output out/ -disable-api
```

Expected:
- Step 1 prints a `total / kept / dropped` summary and the output path.
- Step 2 buckets and scans the filtered rich targets. `opened_results-*.csv`
  contains only open ports.

Troubleshooting:
- If `preprocess` reports 0 kept rows, make sure that `--fab-name` matches the
  `fab` column in `cleaned_cidrs.csv` (case-sensitive).

## Scenario 11: Re-scan pre-processing into the pipeline

Goal: Promote a minimal `host,port` CSV to rich with `enrich-targets`, filter it
with `preprocess`, then run the pipeline.

Commands:
```bash
go run ./cmd/enrich-targets \
  --input previous-scanned/dc-east/20260503T120000Z/opened_targets.csv \
  --cidr-list cidrs.csv --service-map services.csv --output enriched.csv

go run ./cmd/preprocess \
  --input enriched.csv --cleaned-cidrs cleaned_cidrs.csv \
  --fab-name dc-east --output-dir ./scan-input

# Replace <timestamp> with the value printed by preprocess.
IN=scan-input/dc-east/<timestamp>/input.csv
go run ./cmd/port-scan generate-buckets -cidr-file "$IN" -buckets-out out/buckets.json
go run ./cmd/port-scan scan -cidr-file "$IN" -resume out/buckets.json -output out/ -disable-api
```

Expected:
- `enrich-targets` reports `Enriched N rows from M input rows`. It skips
  unparseable ports and writes a warning for each row.
- `preprocess` prints the total/kept/dropped summary and output path.
- The pipeline runs a full rich-mode scan on the hosts discovered before.

---
**Revised**: 2026-07-22 | **Author**: docs-team
